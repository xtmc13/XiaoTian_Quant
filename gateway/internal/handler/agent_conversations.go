package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── AI Agent 会话 CRUD：/agent/conversations ──
// 悬浮 AI 助手多会话持久化。全部走 JWT 鉴权（挂 registerAgentRoutes 的 agent 组），
// 按 aiBotUserID 隔离，非属主访问统一 404（不暴露资源存在性）。
// tool_calls 为 JSON 字符串数组，原样存取（repo 层不做序列化，读出原样返回）。

// agentConversationMsgs 会话消息响应体。tool_calls 存储为 JSON 字符串，
// 响应时解析为数组返回（前端按数组渲染工具卡片）。
type agentConversationMsg struct {
	ID        int64           `json:"id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Reasoning string          `json:"reasoning"`
	ToolCalls json.RawMessage `json:"tool_calls"`
	CreatedAt int64           `json:"created_at"`
}

// AgentConversationCreate 处理 POST /agent/conversations：body{title?} → {success,id,title}。
func AgentConversationCreate(c *gin.Context) {
	var body struct {
		Title string `json:"title"`
	}
	// body 全可选：空 body / 非法 JSON 视为无标题。
	if c.Request.ContentLength > 0 {
		_ = c.ShouldBindJSON(&body)
	}
	rec := &store.AgentConversationRecord{
		UserID: int64(aiBotUserID(c)),
		Title:  strings.TrimSpace(body.Title),
	}
	if err := store.DefaultAgentChatRepo().CreateConversation(rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create conversation failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "id": rec.ID, "title": rec.Title})
}

// AgentConversationsList 处理 GET /agent/conversations?limit=50&offset=0。
// 按 updated_at 倒序返回当前用户的会话。
func AgentConversationsList(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	convs, err := store.DefaultAgentChatRepo().ListConversations(int64(aiBotUserID(c)), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list conversations failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "conversations": convs})
}

// AgentConversationGet 处理 GET /agent/conversations/:id：会话 + 全部消息。
// 非属主或不存在 → 404。
func AgentConversationGet(c *gin.Context) {
	uid := int64(aiBotUserID(c))
	repo := store.DefaultAgentChatRepo()
	rec, err := repo.GetConversation(c.Param("id"))
	if err != nil || rec == nil || rec.UserID != uid {
		c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
		return
	}
	msgs, err := repo.ListMessages(rec.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list messages failed: " + err.Error()})
		return
	}
	out := make([]agentConversationMsg, 0, len(msgs))
	for _, m := range msgs {
		// 存储为 JSON 字符串；空/非法时回退空数组，保证前端拿到的是数组。
		raw := json.RawMessage(m.ToolCalls)
		if len(raw) == 0 || !json.Valid(raw) {
			raw = json.RawMessage("[]")
		}
		out = append(out, agentConversationMsg{
			ID:        m.ID,
			Role:      m.Role,
			Content:   m.Content,
			Reasoning: m.Reasoning,
			ToolCalls: raw,
			CreatedAt: m.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"id":       rec.ID,
		"title":    rec.Title,
		"model":    rec.Model,
		"messages": out,
	})
}

// AgentConversationRename 处理 PUT /agent/conversations/:id：body{title}。
func AgentConversationRename(c *gin.Context) {
	var body struct {
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	uid := int64(aiBotUserID(c))
	repo := store.DefaultAgentChatRepo()
	rec, err := repo.GetConversation(c.Param("id"))
	if err != nil || rec == nil || rec.UserID != uid {
		c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
		return
	}
	if err := repo.RenameConversation(rec.ID, body.Title); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "rename conversation failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AgentConversationDelete 处理 DELETE /agent/conversations/:id：级联删除消息。
func AgentConversationDelete(c *gin.Context) {
	uid := int64(aiBotUserID(c))
	repo := store.DefaultAgentChatRepo()
	rec, err := repo.GetConversation(c.Param("id"))
	if err != nil || rec == nil || rec.UserID != uid {
		c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
		return
	}
	if err := repo.DeleteConversation(rec.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete conversation failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
