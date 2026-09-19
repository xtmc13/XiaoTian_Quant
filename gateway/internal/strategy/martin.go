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

// MartinStrategy implements a martingale trend-following strategy that uses
// exponential position sizing (2, 4, 8, 16, 32, 64x) for add-positions.
// It includes flash crash protection, callback-based entry confirmation, and
// configurable loop modes for continuous or single-cycle execution.
type MartinStrategy struct {
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
	MaxLayers            int     // A1.4: 总层数硬上限（0=回退 OrderCount）
	MaxTotalBudget       float64 // A1.4: 累计投入预算硬限（0=不限）

	name    string
	symbol  string
	running bool
	mu      sync.RWMutex
	logger  *logging.Logger

	inPosition        bool
	positionCount     int
	pendingAddCount   int // 已发加仓信号、未成交确认的笔数（成交回报前不推进层数）
	entryPrice        float64
	avgEntryPrice     float64
	totalQuantity     float64
	totalInvested     float64 // 本轮循环累计投入（成交回报累计；A1.4 预算硬限）
	lowestSinceEntry  float64
	highestSinceEntry float64
	pendingAdd        bool
	triggerLowPrice   float64
	loopExecuted      int
	waterfallPaused   bool

	flashDetector *FlashCrashDetector
	params        *ParamRegistry
}

// MaxLayers 与 MaxTotalBudget（A1.4 安全硬限，对齐 QuantDinger）：
// MaxLayers>0 时作为总买入订单数（首单+加仓）的硬上限；MaxTotalBudget>0
// 时累计成交投入达到该值后拒绝再加仓。0 表示未设置（回退 OrderCount/不限）。

// effectiveAddCap 返回允许的最大加仓笔数（不含首单）：默认 OrderCount-1，
// max_layers>0 时收紧为 MaxLayers-1（max_layers 为含首单的总订单数硬上限）。
func (s *MartinStrategy) effectiveAddCap() int {
	cap_ := s.OrderCount - 1
	if s.MaxLayers > 0 && s.MaxLayers-1 < cap_ {
		cap_ = s.MaxLayers - 1
	}
	if cap_ < 0 {
		cap_ = 0
	}
	return cap_
}

// addCount 返回已发出（含在途）的加仓信号数：positionCount 含首单（成交
// 确认计数），pendingAddCount 为已发未成交的加仓数。
func (s *MartinStrategy) addCount() int {
	n := s.positionCount - 1 + s.pendingAddCount
	if n < 0 {
		n = 0
	}
	return n
}

// NewMartinStrategy creates a new MartinStrategy with sensible defaults.
func NewMartinStrategy() *MartinStrategy {
	s := &MartinStrategy{
		name:                 "martin_trend",
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
		logger:               logging.New("martin_strategy"),
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
	// A1.4 安全硬限：层数硬上限 + 总预算硬限（0=未设置）。
	s.params.Register(IntParameter("max_layers", 0, 0, 20, "protection"))
	s.params.Register(FloatParameter("max_total_budget", 0, 0, 100000000, 100, "protection"))

	return s
}

func (s *MartinStrategy) Name() string   { return s.name }
func (s *MartinStrategy) Symbol() string { return s.symbol }

func (s *MartinStrategy) Params() map[string]any {
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
		"max_layers":             s.MaxLayers,
		"max_total_budget":       s.MaxTotalBudget,
	}
}

func (s *MartinStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *MartinStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("martin strategy apply params: %w", err)
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
	s.logger.Info("martin strategy started", "symbol", s.symbol)
	return nil
}

func (s *MartinStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	s.positionCount = 0
	s.pendingAddCount = 0
	s.totalQuantity = 0
	s.totalInvested = 0
	s.avgEntryPrice = 0
	s.loopExecuted = 0
	s.waterfallPaused = false
	s.pendingAdd = false
	if s.flashDetector != nil {
		s.flashDetector.Reset()
	}
	s.logger.Info("martin strategy stopped")
	return nil
}

func (s *MartinStrategy) GetParameters() *ParamRegistry { return s.params }

func (s *MartinStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *MartinStrategy) ApplyParams(m map[string]any) error {
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
	if p := s.params.Get("max_layers"); p != nil {
		s.MaxLayers = p.GetInt()
	}
	if p := s.params.Get("max_total_budget"); p != nil {
		s.MaxTotalBudget = p.GetFloat()
	}
	return nil
}

func (s *MartinStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

// CalculatePositions computes the exponential position sizes.
func (s *MartinStrategy) CalculatePositions() []float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.calculatePositionsLocked()
}

// calculatePositionsLocked 是无锁内部版：调用方必须已持有 s.mu
// （OnBar 持写锁时直接调用，避免 RWMutex 写锁内重入读锁死锁）。
func (s *MartinStrategy) calculatePositionsLocked() []float64 {
	positions := make([]float64, s.OrderCount)
	base := s.FirstOrderAmount
	if s.DoubleFirstOrder {
		base *= 2
	}
	for i := 0; i < s.OrderCount; i++ {
		positions[i] = base * math.Pow(2, float64(i))
	}
	return positions
}

// CheckFlashCrash determines whether a flash crash condition is present.
func (s *MartinStrategy) CheckFlashCrash(priceHistory []PricePoint) bool {
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
func (s *MartinStrategy) ShouldAddPosition(currentPrice, lastEntryPrice float64, priceHistory []PricePoint) bool {
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

// OnTick processes tick events for flash crash detection.
func (s *MartinStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
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
func (s *MartinStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// OnBar processes bar data for entry/exit/add-position decisions.
func (s *MartinStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
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
		s.pendingAddCount = 0
		s.totalInvested = 0
		s.entryPrice = bar.Close
		s.avgEntryPrice = bar.Close
		s.totalQuantity = 0
		s.lowestSinceEntry = bar.Close
		s.highestSinceEntry = bar.Close
		s.pendingAdd = false
		s.triggerLowPrice = 0
		positions := s.calculatePositionsLocked()
		firstSize := positions[0]
		s.logger.Info("martin first order", "price", bar.Close, "size", firstSize)
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "LONG",
			Strength:  0.8,
			Strategy:  s.name,
			Reason:    "martin_trend first order",
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
		s.pendingAddCount = 0
		s.totalQuantity = 0
		s.totalInvested = 0
		s.avgEntryPrice = 0
		s.loopExecuted++
		s.pendingAdd = false
		s.logger.Info("martin take profit", "price", bar.Close, "loop", s.loopExecuted)
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  1.0,
			Strategy:  s.name,
			Reason:    "martin_trend take profit",
		}, nil
	}
	// A1.4 硬限：层数硬上限（含在途）+ 总预算硬限；有在途加仓单时不发新
	// 加仓信号（未成交不推进），层数推进发生在 OnOrderUpdate 成交回报。
	if s.EnableAddPosition && s.pendingAddCount == 0 && s.addCount() < s.effectiveAddCap() && !s.waterfallPaused {
		targetPrice := s.entryPrice * (1 - s.AddPositionSpread*float64(s.addCount()))
		if bar.Low <= targetPrice {
			if !s.pendingAdd {
				s.pendingAdd = true
				s.triggerLowPrice = bar.Low
			}
		}
		if s.pendingAdd && bar.Close >= s.triggerLowPrice*(1+s.AddPositionCallback) {
			nextIdx := s.addCount() + 1
			positions := s.calculatePositionsLocked()
			addSize := positions[0]
			if nextIdx < len(positions) {
				addSize = positions[nextIdx]
			}
			// 预算硬限：计划加仓金额会使累计投入超 max_total_budget → 拒绝。
			if s.MaxTotalBudget > 0 && s.totalInvested+addSize > s.MaxTotalBudget+1e-9 {
				s.logger.Warn("martin add position blocked by max_total_budget",
					"invested", s.totalInvested, "planned", addSize, "cap", s.MaxTotalBudget)
			} else {
				s.pendingAdd = false
				s.pendingAddCount++
				s.logger.Info("martin add position", "count", s.addCount(), "price", bar.Close, "size", addSize)
				return &model.Signal{
					Symbol:    s.symbol,
					Direction: "LONG",
					Strength:  0.7,
					Strategy:  s.name,
					Reason:    fmt.Sprintf("martin_trend add position #%d", s.addCount()),
					Qty:       addSize,
				}, nil
			}
		}
	}
	return nil, nil
}

// OnOrderUpdate processes order fill updates.
func (s *MartinStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, nil
	}
	if order.Status == model.StatusFilled {
		if order.Side == model.SideBuy {
			filledValue := order.AvgFillPrice * order.Filled
			if s.pendingAddCount > 0 {
				s.pendingAddCount--
			}
			s.totalInvested += filledValue
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
			s.positionCount++ // 成交回报确认后才推进层数（A1.4）
		} else if order.Side == model.SideSell {
			s.inPosition = false
			s.positionCount = 0
			s.pendingAddCount = 0
			s.totalQuantity = 0
			s.totalInvested = 0
			s.avgEntryPrice = 0
			s.loopExecuted++
		}
	} else if order.IsDone() && order.Status != model.StatusFilled {
		// Cancelled / rejected / expired：释放在途加仓，层数不推进（A1.4）。
		if order.Side == model.SideBuy && s.pendingAddCount > 0 {
			s.pendingAddCount--
		}
	}
	return nil, nil
}

// checkTakeProfit evaluates trailing take-profit with profit callback.
func (s *MartinStrategy) checkTakeProfit(bar model.Bar) bool {
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
