package strategies

import (
	"fmt"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// maxBarsHistory limits the number of bars kept in memory for indicator strategies.
const maxBarsHistory = 200

// ── TrendLongStrategy ───────────────────────────────────────────
// Follows the trend on the long side only.
// Enters LONG when fast EMA crosses above slow EMA (golden cross).
// Exits when fast EMA crosses below slow EMA (death cross).

type TrendLongStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	bars       []model.Bar
	inPosition bool

	params *strategy.ParamRegistry
}

// NewTrendLongStrategy creates a default trend-long strategy instance.
func NewTrendLongStrategy() *TrendLongStrategy {
	s := &TrendLongStrategy{
		name:   "trend_long",
		symbol: "BTCUSDT",
	}
	s.params = strategy.NewParamRegistry()
	// Dynamic parameters are intentionally not exposed for trend_long.
	// Entry/add-position indicators are configured via CRA params instead.
	return s
}

func (s *TrendLongStrategy) Name() string { return s.name }

func (s *TrendLongStrategy) Symbol() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.symbol
}

func (s *TrendLongStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"symbol": s.symbol,
	}
}

func (s *TrendLongStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *TrendLongStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.applyParamsLocked(params); err != nil {
		return fmt.Errorf("trend_long apply params: %w", err)
	}
	if sym, ok := params["symbol"].(string); ok && sym != "" {
		s.symbol = sym
	}
	s.running = true
	return nil
}

func (s *TrendLongStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *TrendLongStrategy) GetParameters() *strategy.ParamRegistry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.params
}

func (s *TrendLongStrategy) ValidateParams() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *TrendLongStrategy) ApplyParams(m map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyParamsLocked(m)
}

func (s *TrendLongStrategy) ParamDefs() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

// applyParamsLocked updates parameters. Caller must hold s.mu.
func (s *TrendLongStrategy) applyParamsLocked(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	// Unknown keys are ignored so CRA-style configs can safely include
	// frontend fields that this strategy does not declare.
	return s.params.FromMap(m)
}

// OnTick delegates to bar-based logic.
func (s *TrendLongStrategy) OnTick(_ model.Tick, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnOrderBook is not used for this indicator strategy.
func (s *TrendLongStrategy) OnOrderBook(_ model.OrderBookData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnOrderUpdate is not used for this indicator strategy.
func (s *TrendLongStrategy) OnOrderUpdate(_ model.OrderData, _ *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnBar receives a new bar, updates internal history, and emits signals.
func (s *TrendLongStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
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

	goldenCross := prevFast <= prevSlow && fastEMA > slowEMA
	deathCross := prevFast >= prevSlow && fastEMA < slowEMA

	if goldenCross && !s.inPosition {
		s.inPosition = true
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "LONG",
			Strength:  0.7,
			Strategy:  s.name,
			Reason:    "ema golden cross long",
			Timestamp: bar.Time,
		}, nil
	}
	if deathCross && s.inPosition {
		s.inPosition = false
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  0.7,
			Strategy:  s.name,
			Reason:    "ema death cross close",
			Timestamp: bar.Time,
		}, nil
	}
	return nil, nil
}
