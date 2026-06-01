// Package protection implements automatic trade protection handlers.
// Unlike risk checks (synchronous, pre-trade), protections are
// asynchronous post-trade guards that lock trading when adverse
// patterns are detected, with automatic recovery after a cooldown.
package protection

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

// ── Types ──────────────────────────────────────────────────────

// Scope indicates whether a protection applies globally or per-pair.
type Scope int

const (
	ScopeGlobal Scope = iota
	ScopePair
)

func (s Scope) String() string {
	if s == ScopeGlobal {
		return "GLOBAL"
	}
	return "PAIR"
}

// ProtectionLock represents a trading restriction.
type ProtectionLock struct {
	Pair   string    // empty for global locks
	Until  time.Time // when the lock expires
	Reason string    // human-readable reason
	Source string    // which protection created this lock
}

// IsActive returns true if the lock has not expired.
func (l *ProtectionLock) IsActive() bool {
	return time.Now().Before(l.Until)
}

// TradeRecord is a simplified trade entry for protection analysis.
type TradeRecord struct {
	Pair      string
	EntryTime time.Time
	ExitTime  time.Time
	ProfitPct float64 // realized profit percentage
	IsStoploss bool   // whether exit was triggered by stoploss
}

// PortfolioSnapshot holds the state protections need to evaluate.
type PortfolioSnapshot struct {
	CurrentDrawdownPct float64
	ActivePairs        []string
}

// ── Interface ──────────────────────────────────────────────────

// IProtection defines a single protection rule.
type IProtection interface {
	Name() string
	// LockReason analyzes recent trades and current portfolio state.
	// Returns a lock if the protection should activate, or nil.
	LockReason(trades []TradeRecord, portfolio *PortfolioSnapshot) *ProtectionLock
}

// ── CooldownPeriod ─────────────────────────────────────────────

// CooldownPeriod locks trading globally for a cooldown after each trade.
// This prevents rapid re-entry and overtrading.
type CooldownPeriod struct {
	StopDuration time.Duration // how long to lock after a losing trade
	TradeDuration time.Duration // how long to lock after any trade
}

func NewCooldownPeriod(stopDuration, tradeDuration time.Duration) *CooldownPeriod {
	return &CooldownPeriod{StopDuration: stopDuration, TradeDuration: tradeDuration}
}

func (c *CooldownPeriod) Name() string { return "CooldownPeriod" }

func (c *CooldownPeriod) LockReason(trades []TradeRecord, _ *PortfolioSnapshot) *ProtectionLock {
	if len(trades) == 0 {
		return nil
	}

	last := trades[len(trades)-1]
	duration := c.TradeDuration
	reason := fmt.Sprintf("cooldown after trade on %s (%.2f%%)", last.Pair, last.ProfitPct)

	if last.ProfitPct < 0 && c.StopDuration > 0 {
		duration = c.StopDuration
		reason = fmt.Sprintf("cooldown after losing trade on %s (%.2f%%)", last.Pair, last.ProfitPct)
	}

	if duration <= 0 {
		return nil
	}

	since := time.Since(last.ExitTime)
	if since < duration {
		return &ProtectionLock{
			Until:  last.ExitTime.Add(duration),
			Reason: reason,
			Source: c.Name(),
		}
	}
	return nil
}

// ── LowProfitPairs ─────────────────────────────────────────────

// LowProfitPairs locks pairs that have generated low profit over recent trades.
type LowProfitPairs struct {
	LookbackPeriod time.Duration // how far back to look
	MinProfitPct   float64       // minimum profit to avoid lock
	LockDuration   time.Duration // how long to lock
	MaxTrades      int           // minimum trades needed to evaluate
}

func NewLowProfitPairs(lookback time.Duration, minProfit float64, lockDur time.Duration) *LowProfitPairs {
	return &LowProfitPairs{
		LookbackPeriod: lookback,
		MinProfitPct:   minProfit,
		LockDuration:   lockDur,
		MaxTrades:      3,
	}
}

func (l *LowProfitPairs) Name() string { return "LowProfitPairs" }

func (l *LowProfitPairs) LockReason(trades []TradeRecord, _ *PortfolioSnapshot) *ProtectionLock {
	cutoff := time.Now().Add(-l.LookbackPeriod)
	pairPnL := make(map[string]struct {
		count int
		total float64
		last  time.Time
	})

	for _, t := range trades {
		if t.ExitTime.Before(cutoff) {
			continue
		}
		entry := pairPnL[t.Pair]
		entry.count++
		entry.total += t.ProfitPct
		if t.ExitTime.After(entry.last) {
			entry.last = t.ExitTime
		}
		pairPnL[t.Pair] = entry
	}

	for pair, stats := range pairPnL {
		if stats.count < l.MaxTrades {
			continue
		}
		if stats.total < l.MinProfitPct {
			return &ProtectionLock{
				Pair:   pair,
				Until:  stats.last.Add(l.LockDuration),
				Reason: fmt.Sprintf("%s low profit: %.2f%% over %d trades (min %.2f%%)", pair, stats.total, stats.count, l.MinProfitPct),
				Source: l.Name(),
			}
		}
	}
	return nil
}

// ── MaxDrawdownProtection ──────────────────────────────────────

// MaxDrawdownProtection locks trading when portfolio drawdown exceeds a threshold.
type MaxDrawdownProtection struct {
	MaxDrawdownPct float64
	LockDuration   time.Duration
	StopTrading    bool // if true, locks ALL pairs (global lock)
}

func NewMaxDrawdownProtection(maxDrawdown float64, lockDur time.Duration, stopTrading bool) *MaxDrawdownProtection {
	return &MaxDrawdownProtection{
		MaxDrawdownPct: maxDrawdown,
		LockDuration:   lockDur,
		StopTrading:    stopTrading,
	}
}

func (m *MaxDrawdownProtection) Name() string { return "MaxDrawdownProtection" }

func (m *MaxDrawdownProtection) LockReason(_ []TradeRecord, portfolio *PortfolioSnapshot) *ProtectionLock {
	if portfolio == nil {
		return nil
	}
	if portfolio.CurrentDrawdownPct > m.MaxDrawdownPct {
		lock := &ProtectionLock{
			Until:  time.Now().Add(m.LockDuration),
			Reason: fmt.Sprintf("drawdown %.1f%% exceeds limit %.1f%%", portfolio.CurrentDrawdownPct, m.MaxDrawdownPct),
			Source: m.Name(),
		}
		if m.StopTrading {
			lock.Pair = "" // global lock
		}
		return lock
	}
	return nil
}

// ── StoplossGuard ──────────────────────────────────────────────

// StoplossGuard locks a pair if it has hit stoploss too many times
// in a short period, indicating the strategy is not working for it.
type StoplossGuard struct {
	LookbackPeriod   time.Duration
	MaxStoplossCount int
	LockDuration     time.Duration
}

func NewStoplossGuard(lookback time.Duration, maxCount int, lockDur time.Duration) *StoplossGuard {
	return &StoplossGuard{
		LookbackPeriod:   lookback,
		MaxStoplossCount: maxCount,
		LockDuration:     lockDur,
	}
}

func (s *StoplossGuard) Name() string { return "StoplossGuard" }

func (s *StoplossGuard) LockReason(trades []TradeRecord, _ *PortfolioSnapshot) *ProtectionLock {
	cutoff := time.Now().Add(-s.LookbackPeriod)
	pairStops := make(map[string]struct {
		count int
		last  time.Time
	})

	for _, t := range trades {
		if !t.IsStoploss || t.ExitTime.Before(cutoff) {
			continue
		}
		entry := pairStops[t.Pair]
		entry.count++
		if t.ExitTime.After(entry.last) {
			entry.last = t.ExitTime
		}
		pairStops[t.Pair] = entry
	}

	for pair, stats := range pairStops {
		if stats.count >= s.MaxStoplossCount {
			return &ProtectionLock{
				Pair:   pair,
				Until:  stats.last.Add(s.LockDuration),
				Reason: fmt.Sprintf("%s hit stoploss %d times in %v (max %d)", pair, stats.count, s.LookbackPeriod, s.MaxStoplossCount),
				Source: s.Name(),
			}
		}
	}
	return nil
}

// ── PairLock ───────────────────────────────────────────────────

// PairLock is a simple manual/automatic pair lock with a duration.
// It serves as a generic lock that other protections can delegate to.
type PairLock struct {
	mu           sync.RWMutex
	locks        map[string]time.Time // pair -> unlock time
	globalLocked bool
	globalUntil  time.Time
}

func NewPairLock() *PairLock {
	return &PairLock{
		locks: make(map[string]time.Time),
	}
}

func (p *PairLock) Name() string { return "PairLock" }

// LockPair locks a specific pair for the given duration.
func (p *PairLock) LockPair(pair string, duration time.Duration, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.locks[pair] = time.Now().Add(duration)
	log.Printf("[protection] PairLock: %s locked for %v: %s", pair, duration, reason)
}

// LockGlobal locks all trading for the given duration.
func (p *PairLock) LockGlobal(duration time.Duration, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.globalLocked = true
	p.globalUntil = time.Now().Add(duration)
	log.Printf("[protection] PairLock: GLOBAL lock for %v: %s", duration, reason)
}

// IsLocked checks if a pair (or all trading) is locked.
func (p *PairLock) IsLocked(pair string) (bool, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.globalLocked {
		if time.Now().Before(p.globalUntil) {
			return true, fmt.Sprintf("global lock until %s", p.globalUntil.Format(time.RFC3339))
		}
		// Expired — clear
		p.mu.RUnlock()
		p.mu.Lock()
		p.globalLocked = false
		p.mu.Unlock()
		p.mu.RLock()
	}

	until, ok := p.locks[pair]
	if !ok {
		return false, ""
	}
	if time.Now().Before(until) {
		return true, fmt.Sprintf("pair %s locked until %s", pair, until.Format(time.RFC3339))
	}
	// Expired — clear
	p.mu.RUnlock()
	p.mu.Lock()
	delete(p.locks, pair)
	p.mu.Unlock()
	p.mu.RLock()

	return false, ""
}

// ActiveLocks returns all currently active locks.
func (p *PairLock) ActiveLocks() []ProtectionLock {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var locks []ProtectionLock
	now := time.Now()

	if p.globalLocked && now.Before(p.globalUntil) {
		locks = append(locks, ProtectionLock{
			Until:  p.globalUntil,
			Reason: "global trading lock",
			Source: "PairLock",
		})
	}

	for pair, until := range p.locks {
		if now.Before(until) {
			locks = append(locks, ProtectionLock{
				Pair:   pair,
				Until:  until,
				Reason: fmt.Sprintf("pair %s locked", pair),
				Source: "PairLock",
			})
		}
	}

	sort.Slice(locks, func(i, j int) bool {
		return locks[i].Until.Before(locks[j].Until)
	})

	return locks
}

// ── Protection Manager ─────────────────────────────────────────

// Manager orchestrates all protection handlers and manages locks.
type Manager struct {
	protections []IProtection
	pairLock    *PairLock
	mu          sync.RWMutex

	// Recent trades for analysis
	trades    []TradeRecord
	maxTrades int

	// Portfolio state
	portfolio *PortfolioSnapshot

	// Callback
	OnLock func(lock ProtectionLock)
}

// Config configures the protection manager.
type Config struct {
	MaxTrades int `json:"max_trades"` // max recent trades to keep for analysis
}

func DefaultConfig() Config {
	return Config{MaxTrades: 500}
}

// NewManager creates a new protection manager.
func NewManager(cfg Config) *Manager {
	if cfg.MaxTrades <= 0 {
		cfg.MaxTrades = 500
	}
	return &Manager{
		pairLock:  NewPairLock(),
		maxTrades: cfg.MaxTrades,
	}
}

// AddProtection registers a protection handler.
func (m *Manager) AddProtection(p IProtection) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.protections = append(m.protections, p)
}

// RecordTrade adds a trade to the history.
func (m *Manager) RecordTrade(trade TradeRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.trades = append(m.trades, trade)
	if len(m.trades) > m.maxTrades {
		m.trades = m.trades[len(m.trades)-m.maxTrades:]
	}
}

// UpdatePortfolio refreshes the portfolio snapshot.
func (m *Manager) UpdatePortfolio(snapshot PortfolioSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.portfolio = &snapshot
}

// Check runs all protection handlers against the current state.
// Returns any new locks that should be applied.
func (m *Manager) Check() []ProtectionLock {
	m.mu.RLock()
	trades := make([]TradeRecord, len(m.trades))
	copy(trades, m.trades)
	portfolio := m.portfolio
	protections := make([]IProtection, len(m.protections))
	copy(protections, m.protections)
	m.mu.RUnlock()

	var newLocks []ProtectionLock

	for _, p := range protections {
		lock := p.LockReason(trades, portfolio)
		if lock == nil {
			continue
		}
		if time.Now().After(lock.Until) {
			continue // already expired
		}

		// Apply the lock
		if lock.Pair == "" {
			m.pairLock.LockGlobal(time.Until(lock.Until), lock.Reason)
		} else {
			m.pairLock.LockPair(lock.Pair, time.Until(lock.Until), lock.Reason)
		}

		newLocks = append(newLocks, *lock)

		if m.OnLock != nil {
			m.OnLock(*lock)
		}
	}

	return newLocks
}

// IsPairLocked checks if a pair is currently locked by any protection.
func (m *Manager) IsPairLocked(pair string) (bool, string) {
	locked, reason := m.pairLock.IsLocked(pair)
	return locked, reason
}

// ActiveLocks returns all currently active locks.
func (m *Manager) ActiveLocks() []ProtectionLock {
	return m.pairLock.ActiveLocks()
}

// PairLock returns the underlying pair lock for direct manipulation.
func (m *Manager) PairLock() *PairLock {
	return m.pairLock
}

// Trades returns a copy of recent trade records.
func (m *Manager) Trades() []TradeRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]TradeRecord, len(m.trades))
	copy(result, m.trades)
	return result
}
