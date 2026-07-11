package cra

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/logging"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// BaseCRAStrategy implements the shared CRA spot/contract execution engine.
type BaseCRAStrategy struct {
	strategy.BaseStrategy

	name    string
	symbol  string
	isContract bool

	mu      sync.RWMutex
	running bool
	params  *CRAParams
	state   *CRAState
	bars    []model.Bar

	flashDetector *strategy.FlashCrashDetector
	logger        *logging.Logger
}

// NewCRASpotStrategy creates a spot CRA strategy instance.
func NewCRASpotStrategy(name, symbol string) *BaseCRAStrategy {
	return &BaseCRAStrategy{
		name:       name,
		symbol:     symbol,
		isContract: false,
		state:      &CRAState{},
		logger:     logging.New("cra_spot"),
	}
}

// NewCRAContractStrategy creates a contract CRA strategy instance.
func NewCRAContractStrategy(name, symbol string) *BaseCRAStrategy {
	return &BaseCRAStrategy{
		name:       name,
		symbol:     symbol,
		isContract: true,
		state:      &CRAState{},
		logger:     logging.New("cra_contract"),
	}
}

func (s *BaseCRAStrategy) Name() string   { return s.name }
func (s *BaseCRAStrategy) Symbol() string { return s.symbol }

func (s *BaseCRAStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *BaseCRAStrategy) Params() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.params == nil {
		return nil
	}
	b, _ := json.Marshal(s.params)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["symbol"] = s.symbol
	return m
}

// Start initializes the strategy from a flattened parameter map.
func (s *BaseCRAStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if params == nil {
		params = map[string]any{}
	}
	// Preserve symbol/direction/market_type from top-level params.
	if v, ok := params["symbol"].(string); ok && v != "" {
		s.symbol = v
	}

	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("cra start: marshal params: %w", err)
	}
	p, err := ParseCRAParams(string(data))
	if err != nil {
		return fmt.Errorf("cra start: parse params: %w", err)
	}
	if err := p.Validate(); err != nil {
		return fmt.Errorf("cra start: validate: %w", err)
	}
	if s.isContract {
		p.MarketType = "swap"
	} else {
		p.MarketType = "spot"
	}
	if s.isContract && p.Leverage < 1 {
		p.Leverage = 1
	}

	s.params = p
	s.state = &CRAState{}
	s.bars = nil
	if p.WaterfallEnabled {
		s.flashDetector = strategy.NewFlashCrashDetectorWithParams(time.Minute, p.WaterfallProtection)
	} else {
		s.flashDetector = nil
	}
	s.running = true
	s.logger.Info("cra strategy started", "symbol", s.symbol, "type", s.name, "market", p.MarketType)
	return nil
}

func (s *BaseCRAStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	if s.state != nil {
		s.state.Reset()
	}
	s.bars = nil
	s.logger.Info("cra strategy stopped", "symbol", s.symbol)
	return nil
}

func (s *BaseCRAStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.params == nil || s.flashDetector == nil {
		return nil, nil
	}
	s.flashDetector.AddPrice(tick.Last, tick.Timestamp)
	if s.flashDetector.IsFlashCrash() {
		if !s.state.WaterfallPaused {
			s.state.WaterfallPaused = true
			s.logger.Warn("waterfall protection triggered", "symbol", s.symbol, "drop", s.flashDetector.LastDrop())
		}
	} else if s.state.WaterfallPaused {
		// Resume when the drop has recovered below half the threshold.
		if s.flashDetector.LastDrop() < s.params.WaterfallProtection*0.5 {
			s.state.WaterfallPaused = false
			s.logger.Info("waterfall protection resumed", "symbol", s.symbol)
		}
	}
	return nil, nil
}

func (s *BaseCRAStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *BaseCRAStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.params == nil {
		return nil, nil
	}

	s.bars = append(s.bars, bar)
	if len(s.bars) > 200 {
		s.bars = s.bars[len(s.bars)-200:]
	}

	p := s.params
	st := s.state

	if !st.InPosition {
		if !st.CanStartNewLoop(p.TradeCountMode, p.LoopCount) {
			return nil, nil
		}
		// Limit order price check for the first order.
		if p.FirstOrderPrice > 0 {
			if s.isLongPreferred() && bar.Close >= p.FirstOrderPrice {
				return nil, nil
			}
			if !s.isLongPreferred() && bar.Close <= p.FirstOrderPrice {
				return nil, nil
			}
		}
		if s.isContract {
			side := s.resolveContractSide()
			if !s.openIndicatorsConfirmed(side) {
				return nil, nil
			}
			st.EnterPosition(bar.Close, side)
		} else {
			st.EnterPosition(bar.Close, SideLong)
		}
		qty := RoundQty(s.entryQty(bar.Close, p.FirstOrderMultiplier))
		s.logger.Info("cra first order", "symbol", s.symbol, "price", bar.Close, "qty", qty, "side", st.Side)
		return s.signal(st.SignalDirection(), qty, "cra first order"), nil
	}

	st.UpdateExtremes(bar.Close)

	// Stop-loss (contract only by default, but kept generic).
	if s.isContract && st.CheckStopLoss(bar.Close, p) {
		s.logger.Info("cra stop loss", "symbol", s.symbol, "price", bar.Close)
		st.ExitPosition()
		return s.signal("CLOSE", 0, "cra stop loss"), nil
	}

	// Take profit.
	if s.checkTakeProfit(bar.Close) {
		s.logger.Info("cra take profit", "symbol", s.symbol, "price", bar.Close, "loops", st.LoopExecuted+1)
		st.ExitPosition()
		return s.signal("CLOSE", 0, "cra take profit"), nil
	}

	// Add positions.
	if p.EnableAddPosition && st.PositionCount+st.PendingAddCount < p.OrderCount && !st.WaterfallPaused {
		nextOrder := st.PositionCount + st.PendingAddCount + 1
		cfg := p.AddPositionForOrder(nextOrder)
		if cfg != nil {
			// For contract, check per-order EMA switch and global add indicators.
			if s.isContract && !s.addPositionConfirmed(cfg) {
				return nil, nil
			}
			if st.ShouldAddPosition(bar.Close, cfg) {
				st.PendingAddCount++
				qty := RoundQty(s.entryQty(bar.Close, cfg.Multiplier))
				s.logger.Info("cra add position", "symbol", s.symbol, "order", nextOrder, "price", bar.Close, "qty", qty)
				return s.signal(st.SignalDirection(), qty, fmt.Sprintf("cra add position #%d", nextOrder)), nil
			}
		}
	}

	return nil, nil
}

func (s *BaseCRAStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.params == nil || s.state == nil {
		return nil, nil
	}

	if order.Status == model.StatusFilled {
		if order.ClosePosition {
			s.state.ExitPosition()
			return nil, nil
		}
		isEntry := (s.state.Side == SideLong && order.Side == model.SideBuy) ||
			(s.state.Side == SideShort && order.Side == model.SideSell)
		if isEntry {
			if s.state.PendingAddCount > 0 {
				s.state.PendingAddCount--
			}
			s.state.PositionCount++
			s.state.RecordFill(order.AvgFillPrice, order.Filled, SideBuy)
		}
	}
	return nil, nil
}

// ── helpers ──

func (s *BaseCRAStrategy) signal(direction string, qty float64, reason string) *model.Signal {
	sig := &model.Signal{
		Symbol:    s.symbol,
		Direction: direction,
		Strength:  0.8,
		Strategy:  s.name,
		Reason:    reason,
		Timestamp: time.Now().UnixMilli(),
	}
	if qty > 0 {
		sig.Qty = qty
	}
	return sig
}

func (s *BaseCRAStrategy) entryQty(price, multiplier float64) float64 {
	if price <= 0 {
		return 0
	}
	notional := s.params.FirstOrderAmount * multiplier
	if s.isContract {
		return (notional * s.params.Leverage) / price
	}
	return notional / price
}

func (s *BaseCRAStrategy) isLongPreferred() bool {
	if !s.isContract {
		return true
	}
	switch s.params.Direction {
	case "short":
		return false
	case "dual":
		// For dual, choose based on short-term trend at loop start.
		closes := BarsToCloses(s.bars)
		if len(closes) < 20 {
			return true
		}
		ema := EMA(closes, 20)
		return closes[len(closes)-1] > ema[len(ema)-1]
	}
	return true
}

func (s *BaseCRAStrategy) resolveContractSide() PositionSide {
	switch s.params.Direction {
	case "short":
		return SideShort
	case "dual":
		if s.isLongPreferred() {
			return SideLong
		}
		return SideShort
	}
	return SideLong
}

func (s *BaseCRAStrategy) checkTakeProfit(price float64) bool {
	p := s.params
	st := s.state
	if p.TPMode == "moving" {
		return st.CheckMovingTakeProfit(price, p.MovingTakeProfitTiers)
	}
	return st.CheckStaticTakeProfit(price, p.TakeProfitMethod, p.TakeProfitRatio, p.ProfitCallback)
}

func (s *BaseCRAStrategy) openIndicatorsConfirmed(side PositionSide) bool {
	p := s.params
	bars := s.indicatorBars()
	if p.OpenMacdEnabled && !IndicatorConfirmed(bars, true, p.OpenMacdPeriod, "macd", side) {
		return false
	}
	if p.OpenCounterEmaEnabled && !IndicatorConfirmed(bars, true, p.OpenCounterEmaPeriod, "ema", side) {
		return false
	}
	if p.OpenTrendEmaEnabled && !IndicatorConfirmed(bars, true, p.OpenTrendEmaPeriod, "ema", side) {
		return false
	}
	return true
}

func (s *BaseCRAStrategy) addPositionConfirmed(cfg *AddPositionItem) bool {
	p := s.params
	bars := s.indicatorBars()
	if cfg.EmaEnabled {
		if p.AddEmaEnabled && !IndicatorConfirmed(bars, true, p.AddEmaPeriod, "ema", s.state.Side) {
			return false
		}
	}
	if p.AddMacdEnabled && !IndicatorConfirmed(bars, true, p.AddMacdPeriod, "macd", s.state.Side) {
		return false
	}
	return true
}

func (s *BaseCRAStrategy) indicatorBars() []model.Bar {
	if len(s.bars) > 50 {
		return s.bars[len(s.bars)-50:]
	}
	return s.bars
}

// RoundQty rounds quantity to a reasonable precision for order placement.
func RoundQty(qty float64) float64 {
	if qty <= 0 {
		return 0
	}
	// Round to 6 decimal places for crypto spot, 3 for larger quantities.
	prec := 1000000.0
	if qty >= 1 {
		prec = 1000.0
	}
	return math.Round(qty*prec) / prec
}
