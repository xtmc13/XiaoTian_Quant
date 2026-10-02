package ai

import "testing"

// ContextWindowForModel：已知模型按前缀命中，openrouter 风格 vendor 前缀剥离，
// 未知/空回落 131072。
func TestContextWindowForModel(t *testing.T) {
	cases := map[string]int{
		"kimi-k2.5":              262144,
		"kimi-for-coding":        262144,
		"deepseek-chat":          131072,
		"deepseek-reasoner":      131072,
		"gpt-5.5":                1048576,
		"gpt-4.1":                1048576,
		"gpt-4o":                 131072,
		"o3":                     1048576,
		"claude-opus-4-7":        200000,
		"gemini-2.5-pro":         1048576,
		"gemini-3.1-pro-preview": 1048576,
		"qwen3-max":              262144,
		"glm-4.7":                131072,
		"doubao-seed-1-6":        262144,
		"hunyuan-pro":            131072,
		"llama-4-maverick":       1048576,
		"mistral-large-3":        131072,
		"openai/gpt-5.5":         1048576, // vendor 前缀剥离
		"unknown-model-xyz":      131072,
		"":                       131072,
	}
	for model, want := range cases {
		if got := ContextWindowForModel(model); got != want {
			t.Errorf("ContextWindowForModel(%q) = %d, want %d", model, got, want)
		}
	}
}
