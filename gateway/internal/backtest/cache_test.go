package backtest

import (
	"testing"
	"time"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func ctAssert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func ctAssertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func makeTestResult() *RunResult {
	return &RunResult{
		TotalReturnPct: 25.0,
		TotalTrades:    3,
		WinningTrades:  2,
		LosingTrades:   1,
		EquityCurve: []EquityPoint{
			{Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), Equity: 10000},
			{Timestamp: time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC).UnixMilli(), Equity: 10200},
			{Timestamp: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), Equity: 10500},
			{Timestamp: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), Equity: 11200},
			{Timestamp: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), Equity: 12500},
		},
		Trades: []Position{
			{RealizedPnL: 100, EntryTime: time.Date(2024, 1, 5, 10, 0, 0, 0, time.UTC).UnixMilli(), ExitTime: time.Date(2024, 1, 5, 14, 0, 0, 0, time.UTC).UnixMilli()},
			{RealizedPnL: -50, EntryTime: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC).UnixMilli(), ExitTime: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC).UnixMilli()},
			{RealizedPnL: 200, EntryTime: time.Date(2024, 2, 10, 14, 0, 0, 0, time.UTC).UnixMilli(), ExitTime: time.Date(2024, 2, 10, 16, 0, 0, 0, time.UTC).UnixMilli()},
		},
	}
}

/* ── Cache Tests ─────────────────────────────────────────────── */

func TestCacheSetAndGet(t *testing.T) {
	c := NewResultCache(10, time.Hour)
	r := makeTestResult()
	report := GenerateReport(r, "test", "BTCUSDT")

	params := map[string]any{"fast": 12, "slow": 26}
	c.Set("sma_cross", "BTCUSDT", "1h", params, r, report)

	gotR, gotP, ok := c.Get("sma_cross", "BTCUSDT", "1h", params)
	ctAssert(t, ok, "cache hit")
	ctAssert(t, gotR != nil, "result not nil")
	ctAssert(t, gotP != nil, "report not nil")
	ctAssertEq(t, gotP.TotalTrades, 3, "total trades from RunResult")
}

func TestCacheDifferentParams(t *testing.T) {
	c := NewResultCache(10, time.Hour)
	r := makeTestResult()
	report := GenerateReport(r, "test", "BTCUSDT")

	c.Set("sma_cross", "BTCUSDT", "1h", map[string]any{"fast": 12}, r, report)

	_, _, ok := c.Get("sma_cross", "BTCUSDT", "1h", map[string]any{"fast": 20})
	ctAssert(t, !ok, "different params should miss")
}

func TestCacheTTLExpiry(t *testing.T) {
	c := NewResultCache(10, 1*time.Millisecond)
	r := makeTestResult()
	report := GenerateReport(r, "test", "BTCUSDT")

	c.Set("sma_cross", "BTCUSDT", "1h", nil, r, report)
	time.Sleep(5 * time.Millisecond)

	_, _, ok := c.Get("sma_cross", "BTCUSDT", "1h", nil)
	ctAssert(t, !ok, "expired entry should miss")
}

func TestCacheInvalidate(t *testing.T) {
	c := NewResultCache(10, time.Hour)
	r := makeTestResult()
	report := GenerateReport(r, "test", "BTCUSDT")

	c.Set("strategy", "BTCUSDT", "1h", nil, r, report)
	c.Invalidate("strategy", "BTCUSDT", "1h", nil)

	_, _, ok := c.Get("strategy", "BTCUSDT", "1h", nil)
	ctAssert(t, !ok, "invalidated should miss")
}

func TestCacheClear(t *testing.T) {
	c := NewResultCache(10, time.Hour)
	r := makeTestResult()
	report := GenerateReport(r, "test", "BTCUSDT")

	c.Set("a", "BTC", "1h", nil, r, report)
	c.Set("b", "ETH", "1h", nil, r, report)
	c.Clear()

	ctAssertEq(t, c.Size(), 0, "cleared cache empty")
}

func TestCacheSizeLimit(t *testing.T) {
	c := NewResultCache(2, time.Hour)
	r := makeTestResult()
	report := GenerateReport(r, "test", "BTCUSDT")

	c.Set("a", "BTC", "1h", nil, r, report)
	c.Set("b", "ETH", "1h", nil, r, report)
	c.Set("c", "SOL", "1h", nil, r, report)

	ctAssertEq(t, c.Size(), 2, "size capped at 2")
}

func TestCacheStats(t *testing.T) {
	c := NewResultCache(10, time.Hour)
	s := c.Stats()
	ctAssert(t, s["size"].(int) == 0, "initial size 0")
}

/* ── Breakdown Tests ─────────────────────────────────────────── */

func TestDailyBreakdown(t *testing.T) {
	r := makeTestResult()
	daily := dailyBreakdown(r)
	ctAssert(t, len(daily) > 0, "daily breakdown non-empty")
}

func TestWeeklyBreakdown(t *testing.T) {
	r := makeTestResult()
	weekly := weeklyBreakdown(r)
	ctAssert(t, len(weekly) > 0, "weekly breakdown non-empty")
}

func TestWeekdayBreakdown(t *testing.T) {
	r := makeTestResult()
	weekdays := weekdayBreakdown(r)
	ctAssertEq(t, len(weekdays), 7, "7 days of week")
}

func TestHourlyBreakdown(t *testing.T) {
	r := makeTestResult()
	hourly := hourlyBreakdown(r)
	ctAssertEq(t, len(hourly), 24, "24 hours")
}

func TestGenerateBreakdown(t *testing.T) {
	r := makeTestResult()
	br := GenerateBreakdown(r)
	ctAssert(t, br != nil, "breakdown non-nil")
	ctAssert(t, len(br.Daily) > 0, "daily")
	ctAssert(t, len(br.Weekly) > 0, "weekly")
	ctAssert(t, len(br.Monthly) > 0, "monthly")
	ctAssert(t, len(br.Yearly) > 0, "yearly")
	ctAssert(t, len(br.Weekdays) == 7, "weekdays")
	ctAssert(t, len(br.Hourly) == 24, "hourly")
}

func TestBreakdownFormatSummary(t *testing.T) {
	r := makeTestResult()
	br := GenerateBreakdown(r)
	summary := br.FormatSummary()
	ctAssert(t, len(summary) > 0, "summary non-empty")
}

func TestBreakdownEmptyResult(t *testing.T) {
	r := &RunResult{}
	br := GenerateBreakdown(r)
	ctAssert(t, br != nil, "nil-safe")
}
