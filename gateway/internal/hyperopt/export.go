package hyperopt

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ── Export ───────────────────────────────────────────────────────

// ExportConfig defines how hyperopt parameters map to strategy fields.
type ExportConfig struct {
	StrategyID   string            `json:"strategy_id"`   // target strategy (empty = create new)
	StrategyType string            `json:"strategy_type"` // e.g. "breakout", "grid"
	StrategyName string            `json:"strategy_name"` // human-readable name
	ParamMap     map[string]string `json:"param_map"`     // hyperopt name → config field name
}

// StrategyConfig is the on-disk format matching strategy_configs.json.
type StrategyConfig struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	StrategyType string `json:"strategy_type"`
	Coin       string `json:"coin"`
	Direction  string `json:"direction"`
	Leverage   int    `json:"leverage"`
	Status     string `json:"status"`
	ConfigJSON string `json:"config_json"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// ExportBestParams merges optimal hyperopt parameters into a strategy config file.
// If strategyID is empty, a new config is appended; otherwise the existing one is updated.
func ExportBestParams(result *Result, cfg ExportConfig, configPath string) error {
	if result == nil || result.BestTrial == nil {
		return fmt.Errorf("no best trial to export")
	}
	if len(cfg.ParamMap) == 0 {
		return fmt.Errorf("param_map is empty")
	}

	// Read existing configs
	configs, err := loadStrategyConfigs(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("load configs: %w", err)
	}

	// Build merged config JSON
	merged, err := buildMergedConfig(result.BestTrial.Params, cfg)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	now := time.Now().UnixMilli()
	var target *StrategyConfig

	if cfg.StrategyID != "" {
		// Update existing
		for i := range configs {
			if configs[i].ID == cfg.StrategyID {
				target = &configs[i]
				break
			}
		}
	}

	if target == nil {
		// Create new
		newCfg := StrategyConfig{
			ID:           generateStrategyID(cfg.StrategyType),
			Name:         cfg.StrategyName,
			StrategyType: cfg.StrategyType,
			Status:       "draft",
			ConfigJSON:   merged,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		configs = append(configs, newCfg)
	} else {
		target.ConfigJSON = merged
		target.UpdatedAt = now
		if cfg.StrategyName != "" {
			target.Name = cfg.StrategyName
		}
	}

	// Write back
	data, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal configs: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("write configs: %w", err)
	}

	return nil
}

// ExportBestParamsToMap returns the best parameters as a flat map using the
// strategy field names (via ParamMap).  Useful for API responses.
func ExportBestParamsToMap(result *Result, paramMap map[string]string) (map[string]any, error) {
	if result == nil || result.BestTrial == nil {
		return nil, fmt.Errorf("no best trial")
	}
	if len(paramMap) == 0 {
		return nil, fmt.Errorf("param_map is empty")
	}

	out := make(map[string]any, len(paramMap))
	for hyperName, fieldName := range paramMap {
		if v, ok := result.BestTrial.Params[hyperName]; ok {
			out[fieldName] = v
		}
	}
	return out, nil
}

// ── Helpers ────────────────────────────────────────────────────

func loadStrategyConfigs(path string) ([]StrategyConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var configs []StrategyConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

// buildMergedConfig creates a config_json string from hyperopt params.
// It attempts to preserve existing config structure when updating.
func buildMergedConfig(params map[string]any, cfg ExportConfig) (string, error) {
	// Start with a base template for the strategy type
	base := make(map[string]any)
	base["strategy_type"] = cfg.StrategyType
	base["strategy_name"] = cfg.StrategyName
	base["hyperopt_optimized"] = true
	base["hyperopt_at"] = time.Now().Format(time.RFC3339)

	// Map hyperopt params to strategy fields
	for hyperName, fieldName := range cfg.ParamMap {
		if v, ok := params[hyperName]; ok {
			base[fieldName] = v
		}
	}

	data, err := json.Marshal(base)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func generateStrategyID(strategyType string) string {
	return fmt.Sprintf("strat-hyperopt-%s-%d", strategyType, time.Now().Unix())
}
