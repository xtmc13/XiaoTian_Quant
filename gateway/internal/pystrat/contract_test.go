package pystrat

import "testing"

func TestManifestValidateRiskLeverage(t *testing.T) {
	base := func() *Manifest {
		return &Manifest{
			Name: "t", Symbol: "BTC/USDT", Interval: "15m", Direction: DirectionLong,
		}
	}

	// 边界：1 与 125 通过，0（不覆盖）通过，126 拒绝
	m := base()
	m.Risk.Leverage = 1
	if msg := m.Validate(); msg != "" {
		t.Fatalf("leverage=1 must pass, got %q", msg)
	}
	m.Risk.Leverage = MaxLeverage
	if msg := m.Validate(); msg != "" {
		t.Fatalf("leverage=%d must pass, got %q", MaxLeverage, msg)
	}
	m.Risk.Leverage = 0
	if msg := m.Validate(); msg != "" {
		t.Fatalf("leverage=0 (no override) must pass, got %q", msg)
	}
	m.Risk.Leverage = MaxLeverage + 1
	if msg := m.Validate(); msg == "" {
		t.Fatalf("leverage=%d must be rejected", MaxLeverage+1)
	}
	m.Risk.Leverage = -1
	if msg := m.Validate(); msg == "" {
		t.Fatal("leverage=-1 must be rejected")
	}
}

func TestManifestValidateRiskMarginMode(t *testing.T) {
	m := &Manifest{Name: "t", Symbol: "BTC/USDT", Interval: "15m", Direction: DirectionLong}
	for _, ok := range []string{"", "cross", "isolated"} {
		m.Risk.MarginMode = ok
		if msg := m.Validate(); msg != "" {
			t.Fatalf("margin_mode=%q must pass, got %q", ok, msg)
		}
	}
	m.Risk.MarginMode = "crossed"
	if msg := m.Validate(); msg == "" {
		t.Fatal("margin_mode=crossed must be rejected")
	}
}

func TestManifestEffectiveLeverageMarginMode(t *testing.T) {
	m := &Manifest{}
	// 默认回退
	if got := m.EffectiveLeverage(0); got != 1 {
		t.Fatalf("EffectiveLeverage(0) = %d want 1", got)
	}
	if got := m.EffectiveMarginMode(""); got != "cross" {
		t.Fatalf("EffectiveMarginMode(\"\") = %q want cross", got)
	}
	// record 列生效
	if got := m.EffectiveLeverage(10); got != 10 {
		t.Fatalf("EffectiveLeverage(10) = %d want 10", got)
	}
	if got := m.EffectiveMarginMode("isolated"); got != "isolated" {
		t.Fatalf("EffectiveMarginMode(isolated) = %q", got)
	}
	// manifest 覆盖 record
	m.Risk.Leverage = 25
	m.Risk.MarginMode = "cross"
	if got := m.EffectiveLeverage(10); got != 25 {
		t.Fatalf("manifest leverage must override record: got %d want 25", got)
	}
	if got := m.EffectiveMarginMode("isolated"); got != "cross" {
		t.Fatalf("manifest margin_mode must override record: got %q want cross", got)
	}
}
