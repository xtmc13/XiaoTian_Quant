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
	// xt_orders.created_at/updated_at 为历史 REAL 列，驱动可能返回 float64，
	// 大时间戳会以科学计数法走字符串解析而失败——先收 float64 再显式转换。
	var createdF, updatedF float64
	err := row.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
		&o.UserID, &o.ClientOID, &o.AvgFillPrice, &createdF, &updatedF,
		&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition)
	if err != nil {
		return nil, err
	}
	o.CreatedAt = int64(createdF)
	o.UpdatedAt = int64(updatedF)
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
		// xt_orders.created_at/updated_at 为历史 REAL 列，驱动可能返回 float64，
		// 大时间戳会以科学计数法走字符串解析而失败——先收 float64 再显式转换。
		var createdF, updatedF float64
		if err := rows.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
			&o.UserID, &o.ClientOID, &o.AvgFillPrice, &createdF, &updatedF,
			&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition); err != nil {
			return nil, err
		}
		o.CreatedAt = int64(createdF)
		o.UpdatedAt = int64(updatedF)
		result = append(result, &o)
	}
	return result, nil
}

// ListActiveByClientOIDPrefix 返回 client_oid 带指定前缀且仍在活动状态
// （NEW/PENDING/PARTIALLY_FILLED）的订单，按创建时间升序。
// ltm 重启恢复扫描用：父单前缀 "ltm-"，市价补单前缀 "ltm-mkt:<parentID>"。
func (r *OrderRepo) ListActiveByClientOIDPrefix(prefix string) ([]*OrderRecord, error) {
	rows, err := db.Query(`SELECT id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position
		FROM xt_orders WHERE client_oid LIKE ? AND status IN ('NEW','PENDING','PARTIALLY_FILLED') ORDER BY created_at ASC`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*OrderRecord
	for rows.Next() {
		var o OrderRecord
		// xt_orders.created_at/updated_at 为历史 REAL 列，驱动可能返回 float64，
		// 大时间戳会以科学计数法走字符串解析而失败——先收 float64 再显式转换。
		var createdF, updatedF float64
		if err := rows.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
			&o.UserID, &o.ClientOID, &o.AvgFillPrice, &createdF, &updatedF,
			&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition); err != nil {
			return nil, err
		}
		o.CreatedAt = int64(createdF)
		o.UpdatedAt = int64(updatedF)
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

// Upsert 按主键 id 存在即更新、不存在即插入。
// 修正原 "Update 失败再 Create" 的伪 upsert：UPDATE 命中 0 行并不报错，
// 导致首写永远落不了库（OMS 订单只有经 handler/镜像绕行才有 DB 行）。
func (r *OrderRepo) Upsert(o *OrderRecord) error {
	if o.ID == "" {
		o.ID = fmt.Sprintf("ord-%d", time.Now().UnixMilli())
	}
	if o.CreatedAt == 0 {
		o.CreatedAt = time.Now().UnixMilli()
	}
	o.UpdatedAt = time.Now().UnixMilli()
	if o.Status == "" {
		o.Status = "NEW"
	}
	if o.Exchange == "" {
		o.Exchange = "BINANCE"
	}
	_, err := db.Exec(
		`INSERT INTO xt_orders (id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET symbol=excluded.symbol, side=excluded.side, order_type=excluded.order_type,
		 price=excluded.price, stop_price=excluded.stop_price, quantity=excluded.quantity, filled=excluded.filled,
		 status=excluded.status, exchange=excluded.exchange, user_id=excluded.user_id, client_oid=excluded.client_oid,
		 avg_fill_price=excluded.avg_fill_price, updated_at=excluded.updated_at, market_type=excluded.market_type,
		 position_side=excluded.position_side, leverage=excluded.leverage, margin_mode=excluded.margin_mode,
		 tp_price=excluded.tp_price, sl_price=excluded.sl_price, close_position=excluded.close_position`,
		o.ID, o.Symbol, o.Side, o.OrderType, o.Price, o.StopPrice, o.Quantity, o.Filled, o.Status, o.Exchange,
		o.UserID, o.ClientOID, o.AvgFillPrice, o.CreatedAt, o.UpdatedAt,
		o.MarketType, o.PositionSide, o.Leverage, o.MarginMode, o.TPPrice, o.SLPrice, o.ClosePosition,
	)
	return err
}

func (r *OrderRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_orders WHERE id=?", id)
	return err
}

// ListRecent 返回 updated_at >= sinceMs 的订单（新→旧），偏差监控等扫描用。
func (r *OrderRepo) ListRecent(sinceMs int64, limit int) ([]*OrderRecord, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := db.Query(`SELECT id, symbol, side, order_type, price, stop_price, quantity, filled, status, exchange, user_id, client_oid, avg_fill_price, created_at, updated_at, market_type, position_side, leverage, margin_mode, tp_price, sl_price, close_position
		FROM xt_orders WHERE updated_at >= ? ORDER BY updated_at DESC LIMIT ?`, sinceMs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*OrderRecord
	for rows.Next() {
		var o OrderRecord
		// xt_orders.created_at/updated_at 为历史 REAL 列，驱动可能返回 float64，
		// 大时间戳会以科学计数法走字符串解析而失败——先收 float64 再显式转换。
		var createdF, updatedF float64
		if err := rows.Scan(&o.ID, &o.Symbol, &o.Side, &o.OrderType, &o.Price, &o.StopPrice, &o.Quantity, &o.Filled, &o.Status, &o.Exchange,
			&o.UserID, &o.ClientOID, &o.AvgFillPrice, &createdF, &updatedF,
			&o.MarketType, &o.PositionSide, &o.Leverage, &o.MarginMode, &o.TPPrice, &o.SLPrice, &o.ClosePosition); err != nil {
			return nil, err
		}
		o.CreatedAt = int64(createdF)
		o.UpdatedAt = int64(updatedF)
		result = append(result, &o)
	}
	return result, nil
}
