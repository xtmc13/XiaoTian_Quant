package store

import (
	"database/sql"
	"strconv"
	"strings"
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
	// 用量统计（仅 assistant 行落库）：跨 tool-calling 迭代累计的 token 与整轮耗时毫秒。
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	LLMMs            int64 `json:"llm_ms"`
	CreatedAt        int64 `json:"created_at"` // 秒级 Unix
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
		prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		llm_ms INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}
	// 旧库补用量列（SQLite ADD COLUMN 无 IF NOT EXISTS，重复列错误忽略）。
	for _, ddl := range []string{
		`ALTER TABLE xt_agent_messages ADD COLUMN prompt_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE xt_agent_messages ADD COLUMN completion_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE xt_agent_messages ADD COLUMN llm_ms INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(ddl); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
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
const agentMessageColumns = `id, conversation_id, role, content, reasoning, tool_calls, prompt_tokens, completion_tokens, llm_ms, created_at`

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
	err := s.Scan(&rec.ID, &rec.ConversationID, &rec.Role, &rec.Content, &rec.Reasoning, &rec.ToolCalls,
		&rec.PromptTokens, &rec.CompletionTokens, &rec.LLMMs, &rec.CreatedAt)
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
	if _, err := db.Exec(`INSERT INTO xt_agent_messages (conversation_id, role, content, reasoning, tool_calls, prompt_tokens, completion_tokens, llm_ms, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		rec.ConversationID, rec.Role, rec.Content, rec.Reasoning, rec.ToolCalls,
		rec.PromptTokens, rec.CompletionTokens, rec.LLMMs, rec.CreatedAt); err != nil {
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
		if _, err := tx.Exec(`INSERT INTO xt_agent_messages (conversation_id, role, content, reasoning, tool_calls, prompt_tokens, completion_tokens, llm_ms, created_at)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			conversationID, m.Role, m.Content, m.Reasoning, m.ToolCalls,
			m.PromptTokens, m.CompletionTokens, m.LLMMs, m.CreatedAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE xt_agent_conversations SET updated_at=? WHERE id=?`, now, conversationID); err != nil {
		return err
	}
	return tx.Commit()
}

// ── 用量统计 / 全文搜索 / 撤回一轮 ──

// AgentUsageTotal 用量汇总行（session 会话维度与 totals 用户维度共用）。
type AgentUsageTotal struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	LLMMs            int64 `json:"llm_ms"`
	Rounds           int   `json:"rounds"` // completion_tokens>0 的 assistant 消息数
}

// AgentUsageDay 按日用量（date 为本地时区 YYYY-MM-DD）。
type AgentUsageDay struct {
	Date             string `json:"date"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
}

// AgentUsageModel 按 "provider:model" 用量。
type AgentUsageModel struct {
	Model            string `json:"model"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
}

// AgentUsageSummary 用量汇总响应体：session（指定会话，未指定为 nil）、
// totals（用户全量）、by_day（最近 days 天，日期升序）、by_model。
type AgentUsageSummary struct {
	Session *AgentUsageTotal  `json:"session"`
	Totals  AgentUsageTotal   `json:"totals"`
	ByDay   []AgentUsageDay   `json:"by_day"`
	ByModel []AgentUsageModel `json:"by_model"`
}

// GetUsageSummary 汇总某用户的 agent 对话用量；conversationID 非空时附会话维度 session。
// userID 为十进制字符串（与 handler 层入参一致），解析失败按 0 处理。
func (r *AgentChatRepo) GetUsageSummary(userID, conversationID string, days int) (*AgentUsageSummary, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	uid, _ := strconv.ParseInt(userID, 10, 64)
	if days <= 0 {
		days = 30
	}
	out := &AgentUsageSummary{
		ByDay:   make([]AgentUsageDay, 0, days),
		ByModel: make([]AgentUsageModel, 0, 4),
	}
	scanTotal := func(row *sql.Row) (AgentUsageTotal, error) {
		var t AgentUsageTotal
		err := row.Scan(&t.PromptTokens, &t.CompletionTokens, &t.LLMMs, &t.Rounds)
		return t, err
	}
	// rounds 只计 completion_tokens>0 的 assistant 行；token/耗时为 assistant 行全量合计。
	const sumCols = `COALESCE(SUM(m.prompt_tokens),0), COALESCE(SUM(m.completion_tokens),0),
		COALESCE(SUM(m.llm_ms),0), COALESCE(SUM(CASE WHEN m.completion_tokens>0 THEN 1 ELSE 0 END),0)`

	if conversationID != "" {
		t, err := scanTotal(db.QueryRow(`SELECT `+sumCols+` FROM xt_agent_messages m
			WHERE m.conversation_id=? AND m.role='assistant'`, conversationID))
		if err != nil {
			return nil, err
		}
		out.Session = &t
	}
	totals, err := scanTotal(db.QueryRow(`SELECT `+sumCols+` FROM xt_agent_messages m
		JOIN xt_agent_conversations c ON c.id=m.conversation_id
		WHERE c.user_id=? AND m.role='assistant'`, uid))
	if err != nil {
		return nil, err
	}
	out.Totals = totals

	since := time.Now().AddDate(0, 0, -days).Unix()
	rows, err := db.Query(`SELECT date(m.created_at,'unixepoch','localtime') d,
		COALESCE(SUM(m.prompt_tokens),0), COALESCE(SUM(m.completion_tokens),0)
		FROM xt_agent_messages m
		JOIN xt_agent_conversations c ON c.id=m.conversation_id
		WHERE c.user_id=? AND m.role='assistant' AND m.created_at>=?
		GROUP BY d ORDER BY d ASC`, uid, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d AgentUsageDay
		if err := rows.Scan(&d.Date, &d.PromptTokens, &d.CompletionTokens); err != nil {
			rows.Close()
			return nil, err
		}
		out.ByDay = append(out.ByDay, d)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = db.Query(`SELECT c.model, COALESCE(SUM(m.prompt_tokens),0), COALESCE(SUM(m.completion_tokens),0)
		FROM xt_agent_messages m
		JOIN xt_agent_conversations c ON c.id=m.conversation_id
		WHERE c.user_id=? AND m.role='assistant' AND c.model<>''
		GROUP BY c.model ORDER BY SUM(m.prompt_tokens)+SUM(m.completion_tokens) DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m AgentUsageModel
		if err := rows.Scan(&m.Model, &m.PromptTokens, &m.CompletionTokens); err != nil {
			return nil, err
		}
		out.ByModel = append(out.ByModel, m)
	}
	return out, rows.Err()
}

// AgentConversationSearchResult 全文搜索结果行：snippet 为命中消息前后约 40 字的
// 上下文（命中标题时为空串）。
type AgentConversationSearchResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updated_at"`
	Snippet   string `json:"snippet"`
}

// escapeLikePattern 转义 LIKE 通配符（% / _ / \），配合 ESCAPE '\' 使用。
func escapeLikePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// SearchConversations 按关键字 LIKE 搜索用户会话（标题 + 消息内容），
// 按 updated_at 倒序，limit<=0 默认 20。snippet 取最早一条命中消息的前后约 40 字上下文。
func (r *AgentChatRepo) SearchConversations(userID int64, query string, limit int) ([]*AgentConversationSearchResult, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	if limit <= 0 {
		limit = 20
	}
	like := "%" + escapeLikePattern(query) + "%"
	rows, err := db.Query(`SELECT DISTINCT c.id, c.title, c.updated_at
		FROM xt_agent_conversations c
		LEFT JOIN xt_agent_messages m ON m.conversation_id=c.id
		WHERE c.user_id=? AND (c.title LIKE ? ESCAPE '\' OR m.content LIKE ? ESCAPE '\')
		ORDER BY c.updated_at DESC LIMIT ?`, userID, like, like, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*AgentConversationSearchResult, 0, limit)
	for rows.Next() {
		res := &AgentConversationSearchResult{}
		if err := rows.Scan(&res.ID, &res.Title, &res.UpdatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out = append(out, res)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	// 逐个会话取最早一条命中消息生成 snippet（命中标题的会话无命中消息，snippet 留空）。
	for _, res := range out {
		var content string
		err := db.QueryRow(`SELECT content FROM xt_agent_messages
			WHERE conversation_id=? AND content LIKE ? ESCAPE '\'
			ORDER BY created_at ASC, id ASC LIMIT 1`, res.ID, like).Scan(&content)
		if err != nil {
			continue // 无命中消息（标题命中）或读取失败：snippet 留空
		}
		res.Snippet = matchSnippet(content, query, 40)
	}
	return out, nil
}

// matchSnippet 截取 content 中首个命中位置前后共约 n 个字符的上下文（rune 计）。
func matchSnippet(content, query string, n int) string {
	runes := []rune(content)
	q := []rune(query)
	idx := -1
	lower := strings.ToLower(content)
	if i := strings.Index(lower, strings.ToLower(query)); i >= 0 {
		idx = len([]rune(lower[:i]))
	}
	if idx < 0 {
		if len(runes) <= n {
			return content
		}
		return string(runes[:n])
	}
	start := idx - (n-len(q))/2
	if start < 0 {
		start = 0
	}
	end := start + n
	if end > len(runes) {
		end = len(runes)
		start = end - n
		if start < 0 {
			start = 0
		}
	}
	snip := string(runes[start:end])
	if start > 0 {
		snip = "…" + snip
	}
	if end < len(runes) {
		snip += "…"
	}
	return snip
}

// UndoLastUserTurn 撤回一轮对话：删除会话中最后一条 user 消息及其后的全部
// assistant 消息；无 user 消息时为 no-op。返回剩余消息总数。
func (r *AgentChatRepo) UndoLastUserTurn(conversationID string) (int, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, sql.ErrConnDone
	}
	var lastUserID int64
	err := db.QueryRow(`SELECT id FROM xt_agent_messages WHERE conversation_id=? AND role='user'
		ORDER BY created_at DESC, id DESC LIMIT 1`, conversationID).Scan(&lastUserID)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if err == nil {
		if _, err := db.Exec(`DELETE FROM xt_agent_messages WHERE conversation_id=? AND id>=?
			AND role IN ('user','assistant')`, conversationID, lastUserID); err != nil {
			return 0, err
		}
	}
	var remaining int
	err = db.QueryRow(`SELECT COUNT(*) FROM xt_agent_messages WHERE conversation_id=?`, conversationID).Scan(&remaining)
	return remaining, err
}
