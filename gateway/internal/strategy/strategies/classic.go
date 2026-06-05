package strategies

import (
	"fmt"
	"math"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── EMACrossStrategy ───────────────────────────────────────────
// Enters when fast EMA crosses above/below slow EMA.

type EMACrossStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	fastPeriod   int
	slowPeriod   int
	stopLossPct  float64
	takeProfitPct float64
	positionSize float64

	bars       []model.Bar
	inPosition  bool
	entryPrice  float64
	direction   string

	params *strategy.ParamRegistry
}

func NewEMACrossStrategy() *EMACrossStrategy {
	s := &EMACrossStrategy{
		name:          "ema_cross",
		symbol:        "BTCUSDT",
		fastPeriod:    12,
		slowPeriod:    26,
		stopLossPct:   0.02,
		takeProfitPct: 0.04,
		positionSize:  500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("fast_period", 12, 5, 50, "buy"))
	s.params.Register(strategy.IntParameter("slow_period", 26, 10, 100, "buy"))
	s.params.Register(strategy.FloatParameter("stop_loss_pct", 0.02, 0.005, 0.20, 0.005, "stoploss"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.04, 0.01, 0.20, 0.01, "roi"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *EMACrossStrategy) Name() string   { return s.name }
func (s *EMACrossStrategy) Symbol() string { return s.symbol }

func (s *EMACrossStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":           s.symbol,
		"fast_period":      s.fastPeriod,
		"slow_period":      s.slowPeriod,
		"stop_loss_pct":    s.stopLossPct,
		"take_profit_pct":  s.takeProfitPct,
		"position_size":    s.positionSize,
	}
}

func (s *EMACrossStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *EMACrossStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("ema_cross apply params: %w", err)
	}
	if s.fastPeriod >= s.slowPeriod {
		s.fastPeriod = 12
		s.slowPeriod = 26
	}
	s.running = true
	return nil
}

func (s *EMACrossStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *EMACrossStrategy) ValidateParams() error {
	if s.params == nil { return nil }
	return s.params.Validate()
}
func (s *EMACrossStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil { return nil }
	if err := s.params.FromMap(m); err != nil { return err }
	if p := s.params.Get("fast_period"); p != nil { s.fastPeriod = p.GetInt() }
	if p := s.params.Get("slow_period"); p != nil { s.slowPeriod = p.GetInt() }
	if p := s.params.Get("stop_loss_pct"); p != nil { s.stopLossPct = p.GetFloat() }
	if p := s.params.Get("take_profit_pct"); p != nil { s.takeProfitPct = p.GetFloat() }
	if p := s.params.Get("position_size"); p != nil { s.positionSize = p.GetFloat() }
	return nil
}
func (s *EMACrossStrategy) ParamDefs() []map[string]any {
	if s.params == nil { return nil }
	return s.params.ToJSONDefs()
}

func (s *EMACrossStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *EMACrossStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *EMACrossStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *EMACrossStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }

func (s *EMACrossStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.slowPeriod+2 {
		return nil, nil
	}

	if s.inPosition {
		return s.checkExit(bar), nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars { closes[i] = b.Close }

	fastEMA := ema(closes, s.fastPeriod)
	slowEMA := ema(closes, s.slowPeriod)
	prevFast := ema(closes[:len(closes)-1], s.fastPeriod)
	prevSlow := ema(closes[:len(closes)-1], s.slowPeriod)

	if prevFast <= prevSlow && fastEMA > slowEMA {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.7, Strategy: s.name, Reason: "EMA golden cross"}, nil
	}
	if prevFast >= prevSlow && fastEMA < slowEMA {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.7, Strategy: s.name, Reason: "EMA death cross"}, nil
	}
	return nil, nil
}

func (s *EMACrossStrategy) checkExit(bar model.Bar) *model.Signal {
	if s.direction == "LONG" {
		if bar.Close <= s.entryPrice*(1-s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long stop loss"}
		}
		if bar.Close >= s.entryPrice*(1+s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long take profit"}
		}
	} else {
		if bar.Close >= s.entryPrice*(1+s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short stop loss"}
		}
		if bar.Close <= s.entryPrice*(1-s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short take profit"}
		}
	}
	return nil
}

// ── MACDStrategy ───────────────────────────────────────────────
// Enters when MACD line crosses above/below signal line.

type MACDStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	fastPeriod   int
	slowPeriod   int
	signalPeriod int
	stopLossPct  float64
	takeProfitPct float64
	positionSize float64

	bars       []model.Bar
	inPosition  bool
	entryPrice  float64
	direction   string

	params *strategy.ParamRegistry
}

func NewMACDStrategy() *MACDStrategy {
	s := &MACDStrategy{
		name:          "macd",
		symbol:        "BTCUSDT",
		fastPeriod:    12,
		slowPeriod:    26,
		signalPeriod:  9,
		stopLossPct:   0.02,
		takeProfitPct: 0.04,
		positionSize:  500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("fast_period", 12, 5, 50, "buy"))
	s.params.Register(strategy.IntParameter("slow_period", 26, 10, 100, "buy"))
	s.params.Register(strategy.IntParameter("signal_period", 9, 3, 30, "buy"))
	s.params.Register(strategy.FloatParameter("stop_loss_pct", 0.02, 0.005, 0.20, 0.005, "stoploss"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.04, 0.01, 0.20, 0.01, "roi"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *MACDStrategy) Name() string   { return s.name }
func (s *MACDStrategy) Symbol() string { return s.symbol }

func (s *MACDStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":          s.symbol,
		"fast_period":     s.fastPeriod,
		"slow_period":     s.slowPeriod,
		"signal_period":   s.signalPeriod,
		"stop_loss_pct":   s.stopLossPct,
		"take_profit_pct": s.takeProfitPct,
		"position_size":   s.positionSize,
	}
}

func (s *MACDStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *MACDStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("macd apply params: %w", err)
	}
	s.running = true
	return nil
}

func (s *MACDStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *MACDStrategy) ValidateParams() error {
	if s.params == nil { return nil }
	return s.params.Validate()
}
func (s *MACDStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil { return nil }
	if err := s.params.FromMap(m); err != nil { return err }
	if p := s.params.Get("fast_period"); p != nil { s.fastPeriod = p.GetInt() }
	if p := s.params.Get("slow_period"); p != nil { s.slowPeriod = p.GetInt() }
	if p := s.params.Get("signal_period"); p != nil { s.signalPeriod = p.GetInt() }
	if p := s.params.Get("stop_loss_pct"); p != nil { s.stopLossPct = p.GetFloat() }
	if p := s.params.Get("take_profit_pct"); p != nil { s.takeProfitPct = p.GetFloat() }
	if p := s.params.Get("position_size"); p != nil { s.positionSize = p.GetFloat() }
	return nil
}
func (s *MACDStrategy) ParamDefs() []map[string]any {
	if s.params == nil { return nil }
	return s.params.ToJSONDefs()
}

func (s *MACDStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *MACDStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *MACDStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *MACDStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }

func (s *MACDStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.slowPeriod+s.signalPeriod+5 {
		return nil, nil
	}

	if s.inPosition {
		return s.checkExit(bar), nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars { closes[i] = b.Close }

	macdLine, signalLine := macd(closes, s.fastPeriod, s.slowPeriod, s.signalPeriod)
	prevMACD, prevSignal := macd(closes[:len(closes)-1], s.fastPeriod, s.slowPeriod, s.signalPeriod)

	if prevMACD <= prevSignal && macdLine > signalLine {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.75, Strategy: s.name, Reason: "MACD bullish crossover"}, nil
	}
	if prevMACD >= prevSignal && macdLine < signalLine {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.75, Strategy: s.name, Reason: "MACD bearish crossover"}, nil
	}
	return nil, nil
}

func (s *MACDStrategy) checkExit(bar model.Bar) *model.Signal {
	if s.direction == "LONG" {
		if bar.Close <= s.entryPrice*(1-s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long stop loss"}
		}
		if bar.Close >= s.entryPrice*(1+s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long take profit"}
		}
	} else {
		if bar.Close >= s.entryPrice*(1+s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short stop loss"}
		}
		if bar.Close <= s.entryPrice*(1-s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short take profit"}
		}
	}
	return nil
}

// ── RSIStrategy ────────────────────────────────────────────────
// Enters when RSI exits overbought/oversold zones.

type RSIStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	period       int
	overbought   float64
	oversold     float64
	stopLossPct  float64
	takeProfitPct float64
	positionSize float64

	bars       []model.Bar
	inPosition  bool
	entryPrice  float64
	direction   string

	params *strategy.ParamRegistry
}

func NewRSIStrategy() *RSIStrategy {
	s := &RSIStrategy{
		name:          "rsi",
		symbol:        "BTCUSDT",
		period:        14,
		overbought:    70,
		oversold:      30,
		stopLossPct:   0.02,
		takeProfitPct: 0.04,
		positionSize:  500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("period", 14, 5, 50, "buy"))
	s.params.Register(strategy.FloatParameter("overbought", 70, 50, 90, 1, "buy"))
	s.params.Register(strategy.FloatParameter("oversold", 30, 10, 50, 1, "buy"))
	s.params.Register(strategy.FloatParameter("stop_loss_pct", 0.02, 0.005, 0.20, 0.005, "stoploss"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.04, 0.01, 0.20, 0.01, "roi"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *RSIStrategy) Name() string   { return s.name }
func (s *RSIStrategy) Symbol() string { return s.symbol }

func (s *RSIStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":          s.symbol,
		"period":          s.period,
		"overbought":      s.overbought,
		"oversold":        s.oversold,
		"stop_loss_pct":   s.stopLossPct,
		"take_profit_pct": s.takeProfitPct,
		"position_size":   s.positionSize,
	}
}

func (s *RSIStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *RSIStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("rsi apply params: %w", err)
	}
	s.running = true
	return nil
}

func (s *RSIStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *RSIStrategy) ValidateParams() error {
	if s.params == nil { return nil }
	return s.params.Validate()
}
func (s *RSIStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil { return nil }
	if err := s.params.FromMap(m); err != nil { return err }
	if p := s.params.Get("period"); p != nil { s.period = p.GetInt() }
	if p := s.params.Get("overbought"); p != nil { s.overbought = p.GetFloat() }
	if p := s.params.Get("oversold"); p != nil { s.oversold = p.GetFloat() }
	if p := s.params.Get("stop_loss_pct"); p != nil { s.stopLossPct = p.GetFloat() }
	if p := s.params.Get("take_profit_pct"); p != nil { s.takeProfitPct = p.GetFloat() }
	if p := s.params.Get("position_size"); p != nil { s.positionSize = p.GetFloat() }
	return nil
}
func (s *RSIStrategy) ParamDefs() []map[string]any {
	if s.params == nil { return nil }
	return s.params.ToJSONDefs()
}

func (s *RSIStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *RSIStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *RSIStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *RSIStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }

func (s *RSIStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.period+2 {
		return nil, nil
	}

	if s.inPosition {
		return s.checkExit(bar), nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars { closes[i] = b.Close }

	rsiVal := rsi(closes, s.period)
	prevRSI := rsi(closes[:len(closes)-1], s.period)

	if prevRSI < s.oversold && rsiVal >= s.oversold {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.7, Strategy: s.name, Reason: "RSI exit oversold"}, nil
	}
	if prevRSI > s.overbought && rsiVal <= s.overbought {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.7, Strategy: s.name, Reason: "RSI exit overbought"}, nil
	}
	return nil, nil
}

func (s *RSIStrategy) checkExit(bar model.Bar) *model.Signal {
	if s.direction == "LONG" {
		if bar.Close <= s.entryPrice*(1-s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long stop loss"}
		}
		if bar.Close >= s.entryPrice*(1+s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long take profit"}
		}
	} else {
		if bar.Close >= s.entryPrice*(1+s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short stop loss"}
		}
		if bar.Close <= s.entryPrice*(1-s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short take profit"}
		}
	}
	return nil
}

// ── BollingerBandsStrategy ─────────────────────────────────────
// Enters when price touches/exits Bollinger Bands.

type BollingerBandsStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	period       int
	stdDev       float64
	stopLossPct  float64
	takeProfitPct float64
	positionSize float64

	bars       []model.Bar
	inPosition  bool
	entryPrice  float64
	direction   string

	params *strategy.ParamRegistry
}

func NewBollingerBandsStrategy() *BollingerBandsStrategy {
	s := &BollingerBandsStrategy{
		name:          "bollinger_bands",
		symbol:        "BTCUSDT",
		period:        20,
		stdDev:        2.0,
		stopLossPct:   0.02,
		takeProfitPct: 0.04,
		positionSize:  500,
	}
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("period", 20, 10, 50, "buy"))
	s.params.Register(strategy.FloatParameter("std_dev", 2.0, 1.0, 3.0, 0.1, "buy"))
	s.params.Register(strategy.FloatParameter("stop_loss_pct", 0.02, 0.005, 0.20, 0.005, "stoploss"))
	s.params.Register(strategy.FloatParameter("take_profit_pct", 0.04, 0.01, 0.20, 0.01, "roi"))
	s.params.Register(strategy.FloatParameter("position_size", 500, 100, 5000, 100, "buy"))
	return s
}

func (s *BollingerBandsStrategy) Name() string   { return s.name }
func (s *BollingerBandsStrategy) Symbol() string { return s.symbol }

func (s *BollingerBandsStrategy) Params() map[string]any {
	return map[string]any{
		"symbol":          s.symbol,
		"period":          s.period,
		"std_dev":         s.stdDev,
		"stop_loss_pct":   s.stopLossPct,
		"take_profit_pct": s.takeProfitPct,
		"position_size":   s.positionSize,
	}
}

func (s *BollingerBandsStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *BollingerBandsStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("bollinger_bands apply params: %w", err)
	}
	s.running = true
	return nil
}

func (s *BollingerBandsStrategy) GetParameters() *strategy.ParamRegistry { return s.params }
func (s *BollingerBandsStrategy) ValidateParams() error {
	if s.params == nil { return nil }
	return s.params.Validate()
}
func (s *BollingerBandsStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil { return nil }
	if err := s.params.FromMap(m); err != nil { return err }
	if p := s.params.Get("period"); p != nil { s.period = p.GetInt() }
	if p := s.params.Get("std_dev"); p != nil { s.stdDev = p.GetFloat() }
	if p := s.params.Get("stop_loss_pct"); p != nil { s.stopLossPct = p.GetFloat() }
	if p := s.params.Get("take_profit_pct"); p != nil { s.takeProfitPct = p.GetFloat() }
	if p := s.params.Get("position_size"); p != nil { s.positionSize = p.GetFloat() }
	return nil
}
func (s *BollingerBandsStrategy) ParamDefs() []map[string]any {
	if s.params == nil { return nil }
	return s.params.ToJSONDefs()
}

func (s *BollingerBandsStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	return nil
}

func (s *BollingerBandsStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *BollingerBandsStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }
func (s *BollingerBandsStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) { return nil, nil }

func (s *BollingerBandsStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	if !s.running || len(s.bars) < s.period+2 {
		return nil, nil
	}

	if s.inPosition {
		return s.checkExit(bar), nil
	}

	closes := make([]float64, len(s.bars))
	for i, b := range s.bars { closes[i] = b.Close }

	upper, lower := bollingerBands(closes, s.period, s.stdDev)
	prevUpper, prevLower := bollingerBands(closes[:len(closes)-1], s.period, s.stdDev)
	c := closes[len(closes)-1]
	prevC := closes[len(closes)-2]

	if prevC <= prevLower && c > lower {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		return &model.Signal{Symbol: s.symbol, Direction: "LONG", Strength: 0.7, Strategy: s.name, Reason: "price bounced off lower band"}, nil
	}
	if prevC >= prevUpper && c < upper {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		return &model.Signal{Symbol: s.symbol, Direction: "SHORT", Strength: 0.7, Strategy: s.name, Reason: "price rejected at upper band"}, nil
	}
	return nil, nil
}

func (s *BollingerBandsStrategy) checkExit(bar model.Bar) *model.Signal {
	if s.direction == "LONG" {
		if bar.Close <= s.entryPrice*(1-s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long stop loss"}
		}
		if bar.Close >= s.entryPrice*(1+s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "long take profit"}
		}
	} else {
		if bar.Close >= s.entryPrice*(1+s.stopLossPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short stop loss"}
		}
		if bar.Close <= s.entryPrice*(1-s.takeProfitPct) {
			s.inPosition = false
			return &model.Signal{Symbol: s.symbol, Direction: "CLOSE", Strength: 1.0, Strategy: s.name, Reason: "short take profit"}
		}
	}
	return nil
}

// ── Technical Indicator Helpers ────────────────────────────────

func sma(data []float64, period int) float64 {
	if period > len(data) { period = len(data) }
	if period == 0 { return 0 }
	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i]
	}
	return sum / float64(period)
}

func ema(data []float64, period int) float64 {
	if len(data) < period { return sma(data, len(data)) }
	alpha := 2.0 / float64(period+1)
	e := data[0]
	for i := 1; i < len(data); i++ {
		e = alpha*data[i] + (1-alpha)*e
	}
	return e
}

func stdDev(data []float64, period int) float64 {
	if period > len(data) { period = len(data) }
	if period < 2 { return 0 }
	slice := data[len(data)-period:]
	m := sma(slice, period)
	sumSq := 0.0
	for _, v := range slice {
		d := v - m
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(period-1))
}

func rsi(data []float64, period int) float64 {
	if len(data) < period+1 { return 50 }
	gains, losses := 0.0, 0.0
	for i := len(data) - period; i < len(data); i++ {
		diff := data[i] - data[i-1]
		if diff > 0 {
			gains += diff
		} else {
			losses += -diff
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 { return 100 }
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func macd(data []float64, fast, slow, signal int) (macdLine, signalLine float64) {
	if len(data) < slow+signal {
		return 0, 0
	}
	fastEMA := ema(data, fast)
	slowEMA := ema(data, slow)
	macdLine = fastEMA - slowEMA

	// Signal line is EMA of MACD values — approximate with current MACD
	// For simplicity, compute signal from recent MACD values
	macdVals := make([]float64, signal+1)
	for i := 0; i <= signal; i++ {
		slice := data[:len(data)-i]
		if len(slice) >= slow {
			macdVals[i] = ema(slice, fast) - ema(slice, slow)
		}
	}
	signalLine = ema(macdVals, signal)
	return macdLine, signalLine
}

func bollingerBands(data []float64, period int, multiplier float64) (upper, lower float64) {
	if len(data) < period { return 0, 0 }
	m := sma(data, period)
	sd := stdDev(data, period)
	upper = m + multiplier*sd
	lower = m - multiplier*sd
	return upper, lower
}
