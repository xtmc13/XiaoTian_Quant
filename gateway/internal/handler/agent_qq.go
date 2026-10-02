package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentqq"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
)

// ── QQ 通道 REST（前端接入面板） ──

// AgentQqStatus GET /api/agent/qq/status —— 配置态 + 绑定态。
func AgentQqStatus(c *gin.Context) {
	link, err := agentqq.NewRepo().GetByUserID(int64(aiBotUserID(c)))
	openID := ""
	if err == nil {
		openID = link.OpenID
	}
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"configured": agentqq.Configured(),
		"linked":     err == nil,
		"open_id":    openID,
	})
}

// AgentQqPairCode POST /api/agent/qq/pair-code —— 生成 6 位配对码。
func AgentQqPairCode(c *gin.Context) {
	if !agentqq.Configured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "QQ 未配置（QQ_APP_ID/QQ_APP_SECRET），请配置后重启网关"})
		return
	}
	code, err := agentqq.NewRepo().CreatePairCode(int64(aiBotUserID(c)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "code": code, "expires_in": int(agentqq.PairCodeTTL.Seconds())})
}

// AgentQqUnlink POST /api/agent/qq/unlink
func AgentQqUnlink(c *gin.Context) {
	if err := agentqq.NewRepo().Unlink(int64(aiBotUserID(c))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AgentQqUrlLink POST /api/agent/qq/url-link —— 「添加机器人」分享链接（前端渲染成二维码）。
func AgentQqUrlLink(c *gin.Context) {
	if !agentqq.Configured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "QQ 未配置（QQ_APP_ID/QQ_APP_SECRET），请配置后重启网关"})
		return
	}
	var body struct {
		CallbackData string `json:"callback_data"`
	}
	_ = c.ShouldBindJSON(&body)
	url, err := builtin.QqBot().GenerateURLLink(body.CallbackData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "url": url})
}

// AgentQqConnectorQr POST /api/agent/qq/connector/qr —— 启动/重启 QQ 扫码连接会话，返回二维码落地页 URL。
// 手机 QQ 扫码确认后 sidecar 持久化 AppID/AppSecret，网关自动接管 QQ 通道。
func AgentQqConnectorQr(c *gin.Context) {
	var body struct {
		Restart bool `json:"restart"`
	}
	_ = c.ShouldBindJSON(&body)
	url, err := agentqq.ConnectorQRStart(body.Restart)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "扫码连接器不可用：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"qr_url": url})
}

// AgentQqConnectorStatus GET /api/agent/qq/connector/status —— 扫码进展轮询。
// state ∈ idle/waiting/success/error；success 时网关已注入凭据并自动启动通道。
func AgentQqConnectorStatus(c *gin.Context) {
	state, qrURL, appID, errMsg := agentqq.ConnectorQRStatus()
	out := gin.H{"state": state, "qr_url": qrURL}
	if state == "success" {
		out["app_id"] = appID
		out["configured"] = true
	}
	if errMsg != "" {
		out["error"] = errMsg
	}
	c.JSON(http.StatusOK, out)
}
