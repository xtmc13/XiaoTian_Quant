package service

import "testing"

func TestDemoLiquidityDefaultsOff(t *testing.T) {
	t.Setenv("PAPER_DEMO_LIQUIDITY", "")
	if demoLiquidityEnabled() {
		t.Fatal("demo liquidity must default to off")
	}
}

func TestDemoLiquidityEnvOverride(t *testing.T) {
	t.Setenv("PAPER_DEMO_LIQUIDITY", "true")
	if !demoLiquidityEnabled() {
		t.Fatal("env override should enable demo liquidity")
	}
	t.Setenv("PAPER_DEMO_LIQUIDITY", "false")
	if demoLiquidityEnabled() {
		t.Fatal("env override should disable demo liquidity")
	}
}
