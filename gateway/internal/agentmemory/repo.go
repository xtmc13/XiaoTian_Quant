package agentmemory

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// Memory 一条记忆。
type Memory struct {
	ID                   string `json:"id"`
	UserID               int64  `json:"user_id"`
	Scope                string `json:"scope"`
	Kind                 string `json:"kind"`
	Content              string `json:"content"`
	SourceConversationID string `json:"source_conversation_id"`
	Importance           int    `json:"importance"`
	CreatedAt            int64  `json:"created_at"`
	UpdatedAt            int64  `json:"updated_at"`
}

// Repo 记忆存储（xt_agent_memories）。
type Repo struct{}

// NewRepo 基于网关默认 SQLite。
func NewRepo() *Repo { return &Repo{} }

func (r *Repo) db() (*sql.DB, error) {
	db := store.GetDB()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	return db, nil
}

// NewID 生成记忆 id。
func NewID() string {
	return fmt.Sprintf("mem_%d", time.Now().UnixNano())
}

var validKinds = map[string]bool{"fact": true, "preference": true, "observation": true, "market_note": true}

// Create 写入记忆；kind 非法回落 fact。
func (r *Repo) Create(m *Memory) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	if !validKinds[m.Kind] {
		m.Kind = "fact"
	}
	if m.Scope == "" {
		m.Scope = "user"
	}
	if m.Importance < 1 {
		m.Importance = 1
	}
	if m.Importance > 5 {
		m.Importance = 5
	}
	now := time.Now().Unix()
	m.CreatedAt, m.UpdatedAt = now, now
	_, err = db.Exec(`INSERT INTO xt_agent_memories
		(id, user_id, scope, kind, content, source_conversation_id, importance, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		m.ID, m.UserID, m.Scope, m.Kind, m.Content, m.SourceConversationID, m.Importance, m.CreatedAt, m.UpdatedAt)
	return err
}

const memCols = "id, user_id, scope, kind, content, source_conversation_id, importance, created_at, updated_at"

func scanMem(row interface{ Scan(...any) error }) (*Memory, error) {
	var m Memory
	err := row.Scan(&m.ID, &m.UserID, &m.Scope, &m.Kind, &m.Content, &m.SourceConversationID, &m.Importance, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListByUser 列出用户记忆（importance 降序、updated_at 降序），可按 kind 过滤（空 = 全部）。
func (r *Repo) ListByUser(userID int64, kind string, limit int) ([]*Memory, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := `SELECT ` + memCols + ` FROM xt_agent_memories WHERE user_id = ?`
	args := []any{userID}
	if kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	q += ` ORDER BY importance DESC, updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Memory{}
	for rows.Next() {
		m, err := scanMem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Search 关键词检索（内容 LIKE，多词空格分隔取交集）。
func (r *Repo) Search(userID int64, query string, limit int) ([]*Memory, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	words := strings.Fields(query)
	q := `SELECT ` + memCols + ` FROM xt_agent_memories WHERE user_id = ?`
	args := []any{userID}
	for _, w := range words {
		q += ` AND content LIKE ?`
		args = append(args, "%"+w+"%")
	}
	q += ` ORDER BY importance DESC, updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Memory{}
	for rows.Next() {
		m, err := scanMem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RecentForPrompt 取注入系统提示词的记忆：优先重要记忆（≥minImp），
// 低重要度最多带 3 条兜底；总条数 ≤ limit、总字符 ≤ budgetChars。
func (r *Repo) RecentForPrompt(userID int64, budgetChars, limit, minImp int) ([]*Memory, error) {
	if minImp < 1 {
		minImp = 2
	}
	list, err := r.ListByUser(userID, "", 50)
	if err != nil {
		return nil, err
	}
	out := []*Memory{}
	used := 0
	lowImp := 0
	for _, m := range list {
		if len(out) >= limit {
			break
		}
		if m.Importance < minImp {
			if lowImp >= 3 {
				continue
			}
			lowImp++
		}
		if used+len(m.Content) > budgetChars {
			continue
		}
		out = append(out, m)
		used += len(m.Content)
	}
	return out, nil
}

// Delete 删除（含用户校验）。
func (r *Repo) Delete(id string, userID int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM xt_agent_memories WHERE id = ? AND user_id = ?`, id, userID)
	return err
}
