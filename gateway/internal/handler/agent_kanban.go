package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentkanban"
	"github.com/xiaotian-quant/gateway/internal/agentprofiles"
)

// ── 看板 REST（前端看板面板）：全部限定当前登录用户 ──

// AgentKanbanList GET /api/agent/kanban
func AgentKanbanList(c *gin.Context) {
	cards, err := agentkanban.NewRepo().ListByUser(int64(aiBotUserID(c)), "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "cards": cards})
}

type agentKanbanCreateRequest struct {
	Title       string `json:"title" binding:"required"`
	Description string `json:"description"`
	Column      string `json:"column"`
}

// AgentKanbanCreate POST /api/agent/kanban（面板手动添加，created_by=user）
func AgentKanbanCreate(c *gin.Context) {
	var req agentKanbanCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request: " + err.Error()})
		return
	}
	if req.Column != "" && !agentkanban.ValidColumn(req.Column) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "column must be todo/doing/done"})
		return
	}
	card := &agentkanban.Card{
		ID:          agentkanban.NewID(),
		UserID:      int64(aiBotUserID(c)),
		Title:       req.Title,
		Description: req.Description,
		Column:      req.Column,
		Assignee:    "user",
		CreatedBy:   "user",
		ProfileID:   agentprofiles.NewRepo().ActiveProfileID(int64(aiBotUserID(c))), // 写入当前激活档案
	}
	if err := agentkanban.NewRepo().Create(card); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "id": card.ID})
}

type agentKanbanUpdateRequest struct {
	Column      *string `json:"column"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Comment     *string `json:"comment"`
}

// AgentKanbanUpdate PUT /api/agent/kanban/:id（仅卡主）
func AgentKanbanUpdate(c *gin.Context) {
	var req agentKanbanUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request: " + err.Error()})
		return
	}
	if req.Column != nil && !agentkanban.ValidColumn(*req.Column) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "column must be todo/doing/done"})
		return
	}
	repo := agentkanban.NewRepo()
	uid := int64(aiBotUserID(c))
	card, err := repo.GetByID(c.Param("id"), uid)
	if err != nil {
		writeKanbanNotFound(c, err)
		return
	}
	if req.Column != nil {
		card.Column = *req.Column
	}
	if req.Title != nil {
		card.Title = *req.Title
	}
	if req.Description != nil {
		card.Description = *req.Description
	}
	if req.Comment != nil {
		card.Comment = *req.Comment
	}
	if err := repo.Update(card); err != nil {
		writeKanbanNotFound(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AgentKanbanDelete DELETE /api/agent/kanban/:id（仅卡主）
func AgentKanbanDelete(c *gin.Context) {
	if err := agentkanban.NewRepo().Delete(c.Param("id"), int64(aiBotUserID(c))); err != nil {
		writeKanbanNotFound(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// writeKanbanNotFound 不存在/他人卡统一 404（不泄露存在性），其余错误 500。
func writeKanbanNotFound(c *gin.Context, err error) {
	if errors.Is(err, agentkanban.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"detail": "card not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
}
