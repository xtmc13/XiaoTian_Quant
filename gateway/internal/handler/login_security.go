package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// recordLoginSecurity 记录本次登录 IP；若 IP 变化且用户未启用 MFA，
// 通过 notify 模块发风险登录提醒（A3.1 风险登录）。
func recordLoginSecurity(c *gin.Context, userID int, mfaEnabled bool) {
	ip := c.ClientIP()
	prev := store.UpdateLastLoginIP(userID, ip)
	if prev == "" || prev == ip || mfaEnabled {
		return
	}
	notify.GetNotificationStore().Add(
		"检测到新 IP 地址登录",
		"你的账户刚刚从新的 IP 地址 "+ip+" 登录（上一次为 "+prev+"）。你尚未启用两步验证（MFA），建议立即前往 设置 → 安全 开启。",
		"WARN",
		"system",
	)
}
