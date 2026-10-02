package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── headless 扩展入口（TG 平台投递）：会话绑定 / 流式回调 / 模型覆盖 / ctx 中断 ──

// setupHeadlessMockLLM 注册始终直答 "mock 回复" 的 provider 并指向 agent 链。
func setupHeadlessMockLLM(t *testing.T, reply string) *mockAgentLLM {
	t.Helper()
	m := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return reply, nil
	})
	registerMockAgentProvider(m)
	withAgentAIProvider(t, "agent-chat-mock")
	return m
}

// seedHandoffConversation 建一个带两轮历史的会话（复用 seedConversation）。
func seedHandoffConversation(t *testing.T, userID int64, title string) string {
	t.Helper()
	return seedConversation(t, userID, title, [2]string{"user", "之前的问题"}, [2]string{"assistant", "之前的回答"})
}

func TestRunAgentHeadlessCtx_ConversationBound(t *testing.T) {
	m := setupHeadlessMockLLM(t, "mock 回复")
	convID := seedHandoffConversation(t, 501, "移交会话")

	reply, err := RunAgentHeadlessCtx(context.Background(), HeadlessOptions{
		UserID:         501,
		Prompt:         "继续聊",
		ConversationID: convID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply != "mock 回复" {
		t.Errorf("reply = %q", reply)
	}

	// 模型请求历史：system + 历史两轮 + 本轮 user
	reqs := m.Requests()
	if len(reqs) == 0 {
		t.Fatal("mock 未收到请求")
	}
	msgs, _ := reqs[0]["messages"].([]any)
	var roles []string
	var contents []string
	for _, raw := range msgs {
		mm, _ := raw.(map[string]any)
		roles = append(roles, mm["role"].(string))
		contents = append(contents, mm["content"].(string))
	}
	want := []string{"system", "user", "assistant", "user"}
	if len(roles) != len(want) {
		t.Fatalf("history roles = %v, want %v", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("history roles = %v, want %v", roles, want)
		}
	}
	if contents[1] != "之前的问题" || contents[2] != "之前的回答" || contents[3] != "继续聊" {
		t.Errorf("history contents = %v", contents)
	}

	// 落库：本轮 user + assistant 追加（tool_calls 摘要 JSON 与 web 路径一致）
	stored, err := store.DefaultAgentChatRepo().ListMessages(convID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 4 {
		t.Fatalf("stored = %d 条, want 4", len(stored))
	}
	if stored[2].Role != "user" || stored[2].Content != "继续聊" {
		t.Errorf("user 落库: %+v", stored[2])
	}
	if stored[3].Role != "assistant" || stored[3].Content != "mock 回复" {
		t.Errorf("assistant 落库: %+v", stored[3])
	}
	if stored[3].ToolCalls == "" {
		t.Error("assistant tool_calls 摘要应落库")
	}
}

func TestRunAgentHeadlessCtx_Streaming(t *testing.T) {
	setupHeadlessMockLLM(t, "流式回复内容")

	var buf strings.Builder
	reply, err := RunAgentHeadlessCtx(context.Background(), HeadlessOptions{
		UserID:  502,
		Prompt:  "hi",
		OnDelta: func(d string) { buf.WriteString(d) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply != "流式回复内容" {
		t.Errorf("reply = %q", reply)
	}
	if buf.String() != reply {
		t.Errorf("OnDelta 聚合 = %q, want %q", buf.String(), reply)
	}
}

func TestRunAgentHeadlessCtx_ModelOverride(t *testing.T) {
	m := setupHeadlessMockLLM(t, "ok")

	if _, err := RunAgentHeadlessCtx(context.Background(), HeadlessOptions{
		UserID: 503,
		Prompt: "hi",
		Model:  "agent-chat-mock:other-model",
	}); err != nil {
		t.Fatal(err)
	}
	reqs := m.Requests()
	if got, _ := reqs[0]["model"].(string); got != "other-model" {
		t.Errorf("model override = %q, want other-model", got)
	}
}

func TestRunAgentHeadlessCtx_ConversationNotOwned(t *testing.T) {
	setupHeadlessMockLLM(t, "ok")
	convID := seedHandoffConversation(t, 504, "别人的会话")

	if _, err := RunAgentHeadlessCtx(context.Background(), HeadlessOptions{
		UserID:         999,
		Prompt:         "hi",
		ConversationID: convID,
	}); err == nil {
		t.Error("非属主绑定会话应报错")
	}
}

func TestRunAgentHeadlessCtx_CancelAborts(t *testing.T) {
	setupHeadlessMockLLM(t, "ok")
	convID := seedHandoffConversation(t, 505, "中断会话")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消（/stop 语义）
	if _, err := RunAgentHeadlessCtx(ctx, HeadlessOptions{
		UserID:         505,
		Prompt:         "hi",
		ConversationID: convID,
	}); err == nil {
		t.Error("已取消 ctx 应中断执行")
	}
	// 中断轮次不落库
	stored, _ := store.DefaultAgentChatRepo().ListMessages(convID)
	if len(stored) != 2 {
		t.Errorf("中断不应落库本轮: %d 条", len(stored))
	}
}

// ── 会话移交端点 ──

func doHandoffRequest(t *testing.T, uid int, convID string) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	h := func(c *gin.Context) {
		if uid > 0 {
			c.Set(middleware.UserIDKey, uid)
		}
	}
	r.POST("/agent/conversations/:id/handoff", h, AgentConversationHandoff)
	req := httptest.NewRequest(http.MethodPost, "/agent/conversations/"+convID+"/handoff", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAgentConversationHandoffEndpoint(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	convID := seedHandoffConversation(t, 506, "移交目标")

	// 属主 → 200 + 6 位码
	w := doHandoffRequest(t, 506, convID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["success"] != true {
		t.Errorf("body = %v", body)
	}
	code, _ := body["code"].(string)
	if len(code) != 6 {
		t.Errorf("code = %q, want 6 位", code)
	}
	if exp, _ := body["expires_in"].(float64); exp != 600 {
		t.Errorf("expires_in = %v, want 600", exp)
	}

	// 非属主 → 404
	w = doHandoffRequest(t, 507, convID)
	if w.Code != http.StatusNotFound {
		t.Errorf("非属主 status = %d, want 404", w.Code)
	}
	// 不存在会话 → 404
	w = doHandoffRequest(t, 506, "conv_不存在")
	if w.Code != http.StatusNotFound {
		t.Errorf("不存在会话 status = %d, want 404", w.Code)
	}
}

func TestAgentConversationHandoffNotConfigured(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	convID := seedHandoffConversation(t, 508, "移交目标")
	w := doHandoffRequest(t, 508, convID)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("未配置 token status = %d, want 503", w.Code)
	}
}
