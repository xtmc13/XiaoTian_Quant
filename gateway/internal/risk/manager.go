package risk

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── Risk Context ──

// Context holds aggregated risk data for check evaluation.
type Context struct {
	Symbol           string
	CurrentPrice     float64
	TotalEquity      float64
	AvailableBalance float64
	MarginUsed       float64
	PositionCount    int
	NetExposure      float64
	DailyOrderCount  int
	DailyPnL         float64
	MaxDrawdownPct   float64
	ConsecutiveLosses int
	FundingRate      float64
	MarginRatio      float64
	Volatility       float64
	BidPrice         float64
	AskPrice         float64
	LastTradeTime    int64
	OrderPrice       float64
	OrderQuantity    float64
	OrderSide        model.OrderSide
	Blacklist        map[string]bool

	// ── Contract fields ──
	MarketType     model.MarketType
	PositionSide   model.PositionSide
	Leverage       float64
	MarginMode     model.MarginMode
	ClosePosition  bool
}

// ── Check Function ──

// CheckFn is a single risk check function.
type CheckFn func(ctx *Context) error

// ── Circuit Breaker ──

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed    CircuitState = iota // Normal operation
	CircuitOpen                           // Tripped, rejecting requests
	CircuitHalfOpen                       // Testing if recovery is possible
)

func (cs CircuitState) String() string {
	switch cs {
	case CircuitClosed:
		return "CLOSED"
	case CircuitOpen:
		return "OPEN"
	case CircuitHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	state         CircuitState
	failureCount  int
	threshold     int
	resetTimeout  time.Duration
	lastTrip      time.Time
	tripCount     int
	mu            sync.RWMutex
}

func NewCircuitBreaker(threshold int, resetTimeout time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	if resetTimeout <= 0 {
		resetTimeout = 60 * time.Second
	}
	return &CircuitBreaker{
		state:        CircuitClosed,
		threshold:    threshold,
		resetTimeout: resetTimeout,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastTrip) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == CircuitHalfOpen || cb.state == CircuitOpen {
		cb.state = CircuitClosed
		cb.failureCount = 0
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failureCount++
	if cb.failureCount >= cb.threshold || cb.state == CircuitHalfOpen {
		cb.state = CircuitOpen
		cb.lastTrip = time.Now()
		cb.tripCount++
	}
}

func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) TripCount() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.tripCount
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitClosed
	cb.failureCount = 0
	cb.tripCount = 0
}

// ── 15 Risk Checks ──

// PriceSanity checks if the order price deviates too far from the market price.
func PriceSanity(maxDeviationPct float64) CheckFn {
	if maxDeviationPct <= 0 {
		maxDeviationPct = 5.0
	}
	return func(ctx *Context) error {
		if ctx.CurrentPrice == 0 || ctx.OrderPrice == 0 {
			return nil
		}
		deviation := math.Abs(ctx.OrderPrice-ctx.CurrentPrice) / ctx.CurrentPrice * 100
		if deviation > maxDeviationPct {
			return fmt.Errorf("price deviation %.1f%% exceeds limit %.1f%%", deviation, maxDeviationPct)
		}
		return nil
	}
}

// OrderSize checks if the order exceeds the maximum USDT value.
func OrderSize(maxUSDT float64) CheckFn {
	if maxUSDT <= 0 {
		maxUSDT = 100000
	}
	return func(ctx *Context) error {
		value := ctx.OrderPrice * ctx.OrderQuantity
		if value > maxUSDT {
			return fmt.Errorf("order value %.0f exceeds max %.0f USDT", value, maxUSDT)
		}
		return nil
	}
}

// DailyLimit checks the daily order count limit.
func DailyLimit(maxOrders int) CheckFn {
	if maxOrders <= 0 {
		maxOrders = 100
	}
	return func(ctx *Context) error {
		if ctx.DailyOrderCount >= maxOrders {
			return fmt.Errorf("daily order limit reached (%d/%d)", ctx.DailyOrderCount, maxOrders)
		}
		return nil
	}
}

// RateLimit enforces a minimum interval between trades to prevent rapid-fire orders.
// Deprecated: the Manager now applies per-symbol rate limiting internally using
// ManagerConfig.MinOrderIntervalMs. This standalone helper is kept for backward
// compatibility but is no longer part of the default check chain.
func RateLimit() CheckFn {
	const minIntervalMs = 500
	var lastTradeTime int64
	var mu sync.Mutex
	return func(ctx *Context) error {
		mu.Lock()
		defer mu.Unlock()
		now := time.Now().UnixMilli()
		if lastTradeTime > 0 && (now-lastTradeTime) < minIntervalMs {
			return fmt.Errorf("rate limit: minimum %.0fms between orders (last was %dms ago)",
				float64(minIntervalMs), now-lastTradeTime)
		}
		lastTradeTime = now
		return nil
	}
}

// ConcurrentOrders checks the number of active orders.
func ConcurrentOrders(maxCount int) CheckFn {
	if maxCount <= 0 {
		maxCount = 5
	}
	return func(ctx *Context) error {
		if ctx.PositionCount >= maxCount {
			return fmt.Errorf("too many active orders (%d/%d)", ctx.PositionCount, maxCount)
		}
		return nil
	}
}

// PositionLimit checks that no single position exceeds a percentage of equity.
func PositionLimit(maxPct float64) CheckFn {
	if maxPct <= 0 {
		maxPct = 50.0
	}
	return func(ctx *Context) error {
		if ctx.TotalEquity <= 0 {
			return nil
		}
		exposure := ctx.OrderPrice * ctx.OrderQuantity / ctx.TotalEquity * 100
		if exposure > maxPct {
			return fmt.Errorf("position exposure %.1f%% exceeds limit %.1f%%", exposure, maxPct)
		}
		return nil
	}
}

// NetExposure checks total portfolio exposure.
func NetExposure(maxPct float64) CheckFn {
	if maxPct <= 0 {
		maxPct = 80.0
	}
	return func(ctx *Context) error {
		if ctx.TotalEquity <= 0 {
			return nil
		}
		if ctx.NetExposure > maxPct {
			return fmt.Errorf("net exposure %.1f%% exceeds limit %.1f%%", ctx.NetExposure, maxPct)
		}
		return nil
	}
}

// MaxDrawdown checks portfolio drawdown.
func MaxDrawdown(maxPct float64) CheckFn {
	if maxPct <= 0 {
		maxPct = 10.0
	}
	return func(ctx *Context) error {
		if ctx.MaxDrawdownPct > maxPct {
			return fmt.Errorf("drawdown %.1f%% exceeds limit %.1f%%", ctx.MaxDrawdownPct, maxPct)
		}
		return nil
	}
}

// ConsecutiveLosses checks the number of consecutive losing trades.
func ConsecutiveLossesCheck(maxCount int) CheckFn {
	if maxCount <= 0 {
		maxCount = 5
	}
	return func(ctx *Context) error {
		if ctx.ConsecutiveLosses >= maxCount {
			return fmt.Errorf("consecutive losses %d exceeds limit %d", ctx.ConsecutiveLosses, maxCount)
		}
		return nil
	}
}

// FundingRate checks if the funding rate is acceptable.
func FundingRate(maxRatePct float64) CheckFn {
	if maxRatePct <= 0 {
		maxRatePct = 0.375
	}
	return func(ctx *Context) error {
		if math.Abs(ctx.FundingRate) > maxRatePct {
			return fmt.Errorf("funding rate %.3f%% exceeds limit %.3f%%", ctx.FundingRate, maxRatePct)
		}
		return nil
	}
}

// MarginRatio checks margin safety.
func MarginRatio(minRatioPct float64) CheckFn {
	if minRatioPct <= 0 {
		minRatioPct = 150.0
	}
	return func(ctx *Context) error {
		if ctx.MarginRatio > 0 && ctx.MarginRatio < minRatioPct {
			return fmt.Errorf("margin ratio %.1f%% below minimum %.1f%%", ctx.MarginRatio, minRatioPct)
		}
		return nil
	}
}

// Blacklist checks if the symbol is blacklisted.
func Blacklist(list map[string]bool) CheckFn {
	return func(ctx *Context) error {
		if list[ctx.Symbol] {
			return fmt.Errorf("symbol %s is blacklisted", ctx.Symbol)
		}
		return nil
	}
}

// Volatility checks if market volatility is within limits.
func Volatility(maxPct float64) CheckFn {
	if maxPct <= 0 {
		maxPct = 2.0
	}
	return func(ctx *Context) error {
		if ctx.Volatility > maxPct {
			return fmt.Errorf("volatility %.2f%% exceeds limit %.2f%%", ctx.Volatility, maxPct)
		}
		return nil
	}
}

// TimeWindow restricts trading to specific time windows.
func TimeWindow(startHour, endHour int) CheckFn {
	return func(ctx *Context) error {
		hour := time.Now().Hour()
		if hour < startHour || hour >= endHour {
			return fmt.Errorf("outside trading hours (%d:00-%d:00)", startHour, endHour)
		}
		return nil
	}
}

// PriceSpike checks for sudden price movements.
func PriceSpike(maxMovePct float64) CheckFn {
	if maxMovePct <= 0 {
		maxMovePct = 3.0
	}
	return func(ctx *Context) error {
		if ctx.BidPrice > 0 && ctx.AskPrice > 0 {
			spread := (ctx.AskPrice - ctx.BidPrice) / ctx.BidPrice * 100
			if spread > maxMovePct {
				return fmt.Errorf("price spike detected: spread %.2f%%", spread)
			}
		}
		return nil
	}
}

// ── Risk Manager ──

// Manager orchestrates all risk checks and the circuit breaker.
type Manager struct {
	checks         []CheckFn
	circuitBreaker *CircuitBreaker
	blacklist      map[string]bool
	mu             sync.RWMutex
	cfg            ManagerConfig // stored running config

	// Per-symbol last trade time for rate limiting.
	lastTradeTime map[string]int64
	ltMu          sync.Mutex

	// Metrics
	dailyOrderCount   int
	consecutiveLosses int
	riskEvents        []model.RiskAlert
	resetDay          string

	// Callback
	OnRiskAlert func(alert model.RiskAlert)
}

// ManagerConfig configures the risk manager.
type ManagerConfig struct {
	CircuitThreshold    int           `json:"circuit_threshold"`
	CircuitResetTimeout time.Duration `json:"circuit_reset_timeout"`
	PriceDeviationPct   float64       `json:"price_deviation_pct"`
	MaxOrderUSDT        float64       `json:"max_order_usdt"`
	DailyOrderLimit     int           `json:"daily_order_limit"`
	MaxConcurrentOrders int           `json:"max_concurrent_orders"`
	MaxPositionPct      float64       `json:"max_position_pct"`
	MaxExposurePct      float64       `json:"max_exposure_pct"`
	MaxDrawdownPct      float64       `json:"max_drawdown_pct"`
	MaxConsecutiveLosses int          `json:"max_consecutive_losses"`
	MaxFundingRatePct   float64       `json:"max_funding_rate_pct"`
	MinMarginRatioPct   float64       `json:"min_margin_ratio_pct"`
	MaxVolatilityPct    float64       `json:"max_volatility_pct"`
	PriceSpikePct       float64       `json:"price_spike_pct"`
	// MinOrderIntervalMs is the minimum time (in milliseconds) between two
	// orders on the same symbol. A value <= 0 disables the limit.
	MinOrderIntervalMs int `json:"min_order_interval_ms"`
}

func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		CircuitThreshold:     5,
		CircuitResetTimeout:  60 * time.Second,
		PriceDeviationPct:    5.0,
		MaxOrderUSDT:         100000,
		DailyOrderLimit:      100,
		MaxConcurrentOrders:  5,
		MaxPositionPct:       50.0,
		MaxExposurePct:       80.0,
		MaxDrawdownPct:       10.0,
		MaxConsecutiveLosses: 5,
		MaxFundingRatePct:    0.375,
		MinMarginRatioPct:    150.0,
		MaxVolatilityPct:     2.0,
		PriceSpikePct:        3.0,
		MinOrderIntervalMs:   500,
	}
}

var (
	riskMgr     *Manager
	riskMgrOnce sync.Once
)

// GetManager returns the global risk manager.
func GetManager() *Manager {
	riskMgrOnce.Do(func() {
		riskMgr = NewManager(DefaultManagerConfig())
	})
	return riskMgr
}

// NewManager creates a risk manager with the given config.
func NewManager(cfg ManagerConfig) *Manager {
	mgr := &Manager{
		cfg:           cfg,
		circuitBreaker: NewCircuitBreaker(cfg.CircuitThreshold, cfg.CircuitResetTimeout),
		blacklist:     make(map[string]bool),
		lastTradeTime: make(map[string]int64),
	}

	mgr.checks = mgr.buildChecks(cfg)
	return mgr
}

// buildChecks builds the check chain from the given config.
func (m *Manager) buildChecks(cfg ManagerConfig) []CheckFn {
	return []CheckFn{
		PriceSanity(cfg.PriceDeviationPct),
		OrderSize(cfg.MaxOrderUSDT),
		DailyLimit(cfg.DailyOrderLimit),
		ConcurrentOrders(cfg.MaxConcurrentOrders),
		PositionLimit(cfg.MaxPositionPct),
		NetExposure(cfg.MaxExposurePct),
		MaxDrawdown(cfg.MaxDrawdownPct),
		ConsecutiveLossesCheck(cfg.MaxConsecutiveLosses),
		FundingRate(cfg.MaxFundingRatePct),
		MarginRatio(cfg.MinMarginRatioPct),
		Blacklist(m.blacklist),
		Volatility(cfg.MaxVolatilityPct),
		PriceSpike(cfg.PriceSpikePct),
		// TimeWindow is not added by default
	}
}

// Check runs all risk checks against the given context.
func (m *Manager) Check(ctx *Context) error {
	if ctx == nil {
		return fmt.Errorf("risk context is nil")
	}

	// Check circuit breaker
	if !m.circuitBreaker.Allow() {
		alert := model.RiskAlert{
			Level:     "CRITICAL",
			CheckName: "circuit_breaker",
			Message:   "Circuit breaker is OPEN",
			Timestamp: time.Now().UnixMilli(),
		}
		m.recordRiskEvent(alert)
		return fmt.Errorf("circuit breaker open")
	}

	// Update daily counter
	m.updateDaily()

	// Populate fields managed by the manager itself so standalone check
	// functions see the current runtime state.
	m.mu.RLock()
	ctx.DailyOrderCount = m.dailyOrderCount
	ctx.ConsecutiveLosses = m.consecutiveLosses
	m.mu.RUnlock()

	// Per-symbol rate limit (not included in the generic check chain because
	// it needs mutable manager state).
	if err := m.checkRateLimit(ctx); err != nil {
		alert := model.RiskAlert{
			Level:     "WARN",
			CheckName: "rate_limit",
			Message:   err.Error(),
			Symbol:    ctx.Symbol,
			Timestamp: time.Now().UnixMilli(),
		}
		m.recordRiskEvent(alert)
		m.circuitBreaker.RecordFailure()
		return err
	}

	// Run all checks
	for _, check := range m.checks {
		if err := check(ctx); err != nil {
			m.circuitBreaker.RecordFailure()

			alert := model.RiskAlert{
				Level:     "WARN",
				CheckName: "risk_check",
				Message:   err.Error(),
				Symbol:    ctx.Symbol,
				Timestamp: time.Now().UnixMilli(),
			}
			m.recordRiskEvent(alert)
			return err
		}
	}

	m.circuitBreaker.RecordSuccess()
	m.recordTrade(ctx.Symbol)
	m.mu.Lock()
	m.dailyOrderCount++
	m.mu.Unlock()
	return nil
}

// AddBlacklist adds a symbol to the blacklist.
func (m *Manager) AddBlacklist(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blacklist[symbol] = true
}

// RemoveBlacklist removes a symbol from the blacklist.
func (m *Manager) RemoveBlacklist(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blacklist, symbol)
}

// RecordLoss updates the consecutive loss counter.
func (m *Manager) RecordLoss() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.consecutiveLosses++
}

// RecordWin resets the consecutive loss counter.
func (m *Manager) RecordWin() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.consecutiveLosses = 0
}

// GetCircuitBreaker returns the circuit breaker.
func (m *Manager) GetCircuitBreaker() *CircuitBreaker {
	return m.circuitBreaker
}

// Config returns a copy of the manager's running config.
func (m *Manager) Config() ManagerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// UpdateConfig replaces the running config and rebuilds the check chain and
// circuit breaker. It can be called at runtime to adjust risk parameters.
func (m *Manager) UpdateConfig(cfg ManagerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	m.circuitBreaker = NewCircuitBreaker(cfg.CircuitThreshold, cfg.CircuitResetTimeout)
	m.checks = m.buildChecks(cfg)
}

// ── Internal ──

func (m *Manager) updateDaily() {
	today := time.Now().Format("2006-01-02")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.resetDay != today {
		m.dailyOrderCount = 0
		m.resetDay = today
	}
}

func (m *Manager) checkRateLimit(ctx *Context) error {
	interval := int64(m.cfg.MinOrderIntervalMs)
	if interval <= 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	m.ltMu.Lock()
	defer m.ltMu.Unlock()
	if m.lastTradeTime == nil {
		m.lastTradeTime = make(map[string]int64)
	}
	sym := ctx.Symbol
	if last, ok := m.lastTradeTime[sym]; ok && (now-last) < interval {
		return fmt.Errorf("rate limit: minimum %dms between orders for %s (last was %dms ago)",
			interval, sym, now-last)
	}
	return nil
}

func (m *Manager) recordTrade(symbol string) {
	m.ltMu.Lock()
	defer m.ltMu.Unlock()
	if m.lastTradeTime == nil {
		m.lastTradeTime = make(map[string]int64)
	}
	m.lastTradeTime[symbol] = time.Now().UnixMilli()
}

func (m *Manager) recordRiskEvent(alert model.RiskAlert) {
	m.mu.Lock()
	m.riskEvents = append(m.riskEvents, alert)
	if len(m.riskEvents) > 1000 {
		m.riskEvents = m.riskEvents[len(m.riskEvents)-1000:]
	}
	m.mu.Unlock()

	if m.OnRiskAlert != nil {
		m.OnRiskAlert(alert)
	}
}

// GetRiskEvents returns recent risk events.
func (m *Manager) GetRiskEvents(limit int) []model.RiskAlert {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > len(m.riskEvents) {
		limit = len(m.riskEvents)
	}
	start := len(m.riskEvents) - limit
	result := make([]model.RiskAlert, limit)
	copy(result, m.riskEvents[start:])
	return result
}
