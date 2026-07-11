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

	bars       []model.Bar
	inPosition bool

	params *strategy.ParamRegistry
}

// NewTrendShortStrategy creates a default trend-short strategy instance.
func NewTrendShortStrategy() *TrendShortStrategy {
	s := &TrendShortStrategy{
		name:   "trend_short",
		symbol: "BTCUSDT",
	}
	s.params = strategy.NewParamRegistry()
	// Dynamic parameters are intentionally not exposed for trend_short.
	// Entry/add-position indicators are configured via CRA params instead.
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
		"symbol": s.symbol,
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
	// Unknown keys are ignored so CRA-style configs can safely include
	// frontend fields that this strategy does not declare.
	return s.params.FromMap(m)
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
	if len(s.bars) < trendSlowPeriod+1 {
		return nil, nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars {
		closes[i] = b.Close
	}

	fastEMA := ema(closes, trendFastPeriod)
	slowEMA := ema(closes, trendSlowPeriod)
	prevFast := ema(closes[:len(closes)-1], trendFastPeriod)
	prevSlow := ema(closes[:len(closes)-1], trendSlowPeriod)

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
