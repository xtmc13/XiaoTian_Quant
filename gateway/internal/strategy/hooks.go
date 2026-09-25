package strategy

import (
	"log"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
)

// ── 策略契约扩展钩子（v1.2，对标 freqtrade IStrategy）──
// 与 engine_ext.go 的多周期/universe 同一模式：全部为**可选接口**，策略实现
// 哪个就启用哪个，引擎用类型断言探测（断言前必须经 UnwrapStrategy 解包
// NamedStrategy），BaseStrategy 与存量策略行为完全不变。
//
// ConfirmTradeEntry / ConfirmTradeExit / CustomStakeAmount / CustomStoploss /
// AdjustEntryPrice 已在 Strategy 主接口声明（BaseStrategy 有默认实现），
// v1.2 把它们贯通到信号出口（emitSignal）；本文件定义主接口之外的四个
// 可选能力：AdjustTradePosition / MaxPositionAdjustments /
// CheckEntryTimeout / CheckExitTimeout。
//
// 钩子 panic 统一 recover 并回退默认行为（确认类=放行、金额类=0=默认、
// 超时类=不接管），与 dispatch 订阅回调的 recover 同口径：单个策略钩子
// 崩溃不得拖垮引擎。

// PositionAdjuster 对标 freqtrade adjust_trade_position（DCA 动态加减仓）：
// 持仓期间每根主周期 K 线 OnBar 分发后，引擎询问策略是否调整仓位。
// 返回计价币（USDT）金额：
//
//	>0 加仓：按 bar 收盘价折算数量，生成与持仓同向的入场信号
//	    （同样过 ConfirmTradeEntry 与风控 protection 检查）
//	<0 减仓：|金额| 折算数量生成部分平仓（CLOSE）信号
//	0  不调整
//
// 加仓/减仓次数受 MaxPositionAdjustmentsProvider 限制（未实现 = 不限）。
// 引擎侧持仓快照由 app 层注入（Engine.SetPositionLookup）；未注入持仓视图
// 时该钩子不生效（回测由 backtest.Runner 的同源接口独立支持）。
type PositionAdjuster interface {
	AdjustTradePosition(pos *Position, bar model.Bar) float64
}

// MaxPositionAdjustmentsProvider 对标 freqtrade max_entry_position_adjustment：
// 限制单笔持仓的**加仓**次数（减仓是降风险动作，不受此限）；返回值 <=0
// 表示不限（缺省行为）。持仓归零（无持仓）后计数自动清零。
type MaxPositionAdjustmentsProvider interface {
	MaxPositionAdjustments() int
}

// EntryTimeoutDecider 对标 freqtrade check_entry_timeout：未成交入场挂单
// 超时时由策略决定撤单行为。返回 true = 撤单（同现有默认逻辑），
// false = 保留挂单继续等待（重置计时，下个超时周期再问）。
// 策略未实现时走 order.TimeoutTracker 现有默认（超时撤单）。
type EntryTimeoutDecider interface {
	CheckEntryTimeout(order model.OrderData) bool
}

// ExitTimeoutDecider 对标 freqtrade check_exit_timeout：未成交出场挂单超时
// 由策略决定。返回 true = 撤单（现有默认逻辑，含 N 次后紧急市价平仓），
// false = 保留挂单。
type ExitTimeoutDecider interface {
	CheckExitTimeout(order model.OrderData) bool
}

// ── 引擎侧钩子执行 ──

// SetPositionLookup 注入持仓视图（app 层接 PortfolioManager）：按策略名 +
// symbol 返回当前持仓快照，无持仓返回 nil。ConfirmTradeExit 与
// AdjustTradePosition 的决策依据；nil 时这两个钩子退化为默认行为。
func (e *Engine) SetPositionLookup(fn func(strategyName, symbol string) *Position) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.positionLookup = fn
}

// SetBalanceLookup 注入可用余额视图（CustomStakeAmount 的 availableBalance
// 入参）；nil 时传 0（freqtrade 语义里等价 max_stake 未知，由策略自行权衡）。
func (e *Engine) SetBalanceLookup(fn func() float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.balanceLookup = fn
}

func (e *Engine) positionFor(strategyName, symbol string) *Position {
	e.mu.RLock()
	lookup := e.positionLookup
	e.mu.RUnlock()
	if lookup == nil {
		return nil
	}
	return lookup(strategyName, symbol)
}

func (e *Engine) availableBalance() float64 {
	e.mu.RLock()
	lookup := e.balanceLookup
	e.mu.RUnlock()
	if lookup == nil {
		return 0
	}
	return lookup()
}

func (e *Engine) adjCount(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.adjCounts[name]
}

func (e *Engine) adjIncr(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.adjCounts[name]++
}

func (e *Engine) adjReset(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.adjCounts, name)
}

// applyTradeHooks 在信号出口前应用契约钩子：CustomStakeAmount 折算默认数量、
// ConfirmTradeEntry / ConfirmTradeExit 最后一刻否决。返回 false = 信号被
// 策略否决（不下单、不广播）。core 必须是解包后的真实策略实例。
func (e *Engine) applyTradeHooks(s, core Strategy, signal *model.Signal) bool {
	switch strings.ToUpper(strings.TrimSpace(signal.Direction)) {
	case "LONG", "SHORT", "BUY", "SELL":
		if signal.Qty <= 0 {
			stake := safeCustomStake(core, e.availableBalance(), signal)
			if stake > 0 {
				if px := e.lastPrice(signal.Symbol); px > 0 {
					signal.Qty = stake / px
				}
			}
		}
		if !safeConfirmEntry(core, signal) {
			log.Printf("[StrategyEngine] %s entry signal vetoed by ConfirmTradeEntry (%s %s)",
				s.Name(), signal.Symbol, signal.Direction)
			return false
		}
	case "CLOSE", "EXIT":
		if !safeConfirmExit(core, e.positionFor(s.Name(), signal.Symbol)) {
			log.Printf("[StrategyEngine] %s exit signal vetoed by ConfirmTradeExit (%s)",
				s.Name(), signal.Symbol)
			return false
		}
	}
	return true
}

// lastPrice 取 symbol 最新 K 线收盘价（CustomStakeAmount 金额折算数量用）。
func (e *Engine) lastPrice(symbol string) float64 {
	md := e.MarketData()
	if md == nil {
		return 0
	}
	bar, ok := md.LastBar(symbol)
	if !ok {
		return 0
	}
	return bar.Close
}

// maybeAdjustPosition 在主周期 K 线分发后驱动 PositionAdjuster（对标
// freqtrade adjust_trade_position）：持仓期间每根 K 线询问加/减仓，产出的
// 调整单走 emitSignal —— 同样过 ConfirmTradeEntry/Exit 与 protection 风控。
// s 用于引擎内状态键（配置 id），core 是解包后的真实实例。
func (e *Engine) maybeAdjustPosition(s, core Strategy, bar model.Bar) {
	adj, ok := core.(PositionAdjuster)
	if !ok {
		return
	}
	name := s.Name()
	pos := e.positionFor(name, bar.Symbol)
	if pos == nil || pos.Quantity <= 0 {
		e.adjReset(name)
		return
	}
	amt := safeAdjustPosition(adj, pos, bar)
	if amt == 0 {
		return
	}
	price := bar.Close
	if price <= 0 {
		return
	}
	signal := &model.Signal{
		Symbol:    strings.ToUpper(strings.TrimSpace(pos.Symbol)),
		Qty:       amt / price,
		Reason:    "position_adjust",
		Timestamp: bar.Time,
	}
	if amt > 0 {
		// 次数上限只约束加仓（freqtrade max_entry_position_adjustment 同口径：
		// 减仓是降风险动作，永不被上限拦截）。
		if mp, ok2 := core.(MaxPositionAdjustmentsProvider); ok2 {
			if maxAdj := mp.MaxPositionAdjustments(); maxAdj > 0 && e.adjCount(name) >= maxAdj {
				return
			}
		}
		signal.Direction = "LONG"
		if strings.EqualFold(pos.Side, "SHORT") {
			signal.Direction = "SHORT"
		}
		e.adjIncr(name)
	} else {
		signal.Direction = "CLOSE"
		signal.Qty = (-amt) / price
		signal.Reason = "position_reduce"
	}
	e.emitSignal(s, signal, nil)
}

// TimeoutDecider 返回可注入 order.TimeoutTracker（SetDecider）的决策函数：
// 按 symbol 找运行中策略，实现 EntryTimeoutDecider / ExitTimeoutDecider 的
// 策略获得撤单决定权；无策略接管时 ok=false，tracker 走现有默认逻辑。
func (e *Engine) TimeoutDecider() order.TimeoutDecider {
	return func(state order.OrderState) (string, bool) {
		e.mu.RLock()
		names := e.symbolMap[state.Symbol]
		strategies := make([]Strategy, 0, len(names))
		for _, n := range names {
			if s, ok := e.strategies[n]; ok && s.IsRunning() {
				strategies = append(strategies, s)
			}
		}
		e.mu.RUnlock()
		for _, s := range strategies {
			core := UnwrapStrategy(s)
			od := model.OrderData{
				ID:       state.OrderID,
				Symbol:   state.Symbol,
				Side:     model.OrderSide(strings.ToUpper(state.Side)),
				Price:    state.Price,
				Quantity: state.Quantity,
			}
			if state.Type == "exit" {
				if d, ok := core.(ExitTimeoutDecider); ok {
					if safeCheckExitTimeout(d, od) {
						return "cancel", true
					}
					return "keep", true
				}
				continue
			}
			if d, ok := core.(EntryTimeoutDecider); ok {
				if safeCheckEntryTimeout(d, od) {
					return "cancel", true
				}
				return "keep", true
			}
		}
		return "", false
	}
}

// ── panic 安全包装（钩子崩溃回退默认行为）──

func safeConfirmEntry(core Strategy, sig *model.Signal) (allow bool) {
	allow = true
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[StrategyEngine] panic in ConfirmTradeEntry (recovered, default allow): %v", r)
		}
	}()
	return core.ConfirmTradeEntry(sig)
}

func safeConfirmExit(core Strategy, pos *Position) (allow bool) {
	allow = true
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[StrategyEngine] panic in ConfirmTradeExit (recovered, default allow): %v", r)
		}
	}()
	return core.ConfirmTradeExit(pos)
}

func safeCustomStake(core Strategy, balance float64, sig *model.Signal) (stake float64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[StrategyEngine] panic in CustomStakeAmount (recovered, default 0): %v", r)
			stake = 0
		}
	}()
	return core.CustomStakeAmount(balance, sig)
}

func safeAdjustPosition(adj PositionAdjuster, pos *Position, bar model.Bar) (amt float64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[StrategyEngine] panic in AdjustTradePosition (recovered, no adjust): %v", r)
			amt = 0
		}
	}()
	return adj.AdjustTradePosition(pos, bar)
}

func safeCheckEntryTimeout(d EntryTimeoutDecider, od model.OrderData) (cancel bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[StrategyEngine] panic in CheckEntryTimeout (recovered, default cancel): %v", r)
			cancel = true
		}
	}()
	return d.CheckEntryTimeout(od)
}

func safeCheckExitTimeout(d ExitTimeoutDecider, od model.OrderData) (cancel bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[StrategyEngine] panic in CheckExitTimeout (recovered, default cancel): %v", r)
			cancel = true
		}
	}()
	return d.CheckExitTimeout(od)
}
