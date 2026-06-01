package order

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

func dtAssertFloat(t *testing.T, got, want float64, msg string) {
	t.Helper()
	const eps = 0.001
	if got < want-eps || got > want+eps {
		t.Fatalf("%s: got %.6f, want %.6f", msg, got, want)
	}
}

/* ── SetupDCA Tests ──────────────────────────────────────────── */

func TestSetupDCALong(t *testing.T) {
	mgr := NewDCAManager()
	cfg := DefaultDCAConfig()

	pos, err := mgr.SetupDCA("BTCUSDT", "LONG", 0.1, cfg)
	dtAssert(t, err == nil, "setup should not error")
	dtAssert(t, pos.Active, "position should be active")
	dtAssert(t, pos.Side == "LONG", "side is LONG")
	dtAssert(t, len(pos.Entries) == 3, "3 entries (max_entries=3)")
	dtAssertFloat(t, pos.Entries[0].Size, 0.1, "entry 0: base*1.0")
	dtAssertFloat(t, pos.Entries[1].Size, 0.15, "entry 1: base*1.5")
	dtAssertFloat(t, pos.Entries[2].Size, 0.2, "entry 2: base*2.0")
}

func TestSetupDCAShort(t *testing.T) {
	mgr := NewDCAManager()
	cfg := DCAConfig{
		MaxEntries:     2,
		PriceDeviation: 0.05,
		StakeScale:     []float64{1.0, 2.0},
	}

	pos, err := mgr.SetupDCA("ETHUSDT", "SHORT", 0.5, cfg)
	dtAssert(t, err == nil, "setup should not error")
	dtAssert(t, pos.Side == "SHORT", "side is SHORT")
	dtAssert(t, len(pos.Entries) == 2, "2 entries")
	dtAssertFloat(t, pos.Entries[0].Size, 0.5, "entry 0: base*1.0")
	dtAssertFloat(t, pos.Entries[1].Size, 1.0, "entry 1: base*2.0")
	dtAssertFloat(t, pos.Deviation, 0.05, "deviation 5%")
}

func TestSetupDCANoScale(t *testing.T) {
	mgr := NewDCAManager()
	cfg := DCAConfig{
		MaxEntries:     3,
		PriceDeviation: 0.03,
	}

	pos, err := mgr.SetupDCA("SOLUSDT", "LONG", 1.0, cfg)
	dtAssert(t, err == nil, "setup should not error")
	// Default scale = [1.0]
	dtAssertFloat(t, pos.Entries[0].Size, 1.0, "entry 0: base*1.0")
	dtAssertFloat(t, pos.Entries[1].Size, 1.0, "entry 1: same (default)")
}

func TestSetupDCAInvalid(t *testing.T) {
	mgr := NewDCAManager()

	_, err := mgr.SetupDCA("BTCUSDT", "LONG", 0, DefaultDCAConfig())
	dtAssert(t, err != nil, "zero base size should error")

	_, err = mgr.SetupDCA("BTCUSDT", "LONG", -1, DefaultDCAConfig())
	dtAssert(t, err != nil, "negative base size should error")
}

/* ── CheckEntry Tests ────────────────────────────────────────── */

func TestCheckEntryInitial(t *testing.T) {
	mgr := NewDCAManager()
	cfg := DefaultDCAConfig()
	cfg.PriceDeviation = 0.05

	mgr.SetupDCA("BTCUSDT", "LONG", 0.1, cfg)
	// Initial entry (level 0): always triggers at any price
	entry, err := mgr.CheckEntry("BTCUSDT", 68000.0)
	dtAssert(t, err == nil, "check should not error")
	dtAssert(t, entry != nil, "initial entry should trigger")
	dtAssert(t, entry.Level == 0, "level 0")
}

func TestCheckEntryLongDCA(t *testing.T) {
	mgr := NewDCAManager()
	cfg := DCAConfig{MaxEntries: 3, PriceDeviation: 0.05, StakeScale: []float64{1.0, 1.0, 1.0}}
	mgr.SetupDCA("BTCUSDT", "LONG", 0.1, cfg)

	// Record initial entry at 70000
	mgr.RecordEntry("BTCUSDT", 70000, 0)

	// Next DCA level (level 1) triggers at 70000*(1-0.05) = 66500
	entry, err := mgr.CheckEntry("BTCUSDT", 66000)
	dtAssert(t, err == nil, "check should not error")
	dtAssert(t, entry != nil, "DCA entry should trigger at -5%")
	dtAssert(t, entry.Level == 1, "level 1")

	// At 67000 (above trigger), should NOT trigger
	entry, _ = mgr.CheckEntry("BTCUSDT", 67000)
	dtAssert(t, entry == nil, "should not trigger above DCA level")
}

func TestCheckEntryShortDCA(t *testing.T) {
	mgr := NewDCAManager()
	cfg := DCAConfig{MaxEntries: 3, PriceDeviation: 0.05, StakeScale: []float64{1.0, 1.0, 1.0}}
	mgr.SetupDCA("ETHUSDT", "SHORT", 0.5, cfg)

	// Record initial entry at 3000
	mgr.RecordEntry("ETHUSDT", 3000, 0)

	// DCA level 1 triggers at 3000*(1+0.05) = 3150
	entry, err := mgr.CheckEntry("ETHUSDT", 3200)
	dtAssert(t, err == nil, "check should not error")
	dtAssert(t, entry != nil, "DCA entry should trigger at +5% for short")
	dtAssert(t, entry.Level == 1, "level 1")
}

/* ── RecordEntry Tests ───────────────────────────────────────── */

func TestRecordEntryAvgPrice(t *testing.T) {
	mgr := NewDCAManager()
	cfg := DCAConfig{MaxEntries: 3, PriceDeviation: 0.05, StakeScale: []float64{1.0, 1.0, 1.0}}
	mgr.SetupDCA("BTCUSDT", "LONG", 1.0, cfg)

	// Entry 0: 1 BTC @ 70000
	avg, size, _ := mgr.RecordEntry("BTCUSDT", 70000, 0)
	dtAssertFloat(t, avg, 70000.0, "avg after 1 entry")
	dtAssertFloat(t, size, 1.0, "size after 1 entry")

	// Entry 1: 1 BTC @ 66500
	avg, size, _ = mgr.RecordEntry("BTCUSDT", 66500, 1)
	// Avg = (1*70000 + 1*66500) / 2 = 68250
	dtAssertFloat(t, avg, 68250.0, "avg after 2 entries")
	dtAssertFloat(t, size, 2.0, "size after 2 entries")

	// Entry 2: 1 BTC @ 63000
	avg, size, _ = mgr.RecordEntry("BTCUSDT", 63000, 2)
	// Avg = (70000 + 66500 + 63000) / 3 = 66500
	dtAssertFloat(t, avg, 66500.0, "avg after 3 entries")
	dtAssertFloat(t, size, 3.0, "size after 3 entries")
}

func TestRecordEntryDuplicate(t *testing.T) {
	mgr := NewDCAManager()
	mgr.SetupDCA("BTCUSDT", "LONG", 1.0, DefaultDCAConfig())

	mgr.RecordEntry("BTCUSDT", 70000, 0)
	_, _, err := mgr.RecordEntry("BTCUSDT", 70000, 0)
	dtAssert(t, err != nil, "duplicate entry should error")
}

func TestRecordEntryOutOfRange(t *testing.T) {
	mgr := NewDCAManager()
	mgr.SetupDCA("BTCUSDT", "LONG", 1.0, DefaultDCAConfig())

	_, _, err := mgr.RecordEntry("BTCUSDT", 70000, 5)
	dtAssert(t, err != nil, "out of range entry should error")
}

/* ── UnrealizedPnL Tests ─────────────────────────────────────── */

func TestUnrealizedPnLLong(t *testing.T) {
	mgr := NewDCAManager()
	cfg := DCAConfig{MaxEntries: 2, PriceDeviation: 0.05, StakeScale: []float64{1.0, 1.0}}
	mgr.SetupDCA("BTCUSDT", "LONG", 1.0, cfg)

	mgr.RecordEntry("BTCUSDT", 50000, 0)
	mgr.RecordEntry("BTCUSDT", 48000, 1)

	pos := mgr.GetPosition("BTCUSDT")
	// Avg = (50000+48000)/2 = 49000, size = 2
	pnl := pos.UnrealizedPnL(50000)
	dtAssertFloat(t, pnl, 2000.0, "pnl = (50000-49000)*2 = 2000")
}

func TestUnrealizedPnLShort(t *testing.T) {
	mgr := NewDCAManager()
	cfg := DCAConfig{MaxEntries: 2, PriceDeviation: 0.05, StakeScale: []float64{1.0, 1.0}}
	mgr.SetupDCA("ETHUSDT", "SHORT", 1.0, cfg)

	mgr.RecordEntry("ETHUSDT", 3000, 0)
	mgr.RecordEntry("ETHUSDT", 3150, 1)

	pos := mgr.GetPosition("ETHUSDT")
	// Avg = (3000+3150)/2 = 3075, size = 2
	pnl := pos.UnrealizedPnL(3000)
	dtAssertFloat(t, pnl, 150.0, "pnl = (3075-3000)*2 = 150")
}

/* ── Lifecycle Tests ─────────────────────────────────────────── */

func TestCancelDCA(t *testing.T) {
	mgr := NewDCAManager()
	mgr.SetupDCA("BTCUSDT", "LONG", 1.0, DefaultDCAConfig())

	mgr.CancelDCA("BTCUSDT")
	pos := mgr.GetPosition("BTCUSDT")
	dtAssert(t, !pos.Active, "should be inactive after cancel")
}

func TestResetDCA(t *testing.T) {
	mgr := NewDCAManager()
	mgr.SetupDCA("BTCUSDT", "LONG", 1.0, DefaultDCAConfig())

	mgr.ResetDCA("BTCUSDT")
	pos := mgr.GetPosition("BTCUSDT")
	dtAssert(t, pos == nil, "should be nil after reset")
}

func TestActivePositions(t *testing.T) {
	mgr := NewDCAManager()
	mgr.SetupDCA("BTCUSDT", "LONG", 0.1, DefaultDCAConfig())
	mgr.SetupDCA("ETHUSDT", "LONG", 0.5, DefaultDCAConfig())

	active := mgr.ActivePositions()
	dtAssert(t, len(active) == 2, "2 active positions")

	mgr.CancelDCA("ETHUSDT")
	active = mgr.ActivePositions()
	dtAssert(t, len(active) == 1, "1 active after cancel")
}

func TestAutoDeactivateOnFull(t *testing.T) {
	mgr := NewDCAManager()
	cfg := DCAConfig{MaxEntries: 2, PriceDeviation: 0.05, StakeScale: []float64{1.0, 1.0}}
	mgr.SetupDCA("BTCUSDT", "LONG", 0.1, cfg)

	mgr.RecordEntry("BTCUSDT", 70000, 0)
	pos := mgr.GetPosition("BTCUSDT")
	dtAssert(t, pos.Active, "still active after 1/2 entries")

	mgr.RecordEntry("BTCUSDT", 66500, 1)
	pos = mgr.GetPosition("BTCUSDT")
	dtAssert(t, !pos.Active, "inactive when all entries filled")
}

func TestSummary(t *testing.T) {
	mgr := NewDCAManager()
	mgr.SetupDCA("BTCUSDT", "LONG", 0.1, DefaultDCAConfig())
	mgr.RecordEntry("BTCUSDT", 70000, 0)

	summary := mgr.GetPosition("BTCUSDT").Summary(71000)
	dtAssert(t, len(summary) > 0, "summary non-empty")
}

/* ── DefaultConfig Tests ─────────────────────────────────────── */

func TestDefaultDCAConfig(t *testing.T) {
	cfg := DefaultDCAConfig()
	dtAssert(t, cfg.MaxEntries == 3, "default 3 entries")
	dtAssertFloat(t, cfg.PriceDeviation, 0.03, "default 3%")
	dtAssert(t, len(cfg.StakeScale) == 3, "default 3 scale values")
}

/* ── CheckEntry NoPosition Tests ─────────────────────────────── */

func TestCheckEntryNoPosition(t *testing.T) {
	mgr := NewDCAManager()
	entry, err := mgr.CheckEntry("UNKNOWN", 50000)
	dtAssert(t, err == nil, "no error")
	dtAssert(t, entry == nil, "no entry for non-existent position")
}

func TestGetPositionNoPosition(t *testing.T) {
	mgr := NewDCAManager()
	pos := mgr.GetPosition("UNKNOWN")
	dtAssert(t, pos == nil, "nil for unknown symbol")
}
