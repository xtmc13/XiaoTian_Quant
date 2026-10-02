package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentmemory"
)

// ── 记忆 REST（前端面板）：全部限定当前登录用户 ──

// AgentMemoryList GET /api/agent/memory?kind=
func AgentMemoryList(c *gin.Context) {
	kind := c.Query("kind")
	mem, err := agentmemory.NewRepo().ListByUser(int64(aiBotUserID(c)), kind, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"memories": mem})
}

type agentMemoryCreateRequest struct {
	Content    string `json:"content" binding:"required"`
	Kind       string `json:"kind"`
	Importance int    `json:"importance"`
}

// AgentMemoryCreate POST /api/agent/memory（面板手动添加）
func AgentMemoryCreate(c *gin.Context) {
	var req agentMemoryCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request: " + err.Error()})
		return
	}
	m := &agentmemory.Memory{
		ID:         agentmemory.NewID(),
		UserID:     int64(aiBotUserID(c)),
		Kind:       req.Kind,
		Content:    req.Content,
		Importance: req.Importance,
	}
	if err := agentmemory.NewRepo().Create(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"memory": m})
}

// AgentMemoryDelete DELETE /api/agent/memory/:id
func AgentMemoryDelete(c *gin.Context) {
	if err := agentmemory.NewRepo().Delete(c.Param("id"), int64(aiBotUserID(c))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
