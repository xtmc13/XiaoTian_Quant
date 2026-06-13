package market

import (
	"sync"
	"time"
)

// ── Market data cache ──────────────────────────────────────────────
// Simple in-memory cache with TTL for frequently-accessed market data.
// Reduces load on exchange APIs during rapid page refreshes.

// CacheTTL configures the time-to-live for different data types.
const (
	KLinesTTL    = 15 * time.Second
	OrderBookTTL = 3 * time.Second
	SnapshotTTL  = 10 * time.Second
)

// cacheEntry wraps a cached item with its expiration time.
type cacheEntry struct {
	data      any
	expiresAt time.Time
}

// Cache provides thread-safe, TTL-based in-memory storage.
type Cache struct {
	mu   sync.RWMutex
	items map[string]cacheEntry
}

var (
	globalCache     *Cache
	globalCacheOnce sync.Once
)

// GetCache returns the singleton market data cache.
func GetCache() *Cache {
	globalCacheOnce.Do(func() {
		globalCache = &Cache{items: make(map[string]cacheEntry)}
	})
	return globalCache
}

// Get retrieves a cached item if it exists and hasn't expired.
// Returns the data and true if found, nil and false otherwise.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.items[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}

// Set stores a value with the given TTL duration.
func (c *Cache) Set(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cacheEntry{
		data:      value,
		expiresAt: time.Now().Add(ttl),
	}
}

// Delete removes an entry from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Purge removes all expired entries from the cache.
// Call periodically from a background goroutine.
func (c *Cache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.items {
		if now.After(v.expiresAt) {
			delete(c.items, k)
		}
	}
}

// Size returns the number of cached entries.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// ── Convenience helpers ────────────────────────────────────────────

// KLinesKey returns a cache key for klines.
func KLinesKey(symbol, interval string) string {
	return "klines:" + symbol + ":" + interval
}

// OrderBookKey returns a cache key for orderbook.
func OrderBookKey(symbol string) string {
	return "orderbook:" + symbol
}

// SnapshotKey returns a cache key for market snapshot.
func SnapshotKey(symbol string) string {
	return "snapshot:" + symbol
}
