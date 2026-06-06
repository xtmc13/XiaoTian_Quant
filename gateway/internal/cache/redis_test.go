package cache

import (
	"testing"
	"time"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func assertEq(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func assertTrue(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

/* ── MemCache Tests ──────────────────────────────────────────── */

func TestMemCacheSetGet(t *testing.T) {
	c := NewMemCache("test:")

	err := c.Set("key1", "value1", 0)
	assertTrue(t, err == nil, "set should succeed")

	val, err := c.Get("key1")
	assertTrue(t, err == nil, "get should succeed")
	assertEq(t, val, "value1")
}

func TestMemCacheKeyNotFound(t *testing.T) {
	c := NewMemCache("test:")

	_, err := c.Get("nonexistent")
	assertTrue(t, err != nil, "should return error for missing key")
}

func TestMemCacheDelete(t *testing.T) {
	c := NewMemCache("test:")

	_ = c.Set("key1", "value1", 0)
	err := c.Delete("key1")
	assertTrue(t, err == nil, "delete should succeed")

	_, err = c.Get("key1")
	assertTrue(t, err != nil, "should return error after delete")
}

func TestMemCacheExists(t *testing.T) {
	c := NewMemCache("test:")

	assertTrue(t, !c.Exists("key1"), "should not exist before set")

	_ = c.Set("key1", "value1", 0)
	assertTrue(t, c.Exists("key1"), "should exist after set")

	_ = c.Delete("key1")
	assertTrue(t, !c.Exists("key1"), "should not exist after delete")
}

func TestMemCacheTTL(t *testing.T) {
	c := NewMemCache("test:")

	_ = c.Set("key1", "value1", 50*time.Millisecond)

	val, err := c.Get("key1")
	assertTrue(t, err == nil, "should get value before expiry")
	assertEq(t, val, "value1")

	time.Sleep(100 * time.Millisecond)

	_, err = c.Get("key1")
	assertTrue(t, err != nil, "should return error after expiry")
}

func TestMemCachePrefix(t *testing.T) {
	c := NewMemCache("xt:")

	_ = c.Set("foo", "bar", 0)

	// Should be accessible with or without prefix
	val, err := c.Get("foo")
	assertTrue(t, err == nil, "get without prefix should work")
	assertEq(t, val, "bar")

	val, err = c.Get("xt:foo")
	assertTrue(t, err == nil, "get with prefix should work")
	assertEq(t, val, "bar")
}

func TestMemCacheOverwrite(t *testing.T) {
	c := NewMemCache("test:")

	_ = c.Set("key1", "value1", 0)
	_ = c.Set("key1", "value2", 0)

	val, err := c.Get("key1")
	assertTrue(t, err == nil, "get should succeed")
	assertEq(t, val, "value2")
}

func TestMemCacheJSONValue(t *testing.T) {
	c := NewMemCache("test:")

	jsonVal := `{"symbol":"BTCUSDT","price":50000.5}`
	_ = c.Set("tick", jsonVal, 0)

	val, err := c.Get("tick")
	assertTrue(t, err == nil, "get should succeed")
	assertEq(t, val, jsonVal)
}
