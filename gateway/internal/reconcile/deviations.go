package reconcile

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// A8.4 实盘偏差监控：paper 信号（下单预期价）vs 实盘成交的偏离。
//   - 滑点：限价/市价实盘订单的成交均价偏离预期价超过阈值（默认 0.5%）
//   - 挂单超时：实盘订单长时间未成交（默认 15 分钟）
// 两类偏差都落 reconcile_deviations（order_id+kind 唯一去重）+ risk_events + 通知。

// DeviationMonitor A8.4 偏差监控任务。
type DeviationMonitor struct {
	repo    *store.ReconcileRepo
	cfgFunc func() *Config
	now     func() time.Time
}

// NewDeviationMonitor 构造偏差监控任务；cfgFunc 提供当前生效配置（Service 注入）。
func NewDeviationMonitor(repo *store.ReconcileRepo, cfgFunc func() *Config) *DeviationMonitor {
	if cfgFunc == nil {
		cfgFunc = func() *Config { return LoadConfig(store.NewReconcileRepo()) }
	}
	return &DeviationMonitor{repo: repo, cfgFunc: cfgFunc, now: time.Now}
}

func isPaperExchangeName(ex string) bool {
	ex = strings.ToLower(strings.TrimSpace(ex))
	return ex == "" || ex == "paper" || ex == "matching"
}

// Run 跑一轮偏差扫描。配置取自 Service 配置（阈值/超时可在设置表调整）。
func (m *DeviationMonitor) Run() (string, error) {
	cfg := m.cfgFunc()
	nowMs := m.now().UnixMilli()

	recent, err := store.GetOrderRepo().ListRecent(nowMs-2*int64(cfg.StuckTimeout.Milliseconds())-3600_000, 1000)
	if err != nil {
		return "", fmt.Errorf("list recent orders: %w", err)
	}

	slippageHits, stuckHits := 0, 0
	for _, o := range recent {
		if isPaperExchangeName(o.Exchange) {
			continue // 只监控实盘订单
		}
		st := model.OrderStatus(o.Status)
		if st == model.StatusFilled || st == model.StatusPartiallyFilled {
			if m.checkSlippage(o, cfg) {
				slippageHits++
			}
		}
		if st == model.StatusNew || st == model.StatusPartiallyFilled || st == model.StatusPending {
			if int64(o.UpdatedAt) < nowMs-cfg.StuckTimeout.Milliseconds() {
				if m.reportStuck(o, cfg) {
					stuckHits++
				}
			}
		}
	}
	return fmt.Sprintf("slippage=%d stuck=%d", slippageHits, stuckHits), nil
}

// checkSlippage 限价单成交均价偏离超阈 → 偏差记录。已记录过（唯一索引）则跳过。
func (m *DeviationMonitor) checkSlippage(o *store.OrderRecord, cfg *Config) bool {
	if o.OrderType != string(model.TypeLimit) || o.Price <= 0 || o.AvgFillPrice <= 0 {
		return false
	}
	slipPct := abs(o.AvgFillPrice-o.Price) / o.Price * 100
	if slipPct < cfg.SlippagePct {
		return false
	}
	id, created, err := m.repo.CreateDeviation(&store.ReconcileDeviation{
		OrderID:       o.ID,
		UserID:        int64(o.UserID),
		Symbol:        o.Symbol,
		Exchange:      o.Exchange,
		Kind:          "slippage",
		ExpectedPrice: o.Price,
		AvgPrice:      o.AvgFillPrice,
		SlippagePct:   slipPct,
		Detail: fmt.Sprintf("订单 %s 成交均价 %.8f 偏离限价 %.8f %.2f%%（阈值 %.2f%%）",
			o.ID, o.AvgFillPrice, o.Price, slipPct, cfg.SlippagePct),
	})
	if err != nil {
		log.Printf("[reconcile] 滑点偏差落表失败: %v", err)
		return false
	}
	if !created {
		return false
	}
	m.raiseRiskEvent(o, "slippage", slipPct, cfg)
	notifyDiff(
		fmt.Sprintf("实盘滑点告警: %s", o.Symbol),
		fmt.Sprintf("订单 %s (%s) 滑点 %.2f%% 超阈值 %.2f%%", o.ID, o.Exchange, slipPct, cfg.SlippagePct),
		riskLevelFor(slipPct, cfg.SlippagePct), "risk", "slippage_alert",
		map[string]any{"deviation_id": id, "order_id": o.ID, "symbol": o.Symbol, "slippage_pct": slipPct},
	)
	return true
}

// reportStuck 长时间未成交 → 偏差记录（同订单只报一次）。
func (m *DeviationMonitor) reportStuck(o *store.OrderRecord, cfg *Config) bool {
	id, created, err := m.repo.CreateDeviation(&store.ReconcileDeviation{
		OrderID:       o.ID,
		UserID:        int64(o.UserID),
		Symbol:        o.Symbol,
		Exchange:      o.Exchange,
		Kind:          "stuck",
		ExpectedPrice: o.Price,
		Detail: fmt.Sprintf("订单 %s (%s) 超过 %d 秒未成交（status=%s filled=%.8f/%.8f）",
			o.ID, o.Exchange, int64(cfg.StuckTimeout.Seconds()), o.Status, o.Filled, o.Quantity),
	})
	if err != nil {
		log.Printf("[reconcile] 未成交偏差落表失败: %v", err)
		return false
	}
	if !created {
		return false
	}
	m.raiseRiskEvent(o, "order_stuck", 0, cfg)
	notifyDiff(
		fmt.Sprintf("订单长时间未成交: %s", o.Symbol),
		fmt.Sprintf("订单 %s (%s) 已挂单超过 %d 秒仍未成交", o.ID, o.Exchange, int64(cfg.StuckTimeout.Seconds())),
		"WARN", "risk", "order_stuck_alert",
		map[string]any{"deviation_id": id, "order_id": o.ID, "symbol": o.Symbol},
	)
	return true
}

// raiseRiskEvent 偏差同步写入 risk_events（与现有风控事件同库，便于统一审计）。
func (m *DeviationMonitor) raiseRiskEvent(o *store.OrderRecord, checkName string, slipPct float64, cfg *Config) {
	level := "WARN"
	if checkName == "slippage" && slipPct >= cfg.SlippagePct*4 {
		level = "CRITICAL"
	}
	ctx := fmt.Sprintf(`{"order_id":%q,"exchange":%q,"slippage_pct":%.4f}`, o.ID, o.Exchange, slipPct)
	if err := store.NewRiskEventRepo().Create(&store.RiskEventRecord{
		Level:     level,
		CheckName: checkName,
		Message:   fmt.Sprintf("%s: %s %s order=%s", checkName, o.Exchange, o.Symbol, o.ID),
		Symbol:    o.Symbol,
		Context:   ctx,
	}); err != nil {
		log.Printf("[reconcile] risk_events 写入失败: %v", err)
	}
}

// riskLevelFor 滑点超阈值 4 倍升级为 CRITICAL。
func riskLevelFor(slipPct, threshold float64) string {
	if slipPct >= threshold*4 {
		return "CRITICAL"
	}
	return "WARN"
}
