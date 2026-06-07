package handler

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/logging"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/service"
	"github.com/xiaotian-quant/gateway/internal/store"
)

var startTime = time.Now()

// HealthCheck returns basic service health.
func HealthCheck(c *gin.Context) {
	appCtx := app.Get()
	logLevel := "info"
	if appCtx != nil && appCtx.Config != nil {
		logLevel = appCtx.Config.Server.LogLevel
	}
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"uptime":    int(time.Since(startTime).Seconds()),
		"version":   "3.0.0",
		"log_level": logLevel,
	})
}

// ReloadConfig triggers a hot reload of the configuration file.
func ReloadConfig(c *gin.Context) {
	appCtx := app.Get()
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "config.yaml"
	}
	newCfg, err := config.Reload(cfgPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reload failed", "details": err.Error()})
		return
	}

	// Apply log level change immediately
	if appCtx.Logger != nil {
		appCtx.Logger.SetLevel(logging.LevelFromString(newCfg.Server.LogLevel))
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "reloaded",
		"log_level": newCfg.Server.LogLevel,
		"timestamp": time.Now().UnixMilli(),
	})
}
func ComponentHealth(c *gin.Context) {
	appCtx := app.Get()
	components := map[string]any{}

	// Core services
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

	// Order pipeline
	if om := order.GetOrderManager(); om != nil {
		components["order_manager"] = "healthy"
	} else {
		components["order_manager"] = "not_initialized"
	}
	if ms := service.GetMatchingService(); ms != nil {
		components["matching_service"] = "healthy"
	} else {
		components["matching_service"] = "not_initialized"
	}

	// Exchange adapter
	if appCtx.BinanceWS != nil {
		components["binance_ws"] = "healthy"
	} else {
		components["binance_ws"] = "not_initialized"
	}

	// Store (via DB connection)
	if db := store.GetDB(); db != nil {
		if err := db.Ping(); err == nil {
			components["store"] = "healthy"
		} else {
			components["store"] = "degraded"
		}
	} else {
		components["store"] = "not_initialized"
	}

	// Watchdog
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

	// Runtime stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	components["runtime"] = map[string]any{
		"goroutines": runtime.NumGoroutine(),
		"memory_mb":  fmt.Sprintf("%.2f", float64(m.Alloc)/1024/1024),
		"gc_count":   m.NumGC,
	}

	// Overall status
	overall := "healthy"
	for _, v := range components {
		if s, ok := v.(string); ok && s == "unhealthy" {
			overall = "degraded"
			break
		}
	}
	_ = overall // suppress unused variable warning

	// Convert to array format for frontend ComponentHealth[]
	result := make([]map[string]any, 0, len(components))
	for name, status := range components {
		if s, ok := status.(string); ok {
			result = append(result, map[string]any{
				"name":   name,
				"status": s,
			})
		} else if m, ok := status.(map[string]any); ok {
			result = append(result, map[string]any{
				"name":   name,
				"status": "healthy",
				"message": fmt.Sprintf("%v", m),
			})
		}
	}

	c.JSON(http.StatusOK, result)
}
