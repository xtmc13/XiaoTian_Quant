package strategies

import (
	"fmt"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── TrendShortStrategy ──────────────────────────────────────────
// Follows the trend on the short side only.
// Enters SHORT when fast EMA crosses below slow EMA (death cross).
// Exits when fast EMA crosses above slow EMA (golden cross).

type TrendShortStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	fastPeriod int
	slowPeriod int

	bars       []model.Bar
	inPosition bool

	params *strategy.ParamRegistry
}

// NewTrendShortStrategy creates a default trend-short strategy instance.
func NewTrendShortStrategy() *TrendShortStrategy {
	s := &TrendShortStrategy{
		name:       "trend_short",
		symbol:     "BTCUSDT",
		fastPeriod: 12,
		slowPeriod: 26,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("fast_period", 12, 5, 50, "buy"))
	s.params.Register(strategy.IntParameter("slow_period", 26, 10, 100, "buy"))
	return s
}

func (s *TrendShortStrategy) Name() string { return s.name }

func (s *TrendShortStrategy) Symbol() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.symbol
}

func (s *TrendShortStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"symbol":      s.symbol,
		"fast_period": s.fastPeriod,
		"slow_period": s.slowPeriod,
	}
}

func (s *TrendShortStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *TrendShortStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.applyParamsLocked(params); err != nil {
		return fmt.Errorf("trend_short apply params: %w", err)
	}
	if sym, ok := params["symbol"].(string); ok && sym != "" {
		s.symbol = sym
	}
	if s.fastPeriod >= s.slowPeriod {
		return fmt.Errorf("fast_period (%d) must be less than slow_period (%d)", s.fastPeriod, s.slowPeriod)
	}
	s.running = true
	return nil
}

func (s *TrendShortStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *TrendShortStrategy) GetParameters() *strategy.ParamRegistry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.params
}

func (s *TrendShortStrategy) ValidateParams() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *TrendShortStrategy) ApplyParams(m map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyParamsLocked(m)
}

func (s *TrendShortStrategy) ParamDefs() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

// applyParamsLocked updates parameters. Caller must hold s.mu.
func (s *TrendShortStrategy) applyParamsLocked(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if p := s.params.Get("fast_period"); p != nil {
		s.fastPeriod = p.GetInt()
	}
	if p := s.params.Get("slow_period"); p != nil {
		s.slowPeriod = p.GetInt()
	}
	return nil
}

// OnTick delegates to bar-based logic.
func (s *TrendShortStrategy) OnTick(_ model.Tick, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnOrderBook is not used for this indicator strategy.
func (s *TrendShortStrategy) OnOrderBook(_ model.OrderBookData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnOrderUpdate is not used for this indicator strategy.
func (s *TrendShortStrategy) OnOrderUpdate(_ model.OrderData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnBar receives a new bar, updates internal history, and emits signals.
func (s *TrendShortStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}

	s.bars = append(s.bars, bar)
	if len(s.bars) > maxBarsHistory {
		s.bars = s.bars[len(s.bars)-maxBarsHistory:]
	}
	if len(s.bars) < s.slowPeriod+1 {
		return nil, nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars {
		closes[i] = b.Close
	}

	fastEMA := ema(closes, s.fastPeriod)
	slowEMA := ema(closes, s.slowPeriod)
	prevFast := ema(closes[:len(closes)-1], s.fastPeriod)
	prevSlow := ema(closes[:len(closes)-1], s.slowPeriod)

	deathCross := prevFast >= prevSlow && fastEMA < slowEMA
	goldenCross := prevFast <= prevSlow && fastEMA > slowEMA

	if deathCross && !s.inPosition {
		s.inPosition = true
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "SHORT",
			Strength:  0.7,
			Strategy:  s.name,
			Reason:    "ema death cross short",
			Timestamp: bar.Time,
		}, nil
	}
	if goldenCross && s.inPosition {
		s.inPosition = false
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  0.7,
			Strategy:  s.name,
			Reason:    "ema golden cross close",
			Timestamp: bar.Time,
		}, nil
	}
	return nil, nil
}
