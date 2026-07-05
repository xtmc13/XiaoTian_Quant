package store

import (
	"sync"
	"time"
)

// ── Agent Audit Repository ──

type AuditRecord struct {
	ID            int    `json:"id"`
	TokenID       int    `json:"token_id"`
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	Method        string `json:"method"`
	ParamsSummary string `json:"params_summary"`
	StatusCode    int    `json:"status_code"`
	IP            string `json:"ip"`
	UserAgent     string `json:"user_agent"`
	Timestamp     int64  `json:"timestamp"`
}

type AuditRepo struct{ mu sync.RWMutex }

func NewAuditRepo() *AuditRepo { return &AuditRepo{} }

func (r *AuditRepo) Log(record *AuditRecord) error {
	if record.Timestamp == 0 {
		record.Timestamp = time.Now().UnixMilli()
	}
	if record.Method == "" {
		record.Method = "POST"
	}
	res, err := db.Exec(
		`INSERT INTO agent_audit_log (token_id, name, endpoint, method, params_summary, status_code, ip, user_agent, timestamp)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		record.TokenID, record.Name, record.Endpoint, record.Method, record.ParamsSummary, record.StatusCode, record.IP, record.UserAgent, record.Timestamp,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	record.ID = int(id)
	return nil
}

func (r *AuditRepo) GetRecent(limit int) ([]*AuditRecord, error) {
	rows, err := db.Query(`SELECT id, token_id, name, endpoint, method, params_summary, status_code, ip, user_agent, timestamp FROM agent_audit_log ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*AuditRecord
	for rows.Next() {
		var a AuditRecord
		if err := rows.Scan(&a.ID, &a.TokenID, &a.Name, &a.Endpoint, &a.Method, &a.ParamsSummary, &a.StatusCode, &a.IP, &a.UserAgent, &a.Timestamp); err != nil {
			return nil, err
		}
		result = append(result, &a)
	}
	return result, nil
}

func (r *AuditRepo) GetByTokenID(tokenID int, limit int) ([]*AuditRecord, error) {
	rows, err := db.Query(`SELECT id, token_id, name, endpoint, method, params_summary, status_code, ip, user_agent, timestamp FROM agent_audit_log WHERE token_id=? ORDER BY timestamp DESC LIMIT ?`, tokenID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*AuditRecord
	for rows.Next() {
		var a AuditRecord
		if err := rows.Scan(&a.ID, &a.TokenID, &a.Name, &a.Endpoint, &a.Method, &a.ParamsSummary, &a.StatusCode, &a.IP, &a.UserAgent, &a.Timestamp); err != nil {
			return nil, err
		}
		result = append(result, &a)
	}
	return result, nil
}

func (r *AuditRepo) GetByID(id string) (*AuditRecord, error) {
	row := db.QueryRow(`SELECT id, token_id, name, endpoint, method, params_summary, status_code, ip, user_agent, timestamp FROM agent_audit_log WHERE id=?`, id)
	var a AuditRecord
	err := row.Scan(&a.ID, &a.TokenID, &a.Name, &a.Endpoint, &a.Method, &a.ParamsSummary, &a.StatusCode, &a.IP, &a.UserAgent, &a.Timestamp)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *AuditRepo) List(filter map[string]any, limit int) ([]*AuditRecord, error) {
	return r.GetRecent(limit)
}
func (r *AuditRepo) Update(a *AuditRecord) error { return nil }
func (r *AuditRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM agent_audit_log WHERE id=?", id)
	return err
}
