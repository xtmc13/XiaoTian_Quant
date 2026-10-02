package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentfeishu"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
)

// ── 飞书通道 REST（前端接入面板） ──

// AgentFeishuStatus GET /api/agent/feishu/status —— 配置态 + 绑定态。
func AgentFeishuStatus(c *gin.Context) {
	link, err := agentfeishu.NewRepo().GetByUserID(int64(aiBotUserID(c)))
	openID := ""
	if err == nil {
		openID = link.OpenID
	}
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"configured": agentfeishu.Configured(),
		"linked":     err == nil,
		"open_id":    openID,
	})
}

// AgentFeishuPairCode POST /api/agent/feishu/pair-code —— 生成 6 位配对码。
func AgentFeishuPairCode(c *gin.Context) {
	if !agentfeishu.Configured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "飞书未配置（FEISHU_APP_ID/FEISHU_APP_SECRET 或 config.yaml agent.feishu），请配置后重启网关"})
		return
	}
	code, err := agentfeishu.NewRepo().CreatePairCode(int64(aiBotUserID(c)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "code": code, "expires_in": int(agentfeishu.PairCodeTTL.Seconds())})
}

// AgentFeishuUnlink POST /api/agent/feishu/unlink
func AgentFeishuUnlink(c *gin.Context) {
	if err := agentfeishu.NewRepo().Unlink(int64(aiBotUserID(c))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AgentFeishuWebhook POST /api/agent/feishu/webhook —— 公开路由（无 JWT）：
// url_verification 握手 + im.message.receive_v1 事件回调，处理在 bot 包内。
func AgentFeishuWebhook(c *gin.Context) {
	builtin.FeishuBot().HandleWebhook(c.Writer, c.Request)
}
