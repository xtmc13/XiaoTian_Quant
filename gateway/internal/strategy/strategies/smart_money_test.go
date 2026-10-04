package strategies

import "testing"

// TestSmartMoneyPositionSizeStake position_size USDT 本金契约（2026-10-04 补）：
// 此前 smart_money 无仓位参数、无 CustomStakeAmount 钩子，信号量落到
// resolveSignalQuantity 的"可用余额 10%"兜底，单笔名义不可控。引擎侧
// stake ÷ 最新价 → Qty 的折算由 TestEmitSignalCustomStakeAmount
// （internal/strategy/hooks_test.go）覆盖，这里钉策略侧契约。
func TestSmartMoneyPositionSizeStake(t *testing.T) {
	s := NewSmartMoneyStrategy()

	// 出厂默认 100 USDT。
	if stake := s.CustomStakeAmount(10000, nil); stake != 100 {
		t.Fatalf("default stake = %v, want 100", stake)
	}

	// Start 应用 position_size=50（同 classic/liquidity_heat 的 ApplyParams 路径）。
	if err := s.Start(map[string]any{"symbol": "SOLUSDT", "position_size": 50.0}); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	if s.positionSize != 50 {
		s.mu.RUnlock()
		t.Fatalf("positionSize = %v, want 50", s.positionSize)
	}
	s.mu.RUnlock()
	if stake := s.CustomStakeAmount(10000, nil); stake != 50 {
		t.Fatalf("stake = %v, want 50（50 USDT 本金由引擎按现价折算数量）", stake)
	}
	if p := s.Params()["position_size"]; p != 50.0 {
		t.Fatalf("Params()[position_size] = %v, want 50", p)
	}

	// 可用余额不足退化为全额可用（与 liquidity_heat 同口径）。
	if stake := s.CustomStakeAmount(30, nil); stake != 30 {
		t.Fatalf("stake capped by balance = %v, want 30", stake)
	}

	// 非法值（≤0/类型不符）不覆盖既有值，stake 恒正。
	if err := s.ApplyParams(map[string]any{"position_size": -5.0}); err != nil {
		t.Fatal(err)
	}
	if stake := s.CustomStakeAmount(10000, nil); stake != 50 {
		t.Fatalf("negative position_size must not apply, stake = %v, want 50", stake)
	}
	if err := s.ApplyParams(map[string]any{"position_size": "abc"}); err != nil {
		t.Fatal(err)
	}
	if stake := s.CustomStakeAmount(10000, nil); stake != 50 {
		t.Fatalf("non-numeric position_size must not apply, stake = %v, want 50", stake)
	}
}
