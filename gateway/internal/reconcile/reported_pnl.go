package reconcile

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// A8.5 交易所回报 PnL 对账（对标 QuantDinger pnl_reconciliation）：
// 用交易所结算口径的已实现盈亏（Binance U 本位 GET /fapi/v1/income
// incomeType=REALIZED_PNL）独立核对本地 trades 表 FIFO 口径已实现盈亏。
// 窗口默认近 24h，diff_pct 超阈值（默认 1%，reconcile_settings 键
// reported_pnl_pct 可覆盖）记 mismatch 并走 notify 告警；单所拉取失败记
// error 行留痕，不中断整体对账轮。
//
// 属主口径说明：本系统的交易所凭证是每所一份全局凭证（凭证保险库 alias，
// 即交易所名），交易所回报口径为账户级，因此每轮每所产生一条全账户行
// （user_id=0，与 reconcile_diffs 无属主差异同惯例）；本地侧聚合该所全部
// 用户的 trades 与账户级 reported 对比，admin 可见全部，普通用户可见
// 全账户行+本人行。

const (
	DefaultReportedPnLWindowH      = 24  // 对账窗口（小时）
	DefaultReportedPnLThresholdPct = 1.0 // diff_pct 告警阈值（百分比）

	// reportedPNLBase 是 diff_pct 分母的兜底值：reported 为 0 时避免除零，
	// 也让不足 1 USDT 的零头差异不至于刷出百分比爆炸的告警。
	reportedPNLBase = 1.0
	// localTradesScanCap 是 FIFO 成本基础单次扫描的成交上限（防拖垮对账轮）。
	localTradesScanCap = 20000
)

// ReportedPnL 归一化的交易所结算口径已实现盈亏流水条目。
type ReportedPnL struct {
	Symbol  string
	Asset   string
	Income  float64
	Time    int64
	TradeID string
	Info    string
}

// ReportedPnLQuerier 查询交易所结算口径已实现盈亏（adapter 可选实现）。
type ReportedPnLQuerier interface {
	GetRealizedPnLIncomes(symbol string, startMs, endMs int64) ([]ReportedPnL, error)
}

// ReportedPnLChecker A8.5 回报 PnL 对账任务。
type ReportedPnLChecker struct {
	repo        *store.ReconcileRepo
	exchangeFor func(name string) any
	window      func() time.Duration
	threshold   func() float64
	now         func() time.Time
	notify      func(title, content, level, channel, eventType string, data map[string]any) // 测试可替换
}

// NewReportedPnLChecker exchangeFor 注入带凭证的 adapter（cmd/server），返回 nil 表示未配置；
// window/threshold 为 nil 时用默认值（Service 注入配置读取闭包）。
func NewReportedPnLChecker(repo *store.ReconcileRepo, exchangeFor func(name string) any, window func() time.Duration, threshold func() float64) *ReportedPnLChecker {
	if window == nil {
		window = func() time.Duration { return DefaultReportedPnLWindowH * time.Hour }
	}
	if threshold == nil {
		threshold = func() float64 { return DefaultReportedPnLThresholdPct }
	}
	return &ReportedPnLChecker{
		repo:        repo,
		exchangeFor: exchangeFor,
		window:      window,
		threshold:   threshold,
		now:         time.Now,
		notify:      notifyDiff,
	}
}

// Run 跑一轮回报 PnL 对账（窗口取配置，默认近 24h）。
func (c *ReportedPnLChecker) Run() (string, error) {
	return c.RunWithWindow(c.window())
}

// RunWithWindow 以自定义窗口跑一轮（admin 手动触发可用 days 换算时长）。
// 单所失败只记 error 行，不中断整体；返回值是摘要，error 仅在框架级故障时非 nil。
func (c *ReportedPnLChecker) RunWithWindow(window time.Duration) (string, error) {
	endMs := c.now().UnixMilli()
	startMs := endMs - window.Milliseconds()

	checked, mismatches, errs := 0, 0, 0
	for _, exName := range liveExchangeNames {
		exAny := c.exchangeFor(exName)
		if exAny == nil {
			continue // 该交易所未配置凭证
		}
		q, ok := exAny.(ReportedPnLQuerier)
		if !ok {
			continue // 该 adapter 未实现回报 PnL 查询
		}
		rec := c.checkOne(exName, exName, "", startMs, endMs, q)
		id, err := c.repo.InsertReportedPnLCheck(rec)
		if err != nil {
			log.Printf("[reconcile] 回报PnL对账落表失败 %s: %v", exName, err)
			continue
		}
		checked++
		switch rec.Status {
		case "mismatch":
			mismatches++
			metrics.RecordReconcileDiff("reported_pnl", exName)
			c.notify(
				fmt.Sprintf("交易所回报PnL对账不符: %s", exName),
				rec.Detail, "WARN", "risk", "reported_pnl_mismatch",
				map[string]any{"check_id": id, "exchange": exName, "symbol": rec.Symbol,
					"local_pnl": rec.LocalPNL, "reported_pnl": rec.ReportedPNL, "diff_pct": rec.DiffPct},
			)
		case "error":
			errs++
		}
	}
	return fmt.Sprintf("checked=%d mismatch=%d error=%d", checked, mismatches, errs), nil
}

// checkOne 对单个交易所凭证跑一遍窗口对账，返回待落库记录（除拉取外无副作用）。
func (c *ReportedPnLChecker) checkOne(exchange, credentialID, symbol string, startMs, endMs int64, q ReportedPnLQuerier) *store.ReportedPnLCheckRecord {
	rec := &store.ReportedPnLCheckRecord{
		UserID:       0, // 全账户行（回报口径为账户级）
		CredentialID: credentialID,
		Exchange:     exchange,
		Symbol:       symbol,
		WindowStart:  startMs,
		WindowEnd:    endMs,
		CheckedAt:    nowMilli(),
	}

	incomes, err := q.GetRealizedPnLIncomes(symbol, startMs, endMs)
	if err != nil {
		rec.Status = "error"
		rec.Detail = "pull reported pnl: " + err.Error()
		return rec
	}
	reported := 0.0
	for _, inc := range incomes {
		reported += inc.Income
	}
	rec.ReportedPNL = reported

	local, scanned, err := c.localRealizedPnL(exchange, symbol, startMs, endMs)
	if err != nil {
		rec.Status = "error"
		rec.Detail = "local trades: " + err.Error()
		return rec
	}
	rec.LocalPNL = local
	rec.Diff = local - reported
	base := abs(reported)
	if base < reportedPNLBase {
		base = reportedPNLBase
	}
	rec.DiffPct = abs(rec.Diff) / base * 100
	rec.Status = "ok"
	if rec.DiffPct > c.threshold() {
		rec.Status = "mismatch"
	}
	rec.Detail = fmt.Sprintf("local=%.8f reported=%.8f diff=%.8f diff_pct=%.4f%% threshold=%.2f%% window_trades_scanned=%d",
		local, reported, rec.Diff, rec.DiffPct, c.threshold(), scanned)
	return rec
}

// fifoLot 一仓待平批次。
type fifoLot struct {
	qty, price float64
}

// matchClose 以 closePrice 成交 qty 去 FIFO 平 lots 队列：realized += (closePrice-开仓价)*量*sign，
// sign=+1 平多头（卖出）、-1 平空头（买入回补）。返回已实现盈亏与未平完的剩余量
// （调用方拿剩余量反向开新仓）。
func matchClose(lots *[]fifoLot, qty, closePrice, sign float64) (realized, leftover float64) {
	leftover = qty
	for leftover > 0 && len(*lots) > 0 {
		head := &(*lots)[0]
		take := head.qty
		if take > leftover {
			take = leftover
		}
		realized += (closePrice - head.price) * take * sign
		head.qty -= take
		leftover -= take
		if head.qty <= 0 {
			*lots = (*lots)[1:]
		}
	}
	return realized, leftover
}

// localRealizedPnL 用 FIFO 从本地 trades 计算窗口内平仓的已实现盈亏（不含手续费，
// 与 Binance REALIZED_PNL 口径一致——手续费是独立 COMMISSION 流水）。成本基础依赖
// 窗口前的建仓成交，故扫描 endMs 之前的全部成交，只把平仓成交落在窗口内的部分计入。
//
// 已知口径 caveat：trades 表不区分现货/合约，交易所侧取 U 本位合约账户口径，
// 混有现货成交时会体现为差异（detail 可见窗口扫描成交数，人工甄别）。
func (c *ReportedPnLChecker) localRealizedPnL(exchange, symbol string, startMs, endMs int64) (pnl float64, scanned int, err error) {
	trades, err := store.NewTradeRepo().ListAscBefore(exchange, symbol, endMs, localTradesScanCap)
	if err != nil {
		return 0, 0, err
	}
	longs := map[string][]fifoLot{}  // symbol -> 多头批次
	shorts := map[string][]fifoLot{} // symbol -> 空头批次
	for _, t := range trades {
		qty, px := t.Quantity, t.Price
		if qty <= 0 || px <= 0 {
			continue
		}
		inWindow := t.CreatedAt >= startMs
		switch strings.ToUpper(t.Side) {
		case "BUY":
			lots := shorts[t.Symbol] // map 元素不可取地址，取出匹配后写回
			r, leftover := matchClose(&lots, qty, px, -1)
			shorts[t.Symbol] = lots
			if inWindow {
				pnl += r
			}
			if leftover > 0 {
				longs[t.Symbol] = append(longs[t.Symbol], fifoLot{qty: leftover, price: px})
			}
		case "SELL":
			lots := longs[t.Symbol]
			r, leftover := matchClose(&lots, qty, px, +1)
			longs[t.Symbol] = lots
			if inWindow {
				pnl += r
			}
			if leftover > 0 {
				shorts[t.Symbol] = append(shorts[t.Symbol], fifoLot{qty: leftover, price: px})
			}
		}
	}
	return pnl, len(trades), nil
}
