package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentwecom"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
)

// ── 企业微信通道 REST（前端接入面板） ──

// AgentWecomStatus GET /api/agent/wecom/status —— 配置态 + 绑定态。
func AgentWecomStatus(c *gin.Context) {
	link, err := agentwecom.NewRepo().GetByUserID(int64(aiBotUserID(c)))
	staffID := ""
	if err == nil {
		staffID = link.StaffID
	}
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"configured": agentwecom.Configured(),
		"linked":     err == nil,
		"staff_id":   staffID,
	})
}

// AgentWecomPairCode POST /api/agent/wecom/pair-code —— 生成 6 位配对码。
func AgentWecomPairCode(c *gin.Context) {
	if !agentwecom.Configured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "企业微信未配置（WECOM_CORP_ID/WECOM_SECRET/WECOM_TOKEN/WECOM_AES_KEY），请配置后重启网关"})
		return
	}
	code, err := agentwecom.NewRepo().CreatePairCode(int64(aiBotUserID(c)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "code": code, "expires_in": int(agentwecom.PairCodeTTL.Seconds())})
}

// AgentWecomUnlink POST /api/agent/wecom/unlink
func AgentWecomUnlink(c *gin.Context) {
	if err := agentwecom.NewRepo().Unlink(int64(aiBotUserID(c))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AgentWecomWebhook GET+POST /api/agent/wecom/webhook —— 公开路由（无 JWT）：
// GET URL 验证（echostr），POST 消息回调（WXBizMsgCrypt 验签 + 解密），处理在 bot 包内。
func AgentWecomWebhook(c *gin.Context) {
	builtin.WecomBot().HandleWebhook(c.Writer, c.Request)
}
