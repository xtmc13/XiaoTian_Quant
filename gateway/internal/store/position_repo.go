package store

import (
	"fmt"
	"sync"
	"time"
)

// ── Position Repository ──

type PositionRecord struct {
	ID            string  `json:"id"`
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Quantity      float64 `json:"quantity"`
	AvgEntryPrice float64 `json:"avg_entry_price"`
	CurrentPrice  float64 `json:"current_price"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	RealizedPnL   float64 `json:"realized_pnl"`
	CostBasis     float64 `json:"cost_basis"`
	Exchange      string  `json:"exchange"`
	Status        string  `json:"status"`
	OpenedAt      int64   `json:"opened_at"`
	ClosedAt      int64   `json:"closed_at"`
	UpdatedAt     int64   `json:"updated_at"`
}

type PositionRepo struct{ mu sync.RWMutex }

func NewPositionRepo() *PositionRepo { return &PositionRepo{} }

func (r *PositionRepo) Create(p *PositionRecord) error {
	now := time.Now().UnixMilli()
	if p.ID == "" {
		p.ID = fmt.Sprintf("pos_%d", now)
	}
	if p.Status == "" {
		p.Status = "OPEN"
	}
	p.OpenedAt = now
	p.UpdatedAt = now
	p.CostBasis = p.Quantity * p.AvgEntryPrice
	_, err := db.Exec(
		`INSERT INTO positions (id, symbol, side, quantity, avg_entry_price, current_price, unrealized_pnl, realized_pnl, cost_basis, exchange, status, opened_at, closed_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Symbol, p.Side, p.Quantity, p.AvgEntryPrice, p.CurrentPrice, p.UnrealizedPnL, p.RealizedPnL, p.CostBasis, p.Exchange, p.Status, p.OpenedAt, p.ClosedAt, p.UpdatedAt,
	)
	return err
}

func (r *PositionRepo) GetByID(id string) (*PositionRecord, error) {
	row := db.QueryRow(`SELECT id, symbol, side, quantity, avg_entry_price, current_price, unrealized_pnl, realized_pnl, cost_basis, exchange, status, opened_at, closed_at, updated_at FROM positions WHERE id=?`, id)
	var p PositionRecord
	err := row.Scan(&p.ID, &p.Symbol, &p.Side, &p.Quantity, &p.AvgEntryPrice, &p.CurrentPrice, &p.UnrealizedPnL, &p.RealizedPnL, &p.CostBasis, &p.Exchange, &p.Status, &p.OpenedAt, &p.ClosedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PositionRepo) List(filter map[string]any, limit int) ([]*PositionRecord, error) {
	query := "SELECT id, symbol, side, quantity, avg_entry_price, current_price, unrealized_pnl, realized_pnl, cost_basis, exchange, status, opened_at, closed_at, updated_at FROM positions"
	allowedCols := map[string]bool{
		"id": true, "symbol": true, "side": true, "exchange": true, "status": true,
		"opened_at": true, "closed_at": true, "updated_at": true,
	}
	args, where := buildFilter(filter, allowedCols)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY updated_at DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*PositionRecord
	for rows.Next() {
		var p PositionRecord
		if err := rows.Scan(&p.ID, &p.Symbol, &p.Side, &p.Quantity, &p.AvgEntryPrice, &p.CurrentPrice, &p.UnrealizedPnL, &p.RealizedPnL, &p.CostBasis, &p.Exchange, &p.Status, &p.OpenedAt, &p.ClosedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, &p)
	}
	return result, nil
}

func (r *PositionRepo) Update(p *PositionRecord) error {
	p.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(
		`UPDATE positions SET quantity=?, avg_entry_price=?, current_price=?, unrealized_pnl=?, realized_pnl=?, cost_basis=?, status=?, closed_at=?, updated_at=? WHERE id=?`,
		p.Quantity, p.AvgEntryPrice, p.CurrentPrice, p.UnrealizedPnL, p.RealizedPnL, p.CostBasis, p.Status, p.ClosedAt, p.UpdatedAt, p.ID,
	)
	return err
}

func (r *PositionRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM positions WHERE id=?", id)
	return err
}
