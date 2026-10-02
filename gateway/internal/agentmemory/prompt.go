package agentmemory

import (
	"fmt"
	"strings"
)

// PromptSectionMaxChars 注入系统提示词的记忆总字符预算。
const PromptSectionMaxChars = 800

// LoadPromptSection 组装记忆段落（无记忆返回空串）。
// 由 agent 对话 runner 在每次请求时调用，注入系统提示词尾部。
func LoadPromptSection(userID int64) string {
	if userID <= 0 {
		return ""
	}
	mem, err := NewRepo().RecentForPrompt(userID, PromptSectionMaxChars, 8, 2)
	if err != nil || len(mem) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【用户记忆】以下关于该用户的记忆来自过往会话，回答时可参考（不必然相关）：\n")
	for _, m := range mem {
		label := map[string]string{
			"preference": "偏好", "observation": "观察", "market_note": "市场笔记", "fact": "事实",
		}[m.Kind]
		fmt.Fprintf(&b, "- [%s] %s\n", label, m.Content)
	}
	return b.String()
}
