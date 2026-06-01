package risk

import (
	"testing"
)

/* ── Helpers for this file ───────────────────────────────────── */

func ftAssert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func ftAssertFloat(t *testing.T, got, want float64, msg string) {
	t.Helper()
	const eps = 0.001
	if got < want-eps || got > want+eps {
		t.Fatalf("%s: got %.6f, want %.6f", msg, got, want)
	}
}

/* ── Fixed Mode — LONG Tests ─────────────────────────────────── */

func TestTrailingStopFixedLongInitial(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.02, // 2%
	})
	ts.Initialize(100.0, "LONG")

	stop := ts.StopPrice()
	ftAssertFloat(t, stop, 98.0, "initial stop = 100 * 0.98 = 98")
	ftAssertFloat(t, ts.BestPrice(), 100.0, "best price = entry")
	ftAssert(t, ts.IsEnabled(), "should be enabled")
}

func TestTrailingStopFixedLongTrailsUp(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "LONG")

	// Price rises to 110
	stop, triggered := ts.Update(110.0)
	ftAssert(t, !triggered, "should not trigger on price rise")
	ftAssertFloat(t, stop, 104.5, "stop trails to 110 * 0.95 = 104.5")
	ftAssertFloat(t, ts.BestPrice(), 110.0, "best price updated")
}

func TestTrailingStopFixedLongStopsGoUpOnly(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "LONG")

	// Rise to 110: stop = 104.5
	ts.Update(110.0)
	ftAssertFloat(t, ts.StopPrice(), 104.5, "stop after rise")

	// Drop to 108: stop should NOT go down (ratchet)
	ts.Update(108.0)
	ftAssertFloat(t, ts.StopPrice(), 104.5, "stop stays at 104.5 (ratchet)")
	ftAssertFloat(t, ts.BestPrice(), 110.0, "best price still 110")
}

func TestTrailingStopFixedLongTriggered(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "LONG")

	// Rise to 110: stop = 104.5
	ts.Update(110.0)

	// Drop to 104: triggers stop (104 < 104.5)
	stop, triggered := ts.Update(104.0)
	ftAssert(t, triggered, "should trigger when price drops below stop")
	ftAssertFloat(t, stop, 104.5, "stop price unchanged")
}

func TestTrailingStopFixedLongNotTriggeredAboveStop(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "LONG")

	ts.Update(110.0)  // rise: stop = 104.5
	_, triggered := ts.Update(105.0) // still above stop
	ftAssert(t, !triggered, "105 > 104.5, should not trigger")
}

/* ── Fixed Mode — SHORT Tests ────────────────────────────────── */

func TestTrailingStopFixedShortInitial(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.02,
	})
	ts.Initialize(100.0, "SHORT")

	stop := ts.StopPrice()
	ftAssertFloat(t, stop, 102.0, "initial stop = 100 * 1.02 = 102")
}

func TestTrailingStopFixedShortTrailsDown(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "SHORT")

	// Price drops to 90
	stop, triggered := ts.Update(90.0)
	ftAssert(t, !triggered, "should not trigger on price drop")
	ftAssertFloat(t, stop, 94.5, "stop trails to 90 * 1.05 = 94.5")
	ftAssertFloat(t, ts.BestPrice(), 90.0, "best price = lowest")
}

func TestTrailingStopFixedShortStopsGoDownOnly(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "SHORT")

	ts.Update(90.0)   // stop = 94.5
	ts.Update(92.0)   // price rises but stop should NOT go up
	ftAssertFloat(t, ts.StopPrice(), 94.5, "stop stays (ratchet for short)")
}

func TestTrailingStopFixedShortTriggered(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "SHORT")

	ts.Update(90.0)   // stop = 94.5
	_, triggered := ts.Update(95.0) // rises above stop
	ftAssert(t, triggered, "should trigger when price rises above stop")
}

/* ── ATR Mode Tests ──────────────────────────────────────────── */

func TestTrailingStopATRNeedsEnoughBars(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:          TrailingATR,
		ATRPeriod:     5,
		ATRMultiplier: 2.0,
	})
	ts.Initialize(100.0, "LONG")

	// Entry price is seeded, so after 4 more updates we have 5 data points.
	// Need ATRPeriod+1 = 6 for ATR, so ATR should still be 0 after 4 updates.
	for i := 0; i < 4; i++ {
		price := 100.0 + float64(i)*2.0
		_, triggered := ts.Update(price)
		ftAssert(t, !triggered, "should not trigger with insufficient data")
	}
	ftAssertFloat(t, ts.ATRValue(), 0, "ATR not yet calculated (need 1 more bar)")
}

func TestTrailingStopATRCalculates(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:          TrailingATR,
		ATRPeriod:     3,
		ATRMultiplier: 2.0,
	})
	ts.Initialize(100.0, "LONG")

	// Feed prices: 100, 102, 104, 106 — ATR = avg(|2|, |2|, |2|) = 2.0
	prices := []float64{100, 102, 104, 106}
	for _, p := range prices {
		ts.Update(p)
	}

	atr := ts.ATRValue()
	ftAssertFloat(t, atr, 2.0, "ATR = avg true range = 2.0")

	// With ATR=2.0, multiplier=2.0, stop distance = 4.0
	// best price = 106, stop = 106 - 4 = 102
	ftAssertFloat(t, ts.StopPrice(), 102.0, "ATR stop = best - ATR*mult")
}

func TestTrailingStopATRLongTriggered(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:          TrailingATR,
		ATRPeriod:     3,
		ATRMultiplier: 1.0,
	})
	ts.Initialize(100.0, "LONG")

	// Feed rising prices: ATR = 2.0, stop distance = 2.0
	prices := []float64{100, 102, 104, 106}
	for _, p := range prices {
		ts.Update(p)
	}

	// Best = 106, ATR = 2, stop = 104
	ftAssertFloat(t, ts.StopPrice(), 104.0, "stop at 104")

	// Price drops to 103.5 → triggers
	_, triggered := ts.Update(103.5)
	ftAssert(t, triggered, "should trigger below ATR stop")
}

/* ── Lifecycle Tests ─────────────────────────────────────────── */

func TestTrailingStopDisabled(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingDisabled,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "LONG")

	ftAssert(t, !ts.IsEnabled(), "should be disabled")

	_, triggered := ts.Update(90.0)
	ftAssert(t, !triggered, "disabled stop never triggers")
	ftAssertFloat(t, ts.StopPrice(), 0, "stop price 0 when disabled")
}

func TestTrailingStopEnableDisable(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "LONG")

	ts.Disable()
	ftAssert(t, !ts.IsEnabled(), "should be disabled")
	_, triggered := ts.Update(90.0)
	ftAssert(t, !triggered, "disabled stop should not trigger")

	ts.Enable()
	ftAssert(t, ts.IsEnabled(), "should be enabled")
	_, triggered = ts.Update(94.0)
	ftAssert(t, triggered, "re-enabled stop should trigger")
}

func TestTrailingStopReset(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "LONG")
	ts.Update(110.0)
	ftAssertFloat(t, ts.StopPrice(), 104.5, "stop after rise")

	ts.Reset()
	ftAssert(t, !ts.initialized, "should not be initialized after reset")
	ftAssertFloat(t, ts.StopPrice(), 0, "stop cleared")
	ftAssertFloat(t, ts.BestPrice(), 0, "best price cleared")
}

func TestTrailingStopReinitialize(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.02,
	})

	// First position (long)
	ts.Initialize(100.0, "LONG")
	ts.Update(110.0)

	// Reset and reinitialize for short
	ts.Reset()
	ts.Initialize(200.0, "SHORT")

	ftAssertFloat(t, ts.StopPrice(), 204.0, "new short stop = 200 * 1.02")
	ftAssertFloat(t, ts.BestPrice(), 200.0, "best = entry for new position")
}

/* ── Edge Case Tests ─────────────────────────────────────────── */

func TestTrailingStopUpdateBeforeInit(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	// No Initialize called
	stop, triggered := ts.Update(100.0)
	ftAssertFloat(t, stop, 0, "stop 0 when not initialized")
	ftAssert(t, !triggered, "should not trigger when not initialized")
}

func TestTrailingStopFlatPrice(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.05,
	})
	ts.Initialize(100.0, "LONG")

	// Price stays flat at entry
	for i := 0; i < 10; i++ {
		_, triggered := ts.Update(100.0)
		ftAssert(t, !triggered, "flat price should not trigger")
	}
	ftAssertFloat(t, ts.StopPrice(), 95.0, "stop stays at initial level")
	ftAssertFloat(t, ts.BestPrice(), 100.0, "best stays at entry")
}

func TestTrailingStopLargeMove(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.10, // 10%
	})
	ts.Initialize(100.0, "LONG")

	// Massive rally
	ts.Update(200.0)
	ftAssertFloat(t, ts.StopPrice(), 180.0, "stop = 200 * 0.9 = 180")
	ftAssertFloat(t, ts.BestPrice(), 200.0, "best = 200")
}

func TestTrailingStopSmallStopDistance(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.001, // 0.1% — very tight
	})
	ts.Initialize(100.0, "LONG")

	// Small adverse move should trigger
	_, triggered := ts.Update(99.8)
	ftAssert(t, triggered, "0.1% stop should trigger on small move")
}

func TestTrailingStopStatus(t *testing.T) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingATR,
		ATRPeriod:     14,
		ATRMultiplier: 1.5,
	})
	ts.Initialize(100.0, "LONG")

	status := ts.Status()
	ftAssert(t, len(status) > 0, "status should return non-empty string")
}

/* ── Default Config Tests ────────────────────────────────────── */

func TestDefaultTrailingStopConfig(t *testing.T) {
	cfg := DefaultTrailingStopConfig()
	ftAssert(t, cfg.Mode == TrailingFixed, "default mode")
	ftAssertFloat(t, cfg.StopDistance, 0.02, "default 2%")
	ftAssert(t, cfg.ATRPeriod == 14, "default ATR period")
	ftAssertFloat(t, cfg.ATRMultiplier, 1.5, "default ATR multiplier")
}

func TestNewTrailingStopInvalidConfig(t *testing.T) {
	// All zeros — should use defaults
	ts := NewTrailingStop(TrailingStopConfig{
		Mode: TrailingFixed,
	})
	ftAssert(t, ts.IsEnabled(), "should default to enabled")
	ts.Initialize(100.0, "LONG")
	ftAssertFloat(t, ts.StopPrice(), 98.0, "default 2% stop")
}

/* ── Benchmark ────────────────────────────────────────────────── */

func BenchmarkTrailingStopUpdate(b *testing.B) {
	ts := NewTrailingStop(TrailingStopConfig{
		Mode:         TrailingFixed,
		StopDistance: 0.02,
	})
	ts.Initialize(100.0, "LONG")

	prices := make([]float64, b.N)
	for i := 0; i < b.N; i++ {
		prices[i] = 100.0 + float64(i%100)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts.Update(prices[i])
	}
}
