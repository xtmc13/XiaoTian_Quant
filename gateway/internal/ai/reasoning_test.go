package ai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── reasoning_content 透传 ──
// OpenAI 兼容协议 delta.reasoning_content 聚合、非流式 choices[0].message.reasoning_content
// 解析、Anthropic thinking block / thinking_delta 聚合、Gemini thought 分片。

// OpenAI 流式：reasoning_content 分片聚合进第二返回值，并经回调第二参透传。
func TestProvider_Stream_OpenAI_ReasoningContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"reasoning_content":"先看"}}]}`,
			`{"choices":[{"delta":{"reasoning_content":"K线趋势，"}}]}`,
			`{"choices":[{"delta":{"content":"结论：偏多"}}]}`,
			`{"choices":[{"delta":{"reasoning_content":"再确认量能"}}]}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := newMockProvider("deepseek", server.URL, "sk", "deepseek-reasoner")

	var textDeltas, reasoningDeltas []string
	text, reasoning, calls, err := p.ChatCompletionStreamEx(CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "分析 BTC"}},
	}, func(delta, reasoningDelta string) {
		if delta != "" {
			textDeltas = append(textDeltas, delta)
		}
		if reasoningDelta != "" {
			reasoningDeltas = append(reasoningDeltas, reasoningDelta)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reasoning != "先看K线趋势，再确认量能" {
		t.Errorf("reasoning = %q", reasoning)
	}
	if strings.Join(reasoningDeltas, "") != reasoning {
		t.Errorf("reasoningDeltas = %v", reasoningDeltas)
	}
	if text != "结论：偏多" || strings.Join(textDeltas, "") != text {
		t.Errorf("text = %q, deltas = %v", text, textDeltas)
	}
	if len(calls) != 0 {
		t.Errorf("calls = %v, want empty", calls)
	}
}

// OpenAI 非流式：choices[0].message.reasoning_content 解析进 Message.ReasoningContent。
func TestProvider_ChatCompletion_OpenAI_ReasoningContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "chatcmpl-r1",
			"model": "kimi-for-coding",
			"choices": []map[string]any{{
				"index":   0,
				"message": map[string]any{"role": "assistant", "content": "答案是 42", "reasoning_content": "先列出所有因子再求和"},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newMockProvider("kimi", server.URL, "sk", "kimi-for-coding")
	result, err := p.ChatCompletion(CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "1+1?"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := result.Choices[0].Message
	if msg.Content != "答案是 42" {
		t.Errorf("content = %q", msg.Content)
	}
	if msg.ReasoningContent != "先列出所有因子再求和" {
		t.Errorf("reasoning_content = %q", msg.ReasoningContent)
	}
}

// Anthropic 非流式：content 里 type=thinking 的 block 拼入 ReasoningContent。
func TestProvider_ChatCompletion_Claude_Thinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "msg_think",
			"model": "claude-sonnet-4-6",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "用户在问价格，应该先查行情。"},
				{"type": "text", "text": "BTC 当前 67412.5 USDT。"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newMockProvider("claude", server.URL, "sk-ant", "claude-sonnet-4-6")
	result, err := p.ChatCompletion(CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "BTC 价格？"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := result.Choices[0].Message
	if msg.ReasoningContent != "用户在问价格，应该先查行情。" {
		t.Errorf("reasoning = %q", msg.ReasoningContent)
	}
	if msg.Content != "BTC 当前 67412.5 USDT。" {
		t.Errorf("content = %q", msg.Content)
	}
}

// Anthropic 流式：content_block_start(thinking) + thinking_delta 分片聚合。
func TestProvider_Stream_Claude_ThinkingDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		events := []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"先查"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"行情。"}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"已查询。"}}`,
			`{"type":"message_stop"}`,
		}
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			fl.Flush()
		}
	}))
	defer server.Close()

	p := newMockProvider("claude", server.URL, "sk-ant", "claude-sonnet-4-6")

	var reasoningDeltas []string
	text, reasoning, calls, err := p.ChatCompletionStreamEx(CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "BTC 价格"}},
	}, func(delta, reasoningDelta string) {
		if reasoningDelta != "" {
			reasoningDeltas = append(reasoningDeltas, reasoningDelta)
		}
	})
	_ = calls
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reasoning != "先查行情。" {
		t.Errorf("reasoning = %q", reasoning)
	}
	if strings.Join(reasoningDeltas, "") != reasoning {
		t.Errorf("reasoningDeltas = %v", reasoningDeltas)
	}
	if text != "已查询。" {
		t.Errorf("text = %q", text)
	}
}
