package strategies

import (
	"fmt"
	"math"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// GridTradingStrategy places buy/sell orders at regular price intervals.
// It maintains a grid of price levels and rebalances when price crosses a level.
type GridTradingStrategy struct {
	strategy.BaseStrategy
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

	// Parameter registry
	params *strategy.ParamRegistry
}

func NewGridTradingStrategy() *GridTradingStrategy {
	s := &GridTradingStrategy{
		name:         "grid_trading",
		symbol:       "BTCUSDT",
		upperPrice:   0,
		lowerPrice:   0,
		gridCount:    10,
		orderAmount:  100,
		filledLevels: make(map[int]bool),
	}
	// Register parameters for hyperopt and frontend configuration
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.FloatParameter("upper_price", 0, 1000, 100000, 1000, "buy"))
	s.params.Register(strategy.FloatParameter("lower_price", 0, 1000, 100000, 1000, "buy"))
	s.params.Register(strategy.IntParameter("grid_count", 10, 5, 100, "buy"))
	s.params.Register(strategy.FloatParameter("order_amount", 100, 10, 10000, 10, "buy"))
	return s
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

	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("grid strategy apply params: %w", err)
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

// ── Parameter system implementation ────────────────────────────

func (s *GridTradingStrategy) GetParameters() *strategy.ParamRegistry {
	return s.params
}

func (s *GridTradingStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *GridTradingStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if p := s.params.Get("upper_price"); p != nil {
		s.upperPrice = p.GetFloat()
	}
	if p := s.params.Get("lower_price"); p != nil {
		s.lowerPrice = p.GetFloat()
	}
	if p := s.params.Get("grid_count"); p != nil {
		s.gridCount = p.GetInt()
	}
	if p := s.params.Get("order_amount"); p != nil {
		s.orderAmount = p.GetFloat()
	}
	return nil
}

func (s *GridTradingStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
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
	strategy.BaseStrategy
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

	// Parameter registry
	params *strategy.ParamRegistry
}

func NewMarketMakingStrategy() *MarketMakingStrategy {
	s := &MarketMakingStrategy{
		name:           "market_making",
		symbol:         "BTCUSDT",
		spreadBps:      5.0,
		orderSize:      0.01,
		maxPosition:    0.1,
		cancelInterval: 5,
	}
	// Register parameters for hyperopt and frontend configuration
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.FloatParameter("spread_bps", 5.0, 1.0, 50.0, 1.0, "buy"))
	s.params.Register(strategy.FloatParameter("order_size", 0.01, 0.001, 1.0, 0.001, "buy"))
	s.params.Register(strategy.FloatParameter("max_position", 0.1, 0.01, 1.0, 0.01, "buy"))
	s.params.Register(strategy.IntParameter("cancel_interval", 5, 1, 60, "buy"))
	return s
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

	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("market making strategy apply params: %w", err)
	}

	s.running = true
	return nil
}

// ── Parameter system implementation ────────────────────────────

func (s *MarketMakingStrategy) GetParameters() *strategy.ParamRegistry {
	return s.params
}

func (s *MarketMakingStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *MarketMakingStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if p := s.params.Get("spread_bps"); p != nil {
		s.spreadBps = p.GetFloat()
	}
	if p := s.params.Get("order_size"); p != nil {
		s.orderSize = p.GetFloat()
	}
	if p := s.params.Get("max_position"); p != nil {
		s.maxPosition = p.GetFloat()
	}
	if p := s.params.Get("cancel_interval"); p != nil {
		s.cancelInterval = int64(p.GetInt())
	}
	return nil
}

func (s *MarketMakingStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
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
