package risk

import (
	"fmt"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func assert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func assertNoErr(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", msg, err)
	}
}

func assertErr(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error but got nil", msg)
	}
}

func defaultCtx() *Context {
	return &Context{
		Symbol:         "BTCUSDT",
		CurrentPrice:   68000.0,
		TotalEquity:    100000.0,
		OrderPrice:     68000.0,
		OrderQuantity:  0.1,
		BidPrice:       67990.0,
		AskPrice:       68010.0,
		OrderSide:      model.SideBuy,
	}
}

/* ── PriceSanity Tests ───────────────────────────────────────── */

func TestPriceSanityNormal(t *testing.T) {
	check := PriceSanity(5.0)
	ctx := defaultCtx()
	assertNoErr(t, check(ctx), "normal price should pass")
}

func TestPriceSanityDeviation(t *testing.T) {
	check := PriceSanity(5.0)
	ctx := defaultCtx()
	ctx.OrderPrice = 78000.0 // ~14.7% above market
	assertErr(t, check(ctx), "large deviation should fail")
}

func TestPriceSanityZeroPrice(t *testing.T) {
	check := PriceSanity(5.0)
	ctx := defaultCtx()
	ctx.CurrentPrice = 0
	assertNoErr(t, check(ctx), "zero current price should skip check")
}

func TestPriceSanityDefaultMax(t *testing.T) {
	check := PriceSanity(0) // should default to 5%
	ctx := defaultCtx()
	assertNoErr(t, check(ctx), "default max should work")
}

/* ── OrderSize Tests ─────────────────────────────────────────── */

func TestOrderSizeNormal(t *testing.T) {
	check := OrderSize(100000)
	ctx := defaultCtx()
	ctx.OrderPrice = 50000.0
	ctx.OrderQuantity = 1.0 // 50000 USDT
	assertNoErr(t, check(ctx), "normal order should pass")
}

func TestOrderSizeExceeded(t *testing.T) {
	check := OrderSize(10000)
	ctx := defaultCtx()
	ctx.OrderPrice = 50000.0
	ctx.OrderQuantity = 0.5 // 25000 USDT > 10000
	assertErr(t, check(ctx), "oversized order should fail")
}

func TestOrderSizeDefaultMax(t *testing.T) {
	check := OrderSize(0) // defaults to 100000
	ctx := defaultCtx()
	ctx.OrderPrice = 50000.0
	ctx.OrderQuantity = 2.0 // 100000 exactly
	assertNoErr(t, check(ctx), "exactly at default max should pass")
}

/* ── DailyLimit Tests ────────────────────────────────────────── */

func TestDailyLimitNormal(t *testing.T) {
	check := DailyLimit(100)
	ctx := defaultCtx()
	ctx.DailyOrderCount = 50
	assertNoErr(t, check(ctx), "under limit should pass")
}

func TestDailyLimitReached(t *testing.T) {
	check := DailyLimit(100)
	ctx := defaultCtx()
	ctx.DailyOrderCount = 100
	assertErr(t, check(ctx), "at limit should fail")
}

func TestDailyLimitExceeded(t *testing.T) {
	check := DailyLimit(100)
	ctx := defaultCtx()
	ctx.DailyOrderCount = 150
	assertErr(t, check(ctx), "over limit should fail")
}

/* ── ConcurrentOrders Tests ──────────────────────────────────── */

func TestConcurrentOrdersNormal(t *testing.T) {
	check := ConcurrentOrders(5)
	ctx := defaultCtx()
	ctx.PositionCount = 3
	assertNoErr(t, check(ctx), "under limit should pass")
}

func TestConcurrentOrdersExceeded(t *testing.T) {
	check := ConcurrentOrders(5)
	ctx := defaultCtx()
	ctx.PositionCount = 5
	assertErr(t, check(ctx), "at limit should fail")
}

/* ── PositionLimit Tests ─────────────────────────────────────── */

func TestPositionLimitNormal(t *testing.T) {
	check := PositionLimit(50.0)
	ctx := defaultCtx()
	ctx.OrderPrice = 10000.0
	ctx.OrderQuantity = 1.0 // 10% exposure
	ctx.TotalEquity = 100000.0
	assertNoErr(t, check(ctx), "10% exposure should pass")
}

func TestPositionLimitExceeded(t *testing.T) {
	check := PositionLimit(50.0)
	ctx := defaultCtx()
	ctx.OrderPrice = 60000.0
	ctx.OrderQuantity = 1.0 // 60% exposure
	ctx.TotalEquity = 100000.0
	assertErr(t, check(ctx), "60% exposure should fail")
}

func TestPositionLimitZeroEquity(t *testing.T) {
	check := PositionLimit(50.0)
	ctx := defaultCtx()
	ctx.TotalEquity = 0
	assertNoErr(t, check(ctx), "zero equity should skip")
}

/* ── NetExposure Tests ───────────────────────────────────────── */

func TestNetExposureNormal(t *testing.T) {
	check := NetExposure(80.0)
	ctx := defaultCtx()
	ctx.NetExposure = 50.0
	assertNoErr(t, check(ctx), "under exposure limit should pass")
}

func TestNetExposureExceeded(t *testing.T) {
	check := NetExposure(80.0)
	ctx := defaultCtx()
	ctx.NetExposure = 85.0
	assertErr(t, check(ctx), "over exposure should fail")
}

/* ── MaxDrawdown Tests ───────────────────────────────────────── */

func TestMaxDrawdownNormal(t *testing.T) {
	check := MaxDrawdown(10.0)
	ctx := defaultCtx()
	ctx.MaxDrawdownPct = 5.0
	assertNoErr(t, check(ctx), "under drawdown limit should pass")
}

func TestMaxDrawdownExceeded(t *testing.T) {
	check := MaxDrawdown(10.0)
	ctx := defaultCtx()
	ctx.MaxDrawdownPct = 12.0
	assertErr(t, check(ctx), "over drawdown should fail")
}

/* ── ConsecutiveLossesCheck Tests ────────────────────────────── */

func TestConsecutiveLossesNormal(t *testing.T) {
	check := ConsecutiveLossesCheck(5)
	ctx := defaultCtx()
	ctx.ConsecutiveLosses = 2
	assertNoErr(t, check(ctx), "under limit should pass")
}

func TestConsecutiveLossesExceeded(t *testing.T) {
	check := ConsecutiveLossesCheck(5)
	ctx := defaultCtx()
	ctx.ConsecutiveLosses = 6
	assertErr(t, check(ctx), "over limit should fail")
}

/* ── FundingRate Tests ───────────────────────────────────────── */

func TestFundingRateNormal(t *testing.T) {
	check := FundingRate(0.375)
	ctx := defaultCtx()
	ctx.FundingRate = 0.01
	assertNoErr(t, check(ctx), "normal funding rate should pass")
}

func TestFundingRateExceeded(t *testing.T) {
	check := FundingRate(0.375)
	ctx := defaultCtx()
	ctx.FundingRate = -0.5 // negative high rate
	assertErr(t, check(ctx), "high funding rate should fail")
}

/* ── MarginRatio Tests ───────────────────────────────────────── */

func TestMarginRatioNormal(t *testing.T) {
	check := MarginRatio(150.0)
	ctx := defaultCtx()
	ctx.MarginRatio = 200.0
	assertNoErr(t, check(ctx), "healthy margin should pass")
}

func TestMarginRatioLow(t *testing.T) {
	check := MarginRatio(150.0)
	ctx := defaultCtx()
	ctx.MarginRatio = 110.0
	assertErr(t, check(ctx), "low margin should fail")
}

func TestMarginRatioZero(t *testing.T) {
	check := MarginRatio(150.0)
	ctx := defaultCtx()
	ctx.MarginRatio = 0 // not trading margin
	assertNoErr(t, check(ctx), "zero margin (spot) should skip")
}

/* ── Blacklist Tests ─────────────────────────────────────────── */

func TestBlacklistAllowed(t *testing.T) {
	check := Blacklist(map[string]bool{"BANNED": true})
	ctx := defaultCtx()
	ctx.Symbol = "BTCUSDT"
	assertNoErr(t, check(ctx), "non-blacklisted should pass")
}

func TestBlacklistBlocked(t *testing.T) {
	check := Blacklist(map[string]bool{"BADCOIN": true})
	ctx := defaultCtx()
	ctx.Symbol = "BADCOIN"
	assertErr(t, check(ctx), "blacklisted should fail")
}

/* ── Volatility Tests ────────────────────────────────────────── */

func TestVolatilityNormal(t *testing.T) {
	check := Volatility(2.0)
	ctx := defaultCtx()
	ctx.Volatility = 1.0
	assertNoErr(t, check(ctx), "normal volatility should pass")
}

func TestVolatilityHigh(t *testing.T) {
	check := Volatility(2.0)
	ctx := defaultCtx()
	ctx.Volatility = 3.5
	assertErr(t, check(ctx), "high volatility should fail")
}

/* ── PriceSpike Tests ────────────────────────────────────────── */

func TestPriceSpikeNormal(t *testing.T) {
	check := PriceSpike(3.0)
	ctx := defaultCtx()
	ctx.BidPrice = 67900.0
	ctx.AskPrice = 68000.0
	assertNoErr(t, check(ctx), "normal spread should pass")
}

func TestPriceSpikeWide(t *testing.T) {
	check := PriceSpike(3.0)
	ctx := defaultCtx()
	ctx.BidPrice = 60000.0
	ctx.AskPrice = 68000.0 // ~13% spread
	assertErr(t, check(ctx), "wide spread should fail")
}

func TestPriceSpikeZeroPrices(t *testing.T) {
	check := PriceSpike(3.0)
	ctx := defaultCtx()
	ctx.BidPrice = 0
	ctx.AskPrice = 0
	assertNoErr(t, check(ctx), "zero prices should skip")
}

/* ── TimeWindow Tests ────────────────────────────────────────── */

func TestTimeWindowInWindow(t *testing.T) {
	hour := time.Now().Hour()
	check := TimeWindow(hour-1, hour+2)
	ctx := defaultCtx()
	assertNoErr(t, check(ctx), "current time should be in window")
}

func TestTimeWindowOutOfWindow(t *testing.T) {
	hour := time.Now().Hour()
	// Create a window that excludes the current hour
	start := ((hour + 2) % 24)
	end := ((hour + 4) % 24)
	check := TimeWindow(start, end)
	ctx := defaultCtx()
	assertErr(t, check(ctx), "current time should be out of window")
}

/* ── Circuit Breaker Tests ───────────────────────────────────── */

func TestCircuitBreakerClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)
	assert(t, cb.State() == CircuitClosed, "initial state should be closed")
	assert(t, cb.Allow(), "closed breaker should allow")
}

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	assert(t, cb.State() == CircuitOpen, fmt.Sprintf("breaker should be open after 3 failures, got %s", cb.State()))
	assert(t, !cb.Allow(), "open breaker should deny")
}

func TestCircuitBreakerResets(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	assert(t, cb.State() == CircuitOpen, "should be open")

	time.Sleep(1100 * time.Millisecond)

	// Allow transitions to half-open, then needs success to close
	assert(t, cb.Allow(), "should allow after timeout (half-open)")
	cb.RecordSuccess()
	assert(t, cb.State() == CircuitClosed, "should be closed after success in half-open")
}

func TestCircuitBreakerHalfOpenFailsAgain(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	time.Sleep(1100 * time.Millisecond)
	assert(t, cb.Allow(), "should allow after timeout")
	cb.RecordFailure() // fail in half-open
	assert(t, cb.State() == CircuitOpen, "should re-open after half-open failure")
}

func TestCircuitBreakerTripCount(t *testing.T) {
	cb := NewCircuitBreaker(2, time.Second)
	for i := 0; i < 2; i++ {
		cb.RecordFailure()
	}
	assert(t, cb.TripCount() == 1, "one trip")
}

func TestCircuitBreakerReset(t *testing.T) {
	cb := NewCircuitBreaker(2, time.Minute)
	for i := 0; i < 2; i++ {
		cb.RecordFailure()
	}
	cb.Reset()
	assert(t, cb.State() == CircuitClosed, "reset should close breaker")
	assert(t, cb.TripCount() == 0, "reset should clear trip count")
}

func TestCircuitBreakerSuccessKeepsClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)
	cb.RecordSuccess()
	assert(t, cb.State() == CircuitClosed, "success in closed state stays closed")
}

func TestCircuitBreakerDefaultThreshold(t *testing.T) {
	cb := NewCircuitBreaker(0, 0)
	for i := 0; i < 5; i++ {
		cb.RecordFailure()
	}
	assert(t, cb.State() == CircuitOpen, "default threshold (5) should trip")
}

/* ── Risk Manager Tests ──────────────────────────────────────── */

func TestManagerPassAllChecks(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := defaultCtx()

	err := mgr.Check(ctx)
	assertNoErr(t, err, "all normal checks should pass")
}

func TestManagerFailsOnBadPrice(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := defaultCtx()
	ctx.OrderPrice = 100000.0 // way above market

	err := mgr.Check(ctx)
	assertErr(t, err, "should fail on price deviation")
}

func TestManagerBlacklistManagement(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())

	mgr.AddBlacklist("SCAMCOIN")
	ctx := defaultCtx()
	ctx.Symbol = "SCAMCOIN"

	err := mgr.Check(ctx)
	assertErr(t, err, "should fail on blacklisted symbol")

	mgr.RemoveBlacklist("SCAMCOIN")
	err = mgr.Check(ctx)
	assertNoErr(t, err, "should pass after removing from blacklist")
}

func TestManagerConsecutiveLosses(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())

	// Record 6 losses — should block trading
	for i := 0; i < 6; i++ {
		mgr.RecordLoss()
	}

	ctx := defaultCtx()
	ctx.ConsecutiveLosses = 6
	err := mgr.Check(ctx)
	assertErr(t, err, "should fail on consecutive losses")
}

func TestManagerWinResetsLosses(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())

	mgr.RecordLoss()
	mgr.RecordLoss()
	mgr.RecordWin()

	ctx := defaultCtx()
	ctx.ConsecutiveLosses = 0
	err := mgr.Check(ctx)
	assertNoErr(t, err, "win should reset consecutive losses counter")
}

func TestManagerCircuitBreakerIntegration(t *testing.T) {
	mgr := NewManager(ManagerConfig{
		CircuitThreshold:     2,
		CircuitResetTimeout:  time.Hour,
		PriceDeviationPct:    5.0,
		MaxOrderUSDT:         100000,
		DailyOrderLimit:      100,
		MaxConcurrentOrders:  5,
		MaxPositionPct:       50.0,
		MaxExposurePct:       80.0,
		MaxDrawdownPct:       10.0,
		MaxConsecutiveLosses: 5,
		MaxFundingRatePct:    0.375,
		MinMarginRatioPct:    150.0,
		MaxVolatilityPct:     2.0,
		PriceSpikePct:        3.0,
	})

	ctx := defaultCtx()

	// Trigger 2 risk violations to trip the breaker
	ctx.OrderPrice = 100000.0
	mgr.Check(ctx) // fail 1
	mgr.Check(ctx) // fail 2

	// Now even a normal order should be blocked
	ctx.OrderPrice = 68000.0
	err := mgr.Check(ctx)
	assertErr(t, err, "circuit breaker open should block all checks")
}

func TestManagerRiskEvents(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := defaultCtx()
	ctx.OrderPrice = 100000.0 // will trigger price deviation

	mgr.Check(ctx)

	events := mgr.GetRiskEvents(10)
	assert(t, len(events) > 0, "should have recorded risk events")
}

func TestManagerOnRiskAlert(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())

	alertReceived := false
	mgr.OnRiskAlert = func(alert model.RiskAlert) {
		alertReceived = true
	}

	ctx := defaultCtx()
	ctx.OrderPrice = 100000.0
	mgr.Check(ctx)

	assert(t, alertReceived, "OnRiskAlert callback should fire")
}

func TestManagerGetCircuitBreaker(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	cb := mgr.GetCircuitBreaker()
	assert(t, cb != nil, "circuit breaker should be accessible")
	assert(t, cb.State() == CircuitClosed, "should start closed")
}

/* ── Benchmark ────────────────────────────────────────────────── */

func BenchmarkRiskCheckAll(t *testing.B) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := defaultCtx()

	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		mgr.Check(ctx)
	}
}

func BenchmarkRiskCheckSingle(t *testing.B) {
	check := PriceSanity(5.0)
	ctx := defaultCtx()

	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		check(ctx)
	}
}
