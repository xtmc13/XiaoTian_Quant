package ai

import (
	"encoding/json"
	"sort"
)

// ── Tool Calling (function calling) ──

// Tool 描述一个可调用的工具（OpenAI tools / Anthropic tools / Gemini functionDeclarations 通用表示）
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction 工具的函数定义
type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"` // JSON Schema (map[string]any)
}

// ToolCall 一次工具调用（assistant 发起）
type ToolCall struct {
	ID        string `json:"id"`        // OpenAI 的 id；Anthropic 的 tool_use block id
	Name      string `json:"name"`      // 函数名
	Arguments string `json:"arguments"` // JSON 字符串
}

// MarshalJSON 按 OpenAI tool_call 嵌套格式序列化
func (tc ToolCall) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"id":   tc.ID,
		"type": "function",
		"function": map[string]any{
			"name":      tc.Name,
			"arguments": tc.Arguments,
		},
	})
}

// UnmarshalJSON 兼容 OpenAI 嵌套格式（{"id","type","function":{"name","arguments"}}）与扁平格式
func (tc *ToolCall) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Function  struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	tc.ID = raw.ID
	tc.Name = raw.Name
	tc.Arguments = raw.Arguments
	if raw.Function.Name != "" || raw.Function.Arguments != "" {
		if tc.Name == "" {
			tc.Name = raw.Function.Name
		}
		if tc.Arguments == "" {
			tc.Arguments = raw.Function.Arguments
		}
	}
	return nil
}

// ToolResult 工具执行结果，随 tool 消息回传给模型
type ToolResult struct {
	ToolCallID string `json:"tool_use_id"`    // Anthropic tool_use_id / OpenAI tool_call_id
	Name       string `json:"name,omitempty"` // Gemini functionResponse 需要函数名
	Content    string `json:"content"`
}

// HasToolCalls 判断响应是否包含工具调用
func HasToolCalls(resp *CompletionResponse) bool {
	return len(ExtractToolCalls(resp)) > 0
}

// ExtractToolCalls 提取工具调用列表（各协议解析时已归一到 Choices[0].Message.ToolCalls）
func ExtractToolCalls(resp *CompletionResponse) []ToolCall {
	if resp == nil || len(resp.Choices) == 0 {
		return nil
	}
	return resp.Choices[0].Message.ToolCalls
}

// toolCallAggregator 按 index 聚合流式 tool_calls 分片（OpenAI delta.tool_calls / Anthropic input_json_delta 共用）
type toolCallAggregator struct {
	calls map[int]*ToolCall
	order []int
}

func newToolCallAggregator() *toolCallAggregator {
	return &toolCallAggregator{calls: map[int]*ToolCall{}}
}

// add 合入一个分片：id/name 首次出现即记录，arguments 为分片字符串直接拼接
func (a *toolCallAggregator) add(index int, id, name, args string) {
	tc, ok := a.calls[index]
	if !ok {
		tc = &ToolCall{}
		a.calls[index] = tc
		a.order = append(a.order, index)
	}
	if id != "" {
		tc.ID = id
	}
	if name != "" {
		tc.Name = name
	}
	tc.Arguments += args
}

// result 按 index 升序输出聚合结果
func (a *toolCallAggregator) result() []ToolCall {
	if len(a.order) == 0 {
		return nil
	}
	sort.Ints(a.order)
	out := make([]ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		out = append(out, *a.calls[idx])
	}
	return out
}
