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
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "BTC", QuoteAsset: "USDT", Price: 50000, Volume24h: 1_000_000_000, Spread: 0.01, Volatility: 2.5, PricePrecision: 2, QtyPrecision: 6, MinNotional: 10, Status: "TRADING", AgeDays: 1000, MarketCap: 1_000_000_000_000, BidDepth: 10_000_000, AskDepth: 10_000_000, FundingRate: 0.0001, VolumeChange: 0.1, PriceChange: 2.0, Correlation: 1.0, HasPosition: false, RecentReturn: 5.0, PriceRangeRatio: 0.03}
		case "ETHUSDT":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "ETH", QuoteAsset: "USDT", Price: 3000, Volume24h: 500_000_000, Spread: 0.02, Volatility: 1.8, PricePrecision: 2, QtyPrecision: 5, MinNotional: 10, Status: "TRADING", AgeDays: 800, MarketCap: 300_000_000_000, BidDepth: 5_000_000, AskDepth: 5_000_000, FundingRate: 0.0002, VolumeChange: -0.1, PriceChange: 1.5, Correlation: 0.75, HasPosition: false, RecentReturn: 3.0, PriceRangeRatio: 0.025}
		case "SOLUSDT":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "SOL", QuoteAsset: "USDT", Price: 150, Volume24h: 100_000_000, Spread: 0.05, Volatility: 5.0, PricePrecision: 2, QtyPrecision: 4, MinNotional: 10, Status: "TRADING", AgeDays: 400, MarketCap: 50_000_000_000, BidDepth: 1_000_000, AskDepth: 1_000_000, FundingRate: 0.0005, VolumeChange: 0.5, PriceChange: 5.0, Correlation: 0.6, HasPosition: false, RecentReturn: 8.0, PriceRangeRatio: 0.06}
		case "DOGEUSDT":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "DOGE", QuoteAsset: "USDT", Price: 0.15, Volume24h: 50_000_000, Spread: 0.10, Volatility: 3.0, PricePrecision: 4, QtyPrecision: 0, MinNotional: 10, Status: "TRADING", AgeDays: 600, MarketCap: 20_000_000_000, BidDepth: 500_000, AskDepth: 500_000, FundingRate: 0.001, VolumeChange: -0.6, PriceChange: -2.0, Correlation: 0.5, HasPosition: false, RecentReturn: -1.0, PriceRangeRatio: 0.04}
		case "SHIBUSDT":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "SHIB", QuoteAsset: "USDT", Price: 0.00001, Volume24h: 10_000_000, Spread: 0.20, Volatility: 4.0, PricePrecision: 8, QtyPrecision: 0, MinNotional: 10, Status: "TRADING", AgeDays: 300, MarketCap: 5_000_000_000, BidDepth: 100_000, AskDepth: 100_000, FundingRate: 0.002, VolumeChange: 2.0, PriceChange: 10.0, Correlation: 0.45, HasPosition: false, RecentReturn: 2.0, PriceRangeRatio: 0.05}
		case "NEWCOIN":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "NEW", QuoteAsset: "USDT", Price: 1.0, Volume24h: 0, Spread: 0.50, Volatility: 10.0, PricePrecision: 2, QtyPrecision: 2, MinNotional: 0, Status: "TRADING", AgeDays: 1, MarketCap: 1_000_000, BidDepth: 1000, AskDepth: 1000, FundingRate: 0.01, VolumeChange: 10.0, PriceChange: 50.0, Correlation: 0.3, HasPosition: false, RecentReturn: -10.0, PriceRangeRatio: 0.5}
		case "INACTIVE":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "INA", QuoteAsset: "USDT", Price: 100, Volume24h: 0, Spread: 1.0, Volatility: 0, PricePrecision: 2, QtyPrecision: 2, MinNotional: 10, Status: "BREAK", AgeDays: 100, MarketCap: 0, BidDepth: 0, AskDepth: 0, FundingRate: 0, VolumeChange: 0, PriceChange: 0, Correlation: 0, HasPosition: false, RecentReturn: 0, PriceRangeRatio: 0}
		case "WBTCUSDT":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "WBTC", QuoteAsset: "USDT", Price: 50000, Volume24h: 50_000_000, Spread: 0.03, Volatility: 2.4, PricePrecision: 2, QtyPrecision: 5, MinNotional: 10, Status: "TRADING", AgeDays: 200, MarketCap: 10_000_000_000, BidDepth: 500_000, AskDepth: 500_000, FundingRate: 0.0001, VolumeChange: 0.05, PriceChange: 2.1, Correlation: 0.98, HasPosition: false, RecentReturn: 4.8, PriceRangeRatio: 0.028}
		case "BTCBUSD":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "BTC", QuoteAsset: "BUSD", Price: 50000, Volume24h: 100_000_000, Spread: 0.02, Volatility: 2.5, PricePrecision: 2, QtyPrecision: 5, MinNotional: 10, Status: "TRADING", AgeDays: 900, MarketCap: 0, BidDepth: 1_000_000, AskDepth: 1_000_000, FundingRate: 0, VolumeChange: 0, PriceChange: 2.0, Correlation: 1.0, HasPosition: false, RecentReturn: 5.0, PriceRangeRatio: 0.03}
		case "ETHBUSD":
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: "ETH", QuoteAsset: "BUSD", Price: 3000, Volume24h: 50_000_000, Spread: 0.03, Volatility: 1.8, PricePrecision: 2, QtyPrecision: 5, MinNotional: 10, Status: "TRADING", AgeDays: 800, MarketCap: 0, BidDepth: 500_000, AskDepth: 500_000, FundingRate: 0, VolumeChange: 0, PriceChange: 1.5, Correlation: 0.75, HasPosition: false, RecentReturn: 3.0, PriceRangeRatio: 0.025}
		default:
			infoMap[sym] = &PairInfo{Symbol: sym, BaseAsset: sym, QuoteAsset: "USDT", Price: 10, Volume24h: 1_000_000, Spread: 0.1, Volatility: 1.0, PricePrecision: 2, QtyPrecision: 2, MinNotional: 10, Status: "TRADING", AgeDays: 365, MarketCap: 1_000_000_000, BidDepth: 100_000, AskDepth: 100_000, FundingRate: 0.0001, VolumeChange: 0, PriceChange: 0, Correlation: 0.5, HasPosition: false, RecentReturn: 1.0, PriceRangeRatio: 0.02}
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
	f := NewCorrelationFilter(0.95)
	pairs := []string{"BTCUSDT", "BTCBUSD", "ETHUSDT", "ETHBUSD", "SOLUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// BTCUSDT (corr=1.0) kept, BTCBUSD (corr=1.0) skipped as homogeneous
	// ETHUSDT (corr=0.75) kept, ETHBUSD (corr=0.75) skipped as homogeneous
	// SOLUSDT (corr=0.6) kept
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

/* ── New Filter Tests (Step 2) ──────────────────────────────── */

func TestOffsetFilter(t *testing.T) {
	f := NewOffsetFilter(2)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
	assertEq(t, result[0], "SOLUSDT")
	assertEq(t, result[1], "DOGEUSDT")
}

func TestOffsetFilterZero(t *testing.T) {
	f := NewOffsetFilter(0)
	pairs := []string{"BTCUSDT", "ETHUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(result))
	}
}

func TestOffsetFilterTooLarge(t *testing.T) {
	f := NewOffsetFilter(10)
	pairs := []string{"BTCUSDT", "ETHUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	_, err := f.Filter(pairs, infoMap)
	if err == nil {
		t.Fatal("expected error when offset exceeds list length")
	}
}

func TestRangeFilter(t *testing.T) {
	f := NewRangeFilter(1000, 0.1, 10.0) // ref=1000, allow 100-10000
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// BTC 50000 (> 10000) excluded, ETH 3000 (in range), SOL 150 (in range)
	// DOGE 0.15 (< 100) excluded, SHIB 0.00001 (< 100) excluded
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
}

func TestRangeFilterNoReference(t *testing.T) {
	f := NewRangeFilter(0, 0.5, 2.0) // ref=0, no filtering
	pairs := []string{"BTCUSDT", "ETHUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(result))
	}
}

func TestLowProfitPairsFilter(t *testing.T) {
	f := NewLowProfitPairsFilter(2.0) // min volatility 2%
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// ETH 1.8 excluded (< 2.0); BTC 2.5, SOL 5.0, DOGE 3.0, SHIB 4.0 pass
	if len(result) != 4 {
		t.Fatalf("expected 4 pairs, got %d: %v", len(result), result)
	}
}

func TestVolumeFilter(t *testing.T) {
	f := NewVolumeFilter(100_000_001) // min 100M+1 volume (strictly above 100M)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// BTC 1B, ETH 500M pass; SOL exactly 100M fails, DOGE 50M, SHIB 10M fail
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
}

func TestVolumeFilterZero(t *testing.T) {
	f := NewVolumeFilter(0) // no filtering
	pairs := []string{"BTCUSDT", "ETHUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(result))
	}
}

func TestChangeFilter(t *testing.T) {
	f := NewChangeFilter(1.0, 4.0) // allow volatility 1% to 4%
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	// BTC 2.5, ETH 1.8, DOGE 3.0, SHIB 4.0 pass; SOL 5.0 fails (> 4.0)
	if len(result) != 4 {
		t.Fatalf("expected 4 pairs, got %d: %v", len(result), result)
	}
}

func TestRankFilter(t *testing.T) {
	f := NewRankFilter(3, 0.5, 0.5)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 pairs, got %d: %v", len(result), result)
	}

	// Top 3 by composite score (volume + volatility):
	// BTC: 1B*0.5 + 2.5*0.5 = 500M + 1.25 = very high
	// ETH: 500M*0.5 + 1.8*0.5 = 250M + 0.9
	// SOL: 100M*0.5 + 5.0*0.5 = 50M + 2.5
	// BTC should be #1
	assertEq(t, result[0], "BTCUSDT")
}

func TestRankFilterEmpty(t *testing.T) {
	f := NewRankFilter(5, 0.5, 0.5)
	result, err := f.Filter([]string{}, nil)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 pairs, got %d", len(result))
	}
}

/* ── Advanced Filter Tests ───────────────────────────────────── */

func TestAgeFilter(t *testing.T) {
	f := NewAgeFilter(7)
	pairs := []string{"BTCUSDT", "ETHUSDT", "NEWCOIN"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	// NEWCOIN has AgeDays=1, excluded
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
	assertEq(t, result[0], "BTCUSDT")
	assertEq(t, result[1], "ETHUSDT")
}

func TestDelistFilter(t *testing.T) {
	f := NewDelistFilter()
	pairs := []string{"BTCUSDT", "ETHUSDT", "INACTIVE"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	// INACTIVE has Status=BREAK, excluded
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
}

func TestRangeStabilityFilter(t *testing.T) {
	f := NewRangeStabilityFilter(0.01) // min 1%
	pairs := []string{"BTCUSDT", "ETHUSDT", "INACTIVE"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	// INACTIVE has PriceRangeRatio=0, excluded
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
}

func TestFullTradesFilter(t *testing.T) {
	f := NewFullTradesFilter()
	pairs := []string{"BTCUSDT", "ETHUSDT"}
	infoMap, _ := mockInfoProvider(pairs)
	infoMap["BTCUSDT"].HasPosition = true

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 pair, got %d: %v", len(result), result)
	}
	assertEq(t, result[0], "ETHUSDT")
}

func TestMarketCapFilter(t *testing.T) {
	f := NewMarketCapFilter(10_000_000_000, 0) // min 10B
	pairs := []string{"BTCUSDT", "ETHUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	// SHIBUSDT has MarketCap=5B, excluded
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
}

func TestVolumeChangeFilter(t *testing.T) {
	f := NewVolumeChangeFilter(-0.5, 1.0) // allow -50% to +100%
	pairs := []string{"BTCUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	// DOGEUSDT VolumeChange=-0.6 (excluded), SHIBUSDT VolumeChange=2.0 (excluded)
	if len(result) != 1 {
		t.Fatalf("expected 1 pair, got %d: %v", len(result), result)
	}
	assertEq(t, result[0], "BTCUSDT")
}

func TestPriceJumpFilter(t *testing.T) {
	f := NewPriceJumpFilter(15.0) // max 15% jump
	pairs := []string{"BTCUSDT", "SOLUSDT", "NEWCOIN"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	// NEWCOIN PriceChange=50%, excluded
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
}

func TestLiquidityFilter(t *testing.T) {
	f := NewLiquidityFilter(1_000_000, 1_000_000)
	pairs := []string{"BTCUSDT", "ETHUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	// SHIBUSDT BidDepth=100k, excluded
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
}

func TestFundingRateFilter(t *testing.T) {
	f := NewFundingRateFilter(0.001) // max 0.1%
	pairs := []string{"BTCUSDT", "DOGEUSDT", "SHIBUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	// DOGEUSDT FundingRate=0.001 (abs=0.001, not > 0.001, so passes? Wait, 0.001 > 0.001 is false. So passes.)
	// SHIBUSDT FundingRate=0.002 > 0.001, excluded
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
}

func TestLowProfitPairsFilterAdvanced(t *testing.T) {
	f := NewLowProfitPairsFilter(0.0) // min 0% profit
	pairs := []string{"BTCUSDT", "DOGEUSDT", "NEWCOIN"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	// DOGEUSDT RecentReturn=-1.0, excluded; NEWCOIN RecentReturn=-10.0, excluded
	if len(result) != 1 {
		t.Fatalf("expected 1 pair, got %d: %v", len(result), result)
	}
	assertEq(t, result[0], "BTCUSDT")
}

func TestCorrelationFilterWBTC(t *testing.T) {
	f := NewCorrelationFilter(0.95)
	pairs := []string{"BTCUSDT", "WBTCUSDT", "ETHUSDT"}
	infoMap, _ := mockInfoProvider(pairs)

	result, err := f.Filter(pairs, infoMap)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	// BTCUSDT (corr=1.0) kept, WBTCUSDT (corr=0.98) skipped as homogeneous to BTC
	// ETHUSDT (corr=0.75) kept
	if len(result) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(result), result)
	}
	assertEq(t, result[0], "BTCUSDT")
	assertEq(t, result[1], "ETHUSDT")
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
