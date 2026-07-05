package store

import (
	"sync"
	"time"
)

// ── Risk Event Repository ──

type RiskEventRecord struct {
	ID        int    `json:"id"`
	Level     string `json:"level"`
	CheckName string `json:"check_name"`
	Message   string `json:"message"`
	Symbol    string `json:"symbol"`
	Context   string `json:"context"`
	Timestamp int64  `json:"timestamp"`
}

type RiskEventRepo struct{ mu sync.RWMutex }

func NewRiskEventRepo() *RiskEventRepo { return &RiskEventRepo{} }

func (r *RiskEventRepo) Create(e *RiskEventRecord) error {
	if e.Timestamp == 0 {
		e.Timestamp = time.Now().UnixMilli()
	}
	if e.Context == "" {
		e.Context = "{}"
	}
	res, err := db.Exec(
		`INSERT INTO risk_events (level, check_name, message, symbol, context_json, timestamp) VALUES (?,?,?,?,?,?)`,
		e.Level, e.CheckName, e.Message, e.Symbol, e.Context, e.Timestamp,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	e.ID = int(id)
	return nil
}

func (r *RiskEventRepo) List(filter map[string]any, limit int) ([]*RiskEventRecord, error) {
	query := "SELECT id, level, check_name, message, symbol, context_json, timestamp FROM risk_events"
	allowedCols := map[string]bool{
		"id": true, "level": true, "check_name": true, "symbol": true, "timestamp": true,
	}
	args, where := buildFilter(filter, allowedCols)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY timestamp DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*RiskEventRecord
	for rows.Next() {
		var e RiskEventRecord
		if err := rows.Scan(&e.ID, &e.Level, &e.CheckName, &e.Message, &e.Symbol, &e.Context, &e.Timestamp); err != nil {
			return nil, err
		}
		result = append(result, &e)
	}
	return result, nil
}

func (r *RiskEventRepo) GetByID(id string) (*RiskEventRecord, error) {
	row := db.QueryRow(`SELECT id, level, check_name, message, symbol, context_json, timestamp FROM risk_events WHERE id=?`, id)
	var e RiskEventRecord
	err := row.Scan(&e.ID, &e.Level, &e.CheckName, &e.Message, &e.Symbol, &e.Context, &e.Timestamp)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *RiskEventRepo) Update(e *RiskEventRecord) error { return nil }
func (r *RiskEventRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM risk_events WHERE id=?", id)
	return err
}
