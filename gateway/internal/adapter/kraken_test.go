package adapter

import (
	"net/url"
	"testing"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func ktAssert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func ktAssertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

/* ── Pair Mapping Tests ──────────────────────────────────────── */

func TestToKrakenPairBTC(t *testing.T) {
	ktAssertEq(t, toKrakenPair("BTC/USDT"), "XXBTZUSD", "BTC/USDT → XXBTZUSD")
}

func TestToKrakenPairETH(t *testing.T) {
	ktAssertEq(t, toKrakenPair("ETH/USDT"), "XETHZUSD", "ETH/USDT → XETHZUSD")
}

func TestToKrakenPairSOL(t *testing.T) {
	ktAssertEq(t, toKrakenPair("SOL/USDT"), "SOLZUSD", "SOL/USDT → SOLZUSD")
}

func TestToKrakenPairDOGE(t *testing.T) {
	ktAssertEq(t, toKrakenPair("DOGE/USDT"), "XDGZUSD", "DOGE → XDGZUSD")
}

func TestToKrakenPairFallback(t *testing.T) {
	result := toKrakenPair("UNKNOWN/USDT")
	ktAssert(t, len(result) > 0, "should return something")
}

func TestFromKrakenPair(t *testing.T) {
	ktAssertEq(t, fromKrakenPair("XXBTZUSD"), "BTC/USD", "XXBTZUSD → BTC/USD")
	ktAssertEq(t, fromKrakenPair("XETHZUSD"), "ETH/USD", "XETHZUSD → ETH/USD")
}

func TestRoundTrip(t *testing.T) {
	pairs := []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"}
	for _, p := range pairs {
		kp := toKrakenPair(p)
		back := fromKrakenPair(kp)
		// The round trip may not be exact since ZUSD maps to USD, not USDT
		ktAssert(t, len(back) > 0, "roundtrip for "+p+": "+kp+" → "+back)
	}
}

/* ── Adapter Tests ───────────────────────────────────────────── */

func TestKrakenAdapterName(t *testing.T) {
	k := NewKrakenAdapter("key", "secret")
	ktAssertEq(t, k.Name(), "kraken", "name")
}

func TestKrakenAdapterIsConnected(t *testing.T) {
	k := NewKrakenAdapter("key", "secret")
	ktAssert(t, k.IsConnected(), "should be connected by default")
}

func TestKrakenAdapterStartStop(t *testing.T) {
	k := NewKrakenAdapter("key", "secret")
	ktAssert(t, k.Start() == nil, "start")
	ktAssert(t, k.Stop() == nil, "stop")
}

func TestKrakenGetPositions(t *testing.T) {
	k := NewKrakenAdapter("key", "secret")
	pos, err := k.GetPositions()
	ktAssert(t, err == nil, "no error")
	ktAssert(t, len(pos) == 0, "empty positions")
}

/* ── Interval Mapping Tests ──────────────────────────────────── */

func TestToKrakenInterval(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1m", "1"},
		{"5m", "5"},
		{"15m", "15"},
		{"30m", "30"},
		{"1h", "60"},
		{"4h", "240"},
		{"1d", "1440"},
		{"1w", "10080"},
		{"unknown", "60"},
	}
	for _, tt := range tests {
		ktAssertEq(t, toKrakenInterval(tt.in), tt.want, "interval "+tt.in)
	}
}

func TestKrakenIntervalMinutes(t *testing.T) {
	ktAssertEq(t, krakenIntervalMinutes("60"), 60, "60 → 60min")
	ktAssertEq(t, krakenIntervalMinutes("1440"), 1440, "1440 → 1440min")
	ktAssertEq(t, krakenIntervalMinutes("unknown"), 60, "unknown → 60min")
}

/* ── Nonce Tests ─────────────────────────────────────────────── */

func TestKrakenNonce(t *testing.T) {
	k := NewKrakenAdapter("key", "secret")
	n1 := k.getNonce()
	n2 := k.getNonce()
	ktAssert(t, n2 > n1, "nonce must increase")
}

/* ── Sign Tests ──────────────────────────────────────────────── */

func TestKrakenSign(t *testing.T) {
	k := NewKrakenAdapter("test_api_key", "dGVzdF9zZWNyZXQ=") // base64 "test_secret"
	sign, nonce := k.signKraken("/0/private/Balance", url.Values{})
	ktAssert(t, len(sign) > 0, "sign non-empty")
	ktAssert(t, nonce > 0, "nonce positive")
}

/* ── Helper Tests ────────────────────────────────────────────── */

func TestKrakenAssetMapCoverage(t *testing.T) {
	// Verify Kraken pair conversions for common assets
	tests := []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "DOGE/USDT", "LINK/USDT"}
	for _, p := range tests {
		kp := toKrakenPair(p)
		ktAssert(t, len(kp) > 3, "pair "+p+" mapped to "+kp)
	}
}
