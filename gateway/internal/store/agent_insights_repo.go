package store

import (
	"database/sql"
	"encoding/json"
	"sort"
)

// ── AI Agent 学习闭环观测 Repository ──
// 用量报告（insights）与学习轨迹（journey）的只读聚合查询：
// 消息用量来自 xt_agent_messages（JOIN 会话表做用户归属），记忆/技能直接查
// xt_agent_memories / xt_agent_skills（均为秒级 unixepoch 时间戳）。

// AgentInsightsDay 窗口内按日用量（date 为本地时区 YYYY-MM-DD）。
type AgentInsightsDay struct {
	Date             string `json:"date"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	Rounds           int    `json:"rounds"` // completion_tokens>0 的 assistant 消息数
}

// AgentToolCount 工具调用计数（top_tools 排行）。
type AgentToolCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// AgentJourneyItem 学习轨迹事件（memory | skill | conversation）。
type AgentJourneyItem struct {
	Ts     int64  `json:"ts"`     // 秒级 Unix
	Kind   string `json:"kind"`   // memory | skill | conversation
	Title  string `json:"title"`  // 记忆=kind / 技能=name / 会话=title
	Detail string `json:"detail"` // 记忆=content(≤80字) / 技能=description / 会话=""
}

// GetInsightsTotals 窗口内（created_at>=since 的 assistant 行）用量合计与活跃天数。
func (r *AgentChatRepo) GetInsightsTotals(userID, since int64) (AgentUsageTotal, int, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return AgentUsageTotal{}, 0, sql.ErrConnDone
	}
	var t AgentUsageTotal
	err := db.QueryRow(`SELECT COALESCE(SUM(m.prompt_tokens),0), COALESCE(SUM(m.completion_tokens),0),
		COALESCE(SUM(m.llm_ms),0), COALESCE(SUM(CASE WHEN m.completion_tokens>0 THEN 1 ELSE 0 END),0)
		FROM xt_agent_messages m
		JOIN xt_agent_conversations c ON c.id=m.conversation_id
		WHERE c.user_id=? AND m.role='assistant' AND m.created_at>=?`, userID, since).
		Scan(&t.PromptTokens, &t.CompletionTokens, &t.LLMMs, &t.Rounds)
	if err != nil {
		return t, 0, err
	}
	var activeDays int
	err = db.QueryRow(`SELECT COUNT(DISTINCT date(m.created_at,'unixepoch','localtime'))
		FROM xt_agent_messages m
		JOIN xt_agent_conversations c ON c.id=m.conversation_id
		WHERE c.user_id=? AND m.role='assistant' AND m.created_at>=?`, userID, since).Scan(&activeDays)
	return t, activeDays, err
}

// GetInsightsByDay 窗口内按日用量（日期升序）。
func (r *AgentChatRepo) GetInsightsByDay(userID, since int64) ([]AgentInsightsDay, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	out := make([]AgentInsightsDay, 0, 8)
	rows, err := db.Query(`SELECT date(m.created_at,'unixepoch','localtime') d,
		COALESCE(SUM(m.prompt_tokens),0), COALESCE(SUM(m.completion_tokens),0),
		COALESCE(SUM(CASE WHEN m.completion_tokens>0 THEN 1 ELSE 0 END),0)
		FROM xt_agent_messages m
		JOIN xt_agent_conversations c ON c.id=m.conversation_id
		WHERE c.user_id=? AND m.role='assistant' AND m.created_at>=?
		GROUP BY d ORDER BY d ASC`, userID, since)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var d AgentInsightsDay
		if err := rows.Scan(&d.Date, &d.PromptTokens, &d.CompletionTokens, &d.Rounds); err != nil {
			return out, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetInsightsByModel 窗口内按 "provider:model" 用量（合计 token 降序）。
func (r *AgentChatRepo) GetInsightsByModel(userID, since int64) ([]AgentUsageModel, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	out := make([]AgentUsageModel, 0, 4)
	rows, err := db.Query(`SELECT c.model, COALESCE(SUM(m.prompt_tokens),0), COALESCE(SUM(m.completion_tokens),0)
		FROM xt_agent_messages m
		JOIN xt_agent_conversations c ON c.id=m.conversation_id
		WHERE c.user_id=? AND m.role='assistant' AND m.created_at>=? AND c.model<>''
		GROUP BY c.model ORDER BY SUM(m.prompt_tokens)+SUM(m.completion_tokens) DESC`, userID, since)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var m AgentUsageModel
		if err := rows.Scan(&m.Model, &m.PromptTokens, &m.CompletionTokens); err != nil {
			return out, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetTopTools 扫描窗口内 assistant 消息的 tool_calls JSON（元素 {name,...}），
// 按工具名计数，取调用次数前 limit（次数相同按名称字典序，保证输出稳定）。
func (r *AgentChatRepo) GetTopTools(userID, since int64, limit int) ([]AgentToolCount, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	if limit <= 0 {
		limit = 8
	}
	out := make([]AgentToolCount, 0, limit)
	rows, err := db.Query(`SELECT m.tool_calls FROM xt_agent_messages m
		JOIN xt_agent_conversations c ON c.id=m.conversation_id
		WHERE c.user_id=? AND m.role='assistant' AND m.created_at>=? AND m.tool_calls<>''`, userID, since)
	if err != nil {
		return out, err
	}
	counts := map[string]int{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return out, err
		}
		var calls []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(raw), &calls); err != nil {
			continue // 坏 JSON 行跳过，不影响整体统计
		}
		for _, cl := range calls {
			if cl.Name != "" {
				counts[cl.Name]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	for name, n := range counts {
		out = append(out, AgentToolCount{Name: name, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// GetLearningAdded 窗口内新增记忆数与技能数（created_at>=since）。
func (r *AgentChatRepo) GetLearningAdded(userID, since int64) (memories, skills int, err error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, 0, sql.ErrConnDone
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM xt_agent_memories WHERE user_id=? AND created_at>=?`, userID, since).Scan(&memories); err != nil {
		return 0, 0, err
	}
	err = db.QueryRow(`SELECT COUNT(*) FROM xt_agent_skills WHERE user_id=? AND created_at>=?`, userID, since).Scan(&skills)
	return memories, skills, err
}

// GetJourneyItems 合并记忆 / 技能 / 会话三类事件为学习轨迹：各取最近 limit 条后
// 按 ts 倒序归并，截断到 limit（同刻保持 memory→skill→conversation 的归并顺序）。
func (r *AgentChatRepo) GetJourneyItems(userID int64, limit int) ([]AgentJourneyItem, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	if limit <= 0 {
		limit = 50
	}
	items := make([]AgentJourneyItem, 0, limit)

	scan := func(query string, fill func(ts int64, a, b string)) error {
		rows, err := db.Query(query, userID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ts int64
			var a, b string
			if err := rows.Scan(&ts, &a, &b); err != nil {
				return err
			}
			fill(ts, a, b)
		}
		return rows.Err()
	}

	// 记忆：title=kind，detail=content（≤80 字）
	if err := scan(`SELECT created_at, kind, content FROM xt_agent_memories
		WHERE user_id=? ORDER BY created_at DESC LIMIT ?`, func(ts int64, kind, content string) {
		items = append(items, AgentJourneyItem{Ts: ts, Kind: "memory", Title: kind, Detail: clipRunes(content, 80)})
	}); err != nil {
		return items, err
	}
	// 技能：title=name，detail=description
	if err := scan(`SELECT created_at, name, description FROM xt_agent_skills
		WHERE user_id=? ORDER BY created_at DESC LIMIT ?`, func(ts int64, name, desc string) {
		items = append(items, AgentJourneyItem{Ts: ts, Kind: "skill", Title: name, Detail: desc})
	}); err != nil {
		return items, err
	}
	// 会话：title=title，detail=""
	if err := scan(`SELECT created_at, title, '' FROM xt_agent_conversations
		WHERE user_id=? ORDER BY created_at DESC LIMIT ?`, func(ts int64, title, _ string) {
		items = append(items, AgentJourneyItem{Ts: ts, Kind: "conversation", Title: title, Detail: ""})
	}); err != nil {
		return items, err
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].Ts > items[j].Ts })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// clipRunes 按 rune 截断到 n；截断时末位补省略号（总长仍 ≤n）。
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
