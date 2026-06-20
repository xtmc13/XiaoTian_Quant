package strategy

import (
	"fmt"
	"math"
	"sync"

	"github.com/xiaotian-quant/gateway/internal/logging"
)

// ContractManager handles contract trading parameters including leverage,
// margin mode, position direction, and liquidation calculations.
type ContractManager struct {
	mu sync.RWMutex

	Leverage              float64
	Direction             string
	MarginMode            string
	MaxPositions          int
	MaintenanceMarginRate float64
	MaxLeverage           float64

	logger *logging.Logger
}

// NewContractManager creates a contract manager with sensible defaults.
func NewContractManager() *ContractManager {
	return &ContractManager{
		Leverage:              10,
		Direction:             "both",
		MarginMode:            "cross",
		MaxPositions:          10,
		MaintenanceMarginRate: 0.004,
		MaxLeverage:           125,
		logger:                logging.New("contract_manager"),
	}
}

// NewContractManagerWithParams creates a contract manager with custom parameters.
func NewContractManagerWithParams(leverage float64, direction, marginMode string, maxPositions int) *ContractManager {
	cm := NewContractManager()
	cm.Leverage = leverage
	cm.Direction = direction
	cm.MarginMode = marginMode
	cm.MaxPositions = maxPositions
	return cm
}

// Validate checks that contract parameters are within acceptable bounds.
func (cm *ContractManager) Validate() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if cm.Leverage <= 0 || cm.Leverage > cm.MaxLeverage {
		return fmt.Errorf("leverage must be between 1 and %.0f, got %.1f", cm.MaxLeverage, cm.Leverage)
	}
	if cm.Direction != "long" && cm.Direction != "short" && cm.Direction != "both" {
		return fmt.Errorf("direction must be 'long', 'short', or 'both', got %s", cm.Direction)
	}
	if cm.MarginMode != "isolated" && cm.MarginMode != "cross" {
		return fmt.Errorf("margin_mode must be 'isolated' or 'cross', got %s", cm.MarginMode)
	}
	if cm.MaxPositions <= 0 {
		return fmt.Errorf("max_positions must be positive, got %d", cm.MaxPositions)
	}
	return nil
}

// CalculateMargin computes the initial margin required for a position.
func (cm *ContractManager) CalculateMargin(positionValue float64) float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if cm.Leverage <= 0 {
		return positionValue
	}
	return positionValue / cm.Leverage
}

// CalculatePositionValue computes the total value of a position.
func (cm *ContractManager) CalculatePositionValue(entryPrice, quantity float64) float64 {
	return entryPrice * quantity
}

// CalculateLiquidationPrice estimates the liquidation price for a position.
func (cm *ContractManager) CalculateLiquidationPrice(entryPrice float64, side string, leverage float64) float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if entryPrice <= 0 || leverage <= 0 {
		return 0
	}
	if leverage <= 0 {
		leverage = cm.Leverage
	}
	mmr := cm.MaintenanceMarginRate
	switch side {
	case "LONG", "long", "buy":
		return entryPrice * (1 - 1/leverage + mmr)
	case "SHORT", "short", "sell":
		return entryPrice * (1 + 1/leverage - mmr)
	default:
		return 0
	}
}

// CalculateLiquidationPriceWithFees calculates liquidation price accounting for fees.
func (cm *ContractManager) CalculateLiquidationPriceWithFees(entryPrice float64, side string, leverage, feeRate float64) float64 {
	baseLiq := cm.CalculateLiquidationPrice(entryPrice, side, leverage)
	if baseLiq <= 0 {
		return 0
	}
	feeAdjustment := entryPrice * feeRate * 2
	switch side {
	case "LONG", "long", "buy":
		return baseLiq + feeAdjustment
	case "SHORT", "short", "sell":
		return baseLiq - feeAdjustment
	default:
		return 0
	}
}

// CalculateUnrealizedPnL computes the unrealized P&L for a position.
func (cm *ContractManager) CalculateUnrealizedPnL(entryPrice, currentPrice, quantity float64, side string) float64 {
	switch side {
	case "LONG", "long", "buy":
		return (currentPrice - entryPrice) * quantity
	case "SHORT", "short", "sell":
		return (entryPrice - currentPrice) * quantity
	default:
		return 0
	}
}

// CalculatePnLPercentage computes the P&L as a percentage of margin used.
func (cm *ContractManager) CalculatePnLPercentage(entryPrice, currentPrice float64, side string, leverage float64) float64 {
	pnl := cm.CalculateUnrealizedPnL(entryPrice, currentPrice, 1.0, side)
	if entryPrice <= 0 {
		return 0
	}
	margin := entryPrice / leverage
	if margin <= 0 {
		return 0
	}
	return pnl / margin * 100
}

// CalculateTrendFollowingSize implements trend-following sizing.
func (cm *ContractManager) CalculateTrendFollowingSize(counterPositions int, baseSize float64) float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if counterPositions < 0 {
		counterPositions = 0
	}
	if baseSize <= 0 {
		return 0
	}
	multiplier := float64(counterPositions + 1)
	size := baseSize * multiplier
	cm.logger.Info("trend following size calculated",
		"counter_positions", counterPositions,
		"base_size", baseSize,
		"multiplier", multiplier,
		"final_size", size)
	return size
}

// CalculateMaxPositionSize computes the maximum position size given available margin.
func (cm *ContractManager) CalculateMaxPositionSize(availableMargin, entryPrice float64) float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if entryPrice <= 0 || availableMargin <= 0 {
		return 0
	}
	return availableMargin * cm.Leverage / entryPrice
}

// CalculateMarginRatio computes the current margin ratio.
func (cm *ContractManager) CalculateMarginRatio(positionValue, positionMargin float64) float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if positionMargin <= 0 {
		return math.MaxFloat64
	}
	maintenanceMargin := positionValue * cm.MaintenanceMarginRate
	return maintenanceMargin / positionMargin
}

// IsNearLiquidation checks if a position is close to liquidation.
func (cm *ContractManager) IsNearLiquidation(positionValue, positionMargin float64) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	marginRatio := cm.CalculateMarginRatio(positionValue, positionMargin)
	return marginRatio > 0.8
}

// GetLeverageBracket returns the maximum position size for a given leverage tier.
func (cm *ContractManager) GetLeverageBracket(leverage float64) float64 {
	switch {
	case leverage <= 1:
		return 50000000
	case leverage <= 5:
		return 25000000
	case leverage <= 10:
		return 10000000
	case leverage <= 20:
		return 5000000
	case leverage <= 50:
		return 2500000
	case leverage <= 100:
		return 1000000
	default:
		return 500000
	}
}

// AdjustLeverageForPositionSize recommends a safe leverage level.
func (cm *ContractManager) AdjustLeverageForPositionSize(positionValueUSDT float64) float64 {
	switch {
	case positionValueUSDT <= 500000:
		return 125
	case positionValueUSDT <= 1000000:
		return 100
	case positionValueUSDT <= 2500000:
		return 50
	case positionValueUSDT <= 5000000:
		return 20
	case positionValueUSDT <= 10000000:
		return 10
	case positionValueUSDT <= 25000000:
		return 5
	default:
		return 1
	}
}

// SetLeverage updates the leverage with validation.
func (cm *ContractManager) SetLeverage(leverage float64) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if leverage <= 0 || leverage > cm.MaxLeverage {
		return fmt.Errorf("leverage must be between 1 and %.0f, got %.1f", cm.MaxLeverage, leverage)
	}
	cm.Leverage = leverage
	cm.logger.Info("leverage updated", "leverage", leverage)
	return nil
}

// SetDirection updates the trading direction with validation.
func (cm *ContractManager) SetDirection(direction string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if direction != "long" && direction != "short" && direction != "both" {
		return fmt.Errorf("direction must be 'long', 'short', or 'both', got %s", direction)
	}
	cm.Direction = direction
	return nil
}

// SetMarginMode updates the margin mode with validation.
func (cm *ContractManager) SetMarginMode(mode string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if mode != "isolated" && mode != "cross" {
		return fmt.Errorf("margin_mode must be 'isolated' or 'cross', got %s", mode)
	}
	cm.MarginMode = mode
	return nil
}

// Params returns the contract manager parameters as a map.
func (cm *ContractManager) Params() map[string]any {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return map[string]any{
		"leverage":                cm.Leverage,
		"direction":               cm.Direction,
		"margin_mode":             cm.MarginMode,
		"max_positions":           cm.MaxPositions,
		"maintenance_margin_rate": cm.MaintenanceMarginRate,
	}
}
