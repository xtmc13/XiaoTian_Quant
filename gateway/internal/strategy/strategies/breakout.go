package strategies

import (
	"math"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// BreakoutStrategy enters when price breaks above/below N-period high/low.
type BreakoutStrategy struct {
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	lookback      int     // periods for high/low range
	bufferPct     float64 // break buffer as % of range
	stopLossPct   float64
	takeProfitPct float64
	positionSize  float64

	bars       []model.Bar
	inPosition bool
	entryPrice float64
	direction  string
}

func NewBreakoutStrategy() *BreakoutStrategy {
	return &BreakoutStrategy{
		name:          "breakout",
		symbol:        "BTCUSDT",
		lookback:      20,
		bufferPct:     0.002,
		stopLossPct:   0.02,
		takeProfitPct: 0.04,
		positionSize:  500,
	}
}

func (s *BreakoutStrategy) Name() string   { return s.name }
func (s *BreakoutStrategy) Symbol() string { return s.symbol }

func (s *BreakoutStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":          s.symbol,
		"lookback":        s.lookback,
		"buffer_pct":      s.bufferPct,
		"stop_loss_pct":   s.stopLossPct,
		"take_profit_pct": s.takeProfitPct,
		"position_size":   s.positionSize,
	}
}

func (s *BreakoutStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *BreakoutStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if v, ok := params["lookback"].(float64); ok {
		s.lookback = int(v)
	}
	if v, ok := params["buffer_pct"].(float64); ok {
		s.bufferPct = v
	}
	if v, ok := params["stop_loss_pct"].(float64); ok {
		s.stopLossPct = v
	}
	if v, ok := params["take_profit_pct"].(float64); ok {
		s.takeProfitPct = v
	}
	if v, ok := params["position_size"].(float64); ok {
		s.positionSize = v
	}
	if s.lookback <= 0 {
		s.lookback = 20
	}
	s.running = true
	return nil
}

func (s *BreakoutStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *BreakoutStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *BreakoutStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *BreakoutStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.lookback+2 {
		return nil, nil
	}

	// Check for exit conditions
	if s.inPosition {
		return s.checkExit(bar), nil
	}

	// Calculate lookback high/low
	highest, lowest := s.rangeHighLow(len(s.bars)-2) // exclude current bar
	if highest <= 0 || lowest <= 0 {
		return nil, nil
	}

	rangeSize := highest - lowest
	buffer := rangeSize * s.bufferPct

	// Breakout up
	if bar.Close > highest+buffer {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "LONG",
			Strength:  0.75,
			Strategy:  s.name,
			Reason:    "breakout above resistance",
		}, nil
	}

	// Breakout down
	if bar.Close < lowest-buffer {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "SHORT",
			Strength:  0.75,
			Strategy:  s.name,
			Reason:    "breakdown below support",
		}, nil
	}

	return nil, nil
}

func (s *BreakoutStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *BreakoutStrategy) checkExit(bar model.Bar) *model.Signal {
	if s.direction == "LONG" {
		// Stop loss
		if bar.Close <= s.entryPrice*(1-s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "CLOSE",
				Strength:  1.0,
				Strategy:  s.name,
				Reason:    "long stop loss",
			}
		}
		// Take profit
		if bar.Close >= s.entryPrice*(1+s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "CLOSE",
				Strength:  1.0,
				Strategy:  s.name,
				Reason:    "long take profit",
			}
		}
	} else if s.direction == "SHORT" {
		if bar.Close >= s.entryPrice*(1+s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "CLOSE",
				Strength:  1.0,
				Strategy:  s.name,
				Reason:    "short stop loss",
			}
		}
		if bar.Close <= s.entryPrice*(1-s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "CLOSE",
				Strength:  1.0,
				Strategy:  s.name,
				Reason:    "short take profit",
			}
		}
	}
	return nil
}

func (s *BreakoutStrategy) rangeHighLow(limit int) (float64, float64) {
	if limit > len(s.bars) {
		limit = len(s.bars)
	}
	start := limit - s.lookback
	if start < 0 {
		start = 0
	}
	highest := s.bars[start].High
	lowest := s.bars[start].Low
	for i := start + 1; i < limit; i++ {
		if s.bars[i].High > highest {
			highest = s.bars[i].High
		}
		if s.bars[i].Low < lowest {
			lowest = s.bars[i].Low
		}
	}
	return highest, lowest
}

// ── Arbitrage Strategy ──

// ArbitrageStrategy monitors price differences between exchanges.
type ArbitrageStrategy struct {
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	minSpreadPct float64
	orderSize    float64
	exchangeA    string
	exchangeB    string

	priceA  float64
	priceB  float64
	lastCheck int64
}

func NewArbitrageStrategy() *ArbitrageStrategy {
	return &ArbitrageStrategy{
		name:         "arbitrage",
		symbol:       "BTCUSDT",
		minSpreadPct: 0.5,
		orderSize:    500,
		exchangeA:    "binance",
		exchangeB:    "okx",
	}
}

func (s *ArbitrageStrategy) Name() string   { return s.name }
func (s *ArbitrageStrategy) Symbol() string { return s.symbol }

func (s *ArbitrageStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":         s.symbol,
		"min_spread_pct": s.minSpreadPct,
		"order_size":     s.orderSize,
		"exchange_a":     s.exchangeA,
		"exchange_b":     s.exchangeB,
	}
}

func (s *ArbitrageStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *ArbitrageStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if v, ok := params["min_spread_pct"].(float64); ok {
		s.minSpreadPct = v
	}
	if v, ok := params["order_size"].(float64); ok {
		s.orderSize = v
	}
	if v, ok := params["exchange_a"].(string); ok {
		s.exchangeA = v
	}
	if v, ok := params["exchange_b"].(string); ok {
		s.exchangeB = v
	}
	s.running = true
	return nil
}

func (s *ArbitrageStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	return nil
}

func (s *ArbitrageStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil, nil
	}

	s.lastCheck++

	// In practice, ticks would carry exchange info via the event symbol or metadata
	if tick.Last > 0 {
		s.priceA = tick.Bid
		s.priceB = tick.Ask
	}

	if s.priceA <= 0 || s.priceB <= 0 {
		return nil, nil
	}

	spread := math.Abs(s.priceA-s.priceB) / math.Min(s.priceA, s.priceB) * 100

	if spread >= s.minSpreadPct {
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "LONG",
			Strength:  math.Min(spread/s.minSpreadPct, 1.0),
			Strategy:  s.name,
			Reason:    "arbitrage spread detected",
		}, nil
	}

	return nil, nil
}

func (s *ArbitrageStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *ArbitrageStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *ArbitrageStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
