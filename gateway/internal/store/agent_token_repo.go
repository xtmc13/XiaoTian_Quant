package store

import (
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
		`INSERT INTO agent_tokens (name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		t.Name, t.TokenHash, t.TokenPrefix, t.Scopes, t.RateLimitRPS, t.IsActive, t.ExpiresAt, t.LastUsedAt, t.CreatedAt,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	t.ID = int(id)
	return nil
}

func (r *AgentTokenRepo) GetByTokenHash(hash string) (*AgentTokenRecord, error) {
	row := db.QueryRow(`SELECT id, name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at FROM agent_tokens WHERE token_hash=?`, hash)
	var t AgentTokenRecord
	err := row.Scan(&t.ID, &t.Name, &t.TokenHash, &t.TokenPrefix, &t.Scopes, &t.RateLimitRPS, &t.IsActive, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *AgentTokenRepo) GetByID(id string) (*AgentTokenRecord, error) {
	row := db.QueryRow(`SELECT id, name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at FROM agent_tokens WHERE id=?`, id)
	var t AgentTokenRecord
	err := row.Scan(&t.ID, &t.Name, &t.TokenHash, &t.TokenPrefix, &t.Scopes, &t.RateLimitRPS, &t.IsActive, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
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
	query := "SELECT id, name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at FROM agent_tokens"
	allowedCols := map[string]bool{
		"id": true, "name": true, "is_active": true, "expires_at": true, "created_at": true,
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
		if err := rows.Scan(&t.ID, &t.Name, &t.TokenHash, &t.TokenPrefix, &t.Scopes, &t.RateLimitRPS, &t.IsActive, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt); err != nil {
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
