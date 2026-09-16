package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// ── Grid Bot Repository ──
//
// 真实网格交易机器人（等差现货网格）的 typed CRUD，直接操作包级 db，
// 风格对齐 StrategyConfigRepo。grid_bots 为主表（含 state_json 引擎状态），
// grid_bot_trades 记录每格成交，grid_bot_snapshots 记录周期权益快照。
// 表结构见 migrations/sql/0002_grid_bot.sql。

// GridBotRecord is the SQLite-backed representation of a grid bot.
type GridBotRecord struct {
	ID            string  `json:"id"`
	UserID        int64   `json:"user_id"`
	Name          string  `json:"name"`
	Symbol        string  `json:"symbol"`
	LowerPrice    float64 `json:"lower_price"`
	UpperPrice    float64 `json:"upper_price"`
	GridCount     int     `json:"grid_count"`
	Investment    float64 `json:"investment"`
	Status        string  `json:"status"`
	Exchange      string  `json:"exchange"`
	FeeRate       float64 `json:"fee_rate"`
	RealizedPnL   float64 `json:"realized_pnl"`
	TotalTrades   int     `json:"total_trades"`
	BaseQty       float64 `json:"base_qty"`
	QuoteBalance  float64 `json:"quote_balance"`
	StateJSON     string  `json:"state_json"`
	InitialEquity float64 `json:"initial_equity"`
	CreatedAt     int64   `json:"created_at"`
	UpdatedAt     int64   `json:"updated_at"`
	StartedAt     int64   `json:"started_at"`
	StoppedAt     int64   `json:"stopped_at"`
}

// GridTradeRecord is a single grid fill persisted in grid_bot_trades.
type GridTradeRecord struct {
	ID       int64   `json:"id"`
	BotID    string  `json:"bot_id"`
	Level    int     `json:"level_index"`
	Side     string  `json:"side"`
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
	QuoteQty float64 `json:"quote_qty"`
	Fee      float64 `json:"fee"`
	PnL      float64 `json:"pnl"`
	Ts       int64   `json:"ts"`
}

// GridSnapshotRecord is a periodic equity snapshot in grid_bot_snapshots.
type GridSnapshotRecord struct {
	ID          int64   `json:"id"`
	BotID       string  `json:"bot_id"`
	Equity      float64 `json:"equity"`
	Price       float64 `json:"price"`
	RealizedPnL float64 `json:"realized_pnl"`
	OpenOrders  int     `json:"open_orders"`
	Ts          int64   `json:"ts"`
}

// GridRepo provides typed CRUD for grid bots and their trades/snapshots.
type GridRepo struct {
	mu sync.RWMutex
}

func NewGridRepo() *GridRepo { return &GridRepo{} }

const gridBotColumns = `id, user_id, name, symbol, lower_price, upper_price, grid_count,
	investment, status, exchange, fee_rate, realized_pnl, total_trades, base_qty,
	quote_balance, state_json, initial_equity, created_at, updated_at, started_at, stopped_at`

// Create inserts a new grid bot. It assigns ID/timestamps and status defaults if missing.
func (r *GridRepo) Create(b *GridBotRecord) error {
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
	if b.FeeRate == 0 {
		b.FeeRate = 0.001
	}
	if b.StateJSON == "" {
		b.StateJSON = "{}"
	}
	_, err := db.Exec(`INSERT INTO grid_bots (`+gridBotColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.UserID, b.Name, b.Symbol, b.LowerPrice, b.UpperPrice, b.GridCount,
		b.Investment, b.Status, b.Exchange, b.FeeRate, b.RealizedPnL, b.TotalTrades,
		b.BaseQty, b.QuoteBalance, b.StateJSON, b.InitialEquity, b.CreatedAt,
		b.UpdatedAt, b.StartedAt, b.StoppedAt,
	)
	return err
}

// GetByID returns a grid bot by ID (nil, nil when not found).
func (r *GridRepo) GetByID(id string) (*GridBotRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	row := db.QueryRow(`SELECT `+gridBotColumns+` FROM grid_bots WHERE id=?`, id)
	return scanGridBot(row)
}

// List returns grid bots matching the filter. Use limit=0 for unlimited.
func (r *GridRepo) List(filter map[string]any, limit int) ([]*GridBotRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT ` + gridBotColumns + ` FROM grid_bots`
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

	var result []*GridBotRecord
	for rows.Next() {
		b, err := scanGridBot(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// Update modifies an existing grid bot (full-row write; bumps updated_at).
func (r *GridRepo) Update(b *GridBotRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	b.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(`UPDATE grid_bots SET user_id=?, name=?, symbol=?, lower_price=?,
		upper_price=?, grid_count=?, investment=?, status=?, exchange=?, fee_rate=?,
		realized_pnl=?, total_trades=?, base_qty=?, quote_balance=?, state_json=?,
		initial_equity=?, created_at=?, updated_at=?, started_at=?, stopped_at=? WHERE id=?`,
		b.UserID, b.Name, b.Symbol, b.LowerPrice, b.UpperPrice, b.GridCount,
		b.Investment, b.Status, b.Exchange, b.FeeRate, b.RealizedPnL, b.TotalTrades,
		b.BaseQty, b.QuoteBalance, b.StateJSON, b.InitialEquity, b.CreatedAt,
		b.UpdatedAt, b.StartedAt, b.StoppedAt, b.ID,
	)
	return err
}

// Delete removes a grid bot. Its trades and snapshots are kept for history.
func (r *GridRepo) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec("DELETE FROM grid_bots WHERE id=?", id)
	return err
}

// UpdateState persists the live engine state in a single-row UPDATE (hot path,
// called after every tick that produced fills and once on stop).
func (r *GridRepo) UpdateState(id string, stateJSON string, baseQty, quoteBalance, realizedPnL float64, totalTrades int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE grid_bots SET state_json=?, base_qty=?, quote_balance=?,
		realized_pnl=?, total_trades=?, updated_at=? WHERE id=?`,
		stateJSON, baseQty, quoteBalance, realizedPnL, totalTrades, time.Now().UnixMilli(), id,
	)
	return err
}

// UpdateStatus sets the bot status and stamps started_at (running) or
// stopped_at (stopped/finished) accordingly.
func (r *GridRepo) UpdateStatus(id string, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	now := time.Now().UnixMilli()
	query := `UPDATE grid_bots SET status=?, updated_at=?`
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

// InsertTrade records one grid fill.
func (r *GridRepo) InsertTrade(botID string, level int, side string, price, qty, quoteQty, fee, pnl float64, ts int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`INSERT INTO grid_bot_trades
		(bot_id, level_index, side, price, quantity, quote_qty, fee, pnl, ts)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		botID, level, side, price, qty, quoteQty, fee, pnl, ts,
	)
	return err
}

// InsertSnapshot records one periodic equity snapshot.
func (r *GridRepo) InsertSnapshot(botID string, equity, price, realized float64, openOrders int, ts int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`INSERT INTO grid_bot_snapshots
		(bot_id, equity, price, realized_pnl, open_orders, ts) VALUES (?,?,?,?,?,?)`,
		botID, equity, price, realized, openOrders, ts,
	)
	return err
}

// GetTrades returns the bot's fills, newest first. Use limit=0 for unlimited.
func (r *GridRepo) GetTrades(botID string, limit int) ([]*GridTradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT id, bot_id, level_index, side, price, quantity, quote_qty, fee, pnl, ts
		FROM grid_bot_trades WHERE bot_id=? ORDER BY ts DESC, id DESC`
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

	var result []*GridTradeRecord
	for rows.Next() {
		var t GridTradeRecord
		if err := rows.Scan(&t.ID, &t.BotID, &t.Level, &t.Side, &t.Price, &t.Quantity,
			&t.QuoteQty, &t.Fee, &t.PnL, &t.Ts); err != nil {
			return nil, err
		}
		result = append(result, &t)
	}
	return result, rows.Err()
}

// GetSnapshots returns the bot's equity snapshots, newest first. Use
// limit=0 for unlimited; sinceTs>0 restricts to snapshots at/after that timestamp.
func (r *GridRepo) GetSnapshots(botID string, limit int, sinceTs int64) ([]*GridSnapshotRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT id, bot_id, equity, price, realized_pnl, open_orders, ts
		FROM grid_bot_snapshots WHERE bot_id=?`
	args := []any{botID}
	if sinceTs > 0 {
		query += " AND ts>=?"
		args = append(args, sinceTs)
	}
	query += " ORDER BY ts DESC, id DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*GridSnapshotRecord
	for rows.Next() {
		var s GridSnapshotRecord
		if err := rows.Scan(&s.ID, &s.BotID, &s.Equity, &s.Price, &s.RealizedPnL,
			&s.OpenOrders, &s.Ts); err != nil {
			return nil, err
		}
		result = append(result, &s)
	}
	return result, rows.Err()
}

// scanGridBot scans a single row into a GridBotRecord.
func scanGridBot(scanner interface {
	Scan(dest ...any) error
}) (*GridBotRecord, error) {
	var b GridBotRecord
	err := scanner.Scan(
		&b.ID, &b.UserID, &b.Name, &b.Symbol, &b.LowerPrice, &b.UpperPrice, &b.GridCount,
		&b.Investment, &b.Status, &b.Exchange, &b.FeeRate, &b.RealizedPnL, &b.TotalTrades,
		&b.BaseQty, &b.QuoteBalance, &b.StateJSON, &b.InitialEquity, &b.CreatedAt,
		&b.UpdatedAt, &b.StartedAt, &b.StoppedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}
