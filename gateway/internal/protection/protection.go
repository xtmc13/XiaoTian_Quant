package protection

import (
	"fmt"
	"sync"
	"time"
)

// ProtectionResult is the outcome of a protection check.
type ProtectionResult struct {
	Blocked    bool      `json:"blocked"`     // true if trading should be stopped
	Reason     string    `json:"reason"`      // human-readable reason
	ResumeTime time.Time `json:"resume_time"` // when trading can resume (zero = indefinite)
	Pair       string    `json:"pair,omitempty"` // affected pair (empty = all pairs)
}

// IsPermanent returns true if the block has no automatic resume time.
func (r ProtectionResult) IsPermanent() bool {
	return r.Blocked && r.ResumeTime.IsZero()
}

// TradeRecord holds information about a completed trade for protection checks.
type TradeRecord struct {
	Symbol      string    `json:"symbol"`
	Side        string    `json:"side"`        // "LONG" or "SHORT"
	EntryPrice  float64   `json:"entry_price"`
	ExitPrice   float64   `json:"exit_price"`
	Quantity    float64   `json:"quantity"`
	PnL         float64   `json:"pnl"`         // profit/loss in quote currency
	PnLPct      float64   `json:"pnl_pct"`     // profit/loss percentage
	IsStoploss  bool      `json:"is_stoploss"` // true if exited via stoploss
	EntryTime   time.Time `json:"entry_time"`
	ExitTime    time.Time `json:"exit_time"`
}

// IsProfit returns true if the trade was profitable.
func (t TradeRecord) IsProfit() bool {
	return t.PnL > 0
}

// ProtectionContext provides the data needed for protection checks.
type ProtectionContext struct {
	// Current state
	Symbol      string    `json:"symbol"`
	Timeframe   string    `json:"timeframe"`
	CurrentTime time.Time `json:"current_time"`

	// Trade history (recent trades for lookback calculations)
	TradeHistory []TradeRecord `json:"trade_history"`

	// Portfolio state
	TotalBalance   float64 `json:"total_balance"`
	PeakBalance    float64 `json:"peak_balance"`    // highest balance in lookback period
	CurrentDrawdown float64 `json:"current_drawdown"` // 0.0 to 1.0

	// Per-pair stats
	PairTradeCount  int     `json:"pair_trade_count"`
	PairStoplossCount int   `json:"pair_stoploss_count"`
	PairProfitSum   float64 `json:"pair_profit_sum"`
	PairLossSum     float64 `json:"pair_loss_sum"`
}

// Protection is the interface for all protection mechanisms.
type Protection interface {
	// Name returns the protection's identifier.
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Check evaluates whether the protection should block trading.
	// Returns a ProtectionResult indicating whether trading is blocked.
	Check(ctx ProtectionContext) ProtectionResult

	// Validate checks the protection's configuration.
	Validate() error

	// Reset clears the protection's internal state (e.g., after manual override).
	Reset()
}

// ProtectionManager manages multiple protection mechanisms.
// It checks all protections before each trade and maintains block state.
type ProtectionManager struct {
	protections []Protection
	mu          sync.RWMutex

	// Block state
	globalBlocked bool
	globalResume  time.Time
	globalReason  string

	pairBlocked map[string]time.Time // pair -> resume time
	pairReason  map[string]string    // pair -> reason
}

// NewProtectionManager creates a new protection manager.
func NewProtectionManager() *ProtectionManager {
	return &ProtectionManager{
		pairBlocked: make(map[string]time.Time),
		pairReason:  make(map[string]string),
	}
}

// AddProtection adds a protection to the manager.
func (m *ProtectionManager) AddProtection(p Protection) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("protection %s validation failed: %w", p.Name(), err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.protections = append(m.protections, p)
	return nil
}

// RemoveProtection removes a protection by name.
func (m *ProtectionManager) RemoveProtection(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.protections {
		if p.Name() == name {
			m.protections = append(m.protections[:i], m.protections[i+1:]...)
			return
		}
	}
}

// Protections returns the current protection list.
func (m *ProtectionManager) Protections() []Protection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Protection, len(m.protections))
	copy(result, m.protections)
	return result
}

// CheckAll runs all protections against the given context.
// Returns the most restrictive result (permanent blocks > timed blocks > no block).
func (m *ProtectionManager) CheckAll(ctx ProtectionContext) ProtectionResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// First check cached global block
	if m.globalBlocked && m.globalResume.After(ctx.CurrentTime) {
		return ProtectionResult{
			Blocked:    true,
			Reason:     m.globalReason,
			ResumeTime: m.globalResume,
		}
	}

	// Check cached pair block
	if resume, ok := m.pairBlocked[ctx.Symbol]; ok && resume.After(ctx.CurrentTime) {
		return ProtectionResult{
			Blocked:    true,
			Reason:     m.pairReason[ctx.Symbol],
			ResumeTime: resume,
			Pair:       ctx.Symbol,
		}
	}

	// Run all protections
	var worstResult ProtectionResult
	for _, p := range m.protections {
		result := p.Check(ctx)
		if result.Blocked {
			// Prefer permanent blocks, then earliest resume time
			if !worstResult.Blocked {
				worstResult = result
			} else if worstResult.ResumeTime.IsZero() {
				// already permanent, keep it
			} else if result.ResumeTime.IsZero() {
				worstResult = result // permanent overrides timed
			} else if result.ResumeTime.After(worstResult.ResumeTime) {
				// later resume time = more restrictive
				worstResult = result
			}
		}
	}

	// Cache the result
	if worstResult.Blocked {
		if worstResult.Pair != "" {
			m.pairBlocked[worstResult.Pair] = worstResult.ResumeTime
			m.pairReason[worstResult.Pair] = worstResult.Reason
		} else {
			m.globalBlocked = true
			m.globalResume = worstResult.ResumeTime
			m.globalReason = worstResult.Reason
		}
	}

	return worstResult
}

// IsBlocked checks if trading is currently blocked (global or for a specific pair).
func (m *ProtectionManager) IsBlocked(symbol string, now time.Time) (bool, ProtectionResult) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check global block
	if m.globalBlocked {
		if m.globalResume.IsZero() || m.globalResume.After(now) {
			return true, ProtectionResult{
				Blocked:    true,
				Reason:     m.globalReason,
				ResumeTime: m.globalResume,
			}
		}
		// Expired, clear it
		m.globalBlocked = false
	}

	// Check pair block
	if resume, ok := m.pairBlocked[symbol]; ok {
		if resume.IsZero() || resume.After(now) {
			return true, ProtectionResult{
				Blocked:    true,
				Reason:     m.pairReason[symbol],
				ResumeTime: resume,
				Pair:       symbol,
			}
		}
		// Expired, clear it
		delete(m.pairBlocked, symbol)
		delete(m.pairReason, symbol)
	}

	return false, ProtectionResult{Blocked: false}
}

// ResetGlobal clears the global block.
func (m *ProtectionManager) ResetGlobal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.globalBlocked = false
	m.globalResume = time.Time{}
	m.globalReason = ""
}

// ResetPair clears the block for a specific pair.
func (m *ProtectionManager) ResetPair(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pairBlocked, symbol)
	delete(m.pairReason, symbol)
}

// ResetAll clears all blocks and protection states.
func (m *ProtectionManager) ResetAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.globalBlocked = false
	m.globalResume = time.Time{}
	m.globalReason = ""
	m.pairBlocked = make(map[string]time.Time)
	m.pairReason = make(map[string]string)
	for _, p := range m.protections {
		p.Reset()
	}
}

// Status returns the current block status for all protections.
func (m *ProtectionManager) Status(now time.Time) map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := map[string]any{
		"global_blocked": m.globalBlocked,
		"global_reason":  m.globalReason,
		"pair_blocks":  make(map[string]any),
	}

	if m.globalBlocked && !m.globalResume.IsZero() {
		status["global_resume_in"] = m.globalResume.Sub(now).String()
	}

	pairBlocks := make(map[string]any)
	for pair, resume := range m.pairBlocked {
		if resume.IsZero() || resume.After(now) {
			pairBlocks[pair] = map[string]any{
				"reason":     m.pairReason[pair],
				"resume_in":  resume.Sub(now).String(),
				"permanent":  resume.IsZero(),
			}
		}
	}
	status["pair_blocks"] = pairBlocks

	return status
}

// Config is the JSON-serializable configuration for protections.
type Config struct {
	Protections []ProtectionConfig `json:"protections"`
}

// ProtectionConfig is the configuration for a single protection.
type ProtectionConfig struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params,omitempty"`
}

// BuildManagerFromConfig creates a ProtectionManager from a configuration.
func BuildManagerFromConfig(cfg Config) (*ProtectionManager, error) {
	mgr := NewProtectionManager()
	for _, pc := range cfg.Protections {
		p, err := CreateProtection(pc.Name, pc.Params)
		if err != nil {
			return nil, fmt.Errorf("create protection %s: %w", pc.Name, err)
		}
		if err := mgr.AddProtection(p); err != nil {
			return nil, err
		}
	}
	return mgr, nil
}

// CreateProtection creates a protection by name with the given parameters.
func CreateProtection(name string, params map[string]any) (Protection, error) {
	switch name {
	case "CooldownPeriod":
		return NewCooldownPeriod(params)
	case "StoplossGuard":
		return NewStoplossGuard(params)
	case "MaxDrawdown":
		return NewMaxDrawdown(params)
	case "LowProfitPairs":
		return NewLowProfitPairs(params)
	case "DailyLossLimit":
		return NewDailyLossLimit(params)
	case "ConsecutiveLosses":
		return NewConsecutiveLosses(params)
	case "Overtrading":
		return NewOvertrading(params)
	case "PriceJump":
		return NewPriceJump(params)
	default:
		return nil, fmt.Errorf("unknown protection: %s", name)
	}
}
