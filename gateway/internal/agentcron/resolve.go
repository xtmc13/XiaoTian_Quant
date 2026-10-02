package agentcron

import (
	"fmt"
	"regexp"
)

// cronExprPattern 从文本中提取 5 段 cron 表达式。
var cronExprPattern = regexp.MustCompile(`([\d*,\-/?]+(?:\s+[\d*,\-/?]+){4})`)

// ResolveSchedule 解析 schedule 字段：先是 cron 表达式；解析失败且提供 nlResolver 时，
// 按自然语言走一次转换再校验。返回最终生效的 cron 表达式。
func ResolveSchedule(schedule string, nlResolver func(string) (string, error)) (string, error) {
	if _, err := ParseCron(schedule); err == nil {
		return schedule, nil
	}
	if nlResolver == nil {
		return "", fmt.Errorf("schedule 不是合法的 cron 表达式：%q（示例：0 8 * * *）", schedule)
	}
	raw, err := nlResolver(schedule)
	if err != nil {
		return "", err
	}
	m := cronExprPattern.FindString(raw)
	if m == "" {
		return "", fmt.Errorf("无法从模型回复中提取 cron 表达式：%q", trunc(raw, 120))
	}
	if _, err := ParseCron(m); err != nil {
		return "", fmt.Errorf("模型给出的表达式无效：%q（%v）", m, err)
	}
	return m, nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
