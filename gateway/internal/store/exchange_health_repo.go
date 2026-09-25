package store

import (
	"database/sql"
	"sync"
	"time"
)

// ── 交易所体检结果 Repository ──
// 表结构见 migrations/sql/0024_exchange_health.sql。

// ExchangeHealthRecord 是 xt_exchange_health_checks 的行记录。
type ExchangeHealthRecord struct {
	ID         string `json:"id"`
	Exchange   string `json:"exchange"`
	JobID      string `json:"job_id"`
	Overall    string `json:"overall"`
	Configured bool   `json:"configured"`
	DurationMs int64  `json:"duration_ms"`
	ResultJSON string `json:"result_json"`
	CreatedAt  int64  `json:"created_at"`
}

// ExchangeHealthRepo provides typed CRUD for xt_exchange_health_checks。
type ExchangeHealthRepo struct{ mu sync.RWMutex }

func NewExchangeHealthRepo() *ExchangeHealthRepo { return &ExchangeHealthRepo{} }

const exchangeHealthColumns = `id, exchange, job_id, overall, configured, duration_ms, result_json, created_at`

func scanExchangeHealth(s rowScanner) (*ExchangeHealthRecord, error) {
	var rec ExchangeHealthRecord
	var configured int
	err := s.Scan(&rec.ID, &rec.Exchange, &rec.JobID, &rec.Overall,
		&configured, &rec.DurationMs, &rec.ResultJSON, &rec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Configured = configured != 0
	return &rec, nil
}

// Insert 落库一条体检结果；补 id/时间戳。记录插入后 rec 携带落库值。
func (r *ExchangeHealthRepo) Insert(rec *ExchangeHealthRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec.ID == "" {
		rec.ID = generateShortID()
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	configured := 0
	if rec.Configured {
		configured = 1
	}
	_, err := db.Exec(`INSERT INTO xt_exchange_health_checks (`+exchangeHealthColumns+`)
		VALUES (?,?,?,?,?,?,?,?)`,
		rec.ID, rec.Exchange, rec.JobID, rec.Overall,
		configured, rec.DurationMs, rec.ResultJSON, rec.CreatedAt)
	return err
}

// LatestPerExchange 返回每家交易所最近一次体检结果（按 exchange 升序）。
func (r *ExchangeHealthRepo) LatestPerExchange() ([]*ExchangeHealthRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows, err := db.Query(`SELECT ` + exchangeHealthColumns + `
		FROM xt_exchange_health_checks h
		WHERE h.created_at = (SELECT MAX(created_at) FROM xt_exchange_health_checks WHERE exchange = h.exchange)
		ORDER BY h.exchange`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ExchangeHealthRecord
	for rows.Next() {
		rec, err := scanExchangeHealth(rows)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			out = append(out, rec)
		}
	}
	return out, rows.Err()
}

// History 返回体检历史（created_at 倒序）；exchange 为空表示全部交易所。
func (r *ExchangeHealthRepo) History(exchange string, limit int) ([]*ExchangeHealthRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT ` + exchangeHealthColumns + ` FROM xt_exchange_health_checks`
	args := []any{}
	if exchange != "" {
		query += ` WHERE exchange = ?`
		args = append(args, exchange)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ExchangeHealthRecord
	for rows.Next() {
		rec, err := scanExchangeHealth(rows)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			out = append(out, rec)
		}
	}
	return out, rows.Err()
}
