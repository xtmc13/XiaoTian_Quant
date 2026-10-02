package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 上下文压缩 / 分支 / 临时会话 ──

// doAgentEndpoint 发起任意 agent 端点请求；pattern 为 gin 路由模式（含 :id），
// path 为实际请求路径；jwtUID > 0 时注入 JWT 用户。
func doAgentEndpoint(t *testing.T, method, pattern, path, body string, h gin.HandlerFunc, jwtUID int) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	handlers := make([]gin.HandlerFunc, 0, 2)
	if jwtUID > 0 {
		handlers = append(handlers, func(c *gin.Context) {
			c.Set(middleware.UserIDKey, jwtUID)
		})
	}
	handlers = append(handlers, h)
	r.Handle(method, pattern, handlers...)

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// seedCompressConv 建会话并插入 pairs 对 user+assistant 消息。
func seedCompressConv(t *testing.T, repo *store.AgentChatRepo, uid int64, pairs int) *store.AgentConversationRecord {
	t.Helper()
	rec := &store.AgentConversationRecord{UserID: uid, Title: "压缩测试", Model: "agent-chat-mock:mock-model"}
	if err := repo.CreateConversation(rec); err != nil {
		t.Fatalf("create conv: %v", err)
	}
	for i := 0; i < pairs; i++ {
		for _, m := range []*store.AgentMessageRecord{
			{ConversationID: rec.ID, Role: "user", Content: fmt.Sprintf("这是一条足够长的用户问题%d用来占位", i+1)},
			{ConversationID: rec.ID, Role: "assistant", Content: fmt.Sprintf("这是一段足够长的助手回答%d用来占位", i+1)},
		} {
			if err := repo.InsertMessage(m); err != nil {
				t.Fatalf("insert: %v", err)
			}
		}
	}
	return rec
}

// 手动压缩端点：契约 {"success","summary","compressed_count"}；候选消息标记
// compressed=1 且存档摘要；历史加载跳过压缩消息并注入前情摘要；非属主 404。
func TestAgentConversationCompress(t *testing.T) {
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "用户在做 BTC 短线，偏好低杠杆。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	repo := store.DefaultAgentChatRepo()
	rec := seedCompressConv(t, repo, 91, 5) // 10 条消息

	// 非属主 → 404
	w := doAgentEndpoint(t, http.MethodPost, "/agent/conversations/:id/compress", "/agent/conversations/"+rec.ID+"/compress", "{}", AgentConversationCompress, 92)
	assertEq(t, w.Code, http.StatusNotFound, "非属主应 404")

	// 属主压缩：10 条 - 保留 6 条 = 压缩 4 条
	w = doAgentEndpoint(t, http.MethodPost, "/agent/conversations/:id/compress", "/agent/conversations/"+rec.ID+"/compress", "{}", AgentConversationCompress, 91)
	assertEq(t, w.Code, http.StatusOK, "status")
	var resp struct {
		Success         bool   `json:"success"`
		Summary         string `json:"summary"`
		CompressedCount int    `json:"compressed_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse resp: %v", err)
	}
	if !resp.Success || resp.Summary == "" || resp.CompressedCount != 4 {
		t.Fatalf("resp = %+v, want success+summary+count=4", resp)
	}

	// 库存校验：前 4 条 compressed=1，后 6 条未压缩；摘要存档一条。
	msgs, err := repo.ListMessages(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	compressedN := 0
	for _, m := range msgs[:4] {
		if m.Compressed {
			compressedN++
		}
	}
	assertEq(t, compressedN, 4, "前 4 条应标记压缩")
	for _, m := range msgs[4:] {
		if m.Compressed {
			t.Fatal("最近 6 条不应被压缩")
		}
	}
	comp, err := repo.LatestCompaction(rec.ID)
	if err != nil || comp == nil || comp.CompressedCount != 4 {
		t.Fatalf("compaction = %+v / %v, want count=4", comp, err)
	}

	// 历史加载：跳过压缩消息，头部注入前情摘要（user 角色）。
	history := listSessionHistory(repo, rec.ID)
	if len(history) != 7 { // 摘要 1 + 未压缩 6
		t.Fatalf("history len = %d, want 7", len(history))
	}
	if history[0].Role != ai.RoleUser || !strings.HasPrefix(history[0].Content, "（前情摘要：") ||
		!strings.Contains(history[0].Content, "低杠杆") {
		t.Fatalf("首条应为前情摘要, got %+v", history[0])
	}
	for _, m := range history[1:] {
		if strings.Contains(m.Content, "问题1") || strings.Contains(m.Content, "回答1") {
			t.Fatal("压缩消息不应出现在历史中")
		}
	}

	// 再次压缩：未压缩仅剩 6 条，无候选 → count=0（幂等）。
	w = doAgentEndpoint(t, http.MethodPost, "/agent/conversations/:id/compress", "/agent/conversations/"+rec.ID+"/compress", "{}", AgentConversationCompress, 91)
	assertEq(t, w.Code, http.StatusOK, "status")
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.CompressedCount != 0 {
		t.Fatalf("二次压缩应 count=0, got %d", resp.CompressedCount)
	}
}

// 分支端点：复制 [0..message_index]（保留 content/reasoning/tool_calls/compressed，
// 不带用量），标题 原标题（分支），沿用模型；越界 400，非属主 404。
func TestAgentConversationBranch(t *testing.T) {
	repo := store.DefaultAgentChatRepo()
	rec := &store.AgentConversationRecord{UserID: 93, Title: "原标题", Model: "kimi:kimi-for-coding"}
	if err := repo.CreateConversation(rec); err != nil {
		t.Fatal(err)
	}
	seed := []*store.AgentMessageRecord{
		{ConversationID: rec.ID, Role: "user", Content: "u1"},
		{ConversationID: rec.ID, Role: "assistant", Content: "a1", Reasoning: "r1", ToolCalls: `[{"name":"get_stats"}]`, PromptTokens: 100, CompletionTokens: 50, LLMMs: 999, Compressed: true},
		{ConversationID: rec.ID, Role: "user", Content: "u2"},
		{ConversationID: rec.ID, Role: "assistant", Content: "a2"},
	}
	for _, m := range seed {
		if err := repo.InsertMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	path := "/agent/conversations/" + rec.ID + "/branch"

	// 非属主 → 404
	w := doAgentEndpoint(t, http.MethodPost, "/agent/conversations/:id/branch", path, `{"message_index":1}`, AgentConversationBranch, 94)
	assertEq(t, w.Code, http.StatusNotFound, "非属主应 404")
	// 越界 → 400
	w = doAgentEndpoint(t, http.MethodPost, "/agent/conversations/:id/branch", path, `{"message_index":4}`, AgentConversationBranch, 93)
	assertEq(t, w.Code, http.StatusBadRequest, "越界应 400")
	w = doAgentEndpoint(t, http.MethodPost, "/agent/conversations/:id/branch", path, `{"message_index":-1}`, AgentConversationBranch, 93)
	assertEq(t, w.Code, http.StatusBadRequest, "负索引应 400")

	// 正常分支：index=1 → 复制前 2 条
	w = doAgentEndpoint(t, http.MethodPost, "/agent/conversations/:id/branch", path, `{"message_index":1}`, AgentConversationBranch, 93)
	assertEq(t, w.Code, http.StatusOK, "status")
	var resp struct {
		Success bool   `json:"success"`
		ID      string `json:"id"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || !strings.HasPrefix(resp.ID, "c_") || resp.Title != "原标题（分支）" {
		t.Fatalf("resp = %+v", resp)
	}
	branch, err := repo.GetConversation(resp.ID)
	if err != nil || branch == nil {
		t.Fatal("分支会话不存在")
	}
	if branch.Model != "kimi:kimi-for-coding" {
		t.Fatalf("分支模型 = %q", branch.Model)
	}
	msgs, err := repo.ListMessages(branch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("分支消息数 = %d, want 2", len(msgs))
	}
	if msgs[1].Content != "a1" || msgs[1].Reasoning != "r1" || msgs[1].ToolCalls != `[{"name":"get_stats"}]` {
		t.Fatalf("content/reasoning/tool_calls 未保留: %+v", msgs[1])
	}
	if !msgs[1].Compressed {
		t.Fatal("compressed 标记未保留")
	}
	if msgs[1].PromptTokens != 0 || msgs[1].CompletionTokens != 0 || msgs[1].LLMMs != 0 {
		t.Fatalf("用量不应复制: %+v", msgs[1])
	}
}

// 临时会话（ephemeral）：正常执行但不写任何库行、不建会话；
// 带 conversation_id 时仅回显 conversation 事件，不带则不发该事件。
func TestAgentChatEphemeral(t *testing.T) {
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "旁路回答。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	repo := store.DefaultAgentChatRepo()
	existing := seedCompressConv(t, repo, 95, 1)
	before, _ := repo.ListConversations(95, 100, 0)

	// 无 conversation_id：不建会话、不落库
	w := doAgentChat(t, `{"messages":[{"role":"user","content":"旁路提问"}],"ephemeral":true}`, nil, 95)
	assertEq(t, w.Code, http.StatusOK, "status")
	var plain struct {
		Content        string `json:"content"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &plain); err != nil {
		t.Fatal(err)
	}
	if plain.ConversationID != "" {
		t.Fatalf("临时会话不应有 conversation_id, got %q", plain.ConversationID)
	}
	after, _ := repo.ListConversations(95, 100, 0)
	if len(after) != len(before) {
		t.Fatalf("临时会话不应建会话: before=%d after=%d", len(before), len(after))
	}

	// 带 conversation_id + 流式：回显 conversation 事件，但不写入任何消息
	body := `{"messages":[{"role":"user","content":"再问一下"}],"ephemeral":true,"stream":true,"conversation_id":"` + existing.ID + `"}`
	w = doAgentChat(t, body, nil, 95)
	assertEq(t, w.Code, http.StatusOK, "status")
	if !strings.Contains(w.Body.String(), "event: conversation") ||
		!strings.Contains(w.Body.String(), `"id":"`+existing.ID+`"`) {
		t.Fatalf("应回显 conversation 事件, got %s", w.Body.String())
	}
	msgs, _ := repo.ListMessages(existing.ID)
	if len(msgs) != 2 { // 仍是 seed 的 1 对
		t.Fatalf("临时会话不应落库消息, got %d", len(msgs))
	}

	// 不带 conversation_id 的流式：不发 conversation 事件
	w = doAgentChat(t, `{"messages":[{"role":"user","content":"再来"}],"ephemeral":true,"stream":true}`, nil, 95)
	assertEq(t, w.Code, http.StatusOK, "status")
	if strings.Contains(w.Body.String(), "event: conversation") {
		t.Fatal("无 conversation_id 的临时会话不应发 conversation 事件")
	}
	if !strings.Contains(w.Body.String(), "event: done") {
		t.Fatal("临时会话仍应正常流式输出 done 事件")
	}
}

// 自动压缩：estTokens 超阈值时在 LLM 调用前同步压缩（保留最近 6 条），
// 发 compressed SSE 事件；2 回合内不重复压缩（候选 < 4 跳过）。
func TestAgentChatAutoCompress(t *testing.T) {
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		// 摘要请求与对话请求区分：含"压缩器"系统提示的走摘要
		msgs, _ := req["messages"].([]any)
		if len(msgs) > 0 {
			if sys, _ := msgs[0].(map[string]any); strings.Contains(fmt.Sprint(sys["content"]), "压缩器") {
				return "前情：用户关注 BTC 低杠杆。", nil
			}
		}
		return "好的。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	prevThreshold := agentChatCompressThreshold
	agentChatCompressThreshold = 20 // 极小阈值确保触发
	t.Cleanup(func() { agentChatCompressThreshold = prevThreshold })

	repo := store.DefaultAgentChatRepo()
	rec := seedCompressConv(t, repo, 96, 6) // 12 条消息，estTokens 远超 20

	body := `{"messages":[{"role":"user","content":"继续"}],"stream":true,"conversation_id":"` + rec.ID + `"}`
	w := doAgentChat(t, body, nil, 96)
	assertEq(t, w.Code, http.StatusOK, "status")
	if !strings.Contains(w.Body.String(), "event: compressed") ||
		!strings.Contains(w.Body.String(), `"compressed_count":6`) {
		t.Fatalf("应发 compressed 事件(count=6), got %s", w.Body.String())
	}
	comp, err := repo.LatestCompaction(rec.ID)
	if err != nil || comp == nil || comp.CompressedCount != 6 {
		t.Fatalf("compaction = %+v / %v, want count=6", comp, err)
	}
	// 压缩后模型实际收到的历史：摘要 + 未压缩 6 条 + 本轮 user
	reqs := llm.Requests()
	last := reqs[len(reqs)-1]
	msgs, _ := last["messages"].([]any)
	sys, _ := msgs[0].(map[string]any)
	if strings.Contains(fmt.Sprint(sys["content"]), "压缩器") {
		t.Fatal("最后一个请求应是对话请求而非摘要请求")
	}
	found := false
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if strings.Contains(fmt.Sprint(mm["content"]), "（前情摘要：") {
			found = true
		}
	}
	if !found {
		t.Fatal("压缩后历史应注入前情摘要")
	}

	// 第二回合：上轮新增 2 条，未压缩共 8 条，候选 2 < 4 → 不再压缩
	firstCompID := comp.ID
	w = doAgentChat(t, body, nil, 96)
	assertEq(t, w.Code, http.StatusOK, "status")
	if strings.Contains(w.Body.String(), "event: compressed") {
		t.Fatal("2 回合内不应重复压缩")
	}
	comp2, _ := repo.LatestCompaction(rec.ID)
	if comp2 == nil || comp2.ID != firstCompID {
		t.Fatal("压缩存档不应新增")
	}
}

// 估算：estTokens = 历史总 rune 数 / 2。
func TestEstimateChatHistoryTokens(t *testing.T) {
	history := []ai.ChatMessage{
		{Role: ai.RoleUser, Content: "四个汉字"},        // 4 runes
		{Role: ai.RoleAssistant, Content: "abcdef"}, // 6 runes
	}
	if got := estimateChatHistoryTokens(history); got != 5 {
		t.Fatalf("estTokens = %d, want 5", got)
	}
	if got := estimateChatHistoryTokens(nil); got != 0 {
		t.Fatalf("estTokens(nil) = %d, want 0", got)
	}
}
