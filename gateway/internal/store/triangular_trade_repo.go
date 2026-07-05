package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// ── Triangular Trade Repository ──

type TriangularTradeRecord struct {
	ID          string  `json:"id"`
	Exchange    string  `json:"exchange"`
	CycleJSON   string  `json:"cycle_json"`
	LegsJSON    string  `json:"legs_json"`
	StartAsset  string  `json:"start_asset"`
	StartQty    float64 `json:"start_qty"`
	EndQty      float64 `json:"end_qty"`
	GrossProfit float64 `json:"gross_profit"`
	NetProfit   float64 `json:"net_profit"`
	TotalFees   float64 `json:"total_fees"`
	Status      string  `json:"status"`
	OpenedAt    int64   `json:"opened_at"`
	ClosedAt    int64   `json:"closed_at"`
}

type TriangularTradeRepo struct{ mu sync.RWMutex }

func NewTriangularTradeRepo() *TriangularTradeRepo { return &TriangularTradeRepo{} }

func (r *TriangularTradeRepo) Create(t *TriangularTradeRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	if t.ID == "" {
		t.ID = fmt.Sprintf("tri_%d", time.Now().UnixNano())
	}
	if t.OpenedAt == 0 {
		t.OpenedAt = time.Now().UnixMilli()
	}
	if t.CycleJSON == "" {
		t.CycleJSON = "[]"
	}
	if t.LegsJSON == "" {
		t.LegsJSON = "[]"
	}
	_, err := db.Exec(
		`INSERT INTO triangular_trades (id, exchange, cycle_json, legs_json, start_asset, start_qty, end_qty, gross_profit, net_profit, total_fees, status, opened_at, closed_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Exchange, t.CycleJSON, t.LegsJSON, t.StartAsset, t.StartQty, t.EndQty,
		t.GrossProfit, t.NetProfit, t.TotalFees, t.Status, t.OpenedAt, t.ClosedAt,
	)
	return err
}

func (r *TriangularTradeRepo) GetByID(id string) (*TriangularTradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	row := db.QueryRow(`SELECT id, exchange, cycle_json, legs_json, start_asset, start_qty, end_qty, gross_profit, net_profit, total_fees, status, opened_at, closed_at FROM triangular_trades WHERE id=?`, id)
	var t TriangularTradeRecord
	err := row.Scan(&t.ID, &t.Exchange, &t.CycleJSON, &t.LegsJSON, &t.StartAsset, &t.StartQty, &t.EndQty, &t.GrossProfit, &t.NetProfit, &t.TotalFees, &t.Status, &t.OpenedAt, &t.ClosedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TriangularTradeRepo) Update(t *TriangularTradeRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(
		`UPDATE triangular_trades SET exchange=?, cycle_json=?, legs_json=?, start_asset=?, start_qty=?, end_qty=?, gross_profit=?, net_profit=?, total_fees=?, status=?, opened_at=?, closed_at=? WHERE id=?`,
		t.Exchange, t.CycleJSON, t.LegsJSON, t.StartAsset, t.StartQty, t.EndQty,
		t.GrossProfit, t.NetProfit, t.TotalFees, t.Status, t.OpenedAt, t.ClosedAt, t.ID,
	)
	return err
}

func (r *TriangularTradeRepo) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec("DELETE FROM triangular_trades WHERE id=?", id)
	return err
}

func (r *TriangularTradeRepo) ListActive() ([]*TriangularTradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT id, exchange, cycle_json, legs_json, start_asset, start_qty, end_qty, gross_profit, net_profit, total_fees, status, opened_at, closed_at FROM triangular_trades WHERE status IN ('pending','executing') ORDER BY opened_at DESC`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTriangularTradeRows(rows)
}

func (r *TriangularTradeRepo) ListHistory(limit int) ([]*TriangularTradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT id, exchange, cycle_json, legs_json, start_asset, start_qty, end_qty, gross_profit, net_profit, total_fees, status, opened_at, closed_at FROM triangular_trades WHERE status IN ('completed','failed','dry_run') ORDER BY opened_at DESC`
	var args []any
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTriangularTradeRows(rows)
}

func scanTriangularTradeRows(rows *sql.Rows) ([]*TriangularTradeRecord, error) {
	var result []*TriangularTradeRecord
	for rows.Next() {
		var t TriangularTradeRecord
		if err := rows.Scan(&t.ID, &t.Exchange, &t.CycleJSON, &t.LegsJSON, &t.StartAsset, &t.StartQty, &t.EndQty, &t.GrossProfit, &t.NetProfit, &t.TotalFees, &t.Status, &t.OpenedAt, &t.ClosedAt); err != nil {
			return nil, err
		}
		result = append(result, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
