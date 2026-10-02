package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentdingtalk"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
)

// ── 钉钉通道 REST（前端接入面板） ──

// AgentDingtalkStatus GET /api/agent/dingtalk/status —— 配置态 + 绑定态。
func AgentDingtalkStatus(c *gin.Context) {
	link, err := agentdingtalk.NewRepo().GetByUserID(int64(aiBotUserID(c)))
	staffID := ""
	if err == nil {
		staffID = link.StaffID
	}
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"configured": agentdingtalk.Configured(),
		"linked":     err == nil,
		"staff_id":   staffID,
	})
}

// AgentDingtalkPairCode POST /api/agent/dingtalk/pair-code —— 生成 6 位配对码。
func AgentDingtalkPairCode(c *gin.Context) {
	if !agentdingtalk.Configured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "钉钉未配置（DINGTALK_CLIENT_ID/DINGTALK_CLIENT_SECRET 或 config.yaml agent.dingtalk），请配置后重启网关"})
		return
	}
	code, err := agentdingtalk.NewRepo().CreatePairCode(int64(aiBotUserID(c)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "code": code, "expires_in": int(agentdingtalk.PairCodeTTL.Seconds())})
}

// AgentDingtalkUnlink POST /api/agent/dingtalk/unlink
func AgentDingtalkUnlink(c *gin.Context) {
	if err := agentdingtalk.NewRepo().Unlink(int64(aiBotUserID(c))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AgentDingtalkWebhook POST /api/agent/dingtalk/webhook —— 公开路由（无 JWT）：
// outgoing 机器人回调（HMAC-SHA256 验签），处理在 bot 包内。
func AgentDingtalkWebhook(c *gin.Context) {
	builtin.DingtalkBot().HandleWebhook(c.Writer, c.Request)
}
