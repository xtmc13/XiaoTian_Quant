package data

import (
	"testing"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func dtAssert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func dtAssertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func dtAssertFloat(t *testing.T, got, want float64, msg string) {
	t.Helper()
	const eps = 0.0001
	if got < want-eps || got > want+eps {
		t.Fatalf("%s: got %.6f, want %.6f", msg, got, want)
	}
}

/* ── Converter Tests ─────────────────────────────────────────── */

func TestConvert1hTo4h(t *testing.T) {
	// Create 8 hourly bars
	bars := make([]OHLCV, 8)
	for i := 0; i < 8; i++ {
		bars[i] = OHLCV{
			Symbol: "BTCUSDT", Interval: "1h",
			Time: int64(i) * 3600000,
			Open: 50000 + float64(i)*100, High: 50100 + float64(i)*100,
			Low: 49900 + float64(i)*100, Close: 50050 + float64(i)*100,
			Volume: 100,
		}
	}

	result, err := ConvertTimeframe(bars, "4h")
	dtAssert(t, err == nil, "convert should not error")
	dtAssertEq(t, len(result), 2, "8 hourly → 2 four-hourly bars")

	// First 4h bar: bars[0..3] with open=50000, high ascending from 50100→50400
	dtAssertFloat(t, result[0].Open, 50000.0, "first 4h open")
	dtAssertFloat(t, result[0].Close, 50350.0, "first 4h close")
	dtAssertFloat(t, result[0].Volume, 400.0, "first 4h volume = sum")
	dtAssertFloat(t, result[0].High, 50400.0, "first 4h high = max of all highs")

	// Second 4h bar: bars[4..7]
	dtAssertFloat(t, result[1].Open, 50400.0, "second 4h open")
	dtAssertFloat(t, result[1].Close, 50750.0, "second 4h close")
}

func TestConvertSameInterval(t *testing.T) {
	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 0, Open: 100, High: 110, Low: 90, Close: 105, Volume: 50},
	}
	result, err := ConvertTimeframe(bars, "1h")
	dtAssert(t, err == nil, "same interval should not error")
	dtAssertEq(t, len(result), 1, "1 bar remains 1")
	dtAssertFloat(t, result[0].Open, 100.0, "values preserved")
}

func TestConvertEmpty(t *testing.T) {
	result, err := ConvertTimeframe(nil, "4h")
	dtAssert(t, err == nil, "empty should not error")
	dtAssert(t, result == nil, "result nil")
}

func TestConvertUpsampleNotSupported(t *testing.T) {
	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 0, Open: 100, High: 110, Low: 90, Close: 105, Volume: 50},
	}
	_, err := ConvertTimeframe(bars, "1m")
	dtAssert(t, err != nil, "upsampling should error")
}

func TestConvertPartialBucket(t *testing.T) {
	// 3 hourly bars → 4h (incomplete bucket with only 3 bars)
	bars := make([]OHLCV, 3)
	for i := 0; i < 3; i++ {
		bars[i] = OHLCV{Symbol: "BTCUSDT", Interval: "1h", Time: int64(i) * 3600000,
			Open: 50000, High: 50100, Low: 49900, Close: 50050, Volume: 100}
	}
	result, err := ConvertTimeframe(bars, "4h")
	dtAssert(t, err == nil, "should not error")
	// 3 bars < half of 4 → skip
	// 3 bars > half of 4, so the partial bucket IS included
	dtAssertEq(t, len(result), 1, "partial bucket with >half bars included")
}

/* ── TradesToOHLCV Tests ─────────────────────────────────── */

func TestTradesToOHLCV(t *testing.T) {
	trades := []TradeTick{
		{Symbol: "BTCUSDT", Price: 50000, Quantity: 0.1, Timestamp: 0},
		{Symbol: "BTCUSDT", Price: 50100, Quantity: 0.2, Timestamp: 1000},
		{Symbol: "BTCUSDT", Price: 49900, Quantity: 0.1, Timestamp: 60000}, // next minute
	}

	result := TradesToOHLCV(trades, "1m")
	dtAssertEq(t, len(result), 2, "2 minutes of bars")

	// First minute
	dtAssertFloat(t, result[0].Open, 50000.0, "bar0 open = first trade")
	dtAssertFloat(t, result[0].High, 50100.0, "bar0 high")
	dtAssertFloat(t, result[0].Low, 50000.0, "bar0 low")
	dtAssertFloat(t, result[0].Close, 50100.0, "bar0 close = last trade")
	dtAssertFloat(t, result[0].Volume, 0.3, "bar0 volume")

	// Second minute
	dtAssertFloat(t, result[1].Open, 49900.0, "bar1 open")
	dtAssertFloat(t, result[1].Volume, 0.1, "bar1 volume")
}

func TestTradesToOHLCVEmpty(t *testing.T) {
	result := TradesToOHLCV(nil, "1m")
	dtAssert(t, result == nil, "nil trades = nil result")
}

/* ── Validator Tests ─────────────────────────────────────────── */

func TestValidatorAllPass(t *testing.T) {
	v := NewValidator(5, 0.25)
	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 0, Open: 100, High: 110, Low: 90, Close: 105, Volume: 50},
		{Symbol: "BTCUSDT", Interval: "1h", Time: 3600000, Open: 105, High: 115, Low: 100, Close: 110, Volume: 60},
		{Symbol: "BTCUSDT", Interval: "1h", Time: 7200000, Open: 110, High: 120, Low: 105, Close: 115, Volume: 55},
	}
	r := v.Validate(bars)
	dtAssertEq(t, r.IssueCount, 0, "no issues")
	dtAssertEq(t, r.PassedBars, 3, "all bars pass")
}

func TestValidatorGapDetection(t *testing.T) {
	v := NewValidator(5, 0.25)
	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 0, Open: 100, High: 110, Low: 90, Close: 105, Volume: 50},
		// Missing bar at 3600000!
		{Symbol: "BTCUSDT", Interval: "1h", Time: 7200000, Open: 110, High: 120, Low: 105, Close: 115, Volume: 55},
	}
	r := v.Validate(bars)
	dtAssert(t, r.GapsFound > 0, "gap detected")
	dtAssert(t, r.IssueCount > 0, "issue reported")
}

func TestValidatorZeroPrice(t *testing.T) {
	v := NewValidator(5, 0.25)
	bars := []OHLCV{
		{Symbol: "SCAM", Interval: "1h", Time: 0, Open: 0, High: 0, Low: 0, Close: 0, Volume: 0},
	}
	r := v.Validate(bars)
	dtAssert(t, r.IssueCount > 0, "zero price detected")
}

func TestValidatorNegativeVolume(t *testing.T) {
	v := NewValidator(5, 0.25)
	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 0, Open: 100, High: 110, Low: 90, Close: 105, Volume: -10},
	}
	r := v.Validate(bars)
	dtAssert(t, r.IssueCount > 0, "negative volume detected")
}

func TestValidatorOHLCInconsistency(t *testing.T) {
	v := NewValidator(5, 0.25)
	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 0, Open: 100, High: 90, Low: 110, Close: 105, Volume: 50},
	}
	r := v.Validate(bars)
	dtAssert(t, r.Anomalies > 0, "OHLC inconsistency detected")
}

func TestValidatorEmpty(t *testing.T) {
	v := NewValidator(5, 0.25)
	r := v.Validate(nil)
	dtAssertEq(t, r.TotalBars, 0, "empty = 0 bars")
}

func TestValidatorDefaultConfig(t *testing.T) {
	v := NewValidator(0, 0)
	dtAssert(t, v.maxGapMinutes == 5, "default gap")
	dtAssert(t, v.maxPriceMove == 0.25, "default move")
}

/* ── BarDuration Tests ───────────────────────────────────────── */

func TestBarDuration(t *testing.T) {
	dtAssertEq(t, BarDuration("1m"), "1分钟", "1m")
	dtAssertEq(t, BarDuration("1h"), "1小时", "1h")
	dtAssertEq(t, BarDuration("4h"), "4小时", "4h")
	dtAssertEq(t, BarDuration("1d"), "1天", "1d")
}

/* ── AlignToBarBoundary Tests ────────────────────────────────── */

func TestAlignToBarBoundary(t *testing.T) {
	ts := int64(123456789000) // some random timestamp
	aligned := AlignToBarBoundary(ts, "1h")
	dtAssertEq(t, aligned%3600000, int64(0), "aligned to hour boundary")
}

/* ── RoundFloat Tests ────────────────────────────────────────── */

func TestRoundFloat(t *testing.T) {
	dtAssertFloat(t, RoundFloat(3.14159, 2), 3.14, "2 places")
	dtAssertFloat(t, RoundFloat(3.14159, 4), 3.1416, "4 places")
	dtAssertFloat(t, RoundFloat(100.0, 0), 100.0, "0 places")
}

/* ── Storage Coverage Tests ──────────────────────────────────── */

func TestStorageCoverageEmpty(t *testing.T) {
	s := NewStorage()
	cov := s.GetCoverage()
	dtAssert(t, cov == nil || len(cov) == 0, "no coverage when empty")
}

func TestStorageAvailableSymbolsEmpty(t *testing.T) {
	s := NewStorage()
	symbols := s.GetAvailableSymbols()
	dtAssert(t, symbols == nil || len(symbols) == 0, "no symbols when empty")
}

/* ── Downloader Job Tests ────────────────────────────────────── */

func TestDownloaderGetJobMissing(t *testing.T) {
	s := NewStorage()
	d := NewDownloader(s)
	job := d.GetJob("nonexistent")
	dtAssert(t, job == nil, "nil for missing job")
}

func TestDownloaderStartDownloadDisabled(t *testing.T) {
	s := NewStorage()
	d := NewDownloader(s)
	_, err := d.StartDownload(DownloadConfig{})
	dtAssert(t, err != nil, "StartDownload should return error because network download is disabled")
}
