package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Token Manager ──

// TokenManager handles agent API bearer tokens.
type TokenManager struct {
	repo  *store.AgentTokenRepo
	audit *store.AuditRepo
	mu    sync.RWMutex
}

var (
	tokenMgr     *TokenManager
	tokenMgrOnce sync.Once
)

// GetTokenManager returns the global token manager.
func GetTokenManager() *TokenManager {
	tokenMgrOnce.Do(func() {
		tokenMgr = &TokenManager{
			repo:  store.NewAgentTokenRepo(),
			audit: store.NewAuditRepo(),
		}
	})
	return tokenMgr
}

// CreateToken generates a new bearer token.
func (tm *TokenManager) CreateToken(name, scopes string, rateLimitRPS int, expiresInSeconds int64) (string, error) {
	prefix := "qd_agent_"
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tokenValue := prefix + hex.EncodeToString(buf)

	tokenHash := sha256Hex(tokenValue)

	var expiresAt int64
	if expiresInSeconds > 0 {
		expiresAt = time.Now().UnixMilli() + expiresInSeconds*1000
	}

	err := tm.repo.Create(&store.AgentTokenRecord{
		Name:         name,
		TokenHash:    tokenHash,
		TokenPrefix:  prefix,
		Scopes:       scopes,
		RateLimitRPS: rateLimitRPS,
		IsActive:     1,
		ExpiresAt:    expiresAt,
		CreatedAt:    time.Now().UnixMilli(),
	})
	if err != nil {
		return "", err
	}

	return tokenValue, nil
}

// ValidateToken checks if a bearer token is valid and returns its record.
func (tm *TokenManager) ValidateToken(tokenValue string) (*store.AgentTokenRecord, error) {
	tokenHash := sha256Hex(tokenValue)

	record, err := tm.repo.GetByTokenHash(tokenHash)
	if err != nil {
		return nil, fmt.Errorf("invalid token")
	}

	if record.IsActive == 0 {
		return nil, fmt.Errorf("token revoked")
	}

	if record.ExpiresAt > 0 && record.ExpiresAt < time.Now().UnixMilli() {
		return nil, fmt.Errorf("token expired")
	}

	// Update last used
	_ = tm.repo.UpdateLastUsed(record.ID)

	return record, nil
}

// RevokeToken revokes a token by ID.
func (tm *TokenManager) RevokeToken(id int) error {
	return tm.repo.Revoke(id)
}

// ListTokens returns all tokens.
func (tm *TokenManager) ListTokens() ([]*store.AgentTokenRecord, error) {
	return tm.repo.List(nil, 100)
}

// DeleteToken deletes a token.
func (tm *TokenManager) DeleteToken(id string) error {
	return tm.repo.Delete(id)
}

// ── Audit ──

// LogAccess records an API access to the audit log.
func (tm *TokenManager) LogAccess(tokenID int, name, endpoint, method, paramsSummary string, statusCode int, ip, userAgent string) {
	_ = tm.audit.Log(&store.AuditRecord{
		TokenID:       tokenID,
		Name:          name,
		Endpoint:      endpoint,
		Method:        method,
		ParamsSummary: paramsSummary,
		StatusCode:    statusCode,
		IP:            ip,
		UserAgent:     userAgent,
		Timestamp:     time.Now().UnixMilli(),
	})
}

// GetAuditLogs returns recent audit records.
func (tm *TokenManager) GetAuditLogs(limit int) ([]*store.AuditRecord, error) {
	return tm.audit.GetRecent(limit)
}

// GetAuditByToken returns audit records for a specific token.
func (tm *TokenManager) GetAuditByToken(tokenID int, limit int) ([]*store.AuditRecord, error) {
	return tm.audit.GetByTokenID(tokenID, limit)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
