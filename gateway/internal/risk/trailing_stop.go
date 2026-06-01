package risk

import (
	"fmt"
	"math"
	"sync"
)

// TrailingStopMode defines how the trailing stop distance is calculated.
type TrailingStopMode int

const (
	// TrailingDisabled means no trailing stop is active.
	TrailingDisabled TrailingStopMode = iota
	// TrailingFixed uses a fixed percentage distance from the best price.
	TrailingFixed
	// TrailingATR uses ATR (Average True Range) multiplied by a factor.
	TrailingATR
)

func (m TrailingStopMode) String() string {
	switch m {
	case TrailingDisabled:
		return "DISABLED"
	case TrailingFixed:
		return "FIXED"
	case TrailingATR:
		return "ATR"
	default:
		return "UNKNOWN"
	}
}

// TrailingStopConfig configures a trailing stop.
type TrailingStopConfig struct {
	Mode          TrailingStopMode `json:"mode"`
	StopDistance  float64          `json:"stop_distance"`   // Fixed: percentage (e.g., 0.02 = 2%)
	ATRPeriod     int              `json:"atr_period"`      // ATR: lookback period (default 14)
	ATRMultiplier float64          `json:"atr_multiplier"`  // ATR: multiplier on ATR value
}

// DefaultTrailingStopConfig returns sensible defaults.
func DefaultTrailingStopConfig() TrailingStopConfig {
	return TrailingStopConfig{
		Mode:          TrailingFixed,
		StopDistance:  0.02,  // 2%
		ATRPeriod:     14,
		ATRMultiplier: 1.5,
	}
}

// TrailingStop tracks the best price seen and the current stop level.
// It implements a "ratchet" — the stop only moves in the favorable direction.
type TrailingStop struct {
	config TrailingStopConfig

	mu sync.Mutex

	// For LONG positions
	highestPrice float64 // best price seen since entry
	// For SHORT positions
	lowestPrice float64 // best price seen since entry

	stopPrice     float64 // current trailing stop price
	entryPrice    float64 // the entry price for reference
	direction     string  // "LONG" or "SHORT"
	initialized   bool
	enabled       bool

	// ATR tracking
	prices       []float64 // recent close prices for ATR calculation
	atrValue     float64   // current ATR value
	barsReceived int
}

// NewTrailingStop creates a trailing stop with the given config.
func NewTrailingStop(cfg TrailingStopConfig) *TrailingStop {
	if cfg.ATRPeriod <= 0 {
		cfg.ATRPeriod = 14
	}
	if cfg.ATRMultiplier <= 0 {
		cfg.ATRMultiplier = 1.5
	}
	if cfg.StopDistance <= 0 && (cfg.Mode == TrailingFixed || cfg.Mode == TrailingATR) {
		cfg.StopDistance = 0.02 // fallback when ATR not yet available
	}

	return &TrailingStop{
		config:  cfg,
		enabled: cfg.Mode != TrailingDisabled,
		prices:  make([]float64, 0, cfg.ATRPeriod+1),
	}
}

// Initialize sets the entry point. Call once when a position is opened.
func (ts *TrailingStop) Initialize(entryPrice float64, direction string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.entryPrice = entryPrice
	ts.direction = direction
	ts.initialized = true
	ts.prices = ts.prices[:0]
	ts.prices = append(ts.prices, entryPrice) // seed with entry price for ATR
	ts.barsReceived = 0
	ts.atrValue = 0

	if !ts.enabled {
		return
	}

	if direction == "LONG" {
		ts.highestPrice = entryPrice
		ts.stopPrice = ts.calcInitialStop()
	} else {
		ts.lowestPrice = entryPrice
		ts.stopPrice = ts.calcInitialStop()
	}
}

func (ts *TrailingStop) calcInitialStop() float64 {
	if ts.config.Mode == TrailingFixed || ts.config.Mode == TrailingATR && ts.atrValue <= 0 {
		if ts.direction == "LONG" {
			return ts.entryPrice * (1 - ts.config.StopDistance)
		}
		return ts.entryPrice * (1 + ts.config.StopDistance)
	}
	// ATR with known atr value
	if ts.direction == "LONG" {
		return ts.entryPrice - ts.atrValue*ts.config.ATRMultiplier
	}
	return ts.entryPrice + ts.atrValue*ts.config.ATRMultiplier
}

// Update is called on each new price (tick or bar close).
// It recalculates the trailing stop and returns the current stop price.
// Also returns true if the stop has been hit.
func (ts *TrailingStop) Update(currentPrice float64) (stopPrice float64, triggered bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !ts.enabled || !ts.initialized {
		return 0, false
	}

	// Update ATR if in ATR mode
	if ts.config.Mode == TrailingATR {
		ts.updateATR(currentPrice)
	}

	// Update best price (ratchet)
	if ts.direction == "LONG" {
		if currentPrice > ts.highestPrice {
			ts.highestPrice = currentPrice
			ts.stopPrice = ts.calcStopFromBest()
		}
		triggered = currentPrice <= ts.stopPrice
	} else {
		if currentPrice < ts.lowestPrice {
			ts.lowestPrice = currentPrice
			ts.stopPrice = ts.calcStopFromBest()
		}
		triggered = currentPrice >= ts.stopPrice
	}

	return ts.stopPrice, triggered
}

func (ts *TrailingStop) calcStopFromBest() float64 {
	switch ts.config.Mode {
	case TrailingFixed:
		return ts.calcFixedStop()
	case TrailingATR:
		if ts.atrValue > 0 {
			return ts.calcATRStop()
		}
		return ts.calcFixedStop()
	default:
		return ts.calcFixedStop()
	}
}

func (ts *TrailingStop) calcFixedStop() float64 {
	if ts.direction == "LONG" {
		return ts.highestPrice * (1 - ts.config.StopDistance)
	}
	return ts.lowestPrice * (1 + ts.config.StopDistance)
}

func (ts *TrailingStop) calcATRStop() float64 {
	distance := ts.atrValue * ts.config.ATRMultiplier
	if ts.direction == "LONG" {
		return ts.highestPrice - distance
	}
	return ts.lowestPrice + distance
}

func (ts *TrailingStop) updateATR(price float64) {
	ts.prices = append(ts.prices, price)
	if len(ts.prices) > ts.config.ATRPeriod+1 {
		ts.prices = ts.prices[len(ts.prices)-(ts.config.ATRPeriod+1):]
	}
	ts.barsReceived++

	// Need at least ATRPeriod+1 bars to calculate ATR
	if len(ts.prices) < ts.config.ATRPeriod+1 {
		return
	}

	// Calculate True Range for each bar (except first)
	var sumTR float64
	for i := 1; i < len(ts.prices); i++ {
		// Simplified TR: |close[i] - close[i-1]| as a proxy
		tr := math.Abs(ts.prices[i] - ts.prices[i-1])
		sumTR += tr
	}
	ts.atrValue = sumTR / float64(len(ts.prices)-1)
}

// StopPrice returns the current stop price, or 0 if disabled/not initialized.
func (ts *TrailingStop) StopPrice() float64 {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.enabled || !ts.initialized {
		return 0
	}
	return ts.stopPrice
}

// BestPrice returns the best price seen (highest for long, lowest for short).
func (ts *TrailingStop) BestPrice() float64 {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.direction == "LONG" {
		return ts.highestPrice
	}
	return ts.lowestPrice
}

// ATRValue returns the current ATR value (0 if not in ATR mode or insufficient data).
func (ts *TrailingStop) ATRValue() float64 {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.atrValue
}

// IsEnabled returns whether the trailing stop is active.
func (ts *TrailingStop) IsEnabled() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.enabled
}

// Enable activates the trailing stop.
func (ts *TrailingStop) Enable() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.enabled = true
}

// Disable deactivates the trailing stop without resetting state.
func (ts *TrailingStop) Disable() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.enabled = false
}

// Reset clears the trailing stop state for a new position.
func (ts *TrailingStop) Reset() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.initialized = false
	ts.highestPrice = 0
	ts.lowestPrice = 0
	ts.stopPrice = 0
	ts.entryPrice = 0
	ts.direction = ""
	ts.prices = ts.prices[:0]
	ts.atrValue = 0
	ts.barsReceived = 0
}

// Status returns a human-readable summary.
func (ts *TrailingStop) Status() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !ts.enabled {
		return "TRAILING_DISABLED"
	}
	if !ts.initialized {
		return "NOT_INITIALIZED"
	}

	return fmt.Sprintf(
		"mode=%s direction=%s entry=%.2f best=%.2f stop=%.2f atr=%.4f",
		ts.config.Mode.String(),
		ts.direction,
		ts.entryPrice,
		ts.bestPriceUnsafe(),
		ts.stopPrice,
		ts.atrValue,
	)
}

func (ts *TrailingStop) bestPriceUnsafe() float64 {
	if ts.direction == "LONG" {
		return ts.highestPrice
	}
	return ts.lowestPrice
}
