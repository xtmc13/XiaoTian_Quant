// BotEngine：按模式组合单腿/双腿网格引擎（A1.3 中性对冲）。
//
// long    → 一条 long 腿（现货做多网格，历史行为）；
// short   → 一条 short 腿（合约做空网格，镜像记账）；
// neutral → long 腿 + short 腿（永续对冲双腿：各自独立格子挂单、独立
//
//	盈亏核算，预算均分 Investment/2，净头寸 = 两腿 baseQty 之和）。
//
// 状态持久化格式（grid_bots.state_json）：
//
//	{"mode": "neutral", "legs": {"long": {<Engine.ExportState>}, "short": {...}}}
//
// mode 引入前的存量状态为裸 Engine.ExportState（long 腿），LoadBotState
// 自动识别并视为 long 腿，保证存量机器人可恢复。
package grid

import (
	"fmt"
)

// legLong / legShort 为成交与状态里的腿标识。
const (
	LegLong  = "long"
	LegShort = "short"
)

// LegOrder 带腿标识的挂单（详情接口展示用）。
type LegOrder struct {
	Leg string `json:"leg"`
	Order
}

// LegFill 带腿标识的成交（runner 落库 grid_bot_trades.leg）。
type LegFill struct {
	Leg string `json:"leg"`
	Fill
}

// LegView 详情接口的单腿视图：格子挂单状态 + 独立盈亏核算。
type LegView struct {
	Leg          string  `json:"leg"`
	BaseQty      float64 `json:"base_qty"`      // 净基础币持仓（short 腿为负）
	QuoteBalance float64 `json:"quote_balance"` // 保证金/计价币余额
	RealizedPnL  float64 `json:"realized_pnl"`  // 该腿已实现盈亏
	TotalPnL     float64 `json:"total_pnl"`     // 该腿总盈亏（已实现+浮动）
	OpenOrders   int     `json:"open_orders"`
	Orders       []Order `json:"orders"` // 当前挂单（按档位升序）
}

// BotEngine 一个网格机器人的完整引擎（1~2 条腿）。
type BotEngine struct {
	mode  Mode
	long  *Engine // long / neutral 模式非空
	short *Engine // short / neutral 模式非空
}

// NewBotEngine 按模式建仓：单腿模式用满 Investment；neutral 双腿均分
// Investment/2。返回带腿标识的初始挂单集合。
func NewBotEngine(cfg Config, mode Mode) (*BotEngine, []LegOrder, error) {
	b := &BotEngine{mode: mode}
	var out []LegOrder
	switch mode {
	case ModeLong:
		eng, orders, err := NewEngine(cfg)
		if err != nil {
			return nil, nil, err
		}
		b.long = eng
		for _, o := range orders {
			out = append(out, LegOrder{Leg: LegLong, Order: o})
		}
	case ModeShort:
		cfg.Mode = ModeShort
		eng, orders, err := NewEngine(cfg)
		if err != nil {
			return nil, nil, err
		}
		b.short = eng
		for _, o := range orders {
			out = append(out, LegOrder{Leg: LegShort, Order: o})
		}
	case ModeNeutral:
		longCfg := cfg
		longCfg.Mode = ModeLong
		longCfg.Investment = cfg.Investment / 2
		shortCfg := cfg
		shortCfg.Mode = ModeShort
		shortCfg.Investment = cfg.Investment / 2
		longEng, longOrders, err := NewEngine(longCfg)
		if err != nil {
			return nil, nil, fmt.Errorf("neutral long leg: %w", err)
		}
		shortEng, shortOrders, err := NewEngine(shortCfg)
		if err != nil {
			return nil, nil, fmt.Errorf("neutral short leg: %w", err)
		}
		b.long, b.short = longEng, shortEng
		for _, o := range longOrders {
			out = append(out, LegOrder{Leg: LegLong, Order: o})
		}
		for _, o := range shortOrders {
			out = append(out, LegOrder{Leg: LegShort, Order: o})
		}
	default:
		return nil, nil, fmt.Errorf("grid: unknown mode %q", mode)
	}
	return b, out, nil
}

// LoadBotState 从持久化状态恢复 BotEngine。state 为 mode 引入前导出的
// 裸单腿状态时视为 long 腿（存量兼容）；含 "legs" 键时按腿恢复，
// 只恢复该模式应有的腿（缺失腿报错）。
func LoadBotState(cfg Config, mode Mode, state map[string]any) (*BotEngine, error) {
	b := &BotEngine{mode: mode}
	rawLegs, wrapped := state["legs"].(map[string]any)
	if !wrapped {
		// 存量格式：裸 Engine.ExportState，即 long 腿。
		if mode != ModeLong {
			return nil, fmt.Errorf("grid: legacy state without legs cannot restore mode %q", mode)
		}
		eng, err := LoadState(cfg, state)
		if err != nil {
			return nil, err
		}
		b.long = eng
		return b, nil
	}
	load := func(leg string, m Mode) (*Engine, error) {
		raw, ok := rawLegs[leg].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("grid: state missing %s leg", leg)
		}
		legCfg := cfg
		legCfg.Mode = m
		if mode == ModeNeutral {
			legCfg.Investment = cfg.Investment / 2
		}
		return LoadState(legCfg, raw)
	}
	switch mode {
	case ModeLong:
		eng, err := load(LegLong, ModeLong)
		if err != nil {
			return nil, err
		}
		b.long = eng
	case ModeShort:
		eng, err := load(LegShort, ModeShort)
		if err != nil {
			return nil, err
		}
		b.short = eng
	case ModeNeutral:
		longEng, err := load(LegLong, ModeLong)
		if err != nil {
			return nil, err
		}
		shortEng, err := load(LegShort, ModeShort)
		if err != nil {
			return nil, err
		}
		b.long, b.short = longEng, shortEng
	default:
		return nil, fmt.Errorf("grid: unknown mode %q", mode)
	}
	return b, nil
}

// Mode 返回引擎模式。
func (b *BotEngine) Mode() Mode { return b.mode }

// Long / Short 返回对应腿引擎（不存在返回 nil）。
func (b *BotEngine) Long() *Engine  { return b.long }
func (b *BotEngine) Short() *Engine { return b.short }

// OnPriceTick 把价格 tick 喂给所有腿，返回带腿标识的成交
// （long 腿在前，short 腿在后；tick 出区间时全部无成交）。
func (b *BotEngine) OnPriceTick(price float64, ts int64) []LegFill {
	var out []LegFill
	if b.long != nil {
		for _, f := range b.long.OnPriceTick(price, ts) {
			out = append(out, LegFill{Leg: LegLong, Fill: f})
		}
	}
	if b.short != nil {
		for _, f := range b.short.OnPriceTick(price, ts) {
			out = append(out, LegFill{Leg: LegShort, Fill: f})
		}
	}
	return out
}

// Snapshot 聚合所有腿：equity=总权益，realized=总已实现盈亏，
// openOrders=总挂单数。
func (b *BotEngine) Snapshot(price float64, ts int64) (equity float64, realized float64, openOrders int) {
	var eq, rl float64
	var oo int
	if b.long != nil {
		e, r, o := b.long.Snapshot(price, ts)
		eq += e
		rl += r
		oo += o
	}
	if b.short != nil {
		e, r, o := b.short.Snapshot(price, ts)
		eq += e
		rl += r
		oo += o
	}
	return eq, rl, oo
}

// ExportState 导出持久化状态（{"mode","legs":{...}} 包装格式）。
func (b *BotEngine) ExportState() map[string]any {
	legs := map[string]any{}
	if b.long != nil {
		legs[LegLong] = b.long.ExportState()
	}
	if b.short != nil {
		legs[LegShort] = b.short.ExportState()
	}
	return map[string]any{
		"mode": string(b.mode),
		"legs": legs,
	}
}

// BaseQty 聚合净基础币持仓（净头寸：short 腿为负）。
func (b *BotEngine) BaseQty() float64 {
	var v float64
	if b.long != nil {
		v += b.long.BaseQty()
	}
	if b.short != nil {
		v += b.short.BaseQty()
	}
	return v
}

// QuoteBalance 聚合 quote 余额。
func (b *BotEngine) QuoteBalance() float64 {
	var v float64
	if b.long != nil {
		v += b.long.QuoteBalance()
	}
	if b.short != nil {
		v += b.short.QuoteBalance()
	}
	return v
}

// RealizedPnL 聚合已实现盈亏。
func (b *BotEngine) RealizedPnL() float64 {
	var v float64
	if b.long != nil {
		v += b.long.RealizedPnL()
	}
	if b.short != nil {
		v += b.short.RealizedPnL()
	}
	return v
}

// TotalTrades 聚合成交笔数。
func (b *BotEngine) TotalTrades() int {
	var v int
	if b.long != nil {
		v += b.long.TotalTrades()
	}
	if b.short != nil {
		v += b.short.TotalTrades()
	}
	return v
}

// NetPosition 返回整体净头寸（= 聚合 BaseQty：多头腿为正、空头腿为负）。
func (b *BotEngine) NetPosition() float64 { return b.BaseQty() }

// LegViews 返回每条腿的格子状态与独立盈亏视图（long 腿在前）。
func (b *BotEngine) LegViews(price float64) []LegView {
	var out []LegView
	appendLeg := func(leg string, e *Engine) {
		if e == nil {
			return
		}
		out = append(out, LegView{
			Leg:          leg,
			BaseQty:      e.BaseQty(),
			QuoteBalance: e.QuoteBalance(),
			RealizedPnL:  e.RealizedPnL(),
			TotalPnL:     e.TotalPnL(price),
			OpenOrders:   len(e.Orders()),
			Orders:       e.Orders(),
		})
	}
	appendLeg(LegLong, b.long)
	appendLeg(LegShort, b.short)
	return out
}

// LegViewsFromState 不构造引擎、直接从持久化状态解析单腿视图
// （详情接口离线展示用；price 传 0 时 TotalPnL 以 0 价估算浮动盈亏）。
// 裸单腿（存量）状态视为 long 腿。
func LegViewsFromState(mode Mode, state map[string]any, cfg Config, price float64) ([]LegView, error) {
	b, err := LoadBotState(cfg, mode, state)
	if err != nil {
		return nil, err
	}
	return b.LegViews(price), nil
}

// CountOpenOrders 从持久化状态统计总挂单数（跨腿；兼容裸单腿存量格式）。
func CountOpenOrders(state map[string]any) int {
	count := func(legState map[string]any) int {
		orders, _ := legState["orders"].(map[string]any)
		return len(orders)
	}
	if rawLegs, ok := state["legs"].(map[string]any); ok {
		n := 0
		for _, v := range rawLegs {
			if legState, ok := v.(map[string]any); ok {
				n += count(legState)
			}
		}
		return n
	}
	return count(state)
}
