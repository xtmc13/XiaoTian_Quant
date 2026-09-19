// Package lmartin — 分层马丁格尔机器人引擎（A1.3，对标 QuantDinger layered
// martingale）。
//
// 多分配组（groups）各自独立跑一条马丁阶梯：首层买入 quote_amount，之后价格
// 每较首层参考价下跌 price_deviation_pct × (层-1) 触发下一层加仓，层金额按
// multiplier 倍投。硬上限语义（与 A1.4 一致）：达到 max_layers 停止加仓，
// 累计投入达到 budget_cap 拒绝再加（budget_cap=0 表示不限）；只有成交回报
// 确认后才推进到下一层（pending_layer 跟踪在途订单，未成交不推进）。
// 组内持仓整体止盈（支持追踪止盈）/硬止损，退出后该组 finished；
// 全部组 finished 后引擎到达终态。引擎非线程安全，由 Runner 独占 goroutine
// 串行驱动，与 dca/grid 引擎同一约定。
package lmartin

import (
	"fmt"
	"math"
)

// GroupConfig configures one martingale group (one ladder).
type GroupConfig struct {
	QuoteAmount float64 // 首层买入金额（计价币）
	Multiplier  float64 // 层间倍投系数；0 → 默认 2
	MaxLayers   int     // 层数硬上限（含首层）；<1 → 默认 5
	BudgetCap   float64 // 组预算硬限（累计投入）；0 = 不限
}

// Config configures one layered martingale bot engine.
type Config struct {
	Symbol          string
	DeviationPct    float64 // 每层加仓触发跌幅，0.03 = 较首层参考价每深 3% 加一层
	TakeProfitPct   float64 // 止盈百分比（相对组内持仓均价）；0 = 不启用
	StopLossPct     float64 // 硬止损百分比（相对组内持仓均价）；0 = 不启用
	TrailingEnabled bool    // 追踪止盈：进入盈利区后按最高点回撤止盈
	Groups          []GroupConfig
}

// Validate 校验引擎参数，返回错误消息（空串表示通过）。
func (c Config) Validate() string {
	if c.DeviationPct <= 0 || c.DeviationPct >= 1 {
		return "price_deviation_pct 必须在 (0, 1) 区间"
	}
	if c.TakeProfitPct < 0 || c.StopLossPct < 0 {
		return "take_profit_pct / stop_loss_pct 不能为负数"
	}
	if c.TakeProfitPct >= 1 || c.StopLossPct >= 1 {
		return "take_profit_pct / stop_loss_pct 必须小于 1"
	}
	if len(c.Groups) == 0 {
		return "至少需要一个分组"
	}
	if len(c.Groups) > 20 {
		return "分组数量最多 20 个"
	}
	for i, g := range c.Groups {
		if g.QuoteAmount <= 0 {
			return fmt.Sprintf("第 %d 组 quote_amount 必须大于 0", i+1)
		}
		if g.Multiplier < 0 {
			return fmt.Sprintf("第 %d 组 multiplier 不能为负数", i+1)
		}
		if g.MaxLayers < 0 {
			return fmt.Sprintf("第 %d 组 max_layers 不能为负数", i+1)
		}
		if g.BudgetCap < 0 {
			return fmt.Sprintf("第 %d 组 budget_cap 不能为负数", i+1)
		}
	}
	return ""
}

// groupDefaults normalizes zero values to defaults.
func (g *GroupConfig) normalize() {
	if g.Multiplier == 0 {
		g.Multiplier = 2
	}
	if g.MaxLayers < 1 {
		g.MaxLayers = 5
	}
}

// Action 是一次 Tick 产生的下单意图集合（每组最多一个）；Runner 执行并
// 回报成交后按组 ApplyFill。Finished 为 true 表示全部组到达终态。
type Action struct {
	Buys     []BuyAction
	Sells    []SellAction
	Finished bool
}

// BuyAction is a layer-entry buy intent for one group.
type BuyAction struct {
	Group       int     // 组下标（0-based）
	Layer       int     // 目标层（1-based）
	QuoteAmount float64 // 本层买入金额
}

// SellAction is a full-position exit intent for one group.
type SellAction struct {
	Group  int
	Reason string // take_profit / trailing_take_profit / stop_loss
}

// GroupState is the runtime state of one group.
type GroupState struct {
	cfg GroupConfig

	deviation     float64 // 引擎级：每层加仓触发跌幅
	takeProfitPct float64 // 引擎级：止盈百分比
	stopLossPct   float64 // 引擎级：硬止损百分比
	trailing      bool    // 引擎级：追踪止盈

	layer         int     // 已成交层数（0=尚未建仓）
	pendingLayer  int     // 在途订单层（0=无）；成交确认后才推进 layer
	totalInvested float64 // 累计投入（计价币）
	baseQty       float64
	avgPrice      float64
	entryPrice    float64 // 首层参考价（加仓触发基准）
	highestPrice  float64 // 追踪止盈高点
	realizedPnL   float64 // 本组已实现盈亏
	finished      bool
}

// Engine is the pure layered-martingale state machine (not goroutine-safe).
type Engine struct {
	cfg    Config
	groups []*GroupState

	realizedPnL float64
	totalTrades int
}

// NewEngine creates a layered martingale engine. startPrice is the reference
// price for first-layer entries; the first buy of every group is emitted on
// the first Tick.
func NewEngine(cfg Config, startPrice float64) *Engine {
	groups := make([]*GroupState, len(cfg.Groups))
	for i, g := range cfg.Groups {
		g.normalize()
		gc := g
		gs := &GroupState{
			cfg:           gc,
			deviation:     cfg.DeviationPct,
			takeProfitPct: cfg.TakeProfitPct,
			stopLossPct:   cfg.StopLossPct,
			trailing:      cfg.TrailingEnabled,
		}
		if startPrice > 0 {
			gs.highestPrice = startPrice
		}
		groups[i] = gs
	}
	return &Engine{cfg: cfg, groups: groups}
}

// Config returns the engine config.
func (e *Engine) Config() Config { return e.cfg }

// ── State accessors（供 Runner 持久化与状态查询）──

func (e *Engine) GroupCount() int { return len(e.groups) }

func (e *Engine) Group(i int) *GroupState { return e.groups[i] }

func (e *Engine) RealizedPnL() float64 { return e.realizedPnL }
func (e *Engine) TotalTrades() int     { return e.totalTrades }

// AllFinished 报告是否全部组到达终态（均止盈/止损/限额退出）。
func (e *Engine) AllFinished() bool {
	for _, g := range e.groups {
		if !g.finished {
			return false
		}
	}
	return len(e.groups) > 0
}

// ── GroupState accessors ──

func (g *GroupState) Layer() int             { return g.layer }
func (g *GroupState) PendingLayer() int      { return g.pendingLayer }
func (g *GroupState) TotalInvested() float64 { return g.totalInvested }
func (g *GroupState) BaseQty() float64       { return g.baseQty }
func (g *GroupState) AvgPrice() float64      { return g.avgPrice }
func (g *GroupState) EntryPrice() float64    { return g.entryPrice }
func (g *GroupState) HighestPrice() float64  { return g.highestPrice }
func (g *GroupState) RealizedPnL() float64   { return g.realizedPnL }
func (g *GroupState) Finished() bool         { return g.finished }
func (g *GroupState) InPosition() bool       { return g.baseQty > 0 && g.avgPrice > 0 }

// Status returns the group's coarse status for persistence.
func (g *GroupState) Status() string {
	switch {
	case g.finished:
		return "finished"
	case g.layer > 0:
		return "running"
	default:
		return "idle"
	}
}

// LayerQuoteAmount returns the quote amount of layer L (1-based).
func (g *GroupState) LayerQuoteAmount(layer int) float64 {
	return g.cfg.QuoteAmount * math.Pow(g.cfg.Multiplier, float64(layer-1))
}

// UnrealizedPnL returns the group's unrealized P&L at the given price.
func (g *GroupState) UnrealizedPnL(price float64) float64 {
	if !g.InPosition() {
		return 0
	}
	return (price - g.avgPrice) * g.baseQty
}

// Config returns the group's static configuration.
func (g *GroupState) Config() GroupConfig { return g.cfg }

// ── Core decision ──

// Tick evaluates all groups at the current price and returns the actions to
// execute. Exit (TP/SL) takes precedence over layer entries; a group issues at
// most one order per Tick and never stacks pending orders.
func (e *Engine) Tick(price float64) Action {
	var act Action
	if price <= 0 {
		return act
	}
	for i, g := range e.groups {
		if g.finished {
			continue
		}
		if g.InPosition() {
			if reason := g.checkExit(price); reason != "" {
				act.Sells = append(act.Sells, SellAction{Group: i, Reason: reason})
				continue
			}
		}
		if layer, quote, ok := g.nextLayer(price); ok {
			act.Buys = append(act.Buys, BuyAction{Group: i, Layer: layer, QuoteAmount: quote})
		}
	}
	act.Finished = e.AllFinished()
	return act
}

// nextLayer decides the group's next buy: 首层立即建仓；其后价格跌破
// entry*(1-deviation*(L-1)) 触发第 L 层。层数/预算硬限与在途订单（未成交
// 不推进）在此统一加闸。
func (g *GroupState) nextLayer(price float64) (int, float64, bool) {
	if g.pendingLayer > 0 {
		return 0, 0, false // 有在途订单：成交确认前不加新层
	}
	next := g.layer + 1
	if next > g.cfg.MaxLayers {
		return 0, 0, false // 层数硬上限
	}
	quote := g.LayerQuoteAmount(next)
	if g.cfg.BudgetCap > 0 && g.totalInvested+quote > g.cfg.BudgetCap+1e-9 {
		return 0, 0, false // 组预算硬限
	}
	if g.layer == 0 {
		return next, quote, true // 首层：立即建仓
	}
	target := g.entryPrice * (1 - g.deviation*float64(next-1))
	if target <= 0 || price > target {
		return 0, 0, false // 跌幅未到触发位
	}
	return next, quote, true
}

// checkExit decides TP / trailing TP / SL for the group's position.
func (g *GroupState) checkExit(price float64) string {
	if g.stopLossPct > 0 && price <= g.avgPrice*(1-g.stopLossPct) {
		return "stop_loss"
	}
	if g.takeProfitPct > 0 {
		if price > g.highestPrice {
			g.highestPrice = price
		}
		if g.trailing {
			if price >= g.avgPrice*(1+g.takeProfitPct) &&
				g.highestPrice > 0 && price <= g.highestPrice*(1-g.takeProfitPct) {
				return "trailing_take_profit"
			}
		} else if price >= g.avgPrice*(1+g.takeProfitPct) {
			return "take_profit"
		}
	}
	return ""
}

// ── Fill confirmation（只有成交回报才推进状态）──

// ApplyBuyFill records a confirmed layer fill for the given group and
// advances the layer. Must be called with the layer returned by the Tick
// action (the runner passes action.Layer).
func (e *Engine) ApplyBuyFill(group, layer int, qty, price float64) error {
	if group < 0 || group >= len(e.groups) {
		return fmt.Errorf("lmartin: invalid group %d", group)
	}
	if qty <= 0 || price <= 0 {
		return fmt.Errorf("lmartin: invalid buy fill qty=%v price=%v", qty, price)
	}
	g := e.groups[group]
	if layer != g.pendingLayer && layer != g.layer+1 {
		return fmt.Errorf("lmartin: group %d unexpected layer %d (pending=%d layer=%d)",
			group, layer, g.pendingLayer, g.layer)
	}
	quoteQty := qty * price
	g.layer = layer
	g.pendingLayer = 0
	g.totalInvested += quoteQty
	if g.layer == 1 {
		g.entryPrice = price
	}
	g.avgPrice = (g.avgPrice*g.baseQty + quoteQty) / (g.baseQty + qty)
	g.baseQty += qty
	e.totalTrades++
	return nil
}

// ApplySellFill records a confirmed full-position sell for the given group,
// realizes P&L and marks the group finished (one martingale cycle per group).
func (e *Engine) ApplySellFill(group int, qty, price float64) error {
	if group < 0 || group >= len(e.groups) {
		return fmt.Errorf("lmartin: invalid group %d", group)
	}
	if qty <= 0 || price <= 0 {
		return fmt.Errorf("lmartin: invalid sell fill qty=%v price=%v", qty, price)
	}
	g := e.groups[group]
	if qty > g.baseQty+1e-9 {
		return fmt.Errorf("lmartin: sell qty %v exceeds group position %v", qty, g.baseQty)
	}
	g.realizedPnL += (price - g.avgPrice) * qty
	e.realizedPnL += (price - g.avgPrice) * qty
	g.baseQty = 0
	g.avgPrice = 0
	g.highestPrice = 0
	g.finished = true
	e.totalTrades++
	return nil
}

// MarkPending 记录组内已发出未成交的订单层（在途）；成交回报前 nextLayer 拒绝加仓。
func (e *Engine) MarkPending(group, layer int) error {
	if group < 0 || group >= len(e.groups) {
		return fmt.Errorf("lmartin: invalid group %d", group)
	}
	g := e.groups[group]
	if g.pendingLayer != 0 {
		return fmt.Errorf("lmartin: group %d already has pending layer %d", group, g.pendingLayer)
	}
	g.pendingLayer = layer
	return nil
}

// CancelPending 清除在途订单层（订单被拒/失败时调用），允许后续重新触发。
func (e *Engine) CancelPending(group int) error {
	if group < 0 || group >= len(e.groups) {
		return fmt.Errorf("lmartin: invalid group %d", group)
	}
	e.groups[group].pendingLayer = 0
	return nil
}

// SetState 从落库列恢复组状态。在途层一律清零（进程重启后在途单不可追溯，
// 由价格条件重新触发，宁可不重加也不重复加仓）；status=="finished" 恢复终态。
func (g *GroupState) SetState(layer int, totalInvested, baseQty, avgPrice, entryPrice, highestPrice float64, _ int, status string) {
	g.layer = layer
	g.totalInvested = totalInvested
	g.baseQty = baseQty
	g.avgPrice = avgPrice
	g.entryPrice = entryPrice
	g.highestPrice = highestPrice
	g.pendingLayer = 0
	g.finished = status == "finished"
}
