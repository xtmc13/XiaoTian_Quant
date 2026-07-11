package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// ── Arbitrage Trade Repository ──

type ArbitrageTradeRecord struct {
	ID            string  `json:"id"`
	Symbol        string  `json:"symbol"`
	BuyExchange   string  `json:"buy_exchange"`
	SellExchange  string  `json:"sell_exchange"`
	BuyPrice      float64 `json:"buy_price"`
	SellPrice     float64 `json:"sell_price"`
	Quantity      float64 `json:"quantity"`
	BuyOrderID    string  `json:"buy_order_id"`
	SellOrderID   string  `json:"sell_order_id"`
	BuyFilledQty  float64 `json:"buy_filled_qty"`
	BuyAvgPrice   float64 `json:"buy_avg_price"`
	BuyFee        float64 `json:"buy_fee"`
	SellFilledQty float64 `json:"sell_filled_qty"`
	SellAvgPrice  float64 `json:"sell_avg_price"`
	SellFee       float64 `json:"sell_fee"`
	GrossProfit   float64 `json:"gross_profit"`
	NetProfit     float64 `json:"net_profit"`
	Fees          float64 `json:"fees"`
	Status        string  `json:"status"`
	OpenedAt      int64   `json:"opened_at"`
	ClosedAt      int64   `json:"closed_at"`
}

type ArbitrageTradeRepo struct{ mu sync.RWMutex }

func NewArbitrageTradeRepo() *ArbitrageTradeRepo { return &ArbitrageTradeRepo{} }

func (r *ArbitrageTradeRepo) Create(t *ArbitrageTradeRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	if t.ID == "" {
		t.ID = fmt.Sprintf("arb_%d", time.Now().UnixNano())
	}
	if t.OpenedAt == 0 {
		t.OpenedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(
		`INSERT INTO arbitrage_trades (id, symbol, buy_exchange, sell_exchange, buy_price, sell_price, quantity, buy_order_id, sell_order_id, buy_filled_qty, buy_avg_price, buy_fee, sell_filled_qty, sell_avg_price, sell_fee, gross_profit, net_profit, fees, status, opened_at, closed_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Symbol, t.BuyExchange, t.SellExchange, t.BuyPrice, t.SellPrice, t.Quantity,
		t.BuyOrderID, t.SellOrderID, t.BuyFilledQty, t.BuyAvgPrice, t.BuyFee, t.SellFilledQty, t.SellAvgPrice, t.SellFee,
		t.GrossProfit, t.NetProfit, t.Fees, t.Status, t.OpenedAt, t.ClosedAt,
	)
	return err
}

func (r *ArbitrageTradeRepo) GetByID(id string) (*ArbitrageTradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	row := db.QueryRow(`SELECT id, symbol, buy_exchange, sell_exchange, buy_price, sell_price, quantity, buy_order_id, sell_order_id, buy_filled_qty, buy_avg_price, buy_fee, sell_filled_qty, sell_avg_price, sell_fee, gross_profit, net_profit, fees, status, opened_at, closed_at FROM arbitrage_trades WHERE id=?`, id)
	var t ArbitrageTradeRecord
	err := row.Scan(&t.ID, &t.Symbol, &t.BuyExchange, &t.SellExchange, &t.BuyPrice, &t.SellPrice, &t.Quantity, &t.BuyOrderID, &t.SellOrderID, &t.BuyFilledQty, &t.BuyAvgPrice, &t.BuyFee, &t.SellFilledQty, &t.SellAvgPrice, &t.SellFee, &t.GrossProfit, &t.NetProfit, &t.Fees, &t.Status, &t.OpenedAt, &t.ClosedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *ArbitrageTradeRepo) Update(t *ArbitrageTradeRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(
		`UPDATE arbitrage_trades SET symbol=?, buy_exchange=?, sell_exchange=?, buy_price=?, sell_price=?, quantity=?, buy_order_id=?, sell_order_id=?, buy_filled_qty=?, buy_avg_price=?, buy_fee=?, sell_filled_qty=?, sell_avg_price=?, sell_fee=?, gross_profit=?, net_profit=?, fees=?, status=?, opened_at=?, closed_at=? WHERE id=?`,
		t.Symbol, t.BuyExchange, t.SellExchange, t.BuyPrice, t.SellPrice, t.Quantity,
		t.BuyOrderID, t.SellOrderID, t.BuyFilledQty, t.BuyAvgPrice, t.BuyFee, t.SellFilledQty, t.SellAvgPrice, t.SellFee,
		t.GrossProfit, t.NetProfit, t.Fees, t.Status, t.OpenedAt, t.ClosedAt, t.ID,
	)
	return err
}

func (r *ArbitrageTradeRepo) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec("DELETE FROM arbitrage_trades WHERE id=?", id)
	return err
}

func (r *ArbitrageTradeRepo) ListActive() ([]*ArbitrageTradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT id, symbol, buy_exchange, sell_exchange, buy_price, sell_price, quantity, buy_order_id, sell_order_id, buy_filled_qty, buy_avg_price, buy_fee, sell_filled_qty, sell_avg_price, sell_fee, gross_profit, net_profit, fees, status, opened_at, closed_at FROM arbitrage_trades WHERE status IN ('pending','open_buy','open','open_sell') ORDER BY opened_at DESC`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArbitrageTradeRows(rows)
}

func (r *ArbitrageTradeRepo) ListHistory(limit int) ([]*ArbitrageTradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT id, symbol, buy_exchange, sell_exchange, buy_price, sell_price, quantity, buy_order_id, sell_order_id, buy_filled_qty, buy_avg_price, buy_fee, sell_filled_qty, sell_avg_price, sell_fee, gross_profit, net_profit, fees, status, opened_at, closed_at FROM arbitrage_trades WHERE status IN ('completed','failed','dry_run') ORDER BY opened_at DESC`
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
	return scanArbitrageTradeRows(rows)
}

func scanArbitrageTradeRows(rows *sql.Rows) ([]*ArbitrageTradeRecord, error) {
	var result []*ArbitrageTradeRecord
	for rows.Next() {
		var t ArbitrageTradeRecord
		if err := rows.Scan(&t.ID, &t.Symbol, &t.BuyExchange, &t.SellExchange, &t.BuyPrice, &t.SellPrice, &t.Quantity, &t.BuyOrderID, &t.SellOrderID, &t.BuyFilledQty, &t.BuyAvgPrice, &t.BuyFee, &t.SellFilledQty, &t.SellAvgPrice, &t.SellFee, &t.GrossProfit, &t.NetProfit, &t.Fees, &t.Status, &t.OpenedAt, &t.ClosedAt); err != nil {
			return nil, err
		}
		result = append(result, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
