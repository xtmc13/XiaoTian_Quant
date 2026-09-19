package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// ── DCA Bot Repository ──
//
// DCA 定投机器人（A1.2，对标 QuantDinger DCA bot）的 typed CRUD，直接操作
// 包级 db，风格对齐 GridRepo。dca_bots 为主表（定投参数 + 持仓/累计投入等
// 运行时状态列），dca_bot_orders 记录每笔买入/卖出成交。
// 表结构见 migrations/sql/0006_dca_bots.sql。

// DCABotRecord is the SQLite-backed representation of a DCA bot.
type DCABotRecord struct {
	ID              string  `json:"id"`
	UserID          int64   `json:"user_id"`
	Name            string  `json:"name"`
	Symbol          string  `json:"symbol"`
	Exchange        string  `json:"exchange"`
	QuoteAmount     float64 `json:"quote_amount"`
	IntervalMinutes int     `json:"interval_minutes"`
	MaxOrders       int     `json:"max_orders"`
	PeriodBudget    float64 `json:"period_budget"`
	TakeProfitPct   float64 `json:"take_profit_pct"`
	StopLossPct     float64 `json:"stop_loss_pct"`
	TrailingEnabled bool    `json:"trailing_enabled"`
	Status          string  `json:"status"`
	FilledOrders    int     `json:"filled_orders"`
	TotalInvested   float64 `json:"total_invested"`
	BaseQty         float64 `json:"base_qty"`
	AvgPrice        float64 `json:"avg_price"`
	RealizedPnL     float64 `json:"realized_pnl"`
	LastBuyAt       int64   `json:"last_buy_at"`
	HighestPrice    float64 `json:"highest_price"`
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`
	StartedAt       int64   `json:"started_at"`
	StoppedAt       int64   `json:"stopped_at"`
}

// DCAOrderRecord is a single DCA fill persisted in dca_bot_orders.
type DCAOrderRecord struct {
	ID       int64   `json:"id"`
	BotID    string  `json:"bot_id"`
	Side     string  `json:"side"`
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
	QuoteQty float64 `json:"quote_qty"`
	Reason   string  `json:"reason"`
	Ts       int64   `json:"ts"`
}

// DCARepo provides typed CRUD for DCA bots and their orders.
type DCARepo struct {
	mu sync.RWMutex
}

func NewDCARepo() *DCARepo { return &DCARepo{} }

const dcaBotColumns = `id, user_id, name, symbol, exchange, quote_amount, interval_minutes,
	max_orders, period_budget, take_profit_pct, stop_loss_pct, trailing_enabled, status,
	filled_orders, total_invested, base_qty, avg_price, realized_pnl, last_buy_at,
	highest_price, created_at, updated_at, started_at, stopped_at`

// Create inserts a new DCA bot. It assigns ID/timestamps and status defaults if missing.
func (r *DCARepo) Create(b *DCABotRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	if b.ID == "" {
		b.ID = generateShortID()
	}
	now := time.Now().UnixMilli()
	if b.CreatedAt == 0 {
		b.CreatedAt = now
	}
	if b.UpdatedAt == 0 {
		b.UpdatedAt = now
	}
	if b.Status == "" {
		b.Status = "stopped"
	}
	if b.Exchange == "" {
		b.Exchange = "paper"
	}
	_, err := db.Exec(`INSERT INTO dca_bots (`+dcaBotColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.UserID, b.Name, b.Symbol, b.Exchange, b.QuoteAmount, b.IntervalMinutes,
		b.MaxOrders, b.PeriodBudget, b.TakeProfitPct, b.StopLossPct, b.TrailingEnabled,
		b.Status, b.FilledOrders, b.TotalInvested, b.BaseQty, b.AvgPrice, b.RealizedPnL,
		b.LastBuyAt, b.HighestPrice, b.CreatedAt, b.UpdatedAt, b.StartedAt, b.StoppedAt,
	)
	return err
}

// GetByID returns a DCA bot by ID (nil, nil when not found).
func (r *DCARepo) GetByID(id string) (*DCABotRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	row := db.QueryRow(`SELECT `+dcaBotColumns+` FROM dca_bots WHERE id=?`, id)
	return scanDCABot(row)
}

// List returns DCA bots matching the filter. Use limit=0 for unlimited.
func (r *DCARepo) List(filter map[string]any, limit int) ([]*DCABotRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT ` + dcaBotColumns + ` FROM dca_bots`
	allowedCols := map[string]bool{
		"id": true, "user_id": true, "status": true, "symbol": true, "exchange": true,
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

	var result []*DCABotRecord
	for rows.Next() {
		b, err := scanDCABot(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// Update modifies an existing DCA bot (full-row write; bumps updated_at).
func (r *DCARepo) Update(b *DCABotRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	b.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(`UPDATE dca_bots SET user_id=?, name=?, symbol=?, exchange=?,
		quote_amount=?, interval_minutes=?, max_orders=?, period_budget=?, take_profit_pct=?,
		stop_loss_pct=?, trailing_enabled=?, status=?, filled_orders=?, total_invested=?,
		base_qty=?, avg_price=?, realized_pnl=?, last_buy_at=?, highest_price=?,
		created_at=?, updated_at=?, started_at=?, stopped_at=? WHERE id=?`,
		b.UserID, b.Name, b.Symbol, b.Exchange, b.QuoteAmount, b.IntervalMinutes,
		b.MaxOrders, b.PeriodBudget, b.TakeProfitPct, b.StopLossPct, b.TrailingEnabled,
		b.Status, b.FilledOrders, b.TotalInvested, b.BaseQty, b.AvgPrice, b.RealizedPnL,
		b.LastBuyAt, b.HighestPrice, b.CreatedAt, b.UpdatedAt, b.StartedAt, b.StoppedAt, b.ID,
	)
	return err
}

// UpdateState persists the live engine state in a single-row UPDATE (hot path,
// called after every tick that produced a fill).
func (r *DCARepo) UpdateState(id string, filledOrders int, totalInvested, baseQty, avgPrice, realizedPnL float64, lastBuyAt int64, highestPrice float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE dca_bots SET filled_orders=?, total_invested=?, base_qty=?,
		avg_price=?, realized_pnl=?, last_buy_at=?, highest_price=?, updated_at=? WHERE id=?`,
		filledOrders, totalInvested, baseQty, avgPrice, realizedPnL, lastBuyAt, highestPrice,
		time.Now().UnixMilli(), id,
	)
	return err
}

// UpdateStatus sets the bot status and stamps started_at (running) or
// stopped_at (stopped/finished) accordingly.
func (r *DCARepo) UpdateStatus(id string, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	now := time.Now().UnixMilli()
	query := `UPDATE dca_bots SET status=?, updated_at=?`
	args := []any{status, now}
	switch status {
	case "running":
		query += `, started_at=?`
		args = append(args, now)
	case "stopped", "finished":
		query += `, stopped_at=?`
		args = append(args, now)
	}
	query += ` WHERE id=?`
	args = append(args, id)
	_, err := db.Exec(query, args...)
	return err
}

// Delete removes a DCA bot. Its orders are kept for history.
func (r *DCARepo) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec("DELETE FROM dca_bots WHERE id=?", id)
	return err
}

// DeleteForUser 带属主条件删除（越权防护双保险）：非属主删不掉，按 not found 上报。
func (r *DCARepo) DeleteForUser(id string, userID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	res, err := db.Exec("DELETE FROM dca_bots WHERE id=? AND user_id=?", id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}

// InsertOrder records one DCA fill (buy or sell).
func (r *DCARepo) InsertOrder(botID, side string, price, qty, quoteQty float64, reason string, ts int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`INSERT INTO dca_bot_orders
		(bot_id, side, price, quantity, quote_qty, reason, ts)
		VALUES (?,?,?,?,?,?,?)`,
		botID, side, price, qty, quoteQty, reason, ts,
	)
	return err
}

// GetOrders returns the bot's fills, newest first. Use limit=0 for unlimited.
func (r *DCARepo) GetOrders(botID string, limit int) ([]*DCAOrderRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT id, bot_id, side, price, quantity, quote_qty, reason, ts
		FROM dca_bot_orders WHERE bot_id=? ORDER BY ts DESC, id DESC`
	args := []any{botID}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*DCAOrderRecord
	for rows.Next() {
		var o DCAOrderRecord
		if err := rows.Scan(&o.ID, &o.BotID, &o.Side, &o.Price, &o.Quantity,
			&o.QuoteQty, &o.Reason, &o.Ts); err != nil {
			return nil, err
		}
		result = append(result, &o)
	}
	return result, rows.Err()
}

// scanDCABot scans a single row into a DCABotRecord.
func scanDCABot(scanner interface {
	Scan(dest ...any) error
}) (*DCABotRecord, error) {
	var b DCABotRecord
	err := scanner.Scan(
		&b.ID, &b.UserID, &b.Name, &b.Symbol, &b.Exchange, &b.QuoteAmount, &b.IntervalMinutes,
		&b.MaxOrders, &b.PeriodBudget, &b.TakeProfitPct, &b.StopLossPct, &b.TrailingEnabled,
		&b.Status, &b.FilledOrders, &b.TotalInvested, &b.BaseQty, &b.AvgPrice, &b.RealizedPnL,
		&b.LastBuyAt, &b.HighestPrice, &b.CreatedAt, &b.UpdatedAt, &b.StartedAt, &b.StoppedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}
