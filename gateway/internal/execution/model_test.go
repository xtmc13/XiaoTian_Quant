package execution

import (
	"math"
	"testing"
)

func TestFee(t *testing.T) {
	cfg := Default()
	if got := Fee(1000, cfg); math.Abs(got-1) > 1e-9 {
		t.Fatalf("fee = %v, want 1", got)
	}
	if got := Fee(0, cfg); got != 0 {
		t.Fatalf("zero notional fee = %v, want 0", got)
	}
	if got := Fee(-1000, cfg); got != 0 {
		t.Fatalf("negative notional fee = %v, want 0", got)
	}
}

func TestApplySlippage(t *testing.T) {
	cfg := Config{Slippage: 0.001}
	if got := ApplySlippage(100, "LONG", cfg, 0); math.Abs(got-100.1) > 1e-9 {
		t.Fatalf("LONG u=0: got %v want 100.1", got)
	}
	if got := ApplySlippage(100, "LONG", cfg, 0.5); math.Abs(got-100.2) > 1e-9 {
		t.Fatalf("LONG u=0.5: got %v want 100.2", got)
	}
	if got := ApplySlippage(100, "SHORT", cfg, 0); math.Abs(got-99.9) > 1e-9 {
		t.Fatalf("SHORT u=0: got %v want 99.9", got)
	}
	if got := ApplySlippage(100, "CLOSE", cfg, 0.5); math.Abs(got-99.8) > 1e-9 {
		t.Fatalf("CLOSE u=0.5: got %v want 99.8", got)
	}
	if got := ApplySlippage(0, "LONG", cfg, 0); got != 0 {
		t.Fatalf("zero price: got %v", got)
	}
}

func TestSplitFill(t *testing.T) {
	if got := SplitFill(10, Default()); got != 10 {
		t.Fatalf("full fill: got %v want 10", got)
	}
	cfg := Config{PartialFillRate: 0.4}
	if got := SplitFill(10, cfg); math.Abs(got-4) > 1e-9 {
		t.Fatalf("partial fill: got %v want 4", got)
	}
	if got := SplitFill(0, cfg); got != 0 {
		t.Fatalf("zero remaining: got %v", got)
	}
	// Out-of-range rates fall back to full fill rather than stalling orders.
	if got := SplitFill(10, Config{PartialFillRate: 1.5}); got != 10 {
		t.Fatalf("oversized rate: got %v want 10", got)
	}
}

func TestPlanLimitFillFull(t *testing.T) {
	cfg := Config{FeeRate: 0.001, PartialFillRate: 1}
	plan, ok := PlanLimitFill(100, 10, 0, cfg)
	if !ok || plan.Fill != 10 || !plan.Full || math.Abs(plan.Fee-1) > 1e-9 {
		t.Fatalf("full plan: ok=%v %+v", ok, plan)
	}
}

func TestPlanLimitFillPartial(t *testing.T) {
	cfg := Config{FeeRate: 0.001, PartialFillRate: 0.4}
	plan, ok := PlanLimitFill(100, 10, 2, cfg)
	if !ok {
		t.Fatal("expected a fill plan")
	}
	// remaining 8 * 0.4 = 3.2, cumulative 5.2, still open
	if math.Abs(plan.Fill-3.2) > 1e-9 || math.Abs(plan.Filled-5.2) > 1e-9 || plan.Full {
		t.Fatalf("partial plan: %+v", plan)
	}
}

func TestPlanLimitFillCapsAtQuantity(t *testing.T) {
	plan, ok := PlanLimitFill(100, 10, 9, Config{PartialFillRate: 1})
	if !ok || plan.Fill != 1 || plan.Filled != 10 || !plan.Full {
		t.Fatalf("cap plan: ok=%v %+v", ok, plan)
	}
}

func TestPlanLimitFillRejectsEmpty(t *testing.T) {
	if _, ok := PlanLimitFill(100, 10, 10, Default()); ok {
		t.Fatal("filled order should not plan another fill")
	}
	if _, ok := PlanLimitFill(0, 10, 0, Default()); ok {
		t.Fatal("zero-price order should not fill")
	}
}
