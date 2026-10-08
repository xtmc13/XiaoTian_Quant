package store

import (
	"strings"
	"testing"
	"time"
)

// ── 学习闭环观测 repo：insights 聚合 + journey 归并 ──

// seedInsightsData 造数：user 5 两个会话（窗口内 3 条 assistant + 窗口外 1 条），
// user 6 一条（隔离校验）；user 5 窗口内外各 1 条记忆与技能。返回窗口起点。
func seedInsightsData(t *testing.T, repo *AgentChatRepo) (now, since int64) {
	t.Helper()
	now = time.Now().Unix()
	since = now - 30*86400
	recent := now - 3600
	old := now - 40*86400

	convA := &AgentConversationRecord{UserID: 5, Title: "窗口会话", Model: "kimi:kimi-for-coding", CreatedAt: recent, UpdatedAt: recent}
	if err := repo.CreateConversation(convA); err != nil {
		t.Fatalf("create convA: %v", err)
	}
	msgs := []*AgentMessageRecord{
		{ConversationID: convA.ID, Role: "user", Content: "u1", CreatedAt: recent},
		{ConversationID: convA.ID, Role: "assistant", Content: "a1", CreatedAt: recent,
			PromptTokens: 100, CompletionTokens: 50, LLMMs: 1000,
			ToolCalls: `[{"name":"get_klines","status":"done"},{"name":"get_balance","status":"done"}]`},
		{ConversationID: convA.ID, Role: "assistant", Content: "a2", CreatedAt: recent,
			PromptTokens: 200, CompletionTokens: 0, LLMMs: 500},
		{ConversationID: convA.ID, Role: "assistant", Content: "a3", CreatedAt: recent,
			PromptTokens: 10, CompletionTokens: 5, LLMMs: 100,
			ToolCalls: `[{"name":"get_klines","status":"done"}]`},
		// 窗口外：不计入
		{ConversationID: convA.ID, Role: "assistant", Content: "old", CreatedAt: old,
			PromptTokens: 999, CompletionTokens: 999, LLMMs: 999,
			ToolCalls: `[{"name":"get_klines","status":"done"}]`},
	}
	for _, m := range msgs {
		if err := repo.InsertMessage(m); err != nil {
			t.Fatalf("insert msg: %v", err)
		}
	}
	convB := &AgentConversationRecord{UserID: 6, Title: "他人会话", Model: "deepseek:deepseek-chat", CreatedAt: recent, UpdatedAt: recent}
	if err := repo.CreateConversation(convB); err != nil {
		t.Fatalf("create convB: %v", err)
	}
	if err := repo.InsertMessage(&AgentMessageRecord{ConversationID: convB.ID, Role: "assistant", Content: "x",
		CreatedAt: recent, PromptTokens: 777, CompletionTokens: 777, LLMMs: 777}); err != nil {
		t.Fatalf("insert convB msg: %v", err)
	}

	// 记忆/技能：窗口内外各一条（user 5），他人一条记忆
	longContent := strings.Repeat("记", 100)
	stmt := `INSERT INTO xt_agent_memories (id, user_id, scope, kind, content, source_conversation_id, importance, origin, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`
	for _, row := range []struct{ id, content string; uid, ts int64 }{
		{"mem_i1", longContent, 5, recent},
		{"mem_i2", "旧记忆", 5, old},
		{"mem_i3", "他人记忆", 6, recent},
	} {
		if _, err := db.Exec(stmt, row.id, row.uid, "user", "fact", row.content, "", 3, "auto", row.ts, row.ts); err != nil {
			t.Fatalf("insert memory: %v", err)
		}
	}
	skillStmt := `INSERT INTO xt_agent_skills (id, user_id, name, description, body, usage_count, source, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`
	for _, row := range []struct {
		id, name string
		uid, ts  int64
	}{
		{"sk_i1", "复盘交易", 5, recent},
		{"sk_i2", "旧技能", 5, old},
	} {
		if _, err := db.Exec(skillStmt, row.id, row.uid, row.name, "每日复盘流程", "步骤", 7, "user", row.ts, row.ts); err != nil {
			t.Fatalf("insert skill: %v", err)
		}
	}
	return now, since
}

func TestInsightsRepoTotalsByDayByModel(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()
	_, since := seedInsightsData(t, repo)

	totals, activeDays, err := repo.GetInsightsTotals(5, since)
	if err != nil {
		t.Fatal(err)
	}
	if totals.PromptTokens != 310 || totals.CompletionTokens != 55 || totals.LLMMs != 1600 {
		t.Errorf("totals = %+v, want 310/55/1600", totals)
	}
	if totals.Rounds != 2 {
		t.Errorf("rounds = %d, want 2（completion>0 才计）", totals.Rounds)
	}
	if activeDays != 1 {
		t.Errorf("active_days = %d, want 1", activeDays)
	}

	byDay, err := repo.GetInsightsByDay(5, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(byDay) != 1 || byDay[0].PromptTokens != 310 || byDay[0].CompletionTokens != 55 || byDay[0].Rounds != 2 {
		t.Errorf("by_day = %+v", byDay)
	}
	// 种子时间戳是 now-3600，跨日窗口（00:00-01:00）会落前一日——断言跟随种子日期而非 time.Now()
	if want := time.Now().Add(-time.Hour).Format("2006-01-02"); byDay[0].Date != want {
		t.Errorf("by_day date = %q, want %q（种子 recent 当日）", byDay[0].Date, want)
	}

	byModel, err := repo.GetInsightsByModel(5, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(byModel) != 1 || byModel[0].Model != "kimi:kimi-for-coding" ||
		byModel[0].PromptTokens != 310 || byModel[0].CompletionTokens != 55 {
		t.Errorf("by_model = %+v", byModel)
	}

	// 用户隔离：user 6 只看自己的
	totals6, _, err := repo.GetInsightsTotals(6, since)
	if err != nil {
		t.Fatal(err)
	}
	if totals6.PromptTokens != 777 {
		t.Errorf("user6 totals = %+v, want 777", totals6)
	}
}

func TestInsightsRepoTopToolsAndLearningAdded(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()
	_, since := seedInsightsData(t, repo)

	tools, err := repo.GetTopTools(5, since, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("top_tools = %+v, want 2 项（窗口外 get_klines 不计）", tools)
	}
	if tools[0].Name != "get_klines" || tools[0].Count != 2 {
		t.Errorf("tools[0] = %+v, want get_klines x2", tools[0])
	}
	if tools[1].Name != "get_balance" || tools[1].Count != 1 {
		t.Errorf("tools[1] = %+v, want get_balance x1", tools[1])
	}

	mem, sk, err := repo.GetLearningAdded(5, since)
	if err != nil {
		t.Fatal(err)
	}
	if mem != 1 || sk != 1 {
		t.Errorf("learning added = %d/%d, want 1/1（窗口外不计）", mem, sk)
	}
}

func TestJourneyRepoMergeSortLimit(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()
	now, _ := seedInsightsData(t, repo)

	items, err := repo.GetJourneyItems(5, 50)
	if err != nil {
		t.Fatal(err)
	}
	// user 5：2 记忆 + 2 技能 + 1 会话 = 5
	if len(items) != 5 {
		t.Fatalf("journey len = %d, want 5: %+v", len(items), items)
	}
	// ts 倒序
	for i := 1; i < len(items); i++ {
		if items[i-1].Ts < items[i].Ts {
			t.Fatalf("journey 未按 ts 倒序: %+v", items)
		}
	}
	// 最新一条应为窗口内的记忆/技能/会话（ts = now-3600）
	if items[0].Ts != now-3600 {
		t.Errorf("items[0].Ts = %d, want %d", items[0].Ts, now-3600)
	}
	// 记忆条目字段与 80 字截断
	var memItem *AgentJourneyItem
	for i := range items {
		if items[i].Kind == "memory" && items[i].Title == "fact" && len(items[i].Detail) > 0 {
			memItem = &items[i]
			break
		}
	}
	if memItem == nil {
		t.Fatal("缺少 memory 条目")
	}
	if got := len([]rune(memItem.Detail)); got > 80 {
		t.Errorf("memory detail = %d 字, want ≤80", got)
	}
	// 技能与会话条目
	kinds := map[string]int{}
	for _, it := range items {
		kinds[it.Kind]++
		if it.Kind == "skill" && it.Title == "复盘交易" && it.Detail != "每日复盘流程" {
			t.Errorf("skill detail = %q", it.Detail)
		}
		if it.Kind == "conversation" && (it.Title != "窗口会话" || it.Detail != "") {
			t.Errorf("conversation item = %+v", it)
		}
	}
	if kinds["memory"] != 2 || kinds["skill"] != 2 || kinds["conversation"] != 1 {
		t.Errorf("kind 分布 = %v, want memory2/skill2/conversation1", kinds)
	}

	// limit 截断：取前 2
	top2, err := repo.GetJourneyItems(5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(top2) != 2 || top2[0].Ts != items[0].Ts || top2[1].Ts != items[1].Ts {
		t.Errorf("limit=2 结果 = %+v", top2)
	}

	// 用户隔离
	other, err := repo.GetJourneyItems(6, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 2 { // 1 会话 + 1 记忆
		t.Errorf("user6 journey = %+v, want 2 项", other)
	}
}
