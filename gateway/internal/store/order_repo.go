package store

import (
	"fmt"
	"sync"
	"time"
)

// ── Order Repository ──

type OrderRecord struct {
	ID           string  `json:"id"`
	Symbol       string  `json:"symbol"`
	Side         string  `json:"side"`
	OrderType    string  `json:"order_type"`
	Price        float64 `json:"price"`
	StopPrice    float64 `json:"stop_price"`
	Quantity     float64 `json:"quantity"`
	Filled       float64 `json:"filled"`
	Status       string  `json:"status"`
	Exchange     string  `json:"exchange"`
	UserID       uint64  `json:"user_id"`
	ClientOID    string  `json:"client_oid"`
	AvgFillPrice float64 `json:"avg_fill_price"`
	CreatedAt    int64   `json:"created_at"`
	UpdatedAt    int64   `json:"updated_at"`

	// Contract fields
	MarketType    string  `json:"market_type,omitempty"`
	PositionSide  string  `json:"position_side,omitempty"`
	Leverage      float64 `json:"leverage,omitempty"`
	MarginMode    string  `json:"margin_mode,omitempty"`
	TPPrice       float64 `json:"tp_price,omitempty"`
	SLPrice       float64 `json:"sl_price,omitempty"`
	ClosePosition bool    `json:"close_position,omitempty"`
}

type OrderRepo struct {
	mu sync.RWMutex
}

func NewOrderRepo() *OrderRepo { return &OrderRepo{} }

func (r *OrderRepo) Create(o *OrderRecord) error {
	if o.ID == "" {
		o.ID = fmt.Sprintf("ord-%d", time.Now().UnixMilli())
	}
	if o.CreatedAt == 0 {
		o.CreatedAt = time.Now().UnixMilli()
	}
	if o.UpdatedAt == 0 {
		o.UpdatedAt = o.CreatedAt
	}
	if o.Status == "" {
		o.Status = "NEW"
	}
	if o.Exchange == "" {
		o.Exchange = "BINANCE"
	}
	_, err := db.Exec(
		`INSERT INTO xt_orders (id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.Symbol, o.Side, o.OrderType, o.Price, o.StopPrice, o.Quantity, o.Filled, o.Status, o.Exchange,
		o.UserID, o.ClientOID, o.AvgFillPrice, o.CreatedAt, o.UpdatedAt,
		o.MarketType, o.PositionSide, o.Leverage, o.MarginMode, o.TPPrice, o.SLPrice, o.ClosePosition,
	)
	return err
}

func (r *OrderRepo) GetByID(id string) (*OrderRecord, error) {
	row := db.QueryRow(`SELECT id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position FROM xt_orders WHERE id=?`, id)
	var o OrderRecord
	err := row.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
		&o.UserID, &o.ClientOID, &o.AvgFillPrice, &o.CreatedAt, &o.UpdatedAt,
		&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *OrderRepo) List(filter map[string]any, limit int) ([]*OrderRecord, error) {
	query := "SELECT id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position FROM xt_orders"
	allowedCols := map[string]bool{
		"id": true, "symbol": true, "side": true, "order_type": true, "status": true,
		"exchange": true, "user_id": true, "client_oid": true, "market_type": true,
		"position_side": true, "created_at": true, "updated_at": true,
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
	var result []*OrderRecord
	for rows.Next() {
		var o OrderRecord
		if err := rows.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
			&o.UserID, &o.ClientOID, &o.AvgFillPrice, &o.CreatedAt, &o.UpdatedAt,
			&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition); err != nil {
			return nil, err
		}
		result = append(result, &o)
	}
	return result, nil
}

func (r *OrderRepo) Update(o *OrderRecord) error {
	o.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(
		`UPDATE xt_orders SET symbol=?, side=?, order_type=?, price=?, stop_price=?, quantity=?, filled=?, status=?, exchange=?, user_id=?, client_oid=?, avg_fill_price=?, market_type=?, position_side=?, leverage=?, margin_mode=?, tp_price=?, sl_price=?, close_position=? WHERE id=?`,
		o.Symbol, o.Side, o.OrderType, o.Price, o.StopPrice, o.Quantity, o.Filled, o.Status, o.Exchange, o.UserID,
		o.ClientOID, o.AvgFillPrice, o.MarketType, o.PositionSide, o.Leverage, o.MarginMode, o.TPPrice, o.SLPrice, o.ClosePosition, o.ID,
	)
	return err
}

func (r *OrderRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_orders WHERE id=?", id)
	return err
}
