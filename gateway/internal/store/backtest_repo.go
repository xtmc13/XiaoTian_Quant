package store

import (
	"sync"
	"time"
)

// ── Backtest Repository ──

type BacktestRecord struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Strategy    string `json:"strategy"`
	Symbol      string `json:"symbol"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	DurationMs  int64  `json:"duration_ms"`
	Status      string `json:"status"`
	ReportJSON  string `json:"report_json"`
	CreatedAt   int64  `json:"created_at"`
	CompletedAt int64  `json:"completed_at"`
}

type BacktestRepo struct{ mu sync.RWMutex }

func NewBacktestRepo() *BacktestRepo { return &BacktestRepo{} }

func (r *BacktestRepo) Create(b *BacktestRecord) error {
	if b.Status == "" {
		b.Status = "PENDING"
	}
	if b.CreatedAt == 0 {
		b.CreatedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(
		`INSERT INTO xt_backtests (id, name, strategy, symbol, start_time, end_time, duration_ms, status, report_json, created_at, completed_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.Name, b.Strategy, b.Symbol, b.StartTime, b.EndTime, b.DurationMs, b.Status, b.ReportJSON, b.CreatedAt, b.CompletedAt,
	)
	return err
}

func (r *BacktestRepo) GetByID(id string) (*BacktestRecord, error) {
	row := db.QueryRow(`SELECT id, name, strategy, symbol, start_time, end_time, duration_ms, status, report_json, created_at, completed_at FROM xt_backtests WHERE id=?`, id)
	var b BacktestRecord
	err := row.Scan(&b.ID, &b.Name, &b.Strategy, &b.Symbol, &b.StartTime, &b.EndTime, &b.DurationMs, &b.Status, &b.ReportJSON, &b.CreatedAt, &b.CompletedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *BacktestRepo) List(filter map[string]any, limit int) ([]*BacktestRecord, error) {
	query := "SELECT id, name, strategy, symbol, start_time, end_time, duration_ms, status, report_json, created_at, completed_at FROM xt_backtests"
	allowedCols := map[string]bool{
		"id": true, "name": true, "strategy": true, "symbol": true, "status": true,
		"created_at": true, "completed_at": true,
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
	var result []*BacktestRecord
	for rows.Next() {
		var b BacktestRecord
		if err := rows.Scan(&b.ID, &b.Name, &b.Strategy, &b.Symbol, &b.StartTime, &b.EndTime, &b.DurationMs, &b.Status, &b.ReportJSON, &b.CreatedAt, &b.CompletedAt); err != nil {
			return nil, err
		}
		result = append(result, &b)
	}
	return result, nil
}

func (r *BacktestRepo) Update(b *BacktestRecord) error {
	_, err := db.Exec(
		`UPDATE xt_backtests SET status=?, report_json=?, duration_ms=?, completed_at=? WHERE id=?`,
		b.Status, b.ReportJSON, b.DurationMs, b.CompletedAt, b.ID,
	)
	return err
}

func (r *BacktestRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_backtests WHERE id=?", id)
	return err
}
