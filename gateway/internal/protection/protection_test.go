package protection

import (
	"testing"
	"time"
)

func TestCooldownPeriod(t *testing.T) {
	p, err := NewCooldownPeriod(map[string]any{
		"stop_duration_candles": 5,
		"timeframe":             "1h",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()

	// No exit recorded yet — should not block
	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	result := p.Check(ctx)
	if result.Blocked {
		t.Fatal("should not block before any exit")
	}

	// Record an exit
	p.RecordExit("BTCUSDT", now.Add(-2*time.Hour))

	// Within cooldown (5h) — should block
	result = p.Check(ctx)
	if !result.Blocked {
		t.Fatal("should block during cooldown")
	}
	if result.ResumeTime.IsZero() {
		t.Fatal("resume time should be set")
	}
	if result.Pair != "BTCUSDT" {
		t.Fatalf("pair should be BTCUSDT, got %s", result.Pair)
	}

	// After cooldown — should not block
	ctx.CurrentTime = now.Add(6 * time.Hour)
	result = p.Check(ctx)
	if result.Blocked {
		t.Fatal("should not block after cooldown expires")
	}
}

func TestCooldownPeriodDifferentPairs(t *testing.T) {
	p, err := NewCooldownPeriod(map[string]any{
		"stop_duration_candles": 3,
		"timeframe":             "1h",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()
	p.RecordExit("BTCUSDT", now.Add(-1*time.Hour))

	// BTC should be blocked
	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	if !p.Check(ctx).Blocked {
		t.Fatal("BTC should be blocked")
	}

	// ETH should not be blocked
	ctx.Symbol = "ETHUSDT"
	if p.Check(ctx).Blocked {
		t.Fatal("ETH should not be blocked")
	}
}

func TestStoplossGuardGlobal(t *testing.T) {
	p, err := NewStoplossGuard(map[string]any{
		"lookback_period_candles": 24,
		"trade_limit":             3,
		"stop_duration_candles":   12,
		"timeframe":               "1h",
		"only_per_pair":           false,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()

	// Record 3 stoplosses within lookback
	p.RecordStoploss("BTCUSDT", now.Add(-2*time.Hour))
	p.RecordStoploss("ETHUSDT", now.Add(-4*time.Hour))
	p.RecordStoploss("SOLUSDT", now.Add(-6*time.Hour))

	// Should block globally
	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	result := p.Check(ctx)
	if !result.Blocked {
		t.Fatal("should block after 3 stoplosses")
	}
	if result.Pair != "" {
		t.Fatal("global block should not have pair set")
	}
	if result.ResumeTime.IsZero() {
		t.Fatal("resume time should be set")
	}

	// Any pair should be blocked
	ctx.Symbol = "ANYUSDT"
	if !p.Check(ctx).Blocked {
		t.Fatal("all pairs should be blocked")
	}
}

func TestStoplossGuardPerPair(t *testing.T) {
	p, err := NewStoplossGuard(map[string]any{
		"lookback_period_candles": 24,
		"trade_limit":             2,
		"stop_duration_candles":   6,
		"timeframe":               "1h",
		"only_per_pair":           true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()

	// Record 2 stoplosses on BTC
	p.RecordStoploss("BTCUSDT", now.Add(-1*time.Hour))
	p.RecordStoploss("BTCUSDT", now.Add(-3*time.Hour))

	// BTC should be blocked
	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	if !p.Check(ctx).Blocked {
		t.Fatal("BTC should be blocked")
	}
	if ctx.Symbol != "BTCUSDT" {
		t.Fatal("pair block should be BTCUSDT")
	}

	// ETH should not be blocked
	ctx.Symbol = "ETHUSDT"
	if p.Check(ctx).Blocked {
		t.Fatal("ETH should not be blocked")
	}
}

func TestStoplossGuardOldStoplosses(t *testing.T) {
	p, err := NewStoplossGuard(map[string]any{
		"lookback_period_candles": 6,
		"trade_limit":             2,
		"stop_duration_candles":   3,
		"timeframe":               "1h",
		"only_per_pair":           false,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()

	// Record old stoplosses outside lookback
	p.RecordStoploss("BTCUSDT", now.Add(-10*time.Hour))
	p.RecordStoploss("BTCUSDT", now.Add(-8*time.Hour))

	// Should not block — stoplosses are outside 6h lookback
	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	if p.Check(ctx).Blocked {
		t.Fatal("should not block for old stoplosses")
	}
}

func TestMaxDrawdown(t *testing.T) {
	p, err := NewMaxDrawdown(map[string]any{
		"max_drawdown_pct":        0.20,
		"lookback_period_candles": 48,
		"stop_duration_candles":   12,
		"timeframe":               "1h",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()

	// Drawdown below threshold — should not block
	ctx := ProtectionContext{CurrentTime: now, CurrentDrawdown: 0.10}
	if p.Check(ctx).Blocked {
		t.Fatal("should not block for 10% drawdown")
	}

	// Drawdown at threshold — should block
	ctx.CurrentDrawdown = 0.25
	result := p.Check(ctx)
	if !result.Blocked {
		t.Fatal("should block for 25% drawdown")
	}
	if result.ResumeTime.IsZero() {
		t.Fatal("resume time should be set")
	}
	if result.Pair != "" {
		t.Fatal("max drawdown is global, no pair")
	}
}

func TestMaxDrawdownValidation(t *testing.T) {
	_, err := NewMaxDrawdown(map[string]any{
		"max_drawdown_pct": 1.5,
	})
	if err != nil {
		t.Fatalf("create should succeed with defaults: %v", err)
	}

	p, _ := NewMaxDrawdown(map[string]any{
		"max_drawdown_pct": 0.5,
	})
	if err := p.Validate(); err != nil {
		t.Fatalf("validate should pass: %v", err)
	}
}

func TestLowProfitPairs(t *testing.T) {
	p, err := NewLowProfitPairs(map[string]any{
		"lookback_period_candles": 24,
		"min_profit_ratio":        0.01,
		"min_trade_count":         3,
		"timeframe":               "1h",
		"stop_duration_candles":   6,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()

	// Record 3 losing trades on BTC
	for i := 0; i < 3; i++ {
		p.RecordTrade(TradeRecord{
			Symbol:    "BTCUSDT",
			EntryPrice: 50000,
			ExitPrice:  49000,
			Quantity:   0.1,
			PnL:        -100,
			PnLPct:     -0.02,
			EntryTime:  now.Add(-time.Duration(i) * time.Hour),
			ExitTime:   now.Add(-time.Duration(i) * time.Hour),
		})
	}

	// Should block BTC
	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	result := p.Check(ctx)
	if !result.Blocked {
		t.Fatal("should block low profit pair")
	}
	if result.Pair != "BTCUSDT" {
		t.Fatalf("pair should be BTCUSDT, got %s", result.Pair)
	}

	// ETH should not be blocked (no trades)
	ctx.Symbol = "ETHUSDT"
	if p.Check(ctx).Blocked {
		t.Fatal("ETH should not be blocked")
	}
}

func TestLowProfitPairsProfitable(t *testing.T) {
	p, err := NewLowProfitPairs(map[string]any{
		"lookback_period_candles": 24,
		"min_profit_ratio":        0.01,
		"min_trade_count":         2,
		"timeframe":               "1h",
		"stop_duration_candles":   6,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now()

	// Record 2 profitable trades
	for i := 0; i < 2; i++ {
		p.RecordTrade(TradeRecord{
			Symbol:    "BTCUSDT",
			EntryPrice: 50000,
			ExitPrice:  51000,
			Quantity:   0.1,
			PnL:        100,
			PnLPct:     0.02,
			EntryTime:  now.Add(-time.Duration(i) * time.Hour),
			ExitTime:   now.Add(-time.Duration(i) * time.Hour),
		})
	}

	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	if p.Check(ctx).Blocked {
		t.Fatal("should not block profitable pair")
	}
}

func TestProtectionManager(t *testing.T) {
	mgr := NewProtectionManager()

	// Add multiple protections
	cooldown, _ := NewCooldownPeriod(map[string]any{"stop_duration_candles": 3, "timeframe": "1h"})
	stoploss, _ := NewStoplossGuard(map[string]any{
		"lookback_period_candles": 6,
		"trade_limit":             2,
		"stop_duration_candles":   3,
		"timeframe":               "1h",
		"only_per_pair":           false,
	})
	mgr.AddProtection(cooldown)
	mgr.AddProtection(stoploss)

	now := time.Now()

	// No blocks initially
	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	result := mgr.CheckAll(ctx)
	if result.Blocked {
		t.Fatal("should not block initially")
	}

	// Record a stoploss to trigger StoplossGuard
	stoploss.RecordStoploss("BTCUSDT", now.Add(-1*time.Hour))
	stoploss.RecordStoploss("ETHUSDT", now.Add(-2*time.Hour))

	// Should block globally
	result = mgr.CheckAll(ctx)
	if !result.Blocked {
		t.Fatal("should block after 2 stoplosses")
	}

	// IsBlocked should confirm
	blocked, info := mgr.IsBlocked("BTCUSDT", now)
	if !blocked {
		t.Fatal("IsBlocked should return true")
	}
	if info.Reason == "" {
		t.Fatal("block reason should be set")
	}

	// Reset global
	mgr.ResetGlobal()
	blocked, _ = mgr.IsBlocked("BTCUSDT", now)
	if blocked {
		t.Fatal("should not block after ResetGlobal")
	}
}

func TestProtectionManagerStatus(t *testing.T) {
	mgr := NewProtectionManager()
	stoploss, _ := NewStoplossGuard(map[string]any{
		"lookback_period_candles": 6,
		"trade_limit":             1,
		"stop_duration_candles":   3,
		"timeframe":               "1h",
		"only_per_pair":           true,
	})
	mgr.AddProtection(stoploss)

	now := time.Now()
	stoploss.RecordStoploss("BTCUSDT", now.Add(-30*time.Minute))

	// Trigger block
	ctx := ProtectionContext{Symbol: "BTCUSDT", CurrentTime: now}
	mgr.CheckAll(ctx)

	status := mgr.Status(now)
	if !status["global_blocked"].(bool) {
		// Per-pair block won't show in global_blocked
		pairBlocks := status["pair_blocks"].(map[string]any)
		if len(pairBlocks) == 0 {
			t.Fatal("status should show pair blocks")
		}
	}
}

func TestBuildManagerFromConfig(t *testing.T) {
	cfg := Config{
		Protections: []ProtectionConfig{
			{Name: "CooldownPeriod", Params: map[string]any{"stop_duration_candles": 5, "timeframe": "1h"}},
			{Name: "MaxDrawdown", Params: map[string]any{"max_drawdown_pct": 0.15, "timeframe": "1h"}},
		},
	}

	mgr, err := BuildManagerFromConfig(cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if len(mgr.Protections()) != 2 {
		t.Fatalf("expected 2 protections, got %d", len(mgr.Protections()))
	}
}

func TestCreateProtectionUnknown(t *testing.T) {
	_, err := CreateProtection("UnknownProtection", nil)
	if err == nil {
		t.Fatal("expected error for unknown protection")
	}
}

func TestCooldownPeriodValidation(t *testing.T) {
	p, _ := NewCooldownPeriod(map[string]any{})
	if err := p.Validate(); err != nil {
		t.Fatalf("default should validate: %v", err)
	}

	p.StopDurationCandles = 0
	if err := p.Validate(); err == nil {
		t.Fatal("should fail with stop_duration_candles=0")
	}
}

func TestStoplossGuardValidation(t *testing.T) {
	p, _ := NewStoplossGuard(map[string]any{})
	if err := p.Validate(); err != nil {
		t.Fatalf("default should validate: %v", err)
	}

	p.TradeLimit = 0
	if err := p.Validate(); err == nil {
		t.Fatal("should fail with trade_limit=0")
	}
}
