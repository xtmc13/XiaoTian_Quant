package store

import (
	"fmt"
	"sync"
	"time"
)

// ── Trade Repository ──

type TradeRecord struct {
	ID          string  `json:"id"`
	UserID      int64   `json:"user_id"`
	OrderID     string  `json:"order_id"`
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Price       float64 `json:"price"`
	Quantity    float64 `json:"quantity"`
	Fee         float64 `json:"fee"`
	FeeCurrency string  `json:"fee_currency"`
	Exchange    string  `json:"exchange"`
	ExecPhase   string  `json:"exec_phase,omitempty"` // limit|market|recovered（A2.2 区分补单两部分）
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
		`INSERT INTO trades (id, user_id, order_id, symbol, side, price, quantity, fee, fee_currency, exchange, exec_phase, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.UserID, t.OrderID, t.Symbol, t.Side, t.Price, t.Quantity, t.Fee, t.FeeCurrency, t.Exchange, t.ExecPhase, t.CreatedAt,
	)
	return err
}

// Exists 判断指定 id 的成交是否已存在（成交恢复按交易所 trade id 去重用）。
func (r *TradeRepo) Exists(id string) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(1) FROM trades WHERE id=?`, id).Scan(&n)
	return n > 0
}

// CountBetween 返回 (startMs, endMs] 窗口内的成交笔数（权益曲线异常点
// 过滤用：窗口内有成交的大幅波动视为真实波动保留）。
func (r *TradeRepo) CountBetween(startMs, endMs int64) int {
	var n int
	_ = db.QueryRow(`SELECT COUNT(1) FROM trades WHERE created_at > ? AND created_at <= ?`, startMs, endMs).Scan(&n)
	return n
}

func (r *TradeRepo) GetByID(id string) (*TradeRecord, error) {
	row := db.QueryRow(`SELECT id, user_id, order_id, symbol, side, price, quantity, fee, fee_currency, exchange, COALESCE(exec_phase,''), created_at FROM trades WHERE id=?`, id)
	var t TradeRecord
	err := row.Scan(&t.ID, &t.UserID, &t.OrderID, &t.Symbol, &t.Side, &t.Price, &t.Quantity, &t.Fee, &t.FeeCurrency, &t.Exchange, &t.ExecPhase, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TradeRepo) List(filter map[string]any, limit int) ([]*TradeRecord, error) {
	query := "SELECT id, user_id, order_id, symbol, side, price, quantity, fee, fee_currency, exchange, COALESCE(exec_phase,''), created_at FROM trades"
	allowedCols := map[string]bool{
		"id": true, "user_id": true, "order_id": true, "symbol": true, "side": true, "exchange": true, "created_at": true,
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
		if err := rows.Scan(&t.ID, &t.UserID, &t.OrderID, &t.Symbol, &t.Side, &t.Price, &t.Quantity, &t.Fee, &t.FeeCurrency, &t.Exchange, &t.ExecPhase, &t.CreatedAt); err != nil {
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

// ListAscBefore 按时间升序列出 endMs（含）之前的成交，可按 exchange/symbol 过滤。
// 供回报 PnL 对账计算 FIFO 成本基础：窗口内平仓的盈亏依赖窗口前建仓的成交。
// 专用查询（非通用列表），limit<=0 时使用调用方兜底上限。
func (r *TradeRepo) ListAscBefore(exchange, symbol string, endMs int64, limit int) ([]*TradeRecord, error) {
	if limit <= 0 {
		limit = 20000
	}
	query := `SELECT id, user_id, order_id, symbol, side, price, quantity, fee, fee_currency, exchange, COALESCE(exec_phase,''), created_at FROM trades WHERE created_at<=?`
	var args []any
	args = append(args, endMs)
	if exchange != "" {
		query += ` AND exchange=?`
		args = append(args, exchange)
	}
	if symbol != "" {
		query += ` AND symbol=?`
		args = append(args, symbol)
	}
	query += ` ORDER BY created_at ASC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*TradeRecord
	for rows.Next() {
		var t TradeRecord
		if err := rows.Scan(&t.ID, &t.UserID, &t.OrderID, &t.Symbol, &t.Side, &t.Price, &t.Quantity, &t.Fee, &t.FeeCurrency, &t.Exchange, &t.ExecPhase, &t.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, &t)
	}
	return result, nil
}
