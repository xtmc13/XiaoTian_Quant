package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// ── Layered Martingale Bot Repository ──
//
// 分层马丁格尔机器人（A1.3，对标 QuantDinger layered martingale）的 typed
// CRUD，直接操作包级 db，风格对齐 GridRepo。layered_martin_bots 为机器人
// 主表；layered_martin_groups 为每个分组的运行状态（层数/预算硬限/持仓/
// 在途层）；layered_martin_orders 记录每笔买入/卖出成交。
// 表结构见 migrations/sql/0007_layered_martin_bots.sql。

// LayeredMartinBotRecord is the SQLite-backed representation of a layered martingale bot.
type LayeredMartinBotRecord struct {
	ID                string  `json:"id"`
	UserID            int64   `json:"user_id"`
	Name              string  `json:"name"`
	Symbol            string  `json:"symbol"`
	Exchange          string  `json:"exchange"`
	PriceDeviationPct float64 `json:"price_deviation_pct"`
	TakeProfitPct     float64 `json:"take_profit_pct"`
	StopLossPct       float64 `json:"stop_loss_pct"`
	TrailingEnabled   bool    `json:"trailing_enabled"`
	Status            string  `json:"status"`
	RealizedPnL       float64 `json:"realized_pnl"`
	TotalTrades       int     `json:"total_trades"`
	CreatedAt         int64   `json:"created_at"`
	UpdatedAt         int64   `json:"updated_at"`
	StartedAt         int64   `json:"started_at"`
	StoppedAt         int64   `json:"stopped_at"`
}

// LayeredMartinGroupRecord is the persisted runtime state of one martingale group.
type LayeredMartinGroupRecord struct {
	ID            int64   `json:"id"`
	BotID         string  `json:"bot_id"`
	GroupIndex    int     `json:"group_index"`
	QuoteAmount   float64 `json:"quote_amount"`
	Multiplier    float64 `json:"multiplier"`
	MaxLayers     int     `json:"max_layers"`
	BudgetCap     float64 `json:"budget_cap"`
	Layer         int     `json:"layer"`
	TotalInvested float64 `json:"total_invested"`
	BaseQty       float64 `json:"base_qty"`
	AvgPrice      float64 `json:"avg_price"`
	EntryPrice    float64 `json:"entry_price"`
	HighestPrice  float64 `json:"highest_price"`
	PendingLayer  int     `json:"pending_layer"`
	Status        string  `json:"status"`
	CreatedAt     int64   `json:"created_at"`
	UpdatedAt     int64   `json:"updated_at"`
}

// LayeredMartinOrderRecord is a single group fill persisted in layered_martin_orders.
type LayeredMartinOrderRecord struct {
	ID         int64   `json:"id"`
	BotID      string  `json:"bot_id"`
	GroupIndex int     `json:"group_index"`
	Side       string  `json:"side"`
	Layer      int     `json:"layer"`
	Price      float64 `json:"price"`
	Quantity   float64 `json:"quantity"`
	QuoteQty   float64 `json:"quote_qty"`
	Reason     string  `json:"reason"`
	Ts         int64   `json:"ts"`
}

// LayeredMartinRepo provides typed CRUD for layered martingale bots, groups and orders.
type LayeredMartinRepo struct {
	mu sync.RWMutex
}

func NewLayeredMartinRepo() *LayeredMartinRepo { return &LayeredMartinRepo{} }

const layeredMartinBotColumns = `id, user_id, name, symbol, exchange, price_deviation_pct,
	take_profit_pct, stop_loss_pct, trailing_enabled, status, realized_pnl, total_trades,
	created_at, updated_at, started_at, stopped_at`

const layeredMartinGroupColumns = `id, bot_id, group_index, quote_amount, multiplier,
	max_layers, budget_cap, layer, total_invested, base_qty, avg_price, entry_price,
	highest_price, pending_layer, status, created_at, updated_at`

// Create inserts a new layered martingale bot (groups are inserted separately).
func (r *LayeredMartinRepo) Create(b *LayeredMartinBotRecord) error {
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
	_, err := db.Exec(`INSERT INTO layered_martin_bots (`+layeredMartinBotColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.UserID, b.Name, b.Symbol, b.Exchange, b.PriceDeviationPct,
		b.TakeProfitPct, b.StopLossPct, b.TrailingEnabled, b.Status, b.RealizedPnL,
		b.TotalTrades, b.CreatedAt, b.UpdatedAt, b.StartedAt, b.StoppedAt,
	)
	return err
}

// GetByID returns a layered martingale bot by ID (nil, nil when not found).
func (r *LayeredMartinRepo) GetByID(id string) (*LayeredMartinBotRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	row := db.QueryRow(`SELECT `+layeredMartinBotColumns+` FROM layered_martin_bots WHERE id=?`, id)
	return scanLayeredMartinBot(row)
}

// List returns layered martingale bots matching the filter. Use limit=0 for unlimited.
func (r *LayeredMartinRepo) List(filter map[string]any, limit int) ([]*LayeredMartinBotRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT ` + layeredMartinBotColumns + ` FROM layered_martin_bots`
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

	var result []*LayeredMartinBotRecord
	for rows.Next() {
		b, err := scanLayeredMartinBot(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// Update modifies an existing layered martingale bot (full-row write; bumps updated_at).
func (r *LayeredMartinRepo) Update(b *LayeredMartinBotRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	b.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(`UPDATE layered_martin_bots SET user_id=?, name=?, symbol=?, exchange=?,
		price_deviation_pct=?, take_profit_pct=?, stop_loss_pct=?, trailing_enabled=?,
		status=?, realized_pnl=?, total_trades=?, created_at=?, updated_at=?,
		started_at=?, stopped_at=? WHERE id=?`,
		b.UserID, b.Name, b.Symbol, b.Exchange, b.PriceDeviationPct,
		b.TakeProfitPct, b.StopLossPct, b.TrailingEnabled, b.Status, b.RealizedPnL,
		b.TotalTrades, b.CreatedAt, b.UpdatedAt, b.StartedAt, b.StoppedAt, b.ID,
	)
	return err
}

// UpdateState persists realized pnl / trade count in a single-row UPDATE (hot path).
func (r *LayeredMartinRepo) UpdateState(id string, realizedPnL float64, totalTrades int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE layered_martin_bots SET realized_pnl=?, total_trades=?,
		updated_at=? WHERE id=?`,
		realizedPnL, totalTrades, time.Now().UnixMilli(), id,
	)
	return err
}

// UpdateStatus sets the bot status and stamps started_at (running) or
// stopped_at (stopped/finished) accordingly.
func (r *LayeredMartinRepo) UpdateStatus(id string, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	now := time.Now().UnixMilli()
	query := `UPDATE layered_martin_bots SET status=?, updated_at=?`
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

// Delete removes a layered martingale bot. Its groups and orders are kept for history.
func (r *LayeredMartinRepo) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec("DELETE FROM layered_martin_bots WHERE id=?", id)
	return err
}

// DeleteForUser 带属主条件删除（越权防护双保险）：非属主删不掉，按 not found 上报。
func (r *LayeredMartinRepo) DeleteForUser(id string, userID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	res, err := db.Exec("DELETE FROM layered_martin_bots WHERE id=? AND user_id=?", id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}

// ReplaceGroups deletes and re-inserts all groups of a bot (create/update path).
func (r *LayeredMartinRepo) ReplaceGroups(botID string, groups []*LayeredMartinGroupRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	if _, err := db.Exec("DELETE FROM layered_martin_groups WHERE bot_id=?", botID); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for i, g := range groups {
		if g == nil {
			continue
		}
		if g.Status == "" {
			g.Status = "idle"
		}
		if g.Multiplier == 0 {
			g.Multiplier = 2
		}
		if g.CreatedAt == 0 {
			g.CreatedAt = now
		}
		g.UpdatedAt = now
		g.BotID = botID
		g.GroupIndex = i
		// 全量替换：组主键由 SQLite 重新分配（显式 0 不会触发自增，须写 NULL），
		// 避免携带旧 ID 撞 UNIQUE。
		res, err := db.Exec(`INSERT INTO layered_martin_groups (`+layeredMartinGroupColumns+`)
			VALUES (NULL,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			botID, i, g.QuoteAmount, g.Multiplier, g.MaxLayers, g.BudgetCap,
			g.Layer, g.TotalInvested, g.BaseQty, g.AvgPrice, g.EntryPrice,
			g.HighestPrice, g.PendingLayer, g.Status, g.CreatedAt, g.UpdatedAt,
		)
		if err != nil {
			return err
		}
		if id, err := res.LastInsertId(); err == nil {
			g.ID = id
		}
	}
	return nil
}

// GetGroups returns all groups of a bot ordered by group_index.
func (r *LayeredMartinRepo) GetGroups(botID string) ([]*LayeredMartinGroupRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := db.Query(`SELECT `+layeredMartinGroupColumns+` FROM layered_martin_groups
		WHERE bot_id=? ORDER BY group_index ASC`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*LayeredMartinGroupRecord
	for rows.Next() {
		g, err := scanLayeredMartinGroup(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

// UpdateGroup persists one group's runtime state (hot path, after fills).
func (r *LayeredMartinRepo) UpdateGroup(g *LayeredMartinGroupRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	g.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(`UPDATE layered_martin_groups SET layer=?, total_invested=?, base_qty=?,
		avg_price=?, entry_price=?, highest_price=?, pending_layer=?, status=?, updated_at=?
		WHERE id=?`,
		g.Layer, g.TotalInvested, g.BaseQty, g.AvgPrice, g.EntryPrice,
		g.HighestPrice, g.PendingLayer, g.Status, g.UpdatedAt, g.ID,
	)
	return err
}

// InsertOrder records one group fill (buy or sell).
func (r *LayeredMartinRepo) InsertOrder(botID string, groupIndex int, side string, layer int, price, qty, quoteQty float64, reason string, ts int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`INSERT INTO layered_martin_orders
		(bot_id, group_index, side, layer, price, quantity, quote_qty, reason, ts)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		botID, groupIndex, side, layer, price, qty, quoteQty, reason, ts,
	)
	return err
}

// GetOrders returns the bot's fills, newest first. Use limit=0 for unlimited.
func (r *LayeredMartinRepo) GetOrders(botID string, limit int) ([]*LayeredMartinOrderRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT id, bot_id, group_index, side, layer, price, quantity, quote_qty, reason, ts
		FROM layered_martin_orders WHERE bot_id=? ORDER BY ts DESC, id DESC`
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

	var result []*LayeredMartinOrderRecord
	for rows.Next() {
		var o LayeredMartinOrderRecord
		if err := rows.Scan(&o.ID, &o.BotID, &o.GroupIndex, &o.Side, &o.Layer,
			&o.Price, &o.Quantity, &o.QuoteQty, &o.Reason, &o.Ts); err != nil {
			return nil, err
		}
		result = append(result, &o)
	}
	return result, rows.Err()
}

// scanLayeredMartinBot scans a single row into a LayeredMartinBotRecord.
func scanLayeredMartinBot(scanner interface {
	Scan(dest ...any) error
}) (*LayeredMartinBotRecord, error) {
	var b LayeredMartinBotRecord
	err := scanner.Scan(
		&b.ID, &b.UserID, &b.Name, &b.Symbol, &b.Exchange, &b.PriceDeviationPct,
		&b.TakeProfitPct, &b.StopLossPct, &b.TrailingEnabled, &b.Status, &b.RealizedPnL,
		&b.TotalTrades, &b.CreatedAt, &b.UpdatedAt, &b.StartedAt, &b.StoppedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// scanLayeredMartinGroup scans a single row into a LayeredMartinGroupRecord.
func scanLayeredMartinGroup(scanner interface {
	Scan(dest ...any) error
}) (*LayeredMartinGroupRecord, error) {
	var g LayeredMartinGroupRecord
	err := scanner.Scan(
		&g.ID, &g.BotID, &g.GroupIndex, &g.QuoteAmount, &g.Multiplier, &g.MaxLayers,
		&g.BudgetCap, &g.Layer, &g.TotalInvested, &g.BaseQty, &g.AvgPrice, &g.EntryPrice,
		&g.HighestPrice, &g.PendingLayer, &g.Status, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &g, nil
}
