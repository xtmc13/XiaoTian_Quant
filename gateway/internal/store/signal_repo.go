package store

import (
	"sync"
	"time"
)

// ── Signal Repository ──

type SignalRecord struct {
	ID              int     `json:"id"`
	Symbol          string  `json:"symbol"`
	Direction       string  `json:"direction"`
	Strength        float64 `json:"strength"`
	Strategy        string  `json:"strategy"`
	Reason          string  `json:"reason"`
	EntryPrice      float64 `json:"entry_price"`
	StopLoss        float64 `json:"stop_loss"`
	TakeProfit      float64 `json:"take_profit"`
	PositionSize    float64 `json:"position_size"`
	Status          string  `json:"status"`
	ExecutedOrderID string  `json:"executed_order_id"`
	CreatedAt       int64   `json:"created_at"`
}

type SignalRepo struct{ mu sync.RWMutex }

func NewSignalRepo() *SignalRepo { return &SignalRepo{} }

func (r *SignalRepo) Create(s *SignalRecord) error {
	if s.Status == "" {
		s.Status = "PENDING"
	}
	if s.CreatedAt == 0 {
		s.CreatedAt = time.Now().UnixMilli()
	}
	res, err := db.Exec(
		`INSERT INTO xt_signals (symbol, direction, strength, strategy, reason, entry_price, stop_loss, take_profit, position_size, status, executed_order_id, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.Symbol, s.Direction, s.Strength, s.Strategy, s.Reason, s.EntryPrice, s.StopLoss, s.TakeProfit, s.PositionSize, s.Status, s.ExecutedOrderID, s.CreatedAt,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	s.ID = int(id)
	return nil
}

func (r *SignalRepo) GetByID(id string) (*SignalRecord, error) {
	row := db.QueryRow(`SELECT id, symbol, direction, strength, strategy, reason, entry_price, stop_loss, take_profit, position_size, status, executed_order_id, created_at FROM xt_signals WHERE id=?`, id)
	var s SignalRecord
	err := row.Scan(&s.ID, &s.Symbol, &s.Direction, &s.Strength, &s.Strategy, &s.Reason, &s.EntryPrice, &s.StopLoss, &s.TakeProfit, &s.PositionSize, &s.Status, &s.ExecutedOrderID, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SignalRepo) List(filter map[string]any, limit int) ([]*SignalRecord, error) {
	query := "SELECT id, symbol, direction, strength, strategy, reason, entry_price, stop_loss, take_profit, position_size, status, executed_order_id, created_at FROM xt_signals"
	allowedCols := map[string]bool{
		"id": true, "symbol": true, "direction": true, "strength": true, "strategy": true,
		"status": true, "executed_order_id": true, "created_at": true,
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
	var result []*SignalRecord
	for rows.Next() {
		var s SignalRecord
		if err := rows.Scan(&s.ID, &s.Symbol, &s.Direction, &s.Strength, &s.Strategy, &s.Reason, &s.EntryPrice, &s.StopLoss, &s.TakeProfit, &s.PositionSize, &s.Status, &s.ExecutedOrderID, &s.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, &s)
	}
	return result, nil
}

func (r *SignalRepo) Update(s *SignalRecord) error {
	_, err := db.Exec(
		`UPDATE xt_signals SET status=?, executed_order_id=? WHERE id=?`,
		s.Status, s.ExecutedOrderID, s.ID,
	)
	return err
}

func (r *SignalRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_signals WHERE id=?", id)
	return err
}
