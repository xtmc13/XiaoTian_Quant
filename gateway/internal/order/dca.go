// Package order — DCA (Dollar Cost Averaging) position adjustment.
//
// DCA allows a strategy to scale into a position over multiple entries
// at predefined price deviations, rather than entering all at once.
// This reduces timing risk and averages the entry price.
package order

import (
	"fmt"
	"sync"
)

// DCAMode defines which direction DCA is active for.
type DCAMode string

const (
	DCALongOnly  DCAMode = "long_only"
	DCAShortOnly DCAMode = "short_only"
	DCABoth      DCAMode = "both"
)

// DCAConfig configures the DCA behavior for a position.
type DCAConfig struct {
	// MaxEntries is the maximum number of DCA entries (including the initial).
	// e.g., 3 means: initial entry + up to 2 more DCA entries.
	MaxEntries int `json:"max_entries"`

	// PriceDeviation is the percentage distance between entries.
	// For LONG: each subsequent entry triggers when price drops this % below the last entry.
	// For SHORT: each subsequent entry triggers when price rises this % above the last entry.
	// e.g., 0.03 means enter again at -3%, -6%, -9% for long.
	PriceDeviation float64 `json:"price_deviation"`

	// StakeScale defines how the position size scales per entry.
	// [1.0, 1.5, 2.0] means: 1x on first entry, 1.5x on second, 2x on third.
	// If nil or shorter than MaxEntries, the last value is repeated.
	StakeScale []float64 `json:"stake_scale"`
}

// DefaultDCAConfig returns a sensible DCA configuration.
func DefaultDCAConfig() DCAConfig {
	return DCAConfig{
		MaxEntries:     3,
		PriceDeviation: 0.03, // 3%
		StakeScale:     []float64{1.0, 1.5, 2.0},
	}
}

// DCAEntry represents a single DCA entry level.
type DCAEntry struct {
	Level    int     // 0 = initial, 1 = first DCA, etc.
	Price    float64 // target price for this entry
	Size     float64 // position size for this entry
	Executed bool    // whether this entry has been filled
}

// DCAPosition tracks the DCA state for a single position.
type DCAPosition struct {
	Symbol        string
	Side          string  // "LONG" or "SHORT"
	BaseSize      float64 // base position size (stake * scale[0])
	Deviation     float64 // price deviation between levels
	Entries       []DCAEntry
	FilledCount   int
	TotalSize     float64
	AvgEntryPrice float64
	Active        bool
}

// DCAManager manages DCA for multiple positions.
type DCAManager struct {
	mu        sync.RWMutex
	positions map[string]*DCAPosition // symbol -> DCA state
}

// NewDCAManager creates a new DCA manager.
func NewDCAManager() *DCAManager {
	return &DCAManager{
		positions: make(map[string]*DCAPosition),
	}
}

// SetupDCA initializes DCA levels for a new position.
// baseSize is the size for the first (initial) entry.
func (m *DCAManager) SetupDCA(symbol, side string, baseSize float64, config DCAConfig) (*DCAPosition, error) {
	if baseSize <= 0 {
		return nil, fmt.Errorf("base size must be positive")
	}
	if config.MaxEntries <= 0 {
		config.MaxEntries = 3
	}
	if config.PriceDeviation <= 0 {
		config.PriceDeviation = 0.03
	}

	// Normalize stake scale
	scale := config.StakeScale
	if len(scale) == 0 {
		scale = []float64{1.0}
	}

	entries := make([]DCAEntry, config.MaxEntries)
	for i := 0; i < config.MaxEntries; i++ {
		s := scale[0]
		if i < len(scale) {
			s = scale[i]
		}
		entries[i] = DCAEntry{
			Level: i,
			Size:  baseSize * s,
		}
	}

	pos := &DCAPosition{
		Symbol:    symbol,
		Side:      side,
		BaseSize:  baseSize,
		Deviation: config.PriceDeviation,
		Entries:   entries,
		Active:    true,
	}

	m.mu.Lock()
	m.positions[symbol] = pos
	m.mu.Unlock()

	return pos, nil
}

// CheckEntry determines if a new DCA entry should be triggered at the current price.
// Returns the entry to execute, or nil if no entry is due.
// Call this after filling an entry to record the price.
func (m *DCAManager) CheckEntry(symbol string, currentPrice float64) (*DCAEntry, error) {
	m.mu.RLock()
	pos, ok := m.positions[symbol]
	m.mu.RUnlock()

	if !ok || !pos.Active {
		return nil, nil
	}

	if pos.FilledCount >= len(pos.Entries) {
		// Upgrade to write lock to deactivate
		m.mu.Lock()
		// Re-check after acquiring write lock
		if pos2, ok2 := m.positions[symbol]; ok2 && pos2.FilledCount >= len(pos2.Entries) {
			pos2.Active = false
		}
		m.mu.Unlock()
		return nil, nil
	}

	// For the initial entry (level 0), always allow
	entry := &pos.Entries[pos.FilledCount]
	if entry.Executed {
		return nil, nil
	}

	// Level 0 (initial) always triggers — no price condition
	if entry.Level > 0 {
		triggerPrice := pos.calcTriggerPrice(entry.Level)
		if triggerPrice <= 0 {
			return nil, nil // can't calculate trigger
		}

		if pos.Side == "LONG" {
			if currentPrice > triggerPrice {
				return nil, nil
			}
		} else {
			if currentPrice < triggerPrice {
				return nil, nil
			}
		}
	}

	return entry, nil
}

// RecordEntry marks a DCA entry as executed at the given price.
// Returns the updated average entry price and total size.
func (m *DCAManager) RecordEntry(symbol string, price float64, level int) (avgPrice float64, totalSize float64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pos, ok := m.positions[symbol]
	if !ok {
		return 0, 0, fmt.Errorf("no DCA position for %s", symbol)
	}

	if level >= len(pos.Entries) {
		return 0, 0, fmt.Errorf("entry level %d exceeds max %d", level, len(pos.Entries))
	}

	entry := &pos.Entries[level]
	if entry.Executed {
		return 0, 0, fmt.Errorf("entry level %d already executed", level)
	}

	entry.Price = price
	entry.Executed = true
	pos.FilledCount++

	// Recalculate average entry price
	totalCost := 0.0
	pos.TotalSize = 0
	for _, e := range pos.Entries {
		if e.Executed {
			totalCost += e.Price * e.Size
			pos.TotalSize += e.Size
		}
	}
	if pos.TotalSize > 0 {
		pos.AvgEntryPrice = totalCost / pos.TotalSize
	}

	if pos.FilledCount >= len(pos.Entries) {
		pos.Active = false
	}

	return pos.AvgEntryPrice, pos.TotalSize, nil
}

// GetPosition returns the DCA state for a symbol.
func (m *DCAManager) GetPosition(symbol string) *DCAPosition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.positions[symbol]
}

// CancelDCA stops DCA for a symbol without closing the position.
func (m *DCAManager) CancelDCA(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pos, ok := m.positions[symbol]; ok {
		pos.Active = false
	}
}

// ResetDCA clears the DCA state for a symbol.
func (m *DCAManager) ResetDCA(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.positions, symbol)
}

// ActivePositions returns all symbols with active DCA.
func (m *DCAManager) ActivePositions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var active []string
	for sym, pos := range m.positions {
		if pos.Active {
			active = append(active, sym)
		}
	}
	return active
}

// ── DCAPosition Methods ────────────────────────────────────────

// calcTriggerPrice calculates the trigger price for a given DCA entry level.
// Level 0 is the initial entry (no trigger needed — always allow).
// Level 1+ uses the configured deviation from the previous entry price.
func (p *DCAPosition) calcTriggerPrice(level int) float64 {
	if level <= 0 {
		return 0 // initial entry has no trigger constraint
	}

	// Find the last executed entry
	var lastPrice float64
	for i := level - 1; i >= 0; i-- {
		if p.Entries[i].Executed {
			lastPrice = p.Entries[i].Price
			break
		}
	}

	if lastPrice <= 0 {
		return 0 // no previous entry to reference
	}

	deviation := p.Deviation
	if deviation <= 0 {
		deviation = 0.03
	}

	if p.Side == "LONG" {
		// Each level is deviation % lower than the previous entry
		
		return lastPrice * (1 - deviation)
	}
	// SHORT: each level is deviation % higher than the previous entry
	
	return lastPrice * (1 + deviation)
}

// UnrealizedPnL calculates the current unrealized P&L for the DCA position.
func (p *DCAPosition) UnrealizedPnL(currentPrice float64) float64 {
	if p.TotalSize <= 0 || p.AvgEntryPrice <= 0 {
		return 0
	}

	if p.Side == "LONG" {
		return (currentPrice - p.AvgEntryPrice) * p.TotalSize
	}
	return (p.AvgEntryPrice - currentPrice) * p.TotalSize
}

// Summary returns a human-readable summary of the DCA position.
func (p *DCAPosition) Summary(currentPrice float64) string {
	if p.TotalSize <= 0 {
		return fmt.Sprintf("%s %s: no entries filled", p.Symbol, p.Side)
	}

	_ = len(p.Entries) - p.FilledCount // remaining entries
	pnl := p.UnrealizedPnL(currentPrice)
	pnlPct := 0.0
	if p.AvgEntryPrice > 0 {
		if p.Side == "LONG" {
			pnlPct = (currentPrice - p.AvgEntryPrice) / p.AvgEntryPrice * 100
		} else {
			pnlPct = (p.AvgEntryPrice - currentPrice) / p.AvgEntryPrice * 100
		}
	}

	return fmt.Sprintf(
		"%s %s: avg=%.2f size=%.4f pnl=%.2f(%.1f%%) filled=%d/%d",
		p.Symbol, p.Side, p.AvgEntryPrice, p.TotalSize, pnl, pnlPct,
		p.FilledCount, len(p.Entries),
	)
}
