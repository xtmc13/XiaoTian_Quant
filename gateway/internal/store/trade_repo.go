package store

import (
	"fmt"
	"sync"
	"time"
)

// ── Trade Repository ──

type TradeRecord struct {
	ID          string  `json:"id"`
	OrderID     string  `json:"order_id"`
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Price       float64 `json:"price"`
	Quantity    float64 `json:"quantity"`
	Fee         float64 `json:"fee"`
	FeeCurrency string  `json:"fee_currency"`
	Exchange    string  `json:"exchange"`
	CreatedAt   int64   `json:"created_at"`
}

type TradeRepo struct {
	mu sync.RWMutex
}

func NewTradeRepo() *TradeRepo { return &TradeRepo{} }

func (r *TradeRepo) Create(t *TradeRecord) error {
	if t.ID == "" {
		t.ID = fmt.Sprintf("trade_%d", time.Now().UnixNano())
	}
	if t.CreatedAt == 0 {
		t.CreatedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(
		`INSERT INTO trades (id, order_id, symbol, side, price, quantity, fee, fee_currency, exchange, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.OrderID, t.Symbol, t.Side, t.Price, t.Quantity, t.Fee, t.FeeCurrency, t.Exchange, t.CreatedAt,
	)
	return err
}

func (r *TradeRepo) GetByID(id string) (*TradeRecord, error) {
	row := db.QueryRow(`SELECT id, order_id, symbol, side, price, quantity, fee, fee_currency, exchange, created_at FROM trades WHERE id=?`, id)
	var t TradeRecord
	err := row.Scan(&t.ID, &t.OrderID, &t.Symbol, &t.Side, &t.Price, &t.Quantity, &t.Fee, &t.FeeCurrency, &t.Exchange, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TradeRepo) List(filter map[string]any, limit int) ([]*TradeRecord, error) {
	query := "SELECT id, order_id, symbol, side, price, quantity, fee, fee_currency, exchange, created_at FROM trades"
	allowedCols := map[string]bool{
		"id": true, "order_id": true, "symbol": true, "side": true, "exchange": true, "created_at": true,
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

	var result []*TradeRecord
	for rows.Next() {
		var t TradeRecord
		if err := rows.Scan(&t.ID, &t.OrderID, &t.Symbol, &t.Side, &t.Price, &t.Quantity, &t.Fee, &t.FeeCurrency, &t.Exchange, &t.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, &t)
	}
	return result, nil
}

func (r *TradeRepo) Update(t *TradeRecord) error {
	_, err := db.Exec(
		`UPDATE trades SET fee=?, fee_currency=? WHERE id=?`,
		t.Fee, t.FeeCurrency, t.ID,
	)
	return err
}

func (r *TradeRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM trades WHERE id=?", id)
	return err
}
