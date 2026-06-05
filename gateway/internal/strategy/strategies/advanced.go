package strategies

import (
	"fmt"
	"math"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── ATRTrailingStopStrategy ────────────────────────────────────
// Uses ATR-based trailing stop for dynamic exit management.
// Enters on trend confirmation, exits when price reverses by N*ATR.

type ATRTrailingStopStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	atrPeriod      int
	atrMultiplier  float64
	entryPeriod    int     // EMA period for trend direction
	positionSize   float64

	bars       []model.Bar
	inPosition  bool
	entryPrice  float64
	trailingStop float64
	highestPrice float64
	lowestPrice  float64
	direction   string

	params *strategy.ParamRegistry
}

func NewATRTrailingStopStrategy() *ATRTrailingStopStrategy {
	s := &ATRTrailingStopStrategy{
		name:          "atr_trailing_stop",
		symbol:        "BTCUSDT",
		atrPeriod:     14,
		atrMultiplier: 2.0,
		entryPeriod:   20,
		positionSize:  500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("atr_period", 14, 5, 50, "buy"))
	s.params.Register(strategy.FloatParameter("atr_multiplier", 2.0, 1.0, 5.0, 0.1, "stoploss"))
	s.params.Register(strategy.IntParameter("entry_period", 20, 10, 100, "buy"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *ATRTrailingStopStrategy) Name() string   { return s.name }
func (s *ATRTrailingStopStrategy) Symbol() string { return s.symbol }

func (s *ATRTrailingStopStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":         s.symbol,
		"atr_period":     s.atrPeriod,
		"atr_multiplier": s.atrMultiplier,
		"entry_period":   s.entryPeriod,
		"position_size":  s.positionSize,
	}
}

func (s *ATRTrailingStopStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *ATRTrailingStopStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("atr_trailing_stop apply params: %w", err)
	}
	s.running = true
	return nil
}

func (s *ATRTrailingStopStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *ATRTrailingStopStrategy) ValidateParams() error {
	if s.params == nil { return nil }
	return s.params.Validate()
}
func (s *ATRTrailingStopStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil { return nil }
	if err := s.params.FromMap(m); err != nil { return err }
	if p := s.params.Get("atr_period"); p != nil { s.atrPeriod = p.GetInt() }
	if p := s.params.Get("atr_multiplier"); p != nil { s.atrMultiplier = p.GetFloat() }
	if p := s.params.Get("entry_period"); p != nil { s.entryPeriod = p.GetInt() }
	if p := s.params.Get("position_size"); p != nil { s.positionSize = p.GetFloat() }
	return nil
}
func (s *ATRTrailingStopStrategy) ParamDefs() []map[string]any {
	if s.params == nil { return nil }
	return s.params.ToJSONDefs()
}

func (s *ATRTrailingStopStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *ATRTrailingStopStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *ATRTrailingStopStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *ATRTrailingStopStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }

func (s *ATRTrailingStopStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.entryPeriod+s.atrPeriod+5 {
		return nil, nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars { closes[i] = b.Close }

	emaVal := ema(closes, s.entryPeriod)
	atrVal := atr(s.bars, s.atrPeriod)

	if s.inPosition {
		return s.checkExit(bar, atrVal), nil
	}

	// Entry: price above EMA for long, below for short
	if bar.Close > emaVal && closes[len(closes)-2] <= ema(closes[:len(closes)-1], s.entryPeriod) {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		s.highestPrice = bar.Close
		s.trailingStop = bar.Close - s.atrMultiplier*atrVal
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.7, Strategy: s.name, Reason: "price crossed above EMA with ATR trailing stop"}, nil
	}
	if bar.Close < emaVal && closes[len(closes)-2] >= ema(closes[:len(closes)-1], s.entryPeriod) {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		s.lowestPrice = bar.Close
		s.trailingStop = bar.Close + s.atrMultiplier*atrVal
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.7, Strategy: s.name, Reason: "price crossed below EMA with ATR trailing stop"}, nil
	}
	return nil, nil
}

func (s *ATRTrailingStopStrategy) checkExit(bar model.Bar, atr float64) *model.Signal {
	if s.direction == "LONG" {
		if bar.Close > s.highestPrice {
			s.highestPrice = bar.Close
			s.trailingStop = s.highestPrice - s.atrMultiplier*atr
		}
		if bar.Close <= s.trailingStop {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "ATR trailing stop hit (long)"}
		}
	} else {
		if bar.Close < s.lowestPrice {
			s.lowestPrice = bar.Close
			s.trailingStop = s.lowestPrice + s.atrMultiplier*atr
		}
		if bar.Close >= s.trailingStop {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "ATR trailing stop hit (short)"}
		}
	}
	return nil
}

// ── DualThrustStrategy ─────────────────────────────────────────
// A channel breakout strategy that uses the range of the previous N bars
// to calculate upper and lower thresholds.

type DualThrustStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	lookbackPeriod int
	k1             float64 // upper multiplier
	k2             float64 // lower multiplier
	positionSize   float64

	bars       []model.Bar
	inPosition  bool
	entryPrice  float64
	direction   string

	params *strategy.ParamRegistry
}

func NewDualThrustStrategy() *DualThrustStrategy {
	s := &DualThrustStrategy{
		name:           "dual_thrust",
		symbol:         "BTCUSDT",
		lookbackPeriod: 4,
		k1:             0.5,
		k2:             0.5,
		positionSize:   500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("lookback_period", 4, 1, 20, "buy"))
	s.params.Register(strategy.FloatParameter("k1", 0.5, 0.1, 1.0, 0.05, "buy"))
	s.params.Register(strategy.FloatParameter("k2", 0.5, 0.1, 1.0, 0.05, "buy"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *DualThrustStrategy) Name() string   { return s.name }
func (s *DualThrustStrategy) Symbol() string { return s.symbol }

func (s *DualThrustStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":          s.symbol,
		"lookback_period": s.lookbackPeriod,
		"k1":              s.k1,
		"k2":              s.k2,
		"position_size":   s.positionSize,
	}
}

func (s *DualThrustStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *DualThrustStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("dual_thrust apply params: %w", err)
	}
	s.running = true
	return nil
}

func (s *DualThrustStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *DualThrustStrategy) ValidateParams() error {
	if s.params == nil { return nil }
	return s.params.Validate()
}
func (s *DualThrustStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil { return nil }
	if err := s.params.FromMap(m); err != nil { return err }
	if p := s.params.Get("lookback_period"); p != nil { s.lookbackPeriod = p.GetInt() }
	if p := s.params.Get("k1"); p != nil { s.k1 = p.GetFloat() }
	if p := s.params.Get("k2"); p != nil { s.k2 = p.GetFloat() }
	if p := s.params.Get("position_size"); p != nil { s.positionSize = p.GetFloat() }
	return nil
}
func (s *DualThrustStrategy) ParamDefs() []map[string]any {
	if s.params == nil { return nil }
	return s.params.ToJSONDefs()
}

func (s *DualThrustStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *DualThrustStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *DualThrustStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *DualThrustStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }

func (s *DualThrustStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.lookbackPeriod+2 {
		return nil, nil
	}

	if s.inPosition {
		return s.checkExit(bar), nil
	}

	// Calculate Dual Thrust range from previous N bars
	upper, lower := s.dualThrustRange()
	if upper <= 0 || lower <= 0 {
		return nil, nil
	}

	prevClose := s.bars[len(s.bars)-2].Close

	if bar.Close > upper && prevClose <= upper {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.75, Strategy: s.name, Reason: "Dual Thrust upper breakout"}, nil
	}
	if bar.Close < lower && prevClose >= lower {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.75, Strategy: s.name, Reason: "Dual Thrust lower breakout"}, nil
	}
	return nil, nil
}

func (s *DualThrustStrategy) dualThrustRange() (upper, lower float64) {
	if len(s.bars) < s.lookbackPeriod+1 {
		return 0, 0
	}

	// Use previous N bars (excluding current)
	window := s.bars[len(s.bars)-1-s.lookbackPeriod : len(s.bars)-1]

	hh := window[0].High
	ll := window[0].Low
	maxClose := window[0].Close
	minClose := window[0].Close

	for _, b := range window[1:] {
		if b.High > hh { hh = b.High }
		if b.Low < ll { ll = b.Low }
		if b.Close > maxClose { maxClose = b.Close }
		if b.Close < minClose { minClose = b.Close }
	}

	// Range = max(HH-LC, HC-LL)
	range1 := hh - minClose
	range2 := maxClose - ll
	tr := range1
	if range2 > tr { tr = range2 }

	prevClose := s.bars[len(s.bars)-2].Close
	upper = prevClose + s.k1*tr
	lower = prevClose - s.k2*tr
	return upper, lower
}

func (s *DualThrustStrategy) checkExit(bar model.Bar) *model.Signal {
	// Simple time-based or opposite signal exit
	upper, lower := s.dualThrustRange()
	if s.direction == "LONG" {
		if bar.Close < lower {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "Dual Thrust lower threshold hit (long exit)"}
		}
	} else {
		if bar.Close > upper {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "Dual Thrust upper threshold hit (short exit)"}
		}
	}
	return nil
}

// ── RenkoStrategy ──────────────────────────────────────────────
// Uses Renko brick-based trend detection for cleaner signal generation.

type RenkoStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	brickSize    float64 // fixed brick size in price units
	reversalBricks int   // bricks needed for reversal
	positionSize float64

	bars       []model.Bar
	renkoBricks []RenkoBrick
	inPosition  bool
	direction   string

	params *strategy.ParamRegistry
}

type RenkoBrick struct {
	Open  float64
	Close float64
	Up    bool
}

func NewRenkoStrategy() *RenkoStrategy {
	s := &RenkoStrategy{
		name:           "renko",
		symbol:         "BTCUSDT",
		brickSize:      100, // default 100 USDT per brick
		reversalBricks: 2,
		positionSize:   500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.FloatParameter("brick_size", 100, 10, 1000, 10, "buy"))
	s.params.Register(strategy.IntParameter("reversal_bricks", 2, 1, 5, "buy"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *RenkoStrategy) Name() string   { return s.name }
func (s *RenkoStrategy) Symbol() string { return s.symbol }

func (s *RenkoStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":           s.symbol,
		"brick_size":       s.brickSize,
		"reversal_bricks":  s.reversalBricks,
		"position_size":    s.positionSize,
	}
}

func (s *RenkoStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *RenkoStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("renko apply params: %w", err)
	}
	s.running = true
	return nil
}

func (s *RenkoStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *RenkoStrategy) ValidateParams() error {
	if s.params == nil { return nil }
	return s.params.Validate()
}
func (s *RenkoStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil { return nil }
	if err := s.params.FromMap(m); err != nil { return err }
	if p := s.params.Get("brick_size"); p != nil { s.brickSize = p.GetFloat() }
	if p := s.params.Get("reversal_bricks"); p != nil { s.reversalBricks = p.GetInt() }
	if p := s.params.Get("position_size"); p != nil { s.positionSize = p.GetFloat() }
	return nil
}
func (s *RenkoStrategy) ParamDefs() []map[string]any {
	if s.params == nil { return nil }
	return s.params.ToJSONDefs()
}

func (s *RenkoStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *RenkoStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *RenkoStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *RenkoStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }

func (s *RenkoStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 500 {
		s.bars = s.bars[len(s.bars)-500:]
	}

	if !s.running || len(s.bars) < 10 {
		return nil, nil
	}

	// Rebuild Renko bricks from bars
	s.renkoBricks = s.buildRenko(s.bars)
	if len(s.renkoBricks) < s.reversalBricks+2 {
		return nil, nil
	}

	if s.inPosition {
		return s.checkExit(), nil
	}

	// Entry: trend change detected
	bricks := s.renkoBricks
	last := bricks[len(bricks)-1]
	prev := bricks[len(bricks)-2]

	if !last.Up && prev.Up {
		// Trend changed from up to down
		s.inPosition = true
		s.direction = "SHORT"
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.7, Strategy: s.name, Reason: "Renko trend reversal to down"}, nil
	}
	if last.Up && !prev.Up {
		// Trend changed from down to up
		s.inPosition = true
		s.direction = "LONG"
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.7, Strategy: s.name, Reason: "Renko trend reversal to up"}, nil
	}
	return nil, nil
}

func (s *RenkoStrategy) buildRenko(bars []model.Bar) []RenkoBrick {
	if len(bars) == 0 || s.brickSize <= 0 {
		return nil
	}

	bricks := make([]RenkoBrick, 0)
	base := bars[0].Close
	currentOpen := base

	for _, bar := range bars[1:] {
		for {
			if bar.Close >= currentOpen+s.brickSize {
				bricks = append(bricks, RenkoBrick{Open: currentOpen, Close: currentOpen + s.brickSize, Up: true})
				currentOpen += s.brickSize
			} else if bar.Close <= currentOpen-s.brickSize {
				bricks = append(bricks, RenkoBrick{Open: currentOpen, Close: currentOpen - s.brickSize, Up: false})
				currentOpen -= s.brickSize
			} else {
				break
			}
		}
	}

	return bricks
}

func (s *RenkoStrategy) checkExit() *model.Signal {
	if len(s.renkoBricks) < s.reversalBricks+1 {
		return nil
	}

	bricks := s.renkoBricks
	last := bricks[len(bricks)-1]

	if s.direction == "LONG" && !last.Up {
		// Check if we have enough reversal bricks
		reversalCount := 0
		for i := len(bricks) - 1; i >= 0 && !bricks[i].Up; i-- {
			reversalCount++
		}
		if reversalCount >= s.reversalBricks {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "Renko reversal (long exit)"}
		}
	}
	if s.direction == "SHORT" && last.Up {
		reversalCount := 0
		for i := len(bricks) - 1; i >= 0 && bricks[i].Up; i-- {
			reversalCount++
		}
		if reversalCount >= s.reversalBricks {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "Renko reversal (short exit)"}
		}
	}
	return nil
}

// ── Technical Helpers ──────────────────────────────────────────

func atr(bars []model.Bar, period int) float64 {
	if len(bars) < period+1 {
		return 0
	}
	trValues := make([]float64, period)
	for i := 0; i < period; i++ {
		idx := len(bars) - period + i
		if idx <= 0 {
			continue
		}
		highLow := bars[idx].High - bars[idx].Low
		highClose := math.Abs(bars[idx].High - bars[idx-1].Close)
		lowClose := math.Abs(bars[idx].Low - bars[idx-1].Close)
		trValues[i] = max(highLow, max(highClose, lowClose))
	}
	return sma(trValues, period)
}

func max(a, b float64) float64 {
	if a > b { return a }
	return b
}
