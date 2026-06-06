package cache

import (
	"os"
	"sync"
	"testing"
	"time"
)

// TestRedisCacheInterface verifies that RedisCache implements the Cache interface.
// This test is skipped unless REDIS_URL is set.
func TestRedisCacheInterface(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping Redis tests")
	}

	// Ensure we use Redis for this test
	os.Setenv("CACHE_ENABLED", "true")
	defer os.Unsetenv("CACHE_ENABLED")

	c, err := NewRedisCache("test:")
	if err != nil {
		t.Fatalf("NewRedisCache failed: %v", err)
	}
	defer c.Close()

	// Clean up any leftover keys
	defer c.client.FlushDB(c.ctx)

	// Set
	err = c.Set("key1", "value1", 0)
	assertTrue(t, err == nil, "set should succeed")

	// Get
	val, err := c.Get("key1")
	assertTrue(t, err == nil, "get should succeed")
	assertEq(t, val, "value1")

	// Exists
	assertTrue(t, c.Exists("key1"), "should exist")
	assertTrue(t, !c.Exists("nonexistent"), "should not exist")

	// Delete
	err = c.Delete("key1")
	assertTrue(t, err == nil, "delete should succeed")
	_, err = c.Get("key1")
	assertTrue(t, err != nil, "should error after delete")

	// TTL
	err = c.Set("ttlkey", "ttlval", 50*time.Millisecond)
	assertTrue(t, err == nil, "set with ttl should succeed")
	val, err = c.Get("ttlkey")
	assertTrue(t, err == nil, "should get before expiry")
	assertEq(t, val, "ttlval")

	time.Sleep(100 * time.Millisecond)
	_, err = c.Get("ttlkey")
	assertTrue(t, err != nil, "should expire")

	// Prefix handling
	err = c.Set("foo", "bar", 0)
	assertTrue(t, err == nil, "set without prefix should work")
	val, err = c.Get("foo")
	assertTrue(t, err == nil, "get without prefix should work")
	assertEq(t, val, "bar")

	val, err = c.Get("test:foo")
	assertTrue(t, err == nil, "get with prefix should work")
	assertEq(t, val, "bar")

	// JSON helpers
	type testStruct struct {
		Symbol string  `json:"symbol"`
		Price  float64 `json:"price"`
	}
	obj := testStruct{Symbol: "BTCUSDT", Price: 50000.5}
	err = c.SetJSON("jsonkey", obj, 0)
	assertTrue(t, err == nil, "setjson should succeed")

	var dest testStruct
	err = c.GetJSON("jsonkey", &dest)
	assertTrue(t, err == nil, "getjson should succeed")
	assertTrue(t, dest.Symbol == "BTCUSDT", "symbol should match")
	assertTrue(t, dest.Price == 50000.5, "price should match")

	// Health
	health := c.Health()
	assertTrue(t, health["connected"] == true, "should be connected")
}

// TestRedisCacheFallback verifies that GetCache falls back to MemCache when Redis is unavailable.
func TestRedisCacheFallback(t *testing.T) {
	// Save original env
	origURL := os.Getenv("REDIS_URL")
	origEnabled := os.Getenv("CACHE_ENABLED")
	defer func() {
		if origURL != "" {
			os.Setenv("REDIS_URL", origURL)
		} else {
			os.Unsetenv("REDIS_URL")
		}
		if origEnabled != "" {
			os.Setenv("CACHE_ENABLED", origEnabled)
		} else {
			os.Unsetenv("CACHE_ENABLED")
		}
	}()

	// Point to a non-existent Redis
	os.Setenv("REDIS_URL", "redis://localhost:19999/0")
	os.Setenv("CACHE_ENABLED", "true")

	// Reset global cache to force re-initialization
	globalCache = nil
	cacheOnce = sync.Once{}

	cache := GetCache()
	assertTrue(t, cache != nil, "should return a cache")

	// Should be MemCache fallback
	_, isMem := cache.(*MemCache)
	assertTrue(t, isMem, "should fall back to MemCache when Redis is unreachable")
}

// TestRedisCacheInvalidURL verifies error handling for invalid URLs.
func TestRedisCacheInvalidURL(t *testing.T) {
	os.Setenv("REDIS_URL", "not-a-valid-url::://")
	defer os.Unsetenv("REDIS_URL")

	_, err := NewRedisCache("test:")
	assertTrue(t, err != nil, "should error on invalid URL")
}
