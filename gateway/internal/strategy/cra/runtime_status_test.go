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
