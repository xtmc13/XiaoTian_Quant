package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── /agent/conversations CRUD 测试 ──

// registerConversationRoutes 注册 5 个会话端点 + 用户注入中间件（等价于
// registerAgentRoutes 的 agent 组：AuthRequired 之后 handler 读 UserIDKey）。
func registerConversationRoutes(r *gin.Engine, uid int) {
	h := func(c *gin.Context) {
		if uid > 0 {
			c.Set(middleware.UserIDKey, uid)
		}
	}
	r.POST("/agent/conversations", h, AgentConversationCreate)
	r.GET("/agent/conversations", h, AgentConversationsList)
	r.GET("/agent/conversations/:id", h, AgentConversationGet)
	r.PUT("/agent/conversations/:id", h, AgentConversationRename)
	r.DELETE("/agent/conversations/:id", h, AgentConversationDelete)
}

func doConversationRequest(t *testing.T, uid int, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	registerConversationRoutes(r, uid)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAgentConversationsCRUD(t *testing.T) {
	// 建两个会话（一个带标题、一个无标题 body）
	w := doConversationRequest(t, 1, http.MethodPost, "/agent/conversations", `{"title":"分析BTC"}`)
	assertEq(t, w.Code, http.StatusOK, "create status")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse create body: %v", err)
	}
	if body["success"] != true || body["title"] != "分析BTC" {
		t.Fatalf("create body = %v", body)
	}
	convID, _ := body["id"].(string)
	if convID == "" {
		t.Fatal("create body missing id")
	}

	w = doConversationRequest(t, 1, http.MethodPost, "/agent/conversations", "")
	assertEq(t, w.Code, http.StatusOK, "create without body")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	emptyID, _ := body["id"].(string)
	if body["success"] != true || emptyID == "" {
		t.Fatalf("create without body = %v", body)
	}

	// 列表：只含本人会话，字段齐全
	w = doConversationRequest(t, 1, http.MethodGet, "/agent/conversations?limit=50&offset=0", "")
	assertEq(t, w.Code, http.StatusOK, "list status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true {
		t.Fatalf("list success = %v", body)
	}
	convs, _ := body["conversations"].([]any)
	if len(convs) != 2 {
		t.Fatalf("conversations len = %d, want 2", len(convs))
	}
	item, _ := convs[0].(map[string]any)
	for _, k := range []string{"id", "title", "model", "created_at", "updated_at"} {
		if _, ok := item[k]; !ok {
			t.Fatalf("list item missing %s: %v", k, item)
		}
	}

	// 详情（暂无消息）
	w = doConversationRequest(t, 1, http.MethodGet, "/agent/conversations/"+convID, "")
	assertEq(t, w.Code, http.StatusOK, "get status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true || body["id"] != convID || body["title"] != "分析BTC" {
		t.Fatalf("get body = %v", body)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 0 {
		t.Fatalf("messages should be empty, got %v", msgs)
	}

	// 重命名
	w = doConversationRequest(t, 1, http.MethodPut, "/agent/conversations/"+convID, `{"title":"改名"}`)
	assertEq(t, w.Code, http.StatusOK, "rename status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true {
		t.Fatalf("rename body = %v", body)
	}
	w = doConversationRequest(t, 1, http.MethodGet, "/agent/conversations/"+convID, "")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["title"] != "改名" {
		t.Fatalf("rename not persisted: %v", body["title"])
	}

	// 详情消息字段（tool_calls 原样字符串）
	repo := store.DefaultAgentChatRepo()
	repo.InsertMessage(&store.AgentMessageRecord{ConversationID: convID, Role: "user", Content: "hi"})
	repo.InsertMessage(&store.AgentMessageRecord{ConversationID: convID, Role: "assistant", Content: "ok",
		Reasoning: "思考", ToolCalls: `[{"name":"get_balance","args_summary":"{}","status":"done","result_summary":"x"}]`})
	w = doConversationRequest(t, 1, http.MethodGet, "/agent/conversations/"+convID, "")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	msgs, _ = body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2", len(msgs))
	}
	m0, _ := msgs[0].(map[string]any)
	if m0["role"] != "user" || m0["content"] != "hi" {
		t.Fatalf("message[0] = %v", m0)
	}
	m1, _ := msgs[1].(map[string]any)
	if m1["reasoning"] != "思考" {
		t.Fatalf("message[1].reasoning = %v", m1["reasoning"])
	}
	tc, ok := m1["tool_calls"].([]any)
	if !ok || len(tc) != 1 {
		t.Fatalf("tool_calls 应为数组: %v", m1["tool_calls"])
	}
	tc0, _ := tc[0].(map[string]any)
	if tc0["name"] != "get_balance" || tc0["status"] != "done" {
		t.Fatalf("tool_calls[0] = %v", tc0)
	}

	// 删除（级联）
	w = doConversationRequest(t, 1, http.MethodDelete, "/agent/conversations/"+convID, "")
	assertEq(t, w.Code, http.StatusOK, "delete status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true {
		t.Fatalf("delete body = %v", body)
	}
	w = doConversationRequest(t, 1, http.MethodGet, "/agent/conversations/"+convID, "")
	assertEq(t, w.Code, http.StatusNotFound, "get after delete")
	left, _ := repo.ListMessages(convID)
	if len(left) != 0 {
		t.Fatalf("级联删除失败: %d 条残留", len(left))
	}
}

// 越权：非属主 GET/PUT/DELETE 一律 404；列表不显示他人会话。
func TestAgentConversationsCrossUser(t *testing.T) {
	w := doConversationRequest(t, 1, http.MethodPost, "/agent/conversations", `{"title":"私密"}`)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	convID, _ := body["id"].(string)
	if convID == "" {
		t.Fatal("setup create failed")
	}

	w = doConversationRequest(t, 2, http.MethodGet, "/agent/conversations/"+convID, "")
	assertEq(t, w.Code, http.StatusNotFound, "cross-user get")
	w = doConversationRequest(t, 2, http.MethodPut, "/agent/conversations/"+convID, `{"title":"抢"}`)
	assertEq(t, w.Code, http.StatusNotFound, "cross-user put")
	w = doConversationRequest(t, 2, http.MethodDelete, "/agent/conversations/"+convID, "")
	assertEq(t, w.Code, http.StatusNotFound, "cross-user delete")

	// 越权 PUT 未生效
	w = doConversationRequest(t, 1, http.MethodGet, "/agent/conversations/"+convID, "")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["title"] != "私密" {
		t.Fatalf("title 被越权修改: %v", body["title"])
	}

	// 用户 2 列表为空
	w = doConversationRequest(t, 2, http.MethodGet, "/agent/conversations", "")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if convs, _ := body["conversations"].([]any); len(convs) != 0 {
		t.Fatalf("用户 2 不应看到他人会话: %v", convs)
	}

	// 不存在的 id 也 404
	w = doConversationRequest(t, 1, http.MethodGet, "/agent/conversations/c_ghost", "")
	assertEq(t, w.Code, http.StatusNotFound, "ghost get")
}
