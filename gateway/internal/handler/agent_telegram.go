package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agenttelegram"
	"github.com/xiaotian-quant/gateway/internal/store"
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

// AgentConversationHandoff POST /api/agent/conversations/:id/handoff —— 生成 6 位一次性
// 移交码（10 分钟有效）；TG 端 /handoff <code> 消费后该 chat 的消息在此会话中续跑。
func AgentConversationHandoff(c *gin.Context) {
	if !agenttelegram.Configured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "TELEGRAM_BOT_TOKEN 未配置，请设置环境变量并重启网关"})
		return
	}
	rec, err := store.DefaultAgentChatRepo().GetConversation(c.Param("id"))
	if err != nil || rec == nil || rec.UserID != int64(aiBotUserID(c)) {
		c.JSON(http.StatusNotFound, gin.H{"detail": "conversation not found"})
		return
	}
	code, err := agenttelegram.NewRepo().CreateHandoffCode(int64(aiBotUserID(c)), rec.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "code": code, "expires_in": int(agenttelegram.HandoffCodeTTL.Seconds())})
}

// AgentGatewayHealthSummary TG /status 的网关健康一行（默认 AI provider/模型）。
func AgentGatewayHealthSummary() string {
	p, name := configuredAgentAIProvider()
	if p == nil || p.APIKey == "" {
		return "AI provider '" + name + "' 未配置 API Key"
	}
	return "网关运行中 · AI: " + name + "/" + p.Model
}
