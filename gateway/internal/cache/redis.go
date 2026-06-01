package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ── Cache Interface ──

// Cache is a simple key-value cache interface.
type Cache interface {
	Get(key string) (string, error)
	Set(key string, value string, ttl time.Duration) error
	Delete(key string) error
	Exists(key string) bool
}

// ── In-Memory Cache ──

type entry struct {
	value  string
	expiry time.Time
}

// MemCache is a simple in-memory cache with TTL support.
type MemCache struct {
	prefix string
	data   map[string]entry
	mu     sync.RWMutex
}

func NewMemCache(prefix string) *MemCache {
	if prefix == "" {
		prefix = "xt:"
	}
	c := &MemCache{
		prefix: prefix,
		data:   make(map[string]entry),
	}
	go c.cleanupLoop()
	return c
}

func (c *MemCache) key(k string) string {
	if strings.HasPrefix(k, c.prefix) {
		return k
	}
	return c.prefix + k
}

func (c *MemCache) Get(key string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.data[c.key(key)]
	if !ok {
		return "", fmt.Errorf("key not found")
	}
	if !e.expiry.IsZero() && time.Now().After(e.expiry) {
		return "", fmt.Errorf("key expired")
	}
	return e.value, nil
}

func (c *MemCache) Set(key string, value string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := entry{value: value}
	if ttl > 0 {
		e.expiry = time.Now().Add(ttl)
	}
	c.data[c.key(key)] = e
	return nil
}

func (c *MemCache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, c.key(key))
	return nil
}

func (c *MemCache) Exists(key string) bool {
	_, err := c.Get(key)
	return err == nil
}

func (c *MemCache) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, e := range c.data {
			if !e.expiry.IsZero() && now.After(e.expiry) {
				delete(c.data, k)
			}
		}
		c.mu.Unlock()
	}
}

// ── JSON Helpers ──

func (c *MemCache) GetJSON(key string, dest any) error {
	val, err := c.Get(key)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(val), dest)
}

func (c *MemCache) SetJSON(key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.Set(key, string(data), ttl)
}

// ── Global Cache ──

var globalCache Cache
var cacheOnce sync.Once

// GetCache returns the global cache instance.
func GetCache() Cache {
	cacheOnce.Do(func() {
		if os.Getenv("CACHE_ENABLED") == "true" && os.Getenv("REDIS_URL") != "" {
			// Redis is optional; fall back to memory cache
			globalCache = NewMemCache("xt:")
		} else {
			globalCache = NewMemCache("xt:")
		}
	})
	return globalCache
}

// InvalidatePattern removes keys matching a pattern (prefix match for MemCache).
func InvalidatePattern(pattern string) {
	c := GetCache()
	if mc, ok := c.(*MemCache); ok {
		mc.mu.Lock()
		defer mc.mu.Unlock()
		for k := range mc.data {
			if strings.HasPrefix(k, pattern) {
				delete(mc.data, k)
			}
		}
	}
}
