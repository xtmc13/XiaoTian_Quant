package cra

import (
	"math"
	"strings"
)

// PositionSide describes the current holding direction.
type PositionSide string

const (
	SideLong  PositionSide = "long"
	SideShort PositionSide = "short"
)

// EntryLot 单档持仓（分档记账）：每笔入场成交追加一档，Price 为该档成交均价、
// Qty 为该档剩余数量。尾单/首尾止盈按各档成本独立判定盈利；与
// TotalQty/TotalCost/AvgEntryPrice 同源维护（lots 是真值，聚合量由它重算）。
type EntryLot struct {
	Price float64 `json:"price"`
	Qty   float64 `json:"qty"`
}

// CRAState holds the runtime position/loop state for a CRA strategy instance.
type CRAState struct {
	InPosition       bool
	PositionCount    int // number of filled entry orders (0 = first not filled yet)
	PendingAddCount  int // entry signals sent but not yet filled
	LoopExecuted     int
	Side             PositionSide
	EntryPrice       float64 // first order trigger/reference price
	AvgEntryPrice    float64 // weighted average filled price
	TotalQty         float64 // total filled quantity (notional for contract)
	TotalCost        float64 // total amount spent (used for spot qty averaging)
	HighestPrice     float64 // highest price since entry
	LowestPrice      float64 // lowest price since entry
	PendingAdd       bool
	TriggerLowPrice  float64
	TriggerHighPrice float64
	WaterfallPaused  bool
	HighestProfitPct float64 // tracked for moving take-profit

	// Lots 分档持仓（首档=首单，尾档=最近一笔补仓成交）。旧状态/聚合重建
	// 没有分档时由 currentLots 按均价合成单档（tail/head_tail 退化为该档）。
	Lots []EntryLot
	// TailPeakProfitPct 档级止盈（tail/head_tail）的盈利峰值，档集合变更
	// （新成交/部分平仓核销）即重置重记。与 HighestProfitPct（全仓/移动止盈
	// 口径）相互独立。
	TailPeakProfitPct float64
	// PendingCloseKind/PendingCloseQty 在途部分平仓（尾单/首尾止盈信号已发出、
	// 待成交确认）："" | "tail" | "head_tail"。在途期间阻挡一切新信号
	// （防重复出场超卖），成交/拒单/撤单终态后清除。full 全平直接
	// ExitPosition，不经在途标记。
	PendingCloseKind string
	PendingCloseQty  float64
}

// Reset clears all runtime state.
func (s *CRAState) Reset() {
	*s = CRAState{}
}

// ResetForNextLoop prepares state for the next cycle after a full close.
func (s *CRAState) ResetForNextLoop() {
	s.InPosition = false
	s.PositionCount = 0
	s.PendingAddCount = 0
	s.EntryPrice = 0
	s.AvgEntryPrice = 0
	s.TotalQty = 0
	s.TotalCost = 0
	s.HighestPrice = 0
	s.LowestPrice = 0
	s.PendingAdd = false
	s.TriggerLowPrice = 0
	s.TriggerHighPrice = 0
	s.HighestProfitPct = 0
	s.Lots = nil
	s.TailPeakProfitPct = 0
	s.PendingCloseKind = ""
	s.PendingCloseQty = 0
}

// UpdateExtremes updates highest/lowest prices seen while in position.
func (s *CRAState) UpdateExtremes(price float64) {
	if s.HighestPrice == 0 || price > s.HighestPrice {
		s.HighestPrice = price
	}
	if s.LowestPrice == 0 || price < s.LowestPrice {
		s.LowestPrice = price
		if s.PendingAdd {
			s.TriggerLowPrice = price
		}
	}
}

// RecordFill updates average entry price and total quantity after a fill.
// For contract, qty is the notional value of the position.
// 分档记账：每笔入场成交追加一档（尾单/首尾止盈按档成本判定）；档集合
// 变更后档级盈利峰值（TailPeakProfitPct）作废重记。
func (s *CRAState) RecordFill(price, qty float64, side SideBuySell) {
	if side == SideSell || side == SideClose {
		return
	}
	if price > 0 && qty > 0 {
		s.Lots = append(s.Lots, EntryLot{Price: price, Qty: qty})
		s.TailPeakProfitPct = 0
	}
	if s.TotalQty == 0 {
		s.AvgEntryPrice = price
		s.TotalQty = qty
		s.TotalCost = price * qty
	} else {
		s.TotalCost += price * qty
		s.TotalQty += qty
		if s.TotalQty > 0 {
			s.AvgEntryPrice = s.TotalCost / s.TotalQty
		}
	}
}

// Type aliases to avoid importing model here.
type SideBuySell int

const (
	SideBuy SideBuySell = iota
	SideSell
	SideClose
)

// ShouldAddPosition checks whether the next add-position should be triggered.
// It requires price drop >= spread from the reference entry price, then a callback bounce.
func (s *CRAState) ShouldAddPosition(price float64, cfg *AddPositionItem) bool {
	if cfg == nil {
		return false
	}
	ref := s.EntryPrice
	if ref <= 0 {
		return false
	}
	if s.Side == SideShort {
		// For shorts, price must rise by spread, then pull back.
		risePct := (price - ref) / ref
		if risePct < cfg.Spread {
			return false
		}
		if !s.PendingAdd {
			s.PendingAdd = true
			s.TriggerHighPrice = price
			return false
		}
		if s.TriggerHighPrice > 0 && price <= s.TriggerHighPrice*(1-cfg.Callback) {
			s.PendingAdd = false
			s.TriggerHighPrice = 0
			return true
		}
		return false
	}

	dropPct := (ref - price) / ref
	if dropPct < cfg.Spread {
		return false
	}
	if !s.PendingAdd {
		s.PendingAdd = true
		s.TriggerLowPrice = price
		return false
	}
	if s.TriggerLowPrice > 0 && price >= s.TriggerLowPrice*(1+cfg.Callback) {
		s.PendingAdd = false
		s.TriggerLowPrice = 0
		return true
	}
	return false
}

// ProfitPct returns current unrealized profit percentage for the position.
func (s *CRAState) ProfitPct(price float64) float64 {
	if s.AvgEntryPrice <= 0 {
		return 0
	}
	if s.Side == SideShort {
		return (s.AvgEntryPrice - price) / s.AvgEntryPrice
	}
	return (price - s.AvgEntryPrice) / s.AvgEntryPrice
}

// CheckStaticTakeProfit evaluates static TP with optional callback.
// 注意：本函数是全仓口径（均价达线全平），take_profit_method 的分支在调用方
// （BaseCRAStrategy.evaluateTakeProfit）：tail/head_tail 走 CheckTailTakeProfit /
// CheckHeadTailTakeProfit 的档级判定。method 参数保留仅为签名兼容。
func (s *CRAState) CheckStaticTakeProfit(price float64, method string, tpRatio, callback float64) bool {
	if s.AvgEntryPrice <= 0 {
		return false
	}
	profit := s.ProfitPct(price)
	if profit < tpRatio {
		return false
	}
	if callback > 0 {
		if s.HighestProfitPct == 0 || profit > s.HighestProfitPct {
			s.HighestProfitPct = profit
		}
		if s.HighestProfitPct > 0 {
			drawback := s.HighestProfitPct - profit
			if drawback >= callback {
				return true
			}
		}
		return false
	}
	return true
}

// ── 止盈方式三态分档判定（币富语义）──
//
// full      全仓止盈：全仓盈利（均价）达止盈比例+回调后卖出全部仓位（现状）。
// tail      尾单止盈：最后一档自身盈利达止盈比例+回调后只卖出最后一档减仓，
//           其余档位不动继续持仓。
// head_tail 首尾止盈：首档与尾档同时盈利达线后先行出仓，只卖首档+尾档
//           （例：补了 7 仓可将第 1 仓和第 7 仓盈利后卖出，剩余 5 仓继续）。
//
// tp_mode=moving（移动止盈）时三态失效、按全仓移动止盈执行（币富名词解释
// #29："移动止盈开启后分仓止盈/首尾止盈失效，按固定止盈执行"），见
// BaseCRAStrategy.evaluateTakeProfit。

// currentLots 返回当前分档持仓；旧状态/聚合重建只有总量没有分档时按均价
// 合成单档（此时 tail/head_tail 的"档"即全仓，语义退化为全仓止盈）。
func (s *CRAState) currentLots() []EntryLot {
	if len(s.Lots) == 0 && s.TotalQty > 0 && s.AvgEntryPrice > 0 {
		s.Lots = []EntryLot{{Price: s.AvgEntryPrice, Qty: s.TotalQty}}
	}
	return s.Lots
}

// lotProfitPct 单档盈利比例（方向敏感，与 ProfitPct 同口径）。
func (s *CRAState) lotProfitPct(entry, price float64) float64 {
	if entry <= 0 {
		return 0
	}
	if s.Side == SideShort {
		return (entry - price) / entry
	}
	return (price - entry) / entry
}

// checkLotTakeProfit 档级止盈判定（与 CheckStaticTakeProfit 同构）：盈利达
// tpRatio 后记档级峰值（TailPeakProfitPct），自峰值回调 ≥ callback 触发；
// callback=0 达线即触发。
func (s *CRAState) checkLotTakeProfit(profit, qty, tpRatio, callback float64) (bool, float64) {
	if qty <= 0 {
		return false, 0
	}
	if profit < tpRatio {
		return false, 0
	}
	if s.TailPeakProfitPct == 0 || profit > s.TailPeakProfitPct {
		s.TailPeakProfitPct = profit
	}
	if callback > 0 {
		if s.TailPeakProfitPct-profit >= callback {
			return true, qty
		}
		return false, 0
	}
	return true, qty
}

// CheckTailTakeProfit 尾单止盈：最后一档自身盈利达止盈比例+回调后触发，
// 平仓数量=尾档剩余量（只卖最后一单）。仅剩一档时尾档即全仓。
func (s *CRAState) CheckTailTakeProfit(price, tpRatio, callback float64) (bool, float64) {
	lots := s.currentLots()
	if len(lots) == 0 {
		return false, 0
	}
	tail := lots[len(lots)-1]
	return s.checkLotTakeProfit(s.lotProfitPct(tail.Price, price), tail.Qty, tpRatio, callback)
}

// CheckHeadTailTakeProfit 首尾止盈：首档与尾档同时处于盈利达线状态（较弱端
// min(首,尾) 达止盈比例）+回调后触发，平仓数量=首档+尾档剩余量。回调峰值按
// min(首,尾) 盈利序列记。仅剩一档时首档=尾档（数量不重复计）。
func (s *CRAState) CheckHeadTailTakeProfit(price, tpRatio, callback float64) (bool, float64) {
	lots := s.currentLots()
	if len(lots) == 0 {
		return false, 0
	}
	head := lots[0]
	tail := lots[len(lots)-1]
	profit := s.lotProfitPct(head.Price, price)
	if tp := s.lotProfitPct(tail.Price, price); tp < profit {
		profit = tp
	}
	qty := head.Qty
	if len(lots) > 1 {
		qty += tail.Qty
	}
	return s.checkLotTakeProfit(profit, qty, tpRatio, callback)
}

// ApplyCloseFill 部分平仓成交确认：按出场形态从分档持仓核销 filled 数量，
// 由剩余档重算 TotalQty/TotalCost/AvgEntryPrice，PositionCount 对齐剩余档数。
// kind: "tail"=从尾档向内核销；"head_tail"=先首档后尾档；""/其它=FIFO 从首档
// （手工减仓等未知形态兜底）。档集合变更后档级盈利峰值重置。调用方在核销后
// TotalQty≈0 时按全平（ExitPosition）收尾。
func (s *CRAState) ApplyCloseFill(filled float64, kind string) {
	if filled <= 0 {
		return
	}
	lots := s.currentLots()
	if len(lots) == 0 {
		return
	}
	rest := filled
	consumeTail := func() {
		for rest > 0 && len(lots) > 0 {
			last := len(lots) - 1
			if lots[last].Qty > rest && !approxQty(lots[last].Qty, rest) {
				lots[last].Qty -= rest
				rest = 0
			} else {
				rest -= lots[last].Qty
				if rest < 0 {
					rest = 0
				}
				lots = lots[:last]
			}
		}
	}
	switch kind {
	case "tail":
		consumeTail()
	case "head_tail":
		if lots[0].Qty > rest && !approxQty(lots[0].Qty, rest) {
			lots[0].Qty -= rest
			rest = 0
		} else {
			rest -= lots[0].Qty
			if rest < 0 {
				rest = 0
			}
			lots = lots[1:]
		}
		consumeTail()
	default:
		for rest > 0 && len(lots) > 0 {
			if lots[0].Qty > rest && !approxQty(lots[0].Qty, rest) {
				lots[0].Qty -= rest
				rest = 0
			} else {
				rest -= lots[0].Qty
				if rest < 0 {
					rest = 0
				}
				lots = lots[1:]
			}
		}
	}
	s.Lots = lots
	s.PositionCount = len(lots)
	var qty, cost float64
	for _, l := range lots {
		qty += l.Qty
		cost += l.Price * l.Qty
	}
	s.TotalQty = qty
	s.TotalCost = cost
	if qty > 0 {
		s.AvgEntryPrice = cost / qty
	}
	s.TailPeakProfitPct = 0
}

// ── 重启分档重建（按成交账本逐笔重放）──

// FillRecord 一笔已成交订单的重建视图（重启分档重建输入，按成交时间升序）。
type FillRecord struct {
	Side  string  // BUY | SELL（订单方向）
	Qty   float64 // 成交量
	Price float64 // 成交均价
}

// RebuildLotsFromFills 按时间重放成交明细，重建当前未平仓循环的分档持仓与
// 方向。规则与运行时记账镜像：仓位为空时第一笔成交定方向（BUY=多/SELL=空，
// 支持历史多循环与 dual 换向）；同向成交追加一档；反向成交按数量匹配运行时
// 出场形态核销——≈剩余总量=全平清空、≈尾档=尾单止盈平尾档、≈首+尾档=
// 首尾止盈平首尾、其它=FIFO 从首档消耗（手工/未知减仓兜底）。
func RebuildLotsFromFills(fills []FillRecord) ([]EntryLot, PositionSide) {
	var lots []EntryLot
	side := PositionSide("")
	for _, f := range fills {
		if f.Qty <= 0 || f.Price <= 0 {
			continue
		}
		isBuy := !strings.EqualFold(f.Side, "SELL")
		if len(lots) == 0 {
			if isBuy {
				side = SideLong
			} else {
				side = SideShort
			}
			lots = []EntryLot{{Price: f.Price, Qty: f.Qty}}
			continue
		}
		isEntry := (side == SideLong && isBuy) || (side == SideShort && !isBuy)
		if isEntry {
			lots = append(lots, EntryLot{Price: f.Price, Qty: f.Qty})
			continue
		}
		lots = consumeLotsReplay(lots, f.Qty)
	}
	return lots, side
}

// consumeLotsReplay 重放期的平仓核销：按数量匹配运行时出场形态（全平/尾单/
// 首尾），对不上则 FIFO 从首档消耗。
func consumeLotsReplay(lots []EntryLot, qty float64) []EntryLot {
	total := 0.0
	for _, l := range lots {
		total += l.Qty
	}
	if approxQty(qty, total) {
		return nil
	}
	if n := len(lots); n >= 1 && approxQty(qty, lots[n-1].Qty) {
		return lots[:n-1]
	}
	if n := len(lots); n >= 2 && approxQty(qty, lots[0].Qty+lots[n-1].Qty) {
		return lots[1 : n-1]
	}
	rest := qty
	out := make([]EntryLot, 0, len(lots))
	for _, l := range lots {
		if rest <= 0 {
			out = append(out, l)
			continue
		}
		if l.Qty > rest {
			out = append(out, EntryLot{Price: l.Price, Qty: l.Qty - rest})
			rest = 0
		} else {
			rest -= l.Qty
		}
	}
	return out
}

// approxQty 数量近似相等（相对 1e-6，吸收 RoundQty 舍入与浮点误差）。
func approxQty(a, b float64) bool {
	return math.Abs(a-b) <= 1e-6*math.Max(math.Max(a, b), 1e-9)
}

// CheckMovingTakeProfit evaluates the 4-tier moving take-profit ladder.
func (s *CRAState) CheckMovingTakeProfit(price float64, tiers []*MovingTPTier) bool {
	if s.AvgEntryPrice <= 0 || len(tiers) == 0 {
		return false
	}
	profit := s.ProfitPct(price)
	if profit <= 0 {
		return false
	}
	if s.HighestProfitPct == 0 || profit > s.HighestProfitPct {
		s.HighestProfitPct = profit
	}
	activeDrawback := 0.0
	for _, t := range tiers {
		if t == nil {
			continue
		}
		if s.HighestProfitPct >= t.Ratio {
			activeDrawback = t.Drawback
		}
	}
	if activeDrawback <= 0 {
		return false
	}
	return (s.HighestProfitPct - profit) >= activeDrawback
}

// CheckStopLoss evaluates stop-loss for ratio/amount/price types.
func (s *CRAState) CheckStopLoss(price float64, p *CRAParams) bool {
	if !p.StopLossEnabled {
		return false
	}
	switch p.StopLossType {
	case "ratio":
		if p.StopLossRatio <= 0 {
			return false
		}
		loss := -s.ProfitPct(price)
		return loss >= p.StopLossRatio
	case "amount":
		if p.StopLossAmount <= 0 {
			return false
		}
		// approximate unrealized PnL in notional terms
		return -s.ProfitPct(price)*s.TotalCost >= p.StopLossAmount
	case "price":
		if p.StopLossPrice <= 0 {
			return false
		}
		if s.Side == SideShort {
			return price >= p.StopLossPrice
		}
		return price <= p.StopLossPrice
	}
	return false
}

// CanStartNewLoop checks whether the strategy is allowed to open a new loop.
func (s *CRAState) CanStartNewLoop(mode string, maxLoops int) bool {
	if mode == "single" {
		return s.LoopExecuted < 1
	}
	return s.LoopExecuted < maxLoops
}

// EnterPosition initializes state when the first order signal is emitted.
func (s *CRAState) EnterPosition(price float64, side PositionSide) {
	s.ResetForNextLoop()
	s.InPosition = true
	s.EntryPrice = price
	s.AvgEntryPrice = price
	s.Side = side
	s.HighestPrice = price
	s.LowestPrice = price
}

// ExitPosition finalizes state on close signal.
func (s *CRAState) ExitPosition() {
	s.LoopExecuted++
	s.ResetForNextLoop()
}

// SignalDirection returns the model signal direction for the configured side.
func (s *CRAState) SignalDirection() string {
	if s.Side == SideShort {
		return "SHORT"
	}
	return "LONG"
}
