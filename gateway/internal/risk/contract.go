package risk

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/logging"
)

// LiquidationAlertLevel represents the severity of liquidation risk.
type LiquidationAlertLevel string

const (
	LevelSafe     LiquidationAlertLevel = "safe"
	LevelWarning  LiquidationAlertLevel = "warning"
	LevelDanger   LiquidationAlertLevel = "danger"
	LevelCritical LiquidationAlertLevel = "critical"
)

func (l LiquidationAlertLevel) String() string {
	return string(l)
}

// ContractRiskAlert represents a single risk alert event.
type ContractRiskAlert struct {
	Level       LiquidationAlertLevel `json:"level"`
	Message     string                `json:"message"`
	MarginRatio float64               `json:"margin_ratio"`
	Timestamp   int64                 `json:"timestamp"`
}

// ContractRiskMonitor monitors contract/futures positions for liquidation risk.
type ContractRiskMonitor struct {
	mu sync.RWMutex

	MarginRatio       float64
	MaintenanceRate   float64
	LiquidationAlert  bool
	AlertLevel        LiquidationAlertLevel
	WarningThreshold  float64
	DangerThreshold   float64
	CriticalThreshold float64
	MarginBalance     float64
	MaintenanceMargin float64
	PositionValue     float64
	UnrealizedPnL     float64

	alerts []ContractRiskAlert
	logger *logging.Logger
}

// NewContractRiskMonitor creates a new contract risk monitor with defaults.
func NewContractRiskMonitor() *ContractRiskMonitor {
	return &ContractRiskMonitor{
		MaintenanceRate:   0.004,
		WarningThreshold:  0.5,
		DangerThreshold:   0.75,
		CriticalThreshold: 0.9,
		AlertLevel:        LevelSafe,
		alerts:            make([]ContractRiskAlert, 0, 128),
		logger:            logging.New("contract_risk"),
	}
}

// NewContractRiskMonitorWithThresholds creates a monitor with custom thresholds.
func NewContractRiskMonitorWithThresholds(warning, danger, critical float64) *ContractRiskMonitor {
	crm := NewContractRiskMonitor()
	if warning > 0 {
		crm.WarningThreshold = warning
	}
	if danger > 0 {
		crm.DangerThreshold = danger
	}
	if critical > 0 {
		crm.CriticalThreshold = critical
	}
	return crm
}

// CheckMarginRatio computes the margin ratio from balance and maintenance margin.
func (crm *ContractRiskMonitor) CheckMarginRatio(marginBalance, maintenanceMargin float64) float64 {
	crm.mu.Lock()
	defer crm.mu.Unlock()
	crm.MarginBalance = marginBalance
	crm.MaintenanceMargin = maintenanceMargin
	if marginBalance <= 0 {
		crm.MarginRatio = math.MaxFloat64
		return crm.MarginRatio
	}
	crm.MarginRatio = maintenanceMargin / marginBalance
	crm.updateAlertLevel()
	return crm.MarginRatio
}

// IsLiquidationRisk returns true when the margin ratio exceeds the warning threshold.
func (crm *ContractRiskMonitor) IsLiquidationRisk(marginRatio float64) bool {
	crm.mu.Lock()
	defer crm.mu.Unlock()
	crm.MarginRatio = marginRatio
	crm.updateAlertLevel()
	return marginRatio >= crm.WarningThreshold
}

// GetLiquidationAlertLevel returns the current risk level as a string.
func (crm *ContractRiskMonitor) GetLiquidationAlertLevel() string {
	crm.mu.RLock()
	defer crm.mu.RUnlock()
	return string(crm.AlertLevel)
}

// GetAlertLevel returns the current alert level as a typed constant.
func (crm *ContractRiskMonitor) GetAlertLevel() LiquidationAlertLevel {
	crm.mu.RLock()
	defer crm.mu.RUnlock()
	return crm.AlertLevel
}

// GetRecommendedAction returns a suggested action based on current risk level.
func (crm *ContractRiskMonitor) GetRecommendedAction() string {
	crm.mu.RLock()
	defer crm.mu.RUnlock()
	switch crm.AlertLevel {
	case LevelSafe:
		return "Position is safe. Continue monitoring."
	case LevelWarning:
		return "Consider reducing position size or adding margin."
	case LevelDanger:
		return "Urgent: Reduce leverage, add margin, or close part of the position."
	case LevelCritical:
		return "CRITICAL: Position is near liquidation. Close position or add margin immediately."
	default:
		return "Unknown risk level."
	}
}

// updateAlertLevel updates the alert level based on current margin ratio.
func (crm *ContractRiskMonitor) updateAlertLevel() {
	previousLevel := crm.AlertLevel
	switch {
	case crm.MarginRatio >= crm.CriticalThreshold:
		crm.AlertLevel = LevelCritical
		crm.LiquidationAlert = true
	case crm.MarginRatio >= crm.DangerThreshold:
		crm.AlertLevel = LevelDanger
		crm.LiquidationAlert = true
	case crm.MarginRatio >= crm.WarningThreshold:
		crm.AlertLevel = LevelWarning
		crm.LiquidationAlert = true
	default:
		crm.AlertLevel = LevelSafe
		crm.LiquidationAlert = false
	}
	if previousLevel != crm.AlertLevel && crm.AlertLevel != LevelSafe {
		alert := ContractRiskAlert{
			Level:       crm.AlertLevel,
			Message:     crm.GetRecommendedAction(),
			MarginRatio: crm.MarginRatio,
			Timestamp:   time.Now().UnixMilli(),
		}
		crm.alerts = append(crm.alerts, alert)
		if len(crm.alerts) > 1000 {
			crm.alerts = crm.alerts[len(crm.alerts)-1000:]
		}
		crm.logger.Warn("liquidation risk alert",
			"level", crm.AlertLevel,
			"margin_ratio", fmt.Sprintf("%.4f", crm.MarginRatio),
			"action", crm.GetRecommendedAction())
	}
}

// UpdatePositionMetrics updates all position-related metrics and recalculates risk.
func (crm *ContractRiskMonitor) UpdatePositionMetrics(marginBalance, maintenanceMargin, positionValue, unrealizedPnL float64) {
	crm.mu.Lock()
	defer crm.mu.Unlock()
	crm.MarginBalance = marginBalance
	crm.MaintenanceMargin = maintenanceMargin
	crm.PositionValue = positionValue
	crm.UnrealizedPnL = unrealizedPnL
	if marginBalance <= 0 {
		crm.MarginRatio = math.MaxFloat64
	} else {
		crm.MarginRatio = maintenanceMargin / marginBalance
	}
	crm.updateAlertLevel()
}

// CalculateRequiredMargin computes the additional margin needed to reach a target margin ratio.
func (crm *ContractRiskMonitor) CalculateRequiredMargin(targetRatio float64) float64 {
	crm.mu.RLock()
	defer crm.mu.RUnlock()
	if targetRatio <= 0 || crm.MaintenanceMargin <= 0 {
		return 0
	}
	requiredBalance := crm.MaintenanceMargin / targetRatio
	additionalNeeded := requiredBalance - crm.MarginBalance
	if additionalNeeded < 0 {
		return 0
	}
	return additionalNeeded
}

// GetRecentAlerts returns the most recent N risk alerts.
func (crm *ContractRiskMonitor) GetRecentAlerts(limit int) []ContractRiskAlert {
	crm.mu.RLock()
	defer crm.mu.RUnlock()
	if limit <= 0 || limit > len(crm.alerts) {
		limit = len(crm.alerts)
	}
	if limit == 0 {
		return nil
	}
	start := len(crm.alerts) - limit
	if start < 0 {
		start = 0
	}
	result := make([]ContractRiskAlert, limit)
	copy(result, crm.alerts[start:])
	return result
}

// Reset clears all alerts and resets the monitor state.
func (crm *ContractRiskMonitor) Reset() {
	crm.mu.Lock()
	defer crm.mu.Unlock()
	crm.MarginRatio = 0
	crm.LiquidationAlert = false
	crm.AlertLevel = LevelSafe
	crm.MarginBalance = 0
	crm.MaintenanceMargin = 0
	crm.PositionValue = 0
	crm.UnrealizedPnL = 0
	crm.alerts = crm.alerts[:0]
}

// Summary returns a comprehensive risk summary for the current position.
func (crm *ContractRiskMonitor) Summary() map[string]any {
	crm.mu.RLock()
	defer crm.mu.RUnlock()
	return map[string]any{
		"margin_ratio":       crm.MarginRatio,
		"maintenance_rate":   crm.MaintenanceRate,
		"alert_level":        string(crm.AlertLevel),
		"liquidation_alert":  crm.LiquidationAlert,
		"margin_balance":     crm.MarginBalance,
		"maintenance_margin": crm.MaintenanceMargin,
		"position_value":     crm.PositionValue,
		"unrealized_pnl":     crm.UnrealizedPnL,
		"recommended_action": crm.GetRecommendedAction(),
		"recent_alert_count": len(crm.alerts),
	}
}
