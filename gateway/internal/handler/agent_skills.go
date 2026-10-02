package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentskills"
)

// ── 技能 REST（前端面板）：全部限定当前登录用户 ──

// AgentSkillsList GET /api/agent/skills
func AgentSkillsList(c *gin.Context) {
	skills, err := agentskills.NewRepo().ListByUser(int64(aiBotUserID(c)), 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"skills": skills})
}

type agentSkillCreateRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Body        string `json:"body" binding:"required"`
}

// AgentSkillCreate POST /api/agent/skills（面板手动创建；同名覆盖）
func AgentSkillCreate(c *gin.Context) {
	var req agentSkillCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request: " + err.Error()})
		return
	}
	s := &agentskills.Skill{
		ID:          agentskills.NewID(),
		UserID:      int64(aiBotUserID(c)),
		Name:        req.Name,
		Description: req.Description,
		Body:        req.Body,
		Source:      "user",
	}
	if err := agentskills.NewRepo().Upsert(s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"skill": s})
}

// AgentSkillDelete DELETE /api/agent/skills/:id
func AgentSkillDelete(c *gin.Context) {
	if err := agentskills.NewRepo().Delete(c.Param("id"), int64(aiBotUserID(c))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
