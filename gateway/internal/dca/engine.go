// Package dca — DCA 定投机器人引擎（A1.2，对标 QuantDinger DCA bot）。
//
// 纯逻辑状态机：按 interval_minutes 定时买入固定 quote_amount 的现货（只做多），
// 达 max_orders / period_budget 停止买入；持仓均价之上 take_profit_pct 触发整体
// 卖出止盈，stop_loss_pct 硬止损，trailing_enabled 开启追踪止盈（进入盈利区后
// 从最高点回撤 take_profit_pct 即卖出）。所有推进只在成交回报确认后发生
// （ApplyFill），Tick 只产生意图（Action）。引擎非线程安全，由 Runner 的
// 独占 goroutine 串行驱动，与 grid.Engine 同一约定。
package dca

import (
	"fmt"
	"time"
)

// Config configures one DCA bot engine.
type Config struct {
	Symbol          string
	QuoteAmount     float64 // 每单定投金额（计价币）
	IntervalMinutes int     // 定投间隔（分钟）
	MaxOrders       int     // 最大买入订单数；0 = 不限
	PeriodBudget    float64 // 周期预算硬上限（累计买入金额）；0 = 不限
	TakeProfitPct   float64 // 止盈百分比，0.05 = 均价之上 5%；0 = 不启用
	StopLossPct     float64 // 硬止损百分比，0.05 = 均价之下 5%；0 = 不启用
	TrailingEnabled bool    // 追踪止盈：进入盈利区后按最高点回撤止盈
}

// Validate 校验引擎参数，返回错误消息（空串表示通过）。
func (c Config) Validate() string {
	if c.QuoteAmount <= 0 {
		return "quote_amount 必须大于 0"
	}
	if c.IntervalMinutes < 1 {
		return "interval_minutes 至少为 1"
	}
	if c.MaxOrders < 0 {
		return "max_orders 不能为负数"
	}
	if c.PeriodBudget < 0 {
		return "period_budget 不能为负数"
	}
	if c.TakeProfitPct < 0 || c.StopLossPct < 0 {
		return "take_profit_pct / stop_loss_pct 不能为负数"
	}
	if c.TakeProfitPct >= 1 || c.StopLossPct >= 1 {
		return "take_profit_pct / stop_loss_pct 必须小于 1"
	}
	return ""
}

// Action 是一次 Tick 产生的下单意图；由 Runner 执行并回报成交后 ApplyFill。
type Action struct {
	Buy  *BuyAction  // 定时买入固定金额
	Sell *SellAction // 整体卖出（止盈/止损）
	// Done 为 true 表示买入限额耗尽且已空仓，引擎到达终态（status=finished）。
	Done bool
}

// BuyAction buys approximately QuoteAmount of quote currency worth of base.
type BuyAction struct {
	QuoteAmount float64
}

// SellAction sells the whole position.
type SellAction struct {
	Reason string // take_profit / trailing_take_profit / stop_loss
}

// Engine is the pure DCA state machine (not goroutine-safe).
type Engine struct {
	cfg Config

	filledOrders  int
	totalInvested float64
	baseQty       float64
	avgPrice      float64
	realizedPnL   float64
	lastBuyAt     time.Time
	highestPrice  float64
}

// NewEngine creates a DCA engine from config. StartPrice seeds the trailing
// reference; the first scheduled buy happens immediately on the first Tick.
func NewEngine(cfg Config, startPrice float64) *Engine {
	e := &Engine{cfg: cfg}
	if startPrice > 0 {
		e.highestPrice = startPrice
	}
	return e
}

// Config returns the engine config.
func (e *Engine) Config() Config { return e.cfg }

// ── State accessors（供 Runner 持久化与状态查询）──

func (e *Engine) FilledOrders() int      { return e.filledOrders }
func (e *Engine) TotalInvested() float64 { return e.totalInvested }
func (e *Engine) BaseQty() float64       { return e.baseQty }
func (e *Engine) AvgPrice() float64      { return e.avgPrice }
func (e *Engine) RealizedPnL() float64   { return e.realizedPnL }
func (e *Engine) HighestPrice() float64  { return e.highestPrice }
func (e *Engine) LastBuyAt() time.Time   { return e.lastBuyAt }
func (e *Engine) InPosition() bool       { return e.baseQty > 0 && e.avgPrice > 0 }

// PositionValue returns the current position value at the given price.
func (e *Engine) PositionValue(price float64) float64 { return e.baseQty * price }

// UnrealizedPnL returns the current unrealized P&L at the given price.
func (e *Engine) UnrealizedPnL(price float64) float64 {
	if !e.InPosition() {
		return 0
	}
	return (price - e.avgPrice) * e.baseQty
}

// ── Core decision ──

// Tick evaluates the engine at the current price/time and returns the actions
// to execute. Exit (TP/SL) takes precedence over the scheduled buy; buys
// accumulate into the position every elapsed interval. Buy limits reached →
// no more buys; TP/SL still monitored while holding; once flat with exhausted
// limits → Done.
func (e *Engine) Tick(price float64, now time.Time) Action {
	if price <= 0 {
		return Action{}
	}
	if e.InPosition() {
		if a := e.checkExit(price); a != nil {
			return Action{Sell: a}
		}
	}
	if e.canBuy(now) {
		return Action{Buy: &BuyAction{QuoteAmount: e.cfg.QuoteAmount}}
	}
	// 空仓且买入限额耗尽 → 终态（持仓中继续监控 TP/SL，不算终态）。
	if !e.InPosition() && e.buyLimitsExhausted() {
		return Action{Done: true}
	}
	return Action{}
}

// canBuy: 定时已到 && 订单数未达 max_orders && 预算未超 period_budget。
func (e *Engine) canBuy(now time.Time) bool {
	if !e.lastBuyAt.IsZero() && now.Sub(e.lastBuyAt) < time.Duration(e.cfg.IntervalMinutes)*time.Minute {
		return false
	}
	if e.cfg.MaxOrders > 0 && e.filledOrders >= e.cfg.MaxOrders {
		return false
	}
	if e.cfg.PeriodBudget > 0 && e.totalInvested+e.cfg.QuoteAmount > e.cfg.PeriodBudget+1e-9 {
		return false
	}
	return true
}

// buyLimitsExhausted 报告订单数/预算是否已不可能再买入（未到间隔不算耗尽）。
func (e *Engine) buyLimitsExhausted() bool {
	if e.cfg.MaxOrders > 0 && e.filledOrders >= e.cfg.MaxOrders {
		return true
	}
	if e.cfg.PeriodBudget > 0 && e.totalInvested+e.cfg.QuoteAmount > e.cfg.PeriodBudget+1e-9 {
		return true
	}
	return false
}

// checkExit decides TP / trailing TP / SL. Returns nil when holding and no exit.
func (e *Engine) checkExit(price float64) *SellAction {
	if e.cfg.StopLossPct > 0 && price <= e.avgPrice*(1-e.cfg.StopLossPct) {
		return &SellAction{Reason: "stop_loss"}
	}
	if e.cfg.TakeProfitPct > 0 {
		if price > e.highestPrice {
			e.highestPrice = price
		}
		if e.cfg.TrailingEnabled {
			// 追踪止盈：先进入盈利区（激活），随后从最高点回撤即卖出。
			if price >= e.avgPrice*(1+e.cfg.TakeProfitPct) {
				if e.highestPrice > 0 && price <= e.highestPrice*(1-e.cfg.TakeProfitPct) {
					return &SellAction{Reason: "trailing_take_profit"}
				}
			}
		} else if price >= e.avgPrice*(1+e.cfg.TakeProfitPct) {
			return &SellAction{Reason: "take_profit"}
		}
	}
	return nil
}

// ── Fill confirmation（只有成交回报才推进状态）──

// ApplyBuyFill records a confirmed buy fill and advances the schedule.
func (e *Engine) ApplyBuyFill(qty, price float64, now time.Time) error {
	if qty <= 0 || price <= 0 {
		return fmt.Errorf("dca: invalid buy fill qty=%v price=%v", qty, price)
	}
	quoteQty := qty * price
	e.filledOrders++
	e.totalInvested += quoteQty
	e.avgPrice = (e.avgPrice*e.baseQty + quoteQty) / (e.baseQty + qty)
	e.baseQty += qty
	e.lastBuyAt = now
	return nil
}

// ApplySellFill records a confirmed sell fill, realizes P&L and resets the
// position (engine reaches terminal state; Runner will mark status=finished).
func (e *Engine) ApplySellFill(qty, price float64) error {
	if qty <= 0 || price <= 0 {
		return fmt.Errorf("dca: invalid sell fill qty=%v price=%v", qty, price)
	}
	if qty > e.baseQty+1e-9 {
		return fmt.Errorf("dca: sell qty %v exceeds position %v", qty, e.baseQty)
	}
	e.realizedPnL += (price - e.avgPrice) * qty
	e.baseQty = 0
	e.avgPrice = 0
	e.highestPrice = 0
	return nil
}

// Restore rebuilds engine state from persisted columns (resume after restart).
func Restore(cfg Config, filledOrders int, totalInvested, baseQty, avgPrice, realizedPnL float64, lastBuyAt int64, highestPrice, startPrice float64) *Engine {
	e := NewEngine(cfg, startPrice)
	e.filledOrders = filledOrders
	e.totalInvested = totalInvested
	e.baseQty = baseQty
	e.avgPrice = avgPrice
	e.realizedPnL = realizedPnL
	if lastBuyAt > 0 {
		e.lastBuyAt = time.UnixMilli(lastBuyAt)
	}
	if highestPrice > 0 {
		e.highestPrice = highestPrice
	}
	return e
}
