package store

import (
	"database/sql"
	"errors"
	"time"
)

// AgentUserAIRecord 用户级 AI 覆盖（表结构见 migrations/sql/0046_agent_user_ai.sql）。
type AgentUserAIRecord struct {
	UserID    int64  `json:"user_id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	APIKey    string `json:"-"` // 永不序列化（前端只给 has_key）
	UpdatedAt int64  `json:"updated_at"`
}

// AgentUserAIRepo xt_agent_user_ai 存取。
type AgentUserAIRepo struct{}

// NewAgentUserAIRepo 基于网关默认 SQLite。
func NewAgentUserAIRepo() *AgentUserAIRepo { return &AgentUserAIRepo{} }

func (r *AgentUserAIRepo) db() (*sql.DB, error) {
	db := GetDB()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	return db, nil
}

// Get 读用户级覆盖；无记录返回 (nil, nil)。
func (r *AgentUserAIRepo) Get(userID int64) (*AgentUserAIRecord, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var rec AgentUserAIRecord
	err = db.QueryRow(`SELECT user_id, provider, model, api_key, updated_at FROM xt_agent_user_ai WHERE user_id = ?`, userID).
		Scan(&rec.UserID, &rec.Provider, &rec.Model, &rec.APIKey, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// Upsert 写入/更新用户级覆盖。
func (r *AgentUserAIRepo) Upsert(rec *AgentUserAIRecord) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	rec.UpdatedAt = time.Now().Unix()
	_, err = db.Exec(`INSERT INTO xt_agent_user_ai (user_id, provider, model, api_key, updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET provider = excluded.provider, model = excluded.model, api_key = excluded.api_key, updated_at = excluded.updated_at`,
		rec.UserID, rec.Provider, rec.Model, rec.APIKey, rec.UpdatedAt)
	return err
}

// Delete 清除用户级覆盖。
func (r *AgentUserAIRepo) Delete(userID int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM xt_agent_user_ai WHERE user_id = ?`, userID)
	return err
}
