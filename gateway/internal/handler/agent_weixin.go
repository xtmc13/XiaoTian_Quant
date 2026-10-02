package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/xiaotian-quant/gateway/internal/agentweixin"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
)

// ── 微信通道 REST（前端 QR 登录接入面板；腾讯官方 iLink Bot API） ──

// AgentWeixinStatus GET /api/agent/weixin/status —— 登录态 + 当前用户绑定态。
func AgentWeixinStatus(c *gin.Context) {
	st, err := agentweixin.NewRepo().LoadLogin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	link, linkErr := agentweixin.NewRepo().GetByUserID(int64(aiBotUserID(c)))
	wxid := ""
	if linkErr == nil {
		wxid = link.WXID
	}
	loggedIn := st != nil && st.BotToken != ""
	botID := ""
	if loggedIn {
		botID = st.BotID
	}
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"configured": agentweixin.Configured(),
		"logged_in":  loggedIn,
		"bot_id":     botID,
		"linked":     linkErr == nil,
		"wxid":       wxid,
	})
}

// AgentWeixinQrcode POST /api/agent/weixin/qrcode —— 生成登录二维码并后台轮询登录进展。
func AgentWeixinQrcode(c *gin.Context) {
	img, expiresIn, err := builtin.WeixinBot().StartQRLogin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "qrcode_img": img, "expires_in": expiresIn})
}

// AgentWeixinQrcodeStatus GET /api/agent/weixin/qrcode-status —— QR 登录进展轮询。
// status ∈ none/wait/scaned/confirmed/expired/binded_redirect/need_verifycode/verify_code_blocked/error。
func AgentWeixinQrcodeStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "status": builtin.WeixinBot().QRStatus()})
}

// AgentWeixinUnlink POST /api/agent/weixin/unlink —— 解绑机器人：清 bot_token、停长轮询。
func AgentWeixinUnlink(c *gin.Context) {
	if err := builtin.WeixinBot().Unlink(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
