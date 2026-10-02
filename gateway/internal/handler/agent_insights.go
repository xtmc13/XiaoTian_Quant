package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentskills"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── AI Agent 学习闭环观测：用量报告（insights）与学习轨迹（journey） ──
// 挂 registerAgentRoutes 的 agent 组（JWT 鉴权），全部限定当前登录用户。

// AgentInsights 处理 GET /agent/insights?days=30。
// 响应 {"success":true,"days":30,"active_days":N,"totals":{...},"by_day":[...],
// "by_model":[...],"top_tools":[...],"top_skills":[...],
// "memories_added":N,"skills_added":N}。
func AgentInsights(c *gin.Context) {
	uid := int64(aiBotUserID(c))
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	if days <= 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days).Unix()
	repo := store.DefaultAgentChatRepo()

	totals, activeDays, err := repo.GetInsightsTotals(uid, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "insights failed: " + err.Error()})
		return
	}
	byDay, err := repo.GetInsightsByDay(uid, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "insights failed: " + err.Error()})
		return
	}
	byModel, err := repo.GetInsightsByModel(uid, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "insights failed: " + err.Error()})
		return
	}
	topTools, err := repo.GetTopTools(uid, since, 8)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "insights failed: " + err.Error()})
		return
	}
	memoriesAdded, skillsAdded, err := repo.GetLearningAdded(uid, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "insights failed: " + err.Error()})
		return
	}
	// 技能调用排行：usage_count 累计降序前 8（常用在前）
	topSkills := make([]gin.H, 0, 8)
	if skills, err := agentskills.NewRepo().ListByUser(uid, 8); err == nil {
		for _, s := range skills {
			topSkills = append(topSkills, gin.H{"name": s.Name, "count": s.UsageCount})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"days":           days,
		"active_days":    activeDays,
		"totals":         totals,
		"by_day":         byDay,
		"by_model":       byModel,
		"top_tools":      topTools,
		"top_skills":     topSkills,
		"memories_added": memoriesAdded,
		"skills_added":   skillsAdded,
	})
}

// AgentJourney 处理 GET /agent/journey?limit=50。
// 响应 {"success":true,"items":[{"ts","kind","title","detail"}]}（ts 倒序）。
func AgentJourney(c *gin.Context) {
	uid := int64(aiBotUserID(c))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, err := store.DefaultAgentChatRepo().GetJourneyItems(uid, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "journey failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "items": items})
}
