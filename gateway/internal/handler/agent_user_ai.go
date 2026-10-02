package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 用户级 AI 配置端点：GET/PUT/DELETE /api/agent/user-ai-config ──
// 契约（前端已按此实现）：GET 永不回传 api_key（仅 has_key）；PUT 整体覆盖；DELETE 清除。

// GetAgentUserAIConfig GET：{"success":true,"provider","model","has_key"}。
func GetAgentUserAIConfig(c *gin.Context) {
	uid := int64(aiBotUserID(c))
	rec, err := store.NewAgentUserAIRepo().Get(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": gin.H{"code": "STORE_ERROR", "message": err.Error()}})
		return
	}
	out := gin.H{"success": true, "provider": "", "model": "", "has_key": false}
	if rec != nil {
		out["provider"] = rec.Provider
		out["model"] = rec.Model
		out["has_key"] = rec.APIKey != ""
	}
	c.JSON(http.StatusOK, out)
}

// PutAgentUserAIConfig PUT：{"provider","api_key","model"} → {"success":true}。
// provider 必填（空 = 无覆盖语义，应走 DELETE 清除）。
func PutAgentUserAIConfig(c *gin.Context) {
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		Model    string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "BAD_REQUEST", "message": "invalid request body"}})
		return
	}
	provider := ai.NormalizeProviderName(strings.TrimSpace(req.Provider))
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": "BAD_REQUEST", "message": "provider 不能为空"}})
		return
	}
	uid := int64(aiBotUserID(c))
	rec := &store.AgentUserAIRecord{
		UserID:   uid,
		Provider: provider,
		Model:    strings.TrimSpace(req.Model),
		APIKey:   strings.TrimSpace(req.APIKey),
	}
	if err := store.NewAgentUserAIRepo().Upsert(rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": gin.H{"code": "STORE_ERROR", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteAgentUserAIConfig DELETE：清除用户级覆盖 → {"success":true}。
func DeleteAgentUserAIConfig(c *gin.Context) {
	uid := int64(aiBotUserID(c))
	if err := store.NewAgentUserAIRepo().Delete(uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": gin.H{"code": "STORE_ERROR", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
