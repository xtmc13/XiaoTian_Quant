package store

import (
	"database/sql"
	"sync"
	"time"
)

// ── AI Agent 会话 Repository ──
// 表 xt_agent_conversations / xt_agent_messages 由 EnsureAgentChatSchema 幂等创建
// （见文件内，独立于 schema.go，避免与迁移流程耦合）。
// 悬浮 AI 助手（POST /api/agent/chat）的多会话持久化：每轮对话落库 user 与
// assistant 消息（assistant 带 reasoning 与 tool_calls JSON 原文），会话行记录
// 实际使用的 "provider:model"，支持按会话维度的模型覆盖回溯。

// AgentConversationRecord 是 xt_agent_conversations 的行记录。
// Model 记录会话实际使用的 "provider:model"（如 "kimi:kimi-for-coding"）。
type AgentConversationRecord struct {
	ID        string `json:"id"`
	UserID    int64  `json:"user_id"`
	Title     string `json:"title"`
	Model     string `json:"model"`
	CreatedAt int64  `json:"created_at"` // 秒级 Unix
	UpdatedAt int64  `json:"updated_at"` // 秒级 Unix
}

// AgentMessageRecord 是 xt_agent_messages 的行记录。
// ToolCalls 为 JSON 字符串数组（元素 {name,args_summary,status,result_summary}），
// 原样存取，repo 层不做序列化。
type AgentMessageRecord struct {
	ID             int64  `json:"id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"` // user | assistant | system
	Content        string `json:"content"`
	Reasoning      string `json:"reasoning"`
	ToolCalls      string `json:"tool_calls"` // JSON 字符串数组，空为 ""
	CreatedAt      int64  `json:"created_at"` // 秒级 Unix
}

// EnsureAgentChatSchema 幂等创建 agent 会话/消息两张表（含常用查询索引）。
func EnsureAgentChatSchema() error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS xt_agent_conversations (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL DEFAULT 0,
		title TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_conversations_user_updated
		ON xt_agent_conversations (user_id, updated_at DESC)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS xt_agent_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		reasoning TEXT NOT NULL DEFAULT '',
		tool_calls TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_messages_conv_time
		ON xt_agent_messages (conversation_id, created_at)`)
	return err
}

// AgentChatRepo provides typed CRUD for xt_agent_conversations / xt_agent_messages。
type AgentChatRepo struct {
	mu         sync.RWMutex
	ensureOnce sync.Once
}

func NewAgentChatRepo() *AgentChatRepo { return &AgentChatRepo{} }

var (
	agentChatRepoOnce sync.Once
	agentChatRepoInst *AgentChatRepo
)

// DefaultAgentChatRepo 返回进程级共享 repo（懒 Ensure schema）。
func DefaultAgentChatRepo() *AgentChatRepo {
	agentChatRepoOnce.Do(func() {
		agentChatRepoInst = NewAgentChatRepo()
	})
	return agentChatRepoInst
}

// ensure 幂等建表（无视 db 为 nil 的情况，由调用方错误处理兜底）。
func (r *AgentChatRepo) ensure() {
	r.ensureOnce.Do(func() {
		_ = EnsureAgentChatSchema()
	})
}

const agentConversationColumns = `id, user_id, title, model, created_at, updated_at`
const agentMessageColumns = `id, conversation_id, role, content, reasoning, tool_calls, created_at`

func scanAgentConversation(s rowScanner) (*AgentConversationRecord, error) {
	var rec AgentConversationRecord
	err := s.Scan(&rec.ID, &rec.UserID, &rec.Title, &rec.Model, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func scanAgentMessage(s rowScanner) (*AgentMessageRecord, error) {
	var rec AgentMessageRecord
	err := s.Scan(&rec.ID, &rec.ConversationID, &rec.Role, &rec.Content, &rec.Reasoning, &rec.ToolCalls, &rec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// CreateConversation 插入一条会话；补 id（c_ 前缀短 ID）与时间戳。
func (r *AgentChatRepo) CreateConversation(rec *AgentConversationRecord) error {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return sql.ErrConnDone
	}
	if rec.ID == "" {
		rec.ID = "c_" + generateShortID()
	}
	now := time.Now().Unix()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt == 0 {
		rec.UpdatedAt = now
	}
	_, err := db.Exec(`INSERT INTO xt_agent_conversations (`+agentConversationColumns+`) VALUES (?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.Title, rec.Model, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// ListConversations 按用户拉取会话（updated_at 倒序，最近更新在前）；
// limit<=0 时默认 50，offset<0 时按 0 处理。
func (r *AgentChatRepo) ListConversations(userID int64, limit, offset int) ([]*AgentConversationRecord, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := db.Query(`SELECT `+agentConversationColumns+` FROM xt_agent_conversations
		WHERE user_id=? ORDER BY updated_at DESC LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*AgentConversationRecord, 0, limit)
	for rows.Next() {
		rec, err := scanAgentConversation(rows)
		if err != nil {
			return out, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetConversation 按 id 取会话；不存在返回 (nil, nil)。属主校验由 handler 负责。
func (r *AgentChatRepo) GetConversation(id string) (*AgentConversationRecord, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	row := db.QueryRow(`SELECT `+agentConversationColumns+` FROM xt_agent_conversations WHERE id=?`, id)
	return scanAgentConversation(row)
}

// RenameConversation 更新标题并刷新 updated_at。
func (r *AgentChatRepo) RenameConversation(id, title string) error {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return sql.ErrConnDone
	}
	_, err := db.Exec(`UPDATE xt_agent_conversations SET title=?, updated_at=? WHERE id=?`,
		title, time.Now().Unix(), id)
	return err
}

// SetConversationModel 记录会话实际使用的 "provider:model"（首次落库时由 handler 调用）。
func (r *AgentChatRepo) SetConversationModel(id, model string) error {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return sql.ErrConnDone
	}
	_, err := db.Exec(`UPDATE xt_agent_conversations SET model=? WHERE id=?`, model, id)
	return err
}

// DeleteConversation 删除会话并级联删除其全部消息。
func (r *AgentChatRepo) DeleteConversation(id string) error {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return sql.ErrConnDone
	}
	if _, err := db.Exec(`DELETE FROM xt_agent_messages WHERE conversation_id=?`, id); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM xt_agent_conversations WHERE id=?`, id)
	return err
}

// InsertMessage 插入一条消息；补时间戳并顺带刷新会话的 updated_at（保持列表排序准确）。
func (r *AgentChatRepo) InsertMessage(rec *AgentMessageRecord) error {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return sql.ErrConnDone
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().Unix()
	}
	if _, err := db.Exec(`INSERT INTO xt_agent_messages (conversation_id, role, content, reasoning, tool_calls, created_at)
		VALUES (?,?,?,?,?,?)`,
		rec.ConversationID, rec.Role, rec.Content, rec.Reasoning, rec.ToolCalls, rec.CreatedAt); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE xt_agent_conversations SET updated_at=? WHERE id=?`, rec.CreatedAt, rec.ConversationID)
	return err
}

// ListMessages 拉取会话全部消息（created_at 正序，同刻按自增 id 正序）。
func (r *AgentChatRepo) ListMessages(conversationID string) ([]*AgentMessageRecord, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	rows, err := db.Query(`SELECT `+agentMessageColumns+` FROM xt_agent_messages
		WHERE conversation_id=? ORDER BY created_at ASC, id ASC`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*AgentMessageRecord, 0, 16)
	for rows.Next() {
		rec, err := scanAgentMessage(rows)
		if err != nil {
			return out, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// DeleteLastAssistantMessage 删除会话最近一条 assistant 消息（regenerate 场景：
// 去掉上轮回答后，用最后一条 user 消息重跑）。
func (r *AgentChatRepo) DeleteLastAssistantMessage(conversationID string) error {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return sql.ErrConnDone
	}
	_, err := db.Exec(`DELETE FROM xt_agent_messages WHERE id=(
		SELECT id FROM xt_agent_messages WHERE conversation_id=? AND role='assistant'
		ORDER BY created_at DESC, id DESC LIMIT 1)`, conversationID)
	return err
}

// ReplaceMessages 整体替换会话消息（编辑消息后重跑场景）：事务内先删后插。
// msgs 的时间戳由调用方传入（保持与请求 messages 的顺序一致）。
func (r *AgentChatRepo) ReplaceMessages(conversationID string, msgs []*AgentMessageRecord) error {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return sql.ErrConnDone
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM xt_agent_messages WHERE conversation_id=?`, conversationID); err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, m := range msgs {
		if m.CreatedAt == 0 {
			m.CreatedAt = now
		}
		if _, err := tx.Exec(`INSERT INTO xt_agent_messages (conversation_id, role, content, reasoning, tool_calls, created_at)
			VALUES (?,?,?,?,?,?)`,
			conversationID, m.Role, m.Content, m.Reasoning, m.ToolCalls, m.CreatedAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE xt_agent_conversations SET updated_at=? WHERE id=?`, now, conversationID); err != nil {
		return err
	}
	return tx.Commit()
}
