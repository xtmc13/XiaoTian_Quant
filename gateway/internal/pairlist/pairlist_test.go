package pairlist

import (
	"testing"
	"time"
)

// mockInfoProvider creates test pair info.
func mockInfoProvider(symbols []string) (map[string]*PairInfo, error) {
	infoMap := make(map[string]*PairInfo, len(symbols))
	for _, sym := range symbols {
		switch sym {
		case "BTCUSDT":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "BTC", QuoteAsset: "USDT", Price: 50000, Volume24h: 1_000_000_000, Spread: 0.01, Volatility: 2.5, PricePrecision: 2, QtyPrecision: 6, MinNotional: 10, Status: "TRADING"}
		case "ETHUSDT":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "ETH", QuoteAsset: "USDT", Price: 3000, Volume24h: 500_000_000, Spread: 0.02, Volatility: 1.8, PricePrecision: 2, QtyPrecision: 5, MinNotional: 10, Status: "TRADING"}
		case "SOLUSDT":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "SOL", QuoteAsset: "USDT", Price: 150, Volume24h: 100_000_000, Spread: 0.05, Volatility: 5.0, PricePrecision: 2, QtyPrecision: 4, MinNotional: 10, Status: "TRADING"}
		case "DOGEUSDT":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "DOGE", QuoteAsset: "USDT", Price: 0.15, Volume24h: 50_000_000, Spread: 0.10, Volatility: 3.0, PricePrecision: 4, QtyPrecision: 0, MinNotional: 10, Status: "TRADING"}
		case "SHIBUSDT":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "SHIB", QuoteAsset: "USDT", Price: 0.00001, Volume24h: 10_000_000, Spread: 0.20, Volatility: 4.0, PricePrecision: 8, QtyPrecision: 0, MinNotional: 10, Status: "TRADING"}
		case "NEWCOIN":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "NEW", QuoteAsset: "USDT", Price: 1.0, Volume24h: 0, Spread: 0.50, Volatility: 10.0, PricePrecision: 2, QtyPrecision: 2, MinNotional: 0, Status: "TRADING"}
		case "INACTIVE":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "INA", QuoteAsset: "USDT", Price: 100, Volume24h: 0, Spread: 1.0, Volatility: 0, PricePrecision: 2, QtyPrecision: 2, MinNotional: 10, Status: "BREAK"}
		default:
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: sym, QuoteAsset: "USDT", Price: 10, Volume24h: 1_000_000, Spread: 0.1, Volatility: 1.0, PricePrecision: 2, QtyPrecision: 2, MinNotional: 10, Status: "TRADING"}
		}
	}
	return infoMap, nil
}

func TestManagerFullChain(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.TTL = 1 * time.Second
	m := NewManager(cfg)

	// Set info provider
	m.SetInfoProvider(mockInfoProvider)

	// Producer: static whitelist
	m.AddProducer(NewStaticPairList([]string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT", "NEWCOIN", "INACTIVE"}))

	// Filters
	m.AddFilter(NewPriceFilter(0.1, 100000))      // Price range
	m.AddFilter(NewSpreadFilter(0.15))            // Max spread 0.15%
	m.AddFilter(NewPrecisionFilter(2, 0))         // Min 2 price decimals
	m.AddFilter(NewMaxPairsFilter(5))             // Max 5 pairs

	result, err := m.Whitelist("binance", "USDT")
	if err != nil {
		t.Fatalf("whitelist failed: %v", err)
	}

	// After SpreadFilter (0.15): SHIB (0.20), NEWCOIN (0.50), INACTIVE (1.0) excluded -> 4 left
	// After PriceFilter (0.1 - 100000): all 4 pass
	// After PrecisionFilter (min 2): all 4 pass
	// After MaxPairsFilter (5): no truncation (only 4)
	if len(result) != 4 {
		t.Fatalf("expected 4 pairs, got %d: %v", len(result), result)
	}

	// Verify SHIBUSDT is excluded
	for _, p := range result {
		if p == "SHIBUSDT" {
			t.Fatal("SHIBUSDT should be excluded by SpreadFilter")
		}
	}

	// Verify cache works
	result2, err := m.Whitelist("binance", "USDT")
	if err != nil {
		t.Fatalf("cached whitelist failed: %v", err)
	}
	if len(result2) != 4 {
		t.Fatalf("expected 4 pairs from cache, got %d", len(result2))
	}
}

func TestSpreadFilter(t *testing.T) {
	f := NewSpreadFilter(0.08)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// BTC 0.01, ETH 0.02, SOL 0.05 pass; DOGE 0.10, SHIB 0.20 fail
	if len(result) != 3 {
		t.Fatalf("expected 3 pairs, got %d: %v", len(result), result)
	}
}

func TestPriceFilter(t *testing.T) {
	f := NewPriceFilter(1.0, 10000.0)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// BTC (50000) excluded by max, SHIB (0.00001) excluded by min
	// ETH (3000), SOL (150), DOGE (0.15) — DOGE also excluded by min=1.0
	// Expected: ETH, SOL
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
}

func TestPrecisionFilter(t *testing.T) {
	f := NewPrecisionFilter(2, 0)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// All have PricePrecision >= 2
	if len(result) != 5 {
		t.Fatalf("expected 5 pairs, got %d: %v", len(result), result)
	}
}

func TestMaxPairsFilter(t *testing.T) {
	f := NewMaxPairsFilter(3)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 pairs, got %d: %v", len(result), result)
	}
}

func TestVolatilityFilter(t *testing.T) {
	f := NewVolatilityFilter(1.0, 4.0)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// BTC 2.5, ETH 1.8, DOGE 3.0, SHIB 4.0 pass (<= 4.0); SOL 5.0 fails (> 4.0)
	if len(result) != 4 {
		t.Fatalf("expected 4 pairs, got %d: %v", len(result), result)
	}
}

func TestCorrelationFilter(t *testing.T) {
	f := NewCorrelationFilter(1)
	pairs := []string{"BTCUSDT", "BTCBUSD", "ETHUSDT", "ETHBUSD", "SOLUSDT"}
	infoMap, _ := mockInfoProvider(pairs)
	// Override base assets for correlation test
	infoMap["BTCBUSD"] = &PairInfo{Symbol: "BTCBUSD", BaseAsset: "BTC", QuoteAsset: "BUSD", Price: 50000, Volume24h: 100_000_000}
	infoMap["ETHBUSD"] = &PairInfo{Symbol: "ETHBUSD", BaseAsset: "ETH", QuoteAsset: "BUSD", Price: 3000, Volume24h: 50_000_000}

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// Max 1 per base asset: BTCUSDT, ETHUSDT, SOLUSDT
	if len(result) != 3 {
		t.Fatalf("expected 3 pairs, got %d: %v", len(result), result)
	}
}

func TestShuffleFilter(t *testing.T) {
	f := NewShuffleFilter(42)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	if len(result) != 5 {
		t.Fatalf("expected 5 pairs, got %d", len(result))
	}

	// Verify order is different (with seed 42, it should be shuffled)
	sameOrder := true
	for i, p := range pairs {
		if result[i] != p {
			sameOrder = false
			break
		}
	}
	if sameOrder {
		t.Fatal("shuffle filter should change order")
	}
}

func TestPerformanceFilter(t *testing.T) {
	f := NewPerformanceFilter(3)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// Sorted by volatility descending: SOL 5.0, SHIB 4.0, DOGE 3.0, BTC 2.5, ETH 1.8
	// Top 3: SOL, SHIB, DOGE
	if len(result) != 3 {
		t.Fatalf("expected 3 pairs, got %d: %v", len(result), result)
	}
	assertEq(t, result[0], "SOLUSDT")
	assertEq(t, result[1], "SHIBUSDT")
	assertEq(t, result[2], "DOGEUSDT")
}

func TestStaticPairList(t *testing.T) {
	p := NewStaticPairList([]string{"BTCUSDT", "ETHUSDT"})
	result, err := p.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(result))
	}
	assertEq(t, result[0], "BTCUSDT")
	assertEq(t, result[1], "ETHUSDT")
}

func TestStaticPairListEmpty(t *testing.T) {
	p := NewStaticPairList([]string{})
	_, err := p.Generate("binance", "USDT")
	if err == nil {
		t.Fatal("expected error for empty pairlist")
	}
}

func TestManagerNoProducers(t *testing.T) {
	m := NewManager(DefaultManagerConfig())
	_, err := m.Whitelist("binance", "USDT")
	if err == nil {
		t.Fatal("expected error when no producers configured")
	}
}

func TestManagerFilterRemovesAll(t *testing.T) {
	m := NewManager(DefaultManagerConfig())
	m.SetInfoProvider(mockInfoProvider)
	m.AddProducer(NewStaticPairList([]string{"BTCUSDT"}))
	// Add a filter that will remove all pairs
	m.AddFilter(NewPriceFilter(100000, 200000)) // BTC is 50000, so excluded

	_, err := m.Whitelist("binance", "USDT")
	if err == nil {
		t.Fatal("expected error when filter removes all pairs")
	}
}

/* ── Helpers ─────────────────────────────────────────────────── */

func assertEq[T comparable](t *testing.T, got, want T, msg ...string) {
	t.Helper()
	if got != want {
		prefix := ""
		if len(msg) > 0 {
			prefix = msg[0] + ": "
		}
		t.Fatalf("%sgot %v, want %v", prefix, got, want)
	}
}
