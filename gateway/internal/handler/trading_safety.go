package handler

import (
	"fmt"
	"log"
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

// relaxedRiskBlockMessage 返回非空字符串表示当前 config 的
// risk.position_limit_pct 是 paper 放宽值（>100）且未设逃逸开关，
// 实盘解锁必须拒绝。C2.1 硬校验（2026-09-20）：HANDOFF 红线记载的
// 2500 放宽值此前只口头约定"开实盘前回调"，本闸把它变成硬拦截。
// 注意必须读 config 原始值：risk manager 启动时已把 >100 钳回 100
// （app/context.go），读 manager 的运行时配置会漏检"配置未回调"。
func relaxedRiskBlockMessage() string {
	limit := config.Get().Risk.PositionLimit
	if limit <= 100 {
		return ""
	}
	if os.Getenv("XIAOTIAN_ALLOW_RELAXED_RISK") == "1" {
		log.Printf("[WARN] [trading-safety] 实盘解锁放行：risk.position_limit_pct=%.4g 为 paper 放宽值（非实盘安全值），依赖逃逸开关 XIAOTIAN_ALLOW_RELAXED_RISK=1；请尽快将配置回调到 ≤100", limit)
		return ""
	}
	return fmt.Sprintf("拒绝解锁实盘：risk.position_limit_pct 当前为 %.4g%%，是 paper 模式放宽值，非实盘安全值。请先将 config.yaml 中该值回调到 ≤100，或显式设置环境变量 XIAOTIAN_ALLOW_RELAXED_RISK=1 后再解锁", limit)
}

// UnlockLiveTrading enables live trading at runtime (admin only).
func UnlockLiveTrading(c *gin.Context) {
	if !isAdminContext(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return
	}
	if msg := relaxedRiskBlockMessage(); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
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
