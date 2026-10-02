package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupAgentChatTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-agent-chat-repo")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(CloseDB)
}

func TestAgentChatRepoConversationCRUD(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()

	rec := &AgentConversationRecord{UserID: 1, Title: "分析BTC"}
	if err := repo.CreateConversation(rec); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.ID == "" || !strings.HasPrefix(rec.ID, "c_") {
		t.Errorf("id = %q, want c_ 前缀", rec.ID)
	}
	if rec.CreatedAt == 0 || rec.UpdatedAt == 0 {
		t.Errorf("时间戳未补齐: %+v", rec)
	}

	got, err := repo.GetConversation(rec.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v / %+v", err, got)
	}
	if got.Title != "分析BTC" || got.UserID != 1 {
		t.Errorf("got %+v", got)
	}
	// 不存在 → (nil, nil)
	none, err := repo.GetConversation("c_ghost")
	if err != nil || none != nil {
		t.Errorf("空结果应 (nil,nil), got %v/%v", none, err)
	}

	// 重命名
	if err := repo.RenameConversation(rec.ID, "改名后"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, _ = repo.GetConversation(rec.ID)
	if got.Title != "改名后" {
		t.Errorf("rename 未生效: %q", got.Title)
	}

	// model 记录
	if err := repo.SetConversationModel(rec.ID, "kimi:kimi-for-coding"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	got, _ = repo.GetConversation(rec.ID)
	if got.Model != "kimi:kimi-for-coding" {
		t.Errorf("model = %q", got.Model)
	}
}

func TestAgentChatRepoListConversationsByUser(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()
	now := time.Now().Unix()

	for i, title := range []string{"A", "B", "C"} {
		rec := &AgentConversationRecord{UserID: 10, Title: title, CreatedAt: now - int64(i*100), UpdatedAt: now - int64(i*100)}
		if err := repo.CreateConversation(rec); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	repo.CreateConversation(&AgentConversationRecord{UserID: 11, Title: "他人"})

	// updated_at 倒序
	list, err := repo.ListConversations(10, 50, 0)
	if err != nil || len(list) != 3 {
		t.Fatalf("list: len=%d err=%v", len(list), err)
	}
	if list[0].Title != "A" || list[2].Title != "C" {
		t.Errorf("倒序错误: %v", []string{list[0].Title, list[1].Title, list[2].Title})
	}
	// limit / offset
	list, _ = repo.ListConversations(10, 2, 1)
	if len(list) != 2 || list[0].Title != "B" {
		t.Errorf("limit/offset: %+v", list)
	}
	// 用户隔离
	list, _ = repo.ListConversations(11, 50, 0)
	if len(list) != 1 || list[0].Title != "他人" {
		t.Errorf("用户隔离失败: %+v", list)
	}
}

func TestAgentChatRepoMessagesAndCascade(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()

	rec := &AgentConversationRecord{UserID: 2, Title: "t"}
	repo.CreateConversation(rec)
	for _, m := range []*AgentMessageRecord{
		{ConversationID: rec.ID, Role: "user", Content: "u1"},
		{ConversationID: rec.ID, Role: "assistant", Content: "a1", Reasoning: "思考", ToolCalls: `[{"name":"get_balance","args_summary":"{}","status":"done","result_summary":"ok"}]`},
		{ConversationID: rec.ID, Role: "user", Content: "u2"},
	} {
		if err := repo.InsertMessage(m); err != nil {
			t.Fatalf("insert message: %v", err)
		}
		if m.CreatedAt == 0 {
			t.Error("created_at 未补齐")
		}
	}

	msgs, err := repo.ListMessages(rec.ID)
	if err != nil || len(msgs) != 3 {
		t.Fatalf("list messages: len=%d err=%v", len(msgs), err)
	}
	if msgs[0].Role != "user" || msgs[0].Content != "u1" || msgs[1].Reasoning != "思考" {
		t.Errorf("messages = %+v", msgs)
	}
	// tool_calls 原样存取（JSON 字符串，不反序列化）
	if msgs[1].ToolCalls != `[{"name":"get_balance","args_summary":"{}","status":"done","result_summary":"ok"}]` {
		t.Errorf("tool_calls 被改写: %q", msgs[1].ToolCalls)
	}

	// InsertMessage 刷新会话 updated_at
	conv, _ := repo.GetConversation(rec.ID)
	if conv.UpdatedAt < msgs[2].CreatedAt {
		t.Errorf("updated_at 未刷新: conv=%d msg=%d", conv.UpdatedAt, msgs[2].CreatedAt)
	}

	// 级联删除：会话删除后消息清空
	if err := repo.DeleteConversation(rec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, _ := repo.GetConversation(rec.ID); got != nil {
		t.Error("会话未删除")
	}
	msgs, _ = repo.ListMessages(rec.ID)
	if len(msgs) != 0 {
		t.Errorf("级联删除失败: %d 条残留", len(msgs))
	}
}

func TestAgentChatRepoDeleteLastAssistantMessage(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()
	rec := &AgentConversationRecord{UserID: 3, Title: "t"}
	repo.CreateConversation(rec)
	repo.InsertMessage(&AgentMessageRecord{ConversationID: rec.ID, Role: "user", Content: "u1"})
	repo.InsertMessage(&AgentMessageRecord{ConversationID: rec.ID, Role: "assistant", Content: "a1"})
	repo.InsertMessage(&AgentMessageRecord{ConversationID: rec.ID, Role: "user", Content: "u2"})
	repo.InsertMessage(&AgentMessageRecord{ConversationID: rec.ID, Role: "assistant", Content: "a2"})

	if err := repo.DeleteLastAssistantMessage(rec.ID); err != nil {
		t.Fatalf("delete last assistant: %v", err)
	}
	msgs, _ := repo.ListMessages(rec.ID)
	if len(msgs) != 3 || msgs[2].Role != "user" || msgs[2].Content != "u2" {
		t.Fatalf("应删除 a2: %+v", msgs)
	}
	// 继续删除 a1 → 只剩两条 user
	if err := repo.DeleteLastAssistantMessage(rec.ID); err != nil {
		t.Fatalf("delete second: %v", err)
	}
	// 无 assistant 可删 → no-op
	if err := repo.DeleteLastAssistantMessage(rec.ID); err != nil {
		t.Fatalf("delete on no assistant: %v", err)
	}
	msgs, _ = repo.ListMessages(rec.ID)
	if len(msgs) != 2 {
		t.Errorf("不应再删除: %d", len(msgs))
	}
}

func TestAgentChatRepoReplaceMessages(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()
	rec := &AgentConversationRecord{UserID: 4, Title: "t"}
	repo.CreateConversation(rec)
	repo.InsertMessage(&AgentMessageRecord{ConversationID: rec.ID, Role: "user", Content: "旧1"})
	repo.InsertMessage(&AgentMessageRecord{ConversationID: rec.ID, Role: "assistant", Content: "旧2"})

	replaced := []*AgentMessageRecord{
		{Role: "user", Content: "新1"},
		{Role: "assistant", Content: "新2", Reasoning: "r"},
		{Role: "user", Content: "新3"},
	}
	if err := repo.ReplaceMessages(rec.ID, replaced); err != nil {
		t.Fatalf("replace: %v", err)
	}
	msgs, _ := repo.ListMessages(rec.ID)
	if len(msgs) != 3 || msgs[0].Content != "新1" || msgs[1].Reasoning != "r" || msgs[2].Content != "新3" {
		t.Errorf("replace 结果: %+v", msgs)
	}
	// 新插入的消息 id 自增且有序
	if !(msgs[0].ID < msgs[1].ID && msgs[1].ID < msgs[2].ID) {
		t.Errorf("id 顺序错误: %d %d %d", msgs[0].ID, msgs[1].ID, msgs[2].ID)
	}
}

func TestAgentChatSchemaIdempotent(t *testing.T) {
	setupAgentChatTestDB(t)
	for i := 0; i < 3; i++ {
		if err := EnsureAgentChatSchema(); err != nil {
			t.Fatalf("ensure %d: %v", i, err)
		}
	}
	for _, table := range []string{"xt_agent_conversations", "xt_agent_messages"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

func TestAgentChatRepoNoDBSafe(t *testing.T) {
	repo := NewAgentChatRepo()
	if err := repo.CreateConversation(&AgentConversationRecord{}); err == nil {
		t.Error("无 DB 应报错")
	}
	if _, err := repo.ListConversations(1, 10, 0); err == nil {
		t.Error("无 DB 应报错")
	}
	if _, err := repo.GetConversation("c_x"); err == nil {
		t.Error("无 DB 应报错")
	}
	if err := repo.InsertMessage(&AgentMessageRecord{}); err == nil {
		t.Error("无 DB 应报错")
	}
}

// 用量列：assistant 行的 prompt/completion/llm_ms 落库并可读回。
func TestAgentChatRepoUsageColumns(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()
	rec := &AgentConversationRecord{UserID: 5, Title: "t"}
	repo.CreateConversation(rec)
	repo.InsertMessage(&AgentMessageRecord{ConversationID: rec.ID, Role: "user", Content: "u"})
	repo.InsertMessage(&AgentMessageRecord{ConversationID: rec.ID, Role: "assistant", Content: "a",
		PromptTokens: 100, CompletionTokens: 42, LLMMs: 1500})

	msgs, err := repo.ListMessages(rec.ID)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("list: len=%d err=%v", len(msgs), err)
	}
	if msgs[0].PromptTokens != 0 || msgs[0].CompletionTokens != 0 {
		t.Errorf("user 行用量应为 0: %+v", msgs[0])
	}
	a := msgs[1]
	if a.PromptTokens != 100 || a.CompletionTokens != 42 || a.LLMMs != 1500 {
		t.Errorf("assistant 用量 = %+v", a)
	}
}

// GetUsageSummary：session / totals / by_day / by_model 四个维度。
func TestAgentChatRepoGetUsageSummary(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()
	now := time.Now().Unix()

	c1 := &AgentConversationRecord{UserID: 6, Title: "s1", Model: "kimi:k2"}
	repo.CreateConversation(c1)
	c2 := &AgentConversationRecord{UserID: 6, Title: "s2", Model: "deepseek:chat"}
	repo.CreateConversation(c2)
	other := &AgentConversationRecord{UserID: 7, Title: "他人", Model: "kimi:k2"}
	repo.CreateConversation(other)

	insert := func(convID string, createdAt int64, prompt, completion, llmMs int64) {
		if err := repo.InsertMessage(&AgentMessageRecord{
			ConversationID: convID, Role: "assistant", Content: "a", CreatedAt: createdAt,
			PromptTokens: prompt, CompletionTokens: completion, LLMMs: llmMs,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	insert(c1.ID, now, 10, 20, 100)
	insert(c1.ID, now-2*86400, 30, 40, 200)
	insert(c1.ID, now-40*86400, 999, 999, 999) // 超出 days 窗口：只进 totals 不进 by_day
	insert(c2.ID, now, 5, 0, 50)                // completion=0：计 token 不计 rounds
	insert(other.ID, now, 77, 88, 99)           // 他人数据：全部维度隔离

	s, err := repo.GetUsageSummary("6", c1.ID, 30)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if s.Session == nil {
		t.Fatal("session 应非空")
	}
	if s.Session.PromptTokens != 10+30+999 || s.Session.CompletionTokens != 20+40+999 || s.Session.LLMMs != 100+200+999 || s.Session.Rounds != 3 {
		t.Errorf("session = %+v", s.Session)
	}
	if s.Totals.PromptTokens != 10+30+999+5 || s.Totals.CompletionTokens != 20+40+999+0 || s.Totals.Rounds != 3 {
		t.Errorf("totals = %+v", s.Totals)
	}
	// by_day：30 天窗口内 3 条（c1 今天、昨天前天、c2 今天），日期升序，用户隔离
	var totalDay int64
	for _, d := range s.ByDay {
		totalDay += d.PromptTokens
	}
	if totalDay != 10+30+5 {
		t.Errorf("by_day 合计 = %d, rows=%+v", totalDay, s.ByDay)
	}
	for i := 1; i < len(s.ByDay); i++ {
		if s.ByDay[i-1].Date > s.ByDay[i].Date {
			t.Errorf("by_day 未按日期升序: %+v", s.ByDay)
		}
	}
	// by_model：按 token 合计降序，kimi 在前
	if len(s.ByModel) != 2 || s.ByModel[0].Model != "kimi:k2" || s.ByModel[1].Model != "deepseek:chat" {
		t.Errorf("by_model = %+v", s.ByModel)
	}
	if s.ByModel[0].CompletionTokens != 20+40+999 || s.ByModel[1].CompletionTokens != 0 {
		t.Errorf("by_model tokens = %+v", s.ByModel)
	}

	// 不指定会话：session 为 nil
	s2, err := repo.GetUsageSummary("6", "", 30)
	if err != nil || s2.Session != nil {
		t.Errorf("无 conversationID 时 session 应为 nil: %+v err=%v", s2, err)
	}
}

// SearchConversations：标题命中（snippet 空）、内容命中（snippet 含上下文）、
// LIKE 通配符转义、用户隔离、updated_at 倒序。
func TestAgentChatRepoSearchConversations(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()
	now := time.Now().Unix()

	titleHit := &AgentConversationRecord{UserID: 8, Title: "比特币行情分析", UpdatedAt: now - 10}
	repo.CreateConversation(titleHit)
	contentHit := &AgentConversationRecord{UserID: 8, Title: "普通对话", UpdatedAt: now}
	repo.CreateConversation(contentHit)
	repo.InsertMessage(&AgentMessageRecord{ConversationID: contentHit.ID, Role: "user",
		Content: "你好，请帮我分析一下最近比特币的走势和支撑阻力位，谢谢。"})
	other := &AgentConversationRecord{UserID: 9, Title: "比特币他人会话", UpdatedAt: now}
	repo.CreateConversation(other)
	repo.InsertMessage(&AgentMessageRecord{ConversationID: other.ID, Role: "user", Content: "比特币"})

	res, err := repo.SearchConversations(8, "比特币", 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("应命中 2 个本人会话（隔离他人）: %+v", res)
	}
	// updated_at 倒序：contentHit 在前
	if res[0].ID != contentHit.ID || res[1].ID != titleHit.ID {
		t.Fatalf("排序错误: %+v", res)
	}
	// 内容命中：snippet 含命中词与省略号上下文
	if !strings.Contains(res[0].Snippet, "比特币") {
		t.Errorf("snippet = %q", res[0].Snippet)
	}
	// 标题命中：snippet 为空
	if res[1].Snippet != "" {
		t.Errorf("标题命中 snippet 应为空: %q", res[1].Snippet)
	}

	// 通配符按字面匹配（% 不作为 LIKE 通配）
	res, _ = repo.SearchConversations(8, "100%", 20)
	if len(res) != 0 {
		t.Errorf("%% 未转义: %+v", res)
	}
	// 无命中 → 空数组
	res, _ = repo.SearchConversations(8, "不存在的词", 20)
	if len(res) != 0 {
		t.Errorf("应无命中: %+v", res)
	}
}

// UndoLastUserTurn：删除最后一条 user 及其后全部 assistant；连续撤回；无 user 时 no-op。
func TestAgentChatRepoUndoLastUserTurn(t *testing.T) {
	setupAgentChatTestDB(t)
	repo := NewAgentChatRepo()
	rec := &AgentConversationRecord{UserID: 10, Title: "t"}
	repo.CreateConversation(rec)
	for _, m := range []struct{ role, content string }{
		{"user", "u1"}, {"assistant", "a1"}, {"user", "u2"}, {"assistant", "a2"}, {"assistant", "a2b"},
	} {
		repo.InsertMessage(&AgentMessageRecord{ConversationID: rec.ID, Role: m.role, Content: m.content})
	}

	remaining, err := repo.UndoLastUserTurn(rec.ID)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("remaining = %d, want 2（u1,a1）", remaining)
	}
	msgs, _ := repo.ListMessages(rec.ID)
	if len(msgs) != 2 || msgs[1].Content != "a1" {
		t.Fatalf("应删除 u2/a2/a2b: %+v", msgs)
	}

	// 再撤一轮 → 空
	remaining, _ = repo.UndoLastUserTurn(rec.ID)
	if remaining != 0 {
		t.Fatalf("remaining = %d, want 0", remaining)
	}
	// 无 user 消息 → no-op
	remaining, err = repo.UndoLastUserTurn(rec.ID)
	if err != nil || remaining != 0 {
		t.Fatalf("no-op undo: remaining=%d err=%v", remaining, err)
	}
}
