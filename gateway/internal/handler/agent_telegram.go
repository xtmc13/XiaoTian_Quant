package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agenttelegram"
)

// ── Telegram 通道 REST（前端接入面板） ──

// AgentTelegramStatus GET /api/agent/telegram —— 配置态 + 绑定态。
func AgentTelegramStatus(c *gin.Context) {
	link, err := agenttelegram.NewRepo().GetByUserID(int64(aiBotUserID(c)))
	out := gin.H{
		"configured": agenttelegram.Configured(),
		"linked":     err == nil,
	}
	if err == nil {
		out["chat_id"] = link.TelegramChatID
		out["username"] = link.TelegramUsername
	}
	c.JSON(http.StatusOK, out)
}

// AgentTelegramPairCode POST /api/agent/telegram/pair-code —— 生成 6 位配对码。
func AgentTelegramPairCode(c *gin.Context) {
	if !agenttelegram.Configured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "TELEGRAM_BOT_TOKEN 未配置，请设置环境变量并重启网关"})
		return
	}
	code, err := agenttelegram.NewRepo().CreatePairCode(int64(aiBotUserID(c)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": code, "expires_in": int(agenttelegram.PairCodeTTL.Seconds())})
}

// AgentTelegramUnlink POST /api/agent/telegram/unlink
func AgentTelegramUnlink(c *gin.Context) {
	if err := agenttelegram.NewRepo().Unlink(int64(aiBotUserID(c))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"unlinked": true})
}
