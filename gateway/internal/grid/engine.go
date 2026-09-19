// Package grid 纯逻辑等差网格引擎（真实网格交易机器人的核心）。
//
// 引擎本身不含 IO、持久化与行情源：由调用方喂价格 tick（OnPriceTick），
// 引擎按挂单簿撮合成交并维护持仓 / 余额 / 已实现盈亏；
// 状态可经 ExportState / LoadState 序列化到 grid_bots.state_json 恢复。
//
// 记账模型（每格独立仓位槽，quote 预算均分）：
//   - 每格 quote 预算 quotePerGrid = Investment / GridCount；
//   - 启动时以 currentPrice 市价买入 Investment 价值的 base（手续费从
//     所得 base 中扣除），base 按格均分为 basePerSlot；
//   - 每格同一时间最多挂一单：level > 启动价挂 SELL（量 = basePerSlot），
//     level < 启动价挂 BUY（花费固定 quotePerGrid，量随成交价定），
//     等于启动价不挂；
//   - BUY@i 成交 → 在 i+1 挂 SELL（量 = 本次买入量），SELL@i 成交 →
//     在 i-1 挂 BUY；边界外不挂；
//   - SELL 腿 realizedPnL += 成交额 - 卖费 - quotePerGrid（该槽成本），
//     买端手续费体现为买到的 base 变少（花费 quotePerGrid 固定）。
//
// short 模式为完全镜像（合约做空网格）：初始建立等规模空头（baseQty 记负、
// 卖出所得记保证金），level > 启动价挂 SELL 开空 entry，level < 启动价挂
// BUY 平空 exit；SELL@i 成交 → 在 i-1 挂配对平空 BUY，BUY@i（exit）成交 →
// 已实现盈亏 += quotePerGrid - 回补花费 - 手续费，并在 i+1 挂回开空单。
// neutral 模式（中性对冲）由 BotEngine 组合一条 long 腿 + 一条 short 腿：
// 两腿共用同一组档位价与各自独立的 Investment/2 预算，独立挂单、独立核算
// 已实现盈亏；净头寸 = 两腿 baseQty 之和（short 腿为负）。
//
// 价格 tick 落在 [Lower, Upper] 之外时无成交（机器人闲置等待回区间）。
package grid

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Side 挂单 / 成交方向。
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// Mode 网格模式：long=现货做多网格（默认，历史行为）；short=合约做空网格
// （初始空头 + 上方档开空 entry / 下方档平空 exit 的镜像记账）；
// neutral=永续对冲双网格（由 BotEngine 组合一条 long 腿 + 一条 short 腿，
// 每条腿独立格子挂单与独立盈亏核算）。
type Mode string

const (
	ModeLong    Mode = "long"
	ModeShort   Mode = "short"
	ModeNeutral Mode = "neutral"
)

// ParseMode 宽松解析模式字符串，非法值回退 long。
func ParseMode(s string) Mode {
	switch Mode(strings.TrimSpace(strings.ToLower(s))) {
	case ModeShort:
		return ModeShort
	case ModeNeutral:
		return ModeNeutral
	default:
		return ModeLong
	}
}

// tickEpsilon 为价位比较的浮点容差。
const tickEpsilon = 1e-9

// Config 网格引擎配置。
type Config struct {
	Symbol       string
	Lower        float64
	Upper        float64
	GridCount    int
	Investment   float64
	FeeRate      float64
	CurrentPrice float64 // 启动建仓参考价，必须在 [Lower, Upper] 内
	BaseQty      float64 // 已有基础币持仓（恢复场景传非零，跳过启动建仓）
	QuoteBalance float64 // 已有计价币余额（恢复场景传非零，跳过启动建仓）
	Mode         Mode    // 单腿方向：ModeLong（默认）/ ModeShort；中性双腿见 BotEngine
}

// Order 挂单（挂单簿状态单元）。
type Order struct {
	Level int     `json:"level"`
	Side  Side    `json:"side"`
	Price float64 `json:"price"`
	// Qty 为基础币数量：SELL 单为卖出量；long 模式 BUY 单为 0（成交时按
	// quotePerGrid 固定花费、量 = 花费*(1-fee)/成交价 计算）；short 模式
	// BUY 单为平空量（= 对应 entry 的开空量）。
	Qty float64 `json:"qty"`
}

// Fill 一笔成交。
type Fill struct {
	Level    int
	Side     Side
	Price    float64 // 成交价位（等于所在档位价）
	Qty      float64 // 基础币数量
	QuoteQty float64 // 成交额（gross，未扣手续费）
	Fee      float64 // 手续费（买入扣 base、卖出扣 quote）
	Pnl      float64 // 本笔计入的已实现盈亏（仅 SELL 腿非零）
	Ts       int64
}

// Engine 等差网格引擎（纯逻辑，可持久化恢复）。
type Engine struct {
	cfg          Config
	levels       []float64 // 档位价，len = GridCount+1，levels[i] = Lower + i*step
	quotePerGrid float64   // 每格 quote 预算 = Investment / GridCount
	orders       map[int]*Order
	baseQty      float64 // 净基础币持仓：long 模式为正；short 模式为负（空头）
	quoteBalance float64
	realizedPnL  float64
	equity0      float64 // 建仓时权益（= equity(CurrentPrice)），腿总盈亏 = equity - equity0
	totalTrades  int
}

// NewEngine 创建引擎并做启动建仓，返回初始挂单集合（按档位升序）。
//
// BaseQty 与 QuoteBalance 同时为 0 视为全新启动：long 模式按 CurrentPrice
// 市价买入 Investment 价值的 base（手续费从 base 中扣除）；short 模式建立
// 同等规模的初始空头（baseQty 记负、卖出所得记保证金，费后净额）。
// 非零则视为沿用已有持仓直接开网（short 模式 BaseQty 为签名负值）；
// 需从保存状态恢复时请优先使用 LoadState。
func NewEngine(cfg Config) (*Engine, []Order, error) {
	e, err := newShell(cfg)
	if err != nil {
		return nil, nil, err
	}
	short := cfg.Mode == ModeShort

	var baseTotal float64
	if cfg.BaseQty == 0 && cfg.QuoteBalance == 0 {
		// 经典网格启动：全部投资按市价换成净头寸，手续费从所得中扣除。
		baseTotal = cfg.Investment * (1 - cfg.FeeRate) / cfg.CurrentPrice
		if short {
			e.baseQty = -baseTotal
			e.quoteBalance = baseTotal * cfg.CurrentPrice
		} else {
			e.baseQty = baseTotal
		}
	} else {
		baseTotal = math.Abs(cfg.BaseQty)
		e.baseQty = cfg.BaseQty
		e.quoteBalance = cfg.QuoteBalance
	}
	basePerSlot := baseTotal / float64(cfg.GridCount)
	e.equity0 = e.baseQty*cfg.CurrentPrice + e.quoteBalance

	// 初始挂单：等于启动价不挂。long 模式：高于启动价挂 SELL（卖出已有
	// base），低于启动价挂 BUY（花费 quotePerGrid 建仓）。short 模式镜像：
	// 高于启动价挂 SELL（开空 entry），低于启动价挂 BUY（平空 exit，量已知）。
	for i, lv := range e.levels {
		switch {
		case lv > cfg.CurrentPrice+tickEpsilon:
			e.orders[i] = &Order{Level: i, Side: SideSell, Price: lv, Qty: basePerSlot}
		case lv < cfg.CurrentPrice-tickEpsilon:
			qty := 0.0
			if short {
				qty = basePerSlot
			}
			e.orders[i] = &Order{Level: i, Side: SideBuy, Price: lv, Qty: qty}
		}
	}
	return e, e.Orders(), nil
}

// LoadState 从 ExportState 导出的状态恢复引擎（不重复启动建仓）。
func LoadState(cfg Config, state map[string]any) (*Engine, error) {
	e, err := newShell(cfg)
	if err != nil {
		return nil, err
	}
	if v, ok := num(state["base_qty"]); ok {
		e.baseQty = v
	}
	if v, ok := num(state["quote_balance"]); ok {
		e.quoteBalance = v
	}
	if v, ok := num(state["realized_pnl"]); ok {
		e.realizedPnL = v
	}
	if v, ok := num(state["total_trades"]); ok {
		e.totalTrades = int(v)
	}
	if v, ok := num(state["initial_equity"]); ok {
		e.equity0 = v
	} else {
		// 存量状态（mode 引入前）无该字段：以恢复时点权益近似建仓权益，
		// 仅影响新加的「腿总盈亏」展示，不影响记账与撮合。
		e.equity0 = e.baseQty*cfg.CurrentPrice + e.quoteBalance
	}
	rawOrders, ok := state["orders"].(map[string]any)
	if !ok {
		return nil, errors.New("grid: state missing orders map")
	}
	for k, v := range rawOrders {
		idx, err := strconv.Atoi(k)
		if err != nil || idx < 0 || idx > cfg.GridCount {
			return nil, fmt.Errorf("grid: invalid order level %q", k)
		}
		om, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("grid: invalid order entry at level %s", k)
		}
		side := Side(fmt.Sprint(om["side"]))
		if side != SideBuy && side != SideSell {
			return nil, fmt.Errorf("grid: invalid order side at level %s", k)
		}
		qty, _ := num(om["qty"])
		switch {
		case side == SideSell && qty <= 0:
			return nil, fmt.Errorf("grid: SELL order at level %s must have qty > 0", k)
		case cfg.Mode == ModeShort && side == SideBuy && qty <= 0:
			return nil, fmt.Errorf("grid: short-mode BUY order at level %s must have qty > 0", k)
		case cfg.Mode != ModeShort && side == SideBuy && qty != 0:
			return nil, fmt.Errorf("grid: long-mode BUY order at level %s must have qty = 0 (computed at fill)", k)
		}
		e.orders[idx] = &Order{Level: idx, Side: side, Price: e.levels[idx], Qty: qty}
	}
	return e, nil
}

// newShell 校验配置并预计算档位，不建仓、不挂单。
func newShell(cfg Config) (*Engine, error) {
	if cfg.Lower >= cfg.Upper {
		return nil, fmt.Errorf("grid: lower %v must be < upper %v", cfg.Lower, cfg.Upper)
	}
	if cfg.GridCount < 2 {
		return nil, fmt.Errorf("grid: grid_count %d must be >= 2", cfg.GridCount)
	}
	if cfg.Investment <= 0 {
		return nil, fmt.Errorf("grid: investment %v must be > 0", cfg.Investment)
	}
	if cfg.FeeRate < 0 || cfg.FeeRate >= 1 {
		return nil, fmt.Errorf("grid: fee_rate %v out of range [0, 1)", cfg.FeeRate)
	}
	if cfg.CurrentPrice < cfg.Lower-tickEpsilon || cfg.CurrentPrice > cfg.Upper+tickEpsilon {
		return nil, fmt.Errorf("grid: current price %v outside [%v, %v]", cfg.CurrentPrice, cfg.Lower, cfg.Upper)
	}
	step := (cfg.Upper - cfg.Lower) / float64(cfg.GridCount)
	levels := make([]float64, cfg.GridCount+1)
	for i := range levels {
		levels[i] = cfg.Lower + float64(i)*step
	}
	return &Engine{
		cfg:          cfg,
		levels:       levels,
		quotePerGrid: cfg.Investment / float64(cfg.GridCount),
		orders:       make(map[int]*Order),
	}, nil
}

// OnPriceTick 喂入一个价格 tick，返回本 tick 产生的成交（按触发顺序）。
//
// BUY 单在 tick <= 挂单价时成交，SELL 单在 tick >= 挂单价时成交；
// 一次 tick 可穿越多档，成交后在相邻对侧档位重挂并循环处理，
// 直到无新成交。tick 落在 [Lower, Upper] 之外时无成交。
func (e *Engine) OnPriceTick(price float64, ts int64) []Fill {
	if price < e.levels[0]-tickEpsilon || price > e.levels[len(e.levels)-1]+tickEpsilon {
		return nil
	}
	var fills []Fill
	for pass := 0; pass < 2; pass++ {
		filled := false
		// SELL 升序处理：SELL@i 成交后在 i-1 挂 BUY，低一档的 SELL
		// 先成交可保证重挂目标位已空闲（其上的 SELL 已成交并删除）。
		for i := 0; i < len(e.levels); i++ {
			ord, ok := e.orders[i]
			if ok && ord.Side == SideSell && price >= e.levels[i]-tickEpsilon {
				fills = append(fills, e.fillSell(i, ord, e.levels[i], ts))
				filled = true
			}
		}
		// BUY 降序处理：long 模式 BUY@i 成交后在 i+1 挂 SELL；short 模式
		// BUY@i 为平空 exit，同样在 i+1 挂回配对开空单。
		for i := len(e.levels) - 1; i >= 0; i-- {
			ord, ok := e.orders[i]
			if ok && ord.Side == SideBuy && price <= e.levels[i]+tickEpsilon {
				fills = append(fills, e.fillBuy(i, ord, e.levels[i], ts))
				filled = true
			}
		}
		if !filled {
			break
		}
	}
	return fills
}

// fillBuy 成交 BUY 单。
//
// long 模式（入口买入）：固定花费 quotePerGrid，手续费从买入的 base 中扣除，
// 并在上一档（i+1）挂 SELL 卖出本次买入量。
// short 模式（平空 exit）：按挂单量回补空头，已实现盈亏 +=
// quotePerGrid（该槽开空所得）- 回补花费 - 手续费，并在 i+1 挂回配对开空单。
func (e *Engine) fillBuy(i int, ord *Order, levelPrice float64, ts int64) Fill {
	if e.cfg.Mode == ModeShort {
		cost := ord.Qty * levelPrice
		fee := cost * e.cfg.FeeRate
		pnl := e.quotePerGrid - cost - fee
		e.quoteBalance -= cost + fee
		e.baseQty += ord.Qty
		e.realizedPnL += pnl
		e.totalTrades++
		delete(e.orders, i)
		if i+1 < len(e.levels) {
			e.orders[i+1] = &Order{Level: i + 1, Side: SideSell, Price: e.levels[i+1], Qty: ord.Qty}
		}
		return Fill{
			Level:    i,
			Side:     SideBuy,
			Price:    levelPrice,
			Qty:      ord.Qty,
			QuoteQty: cost,
			Fee:      fee,
			Pnl:      pnl,
			Ts:       ts,
		}
	}

	spend := e.quotePerGrid
	fee := spend * e.cfg.FeeRate
	baseRecv := (spend - fee) / levelPrice
	e.quoteBalance -= spend
	e.baseQty += baseRecv
	e.totalTrades++
	delete(e.orders, i)
	if i+1 < len(e.levels) {
		e.orders[i+1] = &Order{Level: i + 1, Side: SideSell, Price: e.levels[i+1], Qty: baseRecv}
	}
	return Fill{
		Level:    i,
		Side:     SideBuy,
		Price:    levelPrice,
		Qty:      baseRecv,
		QuoteQty: spend,
		Fee:      fee,
		Pnl:      0,
		Ts:       ts,
	}
}

// fillSell 成交 SELL 单。
//
// long 模式（出口卖出）：卖出该格 base，手续费从所得 quote 中扣除，已实现盈亏
// += 净成交额 - 该槽成本 quotePerGrid，并在下一档（i-1）挂 BUY。
// short 模式（开空 entry）：按挂单量新增空头，开空所得进保证金并扣手续费，
// 已实现盈亏为 0，并在 i-1 挂配对平空单。
func (e *Engine) fillSell(i int, ord *Order, levelPrice float64, ts int64) Fill {
	gross := ord.Qty * levelPrice
	fee := gross * e.cfg.FeeRate
	if e.cfg.Mode == ModeShort {
		e.quoteBalance += gross - fee
		e.baseQty -= ord.Qty
		e.totalTrades++
		delete(e.orders, i)
		if i-1 >= 0 {
			e.orders[i-1] = &Order{Level: i - 1, Side: SideBuy, Price: e.levels[i-1], Qty: ord.Qty}
		}
		return Fill{
			Level:    i,
			Side:     SideSell,
			Price:    levelPrice,
			Qty:      ord.Qty,
			QuoteQty: gross,
			Fee:      fee,
			Pnl:      0,
			Ts:       ts,
		}
	}

	e.quoteBalance += gross - fee
	e.baseQty -= ord.Qty
	pnl := gross - fee - e.quotePerGrid
	e.realizedPnL += pnl
	e.totalTrades++
	delete(e.orders, i)
	if i-1 >= 0 {
		e.orders[i-1] = &Order{Level: i - 1, Side: SideBuy, Price: e.levels[i-1]}
	}
	return Fill{
		Level:    i,
		Side:     SideSell,
		Price:    levelPrice,
		Qty:      ord.Qty,
		QuoteQty: gross,
		Fee:      fee,
		Pnl:      pnl,
		Ts:       ts,
	}
}

// Snapshot 返回当前权益、已实现盈亏与挂单数：equity = baseQty*price + quoteBalance。
func (e *Engine) Snapshot(price float64, ts int64) (equity float64, realized float64, openOrders int) {
	return e.baseQty*price + e.quoteBalance, e.realizedPnL, len(e.orders)
}

// ExportState 导出可 JSON 序列化的状态（供持久化到 state_json）。
func (e *Engine) ExportState() map[string]any {
	orders := make(map[string]any, len(e.orders))
	for i, ord := range e.orders {
		orders[strconv.Itoa(i)] = map[string]any{
			"side": string(ord.Side),
			"qty":  ord.Qty,
		}
	}
	return map[string]any{
		"base_qty":       e.baseQty,
		"quote_balance":  e.quoteBalance,
		"realized_pnl":   e.realizedPnL,
		"initial_equity": e.equity0,
		"total_trades":   e.totalTrades,
		"orders":         orders,
	}
}

// Orders 返回当前挂单集合（按档位升序）。
func (e *Engine) Orders() []Order {
	out := make([]Order, 0, len(e.orders))
	for _, ord := range e.orders {
		out = append(out, *ord)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Level < out[b].Level })
	return out
}

// Levels 返回档位价（levels[i] = Lower + i*step，len = GridCount+1）。
func (e *Engine) Levels() []float64 { return e.levels }

// QuotePerGrid 返回每格 quote 预算。
func (e *Engine) QuotePerGrid() float64 { return e.quotePerGrid }

func (e *Engine) BaseQty() float64      { return e.baseQty }
func (e *Engine) QuoteBalance() float64 { return e.quoteBalance }
func (e *Engine) RealizedPnL() float64  { return e.realizedPnL }
func (e *Engine) TotalTrades() int      { return e.totalTrades }

// Equity0 返回建仓时权益（腿总盈亏 = 当前权益 - Equity0）。
func (e *Engine) Equity0() float64 { return e.equity0 }

// TotalPnL 返回腿总盈亏（已实现 + 浮动，含手续费）：equity - equity0。
func (e *Engine) TotalPnL(price float64) float64 {
	return e.baseQty*price + e.quoteBalance - e.equity0
}

// num 宽松读取 JSON 反序列化后的数值（float64 / int / int64 等）。
func num(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}
