package order

import (
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// ── Stoploss Types ─────────────────────────────────────────────

// StoplossMode defines the stoploss mechanism.
type StoplossMode int

const (
	StoplossStatic           StoplossMode = iota // fixed price or percentage from entry
	StoplossTrailing                             // trails best price
	StoplossTrailingPositive                     // trailing only when in profit
)

func (m StoplossMode) String() string {
	switch m {
	case StoplossStatic:
		return "STATIC"
	case StoplossTrailing:
		return "TRAILING"
	case StoplossTrailingPositive:
		return "TRAILING_POSITIVE"
	default:
		return "UNKNOWN"
	}
}

// ── Stoploss Config ────────────────────────────────────────────

// StoplossConfig configures the stoploss behavior.
type StoplossConfig struct {
	Mode           StoplossMode `json:"mode"`
	StopDistance   float64      `json:"stop_distance"`  // decimal (0.02 = 2%)
	ATRMultiplier  float64      `json:"atr_multiplier"` // for ATR-based distance
	ATRPeriod      int          `json:"atr_period"`
	PositiveOffset float64      `json:"positive_offset"` // for trailing positive: activate when profit > offset%

	// Exchange integration
	PlaceOnExchange  bool          `json:"place_on_exchange"` // actually create stop-loss orders on exchange
	UpdateInterval   time.Duration `json:"update_interval"`   // minimum time between stoploss updates
	EmergencyTimeout time.Duration `json:"emergency_timeout"` // timeout before emergency market exit
}

func DefaultStoplossConfig() StoplossConfig {
	return StoplossConfig{
		Mode:             StoplossStatic,
		StopDistance:     0.02,
		ATRMultiplier:    1.5,
		ATRPeriod:        14,
		PositiveOffset:   0.01,
		PlaceOnExchange:  false,
		UpdateInterval:   60 * time.Second,
		EmergencyTimeout: 5 * time.Minute,
	}
}

// ── Stoploss Manager ────────────────────────────────────────────

// StoplossOrder represents a stoploss order placed on an exchange.
type StoplossOrder struct {
	OrderID     string
	Symbol      string
	Side        string // "SELL" for long position, "BUY" for short
	StopPrice   float64
	LimitPrice  float64
	Quantity    float64
	PlacedAt    time.Time
	LastUpdated time.Time
}

// OrderPlacer is the interface for placing orders on exchanges.
// Implemented by exchange adapters.
type OrderPlacer interface {
	PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error)
	CancelOrder(symbol, orderID string) (map[string]any, error)
}

// StoplossManager manages stoploss for positions.
type StoplossManager struct {
	config StoplossConfig
	mu     sync.RWMutex

	// Position tracking
	entryPrice     float64
	direction      string // "LONG" or "SHORT"
	quantity       float64
	bestPrice      float64 // highest for LONG, lowest for SHORT
	currentPrice   float64
	stopPrice      float64
	initialized    bool
	positiveActive bool // for trailing positive: activates after profit threshold

	// Exchange stoploss
	exchangeOrder *StoplossOrder
	orderPlacer   OrderPlacer

	// Rate limiting
	lastUpdate time.Time

	// ATR
	prices   []float64
	atrValue float64
}

// NewStoplossManager creates a stoploss manager.
func NewStoplossManager(cfg StoplossConfig, placer OrderPlacer) *StoplossManager {
	if cfg.ATRPeriod <= 0 {
		cfg.ATRPeriod = 14
	}
	if cfg.UpdateInterval <= 0 {
		cfg.UpdateInterval = 60 * time.Second
	}
	return &StoplossManager{
		config:      cfg,
		orderPlacer: placer,
		prices:      make([]float64, 0, cfg.ATRPeriod+1),
	}
}

// Initialize sets up stoploss for a new position.
func (s *StoplossManager) Initialize(entryPrice, quantity float64, direction string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entryPrice = entryPrice
	s.direction = direction
	s.quantity = quantity
	s.bestPrice = entryPrice
	s.currentPrice = entryPrice
	s.initialized = true
	s.positiveActive = false
	s.prices = s.prices[:0]
	s.prices = append(s.prices, entryPrice)
	s.atrValue = 0

	s.stopPrice = s.calcStaticStop()
	s.exchangeOrder = nil

	log.Printf("[stoploss] initialized: direction=%s entry=%.2f qty=%.4f stop=%.2f mode=%s",
		direction, entryPrice, quantity, s.stopPrice, s.config.Mode)
}

// Update recalculates the stoploss price based on current market price.
// Returns (stopPrice, triggered).
func (s *StoplossManager) Update(currentPrice float64) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.initialized {
		return 0, false
	}

	s.currentPrice = currentPrice

	// Update ATR
	s.prices = append(s.prices, currentPrice)
	if len(s.prices) > s.config.ATRPeriod+1 {
		s.prices = s.prices[1:]
	}
	if len(s.prices) >= s.config.ATRPeriod+1 {
		s.atrValue = calcATR(s.prices)
	}

	// Update best price
	if s.direction == "LONG" {
		if currentPrice > s.bestPrice {
			s.bestPrice = currentPrice
		}
	} else {
		if currentPrice < s.bestPrice {
			s.bestPrice = currentPrice
		}
	}

	// Calculate new stoploss
	newStop := s.calcStop()
	if s.direction == "LONG" {
		// Only move stop UP
		if newStop > s.stopPrice {
			s.stopPrice = newStop
		}
	} else {
		// Only move stop DOWN
		if newStop < s.stopPrice {
			s.stopPrice = newStop
		}
	}

	// Check if triggered
	var triggered bool
	if s.direction == "LONG" {
		triggered = currentPrice <= s.stopPrice
	} else {
		triggered = currentPrice >= s.stopPrice
	}

	// Try to update exchange stoploss order
	if s.config.PlaceOnExchange && s.orderPlacer != nil {
		s.maybeUpdateExchangeOrder()
	}

	s.lastUpdate = time.Now()

	return s.stopPrice, triggered
}

// calcStop calculates the target stop price based on mode.
func (s *StoplossManager) calcStop() float64 {
	switch s.config.Mode {
	case StoplossStatic:
		return s.calcStaticStop()
	case StoplossTrailing:
		return s.calcTrailingStop()
	case StoplossTrailingPositive:
		return s.calcTrailingPositiveStop()
	default:
		return s.calcStaticStop()
	}
}

// calcStaticStop returns a fixed stoploss from entry price.
func (s *StoplossManager) calcStaticStop() float64 {
	if s.direction == "LONG" {
		return s.entryPrice * (1 - s.config.StopDistance)
	}
	return s.entryPrice * (1 + s.config.StopDistance)
}

// calcTrailingStop returns a trailing stop from the best price.
func (s *StoplossManager) calcTrailingStop() float64 {
	distance := s.config.StopDistance
	if s.atrValue > 0 && s.config.ATRMultiplier > 0 {
		distance = s.atrValue * s.config.ATRMultiplier / s.bestPrice
	}
	if s.direction == "LONG" {
		return s.bestPrice * (1 - distance)
	}
	return s.bestPrice * (1 + distance)
}

// calcTrailingPositiveStop only activates trailing when in profit.
func (s *StoplossManager) calcTrailingPositiveStop() float64 {
	// Check if we're in profit enough to activate trailing
	profitPct := (s.currentPrice - s.entryPrice) / s.entryPrice
	if s.direction == "SHORT" {
		profitPct = (s.entryPrice - s.currentPrice) / s.entryPrice
	}

	if profitPct > s.config.PositiveOffset {
		s.positiveActive = true
	}

	if !s.positiveActive {
		// Use static stop until in profit
		return s.calcStaticStop()
	}

	// Activate trailing from best price, offset from activation price
	activationPrice := s.entryPrice * (1 + s.config.PositiveOffset)
	if s.direction == "SHORT" {
		activationPrice = s.entryPrice * (1 - s.config.PositiveOffset)
	}

	if s.direction == "LONG" {
		return math.Max(s.bestPrice*(1-s.config.StopDistance), activationPrice)
	}
	return math.Min(s.bestPrice*(1+s.config.StopDistance), activationPrice)
}

// maybeUpdateExchangeOrder creates or updates a stoploss order on the exchange.
func (s *StoplossManager) maybeUpdateExchangeOrder() {
	if !s.config.PlaceOnExchange || s.orderPlacer == nil {
		return
	}

	// Rate limit
	if time.Since(s.lastUpdate) < s.config.UpdateInterval {
		return
	}

	// Determine order side: long position → SELL stop, short position → BUY stop
	side := "SELL"
	if s.direction == "SHORT" {
		side = "BUY"
	}

	limitPrice := s.stopPrice
	if s.direction == "LONG" {
		limitPrice = s.stopPrice * 0.999 // slightly below stop for limit order
	} else {
		limitPrice = s.stopPrice * 1.001
	}

	// Cancel existing stoploss order if price changed significantly
	if s.exchangeOrder != nil {
		priceDiff := math.Abs(s.exchangeOrder.StopPrice-s.stopPrice) / s.exchangeOrder.StopPrice
		if priceDiff > 0.001 { // >0.1% change
			if _, err := s.orderPlacer.CancelOrder(s.exchangeOrder.Symbol, s.exchangeOrder.OrderID); err != nil {
				log.Printf("[stoploss] cancel order failed: %v", err)
			} else {
				log.Printf("[stoploss] cancelled old stop order %s", s.exchangeOrder.OrderID)
			}
			s.exchangeOrder = nil
		} else {
			return // no significant change, skip update
		}
	}

	// Place new stoploss order
	symbol := ""
	if s.exchangeOrder != nil {
		symbol = s.exchangeOrder.Symbol
	}
	if symbol == "" {
		// Must be set externally when position is opened
		return
	}

	result, err := s.orderPlacer.PlaceOrder(symbol, side, "LIMIT", limitPrice, s.quantity)
	if err != nil {
		log.Printf("[stoploss] place order failed: %v", err)
		// Check emergency timeout
		if s.exchangeOrder != nil && time.Since(s.exchangeOrder.PlacedAt) > s.config.EmergencyTimeout {
			log.Printf("[stoploss] EMERGENCY: stoploss order failed for %v, triggering market exit",
				s.config.EmergencyTimeout)
			// Signal emergency exit — caller should handle this
		}
		return
	}

	orderID := ""
	if id, ok := result["order_id"].(string); ok {
		orderID = id
	} else if id, ok := result["id"].(string); ok {
		orderID = id
	}

	s.exchangeOrder = &StoplossOrder{
		OrderID:     orderID,
		Symbol:      symbol,
		Side:        side,
		StopPrice:   s.stopPrice,
		LimitPrice:  limitPrice,
		Quantity:    s.quantity,
		PlacedAt:    time.Now(),
		LastUpdated: time.Now(),
	}
	s.lastUpdate = time.Now()

	log.Printf("[stoploss] placed exchange order: %s %s stop=%.2f limit=%.2f qty=%.4f",
		side, orderID, s.stopPrice, limitPrice, s.quantity)
}

// SetSymbol sets the trading symbol for exchange stoploss orders.
func (s *StoplossManager) SetSymbol(symbol string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exchangeOrder != nil {
		s.exchangeOrder.Symbol = symbol
	}
}

// CancelExchangeOrder cancels any active stoploss order on the exchange.
func (s *StoplossManager) CancelExchangeOrder() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.exchangeOrder != nil && s.orderPlacer != nil {
		s.orderPlacer.CancelOrder(s.exchangeOrder.Symbol, s.exchangeOrder.OrderID)
		s.exchangeOrder = nil
	}
}

// StopPrice returns the current stoploss price.
func (s *StoplossManager) StopPrice() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopPrice
}

// IsInitialized returns whether the stoploss has been set.
func (s *StoplossManager) IsInitialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.initialized
}

// Reset clears the stoploss state.
func (s *StoplossManager) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Cancel exchange order without double-locking
	if s.exchangeOrder != nil && s.orderPlacer != nil {
		s.orderPlacer.CancelOrder(s.exchangeOrder.Symbol, s.exchangeOrder.OrderID)
	}
	s.initialized = false
	s.exchangeOrder = nil
	s.prices = s.prices[:0]
}

// Status returns a human-readable summary.
func (s *StoplossManager) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exch := "local"
	if s.exchangeOrder != nil {
		exch = fmt.Sprintf("exchange(%s)", s.exchangeOrder.OrderID)
	}

	return fmt.Sprintf("stop=%.2f mode=%s pos=%s active=%v on=%s",
		s.stopPrice, s.config.Mode, s.direction, s.positiveActive, exch)
}

// ── Helpers ────────────────────────────────────────────────────

func calcATR(prices []float64) float64 {
	if len(prices) < 2 {
		return 0
	}
	sum := 0.0
	for i := 1; i < len(prices); i++ {
		sum += math.Abs(prices[i] - prices[i-1])
	}
	return sum / float64(len(prices)-1)
}
