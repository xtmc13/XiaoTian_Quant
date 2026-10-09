package cra

import (
	"testing"
)

// TestBaseCRAStrategyRuntimeStatus 函数级验证：模拟持仓/加仓状态后
// RuntimeStatus 必须如实返回（snake_case 字段、档位统计、平均成本）。
func TestBaseCRAStrategyRuntimeStatus(t *testing.T) {
	s := NewCRAContractStrategy("test_cra_runtime", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":             "BTCUSDT",
		"first_order_amount": 100,
		"order_count":        5,
		"add_positions": []any{
			map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003},
			map[string]any{"order": 2, "multiplier": 2, "spread": 0.04, "callback": 0.005},
		},
		"tp_mode":            "static",
		"take_profit_ratio":  0.013,
		"take_profit_method": "full",
		"market_type":        "swap",
		"leverage":           10,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// 未持仓：running=true、in_position=false、无持仓字段。
	st := s.RuntimeStatus()
	if st["running"] != true {
		t.Errorf("running = %v, want true", st["running"])
	}
	if st["in_position"] != false {
		t.Errorf("in_position = %v, want false", st["in_position"])
	}
	if _, ok := st["avg_entry_price"]; ok {
		t.Errorf("avg_entry_price must be absent when flat, got %v", st["avg_entry_price"])
	}
	// fillMissingAddPositions 会把梯子补齐到 order_count（5 档）。
	if st["total_add_tiers"] != 5 {
		t.Errorf("total_add_tiers = %v, want 5 (filled up to order_count)", st["total_add_tiers"])
	}

	// 模拟首单+一次补仓成交后的在仓状态。
	s.state.EnterPosition(50000, SideLong)
	s.state.RecordFill(50000, 0.2, SideBuy)
	s.state.PositionCount = 1
	s.state.RecordFill(48000, 0.4, SideBuy)
	s.state.PositionCount = 2
	s.lastSignalTime = 1720000000000
	s.lastSignalDirection = "LONG"

	st = s.RuntimeStatus()
	if st["in_position"] != true {
		t.Errorf("in_position = %v, want true", st["in_position"])
	}
	if st["direction"] != "long" {
		t.Errorf("direction = %v, want long", st["direction"])
	}
	if st["add_positions_triggered"] != 1 {
		t.Errorf("add_positions_triggered = %v, want 1", st["add_positions_triggered"])
	}
	if st["current_tier"] != 2 {
		t.Errorf("current_tier = %v, want 2", st["current_tier"])
	}
	avg := st["avg_entry_price"].(float64)
	wantAvg := (50000*0.2 + 48000*0.4) / 0.6
	if avg < wantAvg-1e-6 || avg > wantAvg+1e-6 {
		t.Errorf("avg_entry_price = %v, want %v", avg, wantAvg)
	}
	if st["last_signal_direction"] != "LONG" {
		t.Errorf("last_signal_direction = %v, want LONG", st["last_signal_direction"])
	}
}

// H4：RuntimeStatus 透出 pending_close_kind——仅在途平仓时出现（默认无键
// 零行为变化），各在途形态（尾单/首尾止盈、反向止盈/止损、燃烧、手动减仓/
// 清仓）原样透出，终态清除后键消失。
func TestRuntimeStatusPendingCloseKind(t *testing.T) {
	s := NewCRAContractStrategy("test_cra_pck", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol": "BTCUSDT", "first_order_amount": 100, "order_count": 5,
		"tp_mode": "static", "take_profit_ratio": 0.013, "take_profit_method": "tail",
		"market_type": "swap", "leverage": 10,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// 默认（无在途平仓）：键必须不存在（零行为变化边界）。
	st := s.RuntimeStatus()
	if _, ok := st["pending_close_kind"]; ok {
		t.Fatalf("无在途平仓时 pending_close_kind 不得出现, got %v", st["pending_close_kind"])
	}

	// 在仓但无在途平仓：同样无键。
	s.state.EnterPosition(50000, SideLong)
	s.state.RecordFill(50000, 0.2, SideBuy)
	s.state.PositionCount = 1
	if _, ok := s.RuntimeStatus()["pending_close_kind"]; ok {
		t.Fatal("在仓无在途平仓时不得透出 pending_close_kind")
	}

	// 各在途形态原样透出。
	for _, kind := range []string{"tail", "head_tail", "reverse_tp", "reverse_sl", "burn_dual", "burn_global", "manual_reduce", "manual_close"} {
		s.state.PendingCloseKind = kind
		st = s.RuntimeStatus()
		if st["pending_close_kind"] != kind {
			t.Fatalf("pending_close_kind = %v, want %q", st["pending_close_kind"], kind)
		}
	}

	// 终态清除：键消失。
	s.state.PendingCloseKind = ""
	if _, ok := s.RuntimeStatus()["pending_close_kind"]; ok {
		t.Fatal("在途平仓终态清除后 pending_close_kind 必须消失")
	}
}
