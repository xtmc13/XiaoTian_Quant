package risk

import (
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

type FundingFeeTracker struct {
	mu             sync.RWMutex
	positions      map[string]*FundingPosition
	totalFees      float64
	lastUpdate     time.Time
	settleInterval time.Duration
}

type FundingPosition struct {
	Symbol       string
	Side         string
	Quantity     float64
	EntryPrice   float64
	CurrentPrice float64
	Notional     float64
	FeesPaid     float64
	FeeHistory   []FundingEvent
}

type FundingEvent struct {
	Timestamp   int64   `json:"timestamp"`
	FundingRate float64 `json:"funding_rate"`
	Payment     float64 `json:"payment"`
	Position    float64 `json:"position"`
}

func NewFundingFeeTracker() *FundingFeeTracker {
	return &FundingFeeTracker{
		positions:      make(map[string]*FundingPosition),
		settleInterval: 8 * time.Hour,
	}
}

func (f *FundingFeeTracker) TrackPosition(symbol, side string, quantity, entryPrice float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.positions[symbol] = &FundingPosition{Symbol: symbol, Side: side, Quantity: quantity, EntryPrice: entryPrice, FeeHistory: make([]FundingEvent, 0)}
}

func (f *FundingFeeTracker) UntrackPosition(symbol string) {
	f.mu.Lock(); defer f.mu.Unlock()
	delete(f.positions, symbol)
}

func (f *FundingFeeTracker) SettleFunding(symbol string, currentPrice, fundingRate float64) {
	f.mu.Lock(); defer f.mu.Unlock()
	pos, ok := f.positions[symbol]
	if !ok { return }
	pos.CurrentPrice = currentPrice
	pos.Notional = math.Abs(pos.Quantity) * currentPrice
	var payment float64
	if pos.Side == "LONG" { payment = -pos.Notional * fundingRate } else { payment = pos.Notional * fundingRate }
	pos.FeesPaid += payment
	f.totalFees += payment
	pos.FeeHistory = append(pos.FeeHistory, FundingEvent{Timestamp: time.Now().Unix(), FundingRate: fundingRate, Payment: payment, Position: pos.Notional})
	if len(pos.FeeHistory) > 100 { pos.FeeHistory = pos.FeeHistory[1:] }
	f.lastUpdate = time.Now()
	if math.Abs(payment) > 0.01 {
		direction := "paid"; if payment > 0 { direction = "received" }
		log.Printf("[funding] %s %s: %s %.4f at rate %.4f%%", symbol, pos.Side, direction, math.Abs(payment), fundingRate*100)
	}
}

func (f *FundingFeeTracker) GetFees(symbol string) float64 {
	f.mu.RLock(); defer f.mu.RUnlock()
	if pos, ok := f.positions[symbol]; ok { return pos.FeesPaid }
	return 0
}

func (f *FundingFeeTracker) TotalFees() float64 { f.mu.RLock(); defer f.mu.RUnlock(); return f.totalFees }

func (f *FundingFeeTracker) Summary() map[string]any {
	f.mu.RLock(); defer f.mu.RUnlock()
	positions := make([]map[string]any, 0)
	for _, p := range f.positions {
		positions = append(positions, map[string]any{"symbol": p.Symbol, "side": p.Side, "notional": p.Notional, "fees_paid": p.FeesPaid})
	}
	return map[string]any{"positions": positions, "total_fees": f.totalFees}
}

func (f *FundingFeeTracker) EstimateFunding(symbol string, annualizedRate float64, holdingDays int) float64 {
	f.mu.RLock(); defer f.mu.RUnlock()
	pos, ok := f.positions[symbol]
	if !ok { return 0 }
	return pos.Notional * annualizedRate / 365 * float64(holdingDays)
}

func IsSettlementTime() bool { return time.Now().UTC().Hour()%8 == 0 }
func FormatFundingRate(rate float64) string { return fmt.Sprintf("%.4f%%", rate*100) }
