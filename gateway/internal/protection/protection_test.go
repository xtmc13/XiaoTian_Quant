package protection

import (
	"testing"
	"time"
)

/* ── Helpers ─────────────────────────────────────────────────── */

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

func makeTrade(pair string, profit float64, exitAgo time.Duration, stoploss bool) TradeRecord {
	return TradeRecord{
		Pair:        pair,
		EntryTime:   time.Now().Add(-exitAgo - time.Hour),
		ExitTime:    time.Now().Add(-exitAgo),
		ProfitPct:   profit,
		IsStoploss:  stoploss,
	}
}

/* ── CooldownPeriod Tests ────────────────────────────────────── */

func TestCooldownPeriodNoTrades(t *testing.T) {
	c := NewCooldownPeriod(time.Minute, 30*time.Second)
	lock := c.LockReason(nil, nil)
	ftAssert(t, lock == nil, "no trades should not lock")
}

func TestCooldownPeriodLosingTrade(t *testing.T) {
	c := NewCooldownPeriod(5*time.Minute, time.Minute)
	trades := []TradeRecord{
		makeTrade("BTC/USDT", -2.0, 10*time.Second, false),
	}
	lock := c.LockReason(trades, nil)
	ftAssert(t, lock != nil, "recent losing trade should lock")
	ftAssert(t, lock.Source == "CooldownPeriod", "source")
}

func TestCooldownPeriodWinningTrade(t *testing.T) {
	c := NewCooldownPeriod(5*time.Minute, time.Minute)
	trades := []TradeRecord{
		makeTrade("BTC/USDT", 3.0, 10*time.Second, false),
	}
	lock := c.LockReason(trades, nil)
	ftAssert(t, lock != nil, "recent winning trade should also lock (trade cooldown)")
}

func TestCooldownPeriodOldTrade(t *testing.T) {
	c := NewCooldownPeriod(5*time.Minute, time.Minute)
	trades := []TradeRecord{
		// Winning trade 2min ago — trade cooldown=1min, already passed
		makeTrade("BTC/USDT", 1.0, 2*time.Minute, false),
	}
	lock := c.LockReason(trades, nil)
	ftAssert(t, lock == nil, "old trade outside cooldown should not lock")
}

func TestCooldownPeriodZeroDuration(t *testing.T) {
	c := NewCooldownPeriod(0, 0)
	trades := []TradeRecord{
		makeTrade("BTC/USDT", -2.0, 10*time.Second, false),
	}
	lock := c.LockReason(trades, nil)
	ftAssert(t, lock == nil, "zero duration should not lock")
}

/* ── LowProfitPairs Tests ────────────────────────────────────── */

func TestLowProfitPairsNoTrades(t *testing.T) {
	l := NewLowProfitPairs(time.Hour, 1.0, time.Hour)
	lock := l.LockReason(nil, nil)
	ftAssert(t, lock == nil, "no trades should not lock")
}

func TestLowProfitPairsLowProfit(t *testing.T) {
	l := NewLowProfitPairs(time.Hour, 1.0, 30*time.Minute)
	trades := []TradeRecord{
		makeTrade("ETH/USDT", 0.1, 5*time.Minute, false),
		makeTrade("ETH/USDT", 0.2, 4*time.Minute, false),
		makeTrade("ETH/USDT", -0.1, 3*time.Minute, false),
	}
	lock := l.LockReason(trades, nil)
	ftAssert(t, lock != nil, "low profit pair should lock")
	ftAssert(t, lock.Pair == "ETH/USDT", "ETH locked")
}

func TestLowProfitPairsGoodProfit(t *testing.T) {
	l := NewLowProfitPairs(time.Hour, 1.0, 30*time.Minute)
	trades := []TradeRecord{
		makeTrade("BTC/USDT", 5.0, 5*time.Minute, false),
		makeTrade("BTC/USDT", 3.0, 4*time.Minute, false),
		makeTrade("BTC/USDT", 2.0, 3*time.Minute, false),
	}
	lock := l.LockReason(trades, nil)
	ftAssert(t, lock == nil, "profitable pair should not lock")
}

func TestLowProfitPairsInsufficientTrades(t *testing.T) {
	l := NewLowProfitPairs(time.Hour, 1.0, 30*time.Minute)
	trades := []TradeRecord{
		makeTrade("SOL/USDT", -0.5, 5*time.Minute, false),
		makeTrade("SOL/USDT", -0.3, 4*time.Minute, false),
		// Only 2 trades, need 3
	}
	lock := l.LockReason(trades, nil)
	ftAssert(t, lock == nil, "insufficient trades should not lock")
}

/* ── MaxDrawdownProtection Tests ─────────────────────────────── */

func TestMaxDrawdownNormal(t *testing.T) {
	m := NewMaxDrawdownProtection(10.0, time.Hour, false)
	lock := m.LockReason(nil, &PortfolioSnapshot{CurrentDrawdownPct: 5.0})
	ftAssert(t, lock == nil, "under limit should not lock")
}

func TestMaxDrawdownExceeded(t *testing.T) {
	m := NewMaxDrawdownProtection(10.0, 30*time.Minute, false)
	lock := m.LockReason(nil, &PortfolioSnapshot{CurrentDrawdownPct: 15.0})
	ftAssert(t, lock != nil, "over limit should lock")
}

func TestMaxDrawdownGlobalLock(t *testing.T) {
	m := NewMaxDrawdownProtection(10.0, time.Hour, true)
	lock := m.LockReason(nil, &PortfolioSnapshot{CurrentDrawdownPct: 12.0})
	ftAssert(t, lock != nil, "should lock")
	ftAssert(t, lock.Pair == "", "global lock has no pair")
}

func TestMaxDrawdownNilPortfolio(t *testing.T) {
	m := NewMaxDrawdownProtection(10.0, time.Hour, false)
	lock := m.LockReason(nil, nil)
	ftAssert(t, lock == nil, "nil portfolio should not lock")
}

/* ── StoplossGuard Tests ─────────────────────────────────────── */

func TestStoplossGuardNormal(t *testing.T) {
	s := NewStoplossGuard(time.Hour, 5, time.Hour)
	trades := []TradeRecord{
		makeTrade("BTC/USDT", -1.0, 10*time.Minute, true),
		makeTrade("BTC/USDT", -1.0, 8*time.Minute, true),
		// Only 2 stoplosses, need 5
	}
	lock := s.LockReason(trades, nil)
	ftAssert(t, lock == nil, "under limit should not lock")
}

func TestStoplossGuardTriggered(t *testing.T) {
	s := NewStoplossGuard(time.Hour, 3, 30*time.Minute)
	trades := []TradeRecord{
		makeTrade("DOGE/USDT", -2.0, 10*time.Minute, true),
		makeTrade("DOGE/USDT", -2.0, 8*time.Minute, true),
		makeTrade("DOGE/USDT", -1.0, 5*time.Minute, true),
	}
	lock := s.LockReason(trades, nil)
	ftAssert(t, lock != nil, "3 stoplosses should lock")
	ftAssert(t, lock.Pair == "DOGE/USDT", "DOGE locked")
}

func TestStoplossGuardOldStops(t *testing.T) {
	s := NewStoplossGuard(time.Minute, 3, 30*time.Minute)
	trades := []TradeRecord{
		makeTrade("DOGE/USDT", -2.0, 10*time.Minute, true), // older than 1min lookback
		makeTrade("DOGE/USDT", -2.0, 9*time.Minute, true),
		makeTrade("DOGE/USDT", -1.0, 5*time.Minute, true),
	}
	lock := s.LockReason(trades, nil)
	ftAssert(t, lock == nil, "stops outside lookback should not count")
}

func TestStoplossGuardOnlyCountsStoploss(t *testing.T) {
	s := NewStoplossGuard(time.Hour, 2, 30*time.Minute)
	trades := []TradeRecord{
		makeTrade("BTC/USDT", -2.0, 5*time.Minute, true),
		makeTrade("BTC/USDT", 3.0, 4*time.Minute, false),  // manual close, not stoploss
		makeTrade("BTC/USDT", -1.0, 3*time.Minute, false), // manual close
	}
	lock := s.LockReason(trades, nil)
	ftAssert(t, lock == nil, "only 1 stoploss should not trigger with limit 2")
}

/* ── PairLock Tests ──────────────────────────────────────────── */

func TestPairLockLockUnlock(t *testing.T) {
	p := NewPairLock()

	locked, _ := p.IsLocked("BTC/USDT")
	ftAssert(t, !locked, "should not be locked initially")

	p.LockPair("BTC/USDT", 100*time.Millisecond, "test")
	locked, reason := p.IsLocked("BTC/USDT")
	ftAssert(t, locked, "should be locked")
	ftAssert(t, len(reason) > 0, "should have reason")

	time.Sleep(150 * time.Millisecond)
	locked, _ = p.IsLocked("BTC/USDT")
	ftAssert(t, !locked, "should auto-unlock after expiry")
}

func TestPairLockGlobal(t *testing.T) {
	p := NewPairLock()

	p.LockGlobal(100*time.Millisecond, "global test")
	locked, _ := p.IsLocked("BTC/USDT")
	ftAssert(t, locked, "global lock affects all pairs")
	locked, _ = p.IsLocked("ETH/USDT")
	ftAssert(t, locked, "global lock affects all pairs")
}

func TestPairLockActiveLocks(t *testing.T) {
	p := NewPairLock()

	p.LockPair("BTC/USDT", time.Hour, "test1")
	p.LockPair("ETH/USDT", time.Hour, "test2")

	locks := p.ActiveLocks()
	ftAssert(t, len(locks) == 2, "2 active locks")
}

func TestPairLockActiveLocksSorted(t *testing.T) {
	p := NewPairLock()

	p.LockPair("A", 30*time.Minute, "test")
	p.LockPair("B", 10*time.Minute, "test")

	locks := p.ActiveLocks()
	ftAssert(t, len(locks) == 2, "2 locks")
	ftAssert(t, locks[0].Until.Before(locks[1].Until) || locks[0].Until.Equal(locks[1].Until),
		"locks sorted by expiry")
}

/* ── Manager Tests ───────────────────────────────────────────── */

func TestManagerNew(t *testing.T) {
	mgr := NewManager(Config{MaxTrades: 100})
	ftAssert(t, mgr != nil, "manager created")
	ftAssert(t, len(mgr.ActiveLocks()) == 0, "no locks initially")
}

func TestManagerCooldownIntegration(t *testing.T) {
	mgr := NewManager(DefaultConfig())

	// Add cooldown protection
	mgr.AddProtection(NewCooldownPeriod(5*time.Minute, 30*time.Second))

	// Record a recent trade
	mgr.RecordTrade(makeTrade("BTC/USDT", -2.0, 5*time.Second, false))

	locks := mgr.Check()
	ftAssert(t, len(locks) > 0, "should create cooldown lock")
	ftAssert(t, locks[0].Source == "CooldownPeriod", "from cooldown")
}

func TestManagerStoplossGuardIntegration(t *testing.T) {
	mgr := NewManager(DefaultConfig())

	mgr.AddProtection(NewStoplossGuard(time.Hour, 2, time.Hour))

	// Record 2 stoploss trades for same pair
	mgr.RecordTrade(makeTrade("DOGE/USDT", -2.0, 5*time.Minute, true))
	mgr.RecordTrade(makeTrade("DOGE/USDT", -1.0, 3*time.Minute, true))

	locks := mgr.Check()
	ftAssert(t, len(locks) > 0, "should lock DOGE")
	ftAssert(t, locks[0].Pair == "DOGE/USDT", "DOGE locked")

	// Verify pair is locked in manager
	locked, _ := mgr.IsPairLocked("DOGE/USDT")
	ftAssert(t, locked, "manager reports DOGE as locked")
}

func TestManagerMultipleProtections(t *testing.T) {
	mgr := NewManager(DefaultConfig())

	mgr.AddProtection(NewCooldownPeriod(5*time.Minute, time.Minute))
	mgr.AddProtection(NewStoplossGuard(time.Hour, 2, time.Hour))

	mgr.RecordTrade(makeTrade("BTC/USDT", -3.0, 5*time.Second, false))
	mgr.RecordTrade(makeTrade("ETH/USDT", -1.0, 5*time.Minute, true))
	mgr.RecordTrade(makeTrade("ETH/USDT", -1.0, 3*time.Minute, true))

	locks := mgr.Check()
	// Should have at least 2: cooldown (global) + stoploss guard (ETH)
	ftAssert(t, len(locks) >= 2, "multiple protections should fire")
}

func TestManagerOnLockCallback(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	mgr.AddProtection(NewCooldownPeriod(5*time.Minute, time.Minute))

	received := false
	mgr.OnLock = func(lock ProtectionLock) {
		received = true
	}

	mgr.RecordTrade(makeTrade("BTC/USDT", -2.0, 5*time.Second, false))
	mgr.Check()

	ftAssert(t, received, "OnLock callback should fire")
}

func TestManagerDrawdownIntegration(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	mgr.AddProtection(NewMaxDrawdownProtection(10.0, 30*time.Minute, true))

	mgr.UpdatePortfolio(PortfolioSnapshot{CurrentDrawdownPct: 15.0})

	locks := mgr.Check()
	ftAssert(t, len(locks) > 0, "should lock on high drawdown")

	locked, _ := mgr.IsPairLocked("BTC/USDT")
	ftAssert(t, locked, "global drawdown lock should affect any pair")
}

func TestManagerTradeCap(t *testing.T) {
	mgr := NewManager(Config{MaxTrades: 3})

	for i := 0; i < 10; i++ {
		mgr.RecordTrade(makeTrade("BTC/USDT", float64(i), time.Duration(10-i)*time.Second, false))
	}

	trades := mgr.Trades()
	ftAssert(t, len(trades) == 3, "trades capped at 3")
}

func TestManagerNoProtections(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	// No protections added — should return empty
	locks := mgr.Check()
	ftAssert(t, len(locks) == 0, "no protections = no locks")
}

/* ── ProtectionLock Tests ────────────────────────────────────── */

func TestProtectionLockIsActive(t *testing.T) {
	lock := &ProtectionLock{Until: time.Now().Add(time.Minute)}
	ftAssert(t, lock.IsActive(), "future lock is active")

	expired := &ProtectionLock{Until: time.Now().Add(-time.Minute)}
	ftAssert(t, !expired.IsActive(), "past lock is not active")
}

func TestScopeString(t *testing.T) {
	ftAssert(t, ScopeGlobal.String() == "GLOBAL", "global scope")
	ftAssert(t, ScopePair.String() == "PAIR", "pair scope")
}
