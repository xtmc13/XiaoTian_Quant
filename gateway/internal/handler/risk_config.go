package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/risk"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// RiskConfigPayload 是 GET/PUT /api/risk/config 的契约：
// max_concurrent_orders（1-50 整数）、position_limit_pct（百分比类，
// 单笔订单名义价值占账户权益比例上限 %，C2.1 起限定 1-100——历史上
// 曾被调到 2500% 导致风控形同虚设，PUT 越界一律 400）、
// profit_protection_enabled（盈利保护开关）、
// indicator_fail_open（自定义指标开仓检查失败时放行，默认 true）。
type RiskConfigPayload struct {
	MaxConcurrentOrders     int     `json:"max_concurrent_orders"`
	PositionLimitPct        float64 `json:"position_limit_pct"`
	ProfitProtectionEnabled bool    `json:"profit_protection_enabled"`
	IndicatorFailOpen       bool    `json:"indicator_fail_open"`
}

// GetRiskConfig 返回当前生效的风控参数。来源 = 运行时内存（risk manager 启动时
// 以 config.yaml risk 段初始化，PUT 后即为覆盖值），盈利保护/指标失败放行开关
// 为包级原子变量。
func GetRiskConfig(c *gin.Context) {
	cfg := risk.GetManager().Config()
	c.JSON(http.StatusOK, RiskConfigPayload{
		MaxConcurrentOrders:     cfg.MaxConcurrentOrders,
		PositionLimitPct:        cfg.MaxPositionPct,
		ProfitProtectionEnabled: risk.ProfitProtectionEnabled(),
		IndicatorFailOpen:       risk.IndicatorFailOpen(),
	})
}

// UpdateRiskConfig（仅 admin）校验并持久化风控参数：写 config.yaml risk 段
// （保留其他键）+ risk manager UpdateConfig 立即重建检查链 + 盈利保护原子开关。
func UpdateRiskConfig(c *gin.Context) {
	var body RiskConfigPayload
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}
	if body.MaxConcurrentOrders < 1 || body.MaxConcurrentOrders > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "max_concurrent_orders 必须在 1-50 之间"})
		return
	}
	// C2.1: 百分比类参数限定 0-100。position_limit_pct 曾被调到 2500%
	// 使仓位风控失效；>100 视为非法（0/负值无意义，等同关闭检查，拒绝）。
	if body.PositionLimitPct <= 0 || body.PositionLimitPct > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "position_limit_pct 必须在 1-100 之间（百分比上限 100%）"})
		return
	}

	// 1) 持久化到 config.yaml risk 段（合并写回，保留其他键）。
	patch := map[string]any{
		"max_concurrent_orders":     body.MaxConcurrentOrders,
		"position_limit_pct":        body.PositionLimitPct,
		"profit_protection_enabled": body.ProfitProtectionEnabled,
		"indicator_fail_open":       body.IndicatorFailOpen,
	}
	if err := store.SaveRiskSection(patch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "保存配置失败: " + err.Error()})
		return
	}

	// 2) 运行时立即生效：risk manager 重建检查链 + 盈利保护原子开关。
	mgr := risk.GetManager()
	cur := mgr.Config()
	cur.MaxConcurrentOrders = body.MaxConcurrentOrders
	cur.MaxPositionPct = body.PositionLimitPct
	mgr.UpdateConfig(cur)
	risk.SetProfitProtectionEnabled(body.ProfitProtectionEnabled)
	risk.SetIndicatorFailOpen(body.IndicatorFailOpen)

	c.JSON(http.StatusOK, body)
}
