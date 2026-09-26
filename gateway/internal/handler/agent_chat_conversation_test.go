package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── /agent/chat 会话集成测试 ──
// 覆盖：自动建会话落库、续聊追加、regenerate、replace_history、
// model 覆盖三档（"kimi:x" / "kimi" / ""）、SSE conversation/reasoning 事件顺序。

// mockReasoningLLM 支持 reasoning_content 的 OpenAI 兼容 mock：
// 流式按 2 rune 分片先发 reasoning 后发正文；非流式 message 带 reasoning_content。
type mockReasoningLLM struct {
	mu       sync.Mutex
	requests []map[string]any
	srv      *httptest.Server
}

func splitRuneChunks(s string, n int) []string {
	runes := []rune(s)
	var out []string
	for i := 0; i < len(runes); i += n {
		end := i + n
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

func newMockReasoningLLM(t *testing.T, text, reasoning string) *mockReasoningLLM {
	t.Helper()
	m := &mockReasoningLLM{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.mu.Lock()
		m.requests = append(m.requests, req)
		m.mu.Unlock()

		if stream, _ := req["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			for _, chunk := range splitRuneChunks(reasoning, 2) {
				fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":%q}}]}\n\n", chunk)
			}
			for _, chunk := range splitRuneChunks(text, 2) {
				fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", chunk)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			fl.Flush()
			return
		}
		msg := map[string]any{"role": "assistant", "content": text, "reasoning_content": reasoning}
		resp := map[string]any{
			"id":      "chatcmpl-mock",
			"model":   "mock-model",
			"choices": []map[string]any{{"index": 0, "message": msg}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockReasoningLLM) Requests() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]any{}, m.requests...)
}

func registerReasoningProvider(t *testing.T, m *mockReasoningLLM, name, model string) {
	t.Helper()
	prev := ai.GetProvider(name)
	ai.RegisterProvider(ai.Provider{
		Name:     name,
		BaseURL:  m.srv.URL,
		APIKey:   "mock-key",
		Model:    model,
		TimeoutS: 10,
	})
	t.Cleanup(func() {
		if prev != nil {
			ai.RegisterProvider(*prev)
		}
	})
}

// chatRequestRoles 取某次 provider 请求的消息角色序列（跳过 system）。
func chatRequestRoles(t *testing.T, req map[string]any) []string {
	t.Helper()
	msgs, _ := req["messages"].([]any)
	roles := make([]string, 0, len(msgs))
	for _, item := range msgs {
		m, _ := item.(map[string]any)
		if m["role"] == "system" {
			continue
		}
		roles = append(roles, fmt.Sprint(m["role"]))
	}
	return roles
}

func chatRequestContents(t *testing.T, req map[string]any) []string {
	t.Helper()
	msgs, _ := req["messages"].([]any)
	out := make([]string, 0, len(msgs))
	for _, item := range msgs {
		m, _ := item.(map[string]any)
		if m["role"] == "system" {
			continue
		}
		out = append(out, fmt.Sprint(m["content"]))
	}
	return out
}

// seedConversation 直接落库建会话 + 消息（绕过 chat，专注单一场景）。
func seedConversation(t *testing.T, userID int64, title string, msgs ...[2]string) string {
	t.Helper()
	repo := store.DefaultAgentChatRepo()
	rec := &store.AgentConversationRecord{UserID: userID, Title: title, Model: "agent-chat-mock:mock-model"}
	if err := repo.CreateConversation(rec); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	for _, m := range msgs {
		if err := repo.InsertMessage(&store.AgentMessageRecord{ConversationID: rec.ID, Role: m[0], Content: m[1]}); err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}
	return rec.ID
}

// 自动建会话：无 conversation_id → 返回 conversation_id，落库 user+assistant（含 reasoning/tool_calls）。
func TestAgentChat_AutoCreateConversation(t *testing.T) {
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, &chatMockMatcher{})
	llm := newMockReasoningLLM(t, "你好，BTC 偏强。", "先看量价，再定方向。")
	registerReasoningProvider(t, llm, "agent-chat-mock", "mock-model")
	withAgentAIProvider(t, "agent-chat-mock")

	userMsg := "分析BTC走势，帮我看看多头还是空头，再决定仓位大小"
	w := doAgentChat(t, fmt.Sprintf(`{"messages":[{"role":"user","content":%q}]}`, userMsg), nil, 5)
	assertEq(t, w.Code, http.StatusOK, "status code")

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	convID, _ := body["conversation_id"].(string)
	if convID == "" {
		t.Fatalf("conversation_id missing: %v", body)
	}
	if body["reasoning"] != "先看量价，再定方向。" {
		t.Fatalf("reasoning = %v", body["reasoning"])
	}
	if body["content"] != "你好，BTC 偏强。" {
		t.Fatalf("content = %v", body["content"])
	}

	// 会话行：属主 / 标题（首条 user 截 20 字）/ model
	repo := store.DefaultAgentChatRepo()
	rec, err := repo.GetConversation(convID)
	if err != nil || rec == nil {
		t.Fatalf("get conversation: %v", err)
	}
	assertEq(t, int(rec.UserID), 5, "conversation owner")
	wantTitle := truncateAgentChat(userMsg, 20)
	if rec.Title != wantTitle {
		t.Fatalf("title = %q, want %q", rec.Title, wantTitle)
	}
	if rec.Model != "agent-chat-mock:mock-model" {
		t.Fatalf("model = %q", rec.Model)
	}

	// 消息行：user + assistant（reasoning / tool_calls JSON）
	msgs, err := repo.ListMessages(convID)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("messages: len=%d err=%v", len(msgs), err)
	}
	if msgs[0].Role != "user" || msgs[0].Content != userMsg {
		t.Fatalf("user msg = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "你好，BTC 偏强。" {
		t.Fatalf("assistant msg = %+v", msgs[1])
	}
	if msgs[1].Reasoning != "先看量价，再定方向。" {
		t.Fatalf("assistant reasoning = %q", msgs[1].Reasoning)
	}
	if msgs[1].ToolCalls != "[]" {
		t.Fatalf("tool_calls = %q, want []", msgs[1].ToolCalls)
	}
}

// 续聊：conversation_id 指定会话 → 服务端历史 + 本轮 user；落库追加两条。
func TestAgentChat_ExistingConversationAppend(t *testing.T) {
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, &chatMockMatcher{})
	llm := newMockReasoningLLM(t, "第二问答完了。", "")
	registerReasoningProvider(t, llm, "agent-chat-mock", "mock-model")
	withAgentAIProvider(t, "agent-chat-mock")

	convID := seedConversation(t, 5, "旧会话",
		[2]string{"user", "第一问"}, [2]string{"assistant", "第一答"})

	w := doAgentChat(t, fmt.Sprintf(`{"messages":[{"role":"user","content":"第二问"}],"conversation_id":%q}`, convID), nil, 5)
	assertEq(t, w.Code, http.StatusOK, "status code")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["conversation_id"] != convID {
		t.Fatalf("conversation_id = %v", body["conversation_id"])
	}

	// 模型输入：服务端历史 + 本轮 user（无重复）
	reqs := llm.Requests()
	assertEq(t, len(reqs), 1, "provider calls")
	contents := chatRequestContents(t, reqs[0])
	want := []string{"第一问", "第一答", "第二问"}
	if strings.Join(contents, "|") != strings.Join(want, "|") {
		t.Fatalf("history contents = %v, want %v", contents, want)
	}

	msgs, _ := store.DefaultAgentChatRepo().ListMessages(convID)
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d, want 4", len(msgs))
	}
	if msgs[3].Role != "assistant" || msgs[3].Content != "第二问答完了。" {
		t.Fatalf("last msg = %+v", msgs[3])
	}
}

// regenerate：删最后一条 assistant，用 messages 最后一条 user 重跑。
func TestAgentChat_Regenerate(t *testing.T) {
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, &chatMockMatcher{})
	llm := newMockReasoningLLM(t, "重答第二问。", "")
	registerReasoningProvider(t, llm, "agent-chat-mock", "mock-model")
	withAgentAIProvider(t, "agent-chat-mock")

	convID := seedConversation(t, 5, "重跑",
		[2]string{"user", "第一问"}, [2]string{"assistant", "第一答"},
		[2]string{"user", "第二问"}, [2]string{"assistant", "旧第二答"})

	w := doAgentChat(t, fmt.Sprintf(`{"messages":[{"role":"user","content":"第二问"}],"conversation_id":%q,"regenerate":true}`, convID), nil, 5)
	assertEq(t, w.Code, http.StatusOK, "status code")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["content"] != "重答第二问。" {
		t.Fatalf("content = %v", body["content"])
	}

	// 模型输入：u1 a1 u2（删 a2 后追加同内容 user 被去重）
	reqs := llm.Requests()
	assertEq(t, len(reqs), 1, "provider calls")
	contents := chatRequestContents(t, reqs[0])
	want := []string{"第一问", "第一答", "第二问"}
	if strings.Join(contents, "|") != strings.Join(want, "|") {
		t.Fatalf("regenerate history = %v, want %v", contents, want)
	}

	// 落库：a2 被替换为"重答第二问。"，user 不重复
	msgs, _ := store.DefaultAgentChatRepo().ListMessages(convID)
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d, want 4", len(msgs))
	}
	if msgs[2].Role != "user" || msgs[2].Content != "第二问" {
		t.Fatalf("msgs[2] = %+v", msgs[2])
	}
	if msgs[3].Role != "assistant" || msgs[3].Content != "重答第二问。" {
		t.Fatalf("msgs[3] = %+v", msgs[3])
	}
}

// replace_history：请求 messages 整体替换会话历史，再跑最后一轮；user 不重复落库。
func TestAgentChat_ReplaceHistory(t *testing.T) {
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, &chatMockMatcher{})
	llm := newMockReasoningLLM(t, "新答。", "")
	registerReasoningProvider(t, llm, "agent-chat-mock", "mock-model")
	withAgentAIProvider(t, "agent-chat-mock")

	convID := seedConversation(t, 5, "编辑",
		[2]string{"user", "旧问题"}, [2]string{"assistant", "旧回答"})

	body := fmt.Sprintf(`{"messages":[{"role":"user","content":"改后问题"},{"role":"assistant","content":"改后回答"},{"role":"user","content":"追加问题"}],"conversation_id":%q,"replace_history":true}`, convID)
	w := doAgentChat(t, body, nil, 5)
	assertEq(t, w.Code, http.StatusOK, "status code")
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["content"] != "新答。" {
		t.Fatalf("content = %v", resp["content"])
	}

	reqs := llm.Requests()
	assertEq(t, len(reqs), 1, "provider calls")
	contents := chatRequestContents(t, reqs[0])
	want := []string{"改后问题", "改后回答", "追加问题"}
	if strings.Join(contents, "|") != strings.Join(want, "|") {
		t.Fatalf("replace history input = %v, want %v", contents, want)
	}

	// 库内：整体替换 + 只补 assistant
	msgs, _ := store.DefaultAgentChatRepo().ListMessages(convID)
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d, want 4", len(msgs))
	}
	if msgs[0].Content != "改后问题" || msgs[1].Content != "改后回答" || msgs[2].Content != "追加问题" {
		t.Fatalf("replaced msgs = %+v", msgs[:3])
	}
	if msgs[3].Role != "assistant" || msgs[3].Content != "新答。" {
		t.Fatalf("msgs[3] = %+v", msgs[3])
	}
}

// 会话不存在 / 非属主：404。
func TestAgentChat_ConversationNotFound(t *testing.T) {
	llm := newMockReasoningLLM(t, "ok", "")
	registerReasoningProvider(t, llm, "agent-chat-mock", "mock-model")
	withAgentAIProvider(t, "agent-chat-mock")

	// 不存在
	w := doAgentChat(t, `{"messages":[{"role":"user","content":"hi"}],"conversation_id":"c_ghost"}`, nil, 5)
	assertEq(t, w.Code, http.StatusNotFound, "ghost status")

	// 非属主
	convID := seedConversation(t, 9, "他人会话", [2]string{"user", "hi"})
	w = doAgentChat(t, fmt.Sprintf(`{"messages":[{"role":"user","content":"hi"}],"conversation_id":%q}`, convID), nil, 5)
	assertEq(t, w.Code, http.StatusNotFound, "cross-user status")

	// 流式：SSE error 事件
	w = doAgentChat(t, fmt.Sprintf(`{"messages":[{"role":"user","content":"hi"}],"conversation_id":%q,"stream":true}`, convID), nil, 5)
	assertEq(t, w.Code, http.StatusOK, "stream cross-user status")
	if !strings.Contains(w.Body.String(), "event: error") || !strings.Contains(w.Body.String(), "conversation not found") {
		t.Fatalf("sse body = %q", w.Body.String())
	}
}

// model 覆盖三档："kimi:x" / "kimi" / ""（默认链）。
func TestAgentChat_ModelOverride(t *testing.T) {
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, &chatMockMatcher{})
	llm := newMockReasoningLLM(t, "答。", "")
	registerReasoningProvider(t, llm, "agent-chat-mock", "mock-model")
	registerReasoningProvider(t, llm, "kimi", "registry-model")
	withAgentAIProvider(t, "agent-chat-mock")

	// ai.kimi 设置页配置：api_key + model（凭证回落链验证点）
	cfg := store.GetConfig()
	prevAI, hadAI := cfg["ai"]
	cfg["ai"] = map[string]any{
		"kimi": map[string]any{"api_key": "sk-kimi-cfg", "model": "cfg-model"},
	}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Cleanup(func() {
		cfg := store.GetConfig()
		if hadAI {
			cfg["ai"] = prevAI
		} else {
			delete(cfg, "ai")
		}
		_ = store.SaveConfig(cfg)
	})

	// 档 1："kimi:kimi-for-coding" → provider kimi + 模型覆盖
	w := doAgentChat(t, `{"messages":[{"role":"user","content":"写代码"}],"model":"kimi:kimi-for-coding"}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "tier1 status")
	reqs := llm.Requests()
	assertEq(t, len(reqs), 1, "tier1 calls")
	if reqs[0]["model"] != "kimi-for-coding" {
		t.Fatalf("tier1 model = %v, want kimi-for-coding", reqs[0]["model"])
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	rec, err := store.DefaultAgentChatRepo().GetConversation(fmt.Sprint(resp["conversation_id"]))
	if err != nil || rec == nil {
		t.Fatalf("tier1 conversation: %v", err)
	}
	if rec.Model != "kimi:kimi-for-coding" {
		t.Fatalf("tier1 conv model = %q", rec.Model)
	}

	// 档 2："kimi" → provider kimi，用其配置模型（cfg-model）
	w = doAgentChat(t, `{"messages":[{"role":"user","content":"写代码"}],"model":"kimi"}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "tier2 status")
	reqs = llm.Requests()
	assertEq(t, len(reqs), 2, "tier2 calls")
	if reqs[1]["model"] != "cfg-model" {
		t.Fatalf("tier2 model = %v, want cfg-model", reqs[1]["model"])
	}

	// 档 3："" → 默认链（agent-chat-mock / mock-model）
	w = doAgentChat(t, `{"messages":[{"role":"user","content":"写代码"}]}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "tier3 status")
	reqs = llm.Requests()
	assertEq(t, len(reqs), 3, "tier3 calls")
	if reqs[2]["model"] != "mock-model" {
		t.Fatalf("tier3 model = %v, want mock-model", reqs[2]["model"])
	}
}

// 覆盖解析单测："provider:model" / "provider" / "" / 名归一。
func TestParseAgentModelOverride(t *testing.T) {
	cases := []struct {
		in       string
		provider string
		model    string
	}{
		{"kimi:kimi-for-coding", "kimi", "kimi-for-coding"},
		{"kimi", "kimi", ""},
		{"", "", ""},
		{"anthropic:claude-opus", "claude", "claude-opus"}, // legacy 名归一
		{" deepseek ", "deepseek", ""},                     // 空白裁剪
	}
	for _, tc := range cases {
		p, m := parseAgentModelOverride(tc.in)
		if p != tc.provider || m != tc.model {
			t.Errorf("parseAgentModelOverride(%q) = (%q,%q), want (%q,%q)", tc.in, p, m, tc.provider, tc.model)
		}
	}
}

// SSE 事件顺序：reasoning → delta → conversation → done → [DONE]；
// conversation 事件恰一次且在 done 之前；done 带 conversation_id/reasoning。
func TestAgentChat_SSE_ConversationAndReasoning(t *testing.T) {
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, &chatMockMatcher{})
	llm := newMockReasoningLLM(t, "最终建议。", "先看量再看价")
	registerReasoningProvider(t, llm, "agent-chat-mock", "mock-model")
	withAgentAIProvider(t, "agent-chat-mock")

	userMsg := "给我一条关于BTC的交易建议吧朋友"
	w := doAgentChat(t, fmt.Sprintf(`{"messages":[{"role":"user","content":%q}],"stream":true}`, userMsg), nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	raw := w.Body.String()

	events := parseSSE(t, raw)
	doneIdx, convIdx, reasoningCount := -1, -1, 0
	var convPayload, donePayload map[string]any
	var reasoningParts []string
	for i, ev := range events {
		switch ev.Event {
		case "reasoning":
			reasoningCount++
			var s string
			if err := json.Unmarshal([]byte(ev.Data), &s); err != nil {
				t.Fatalf("reasoning data 应 JSON 编码字符串: %q", ev.Data)
			}
			reasoningParts = append(reasoningParts, s)
		case "conversation":
			convIdx = i
			if err := json.Unmarshal([]byte(ev.Data), &convPayload); err != nil {
				t.Fatalf("conversation data not JSON: %v", err)
			}
		case "done":
			doneIdx = i
			if err := json.Unmarshal([]byte(ev.Data), &donePayload); err != nil {
				t.Fatalf("done data not JSON: %v", err)
			}
		}
	}
	if doneIdx < 0 || convIdx < 0 {
		t.Fatalf("missing conversation/done event: %q", raw)
	}
	if convIdx >= doneIdx {
		t.Fatalf("conversation 事件应在 done 之前: conv=%d done=%d", convIdx, doneIdx)
	}
	if reasoningCount == 0 {
		t.Fatal("missing reasoning events")
	}
	if strings.Join(reasoningParts, "") != "先看量再看价" {
		t.Fatalf("reasoning 分片 = %v", reasoningParts)
	}
	// conversation 事件：id 与 done 一致，标题=首条 user 截 20 字
	convID, _ := donePayload["conversation_id"].(string)
	if convPayload["id"] != convID || convID == "" {
		t.Fatalf("conversation payload = %v, done id = %v", convPayload, convID)
	}
	if convPayload["title"] != truncateAgentChat(userMsg, 20) {
		t.Fatalf("conversation title = %v", convPayload["title"])
	}
	// done 全量字段
	if donePayload["reasoning"] != "先看量再看价" || donePayload["content"] != "最终建议。" {
		t.Fatalf("done payload = %v", donePayload)
	}
	if calls, ok := donePayload["tool_calls"].([]any); !ok || len(calls) != 0 {
		t.Fatalf("done tool_calls = %v", donePayload["tool_calls"])
	}
	// 库已落
	rec, err := store.DefaultAgentChatRepo().GetConversation(convID)
	if err != nil || rec == nil {
		t.Fatalf("persisted conversation missing: %v", err)
	}
	msgs, _ := store.DefaultAgentChatRepo().ListMessages(convID)
	if len(msgs) != 2 || msgs[1].Reasoning != "先看量再看价" {
		t.Fatalf("persisted msgs = %+v", msgs)
	}
}

// agent token 路径：会话属主取 token.UserID；他人 JWT 访问其会话 404。
func TestAgentChat_ConversationViaAgentToken(t *testing.T) {
	matcher := &chatMockMatcher{}
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, matcher)
	llm := newMockReasoningLLM(t, "token 答。", "")
	registerReasoningProvider(t, llm, "agent-chat-mock", "mock-model")
	withAgentAIProvider(t, "agent-chat-mock")

	token, _, err := agent.GetTokenManager().CreateTokenForUser("conv-token", "R", 10, 3600, 42)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"token 问"}]}`, map[string]string{
		"X-Agent-Token": token,
	}, 0)
	assertEq(t, w.Code, http.StatusOK, "token chat status")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	convID, _ := body["conversation_id"].(string)
	if convID == "" {
		t.Fatalf("conversation_id missing: %v", body)
	}
	recConv, err := store.DefaultAgentChatRepo().GetConversation(convID)
	if err != nil || recConv == nil {
		t.Fatalf("conversation: %v", err)
	}
	assertEq(t, int(recConv.UserID), 42, "conversation owner from token")

	// 其他 JWT 用户访问该会话 → 404
	w = doAgentChat(t, fmt.Sprintf(`{"messages":[{"role":"user","content":"hi"}],"conversation_id":%q}`, convID), nil, 7)
	assertEq(t, w.Code, http.StatusNotFound, "cross-user via JWT")
}

// 会话级并发互斥锁基本行为：同 id 串行、不同 id 并行。
func TestAgentChat_ConversationLock(t *testing.T) {
	unlockA := lockAgentConversation("c_lock_a")
	done := make(chan struct{})
	go func() {
		unlockB := lockAgentConversation("c_lock_a")
		unlockB()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("同一会话锁应互斥（持锁期间另一请求阻塞）")
	case <-time.After(50 * time.Millisecond):
	}
	unlockA()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("解锁后应获得锁")
	}
	// 不同 id 不互相阻塞
	unlockC := lockAgentConversation("c_lock_b")
	unlockC()
}
