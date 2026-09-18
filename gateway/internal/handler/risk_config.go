package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/risk"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// RiskConfigPayload 是 GET/PUT /api/risk/config 的契约：
// max_concurrent_orders（1-50 整数）、position_limit_pct（1-10000，订单名义价值
// 占账户权益比例上限%）、profit_protection_enabled（盈利保护开关）。
type RiskConfigPayload struct {
	MaxConcurrentOrders     int     `json:"max_concurrent_orders"`
	PositionLimitPct        float64 `json:"position_limit_pct"`
	ProfitProtectionEnabled bool    `json:"profit_protection_enabled"`
}

// GetRiskConfig 返回当前生效的风控参数。来源 = 运行时内存（risk manager 启动时
// 以 config.yaml risk 段初始化，PUT 后即为覆盖值），盈利保护开关为包级原子变量。
func GetRiskConfig(c *gin.Context) {
	cfg := risk.GetManager().Config()
	c.JSON(http.StatusOK, RiskConfigPayload{
		MaxConcurrentOrders:     cfg.MaxConcurrentOrders,
		PositionLimitPct:        cfg.MaxPositionPct,
		ProfitProtectionEnabled: risk.ProfitProtectionEnabled(),
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
	if body.PositionLimitPct < 1 || body.PositionLimitPct > 10000 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "position_limit_pct 必须在 1-10000 之间"})
		return
	}

	// 1) 持久化到 config.yaml risk 段（合并写回，保留其他键）。
	patch := map[string]any{
		"max_concurrent_orders":     body.MaxConcurrentOrders,
		"position_limit_pct":        body.PositionLimitPct,
		"profit_protection_enabled": body.ProfitProtectionEnabled,
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

	c.JSON(http.StatusOK, body)
}
