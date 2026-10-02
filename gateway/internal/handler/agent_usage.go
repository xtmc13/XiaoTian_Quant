package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── AI Agent 用量统计：GET /agent/usage ──
// 汇总当前用户的 agent 对话 token 用量与耗时：会话维度（conversation_id 指定时）、
// 用户全量、最近 days 天按日、按 "provider:model"。挂 registerAgentRoutes 的
// agent 组（JWT 鉴权），指定会话时校验属主（非属主统一 404）。

// AgentUsageSummary 处理 GET /agent/usage?conversation_id=&days=30。
// 响应 {"success":true,"session":{...}|null,"totals":{...},"by_day":[...],"by_model":[...]}。
func AgentUsageSummary(c *gin.Context) {
	uid := int64(aiBotUserID(c))
	convID := c.Query("conversation_id")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	if days <= 0 {
		days = 30
	}
	repo := store.DefaultAgentChatRepo()
	if convID != "" {
		// 指定会话：校验存在且属主（非属主不暴露资源存在性）
		rec, err := repo.GetConversation(convID)
		if err != nil || rec == nil || rec.UserID != uid {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
	}
	summary, err := repo.GetUsageSummary(strconv.FormatInt(uid, 10), convID, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "usage summary failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"session":  summary.Session,
		"totals":   summary.Totals,
		"by_day":   summary.ByDay,
		"by_model": summary.ByModel,
	})
}
