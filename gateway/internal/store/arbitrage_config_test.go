package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestArbitrageConfigRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	oldPath := configPath
	configPath = filepath.Join(tmpDir, "config.yaml")
	defer func() { configPath = oldPath }()

	// Reset cache so LoadConfig reads from the temp file.
	configCache = nil

	cfg := map[string]any{
		"adaptive_qty_enabled": true,
		"max_slippage_pct":     0.25,
		"min_order_qty":        0.002,
		"min_order_value":      20.5,
	}
	if err := SaveArbitrageConfig(cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Simulate a fresh process start: clear cache and reload from disk.
	configCache = nil
	LoadConfig()

	loaded := LoadArbitrageConfig()
	if loaded == nil {
		t.Fatal("expected loaded config")
	}
	if loaded["adaptive_qty_enabled"] != true {
		t.Errorf("adaptive_qty_enabled mismatch: got %v, want true", loaded["adaptive_qty_enabled"])
	}
	if loaded["max_slippage_pct"] != 0.25 {
		t.Errorf("max_slippage_pct mismatch: got %v, want 0.25", loaded["max_slippage_pct"])
	}
	if loaded["min_order_qty"] != 0.002 {
		t.Errorf("min_order_qty mismatch: got %v, want 0.002", loaded["min_order_qty"])
	}
	if loaded["min_order_value"] != 20.5 {
		t.Errorf("min_order_value mismatch: got %v, want 20.5", loaded["min_order_value"])
	}
}

// TestClearFakeSeedPerformance：种子编造业绩幂等清空（P1-1）。catalog 业绩
// 唯一写者是 Seed，清空安全；真实运行业绩在 ai_bot_instances 不受影响。
func TestClearFakeSeedPerformance(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	// 造一条带编造业绩的内置 catalog 行 + 一条非内置行（不应被动）。
	now := time.Now().Unix()
	_, err := db.Exec(`INSERT OR REPLACE INTO ai_bot_catalog
		(id, name, description, strategy_type, market_type, risk_level, fee_model, fee_percent, monthly_fee,
		 performance_json, config_json, is_builtin, is_active, created_at, updated_at)
		VALUES ('test-fake-perf', 'T', 't', 'optimus', 'spot', 'low', 'free', 0, 0,
		 '{"avg_monthly_profit":99.9}', '{}', 1, 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("seed row: %v", err)
	}
	_, err = db.Exec(`INSERT OR REPLACE INTO ai_bot_catalog
		(id, name, description, strategy_type, market_type, risk_level, fee_model, fee_percent, monthly_fee,
		 performance_json, config_json, is_builtin, is_active, created_at, updated_at)
		VALUES ('test-user-perf', 'U', 't', 'optimus', 'spot', 'low', 'free', 0, 0,
		 '{"avg_monthly_profit":1.2}', '{}', 0, 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("seed user row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM ai_bot_catalog WHERE id IN ('test-fake-perf','test-user-perf')")
	})

	clearFakeSeedPerformance()

	var builtinPerf, userPerf *string
	assertQuery := func(id string) *string {
		var v *string
		if err := db.QueryRow("SELECT performance_json FROM ai_bot_catalog WHERE id=?", id).Scan(&v); err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		return v
	}
	builtinPerf = assertQuery("test-fake-perf")
	userPerf = assertQuery("test-user-perf")
	if builtinPerf != nil {
		t.Fatalf("builtin fake performance must be cleared, got %v", *builtinPerf)
	}
	if userPerf == nil {
		t.Fatal("non-builtin performance must NOT be cleared")
	}

	// 幂等：再次调用无错误、仍为空。
	clearFakeSeedPerformance()
	if v := assertQuery("test-fake-perf"); v != nil {
		t.Fatalf("idempotent clear failed, got %v", *v)
	}
}
