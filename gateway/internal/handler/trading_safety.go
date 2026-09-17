package handler

import (
	"net/http"
	"os"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/config"
)

// liveTradingOverride allows runtime enable/disable of live trading.
// 0 = use env var / config.yaml, 1 = force enabled, 2 = force disabled.
var liveTradingOverride int32

// isLiveTradingEnabledRuntime 实盘总闸三态：运行时覆盖（管理员解锁/锁定）
// > 环境变量 LIVE_TRADING_ENABLED > config.yaml trading.live_enabled。
// 默认 false——任何一环未显式开启即视为未开启。
func isLiveTradingEnabledRuntime() bool {
	switch atomic.LoadInt32(&liveTradingOverride) {
	case 1:
		return true
	case 2:
		return false
	default:
		if os.Getenv("LIVE_TRADING_ENABLED") == "true" {
			return true
		}
		return config.Get().Trading.LiveEnabled
	}
}

// GetTradingSafetyStatus returns the current live trading safety status.
func GetTradingSafetyStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"live_trading_enabled":    isLiveTradingEnabledRuntime(),
		"confirm_required":        isConfirmRequired(),
		"paper_trading_default":   true,
		"env_live_trading":        os.Getenv("LIVE_TRADING_ENABLED") == "true",
		"config_live_trading":     config.Get().Trading.LiveEnabled,
		"runtime_override_locked": atomic.LoadInt32(&liveTradingOverride) != 0,
	})
}

func isAdminContext(c *gin.Context) bool {
	return c.GetString("role") == "admin"
}

// UnlockLiveTrading enables live trading at runtime (admin only).
func UnlockLiveTrading(c *gin.Context) {
	if !isAdminContext(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	atomic.StoreInt32(&liveTradingOverride, 1)
	c.JSON(http.StatusOK, gin.H{"message": "实盘交易已启用（运行时，重启后需重新解锁或配置环境变量）"})
}

// LockLiveTrading disables live trading at runtime (admin only).
func LockLiveTrading(c *gin.Context) {
	if !isAdminContext(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	atomic.StoreInt32(&liveTradingOverride, 2)
	c.JSON(http.StatusOK, gin.H{"message": "实盘交易已禁用"})
}
