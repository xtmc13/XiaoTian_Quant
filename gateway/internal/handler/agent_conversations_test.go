package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── /agent/conversations CRUD 测试 ──

// registerConversationRoutes 注册会话端点 + 用户注入中间件（等价于
// registerAgentRoutes 的 agent 组：AuthRequired 之后 handler 读 UserIDKey）。
func registerConversationRoutes(r *gin.Engine, uid int) {
	h := func(c *gin.Context) {
		if uid > 0 {
			c.Set(middleware.UserIDKey, uid)
		}
	}
	r.POST("/agent/conversations", h, AgentConversationCreate)
	r.GET("/agent/conversations", h, AgentConversationsList)
	r.GET("/agent/conversations/search", h, AgentConversationSearch)
	r.GET("/agent/conversations/:id", h, AgentConversationGet)
	r.PUT("/agent/conversations/:id", h, AgentConversationRename)
	r.DELETE("/agent/conversations/:id", h, AgentConversationDelete)
	r.POST("/agent/conversations/:id/undo", h, AgentConversationUndo)
	r.GET("/agent/usage", h, AgentUsageSummary)
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

// 全文搜索：标题/内容命中、snippet、用户隔离、空 q。
func TestAgentConversationSearch(t *testing.T) {
	w := doConversationRequest(t, 21, http.MethodPost, "/agent/conversations", `{"title":"以太坊质押收益"}`)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	titleHitID, _ := body["id"].(string)

	w = doConversationRequest(t, 21, http.MethodPost, "/agent/conversations", `{"title":"随便聊聊"}`)
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	contentHitID, _ := body["id"].(string)
	repo := store.DefaultAgentChatRepo()
	// 显式给消息一个更晚的时间戳：同秒创建的两个会话 updated_at 打平时排序不确定，
	// 借此让 contentHit 稳定排在 titleHit 前（InsertMessage 会刷新会话 updated_at）。
	repo.InsertMessage(&store.AgentMessageRecord{ConversationID: contentHitID, Role: "user",
		Content: "帮我把这段话翻译一下：以太坊的质押收益率最近有所下降。", CreatedAt: time.Now().Unix() + 10})
	// 他人会话（标题同样含关键词，不应出现）
	w = doConversationRequest(t, 22, http.MethodPost, "/agent/conversations", `{"title":"以太坊他人"}`)
	_ = json.Unmarshal(w.Body.Bytes(), &body)

	w = doConversationRequest(t, 21, http.MethodGet, "/agent/conversations/search?q=以太坊", "")
	assertEq(t, w.Code, http.StatusOK, "search status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true {
		t.Fatalf("search body = %v", body)
	}
	results, _ := body["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2（隔离他人）: %v", len(results), results)
	}
	// updated_at 倒序：后建的 contentHit 在前；内容命中带 snippet，标题命中 snippet 空
	r0, _ := results[0].(map[string]any)
	r1, _ := results[1].(map[string]any)
	if r0["id"] != contentHitID || r1["id"] != titleHitID {
		t.Fatalf("排序/命中错误: %v", results)
	}
	snippet, _ := r0["snippet"].(string)
	if !strings.Contains(snippet, "以太坊") {
		t.Fatalf("snippet = %q", snippet)
	}
	if r1["snippet"] != "" {
		t.Fatalf("标题命中 snippet 应为空: %v", r1["snippet"])
	}

	// 空 q → 空结果
	w = doConversationRequest(t, 21, http.MethodGet, "/agent/conversations/search", "")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if results, _ := body["results"].([]any); len(results) != 0 {
		t.Fatalf("空 q 应空结果: %v", body)
	}
}

// 撤回一轮：删除最后 user + 其后 assistant；越权 404。
func TestAgentConversationUndo(t *testing.T) {
	w := doConversationRequest(t, 23, http.MethodPost, "/agent/conversations", `{"title":"t"}`)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	convID, _ := body["id"].(string)
	repo := store.DefaultAgentChatRepo()
	for _, m := range [][2]string{{"user", "u1"}, {"assistant", "a1"}, {"user", "u2"}, {"assistant", "a2"}} {
		repo.InsertMessage(&store.AgentMessageRecord{ConversationID: convID, Role: m[0], Content: m[1]})
	}

	w = doConversationRequest(t, 23, http.MethodPost, "/agent/conversations/"+convID+"/undo", "")
	assertEq(t, w.Code, http.StatusOK, "undo status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true || body["remaining"] != float64(2) {
		t.Fatalf("undo body = %v", body)
	}
	msgs, _ := repo.ListMessages(convID)
	if len(msgs) != 2 || msgs[1].Content != "a1" {
		t.Fatalf("undo 后消息错误: %+v", msgs)
	}

	// 越权 undo → 404，且消息未被动过
	w = doConversationRequest(t, 24, http.MethodPost, "/agent/conversations/"+convID+"/undo", "")
	assertEq(t, w.Code, http.StatusNotFound, "cross-user undo")
	msgs, _ = repo.ListMessages(convID)
	if len(msgs) != 2 {
		t.Fatalf("越权 undo 生效了: %d", len(msgs))
	}
}

// 用量汇总：totals / session / by_day / by_model；指定会话越权 404。
func TestAgentUsageSummary(t *testing.T) {
	w := doConversationRequest(t, 25, http.MethodPost, "/agent/conversations", `{"title":"u"}`)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	convID, _ := body["id"].(string)
	repo := store.DefaultAgentChatRepo()
	_ = repo.SetConversationModel(convID, "kimi:k2")
	repo.InsertMessage(&store.AgentMessageRecord{ConversationID: convID, Role: "user", Content: "hi"})
	repo.InsertMessage(&store.AgentMessageRecord{ConversationID: convID, Role: "assistant", Content: "ok",
		PromptTokens: 100, CompletionTokens: 50, LLMMs: 800})

	w = doConversationRequest(t, 25, http.MethodGet, "/agent/usage?conversation_id="+convID+"&days=30", "")
	assertEq(t, w.Code, http.StatusOK, "usage status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true {
		t.Fatalf("usage body = %v", body)
	}
	session, _ := body["session"].(map[string]any)
	if session["prompt_tokens"] != float64(100) || session["completion_tokens"] != float64(50) ||
		session["llm_ms"] != float64(800) || session["rounds"] != float64(1) {
		t.Fatalf("session = %v", session)
	}
	totals, _ := body["totals"].(map[string]any)
	if totals["rounds"] != float64(1) {
		t.Fatalf("totals = %v", totals)
	}
	byDay, _ := body["by_day"].([]any)
	if len(byDay) != 1 {
		t.Fatalf("by_day = %v", byDay)
	}
	day0, _ := byDay[0].(map[string]any)
	if day0["prompt_tokens"] != float64(100) || day0["date"] == "" {
		t.Fatalf("by_day[0] = %v", day0)
	}
	byModel, _ := body["by_model"].([]any)
	if len(byModel) != 1 {
		t.Fatalf("by_model = %v", byModel)
	}
	m0, _ := byModel[0].(map[string]any)
	if m0["model"] != "kimi:k2" || m0["completion_tokens"] != float64(50) {
		t.Fatalf("by_model[0] = %v", m0)
	}

	// 不指定会话：session 为 null
	w = doConversationRequest(t, 25, http.MethodGet, "/agent/usage", "")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["session"] != nil {
		t.Fatalf("session 应为 null: %v", body["session"])
	}

	// 越权指定会话 → 404
	w = doConversationRequest(t, 26, http.MethodGet, "/agent/usage?conversation_id="+convID, "")
	assertEq(t, w.Code, http.StatusNotFound, "cross-user usage session")
}
