package pairlist

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// Manager orchestrates pairlist generation and filtering.
type Manager struct {
	producers []IProducer
	filters   []IFilter
	mu        sync.RWMutex

	// Cached whitelist
	whitelist  []string
	lastUpdate time.Time
	ttl        time.Duration

	// Pair info provider (injected — allows exchange adapter to populate data)
	InfoProvider func(symbols []string) (map[string]*PairInfo, error)
}

// ManagerConfig configures the pairlist manager.
type ManagerConfig struct {
	TTL         time.Duration `json:"ttl"`          // cache TTL for the whitelist
	QuoteAsset  string        `json:"quote_asset"`  // e.g., "USDT"
	Exchange    string        `json:"exchange"`     // e.g., "binance"
}

// DefaultManagerConfig returns sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		TTL:       5 * time.Minute,
		QuoteAsset: "USDT",
		Exchange:   "binance",
	}
}

// NewManager creates a new pairlist manager.
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		ttl: cfg.TTL,
	}
}

// AddProducer appends a producer to the chain.
func (m *Manager) AddProducer(p IProducer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.producers = append(m.producers, p)
}

// AddFilter appends a filter to the chain.
func (m *Manager) AddFilter(f IFilter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.filters = append(m.filters, f)
}

// SetInfoProvider sets the function used to fetch pair information.
func (m *Manager) SetInfoProvider(fn func(symbols []string) (map[string]*PairInfo, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InfoProvider = fn
}

// Whitelist returns the current whitelist, refreshing if the TTL has expired.
func (m *Manager) Whitelist(exchange, quoteAsset string) ([]string, error) {
	m.mu.RLock()
	cacheValid := time.Since(m.lastUpdate) < m.ttl && len(m.whitelist) > 0
	m.mu.RUnlock()

	if cacheValid {
		m.mu.RLock()
		defer m.mu.RUnlock()
		result := make([]string, len(m.whitelist))
		copy(result, m.whitelist)
		return result, nil
	}

	return m.Refresh(exchange, quoteAsset)
}

// Refresh forces a full regeneration of the whitelist.
func (m *Manager) Refresh(exchange, quoteAsset string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.producers) == 0 {
		return nil, fmt.Errorf("no producers configured")
	}

	// Step 1: Generate initial list from the first producer
	pairs, err := m.producers[0].Generate(exchange, quoteAsset)
	if err != nil {
		return nil, fmt.Errorf("producer %s failed: %w", m.producers[0].Name(), err)
	}

	if len(pairs) == 0 {
		return nil, fmt.Errorf("producer %s returned empty whitelist", m.producers[0].Name())
	}

	// Step 2: Fetch pair info if we have filters that need it
	var infoMap map[string]*PairInfo
	if len(m.filters) > 0 && m.InfoProvider != nil {
		infoMap, err = m.InfoProvider(pairs)
		if err != nil {
			log.Printf("[pairlist] warning: info provider failed: %v, proceeding without filters", err)
			infoMap = nil
		}
	}
	if infoMap == nil {
		infoMap = make(map[string]*PairInfo)
	}

	// Step 3: Apply each filter in sequence
	for _, filter := range m.filters {
		filtered, err := filter.Filter(pairs, infoMap)
		if err != nil {
			return nil, fmt.Errorf("filter %s failed: %w", filter.Name(), err)
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("filter %s removed all pairs", filter.Name())
		}
		pairs = filtered
	}

	// Step 4: Cache and return
	m.whitelist = make([]string, len(pairs))
	copy(m.whitelist, pairs)
	m.lastUpdate = time.Now()

	log.Printf("[pairlist] generated whitelist: %d pairs (producer=%s, %d filters)",
		len(pairs), m.producers[0].Name(), len(m.filters))

	return pairs, nil
}

// Producers returns the names of registered producers.
func (m *Manager) Producers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, len(m.producers))
	for i, p := range m.producers {
		names[i] = p.Name()
	}
	return names
}

// Filters returns the names of registered filters.
func (m *Manager) Filters() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, len(m.filters))
	for i, f := range m.filters {
		names[i] = f.Name()
	}
	return names
}

// Cached returns the cached whitelist without refreshing.
func (m *Manager) Cached() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, len(m.whitelist))
	copy(result, m.whitelist)
	return result
}

// LastUpdate returns when the whitelist was last refreshed.
func (m *Manager) LastUpdate() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastUpdate
}
