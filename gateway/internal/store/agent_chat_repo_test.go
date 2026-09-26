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
