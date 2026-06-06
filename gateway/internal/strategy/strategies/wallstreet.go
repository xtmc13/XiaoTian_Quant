package strategies

import (
	"fmt"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// WallstreetStrategy implements a Fibonacci-scaled averaging-down strategy.
// Similar to Martingale but uses Fibonacci sequence (1,2,3,5,8,13,21,34,55)
// for position scaling instead of exponential doubling.
type WallstreetStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	// Parameters (registered in ParamRegistry)
	firstOrderAmount  float64
	maxAddPositions   int
	stakeScale        []float64 // parsed from comma-separated string
	priceDeviationPct float64
	callbackPct       float64
	antiWaterfallPct  float64
	takeProfitMode    string
	takeProfitPct     float64
	profitCallbackPct float64
	loopType          string
	maxLoops          int

	// State
	inPosition        bool
	positionCount     int     // filled buy orders count
	pendingAddCount   int     // buy signals sent but not yet filled
	entryPrice        float64 // first order price
	avgEntryPrice     float64 // weighted average entry price
	totalQuantity     float64 // total filled quantity
	lowestSinceEntry  float64
	highestSinceEntry float64
	pendingAdd        bool    // price-deviation triggered, waiting callback
	triggerLowPrice   float64 // low price when deviation was triggered
	loopCount         int
	waterfallPaused   bool
	lastTickPrice     float64
	lastTickTime      int64
	bars              []model.Bar

	params *strategy.ParamRegistry
}

func NewWallstreetStrategy() *WallstreetStrategy {
	s := &WallstreetStrategy{
		name:              "wallstreet",
		symbol:            "BTCUSDT",
		firstOrderAmount:  100,
		maxAddPositions:   7,
		stakeScale:        []float64{1, 2, 3, 5, 8, 13, 21, 34, 55},
		priceDeviationPct: 0.03,
		callbackPct:       0.003,
		antiWaterfallPct:  0.02,
		takeProfitMode:    "full",
		takeProfitPct:     0.015,
		profitCallbackPct: 0.003,
		loopType:          "loop",
		maxLoops:          100,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.FloatParameter("first_order_amount", 100, 10, 10000, 10, "buy"))
	s.params.Register(strategy.IntParameter("max_add_positions", 7, 1, 20, "buy"))
	s.params.Register(strategy.CategoricalParameter("stake_scale", "1,2,3,5,8,13,21,34,55", nil, "buy"))
	s.params.Register(strategy.FloatParameter("price_deviation_pct", 0.03, 0.005, 0.50, 0.005, "buy"))
	s.params.Register(strategy.FloatParameter("callback_pct", 0.003, 0.0001, 0.005, 0.0001, "buy"))
	s.params.Register(strategy.FloatParameter("anti_waterfall_pct", 0.02, 0.01, 0.10, 0.01, "protection"))
	s.params.Register(strategy.CategoricalParameter("take_profit_mode", "full", []string{"full", "last", "first_last"}, "roi"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.015, 0.005, 0.20, 0.005, "roi"))
	s.params.Register(strategy.FloatParameter("profit_callback_pct", 0.003, 0.0001, 0.005, 0.0001, "roi"))
	s.params.Register(strategy.CategoricalParameter("loop_type", "loop", []string{"once", "loop"}, "buy"))
	s.params.Register(strategy.IntParameter("max_loops", 100, 1, 10000, "buy"))
	return s
}

func (s *WallstreetStrategy) Name() string   { return s.name }
func (s *WallstreetStrategy) Symbol() string { return s.symbol }

func (s *WallstreetStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":              s.symbol,
		"first_order_amount":  s.firstOrderAmount,
		"max_add_positions":   s.maxAddPositions,
		"stake_scale":         formatStakeScale(s.stakeScale),
		"price_deviation_pct": s.priceDeviationPct,
		"callback_pct":        s.callbackPct,
		"anti_waterfall_pct":  s.antiWaterfallPct,
		"take_profit_mode":    s.takeProfitMode,
		"take_profit_pct":     s.takeProfitPct,
		"profit_callback_pct": s.profitCallbackPct,
		"loop_type":           s.loopType,
		"max_loops":           s.maxLoops,
	}
}

func (s *WallstreetStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *WallstreetStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("wallstreet strategy apply params: %w", err)
	}

	if s.maxAddPositions <= 0 {
		s.maxAddPositions = 7
	}
	if s.firstOrderAmount <= 0 {
		s.firstOrderAmount = 100
	}
	if len(s.stakeScale) == 0 {
		s.stakeScale = []float64{1, 2, 3, 5, 8, 13, 21, 34, 55}
	}

	s.running = true
	return nil
}

// ── Parameter system implementation ────────────────────────────

func (s *WallstreetStrategy) GetParameters() *strategy.ParamRegistry {
	return s.params
}

func (s *WallstreetStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *WallstreetStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if p := s.params.Get("first_order_amount"); p != nil {
		s.firstOrderAmount = p.GetFloat()
	}
	if p := s.params.Get("max_add_positions"); p != nil {
		s.maxAddPositions = p.GetInt()
	}
	if p := s.params.Get("stake_scale"); p != nil {
		s.stakeScale = parseStakeScale(p.GetString())
	}
	if p := s.params.Get("price_deviation_pct"); p != nil {
		s.priceDeviationPct = p.GetFloat()
	}
	if p := s.params.Get("callback_pct"); p != nil {
		s.callbackPct = p.GetFloat()
	}
	if p := s.params.Get("anti_waterfall_pct"); p != nil {
		s.antiWaterfallPct = p.GetFloat()
	}
	if p := s.params.Get("take_profit_mode"); p != nil {
		s.takeProfitMode = p.GetString()
	}
	if p := s.params.Get("take_profit_pct"); p != nil {
		s.takeProfitPct = p.GetFloat()
	}
	if p := s.params.Get("profit_callback_pct"); p != nil {
		s.profitCallbackPct = p.GetFloat()
	}
	if p := s.params.Get("loop_type"); p != nil {
		s.loopType = p.GetString()
	}
	if p := s.params.Get("max_loops"); p != nil {
		s.maxLoops = p.GetInt()
	}
	return nil
}

func (s *WallstreetStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

func (s *WallstreetStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	s.positionCount = 0
	s.pendingAddCount = 0
	s.totalQuantity = 0
	s.avgEntryPrice = 0
	s.loopCount = 0
	s.waterfallPaused = false
	s.pendingAdd = false
	return nil
}

func (s *WallstreetStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil, nil
	}

	now := tick.Timestamp
	if s.lastTickTime == 0 {
		s.lastTickPrice = tick.Last
		s.lastTickTime = now
		return nil, nil
	}

	// Update reference price every 60 seconds
	if now-s.lastTickTime >= 60 {
		s.lastTickPrice = tick.Last
		s.lastTickTime = now
	}

	if s.lastTickPrice > 0 {
		dropPct := (s.lastTickPrice - tick.Last) / s.lastTickPrice
		if dropPct >= s.antiWaterfallPct {
			s.waterfallPaused = true
		} else if s.waterfallPaused && tick.Last > s.lastTickPrice*(1-s.antiWaterfallPct*0.5) {
			s.waterfallPaused = false
		}
	}

	return nil, nil
}

func (s *WallstreetStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *WallstreetStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running {
		return nil, nil
	}

	// Not in position — check if we can start a new loop
	if !s.inPosition {
		if s.loopType == "once" && s.loopCount >= 1 {
			return nil, nil
		}
		if s.loopType == "loop" && s.loopCount >= s.maxLoops {
			return nil, nil
		}
		// Open first order
		s.inPosition = true
		s.positionCount = 0
		s.pendingAddCount = 0
		s.entryPrice = bar.Close
		s.avgEntryPrice = bar.Close
		s.totalQuantity = 0
		s.lowestSinceEntry = bar.Close
		s.highestSinceEntry = bar.Close
		s.pendingAdd = false
		s.triggerLowPrice = 0
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "LONG",
			Strength:  0.8,
			Strategy:  s.name,
			Reason:    "wallstreet first order",
		}, nil
	}

	// Update high/low since entry
	if bar.Close < s.lowestSinceEntry {
		s.lowestSinceEntry = bar.Close
		if s.pendingAdd {
			// New low resets callback reference
			s.triggerLowPrice = bar.Low
		}
	}
	if bar.Close > s.highestSinceEntry {
		s.highestSinceEntry = bar.Close
	}

	// Check take-profit (trailing / callback)
	if s.checkTakeProfit(bar) {
		s.inPosition = false
		s.positionCount = 0
		s.pendingAddCount = 0
		s.totalQuantity = 0
		s.avgEntryPrice = 0
		s.loopCount++
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  1.0,
			Strategy:  s.name,
			Reason:    fmt.Sprintf("wallstreet take profit (%s)", s.takeProfitMode),
		}, nil
	}

	// Check add-position (averaging down)
	if s.positionCount+s.pendingAddCount < s.maxAddPositions && !s.waterfallPaused {
		// Target price for next add-position based on entry price
		targetPrice := s.entryPrice * (1 - s.priceDeviationPct*float64(s.positionCount))
		if bar.Low <= targetPrice {
			if !s.pendingAdd {
				s.pendingAdd = true
				s.triggerLowPrice = bar.Low
			}
		}
		if s.pendingAdd && bar.Close >= s.triggerLowPrice*(1+s.callbackPct) {
			s.pendingAdd = false
			s.pendingAddCount++
			return &model.Signal{
				Symbol:    s.symbol,
				Direction: "LONG",
				Strength:  0.7,
				Strategy:  s.name,
				Reason:    fmt.Sprintf("wallstreet add position #%d", s.positionCount+1),
			}, nil
		}
	}

	return nil, nil
}

func (s *WallstreetStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil, nil
	}

	if order.Status == model.StatusFilled {
		if order.Side == model.SideBuy {
			if s.pendingAddCount > 0 {
				s.pendingAddCount--
			}
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
			s.positionCount++
		} else if order.Side == model.SideSell {
			// Take-profit exit filled
			s.inPosition = false
			s.positionCount = 0
			s.pendingAddCount = 0
			s.totalQuantity = 0
			s.avgEntryPrice = 0
			s.loopCount++
		}
	} else if order.IsDone() && order.Status != model.StatusFilled {
		// Cancelled / rejected / expired
		if order.Side == model.SideBuy && s.pendingAddCount > 0 {
			s.pendingAddCount--
		}
	}

	return nil, nil
}

// checkTakeProfit evaluates trailing take-profit with profit callback.
func (s *WallstreetStrategy) checkTakeProfit(bar model.Bar) bool {
	if s.avgEntryPrice <= 0 || s.totalQuantity <= 0 {
		return false
	}
	profitPct := (bar.Close - s.avgEntryPrice) / s.avgEntryPrice
	if profitPct < s.takeProfitPct {
		return false
	}
	if s.highestSinceEntry > 0 {
		callbackFromHigh := (s.highestSinceEntry - bar.Close) / s.highestSinceEntry
		if callbackFromHigh >= s.profitCallbackPct {
			return true
		}
	}
	return false
}
