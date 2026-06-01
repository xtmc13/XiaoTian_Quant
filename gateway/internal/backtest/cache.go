package backtest

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ── Result Cache ───────────────────────────────────────────────

// CacheEntry holds a cached backtest result with metadata.
type CacheEntry struct {
	Result    *RunResult
	Report    *PerformanceReport
	Params    map[string]any
	CreatedAt time.Time
	HitCount  int
}

// ResultCache provides in-memory caching of backtest results.
// Reuses results for identical (strategy, symbol, interval, params) within TTL.
type ResultCache struct {
	mu       sync.RWMutex
	entries  map[string]*CacheEntry
	maxSize  int
	ttl      time.Duration
}

// NewResultCache creates a new backtest result cache.
func NewResultCache(maxSize int, ttl time.Duration) *ResultCache {
	if maxSize <= 0 {
		maxSize = 100
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &ResultCache{
		entries: make(map[string]*CacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// cacheKey generates a deterministic key from strategy + symbol + interval + params.
func cacheKey(strategy, symbol, interval string, params map[string]any) string {
	data, _ := json.Marshal(map[string]any{
		"strategy": strategy,
		"symbol":   symbol,
		"interval": interval,
		"params":   params,
	})
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:16])
}

// Get retrieves a cached result if available and not expired.
func (c *ResultCache) Get(strategy, symbol, interval string, params map[string]any) (*RunResult, *PerformanceReport, bool) {
	key := cacheKey(strategy, symbol, interval, params)

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		return nil, nil, false
	}

	if time.Since(entry.CreatedAt) > c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil, nil, false
	}

	c.mu.Lock()
	entry.HitCount++
	c.mu.Unlock()

	return entry.Result, entry.Report, true
}

// Set stores a result in the cache.
func (c *ResultCache) Set(strategy, symbol, interval string, params map[string]any, result *RunResult, report *PerformanceReport) {
	key := cacheKey(strategy, symbol, interval, params)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize {
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, e := range c.entries {
			if first || e.CreatedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = e.CreatedAt
				first = false
			}
		}
		delete(c.entries, oldestKey)
	}

	c.entries[key] = &CacheEntry{
		Result:    result,
		Report:    report,
		Params:    params,
		CreatedAt: time.Now(),
		HitCount:  0,
	}
}

// Invalidate removes a specific entry from the cache.
func (c *ResultCache) Invalidate(strategy, symbol, interval string, params map[string]any) {
	key := cacheKey(strategy, symbol, interval, params)
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Clear removes all cached entries.
func (c *ResultCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CacheEntry)
}

// Size returns the number of cached entries.
func (c *ResultCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Stats returns cache statistics.
func (c *ResultCache) Stats() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalHits := 0
	for _, e := range c.entries {
		totalHits += e.HitCount
	}

	return map[string]any{
		"size":      len(c.entries),
		"max_size":  c.maxSize,
		"ttl_hours": c.ttl.Hours(),
		"hits":      totalHits,
	}
}

// ── Global cache instance ─────────────────────────────────────

var globalCache = NewResultCache(100, 24*time.Hour)

// GetGlobalCache returns the global backtest cache.
func GetGlobalCache() *ResultCache {
	return globalCache
}
