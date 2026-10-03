package handler

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

// ── 沙箱文件读取（Artifacts 预览/下载）：登录用户可读，与文件工具同一套路径安全校验 ──

// agentFileContentMaxBytes 单文件读取上限（预览场景，8MB 足够覆盖 HTML/图表/文本产物）。
const agentFileContentMaxBytes = 8 << 20

// AgentFileContent GET /api/agent/files/content?path=...&download=1
// 契约：200 直接回文件流（inline 预览或 attachment 下载）；404/400/403 回 JSON 错误。
// 根目录按请求角色选：管理员→开放根（真实项目工作区），普通用户→沙箱根（B 方案隔离）。
func AgentFileContent(c *gin.Context) {
	fp := builtin.FilesPlugin()
	root := fp.Root
	if c.GetString("role") == "admin" && fp.AdminRoot != "" {
		root = fp.AdminRoot
	}
	abs, _, err := agentfiles.Resolve(root, c.Query("path"))
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "detail": err.Error()})
		return
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "detail": "file not found"})
		return
	}
	if info.Size() > agentFileContentMaxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "detail": "file too large for preview (max 8MB)"})
		return
	}
	// 显式设置 Content-Type（ServeFile 不会覆盖已设置的值），download=1 强制附件下载
	c.Header("Content-Type", agentFileContentType(abs))
	c.Header("X-Content-Type-Options", "nosniff")
	if c.Query("download") == "1" {
		c.Header("Content-Disposition", "attachment; filename=\""+filepath.Base(abs)+"\"")
	}
	c.File(abs)
}

// agentFileContentType 按扩展名映射预览 Content-Type；未知类型一律 octet-stream。
func agentFileContentType(name string) string {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "html", "htm":
		return "text/html; charset=utf-8"
	case "svg":
		return "image/svg+xml"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "ico":
		return "image/x-icon"
	case "pdf":
		return "application/pdf"
	case "txt", "log":
		return "text/plain; charset=utf-8"
	case "md", "markdown":
		return "text/plain; charset=utf-8"
	case "csv":
		return "text/csv; charset=utf-8"
	case "json":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
