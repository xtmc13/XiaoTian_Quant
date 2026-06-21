package handler

import (
	"net/http"
	"os"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// liveTradingOverride allows runtime enable/disable of live trading.
// 0 = use env var, 1 = force enabled, 2 = force disabled.
var liveTradingOverride int32

func isLiveTradingEnabledRuntime() bool {
	switch atomic.LoadInt32(&liveTradingOverride) {
	case 1:
		return true
	case 2:
		return false
	default:
		return os.Getenv("LIVE_TRADING_ENABLED") == "true"
	}
}

// GetTradingSafetyStatus returns the current live trading safety status.
func GetTradingSafetyStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"live_trading_enabled":    isLiveTradingEnabledRuntime(),
		"confirm_required":        isConfirmRequired(),
		"paper_trading_default":   true,
		"env_live_trading":        os.Getenv("LIVE_TRADING_ENABLED") == "true",
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
