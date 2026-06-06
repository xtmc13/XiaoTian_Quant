package hyperopt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExportBestParamsToMap(t *testing.T) {
	result := &Result{
		BestTrial: &Trial{
			Params: map[string]any{"lookback": 20, "stop_loss": 0.05},
			Loss:   1.0,
		},
	}

	paramMap := map[string]string{
		"lookback":    "entry_period",
		"stop_loss":   "sl_pct",
	}

	m, err := ExportBestParamsToMap(result, paramMap)
	if err != nil {
		t.Fatalf("export to map: %v", err)
	}

	if m["entry_period"] != 20 {
		t.Errorf("expected entry_period=20, got %v", m["entry_period"])
	}
	if m["sl_pct"] != 0.05 {
		t.Errorf("expected sl_pct=0.05, got %v", m["sl_pct"])
	}
}

func TestExportBestParamsNoTrial(t *testing.T) {
	_, err := ExportBestParamsToMap(nil, map[string]string{"a": "b"})
	if err == nil {
		t.Error("expected error for nil result")
	}
}

func TestExportBestParamsEmptyMap(t *testing.T) {
	result := &Result{BestTrial: &Trial{Params: map[string]any{"x": 1}}}
	_, err := ExportBestParamsToMap(result, nil)
	if err == nil {
		t.Error("expected error for empty param map")
	}
}

func TestExportToFileNewConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "strategy_configs.json")

	// Seed with one existing config
	existing := []StrategyConfig{
		{ID: "existing-001", Name: "Old", ConfigJSON: "{}", CreatedAt: 1, UpdatedAt: 1},
	}
	data, _ := json.Marshal(existing)
	os.WriteFile(configPath, data, 0644)

	result := &Result{
		BestTrial: &Trial{
			Params: map[string]any{"fast_ema": 12, "slow_ema": 26},
			Loss:   0.5,
		},
	}

	cfg := ExportConfig{
		StrategyType: "ema_cross",
		StrategyName: "EMA Optimized",
		ParamMap: map[string]string{
			"fast_ema": "fast_period",
			"slow_ema": "slow_period",
		},
	}

	if err := ExportBestParams(result, cfg, configPath); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Verify
	configs, err := loadStrategyConfigs(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}

	newCfg := configs[1]
	if newCfg.StrategyType != "ema_cross" {
		t.Errorf("expected type ema_cross, got %s", newCfg.StrategyType)
	}
	if newCfg.Status != "draft" {
		t.Errorf("expected status draft, got %s", newCfg.Status)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(newCfg.ConfigJSON), &parsed)
	if parsed["fast_period"] != float64(12) {
		t.Errorf("expected fast_period=12, got %v", parsed["fast_period"])
	}
	if parsed["hyperopt_optimized"] != true {
		t.Error("expected hyperopt_optimized flag")
	}
}

func TestExportToFileUpdateExisting(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "strategy_configs.json")

	existing := []StrategyConfig{
		{ID: "target-001", Name: "Before", ConfigJSON: "{\"old\":true}", CreatedAt: 1, UpdatedAt: 1},
	}
	data, _ := json.Marshal(existing)
	os.WriteFile(configPath, data, 0644)

	result := &Result{
		BestTrial: &Trial{
			Params: map[string]any{"lookback": 50},
			Loss:   0.3,
		},
	}

	cfg := ExportConfig{
		StrategyID:   "target-001",
		StrategyType: "breakout",
		StrategyName: "Breakout V2",
		ParamMap:     map[string]string{"lookback": "lookback_period"},
	}

	if err := ExportBestParams(result, cfg, configPath); err != nil {
		t.Fatalf("export: %v", err)
	}

	configs, err := loadStrategyConfigs(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	if configs[0].Name != "Breakout V2" {
		t.Errorf("expected name updated, got %s", configs[0].Name)
	}
	if configs[0].UpdatedAt <= 1 {
		t.Error("expected updated_at to be newer")
	}

	var parsed map[string]any
	json.Unmarshal([]byte(configs[0].ConfigJSON), &parsed)
	if parsed["lookback_period"] != float64(50) {
		t.Errorf("expected lookback_period=50, got %v", parsed["lookback_period"])
	}
}

func TestExportNoBestTrial(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "strategy_configs.json")

	result := &Result{BestTrial: nil}
	cfg := ExportConfig{ParamMap: map[string]string{"a": "b"}}

	err := ExportBestParams(result, cfg, configPath)
	if err == nil {
		t.Error("expected error for nil best trial")
	}
}

func TestExportEmptyParamMap(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "strategy_configs.json")

	result := &Result{BestTrial: &Trial{Params: map[string]any{"x": 1}}}
	cfg := ExportConfig{}

	err := ExportBestParams(result, cfg, configPath)
	if err == nil {
		t.Error("expected error for empty param map")
	}
}
