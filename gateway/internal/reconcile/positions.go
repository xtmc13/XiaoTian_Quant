package reconcile

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// liveExchangeNames 参与对账的实盘交易所候选（有凭证的才实际查询）。
var liveExchangeNames = []string{
	"binance", "bybit", "okx", "mexc", "gateio", "kraken", "bitget", "coinbase", "alpaca", "ibkr",
}

// PositionQuerier 交易所真实持仓查询（adapter 的 GetPositions 即满足）。
type PositionQuerier interface {
	GetPositions() ([]map[string]any, error)
}

// exchangePosition 归一化后的交易所持仓。
type exchangePosition struct {
	Symbol    string
	SignedQty float64 // 多头为正、空头为负
	EntryPx   float64
}

// extractPositions 把各 adapter GetPositions 的 map 归一化（容忍 spot/futures 两种字段名）。
func extractPositions(raw []map[string]any) []exchangePosition {
	out := make([]exchangePosition, 0, len(raw))
	for _, m := range raw {
		symbol, _ := m["symbol"].(string)
		if symbol == "" {
			continue
		}
		qty := firstFloat(m, "positionAmt", "position_amt", "quantity", "qty", "amount")
		if abs(qty) <= 0 {
			continue // 交易所常返回全合约零持仓列表，忽略
		}
		out = append(out, exchangePosition{
			Symbol:    symbol,
			SignedQty: qty,
			EntryPx:   firstFloat(m, "entryPrice", "entry_price", "avg_entry_price"),
		})
	}
	return out
}

func firstFloat(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch n := v.(type) {
			case float64:
				return n
			case float32:
				return float64(n)
			case int:
				return float64(n)
			case int64:
				return float64(n)
			case json.Number:
				f, _ := n.Float64()
				return f
			case string:
				var f float64
				if _, err := fmt.Sscanf(n, "%g", &f); err == nil {
					return f
				}
			}
		}
	}
	return 0
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// PositionReconciler A8.1 持仓对账：逐交易所比对本地 positions 表 vs 交易所真实持仓。
type PositionReconciler struct {
	repo        *store.ReconcileRepo
	exchangeFor func(name string) any
	autoFix     func() bool
	minDrift    func() float64
}

// NewPositionReconciler exchangeFor 由 cmd/server 注入（返回带凭证的 adapter 或 nil）。
func NewPositionReconciler(repo *store.ReconcileRepo, exchangeFor func(name string) any, autoFix func() bool, minDrift func() float64) *PositionReconciler {
	if autoFix == nil {
		autoFix = func() bool { return false }
	}
	if minDrift == nil {
		minDrift = func() float64 { return DefaultMinDrift }
	}
	return &PositionReconciler{repo: repo, exchangeFor: exchangeFor, autoFix: autoFix, minDrift: minDrift}
}

// Run 跑一轮持仓对账，返回摘要信息。
func (r *PositionReconciler) Run() (string, error) {
	driftCount := 0
	for _, exName := range liveExchangeNames {
		exAny := r.exchangeFor(exName)
		if exAny == nil {
			continue
		}
		q, ok := exAny.(PositionQuerier)
		if !ok {
			log.Printf("[reconcile] %s adapter 未实现 GetPositions，跳过持仓对账", exName)
			continue
		}
		raw, err := q.GetPositions()
		if err != nil {
			// 网络/凭证错误只记日志，下一轮重试，不误报差异。
			log.Printf("[reconcile] %s GetPositions 失败: %v", exName, err)
			continue
		}
		n, err := r.compareExchange(exName, extractPositions(raw), r.minDrift())
		if err != nil {
			return "", err
		}
		driftCount += n
	}
	return fmt.Sprintf("drifts=%d", driftCount), nil
}

// compareExchange 比对单个交易所：本地 OPEN 持仓 vs 交易所持仓。
func (r *PositionReconciler) compareExchange(exName string, exchPos []exchangePosition, minDrift float64) (int, error) {
	exchBySymbol := make(map[string]exchangePosition, len(exchPos))
	for _, p := range exchPos {
		exchBySymbol[p.Symbol] = p
	}

	localList, err := store.NewPositionRepo().List(map[string]any{"exchange": exName, "status": "OPEN"}, 0)
	if err != nil {
		return 0, fmt.Errorf("list local positions: %w", err)
	}
	localSet := make(map[string]bool, len(localList))
	for _, p := range localList {
		localSet[p.Symbol] = true
	}

	drift := 0
	report := func(d *store.ReconcileDiff) {
		created, isNew, err := r.repo.CreateDiff(d)
		if err != nil {
			log.Printf("[reconcile] 落差异失败: %v", err)
			return
		}
		drift++
		if !isNew {
			return // open 差异已存在，不重复通知
		}
		metrics.RecordReconcileDiff(d.DiffType, d.Exchange)
		notifyDiff(
			fmt.Sprintf("持仓漂移: %s %s", d.Exchange, d.Symbol),
			fmt.Sprintf("类型=%s 本地=%.8f 交易所=%.8f (%s)", d.DiffType, d.LocalQty, d.ExchangeQty, d.Detail),
			"WARN", "position", "position_drift",
			map[string]any{"diff_id": created, "exchange": d.Exchange, "symbol": d.Symbol, "type": d.DiffType},
		)
	}

	// 1) 本地 vs 交易所（数量漂移 / 交易所缺失）
	for _, lp := range localList {
		localSigned := lp.Quantity
		if lp.Side == "SHORT" {
			localSigned = -lp.Quantity
		}
		ep, ok := exchBySymbol[lp.Symbol]
		if !ok || abs(ep.SignedQty) <= 0 {
			report(&store.ReconcileDiff{
				UserID:          lp.UserID,
				Exchange:        exName,
				Symbol:          lp.Symbol,
				DiffType:        "position_missing_exchange",
				LocalQty:        localSigned,
				LocalEntryPrice: lp.AvgEntryPrice,
				Detail:          "本地持仓在交易所不存在（可能已平仓/强平）",
			})
			continue
		}
		if abs(localSigned-ep.SignedQty) > minDrift {
			d := &store.ReconcileDiff{
				UserID:          lp.UserID,
				Exchange:        exName,
				Symbol:          lp.Symbol,
				DiffType:        "position_quantity",
				LocalQty:        localSigned,
				ExchangeQty:     ep.SignedQty,
				LocalEntryPrice: lp.AvgEntryPrice,
				ExchangeEntryPx: ep.EntryPx,
				Detail:          "本地持仓数量与交易所不一致",
			}
			report(d)
			if r.autoFix() {
				r.autoFixPosition(exName, lp, ep)
			}
		}
	}

	// 2) 交易所存在但本地缺失
	for _, ep := range exchPos {
		if localSet[ep.Symbol] {
			continue // 本地有记录，已在上面的漂移分支处理
		}
		report(&store.ReconcileDiff{
			Exchange:        exName,
			Symbol:          ep.Symbol,
			DiffType:        "position_missing_local",
			ExchangeQty:     ep.SignedQty,
			ExchangeEntryPx: ep.EntryPx,
			Detail:          "交易所持仓本地未记录（可能网关断连期间开单）",
		})
	}
	return drift, nil
}

// autoFixPosition 自动修正：把本地持仓拉平到交易所值，并写审计日志。
func (r *PositionReconciler) autoFixPosition(exName string, lp *store.PositionRecord, ep exchangePosition) {
	repo := store.NewPositionRepo()
	newQty := abs(ep.SignedQty)
	newSide := "LONG"
	if ep.SignedQty < 0 {
		newSide = "SHORT"
	}
	old := fmt.Sprintf("qty=%.8f entry=%.8f side=%s", lp.Quantity, lp.AvgEntryPrice, lp.Side)
	lp.Quantity = newQty
	lp.Side = newSide
	if ep.EntryPx > 0 {
		lp.AvgEntryPrice = ep.EntryPx
	}
	lp.CostBasis = newQty * lp.AvgEntryPrice
	if err := repo.Update(lp); err != nil {
		log.Printf("[reconcile] 自动修正本地持仓失败 %s/%s: %v", exName, lp.Symbol, err)
		return
	}
	detail := fmt.Sprintf("auto_fix position %s -> qty=%.8f entry=%.8f side=%s", old, newQty, lp.AvgEntryPrice, newSide)
	_ = r.repo.LogAudit("auto_fix_position", exName, lp.Symbol, lp.UserID, detail)
	log.Printf("[reconcile] %s", detail)
	notifyDiff(
		fmt.Sprintf("持仓已自动修正: %s %s", exName, lp.Symbol),
		detail, "INFO", "position", "position_autofix",
		map[string]any{"exchange": exName, "symbol": lp.Symbol},
	)
}
