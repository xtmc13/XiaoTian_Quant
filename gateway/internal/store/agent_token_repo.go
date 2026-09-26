package store

import (
	"fmt"
	"sync"
	"time"
)

// ── Agent Token Repository ──

type AgentTokenRecord struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	TokenHash    string `json:"token_hash"`
	TokenPrefix  string `json:"token_prefix"`
	Scopes       string `json:"scopes"`
	RateLimitRPS int    `json:"rate_limit_rps"`
	IsActive     int    `json:"is_active"`
	ExpiresAt    int64  `json:"expires_at"`
	LastUsedAt   int64  `json:"last_used_at"`
	CreatedAt    int64  `json:"created_at"`
	UserID       int64  `json:"user_id"` // 属主用户（0=历史无属主），C3 越权修复
}

type AgentTokenRepo struct{ mu sync.RWMutex }

func NewAgentTokenRepo() *AgentTokenRepo { return &AgentTokenRepo{} }

func (r *AgentTokenRepo) Create(t *AgentTokenRecord) error {
	if t.CreatedAt == 0 {
		t.CreatedAt = time.Now().UnixMilli()
	}
	if t.Scopes == "" {
		t.Scopes = "read"
	}
	if t.RateLimitRPS <= 0 {
		t.RateLimitRPS = 10
	}
	res, err := db.Exec(
		`INSERT INTO agent_tokens (name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at, user_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		t.Name, t.TokenHash, t.TokenPrefix, t.Scopes, t.RateLimitRPS, t.IsActive, t.ExpiresAt, t.LastUsedAt, t.CreatedAt, t.UserID,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	t.ID = int(id)
	return nil
}

func (r *AgentTokenRepo) GetByTokenHash(hash string) (*AgentTokenRecord, error) {
	row := db.QueryRow(`SELECT id, name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at, user_id FROM agent_tokens WHERE token_hash=?`, hash)
	var t AgentTokenRecord
	err := row.Scan(&t.ID, &t.Name, &t.TokenHash, &t.TokenPrefix, &t.Scopes, &t.RateLimitRPS, &t.IsActive, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt, &t.UserID)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *AgentTokenRepo) GetByID(id string) (*AgentTokenRecord, error) {
	row := db.QueryRow(`SELECT id, name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at, user_id FROM agent_tokens WHERE id=?`, id)
	var t AgentTokenRecord
	err := row.Scan(&t.ID, &t.Name, &t.TokenHash, &t.TokenPrefix, &t.Scopes, &t.RateLimitRPS, &t.IsActive, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt, &t.UserID)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetByIDForUser 取 token 并校验属主：非属主/不存在统一返回 not found（C3）。
func (r *AgentTokenRepo) GetByIDForUser(id string, userID int64) (*AgentTokenRecord, error) {
	row := db.QueryRow(`SELECT id, name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at, user_id FROM agent_tokens WHERE id=? AND user_id=?`, id, userID)
	var t AgentTokenRecord
	err := row.Scan(&t.ID, &t.Name, &t.TokenHash, &t.TokenPrefix, &t.Scopes, &t.RateLimitRPS, &t.IsActive, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt, &t.UserID)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *AgentTokenRepo) UpdateLastUsed(id int) error {
	_, err := db.Exec("UPDATE agent_tokens SET last_used_at=? WHERE id=?", time.Now().UnixMilli(), id)
	return err
}

func (r *AgentTokenRepo) Revoke(id int) error {
	_, err := db.Exec("UPDATE agent_tokens SET is_active=0 WHERE id=?", id)
	return err
}

func (r *AgentTokenRepo) List(filter map[string]any, limit int) ([]*AgentTokenRecord, error) {
	query := "SELECT id, name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at, user_id FROM agent_tokens"
	allowedCols := map[string]bool{
		"id": true, "name": true, "is_active": true, "expires_at": true, "created_at": true,
		"user_id": true, // C3: 列表按属主过滤
	}
	args, where := buildFilter(filter, allowedCols)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*AgentTokenRecord
	for rows.Next() {
		var t AgentTokenRecord
		if err := rows.Scan(&t.ID, &t.Name, &t.TokenHash, &t.TokenPrefix, &t.Scopes, &t.RateLimitRPS, &t.IsActive, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt, &t.UserID); err != nil {
			return nil, err
		}
		result = append(result, &t)
	}
	return result, nil
}

func (r *AgentTokenRepo) Update(t *AgentTokenRecord) error { return nil }
func (r *AgentTokenRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM agent_tokens WHERE id=?", id)
	return err
}

// DeleteForUser 删除 token 并带属主条件：非属主删不掉（C3）。
func (r *AgentTokenRepo) DeleteForUser(id string, userID int64) error {
	res, err := db.Exec("DELETE FROM agent_tokens WHERE id=? AND user_id=?", id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}
