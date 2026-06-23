package adapter

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

/* ── Adapter Creation Tests ──────────────────────────────────── */

func TestBybitAdapterName(t *testing.T) {
	b := NewBybitAdapter("key", "secret", false)
	btAssertEq(t, b.Name(), "bybit", "adapter name")
}

func TestBybitAdapterTestnet(t *testing.T) {
	b := NewBybitAdapter("key", "secret", true)
	btAssert(t, b.testnet, "should be testnet")
	btAssertEq(t, b.baseURL(), BybitTestURL, "testnet URL")
}

func TestBybitAdapterProduction(t *testing.T) {
	b := NewBybitAdapter("key", "secret", false)
	btAssert(t, !b.testnet, "should be production")
	btAssertEq(t, b.baseURL(), BybitRestURL, "production URL")
}

func TestBybitAdapterIsConnected(t *testing.T) {
	b := NewBybitAdapter("key", "secret", false)
	btAssert(t, !b.IsConnected(), "should not be connected initially")
}

func TestBybitAdapterStartStop(t *testing.T) {
	b := NewBybitAdapter("key", "secret", false)

	err := b.Start()
	btAssert(t, err == nil, "start should not error")

	err = b.Stop()
	btAssert(t, err == nil, "stop should not error")
	btAssert(t, !b.IsConnected(), "should disconnect on stop")
}

/* ── Helper Function Tests ───────────────────────────────────── */

func TestToBybitInterval(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1m", "1"},
		{"5m", "5"},
		{"15m", "15"},
		{"30m", "30"},
		{"1h", "60"},
		{"4h", "240"},
		{"1d", "D"},
		{"1w", "W"},
		{"1M", "M"},
		{"unknown", "60"},
	}

	for _, tt := range tests {
		got := toBybitInterval(tt.input)
		btAssertEq(t, got, tt.expected, "interval "+tt.input)
	}
}

func TestParseBybitFloat(t *testing.T) {
	btAssertEq(t, parseBybitFloat(3.14), 3.14, "float64")
	btAssertEq(t, parseBybitFloat("2.718"), 2.718, "string")
	btAssertEq(t, parseBybitFloat(nil), 0.0, "nil")
	btAssertEq(t, parseBybitFloat(""), 0.0, "empty string")
}

func TestParseBybitInt(t *testing.T) {
	btAssertEq(t, parseBybitInt(float64(42)), int64(42), "float64")
	btAssertEq(t, parseBybitInt("123"), int64(123), "string")
	btAssertEq(t, parseBybitInt(nil), int64(0), "nil")
}

func TestClampLimit(t *testing.T) {
	btAssertEq(t, clampLimit(5, 1, 100), 5, "in range")
	btAssertEq(t, clampLimit(0, 1, 100), 1, "below min")
	btAssertEq(t, clampLimit(200, 1, 100), 100, "above max")
	btAssertEq(t, clampLimit(1, 1, 100), 1, "at min")
	btAssertEq(t, clampLimit(100, 1, 100), 100, "at max")
}

/* ── Exported Methods Tests ──────────────────────────────────── */

func TestBybitSubscribeKline(t *testing.T) {
	b := NewBybitAdapter("key", "secret", false)

	received := false
	err := b.SubscribeKline("BTCUSDT", "1h", func(bar model.Bar) {
		received = true
	})
	btAssert(t, err == nil, "subscribe should not error")
	btAssert(t, !received, "callback not called immediately (WS not connected)")
}

func TestBybitSetOnKline(t *testing.T) {
	b := NewBybitAdapter("key", "secret", false)

	var lastBar model.Bar
	b.SetOnKline(func(bar model.Bar) {
		lastBar = bar
	})

	btAssertEq(t, lastBar.Close, 0.0, "no bar received initially")
}

/* ── Position Tests ──────────────────────────────────────────── */

func TestBybitGetPositions(t *testing.T) {
	b := NewBybitAdapter("key", "secret", false)

	positions, err := b.GetPositions()
	// Without real credentials this will error; just ensure it doesn't crash.
	if err == nil {
		btAssert(t, len(positions) == 0, "no positions initially")
	}
}

/* ── Signature Format Test ───────────────────────────────────── */

func TestBybitSignFormat(t *testing.T) {
	b := NewBybitAdapter("test_api_key", "test_secret", false)

	params := map[string]any{"category": "spot", "symbol": "BTCUSDT"}
	sig := b.signBybit(1690000000000, params)

	btAssert(t, len(sig) == 64, "HMAC-SHA256 produces 64 hex chars")
	// Verify deterministic — same inputs produce same signature
	sig2 := b.signBybit(1690000000000, params)
	btAssertEq(t, sig, sig2, "deterministic signature")
}
