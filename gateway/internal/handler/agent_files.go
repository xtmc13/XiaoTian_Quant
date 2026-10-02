package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentfiles"
	"github.com/xiaotian-quant/gateway/internal/plugins/builtin"
)

// ── 文件检查点 REST（文件回滚面板）：管理员专属（普通用户 403）──

// requireAgentAdmin 管理员校验；非管理员写 403 并返回 false。
func requireAgentAdmin(c *gin.Context) bool {
	if c.GetString("role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "detail": "Admin access required"})
		return false
	}
	return true
}

// AgentFileCheckpoints GET /api/agent/files/checkpoints（管理员）
// 契约：{"success":true,"checkpoints":[{"id","path","size","conversation_id","created_at"}]}（当前用户的检查点）。
func AgentFileCheckpoints(c *gin.Context) {
	if !requireAgentAdmin(c) {
		return
	}
	cps, err := agentfiles.NewRepo().ListByUser(int64(aiBotUserID(c)), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(cps))
	for _, cp := range cps {
		out = append(out, gin.H{
			"id":              cp.ID,
			"path":            cp.Path,
			"size":            cp.Size,
			"conversation_id": cp.ConversationID,
			"created_at":      cp.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "checkpoints": out})
}

type agentFileRollbackRequest struct {
	CheckpointID string `json:"checkpoint_id" binding:"required"`
}

// AgentFileRollback POST /api/agent/files/rollback（管理员）
// 契约：{"success":true,"restored":"..."}（备份内容写回原路径，检查点行保留）。
func AgentFileRollback(c *gin.Context) {
	if !requireAgentAdmin(c) {
		return
	}
	var req agentFileRollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request: " + err.Error()})
		return
	}
	restored, err := builtin.FilesPlugin().Rollback(int64(aiBotUserID(c)), req.CheckpointID)
	if err != nil {
		if errors.Is(err, agentfiles.ErrCheckpointNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "detail": "checkpoint not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "restored": restored})
}
