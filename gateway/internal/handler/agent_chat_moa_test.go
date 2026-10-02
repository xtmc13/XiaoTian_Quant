package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/ai"
)

// ── MoA（多厂商混合）测试 ──
// 主厂商固定走 agent-chat-mock；提案厂商的发现与提问均可注入（包级变量，
// 与 agentChatTitleUpgrade 等既有测试门同模式），避免跨用例的注册表污染
// 影响断言。真实发现逻辑由 TestMoaProposerProviders 单独覆盖（包含式断言）。

// registerNamedMockProvider 以指定名字注册 mock provider（提案厂商用）。
func registerNamedMockProvider(m *mockAgentLLM, name string) {
	ai.RegisterProvider(ai.Provider{
		Name:     name,
		BaseURL:  m.srv.URL,
		APIKey:   "mock-key",
		Model:    "mock-model",
		TimeoutS: 10,
	})
}

// withMoaDiscovery 注入固定的提案厂商集合，测试结束恢复真实发现。
func withMoaDiscovery(t *testing.T, proposers []*ai.Provider) {
	t.Helper()
	prev := agentMoaDiscoverProposers
	agentMoaDiscoverProposers = func(int64, string) []*ai.Provider { return proposers }
	t.Cleanup(func() { agentMoaDiscoverProposers = prev })
}

// lastUserContentFromReq 取 mock 记录请求里最后一条 user 消息内容。
func lastUserContentFromReq(t *testing.T, req map[string]any) string {
	t.Helper()
	msgs, _ := req["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m, _ := msgs[i].(map[string]any)
		if m["role"] == "user" {
			return fmt.Sprint(m["content"])
		}
	}
	t.Fatalf("no user message in request")
	return ""
}

// 流式 MoA：moa 事件在任何 delta 之前发出（主厂商在前），综合参考块注入主厂商上下文。
func TestAgentChatMoa_StreamFanOutAndSynthesis(t *testing.T) {
	primary := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "综合回答", nil
	})
	registerMockAgentProvider(primary)
	withAgentAIProvider(t, "agent-chat-mock")

	fakeA := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) { return "厂商A回答", nil })
	fakeB := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) { return "厂商B回答", nil })
	registerNamedMockProvider(fakeA, "moa-fake-a")
	registerNamedMockProvider(fakeB, "moa-fake-b")
	withMoaDiscovery(t, []*ai.Provider{ai.GetProvider("moa-fake-a"), ai.GetProvider("moa-fake-b")})

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"网格策略怎么做"}],"stream":true,"moa":true}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	events := parseSSE(t, w.Body.String())

	// 第一个事件必须是 moa，且只发一次；proposers 主厂商在前。
	assertStrEq(t, events[0].Event, "moa", "first SSE event")
	var moaPayload struct {
		Proposers []string `json:"proposers"`
		Note      string   `json:"note"`
	}
	if err := json.Unmarshal([]byte(events[0].Data), &moaPayload); err != nil {
		t.Fatalf("parse moa event: %v", err)
	}
	want := []string{"agent-chat-mock", "moa-fake-a", "moa-fake-b"}
	if strings.Join(moaPayload.Proposers, ",") != strings.Join(want, ",") {
		t.Fatalf("proposers = %v, want %v", moaPayload.Proposers, want)
	}
	if moaPayload.Note != "" {
		t.Fatalf("unexpected note: %q", moaPayload.Note)
	}
	moaCount := 0
	for _, ev := range events {
		if ev.Event == "moa" {
			moaCount++
		}
	}
	assertEq(t, moaCount, 1, "moa event count")

	// 主厂商收到的最后一条 user 消息带综合参考块（原提问保留在后）。
	reqs := primary.Requests()
	assertEq(t, len(reqs), 1, "primary calls")
	lastUser := lastUserContentFromReq(t, reqs[0])
	if !strings.HasPrefix(lastUser, "（以下是对同一问题的多家模型回答") {
		t.Fatalf("missing synthesis block head: %q", lastUser)
	}
	for _, frag := range []string{"【moa-fake-a】厂商A回答", "【moa-fake-b】厂商B回答", "网格策略怎么做"} {
		if !strings.Contains(lastUser, frag) {
			t.Fatalf("synthesis missing %q: %q", frag, lastUser)
		}
	}

	// 提案厂商收到：同一系统提示词 + 用户提问，不带工具。
	for name, m := range map[string]*mockAgentLLM{"moa-fake-a": fakeA, "moa-fake-b": fakeB} {
		preqs := m.Requests()
		assertEq(t, len(preqs), 1, name+" calls")
		if _, hasTools := preqs[0]["tools"]; hasTools {
			t.Fatalf("%s request must not carry tools", name)
		}
		msgs, _ := preqs[0]["messages"].([]any)
		sys, _ := msgs[0].(map[string]any)
		if sys["role"] != "system" || !strings.Contains(fmt.Sprint(sys["content"]), "小天量化助手") {
			t.Fatalf("%s missing system prompt", name)
		}
		if got := lastUserContentFromReq(t, preqs[0]); got != "网格策略怎么做" {
			t.Fatalf("%s prompt = %q", name, got)
		}
	}

	// done 事件照常（内容为主厂商综合回答）。
	last := events[len(events)-2] // 末尾是 [DONE] 的 message 事件
	assertStrEq(t, last.Event, "done", "last named event")
	var done struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(last.Data), &done); err != nil || done.Content != "综合回答" {
		t.Fatalf("done content = %q (err %v)", done.Content, err)
	}
}

// 厂商不足（含主厂商 <2）：moa 事件带 note，本轮按普通模式进行（无综合块）。
func TestAgentChatMoa_InsufficientProposers(t *testing.T) {
	primary := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "普通回答", nil
	})
	registerMockAgentProvider(primary)
	withAgentAIProvider(t, "agent-chat-mock")
	withMoaDiscovery(t, nil)

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"你好"}],"stream":true,"moa":true}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	events := parseSSE(t, w.Body.String())
	assertStrEq(t, events[0].Event, "moa", "first SSE event")
	var moaPayload struct {
		Proposers []string `json:"proposers"`
		Note      string   `json:"note"`
	}
	if err := json.Unmarshal([]byte(events[0].Data), &moaPayload); err != nil {
		t.Fatalf("parse moa event: %v", err)
	}
	assertEq(t, len(moaPayload.Proposers), 0, "proposers len")
	assertStrEq(t, moaPayload.Note, "厂商不足，按普通模式", "note")

	// 主厂商上下文未被注入综合块，回答与落库照旧。
	reqs := primary.Requests()
	assertEq(t, len(reqs), 1, "primary calls")
	if got := lastUserContentFromReq(t, reqs[0]); got != "你好" {
		t.Fatalf("user content = %q, want 原样", got)
	}
}

// 部分提案失败被容忍：1 家成功仍注入综合块（仅含成功方）。
func TestAgentChatMoa_PartialFailureTolerated(t *testing.T) {
	primary := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "综合回答", nil
	})
	registerMockAgentProvider(primary)
	withAgentAIProvider(t, "agent-chat-mock")
	withMoaDiscovery(t, []*ai.Provider{
		ai.NewEphemeralProvider("moa-ok", "http://unused", "m", "k"),
		ai.NewEphemeralProvider("moa-bad", "http://unused", "m", "k"),
	})
	prevPropose := agentMoaPropose
	agentMoaPropose = func(_ context.Context, p *ai.Provider, _, _ string) (string, error) {
		if p.Name == "moa-bad" {
			return "", fmt.Errorf("boom")
		}
		return "唯一成功回答", nil
	}
	t.Cleanup(func() { agentMoaPropose = prevPropose })

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"问点什么"}],"stream":true,"moa":true}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	events := parseSSE(t, w.Body.String())
	// moa 事件仍列出全部实际参与厂商（含失败方）。
	var moaPayload struct {
		Proposers []string `json:"proposers"`
	}
	if err := json.Unmarshal([]byte(events[0].Data), &moaPayload); err != nil {
		t.Fatalf("parse moa event: %v", err)
	}
	assertStrEq(t, strings.Join(moaPayload.Proposers, ","), "agent-chat-mock,moa-ok,moa-bad", "proposers")

	reqs := primary.Requests()
	lastUser := lastUserContentFromReq(t, reqs[0])
	if !strings.Contains(lastUser, "【moa-ok】唯一成功回答") {
		t.Fatalf("synthesis missing survivor: %q", lastUser)
	}
	if strings.Contains(lastUser, "moa-bad") {
		t.Fatalf("synthesis must not mention failed proposer: %q", lastUser)
	}
}

// 临时会话（/btw 旁路）同样支持 moa：发 moa 事件、注入综合块，且不落库。
func TestAgentChatMoa_Ephemeral(t *testing.T) {
	primary := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "旁路综合", nil
	})
	registerMockAgentProvider(primary)
	withAgentAIProvider(t, "agent-chat-mock")
	withMoaDiscovery(t, []*ai.Provider{ai.NewEphemeralProvider("moa-ep", "http://unused", "m", "k")})
	prevPropose := agentMoaPropose
	agentMoaPropose = func(_ context.Context, _ *ai.Provider, _, _ string) (string, error) {
		return "旁路厂商回答", nil
	}
	t.Cleanup(func() { agentMoaPropose = prevPropose })

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"临时问一下"}],"stream":true,"ephemeral":true,"moa":true}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	events := parseSSE(t, w.Body.String())
	assertStrEq(t, events[0].Event, "moa", "first SSE event")

	reqs := primary.Requests()
	lastUser := lastUserContentFromReq(t, reqs[0])
	if !strings.Contains(lastUser, "【moa-ep】旁路厂商回答") || !strings.Contains(lastUser, "临时问一下") {
		t.Fatalf("ephemeral synthesis = %q", lastUser)
	}
	// 临时会话不发 conversation 事件（未指定 conversation_id）。
	for _, ev := range events {
		if ev.Event == "conversation" {
			t.Fatalf("ephemeral must not emit conversation event")
		}
	}
}

// 非流式 JSON 路径：无 moa 事件概念，响应体带 moa_proposers。
func TestAgentChatMoa_NonStreamJSON(t *testing.T) {
	primary := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "综合回答", nil
	})
	registerMockAgentProvider(primary)
	withAgentAIProvider(t, "agent-chat-mock")
	withMoaDiscovery(t, []*ai.Provider{ai.NewEphemeralProvider("moa-json", "http://unused", "m", "k")})
	prevPropose := agentMoaPropose
	agentMoaPropose = func(_ context.Context, _ *ai.Provider, _, _ string) (string, error) {
		return "JSON 厂商回答", nil
	}
	t.Cleanup(func() { agentMoaPropose = prevPropose })

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"非流式提问"}],"moa":true}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	proposers, _ := body["moa_proposers"].([]any)
	if len(proposers) != 2 || proposers[0] != "agent-chat-mock" || proposers[1] != "moa-json" {
		t.Fatalf("moa_proposers = %v", body["moa_proposers"])
	}
	reqs := primary.Requests()
	if got := lastUserContentFromReq(t, reqs[0]); !strings.Contains(got, "【moa-json】JSON 厂商回答") {
		t.Fatalf("synthesis = %q", got)
	}

	// 未请求 moa 时响应不带 moa_proposers 字段。
	w2 := doAgentChat(t, `{"messages":[{"role":"user","content":"普通提问"}]}`, nil, 0)
	var body2 map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if _, has := body2["moa_proposers"]; has {
		t.Fatalf("moa_proposers must be absent without moa:true")
	}
}

// 真实发现逻辑：持有凭证的厂商入选（注册表 env key / 全局配置 key），
// 无凭证与主厂商被排除；cap 生效。
func TestMoaProposerProviders_Discovery(t *testing.T) {
	registerMockAgentProvider(newMockAgentLLM(t, func(map[string]any) (string, []ai.ToolCall) { return "", nil }))
	ai.RegisterProvider(ai.Provider{Name: "moa-disc-keyed", BaseURL: "http://unused", APIKey: "k", Model: "m"})
	ai.RegisterProvider(ai.Provider{Name: "moa-disc-nokey", BaseURL: "http://unused", APIKey: "", Model: "m"})

	got := moaProposerProviders(0, "agent-chat-mock")
	names := map[string]bool{}
	for _, p := range got {
		names[p.Name] = true
		if p.APIKey == "" {
			t.Fatalf("proposer %s without credentials", p.Name)
		}
	}
	if !names["moa-disc-keyed"] {
		t.Fatalf("keyed provider missing: %v", names)
	}
	if names["moa-disc-nokey"] {
		t.Fatalf("keyless provider must be excluded")
	}
	if names["agent-chat-mock"] {
		t.Fatalf("primary provider must be excluded")
	}

	// cap：上限 1 时只取 1 家。
	prevCap := agentMoaMaxProposers
	agentMoaMaxProposers = 1
	t.Cleanup(func() { agentMoaMaxProposers = prevCap })
	if got := moaProposerProviders(0, "agent-chat-mock"); len(got) != 1 {
		t.Fatalf("cap = %d, want 1", len(got))
	}
}
