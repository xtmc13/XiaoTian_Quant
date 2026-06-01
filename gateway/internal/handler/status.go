package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
)

var startTime = time.Now()

// HealthCheck returns basic service health.
func HealthCheck(c *gin.Context) {
	appCtx := app.Get()
	c.JSON(http.StatusOK, gin.H{
		"status":       "ok",
		"uptime_secs":  int(time.Since(startTime).Seconds()),
		"version":      "2.0.0",
		"log_level":    appCtx.Config.Server.LogLevel,
	})
}

// ComponentHealth returns health status of all internal components.
func ComponentHealth(c *gin.Context) {
	appCtx := app.Get()
	components := map[string]string{}

	if appCtx.EventBus != nil {
		components["event_bus"] = "healthy"
	} else {
		components["event_bus"] = "not_initialized"
	}
	if appCtx.RiskManager != nil {
		components["risk_manager"] = "healthy"
	} else {
		components["risk_manager"] = "not_initialized"
	}
	if appCtx.PortfolioManager != nil {
		components["portfolio"] = "healthy"
	} else {
		components["portfolio"] = "not_initialized"
	}
	if appCtx.StrategyEngine != nil {
		components["strategy_engine"] = "healthy"
	} else {
		components["strategy_engine"] = "not_initialized"
	}
	if appCtx.Notifier != nil {
		components["notifier"] = "healthy"
	} else {
		components["notifier"] = "not_initialized"
	}

	if appCtx.Watchdog != nil {
		wdStatus := appCtx.Watchdog.GetStatus()
		for name, s := range wdStatus {
			if m, ok := s.(map[string]any); ok {
				if h, ok := m["healthy"].(bool); ok && h {
					components["watchdog_"+name] = "healthy"
				} else {
					components["watchdog_"+name] = "unhealthy"
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"timestamp":  time.Now().UnixMilli(),
		"components": components,
	})
}
