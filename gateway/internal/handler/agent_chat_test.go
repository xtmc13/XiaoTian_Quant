package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// init 先注入一套 mock 工具上下文：避免 chat 用例触发 agent.GetToolContext 的
// 懒加载默认实现（其内部 strategy.GetEngine(nil) 会抢先占用引擎单例的
// sync.Once，导致 strategy_test 的用例拿到 nil 事件总线而 panic）。
// 需要特定 mock 的用例会再自行 SetToolContext 覆盖。
func init() {
	agent.SetToolContext(&agent.ToolContext{
		Market:    chatMockMarket{},
		Portfolio: chatMockPortfolio{},
		Matcher:   &chatMockMatcher{},
	})
}

// ── 测试基建：mock LLM 服务 + mock 工具依赖 ──

// mockAgentLLM 用 httptest.Server 模拟 OpenAI 兼容后端，
// 记录全部请求（供断言历史/工具/系统提示词），响应由 respond 脚本决定。
type mockAgentLLM struct {
	mu       sync.Mutex
	requests []map[string]any
	srv      *httptest.Server
}

func newMockAgentLLM(t *testing.T, respond func(req map[string]any) (text string, toolCalls []ai.ToolCall)) *mockAgentLLM {
	t.Helper()
	m := &mockAgentLLM{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		text, toolCalls := respond(req)
		m.mu.Lock()
		m.requests = append(m.requests, req)
		m.mu.Unlock()

		if stream, _ := req["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			for _, chunk := range sseChunksFor(text, toolCalls) {
				fmt.Fprintf(w, "data: %s\n\n", chunk)
				fl.Flush()
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			fl.Flush()
			return
		}

		msg := map[string]any{"role": "assistant", "content": text}
		if len(toolCalls) > 0 {
			arr := make([]map[string]any, 0, len(toolCalls))
			for i, tc := range toolCalls {
				arr = append(arr, map[string]any{
					"id":   fmt.Sprintf("call_%d", i+1),
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": tc.Arguments,
					},
				})
			}
			msg["tool_calls"] = arr
		}
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

// sseChunksFor 生成流式响应块：文本按 2 rune 分片；工具 arguments 拆两片发，
// 验证 ChatCompletionStreamEx 的分片聚合。
func sseChunksFor(text string, toolCalls []ai.ToolCall) []string {
	var chunks []string
	runes := []rune(text)
	for i := 0; i < len(runes); i += 2 {
		end := i + 2
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, fmt.Sprintf(`{"choices":[{"index":0,"delta":{"content":%q}}]}`, string(runes[i:end])))
	}
	for i, tc := range toolCalls {
		id := fmt.Sprintf("call_%d", i+1)
		chunks = append(chunks, fmt.Sprintf(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":%d,"id":%q,"type":"function","function":{"name":%q,"arguments":""}}]}}]}`, i, id, tc.Name))
		// arguments 为原始 JSON 文本，分两片发送以验证聚合
		s := tc.Arguments
		half := len(s) / 2
		chunks = append(chunks, fmt.Sprintf(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":%d,"function":{"arguments":%q}}]}}]}`, i, s[:half]))
		chunks = append(chunks, fmt.Sprintf(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":%d,"function":{"arguments":%q}}]}}]}`, i, s[half:]))
	}
	return chunks
}

func (m *mockAgentLLM) Requests() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]any{}, m.requests...)
}

func registerMockAgentProvider(m *mockAgentLLM) {
	ai.RegisterProvider(ai.Provider{
		Name:     "agent-chat-mock",
		BaseURL:  m.srv.URL,
		APIKey:   "mock-key",
		Model:    "mock-model",
		TimeoutS: 10,
	})
}

// withAgentAIProvider 把 agent.ai.provider 指向指定名字，测试结束恢复原值。
func withAgentAIProvider(t *testing.T, name string) {
	t.Helper()
	prev := currentAgentAIProvider()

	cfg := store.GetConfig()
	agentCfg, _ := cfg["agent"].(map[string]any)
	if agentCfg == nil {
		agentCfg = map[string]any{}
		cfg["agent"] = agentCfg
	}
	aiCfg, _ := agentCfg["ai"].(map[string]any)
	if aiCfg == nil {
		aiCfg = map[string]any{}
		agentCfg["ai"] = aiCfg
	}
	aiCfg["provider"] = name
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	t.Cleanup(func() {
		cfg := store.GetConfig()
		agentCfg, _ := cfg["agent"].(map[string]any)
		aiCfg, _ := agentCfg["ai"].(map[string]any)
		if aiCfg == nil {
			return
		}
		if prev == "" {
			delete(aiCfg, "provider")
		} else {
			aiCfg["provider"] = prev
		}
		_ = store.SaveConfig(cfg)
	})
}

func currentAgentAIProvider() string {
	cfg := store.GetConfig()
	agentCfg, _ := cfg["agent"].(map[string]any)
	aiCfg, _ := agentCfg["ai"].(map[string]any)
	p, _ := aiCfg["provider"].(string)
	return p
}

// doAgentChat 发起 POST /agent/chat；jwtUID > 0 时模拟 JWT 中间件注入用户。
func doAgentChat(t *testing.T, body string, headers map[string]string, jwtUID int) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	handlers := make([]gin.HandlerFunc, 0, 2)
	if jwtUID > 0 {
		handlers = append(handlers, func(c *gin.Context) {
			c.Set(middleware.UserIDKey, jwtUID)
		})
	}
	handlers = append(handlers, AgentChatStream)
	r.POST("/agent/chat", handlers...)

	req := httptest.NewRequest(http.MethodPost, "/agent/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func reqHasToolMessage(req map[string]any) bool {
	msgs, _ := req["messages"].([]any)
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" {
			return true
		}
	}
	return false
}

// ── mock 工具依赖 ──

type chatMockPortfolio struct{ equity, pnl float64 }

func (m chatMockPortfolio) TotalEquity() float64              { return m.equity }
func (m chatMockPortfolio) TotalPnL() float64                 { return m.pnl }
func (m chatMockPortfolio) Balances(string) []*model.Balance  { return []*model.Balance{} }
func (m chatMockPortfolio) Positions() []*model.PositionData  { return nil }

type chatMockMarket struct{ err error }

func (m chatMockMarket) Ticker24h(context.Context, string) (map[string]any, error) {
	if m.err != nil {
		return nil, m.err
	}
	return map[string]any{"lastPrice": "67412.5", "priceChangePercent": "1.2"}, nil
}
func (m chatMockMarket) BookTicker(context.Context, string) (float64, float64, error) {
	if m.err != nil {
		return 0, 0, m.err
	}
	return 67412.4, 67412.6, nil
}
func (m chatMockMarket) Klines(context.Context, string, string, int, int64, int64) ([]map[string]any, error) {
	return nil, m.err
}
func (m chatMockMarket) ExchangeSymbols(context.Context) ([]string, error) { return nil, m.err }

type chatMockMatcher struct {
	mu       sync.Mutex
	lastUser uint64
}

func (m *chatMockMatcher) PlaceOrder(_, _, _ string, _, _ float64, userID uint64, _ string) (map[string]any, error) {
	m.mu.Lock()
	m.lastUser = userID
	m.mu.Unlock()
	return map[string]any{"store_order_id": "mock-order-1", "filled": true, "status": "FILLED"}, nil
}
func (m *chatMockMatcher) CancelOrder(string, string) error { return nil }
func (m *chatMockMatcher) LastUser() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastUser
}

func injectAgentToolContext(mkt agent.MarketDataSource, pf agent.PortfolioReader, matcher agent.OrderMatcher) {
	agent.SetToolContext(&agent.ToolContext{Market: mkt, Portfolio: pf, Matcher: matcher})
}

// ── SSE 解析 ──

type sseEvent struct {
	Event string
	Data  string
}

func parseSSE(t *testing.T, raw string) []sseEvent {
	t.Helper()
	var events []sseEvent
	cur := sseEvent{Event: "message"}
	hasData := false
	flush := func() {
		if hasData {
			events = append(events, cur)
		}
		cur = sseEvent{Event: "message"}
		hasData = false
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "event:"):
			cur.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			d := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if hasData {
				cur.Data += "\n" + d
			} else {
				cur.Data = d
			}
			hasData = true
		case line == "":
			flush()
		}
	}
	flush()
	return events
}

func toolNamesFromRequest(t *testing.T, req map[string]any) []string {
	t.Helper()
	raw, _ := req["tools"].([]any)
	names := make([]string, 0, len(raw))
	for _, item := range raw {
		tool, _ := item.(map[string]any)
		fn, _ := tool["function"].(map[string]any)
		name, _ := fn["name"].(string)
		names = append(names, name)
	}
	return names
}

func lastAuditByName(t *testing.T, name string) *store.AuditRecord {
	t.Helper()
	recs, err := store.NewAuditRepo().GetRecent(100)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	for _, r := range recs {
		if r.Name == name && r.Endpoint == "/agent/chat/tools/call" {
			return r
		}
	}
	return nil
}

// ── 用例 ──

// 无工具调用：模型直接回答，非流式返回 {content, tool_calls}。
func TestAgentChat_DirectAnswer_NonStream(t *testing.T) {
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "你好，BTC 当前走势偏强。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"你好"}]}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["content"] != "你好，BTC 当前走势偏强。" {
		t.Fatalf("content = %v", body["content"])
	}
	if calls, ok := body["tool_calls"].([]any); !ok || len(calls) != 0 {
		t.Fatalf("tool_calls should be empty, got %v", body["tool_calls"])
	}

	reqs := llm.Requests()
	assertEq(t, len(reqs), 1, "provider calls")
	// 系统提示词：身份 + 工具清单
	msgs, _ := reqs[0]["messages"].([]any)
	sys, _ := msgs[0].(map[string]any)
	if sys["role"] != "system" || !strings.Contains(fmt.Sprint(sys["content"]), "小天量化助手") {
		t.Fatalf("missing system prompt: %v", sys)
	}
	if !strings.Contains(fmt.Sprint(sys["content"]), "get_balance") {
		t.Fatalf("system prompt missing tool list")
	}
	// 默认路径：JWT 未注入也未带 token → 全量 16 个工具
	assertEq(t, len(toolNamesFromRequest(t, reqs[0])), 16, "tools count")
}

// 单轮工具调用：调用 get_balance → 结果回传 → 最终回答；写审计（token_id=0）。
func TestAgentChat_SingleToolCall_NonStream(t *testing.T) {
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{equity: 12345.6, pnl: 78.9}, &chatMockMatcher{})
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			return "", []ai.ToolCall{{Name: "get_balance", Arguments: `{}`}}
		}
		return "您的账户权益为12345.60USDT。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"我有多少钱"}]}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["content"] != "您的账户权益为12345.60USDT。" {
		t.Fatalf("content = %v", body["content"])
	}
	calls, _ := body["tool_calls"].([]any)
	assertEq(t, len(calls), 1, "tool_calls len")
	call, _ := calls[0].(map[string]any)
	if call["name"] != "get_balance" || call["status"] != "done" {
		t.Fatalf("tool call record = %v", call)
	}
	if !strings.Contains(fmt.Sprint(call["result_summary"]), "total_equity") {
		t.Fatalf("result_summary = %v", call["result_summary"])
	}

	// 第二轮请求：assistant 工具调用 + tool 结果回传
	reqs := llm.Requests()
	assertEq(t, len(reqs), 2, "provider calls")
	msgs, _ := reqs[1]["messages"].([]any)
	lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
	if lastMsg["role"] != "tool" || lastMsg["tool_call_id"] != "call_1" {
		t.Fatalf("tool message = %v", lastMsg)
	}
	if !strings.Contains(fmt.Sprint(lastMsg["content"]), "12345.6") {
		t.Fatalf("tool result content = %v", lastMsg["content"])
	}
	prevMsg, _ := msgs[len(msgs)-2].(map[string]any)
	if prevMsg["role"] != "assistant" {
		t.Fatalf("assistant message expected, got %v", prevMsg["role"])
	}

	// 审计：JWT 路径 token_id = 0
	rec := lastAuditByName(t, "get_balance")
	if rec == nil {
		t.Fatal("audit record for get_balance not found")
	}
	assertEq(t, rec.TokenID, 0, "audit token_id")
	assertEq(t, rec.StatusCode, 200, "audit status")
}

// 工具报错：ERROR 内容回传模型，模型据此给出失败说明。
func TestAgentChat_ToolError_FedBack(t *testing.T) {
	injectAgentToolContext(chatMockMarket{err: errors.New("binance unreachable")}, chatMockPortfolio{}, &chatMockMatcher{})
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			return "", []ai.ToolCall{{Name: "get_market_data", Arguments: `{"symbol":"BTCUSDT"}`}}
		}
		return "很抱歉，行情服务暂时不可用。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"查一下 BTC 行情"}]}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["content"] != "很抱歉，行情服务暂时不可用。" {
		t.Fatalf("content = %v", body["content"])
	}
	calls, _ := body["tool_calls"].([]any)
	call, _ := calls[0].(map[string]any)
	if !strings.Contains(fmt.Sprint(call["result_summary"]), "ERROR") {
		t.Fatalf("result_summary = %v", call["result_summary"])
	}

	reqs := llm.Requests()
	msgs, _ := reqs[1]["messages"].([]any)
	lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
	content := fmt.Sprint(lastMsg["content"])
	if !strings.Contains(content, "ERROR:") || !strings.Contains(content, "binance unreachable") {
		t.Fatalf("error not fed back: %v", content)
	}

	rec := lastAuditByName(t, "get_market_data")
	if rec == nil {
		t.Fatal("audit record for get_market_data not found")
	}
	assertEq(t, rec.StatusCode, 500, "audit status for failed tool")
}

// 模型每轮都调工具：5 轮后耗尽，返回兜底文案。
func TestAgentChat_MaxIterations(t *testing.T) {
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{equity: 1}, &chatMockMatcher{})
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "", []ai.ToolCall{{Name: "get_balance", Arguments: `{}`}}
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"循环"}]}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["content"] != "任务较复杂，已达最大执行步数，请把需求拆小后重试" {
		t.Fatalf("content = %v", body["content"])
	}
	assertEq(t, len(llm.Requests()), agentChatMaxIterations, "provider calls")
}

// SSE 事件序列：delta（默认事件，原文）+ tool_call running/done + done + [DONE]。
func TestAgentChat_SSE_EventSequence(t *testing.T) {
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{equity: 12345.6, pnl: 0}, &chatMockMatcher{})
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			return "", []ai.ToolCall{{Name: "get_balance", Arguments: `{}`}}
		}
		return "您的账户权益为12345.60USDT。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"余额"}],"stream":true}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	raw := w.Body.String()

	// 文本分片为原文（非 JSON 编码）
	if !strings.Contains(raw, "data: 您的") {
		t.Fatalf("raw delta missing: %q", raw[:min(200, len(raw))])
	}

	events := parseSSE(t, raw)
	var toolEvents []map[string]any
	var donePayload map[string]any
	var deltas []string
	lastData := ""
	for _, ev := range events {
		lastData = ev.Data
		switch ev.Event {
		case "tool_call":
			var p map[string]any
			if err := json.Unmarshal([]byte(ev.Data), &p); err != nil {
				t.Fatalf("tool_call data not JSON: %v", err)
			}
			toolEvents = append(toolEvents, p)
		case "done":
			if err := json.Unmarshal([]byte(ev.Data), &donePayload); err != nil {
				t.Fatalf("done data not JSON: %v", err)
			}
		case "message":
			if ev.Data != "[DONE]" { // 流终止标记，不是文本分片
				deltas = append(deltas, ev.Data)
			}
		}
	}
	assertEq(t, len(toolEvents), 2, "tool_call events (running+done)")
	if toolEvents[0]["status"] != "running" || toolEvents[0]["name"] != "get_balance" {
		t.Fatalf("running tool_call = %v", toolEvents[0])
	}
	if toolEvents[1]["status"] != "done" || fmt.Sprint(toolEvents[1]["result_summary"]) == "" {
		t.Fatalf("done tool_call = %v", toolEvents[1])
	}
	if donePayload["content"] != "您的账户权益为12345.60USDT。" {
		t.Fatalf("done content = %v", donePayload["content"])
	}
	doneCalls, _ := donePayload["tool_calls"].([]any)
	assertEq(t, len(doneCalls), 1, "done tool_calls len")
	// 流式分片聚合后的工具结果已正确回传第二轮请求
	msgs, _ := llm.Requests()[1]["messages"].([]any)
	lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
	if lastMsg["role"] != "tool" || !strings.Contains(fmt.Sprint(lastMsg["content"]), "total_equity") {
		t.Fatalf("streamed tool result not fed back: %v", lastMsg)
	}
	if strings.Join(deltas, "") != "您的账户权益为12345.60USDT。" {
		t.Fatalf("deltas = %q", strings.Join(deltas, ""))
	}
	if lastData != "[DONE]" {
		t.Fatalf("stream should end with [DONE], got %q", lastData)
	}
}

// provider 调用失败（连接被拒）：SSE event: error 后关闭。
func TestAgentChat_SSE_ProviderError(t *testing.T) {
	// 指向一个必然连接失败的地址
	ai.RegisterProvider(ai.Provider{
		Name:     "agent-chat-mock",
		BaseURL:  "http://127.0.0.1:1",
		APIKey:   "mock-key",
		Model:    "mock-model",
		TimeoutS: 5,
	})
	withAgentAIProvider(t, "agent-chat-mock")

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"hi"}],"stream":true}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	raw := w.Body.String()
	if !strings.Contains(raw, "event: error") {
		t.Fatalf("missing error event: %q", raw)
	}
	if !strings.Contains(raw, `"message"`) || !strings.Contains(raw, "data: [DONE]") {
		t.Fatalf("error payload / [DONE] missing: %q", raw)
	}
	events := parseSSE(t, raw)
	var sawErr, sawDone bool
	for _, ev := range events {
		if ev.Event == "error" {
			sawErr = true
			var p map[string]any
			if err := json.Unmarshal([]byte(ev.Data), &p); err != nil || p["message"] == "" {
				t.Fatalf("error payload = %q", ev.Data)
			}
		}
		if ev.Event == "done" {
			sawDone = true
		}
	}
	assertTrue(t, sawErr, "should emit error event")
	assertTrue(t, !sawDone, "should not emit done on error")
}

// provider 未配置：200 {status:"error", reply:...}（与 AgentChat 行为一致）。
func TestAgentChat_ProviderNotConfigured(t *testing.T) {
	withAgentAIProvider(t, "ghost-provider-not-registered")

	// 非流式：JSON error。
	w := doAgentChat(t, `{"messages":[{"role":"user","content":"hi"}]}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["status"] != "error" || !strings.Contains(fmt.Sprint(body["reply"]), "API Key") {
		t.Fatalf("body = %v", body)
	}

	// 流式：必须回 SSE error 事件（前端按事件协议解析，JSON 会静默落空）。
	w = doAgentChat(t, `{"messages":[{"role":"user","content":"hi"}],"stream":true}`, nil, 0)
	assertEq(t, w.Code, http.StatusOK, "stream status code")
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	s := w.Body.String()
	if !strings.Contains(s, "event: error") || !strings.Contains(s, "API Key") {
		t.Fatalf("sse body = %q", s)
	}
}

// agent token 鉴权：无效 / 过期 → 401。
func TestAgentChat_AgentTokenInvalid(t *testing.T) {
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "ok", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"hi"}]}`, map[string]string{
		"X-Agent-Token": "qd_agent_deadbeef",
	}, 0)
	assertEq(t, w.Code, http.StatusUnauthorized, "invalid token status")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["error"] != "invalid agent token" {
		t.Fatalf("body = %v", body)
	}

	// 过期 token：1 秒有效期，等其过期后再用
	tm := agent.GetTokenManager()
	expired, _, err := tm.CreateTokenForUser("expired-chat", "R", 10, 1, 0)
	if err != nil {
		t.Fatalf("create expired token: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	w = doAgentChat(t, `{"messages":[{"role":"user","content":"hi"}]}`, map[string]string{
		"X-Agent-Token": expired,
	}, 0)
	assertEq(t, w.Code, http.StatusUnauthorized, "expired token status")
}

// agent token + scope 过滤：只下发读类工具；工具执行按 token.UserID 归属并写 token_id 审计。
func TestAgentChat_AgentTokenScopeFilter(t *testing.T) {
	matcher := &chatMockMatcher{}
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{equity: 999}, matcher)
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			// 带 W scope 的 token 可以放模拟单
			return "", []ai.ToolCall{{
				Name:      "place_paper_order",
				Arguments: `{"symbol":"BTCUSDT","side":"BUY","order_type":"MARKET","quantity":0.1}`,
			}}
		}
		return "已提交模拟买单。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	tm := agent.GetTokenManager()

	// scope=R：只读工具，place_paper_order 不应出现在下发列表里
	readOnly, _, err := tm.CreateTokenForUser("chat-scope-read", "R", 10, 3600, 42)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	w := doAgentChat(t, `{"messages":[{"role":"user","content":"看看"}]}`, map[string]string{
		"X-Agent-Token": readOnly,
	}, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	reqs := llm.Requests()
	names := toolNamesFromRequest(t, reqs[0])
	assertEq(t, len(names), 8, "read-scope tool count")
	for _, n := range names {
		switch n {
		case "place_paper_order", "cancel_order", "deploy_strategy", "start_strategy", "stop_strategy", "delete_strategy", "run_backtest", "list_backtests":
			t.Fatalf("tool %s should be filtered out for scope R", n)
		}
	}
	// 模型若仍请求未下发的写工具，执行侧必须拒绝（双保险）
	msgs, _ := reqs[1]["messages"].([]any)
	lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
	if !strings.Contains(fmt.Sprint(lastMsg["content"]), "not permitted") {
		t.Fatalf("forbidden tool should be refused, got %v", lastMsg["content"])
	}

	// scope=R,W：可以放模拟单；UserID=42 传入撮合；审计带 token_id
	full, recFull, err := tm.CreateTokenForUser("chat-scope-rw", "R,W", 10, 3600, 42)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	w = doAgentChat(t, `{"messages":[{"role":"user","content":"买 0.1 个 BTC"}]}`, map[string]string{
		"X-Agent-Token": full,
	}, 0)
	assertEq(t, w.Code, http.StatusOK, "status code")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["content"] != "已提交模拟买单。" {
		t.Fatalf("content = %v", body["content"])
	}
	assertEq(t, int(matcher.LastUser()), 42, "tool UserID from token")

	rec := lastAuditByName(t, "place_paper_order")
	if rec == nil {
		t.Fatal("audit record for place_paper_order not found")
	}
	assertEq(t, rec.TokenID, recFull.ID, "audit token_id")
}

// JWT 路径：注入的用户 ID 透传到工具（place_paper_order 的撮合 userID）。
func TestAgentChat_JWTUserIDPropagates(t *testing.T) {
	matcher := &chatMockMatcher{}
	injectAgentToolContext(chatMockMarket{}, chatMockPortfolio{}, matcher)
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		if !reqHasToolMessage(req) {
			return "", []ai.ToolCall{{
				Name:      "place_paper_order",
				Arguments: `{"symbol":"ETHUSDT","side":"SELL","order_type":"MARKET","quantity":1}`,
			}}
		}
		return "已提交模拟卖单。", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	w := doAgentChat(t, `{"messages":[{"role":"user","content":"卖 1 个 ETH"}]}`, nil, 7)
	assertEq(t, w.Code, http.StatusOK, "status code")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["content"] != "已提交模拟卖单。" {
		t.Fatalf("content = %v", body["content"])
	}
	assertEq(t, int(matcher.LastUser()), 7, "tool UserID from JWT")

	rec := lastAuditByName(t, "place_paper_order")
	if rec == nil {
		t.Fatal("audit record missing")
	}
	assertEq(t, rec.TokenID, 0, "JWT path audit token_id = 0")
}

// 解析链：未设 agent.ai.provider 时，回落到设置页的 default_ai_provider，
// 并采用 ai.{name} 里保存的 api_key（设置页保存路径）。
func TestConfiguredAgentAIProvider_ResolutionChain(t *testing.T) {
	cfg := store.GetConfig()
	prevDefault, hadDefault := cfg["default_ai_provider"]
	prevAI, hadAI := cfg["ai"]
	prevAgent, hadAgent := cfg["agent"]
	t.Cleanup(func() {
		restoreCfgKey(t, cfg, "default_ai_provider", prevDefault, hadDefault)
		restoreCfgKey(t, cfg, "ai", prevAI, hadAI)
		restoreCfgKey(t, cfg, "agent", prevAgent, hadAgent)
	})

	cfg["default_ai_provider"] = "kimi"
	cfg["ai"] = map[string]any{
		"kimi": map[string]any{"api_key": "sk-kimi-from-settings", "model": "kimi-k2.5"},
	}
	// 清掉 agent.ai.provider，验证回落链。
	agentCfg, _ := cfg["agent"].(map[string]any)
	if agentCfg != nil {
		delete(agentCfg, "ai")
	}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	p, name := configuredAgentAIProvider()
	if name != "kimi" {
		t.Fatalf("provider name = %s, want kimi", name)
	}
	if p == nil || p.APIKey != "sk-kimi-from-settings" {
		t.Fatalf("provider = %+v, want key from ai.kimi config", p)
	}
	if p.Model != "kimi-k2.5" {
		t.Fatalf("model = %s, want kimi-k2.5", p.Model)
	}

	// agent.ai.provider 显式设置时优先级最高。
	agentCfg = map[string]any{}
	cfg["agent"] = agentCfg
	agentCfg["ai"] = map[string]any{"provider": "deepseek"}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	_, name = configuredAgentAIProvider()
	if name != "deepseek" {
		t.Fatalf("agent.ai.provider 应优先，得到 %s", name)
	}
}

func restoreCfgKey(t *testing.T, cfg map[string]any, key string, prev any, had bool) {
	t.Helper()
	if had {
		cfg[key] = prev
	} else {
		delete(cfg, key)
	}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("restore config: %v", err)
	}
}

// 请求校验：无 user 消息 / 非法角色 → 400。
func TestAgentChat_BadRequest(t *testing.T) {
	w := doAgentChat(t, `{"messages":[{"role":"assistant","content":"hi"}]}`, nil, 0)
	assertEq(t, w.Code, http.StatusBadRequest, "no user message")
	w = doAgentChat(t, `{"messages":[{"role":"hacker","content":"hi"}]}`, nil, 0)
	assertEq(t, w.Code, http.StatusBadRequest, "bad role")
	w = doAgentChat(t, `{"messages":[]}`, nil, 0)
	assertEq(t, w.Code, http.StatusBadRequest, "empty messages")
}

// 客户端中途断开：循环静默终止，不发 done。
func TestAgentChat_ClientDisconnect(t *testing.T) {
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "", []ai.ToolCall{{Name: "get_balance", Arguments: `{}`}}
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	r := setupRouter()
	r.POST("/agent/chat", AgentChatStream)
	req := httptest.NewRequest(http.MethodPost, "/agent/chat",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}],"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithCancel(req.Context())
	cancel() // 模拟客户端已断开
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assertEq(t, len(llm.Requests()), 0, "no provider calls after disconnect")
	assertTrue(t, !strings.Contains(w.Body.String(), "event: done"), "no done event")
}

// AgentAITest：真实拨测成功 / 未配置两条路径。
func TestAgentAITest_RealProbe(t *testing.T) {
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) {
		return "pong", nil
	})
	registerMockAgentProvider(llm)
	withAgentAIProvider(t, "agent-chat-mock")

	r := setupRouter()
	r.POST("/agent/ai-test", AgentAITest)
	do := func() map[string]any {
		req := httptest.NewRequest(http.MethodPost, "/agent/ai-test", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusOK, "status code")
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("parse body: %v", err)
		}
		return body
	}

	body := do()
	if body["success"] != true || body["message"] != "OK" || body["model"] != "mock-model" {
		t.Fatalf("body = %v", body)
	}
	if _, ok := body["latency_ms"].(float64); !ok {
		t.Fatalf("latency_ms missing: %v", body)
	}
	// 拨测请求 max_tokens=1
	reqs := llm.Requests()
	if mt, _ := reqs[0]["max_tokens"].(float64); mt != 1 {
		t.Fatalf("max_tokens = %v", reqs[0]["max_tokens"])
	}

	// 未配置 provider
	withAgentAIProvider(t, "ghost-provider-not-registered")
	body = do()
	if body["success"] != false || !strings.Contains(fmt.Sprint(body["message"]), "not configured") {
		t.Fatalf("body = %v", body)
	}
}
