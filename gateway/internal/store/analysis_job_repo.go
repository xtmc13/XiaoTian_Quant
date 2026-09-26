package store

import (
	"database/sql"
	"sync"
	"time"
)

// ── Analysis Job Repository ──
// 表结构见 migrations/sql/0025_analysis_jobs.sql。
// 回测偏差检测（lookahead / recursive）异步任务持久化。

type AnalysisJobRecord struct {
	ID           string `json:"id"`
	UserID       int64  `json:"user_id"`
	Kind         string `json:"kind"` // lookahead | recursive
	Status       string `json:"status"`
	Symbol       string `json:"symbol"`
	Interval     string `json:"interval"`
	StrategyType string `json:"strategy_type"`
	ParamsJSON   string `json:"params"`
	ResultJSON   string `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	CompletedAt  int64  `json:"completed_at,omitempty"`
}

type AnalysisJobRepo struct{ mu sync.RWMutex }

// NewAnalysisJobRepo creates a repo instance.
func NewAnalysisJobRepo() *AnalysisJobRepo { return &AnalysisJobRepo{} }

const analysisJobColumns = `id, user_id, kind, status, symbol, interval, strategy_type,
	params_json, result_json, error, created_at, updated_at, completed_at`

// Create inserts a new job record (status 通常为 running)。
func (r *AnalysisJobRepo) Create(rec *AnalysisJobRecord) error {
	now := time.Now().UnixMilli()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt == 0 {
		rec.UpdatedAt = rec.CreatedAt
	}
	if rec.ParamsJSON == "" {
		rec.ParamsJSON = "{}"
	}
	_, err := db.Exec(
		`INSERT INTO xt_analysis_jobs
			(id, user_id, kind, status, symbol, interval, strategy_type, params_json, result_json, error, created_at, updated_at, completed_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.Kind, rec.Status, rec.Symbol, rec.Interval, rec.StrategyType,
		rec.ParamsJSON, rec.ResultJSON, rec.Error, rec.CreatedAt, rec.UpdatedAt, rec.CompletedAt,
	)
	return err
}

// Finish 标记任务终态（completed / failed）并写入结果或错误。
func (r *AnalysisJobRepo) Finish(id, status, resultJSON, errMsg string) error {
	now := time.Now().UnixMilli()
	_, err := db.Exec(
		`UPDATE xt_analysis_jobs SET status = ?, result_json = ?, error = ?, updated_at = ?, completed_at = ? WHERE id = ?`,
		status, resultJSON, errMsg, now, now, id,
	)
	return err
}

// Delete 物理删除任务记录（handler 保证仅终态可删、属主校验）。
func (r *AnalysisJobRepo) Delete(id string) error {
	_, err := db.Exec(`DELETE FROM xt_analysis_jobs WHERE id = ?`, id)
	return err
}

// GetByID returns a single job by ID (属主校验由 handler 做)。
func (r *AnalysisJobRepo) GetByID(id string) (*AnalysisJobRecord, error) {
	row := db.QueryRow(`SELECT `+analysisJobColumns+` FROM xt_analysis_jobs WHERE id = ?`, id)
	return r.scan(row)
}

// List 按属主（含历史无属主 user_id=0）倒序列出任务；kind 可选过滤。
func (r *AnalysisJobRepo) List(userID int64, kind string, limit int) ([]*AnalysisJobRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT ` + analysisJobColumns + ` FROM xt_analysis_jobs WHERE 1=1`
	var args []any
	if userID > 0 {
		query += ` AND (user_id = ? OR user_id = 0)`
		args = append(args, userID)
	}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*AnalysisJobRecord, 0, limit)
	for rows.Next() {
		rec, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *AnalysisJobRepo) scan(s rowScanner) (*AnalysisJobRecord, error) {
	var rec AnalysisJobRecord
	err := s.Scan(&rec.ID, &rec.UserID, &rec.Kind, &rec.Status, &rec.Symbol, &rec.Interval,
		&rec.StrategyType, &rec.ParamsJSON, &rec.ResultJSON, &rec.Error,
		&rec.CreatedAt, &rec.UpdatedAt, &rec.CompletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}
