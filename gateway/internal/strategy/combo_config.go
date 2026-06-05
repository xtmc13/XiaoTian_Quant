package strategy

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"
)

// ComboMember defines a single strategy within a combo.
type ComboMember struct {
	StrategyName string  `json:"strategy_name"`
	Weight       float64 `json:"weight"`
	Enabled      bool    `json:"enabled"`
}

// ComboConfig holds the configuration for a strategy combo.
type ComboConfig struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Symbol          string        `json:"symbol"`
	Members         []ComboMember `json:"members"`
	AggregationMode string        `json:"aggregation_mode"` // "vote", "weighted", "unanimous"
	Status          string        `json:"status"`
	CreatedAt       int64         `json:"created_at"`
	UpdatedAt       int64         `json:"updated_at"`
}

// AddMember appends a new member to the combo.
func (c *ComboConfig) AddMember(m ComboMember) {
	c.Members = append(c.Members, m)
	c.UpdatedAt = time.Now().UnixMilli()
}

// RemoveMember removes a member by strategy name.
func (c *ComboConfig) RemoveMember(strategyName string) {
	filtered := make([]ComboMember, 0, len(c.Members))
	for _, m := range c.Members {
		if m.StrategyName != strategyName {
			filtered = append(filtered, m)
		}
	}
	c.Members = filtered
	c.UpdatedAt = time.Now().UnixMilli()
}

// UpdateWeight updates the weight of a specific member.
func (c *ComboConfig) UpdateWeight(strategyName string, weight float64) {
	for i := range c.Members {
		if c.Members[i].StrategyName == strategyName {
			c.Members[i].Weight = weight
			break
		}
	}
	c.UpdatedAt = time.Now().UnixMilli()
}

// SetAggregationMode changes the aggregation mode.
func (c *ComboConfig) SetAggregationMode(mode string) {
	c.AggregationMode = mode
	c.UpdatedAt = time.Now().UnixMilli()
}

// Validate checks the combo configuration for errors.
func (c *ComboConfig) Validate() error {
	if c.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch c.AggregationMode {
	case "vote", "weighted", "unanimous":
		// ok
	default:
		return fmt.Errorf("invalid aggregation_mode: %s", c.AggregationMode)
	}
	if c.AggregationMode == "weighted" {
		var sum float64
		enabledCount := 0
		for _, m := range c.Members {
			if m.Enabled {
				sum += m.Weight
				enabledCount++
			}
		}
		if enabledCount > 0 && math.Abs(sum-1.0) > 0.001 {
			return fmt.Errorf("weighted mode requires enabled weights to sum to 1.0, got %.3f", sum)
		}
	}
	return nil
}

// ToMap serializes the config to a generic map.
func (c *ComboConfig) ToMap() map[string]any {
	data, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return m
}

// ── Global combo registry ──

var (
	comboRegistry = make(map[string]*ComboConfig)
	comboMu       sync.RWMutex
)

// RegisterComboConfig stores a combo config in the global registry.
func RegisterComboConfig(c *ComboConfig) {
	comboMu.Lock()
	defer comboMu.Unlock()
	comboRegistry[c.ID] = c
}

// GetComboConfig retrieves a combo config by ID.
func GetComboConfig(id string) *ComboConfig {
	comboMu.RLock()
	defer comboMu.RUnlock()
	return comboRegistry[id]
}

// ListComboConfigs returns all registered combo configs.
func ListComboConfigs() []*ComboConfig {
	comboMu.RLock()
	defer comboMu.RUnlock()
	result := make([]*ComboConfig, 0, len(comboRegistry))
	for _, c := range comboRegistry {
		result = append(result, c)
	}
	return result
}

// DeleteComboConfig removes a combo config from the registry.
func DeleteComboConfig(id string) {
	comboMu.Lock()
	defer comboMu.Unlock()
	delete(comboRegistry, id)
}
