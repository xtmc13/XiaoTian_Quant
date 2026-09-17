package strategies

import (
	"testing"
)

// TestMACDStrategyRuntimeStatus 函数级验证（P0-1）：mock 持仓/信号状态后
// RuntimeStatus 返回的 snake_case 字段必须与实际状态一致。
func TestMACDStrategyRuntimeStatus(t *testing.T) {
	s := NewMACDStrategy()

	// 未启动：running=false、无持仓字段、无信号字段。
	st := s.RuntimeStatus()
	if st["running"] != false {
		t.Errorf("running = %v, want false", st["running"])
	}
	if _, ok := st["direction"]; ok {
		t.Errorf("direction must be absent when flat, got %v", st["direction"])
	}
	if _, ok := st["last_signal_time"]; ok {
		t.Errorf("last_signal_time must be absent before any signal, got %v", st["last_signal_time"])
	}

	// mock 在仓状态 + 最近信号。
	s.running = true
	s.inPosition = true
	s.direction = "LONG"
	s.entryPrice = 50000
	s.positionSize = 500
	s.lastSignalTime = 1720000000000
	s.lastSignalDirection = "LONG"

	st = s.RuntimeStatus()
	if st["running"] != true {
		t.Errorf("running = %v, want true", st["running"])
	}
	if st["in_position"] != true {
		t.Errorf("in_position = %v, want true", st["in_position"])
	}
	if st["direction"] != "LONG" {
		t.Errorf("direction = %v, want LONG", st["direction"])
	}
	if st["entry_price"] != 50000.0 {
		t.Errorf("entry_price = %v, want 50000", st["entry_price"])
	}
	if st["quantity"] != 500.0 {
		t.Errorf("quantity = %v, want 500", st["quantity"])
	}
	if st["last_signal_time"] != int64(1720000000000) {
		t.Errorf("last_signal_time = %v, want 1720000000000", st["last_signal_time"])
	}
	if st["last_signal_direction"] != "LONG" {
		t.Errorf("last_signal_direction = %v, want LONG", st["last_signal_direction"])
	}
}
