package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ═══════════════════════════════════════════════════════════════════
// Tool Calling Tests
// ═══════════════════════════════════════════════════════════════════

// sampleTools 测试共用的工具定义
func sampleTools() []Tool {
	return []Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "get_price",
			Description: "查询币种价格",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"symbol": map[string]any{"type": "string", "description": "交易对"}},
				"required":   []any{"symbol"},
			},
		},
	}}
}

func newMockProvider(name, baseURL, key, model string) *Provider {
	return &Provider{
		Name:     name,
		BaseURL:  baseURL,
		APIKey:   key,
		Model:    model,
		TimeoutS: 30,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

// decodeRequestBody 读取并解析 mock server 收到的请求体
func decodeRequestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("failed to decode request body: %v — %s", err, raw)
	}
	return body
}

// ── OpenAI 兼容路径 ──

func TestProvider_ChatCompletion_OpenAI_ToolCalls(t *testing.T) {
	var reqBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody = decodeRequestBody(t, r)

		resp := map[string]any{
			"id":    "chatcmpl-tool",
			"model": "gpt-4o",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]any{
						{
							"id":   "call_abc",
							"type": "function",
							"function": map[string]any{
								"name":      "get_price",
								"arguments": `{"symbol":"BTCUSDT"}`,
							},
						},
						{
							"id":   "call_def",
							"type": "function",
							"function": map[string]any{
								"name":      "get_price",
								"arguments": `{"symbol":"ETHUSDT"}`,
							},
						},
					},
				},
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newMockProvider("openai-mock", server.URL, "sk-mock", "gpt-4o")

	result, err := p.ChatCompletion(CompletionRequest{
		Messages:   []ChatMessage{{Role: RoleUser, Content: "BTC 和 ETH 价格多少"}},
		Tools:      sampleTools(),
		ToolChoice: "auto",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 请求体：tools 数组 + tool_choice
	tools, _ := reqBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("request tools len = %d, want 1", len(tools))
	}
	t0, _ := tools[0].(map[string]any)
	if t0["type"] != "function" {
		t.Errorf("tools[0].type = %v, want function", t0["type"])
	}
	fn, _ := t0["function"].(map[string]any)
	if fn["name"] != "get_price" || fn["description"] != "查询币种价格" {
		t.Errorf("tools[0].function = %v", fn)
	}
	if _, ok := fn["parameters"].(map[string]any); !ok {
		t.Errorf("tools[0].function.parameters missing: %v", fn)
	}
	if reqBody["tool_choice"] != "auto" {
		t.Errorf("tool_choice = %v, want auto", reqBody["tool_choice"])
	}

	// 响应解析：tool_calls 归一到 Choices[0].Message.ToolCalls
	if !HasToolCalls(result) {
		t.Fatal("expected tool calls in response")
	}
	calls := ExtractToolCalls(result)
	if len(calls) != 2 {
		t.Fatalf("tool calls len = %d, want 2", len(calls))
	}
	if calls[0].ID != "call_abc" || calls[0].Name != "get_price" {
		t.Errorf("calls[0] = %+v", calls[0])
	}
	if calls[0].Arguments != `{"symbol":"BTCUSDT"}` {
		t.Errorf("calls[0].Arguments = %q", calls[0].Arguments)
	}
	if calls[1].ID != "call_def" || calls[1].Arguments != `{"symbol":"ETHUSDT"}` {
		t.Errorf("calls[1] = %+v", calls[1])
	}
}

func TestProvider_ChatCompletionStream_OpenAI_ToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher support")
		}

		chunks := []string{
			// 首个分片：index 0 的 id/name 与 arguments 开头
			`data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_price","arguments":"{\"sym"}}]}}]}`,
			// arguments 跨分片拼接
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"bol\":\"BTCUSDT\"}"}}]}}]}`,
			// 第二个工具调用 index 1
			`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"place_order","arguments":"{\"side\":\"buy\"}"}}]}}]}`,
			`data: {"choices":[{"delta":{"content":"已完成"}}]}`,
			"data: [DONE]",
		}
		for _, chunk := range chunks {
			fmt.Fprintln(w, chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := newMockProvider("stream-tools", server.URL, "sk", "gpt-4o")

	var deltas []string
	text, reasoning, calls, err := p.ChatCompletionStreamEx(CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "买入 BTC"}},
		Tools:    sampleTools(),
	}, func(delta, _ string) {
		deltas = append(deltas, delta)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "已完成" {
		t.Errorf("full text = %q, want 已完成", text)
	}
	if reasoning != "" {
		t.Errorf("reasoning = %q, want empty", reasoning)
	}
	if strings.Join(deltas, "") != "已完成" {
		t.Errorf("deltas = %v, want [已完成]", deltas)
	}

	if len(calls) != 2 {
		t.Fatalf("tool calls len = %d, want 2", len(calls))
	}
	// index 0：id/name 首片记录，arguments 分片拼接
	if calls[0].ID != "call_1" || calls[0].Name != "get_price" {
		t.Errorf("calls[0] = %+v", calls[0])
	}
	if calls[0].Arguments != `{"symbol":"BTCUSDT"}` {
		t.Errorf("calls[0].Arguments = %q, want {\"symbol\":\"BTCUSDT\"}", calls[0].Arguments)
	}
	if calls[1].ID != "call_2" || calls[1].Name != "place_order" {
		t.Errorf("calls[1] = %+v", calls[1])
	}
	if calls[1].Arguments != `{"side":"buy"}` {
		t.Errorf("calls[1].Arguments = %q", calls[1].Arguments)
	}
}

// ── Anthropic 路径 ──

func TestProvider_ChatCompletion_Claude_ToolUse(t *testing.T) {
	var reqBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		reqBody = decodeRequestBody(t, r)

		resp := map[string]any{
			"id":    "msg_tool",
			"model": "claude-sonnet-4-6",
			"content": []map[string]any{
				{"type": "text", "text": "我来查询价格。"},
				{"type": "tool_use", "id": "toolu_01", "name": "get_price", "input": map[string]any{"symbol": "BTCUSDT"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newMockProvider("claude", server.URL, "sk-ant", "claude-sonnet-4-6")

	result, err := p.ChatCompletion(CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "BTC 价格？"}},
		Tools:    sampleTools(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 请求体：tools 应为 Anthropic 格式（name/description/input_schema，无嵌套 function）
	tools, _ := reqBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("request tools len = %d, want 1", len(tools))
	}
	t0, _ := tools[0].(map[string]any)
	if t0["name"] != "get_price" || t0["description"] != "查询币种价格" {
		t.Errorf("tool = %v", t0)
	}
	if _, ok := t0["input_schema"].(map[string]any); !ok {
		t.Errorf("tool input_schema missing: %v", t0)
	}
	if _, exists := t0["function"]; exists {
		t.Errorf("anthropic tool should not have nested function key: %v", t0)
	}

	// 响应解析：text 与 tool_use 混合 block
	if len(result.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(result.Choices))
	}
	msg := result.Choices[0].Message
	if msg.Content != "我来查询价格。" {
		t.Errorf("content = %q", msg.Content)
	}
	calls := ExtractToolCalls(result)
	if len(calls) != 1 {
		t.Fatalf("tool calls len = %d, want 1", len(calls))
	}
	if calls[0].ID != "toolu_01" || calls[0].Name != "get_price" {
		t.Errorf("call = %+v", calls[0])
	}
	if calls[0].Arguments != `{"symbol":"BTCUSDT"}` {
		t.Errorf("arguments = %q, want input map 序列化为 JSON", calls[0].Arguments)
	}
}

func TestProvider_ChatCompletion_Claude_ToolResult(t *testing.T) {
	var reqBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody = decodeRequestBody(t, r)

		resp := map[string]any{
			"id":      "msg_ok",
			"content": []map[string]any{{"type": "text", "text": "BTC 当前价格 65000"}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newMockProvider("claude", server.URL, "sk-ant", "claude-sonnet-4-6")

	_, err := p.ChatCompletion(CompletionRequest{
		Messages: []ChatMessage{
			{Role: RoleUser, Content: "BTC 价格？"},
			{
				Role:      RoleAssistant,
				Content:   "我来查询。",
				ToolCalls: []ToolCall{{ID: "toolu_01", Name: "get_price", Arguments: `{"symbol":"BTCUSDT"}`}},
			},
			{
				Role:       RoleUser,
				ToolResult: &ToolResult{ToolCallID: "toolu_01", Content: "65000"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs, _ := reqBody["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages len = %d, want 3", len(msgs))
	}

	// assistant 消息：text + tool_use blocks 混合
	m1, _ := msgs[1].(map[string]any)
	if m1["role"] != "assistant" {
		t.Errorf("msgs[1].role = %v, want assistant", m1["role"])
	}
	blocks, _ := m1["content"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("msgs[1] blocks len = %d, want 2 (text + tool_use)", len(blocks))
	}
	tu, _ := blocks[1].(map[string]any)
	if tu["type"] != "tool_use" || tu["id"] != "toolu_01" || tu["name"] != "get_price" {
		t.Errorf("tool_use block = %v", tu)
	}
	input, _ := tu["input"].(map[string]any)
	if input["symbol"] != "BTCUSDT" {
		t.Errorf("tool_use input = %v", tu["input"])
	}

	// tool 结果消息：tool_result block
	m2, _ := msgs[2].(map[string]any)
	if m2["role"] != "user" {
		t.Errorf("msgs[2].role = %v, want user", m2["role"])
	}
	rblocks, _ := m2["content"].([]any)
	if len(rblocks) != 1 {
		t.Fatalf("msgs[2] blocks len = %d, want 1", len(rblocks))
	}
	tr, _ := rblocks[0].(map[string]any)
	if tr["type"] != "tool_result" || tr["tool_use_id"] != "toolu_01" || tr["content"] != "65000" {
		t.Errorf("tool_result block = %v", tr)
	}
}

func TestProvider_ChatCompletionStream_Claude_ToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher support")
		}

		events := []string{
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"查询中"}}`,
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_9","name":"get_price","input":{}}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"sym"}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"bol\":\"BTCUSDT\"}"}}`,
			`data: {"type":"message_stop"}`,
		}
		for _, e := range events {
			fmt.Fprintln(w, e)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := newMockProvider("claude", server.URL, "sk-ant", "claude-sonnet-4-6")

	var deltas []string
	text, reasoning, calls, err := p.ChatCompletionStreamEx(CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "BTC 价格"}},
		Tools:    sampleTools(),
	}, func(d, _ string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "查询中" {
		t.Errorf("text = %q, want 查询中", text)
	}
	if reasoning != "" {
		t.Errorf("reasoning = %q, want empty", reasoning)
	}
	if strings.Join(deltas, "") != "查询中" {
		t.Errorf("deltas = %v", deltas)
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls len = %d, want 1", len(calls))
	}
	if calls[0].ID != "toolu_9" || calls[0].Name != "get_price" {
		t.Errorf("call = %+v", calls[0])
	}
	if calls[0].Arguments != `{"symbol":"BTCUSDT"}` {
		t.Errorf("arguments = %q, want partial_json 分片拼接结果", calls[0].Arguments)
	}
}

// ── Gemini 路径 ──

func TestProvider_ChatCompletion_Gemini_FunctionCall(t *testing.T) {
	var reqBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("path = %q, want :generateContent", r.URL.Path)
		}
		reqBody = decodeRequestBody(t, r)

		resp := map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{
					"role": "model",
					"parts": []map[string]any{
						{"text": "查询中"},
						{"functionCall": map[string]any{"name": "get_price", "args": map[string]any{"symbol": "BTCUSDT"}}},
					},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newMockProvider("gemini", server.URL, "gem-key", "gemini-2.5-pro")

	result, err := p.ChatCompletion(CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "BTC 价格？"}},
		Tools:    sampleTools(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 请求体：functionDeclarations
	tools, _ := reqBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("request tools len = %d, want 1", len(tools))
	}
	t0, _ := tools[0].(map[string]any)
	decls, _ := t0["functionDeclarations"].([]any)
	if len(decls) != 1 {
		t.Fatalf("functionDeclarations len = %d, want 1", len(decls))
	}
	d0, _ := decls[0].(map[string]any)
	if d0["name"] != "get_price" || d0["description"] != "查询币种价格" {
		t.Errorf("declaration = %v", d0)
	}

	// 响应解析：text + functionCall parts
	msg := result.Choices[0].Message
	if msg.Content != "查询中" {
		t.Errorf("content = %q", msg.Content)
	}
	calls := ExtractToolCalls(result)
	if len(calls) != 1 {
		t.Fatalf("tool calls len = %d, want 1", len(calls))
	}
	if calls[0].Name != "get_price" {
		t.Errorf("call name = %q", calls[0].Name)
	}
	if calls[0].Arguments != `{"symbol":"BTCUSDT"}` {
		t.Errorf("arguments = %q, want args map 序列化为 JSON", calls[0].Arguments)
	}
}

func TestProvider_ChatCompletion_Gemini_FunctionResponse(t *testing.T) {
	var reqBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody = decodeRequestBody(t, r)

		resp := map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{"role": "model", "parts": []map[string]any{{"text": "完成"}}},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newMockProvider("gemini", server.URL, "gem-key", "gemini-2.5-pro")

	_, err := p.ChatCompletion(CompletionRequest{
		Messages: []ChatMessage{
			{Role: RoleUser, Content: "BTC 价格？"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{Name: "get_price", Arguments: `{"symbol":"BTCUSDT"}`}}},
			{Role: RoleUser, ToolResult: &ToolResult{Name: "get_price", Content: "65000"}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, _ := reqBody["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents len = %d, want 3", len(contents))
	}

	// assistant 回传 functionCall part
	c1, _ := contents[1].(map[string]any)
	if c1["role"] != "model" {
		t.Errorf("contents[1].role = %v, want model", c1["role"])
	}
	parts1, _ := c1["parts"].([]any)
	fc, _ := parts1[0].(map[string]any)["functionCall"].(map[string]any)
	if fc["name"] != "get_price" {
		t.Errorf("functionCall name = %v", fc["name"])
	}

	// tool 结果 functionResponse part
	c2, _ := contents[2].(map[string]any)
	parts2, _ := c2["parts"].([]any)
	fr, _ := parts2[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "get_price" {
		t.Errorf("functionResponse name = %v", fr["name"])
	}
	respObj, _ := fr["response"].(map[string]any)
	if respObj["content"] != "65000" {
		t.Errorf("functionResponse.response = %v", respObj)
	}
}

// ── 序列化与辅助函数 ──

func TestCompletionRequest_ToolsSerialization(t *testing.T) {
	// 不带 tools：请求体与旧版一致（无 tools / tool_choice 字段）
	b, err := json.Marshal(CompletionRequest{
		Model:    "gpt-4o",
		Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	json.Unmarshal(b, &raw)
	if _, exists := raw["tools"]; exists {
		t.Error("tools field should be omitted when empty (backward compat)")
	}
	if _, exists := raw["tool_choice"]; exists {
		t.Error("tool_choice field should be omitted when empty (backward compat)")
	}

	// 带 tools：字段出现在请求体顶层
	b, _ = json.Marshal(CompletionRequest{
		Model:      "gpt-4o",
		Messages:   []ChatMessage{{Role: RoleUser, Content: "hi"}},
		Tools:      sampleTools(),
		ToolChoice: "auto",
	})
	json.Unmarshal(b, &raw)
	tools, ok := raw["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v", raw["tools"])
	}
	t0 := tools[0].(map[string]any)
	if t0["type"] != "function" {
		t.Errorf("tools[0].type = %v, want function", t0["type"])
	}
	if raw["tool_choice"] != "auto" {
		t.Errorf("tool_choice = %v, want auto", raw["tool_choice"])
	}
}

func TestToolCall_JSON(t *testing.T) {
	// OpenAI 嵌套格式反序列化
	var tc ToolCall
	err := json.Unmarshal([]byte(`{"id":"call_1","type":"function","function":{"name":"get_price","arguments":"{\"symbol\":\"BTC\"}"}}`), &tc)
	if err != nil {
		t.Fatal(err)
	}
	if tc.ID != "call_1" || tc.Name != "get_price" || tc.Arguments != `{"symbol":"BTC"}` {
		t.Errorf("unmarshaled = %+v", tc)
	}

	// 序列化为 OpenAI 嵌套格式
	b, err := json.Marshal(tc)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	json.Unmarshal(b, &raw)
	if raw["id"] != "call_1" || raw["type"] != "function" {
		t.Errorf("marshaled = %s", b)
	}
	fn := raw["function"].(map[string]any)
	if fn["name"] != "get_price" || fn["arguments"] != `{"symbol":"BTC"}` {
		t.Errorf("function = %v", fn)
	}
}

func TestHasToolCalls(t *testing.T) {
	if HasToolCalls(nil) {
		t.Error("nil response should have no tool calls")
	}
	if HasToolCalls(&CompletionResponse{}) {
		t.Error("empty response should have no tool calls")
	}
	resp := &CompletionResponse{Choices: []Choice{{
		Message: ChatMessage{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "f"}}},
	}}}
	if !HasToolCalls(resp) {
		t.Error("expected tool calls detected")
	}
	if got := ExtractToolCalls(resp); len(got) != 1 || got[0].ID != "c1" {
		t.Errorf("ExtractToolCalls = %v", got)
	}
	if got := ExtractToolCalls(&CompletionResponse{}); got != nil {
		t.Errorf("ExtractToolCalls(empty) = %v, want nil", got)
	}
}
