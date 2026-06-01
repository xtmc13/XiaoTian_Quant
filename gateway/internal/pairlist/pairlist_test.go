package pairlist

import (
	"strings"
	"testing"
	"time"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func assert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func assertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func assertNoErr(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", msg, err)
	}
}

func makePairInfo(symbol string, price, volume, spread, volatility float64) *PairInfo {
	parts := strings.Split(symbol, "/")
	base := symbol
	quote := "USDT"
	if len(parts) == 2 {
		base = parts[0]
		quote = parts[1]
	} else if strings.HasSuffix(symbol, "USDT") {
		base = strings.TrimSuffix(symbol, "USDT")
		quote = "USDT"
	}
	return &PairInfo{
		Symbol:         symbol,
		BaseAsset:      base,
		QuoteAsset:     quote,
		Price:          price,
		Volume24h:      volume,
		Spread:         spread,
		Volatility:     volatility,
		PricePrecision: 2,
		QtyPrecision:   4,
		MinNotional:    10,
		Status:         "TRADING",
		Exchange:       "binance",
	}
}

func makeInfoMap(pairs []*PairInfo) map[string]*PairInfo {
	m := make(map[string]*PairInfo)
	for _, p := range pairs {
		m[p.Symbol] = p
	}
	return m
}

/* ── StaticPairList Tests ────────────────────────────────────── */

func TestStaticPairList(t *testing.T) {
	p := NewStaticPairList([]string{"BTC/USDT", "ETH/USDT", "SOL/USDT"})
	assertEq(t, p.Name(), "StaticPairList", "name")

	pairs, err := p.Generate("binance", "USDT")
	assertNoErr(t, err, "generate should succeed")
	assertEq(t, len(pairs), 3, "should return 3 pairs")
}

func TestStaticPairListEmpty(t *testing.T) {
	p := NewStaticPairList(nil)
	_, err := p.Generate("binance", "USDT")
	assert(t, err != nil, "empty list should error")
}

/* ── SpreadFilter Tests ──────────────────────────────────────── */

func TestSpreadFilterPass(t *testing.T) {
	f := NewSpreadFilter(0.5)
	pairs := []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"}
	infoMap := makeInfoMap([]*PairInfo{
		makePairInfo("BTC/USDT", 68000, 1e9, 0.01, 1.5),
		makePairInfo("ETH/USDT", 3500, 5e8, 0.05, 2.0),
		makePairInfo("SOL/USDT", 150, 2e8, 0.1, 3.0),
	})
	result, err := f.Filter(pairs, infoMap)
	assertNoErr(t, err, "filter should not error")
	assertEq(t, len(result), 3, "all pairs should pass spread < 0.5%")
}

func TestSpreadFilterReject(t *testing.T) {
	f := NewSpreadFilter(0.05)
	pairs := []string{"BTC/USDT", "ETH/USDT", "SHIT/USDT"}
	infoMap := makeInfoMap([]*PairInfo{
		makePairInfo("BTC/USDT", 68000, 1e9, 0.01, 1.5),
		makePairInfo("ETH/USDT", 3500, 5e8, 0.02, 2.0),
		makePairInfo("SHIT/USDT", 0.001, 100, 5.0, 80.0), // huge spread
	})
	result, err := f.Filter(pairs, infoMap)
	assertNoErr(t, err, "filter should not error")
	assertEq(t, len(result), 2, "SHIT should be filtered out by spread")
}

func TestSpreadFilterMissingInfo(t *testing.T) {
	f := NewSpreadFilter(0.5)
	pairs := []string{"BTC/USDT", "UNKNOWN/USDT"}
	infoMap := makeInfoMap([]*PairInfo{
		makePairInfo("BTC/USDT", 68000, 1e9, 0.01, 1.5),
	})
	// UNKNOWN has no info — should be kept (no data = no filter)
	result, err := f.Filter(pairs, infoMap)
	assertNoErr(t, err, "filter should not error")
	assertEq(t, len(result), 2, "missing info should be kept")
}

/* ── PriceFilter Tests ───────────────────────────────────────── */

func TestPriceFilterRange(t *testing.T) {
	f := NewPriceFilter(1.0, 500000.0)
	pairs := []string{"BTC/USDT", "ETH/USDT", "PENNY/USDT", "UNKNOWN/USDT"}
	infoMap := makeInfoMap([]*PairInfo{
		makePairInfo("BTC/USDT", 68000, 1e9, 0.01, 1.5),
		makePairInfo("ETH/USDT", 3500, 5e8, 0.05, 2.0),
		makePairInfo("PENNY/USDT", 0.0001, 10, 10.0, 50.0),
	})
	result, err := f.Filter(pairs, infoMap)
	assertNoErr(t, err, "filter should not error")
	assertEq(t, len(result), 3, "PENNY(too low) filtered; BTC+ETH+UNKNOWN kept")
}

func TestPriceFilterNoLimits(t *testing.T) {
	f := NewPriceFilter(0, 0)
	pairs := []string{"BTC/USDT"}
	result, err := f.Filter(pairs, nil)
	assertNoErr(t, err, "filter should not error")
	assertEq(t, len(result), 1, "no limits = keep all")
}

/* ── VolatilityFilter Tests ──────────────────────────────────── */

func TestVolatilityFilterRange(t *testing.T) {
	f := NewVolatilityFilter(0.5, 10.0)
	pairs := []string{"STABLE/USDT", "NORMAL/USDT", "WILD/USDT", "NODATA/USDT"}
	infoMap := makeInfoMap([]*PairInfo{
		makePairInfo("STABLE/USDT", 1.0, 1e8, 0.01, 0.1),
		makePairInfo("NORMAL/USDT", 100, 2e8, 0.05, 5.0),
		makePairInfo("WILD/USDT", 10, 1e7, 2.0, 50.0),
	})
	result, err := f.Filter(pairs, infoMap)
	assertNoErr(t, err, "filter should not error")
	assertEq(t, len(result), 2, "STABLE(too low)+WILD(too high) filtered; NORMAL+NODATA kept")
}

/* ── PrecisionFilter Tests ───────────────────────────────────── */

func TestPrecisionFilter(t *testing.T) {
	f := NewPrecisionFilter(2, 2)
	pairs := []string{"GOOD/USDT", "COARSE/USDT"}
	infoMap := map[string]*PairInfo{
		"GOOD/USDT":   {Symbol: "GOOD/USDT", PricePrecision: 6, QtyPrecision: 4},
		"COARSE/USDT": {Symbol: "COARSE/USDT", PricePrecision: 1, QtyPrecision: 0},
	}
	result, err := f.Filter(pairs, infoMap)
	assertNoErr(t, err, "filter should not error")
	assertEq(t, len(result), 1, "COARSE should be filtered (low precision)")
}

/* ── ShuffleFilter Tests ─────────────────────────────────────── */

func TestShuffleFilterSameElements(t *testing.T) {
	f := NewShuffleFilter(42)
	pairs := []string{"A", "B", "C", "D", "E"}
	result, err := f.Filter(pairs, nil)
	assertNoErr(t, err, "shuffle should not error")
	assertEq(t, len(result), len(pairs), "same count")

	// Check all elements still present
	seen := make(map[string]bool)
	for _, s := range result {
		seen[s] = true
	}
	for _, s := range pairs {
		assert(t, seen[s], "element "+s+" should still be present")
	}
}

/* ── MaxPairsFilter Tests ────────────────────────────────────── */

func TestMaxPairsFilter(t *testing.T) {
	f := NewMaxPairsFilter(3)
	pairs := []string{"A", "B", "C", "D", "E", "F", "G"}
	result, err := f.Filter(pairs, nil)
	assertNoErr(t, err, "filter should not error")
	assertEq(t, len(result), 3, "capped at 3")
}

func TestMaxPairsFilterMoreThanAvailable(t *testing.T) {
	f := NewMaxPairsFilter(10)
	pairs := []string{"A", "B", "C"}
	result, err := f.Filter(pairs, nil)
	assertNoErr(t, err, "filter should not error")
	assertEq(t, len(result), 3, "all kept when max > len")
}

/* ── CorrelationFilter Tests ─────────────────────────────────── */

func TestCorrelationFilter(t *testing.T) {
	f := NewCorrelationFilter(2)
	pairs := []string{"BTC/USDT", "ETH/USDT", "BTC/TUSD", "SOL/USDT", "BTC/BUSD", "ETH/TUSD"}
	infoMap := makeInfoMap([]*PairInfo{
		makePairInfo("BTC/USDT", 68000, 1e9, 0.01, 1.5),
		makePairInfo("ETH/USDT", 3500, 5e8, 0.05, 2.0),
		makePairInfo("BTC/TUSD", 68000, 1e7, 0.02, 1.5),
		makePairInfo("SOL/USDT", 150, 2e8, 0.1, 3.0),
		makePairInfo("BTC/BUSD", 68005, 5e6, 0.03, 1.5),
		makePairInfo("ETH/TUSD", 3500, 1e7, 0.06, 2.0),
	})
	result, err := f.Filter(pairs, infoMap)
	assertNoErr(t, err, "filter should not error")
	// Max 2 BTC*, 2 ETH*, 1 SOL* = 5
	assertEq(t, len(result), 5, "max 2 of each base asset")
}

/* ── Manager Integration Tests ───────────────────────────────── */

func TestManagerFullPipeline(t *testing.T) {
	mgr := NewManager(ManagerConfig{TTL: time.Minute})

	// Producer: static list
	mgr.AddProducer(NewStaticPairList([]string{
		"BTC/USDT", "ETH/USDT", "SOL/USDT", "DOGE/USDT", "PENNY/USDT",
	}))

	// Filters
	mgr.AddFilter(NewSpreadFilter(0.5))
	mgr.AddFilter(NewPriceFilter(1.0, 10000.0))
	mgr.AddFilter(NewMaxPairsFilter(4))

	// Mock info provider
	mgr.SetInfoProvider(func(symbols []string) (map[string]*PairInfo, error) {
		return makeInfoMap([]*PairInfo{
			makePairInfo("BTC/USDT", 68000, 1e9, 0.01, 1.5),
			makePairInfo("ETH/USDT", 3500, 5e8, 0.05, 2.0),
			makePairInfo("SOL/USDT", 150, 2e8, 0.1, 3.0),
			makePairInfo("DOGE/USDT", 0.15, 1e8, 0.3, 5.0),
			makePairInfo("PENNY/USDT", 0.0001, 100, 10.0, 50.0),
		}), nil
	})

	pairs, err := mgr.Refresh("binance", "USDT")
	assertNoErr(t, err, "refresh should not error")
	assert(t, len(pairs) > 0, "should have pairs")
	// PENNY should be filtered by price; the rest capped at 4
	assert(t, len(pairs) <= 4, "should be capped at 4 by MaxPairsFilter")

	for _, p := range pairs {
		assert(t, p != "PENNY/USDT", "PENNY should be filtered out")
	}
}

func TestManagerCache(t *testing.T) {
	mgr := NewManager(ManagerConfig{TTL: time.Hour})

	mgr.AddProducer(NewStaticPairList([]string{"BTC/USDT", "ETH/USDT"}))
	mgr.SetInfoProvider(func(symbols []string) (map[string]*PairInfo, error) {
		return makeInfoMap([]*PairInfo{
			makePairInfo("BTC/USDT", 68000, 1e9, 0.01, 1.5),
			makePairInfo("ETH/USDT", 3500, 5e8, 0.05, 2.0),
		}), nil
	})

	// First call should refresh
	p1, err := mgr.Whitelist("binance", "USDT")
	assertNoErr(t, err, "first whitelist should succeed")
	assertEq(t, len(p1), 2, "should have 2 pairs")

	// Second call should use cache
	p2, err := mgr.Whitelist("binance", "USDT")
	assertNoErr(t, err, "second whitelist should succeed")
	assertEq(t, len(p2), 2, "cache should return same count")
}

func TestManagerNoProducer(t *testing.T) {
	mgr := NewManager(ManagerConfig{})
	_, err := mgr.Refresh("binance", "USDT")
	assert(t, err != nil, "should error without producer")
}

func TestManagerNames(t *testing.T) {
	mgr := NewManager(ManagerConfig{})

	mgr.AddProducer(NewStaticPairList([]string{"BTC/USDT"}))
	mgr.AddFilter(NewSpreadFilter(0.5))
	mgr.AddFilter(NewPriceFilter(1.0, 1000.0))

	assertEq(t, len(mgr.Producers()), 1, "1 producer")
	assertEq(t, len(mgr.Filters()), 2, "2 filters")
}

func TestManagerTTLExpiry(t *testing.T) {
	mgr := NewManager(ManagerConfig{TTL: 1 * time.Millisecond})

	mgr.AddProducer(NewStaticPairList([]string{"BTC/USDT"}))
	mgr.SetInfoProvider(func(symbols []string) (map[string]*PairInfo, error) {
		return makeInfoMap([]*PairInfo{
			makePairInfo("BTC/USDT", 68000, 1e9, 0.01, 1.5),
		}), nil
	})

	_, err := mgr.Whitelist("binance", "USDT")
	assertNoErr(t, err, "first whitelist should succeed")

	// Wait for TTL to expire
	time.Sleep(5 * time.Millisecond)

	// Should refresh now
	_, err = mgr.Whitelist("binance", "USDT")
	assertNoErr(t, err, "refresh after TTL should succeed")
}

/* ── VolumePairList Tests ────────────────────────────────────── */

func TestVolumePairListSorting(t *testing.T) {
	mockProvider := func() ([]*PairInfo, error) {
		return []*PairInfo{
			makePairInfo("SMALL/USDT", 10, 1000, 0.1, 1.0),
			makePairInfo("LARGE/USDT", 100, 1e9, 0.01, 1.0),
			makePairInfo("MEDIUM/USDT", 50, 1e6, 0.05, 1.0),
		}, nil
	}

	v := NewVolumePairList(2, 0, mockProvider)
	pairs, err := v.Generate("binance", "USDT")
	assertNoErr(t, err, "generate should succeed")
	assertEq(t, len(pairs), 2, "top 2 by volume")
	assertEq(t, pairs[0], "LARGE/USDT", "highest volume first")
	assertEq(t, pairs[1], "MEDIUM/USDT", "second highest")
}

func TestVolumePairListMinVolume(t *testing.T) {
	mockProvider := func() ([]*PairInfo, error) {
		return []*PairInfo{
			makePairInfo("LARGE/USDT", 100, 1e9, 0.01, 1.0),
			makePairInfo("SMALL/USDT", 10, 100, 0.1, 1.0),
		}, nil
	}

	v := NewVolumePairList(10, 10000, mockProvider)
	pairs, err := v.Generate("binance", "USDT")
	assertNoErr(t, err, "generate should succeed")
	assertEq(t, len(pairs), 1, "only LARGE meets min volume")
	assertEq(t, pairs[0], "LARGE/USDT", "LARGE should be kept")
}

/* ── ProducerFunc / FilterFunc Tests ─────────────────────────── */

func TestProducerFunc(t *testing.T) {
	p := &ProducerFunc{
		name: "test-producer",
		fn: func(exchange, quote string) ([]string, error) {
			return []string{exchange + "_" + quote}, nil
		},
	}
	pairs, err := p.Generate("binance", "USDT")
	assertNoErr(t, err, "should not error")
	assertEq(t, pairs[0], "binance_USDT", "function result")
}

func TestFilterFunc(t *testing.T) {
	f := &FilterFunc{
		name: "test-filter",
		fn: func(pairs []string, _ map[string]*PairInfo) ([]string, error) {
			// Keep only pairs starting with "B"
			var result []string
			for _, p := range pairs {
				if len(p) > 0 && p[0] == 'B' {
					result = append(result, p)
				}
			}
			return result, nil
		},
	}
	result, err := f.Filter([]string{"BTC/USDT", "ETH/USDT", "BNB/USDT"}, nil)
	assertNoErr(t, err, "should not error")
	assertEq(t, len(result), 2, "only B* pairs kept")
}
