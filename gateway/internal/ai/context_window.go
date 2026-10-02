package ai

import "strings"

// ── 模型上下文窗口元数据 ──
// 供 /config/ai-models 目录按 current_model 暴露 context_window（前端据此
// 显示上下文用量条）。按前缀匹配（openrouter 等聚合模型先剥离 "vendor/" 前缀），
// 未知模型回落 131072。

// modelContextWindowRule 前缀规则；先命中先生效，注意长前缀排前。
type modelContextWindowRule struct {
	prefix string
	window int
}

var modelContextWindowRules = []modelContextWindowRule{
	{"kimi-for-coding", 262144},
	{"kimi-k2.5", 262144},
	{"deepseek-chat", 131072},
	{"deepseek-reasoner", 131072},
	{"gpt-5", 1048576},
	{"gpt-4.1", 1048576},
	{"gpt-4o", 131072},
	{"o3", 1048576},
	{"claude-", 200000},
	{"gemini-2.5", 1048576},
	{"gemini-3", 1048576},
	{"qwen3-max", 262144},
	{"glm-4.7", 131072},
	{"doubao-seed-1-6", 262144},
	{"hunyuan-pro", 131072},
	{"llama-4-maverick", 1048576},
	{"mistral-large-3", 131072},
}

// defaultModelContextWindow 未知模型的兜底上下文窗口。
const defaultModelContextWindow = 131072

// ContextWindowForModel 按模型名查上下文窗口（tokens）；空名/未知 → 131072。
func ContextWindowForModel(model string) int {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return defaultModelContextWindow
	}
	// 聚合网关模型带 "vendor/" 前缀（openrouter 的 openai/gpt-5.5 等），剥离后匹配。
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	for _, r := range modelContextWindowRules {
		if strings.HasPrefix(m, r.prefix) {
			return r.window
		}
	}
	return defaultModelContextWindow
}
