package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/ai"
)

// token 路径限流：超出 rate_limit_rps → 429 RATE_LIMITED，并写审计；JWT 路径不受影响。
func TestAgentChat_RateLimit(t *testing.T) {
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "ok", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	tm := agent.GetTokenManager()
	// rate_limit_rps=2：1 秒窗口内第 3 次请求应被拒
	token, rec, err := tm.CreateTokenForUser("chat-rl", "R", 2, 3600, 42)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	headers := map[string]string{"X-Agent-Token": token}
	for i := 0; i < 2; i++ {
		w := doAgentChat(t, `{"messages":[{"role":"user","content":"hi"}]}`, headers, 0)
		assertEq(t, w.Code, http.StatusOK, "窗口内请求应放行")
	}
	w := doAgentChat(t, `{"messages":[{"role":"user","content":"hi"}]}`, headers, 0)
	assertEq(t, w.Code, http.StatusTooManyRequests, "超限应 429")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["success"] != false {
		t.Fatalf("body = %v", body)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "RATE_LIMITED" {
		t.Fatalf("error.code = %v", errObj)
	}

	// 审计：限流拒绝留痕（429）
	recs, err := tm.GetAuditByToken(rec.ID, 20)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	found := false
	for _, r := range recs {
		if r.StatusCode == http.StatusTooManyRequests && r.Endpoint == "/agent/chat" {
			found = true
			break
		}
	}
	assertTrue(t, found, "限流拒绝应写审计")

	// JWT 路径不受 token 限流影响：连续 3 次全放行
	for i := 0; i < 3; i++ {
		w := doAgentChat(t, `{"messages":[{"role":"user","content":"hi"}]}`, nil, 7)
		assertEq(t, w.Code, http.StatusOK, "JWT 路径不应被限流")
	}
}

// AllowAgentChatRequest：rps<=0 不限流。
func TestAllowAgentChatRequest_Unlimited(t *testing.T) {
	for i := 0; i < 20; i++ {
		if !agent.AllowAgentChatRequest(999999, 0) {
			t.Fatal("rps=0 应不限流")
		}
		if !agent.AllowAgentChatRequest(999998, -1) {
			t.Fatal("rps<0 应不限流")
		}
	}
}
