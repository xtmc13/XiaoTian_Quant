package agentskills

import (
	"strings"
)

// CatalogMaxChars 注入系统提示词的技能目录字符预算。
const CatalogMaxChars = 600

// LoadCatalogSection 组装技能目录段落（无技能返回空串）。
// 注入系统提示词尾部，让 agent 知道有哪些技能可用（经 run_skill 调用）。
func LoadCatalogSection(userID int64) string {
	if userID <= 0 {
		return ""
	}
	skills, err := NewRepo().ListByUser(userID, 50)
	if err != nil || len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【可用技能】以下技能是可复用的程序性指令，用户输入 /技能名 或你调用 run_skill 即执行对应流程：\n")
	used := 0
	for _, s := range skills {
		line := "- /" + s.Name
		if s.Description != "" {
			line += "：" + s.Description
		}
		line += "\n"
		if used+len(line) > CatalogMaxChars {
			break
		}
		b.WriteString(line)
		used += len(line)
	}
	return b.String()
}
