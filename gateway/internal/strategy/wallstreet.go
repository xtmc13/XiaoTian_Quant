package strategy

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/logging"
	"github.com/xiaotian-quant/gateway/internal/model"
)

var fibonacciSequence = []float64{1, 2, 3, 5, 8, 13, 21, 34, 55, 89}

// WallStreetStrategy implements a Fibonacci-scaled averaging-down strategy.
type WallStreetStrategy struct {
	BaseStrategy

	FirstOrderAmount     float64
	OrderCount           int
	AddPositionSpread    float64
	AddPositionCallback  float64
	TakeProfitRatio      float64
	ProfitCallback       float64
	DoubleFirstOrder     bool
	LoopType             string
	LoopCount            int
	EnableAddPosition    bool
	FlashCrashProtection float64

	name    string
	symbol  string
	running bool
	mu      sync.RWMutex
	logger  *logging.Logger

	inPosition        bool
	positionCount     int
	entryPrice        float64
	avgEntryPrice     float64
	totalQuantity     float64
	lowestSinceEntry  float64
	highestSinceEntry float64
	pendingAdd        bool
	triggerLowPrice   float64
	loopExecuted      int
	waterfallPaused   bool

	flashDetector *FlashCrashDetector
	params        *ParamRegistry
}

// NewWallStreetStrategy creates a new WallStreetStrategy with sensible defaults.
func NewWallStreetStrategy() *WallStreetStrategy {
	s := &WallStreetStrategy{
		name:                 "wallstreet",
		symbol:               "BTCUSDT",
		FirstOrderAmount:     100,
		OrderCount:           7,
		AddPositionSpread:    0.03,
		AddPositionCallback:  0.003,
		TakeProfitRatio:      0.013,
		ProfitCallback:       0.003,
		DoubleFirstOrder:     false,
		LoopType:             "cycle",
		LoopCount:            100,
		EnableAddPosition:    true,
		FlashCrashProtection: 0.02,
		logger:               logging.New("wallstreet_strategy"),
		flashDetector:        NewFlashCrashDetector(),
	}

	s.params = NewParamRegistry()
	s.params.Register(FloatParameter("first_order_amount", 100, 10, 10000, 10, "buy"))
	s.params.Register(IntParameter("order_count", 7, 1, 20, "buy"))
	s.params.Register(FloatParameter("add_position_spread", 0.03, 0.005, 0.50, 0.005, "buy"))
	s.params.Register(FloatParameter("add_position_callback", 0.003, 0.0001, 0.005, 0.0001, "buy"))
	s.params.Register(FloatParameter("take_profit_ratio", 0.013, 0.005, 0.20, 0.005, "roi"))
	s.params.Register(FloatParameter("profit_callback", 0.003, 0.0001, 0.005, 0.0001, "roi"))
	s.params.Register(BoolParameter("double_first_order", false, "buy"))
	s.params.Register(CategoricalParameter("loop_type", "cycle", []string{"single", "cycle"}, "buy"))
	s.params.Register(IntParameter("loop_count", 100, 1, 10000, "buy"))
	s.params.Register(BoolParameter("enable_add_position", true, "buy"))
	s.params.Register(FloatParameter("flash_crash_protection", 0.02, 0.01, 0.10, 0.01, "protection"))

	return s
}

func (s *WallStreetStrategy) Name() string   { return s.name }
func (s *WallStreetStrategy) Symbol() string { return s.symbol }

func (s *WallStreetStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"symbol":                 s.symbol,
		"first_order_amount":     s.FirstOrderAmount,
		"order_count":            s.OrderCount,
		"add_position_spread":    s.AddPositionSpread,
		"add_position_callback":  s.AddPositionCallback,
		"take_profit_ratio":      s.TakeProfitRatio,
		"profit_callback":        s.ProfitCallback,
		"double_first_order":     s.DoubleFirstOrder,
		"loop_type":              s.LoopType,
		"loop_count":             s.LoopCount,
		"enable_add_position":    s.EnableAddPosition,
		"flash_crash_protection": s.FlashCrashProtection,
	}
}

func (s *WallStreetStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *WallStreetStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("wallstreet strategy apply params: %w", err)
	}
	if s.OrderCount <= 0 {
		s.OrderCount = 7
	}
	if s.FirstOrderAmount <= 0 {
		s.FirstOrderAmount = 100
	}
	if s.AddPositionSpread <= 0 {
		s.AddPositionSpread = 0.03
	}
	if s.TakeProfitRatio <= 0 {
		s.TakeProfitRatio = 0.013
	}
	if s.FlashCrashProtection <= 0 {
		s.FlashCrashProtection = 0.02
	}
	s.flashDetector = NewFlashCrashDetectorWithParams(time.Minute, s.FlashCrashProtection)
	s.running = true
	s.logger.Info("wallstreet strategy started", "symbol", s.symbol)
	return nil
}

func (s *WallStreetStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	s.positionCount = 0
	s.totalQuantity = 0
	s.avgEntryPrice = 0
	s.loopExecuted = 0
	s.waterfallPaused = false
	s.pendingAdd = false
	if s.flashDetector != nil {
		s.flashDetector.Reset()
	}
	s.logger.Info("wallstreet strategy stopped")
	return nil
}

func (s *WallStreetStrategy) GetParameters() *ParamRegistry { return s.params }

func (s *WallStreetStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *WallStreetStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if p := s.params.Get("first_order_amount"); p != nil {
		s.FirstOrderAmount = p.GetFloat()
	}
	if p := s.params.Get("order_count"); p != nil {
		s.OrderCount = p.GetInt()
	}
	if p := s.params.Get("add_position_spread"); p != nil {
		s.AddPositionSpread = p.GetFloat()
	}
	if p := s.params.Get("add_position_callback"); p != nil {
		s.AddPositionCallback = p.GetFloat()
	}
	if p := s.params.Get("take_profit_ratio"); p != nil {
		s.TakeProfitRatio = p.GetFloat()
	}
	if p := s.params.Get("profit_callback"); p != nil {
		s.ProfitCallback = p.GetFloat()
	}
	if p := s.params.Get("double_first_order"); p != nil {
		s.DoubleFirstOrder = p.GetBool()
	}
	if p := s.params.Get("loop_type"); p != nil {
		s.LoopType = p.GetString()
	}
	if p := s.params.Get("loop_count"); p != nil {
		s.LoopCount = p.GetInt()
	}
	if p := s.params.Get("enable_add_position"); p != nil {
		s.EnableAddPosition = p.GetBool()
	}
	if p := s.params.Get("flash_crash_protection"); p != nil {
		s.FlashCrashProtection = p.GetFloat()
	}
	return nil
}

func (s *WallStreetStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

// CalculatePositions computes Fibonacci-scaled position sizes.
func (s *WallStreetStrategy) CalculatePositions() []float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	positions := make([]float64, s.OrderCount)
	base := s.FirstOrderAmount
	if s.DoubleFirstOrder {
		base *= 2
	}
	for i := 0; i < s.OrderCount && i < len(fibonacciSequence); i++ {
		positions[i] = base * fibonacciSequence[i]
	}
	if s.OrderCount > len(fibonacciSequence) {
		lastFib := fibonacciSequence[len(fibonacciSequence)-1]
		for i := len(fibonacciSequence); i < s.OrderCount; i++ {
			nextFib := lastFib * 1.618
			positions[i] = base * nextFib
			lastFib = nextFib
		}
	}
	return positions
}

// CheckFlashCrash determines whether a flash crash condition is present.
func (s *WallStreetStrategy) CheckFlashCrash(priceHistory []PricePoint) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.flashDetector == nil || len(priceHistory) < 2 {
		return false
	}
	s.flashDetector.Reset()
	for _, p := range priceHistory {
		s.flashDetector.AddPrice(p.Price, p.Timestamp)
	}
	return s.flashDetector.IsFlashCrash()
}

// ShouldAddPosition determines whether a new add-position should be opened.
func (s *WallStreetStrategy) ShouldAddPosition(currentPrice, lastEntryPrice float64, priceHistory []PricePoint) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.EnableAddPosition {
		return false
	}
	if len(priceHistory) >= 2 {
		if s.CheckFlashCrash(priceHistory) {
			s.waterfallPaused = true
			s.logger.Warn("flash crash detected, pausing add-positions",
				"drop", s.flashDetector.LastDrop(),
				"threshold", s.FlashCrashProtection)
			return false
		}
	}
	s.waterfallPaused = false
	if lastEntryPrice <= 0 {
		return false
	}
	spread := (lastEntryPrice - currentPrice) / lastEntryPrice
	if spread < s.AddPositionSpread {
		return false
	}
	if len(priceHistory) == 0 {
		return false
	}
	lowest := priceHistory[0].Price
	for _, p := range priceHistory {
		if p.Price < lowest {
			lowest = p.Price
		}
	}
	if currentPrice >= lowest*(1+s.AddPositionCallback) {
		return true
	}
	return false
}

// TotalPositionValue returns the sum of all planned position values.
func (s *WallStreetStrategy) TotalPositionValue() float64 {
	positions := s.CalculatePositions()
	total := 0.0
	for _, p := range positions {
		total += p
	}
	return total
}

// OnTick processes tick events for flash crash detection.
func (s *WallStreetStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}
	if s.flashDetector != nil {
		s.flashDetector.AddPrice(tick.Last, tick.Timestamp)
		if s.flashDetector.IsFlashCrash() {
			if !s.waterfallPaused {
				s.waterfallPaused = true
				s.logger.Warn("flash crash detected on tick",
					"price", tick.Last,
					"drop", s.flashDetector.LastDrop())
			}
		} else if s.waterfallPaused {
			s.waterfallPaused = false
			s.logger.Info("flash crash recovery detected", "price", tick.Last)
		}
	}
	return nil, nil
}

// OnOrderBook handles order book updates.
func (s *WallStreetStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnBar processes bar data for entry/exit/add-position decisions.
func (s *WallStreetStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}
	if !s.inPosition {
		if s.LoopType == "single" && s.loopExecuted >= 1 {
			return nil, nil
		}
		if s.LoopType == "cycle" && s.loopExecuted >= s.LoopCount {
			return nil, nil
		}
		s.inPosition = true
		s.positionCount = 0
		s.entryPrice = bar.Close
		s.avgEntryPrice = bar.Close
		s.totalQuantity = 0
		s.lowestSinceEntry = bar.Close
		s.highestSinceEntry = bar.Close
		s.pendingAdd = false
		s.triggerLowPrice = 0
		positions := s.CalculatePositions()
		firstSize := positions[0]
		s.logger.Info("wallstreet first order", "price", bar.Close, "size", firstSize)
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "LONG",
			Strength:  0.8,
			Strategy:  s.name,
			Reason:    "wallstreet first order",
			Qty:       firstSize,
		}, nil
	}
	if bar.Close < s.lowestSinceEntry {
		s.lowestSinceEntry = bar.Close
		if s.pendingAdd {
			s.triggerLowPrice = bar.Low
		}
	}
	if bar.Close > s.highestSinceEntry {
		s.highestSinceEntry = bar.Close
	}
	if s.checkTakeProfit(bar) {
		s.inPosition = false
		s.positionCount = 0
		s.totalQuantity = 0
		s.avgEntryPrice = 0
		s.loopExecuted++
		s.pendingAdd = false
		s.logger.Info("wallstreet take profit", "price", bar.Close, "loop", s.loopExecuted)
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  1.0,
			Strategy:  s.name,
			Reason:    "wallstreet take profit",
		}, nil
	}
	if s.EnableAddPosition && s.positionCount < s.OrderCount-1 && !s.waterfallPaused {
		targetPrice := s.entryPrice * (1 - s.AddPositionSpread*float64(s.positionCount))
		if bar.Low <= targetPrice {
			if !s.pendingAdd {
				s.pendingAdd = true
				s.triggerLowPrice = bar.Low
			}
		}
		if s.pendingAdd && bar.Close >= s.triggerLowPrice*(1+s.AddPositionCallback) {
			s.pendingAdd = false
			s.positionCount++
			positions := s.CalculatePositions()
			addSize := positions[s.positionCount]
			s.logger.Info("wallstreet add position", "count", s.positionCount, "price", bar.Close, "size", addSize)
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "LONG",
				Strength:  0.7,
				Strategy:  s.name,
				Reason:    fmt.Sprintf("wallstreet add position #%d", s.positionCount),
				Qty:       addSize,
			}, nil
		}
	}
	return nil, nil
}

// OnOrderUpdate processes order fill updates.
func (s *WallStreetStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}
	if order.Status == model.StatusFilled {
		if order.Side == model.SideBuy {
			filledValue := order.AvgFillPrice * order.Filled
			if s.positionCount == 0 {
				s.avgEntryPrice = order.AvgFillPrice
				s.totalQuantity = order.Filled
			} else {
				totalValue := s.avgEntryPrice*s.totalQuantity + filledValue
				s.totalQuantity += order.Filled
				if s.totalQuantity > 0 {
					s.avgEntryPrice = totalValue / s.totalQuantity
				}
			}
		} else if order.Side == model.SideSell {
			s.inPosition = false
			s.positionCount = 0
			s.totalQuantity = 0
			s.avgEntryPrice = 0
			s.loopExecuted++
		}
	}
	return nil, nil
}

// checkTakeProfit evaluates trailing take-profit with profit callback.
func (s *WallStreetStrategy) checkTakeProfit(bar model.Bar) bool {
	if s.avgEntryPrice <= 0 || s.totalQuantity <= 0 {
		return false
	}
	profitPct := (bar.Close - s.avgEntryPrice) / s.avgEntryPrice
	if profitPct < s.TakeProfitRatio {
		return false
	}
	if s.highestSinceEntry > 0 {
		callbackFromHigh := (s.highestSinceEntry - bar.Close) / s.highestSinceEntry
		if callbackFromHigh >= s.ProfitCallback {
			return true
		}
	}
	return false
}
