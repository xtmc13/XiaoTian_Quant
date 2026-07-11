package cra

// PositionSide describes the current holding direction.
type PositionSide string

const (
	SideLong  PositionSide = "long"
	SideShort PositionSide = "short"
)

// CRAState holds the runtime position/loop state for a CRA strategy instance.
type CRAState struct {
	InPosition        bool
	PositionCount     int       // number of filled entry orders (0 = first not filled yet)
	PendingAddCount   int       // entry signals sent but not yet filled
	LoopExecuted      int
	Side              PositionSide
	EntryPrice        float64   // first order trigger/reference price
	AvgEntryPrice     float64   // weighted average filled price
	TotalQty          float64   // total filled quantity (notional for contract)
	TotalCost         float64   // total amount spent (used for spot qty averaging)
	HighestPrice      float64   // highest price since entry
	LowestPrice       float64   // lowest price since entry
	PendingAdd        bool
	TriggerLowPrice   float64
	TriggerHighPrice  float64
	WaterfallPaused   bool
	HighestProfitPct  float64   // tracked for moving take-profit
}

// Reset clears all runtime state.
func (s *CRAState) Reset() {
	* s = CRAState{}
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
func (s *CRAState) RecordFill(price, qty float64, side SideBuySell) {
	if side == SideSell || side == SideClose {
		return
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
