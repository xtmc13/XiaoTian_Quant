package agentskills

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// Skill 一条技能（可复用的程序性指令）。
type Skill struct {
	ID          string `json:"id"`
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
	UsageCount  int    `json:"usage_count"`
	Source      string `json:"source"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// Repo 技能存储（xt_agent_skills）。
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

// NewID 生成技能 id。
func NewID() string {
	return fmt.Sprintf("sk_%d", time.Now().UnixNano())
}

// ErrDuplicateName 技能名冲突（同一用户）。
var ErrDuplicateName = errors.New("同名技能已存在")

// Upsert 创建或按 (user_id, name) 覆盖更新（对话中沉淀技能时同名即更新）。
func (r *Repo) Upsert(s *Skill) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	if s.Source == "" {
		s.Source = "user"
	}
	now := time.Now().Unix()
	s.CreatedAt, s.UpdatedAt = now, now
	_, err = db.Exec(`INSERT INTO xt_agent_skills
		(id, user_id, name, description, body, usage_count, source, created_at, updated_at)
		VALUES (?,?,?,?,?,0,?,?,?)
		ON CONFLICT(user_id, name) DO UPDATE SET
		description = excluded.description, body = excluded.body,
		source = excluded.source, updated_at = excluded.updated_at`,
		s.ID, s.UserID, s.Name, s.Description, s.Body, s.Source, s.CreatedAt, s.UpdatedAt)
	return err
}

const skillCols = "id, user_id, name, description, body, usage_count, source, created_at, updated_at"

func scanSkill(row interface{ Scan(...any) error }) (*Skill, error) {
	var s Skill
	err := row.Scan(&s.ID, &s.UserID, &s.Name, &s.Description, &s.Body, &s.UsageCount, &s.Source, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListByUser 列出用户技能（usage_count 降序，常用在前）。
func (r *Repo) ListByUser(userID int64, limit int) ([]*Skill, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := db.Query(`SELECT `+skillCols+` FROM xt_agent_skills WHERE user_id = ? ORDER BY usage_count DESC, updated_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Skill{}
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetByName 按名取技能（调用/执行用）。
func (r *Repo) GetByName(userID int64, name string) (*Skill, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	return scanSkill(db.QueryRow(`SELECT `+skillCols+` FROM xt_agent_skills WHERE user_id = ? AND name = ?`, userID, name))
}

// TouchUsage 调用计数 +1。
func (r *Repo) TouchUsage(id string, userID int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_agent_skills SET usage_count = usage_count + 1, updated_at = ? WHERE id = ? AND user_id = ?`,
		time.Now().Unix(), id, userID)
	return err
}

// Delete 删除（含用户校验）。
func (r *Repo) Delete(id string, userID int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM xt_agent_skills WHERE id = ? AND user_id = ?`, id, userID)
	return err
}
