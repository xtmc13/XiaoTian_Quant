package strategies

import (
	"math"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// GridTradingStrategy places buy/sell orders at regular price intervals.
// It maintains a grid of price levels and rebalances when price crosses a level.
type GridTradingStrategy struct {
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	// Grid parameters
	upperPrice  float64
	lowerPrice  float64
	gridCount   int
	gridSize    float64
	orderAmount float64

	// Grid state — tracks which levels have active orders
	gridLevels   []float64
	filledLevels map[int]bool // level index -> has buy order filled
	lastPrice    float64
	bars         []model.Bar
}

func NewGridTradingStrategy() *GridTradingStrategy {
	return &GridTradingStrategy{
		name:         "grid_trading",
		symbol:       "BTCUSDT",
		upperPrice:   0,
		lowerPrice:   0,
		gridCount:    10,
		orderAmount:  100,
		filledLevels: make(map[int]bool),
	}
}

func (s *GridTradingStrategy) Name() string   { return s.name }
func (s *GridTradingStrategy) Symbol() string { return s.symbol }

func (s *GridTradingStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":       s.symbol,
		"upper_price":  s.upperPrice,
		"lower_price":  s.lowerPrice,
		"grid_count":   s.gridCount,
		"order_amount": s.orderAmount,
	}
}

func (s *GridTradingStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *GridTradingStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if v, ok := params["upper_price"].(float64); ok {
		s.upperPrice = v
	}
	if v, ok := params["lower_price"].(float64); ok {
		s.lowerPrice = v
	}
	if v, ok := params["grid_count"].(float64); ok {
		s.gridCount = int(v)
	}
	if v, ok := params["order_amount"].(float64); ok {
		s.orderAmount = v
	}
	if s.gridCount <= 0 {
		s.gridCount = 10
	}
	if s.orderAmount <= 0 {
		s.orderAmount = 100
	}

	s.buildGrid()
	s.running = true
	return nil
}

func (s *GridTradingStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.filledLevels = make(map[int]bool)
	return nil
}

func (s *GridTradingStrategy) buildGrid() {
	if s.upperPrice <= 0 || s.lowerPrice <= 0 || s.upperPrice <= s.lowerPrice {
		return
	}
	s.gridSize = (s.upperPrice - s.lowerPrice) / float64(s.gridCount)
	s.gridLevels = make([]float64, s.gridCount+1)
	for i := 0; i <= s.gridCount; i++ {
		s.gridLevels[i] = s.lowerPrice + float64(i)*s.gridSize
	}
}

func (s *GridTradingStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || len(s.gridLevels) == 0 {
		return nil, nil
	}

	price := tick.Last
	if price <= 0 {
		return nil, nil
	}

	s.lastPrice = price

	// Check if price crossed any grid levels
	for i, level := range s.gridLevels {
		if i == 0 || i == len(s.gridLevels)-1 {
			continue // don't trade at boundaries
		}

		// Price dropped below a grid level — buy signal
		if price < level && s.lastPrice >= level && !s.filledLevels[i] {
			s.filledLevels[i] = true
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "LONG",
				Strength:  0.7,
				Strategy:  s.name,
				Reason:    "grid buy at level",
			}, nil
		}

		// Price rose above a filled grid level — sell signal
		if price > level && s.lastPrice <= level && s.filledLevels[i] {
			delete(s.filledLevels, i)
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "CLOSE",
				Strength:  0.7,
				Strategy:  s.name,
				Reason:    "grid sell at level",
			}, nil
		}
	}

	return nil, nil
}

func (s *GridTradingStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *GridTradingStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	// Recalculate grid if range has shifted significantly
	if len(s.bars) >= 100 && math.Abs(bar.Close-s.gridLevels[len(s.gridLevels)/2]) > s.gridSize*float64(s.gridCount)/2 {
		avgPrice := s.averagePrice(100)
		rangePct := 0.05
		s.upperPrice = avgPrice * (1 + rangePct)
		s.lowerPrice = avgPrice * (1 - rangePct)
		s.buildGrid()
	}

	return nil, nil
}

func (s *GridTradingStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *GridTradingStrategy) averagePrice(period int) float64 {
	if len(s.bars) < period {
		period = len(s.bars)
	}
	if period == 0 {
		return 0
	}
	sum := 0.0
	for i := len(s.bars) - period; i < len(s.bars); i++ {
		sum += s.bars[i].Close
	}
	return sum / float64(period)
}

// ── Market Making Strategy ──

// MarketMakingStrategy places two-sided limit orders around the mid price.
type MarketMakingStrategy struct {
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	spreadBps     float64
	orderSize     float64
	maxPosition   float64
	cancelInterval int64 // bars between re-quoting

	position  float64
	lastQuote int64
	bars      []model.Bar
}

func NewMarketMakingStrategy() *MarketMakingStrategy {
	return &MarketMakingStrategy{
		name:           "market_making",
		symbol:         "BTCUSDT",
		spreadBps:      5.0,
		orderSize:      0.01,
		maxPosition:    0.1,
		cancelInterval: 5,
	}
}

func (s *MarketMakingStrategy) Name() string   { return s.name }
func (s *MarketMakingStrategy) Symbol() string { return s.symbol }

func (s *MarketMakingStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":      s.symbol,
		"spread_bps":  s.spreadBps,
		"order_size":  s.orderSize,
		"max_position": s.maxPosition,
	}
}

func (s *MarketMakingStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *MarketMakingStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if v, ok := params["spread_bps"].(float64); ok {
		s.spreadBps = v
	}
	if v, ok := params["order_size"].(float64); ok {
		s.orderSize = v
	}
	if v, ok := params["max_position"].(float64); ok {
		s.maxPosition = v
	}
	s.running = true
	return nil
}

func (s *MarketMakingStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.position = 0
	return nil
}

func (s *MarketMakingStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *MarketMakingStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil, nil
	}

	mid := ob.MidPrice()
	if mid <= 0 || math.Abs(s.position) >= s.maxPosition {
		return nil, nil
	}

	halfSpread := mid * s.spreadBps / 10000.0

	// Re-quote periodically
	s.lastQuote++
	if s.lastQuote%int64(s.cancelInterval) != 0 {
		return nil, nil
	}

	_ = halfSpread

	imbalance := ob.Imbalance(5)
	direction := "LONG"
	if imbalance < -0.1 {
		direction = "SHORT"
	} else if imbalance > 0.1 {
		direction = "LONG"
	} else {
		return nil, nil
	}

	// Avoid exceeding position limits
	if direction == "LONG" && s.position >= s.maxPosition {
		return nil, nil
	}
	if direction == "SHORT" && s.position <= -s.maxPosition {
		return nil, nil
	}

	return &model.Signal{
		Symbol:    s.symbol,
		Direction: direction,
		Strength:  0.5,
		Strategy:  s.name,
		Reason:    "market making imbalance",
	}, nil
}

func (s *MarketMakingStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bars = append(s.bars, bar)
	if len(s.bars) > 100 {
		s.bars = s.bars[len(s.bars)-100:]
	}
	return nil, nil
}

func (s *MarketMakingStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if order.Status == model.StatusFilled {
		if order.Side == model.SideBuy {
			s.position += order.Filled
		} else {
			s.position -= order.Filled
		}
	}
	return nil, nil
}
