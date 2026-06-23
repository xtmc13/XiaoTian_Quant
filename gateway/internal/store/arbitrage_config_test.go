package store

import (
	"path/filepath"
	"testing"
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
