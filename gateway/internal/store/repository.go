package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ── Repository generic interface ──

// Repository provides typed CRUD for a domain entity.
type Repository[T any] interface {
	Create(item *T) error
	GetByID(id string) (*T, error)
	Update(item *T) error
	Delete(id string) error
	List(filter map[string]any, limit int) ([]*T, error)
}

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
	args, where := buildFilter(filter)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
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

// ── Position Repository ──

type PositionRecord struct {
	ID             string  `json:"id"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	Quantity       float64 `json:"quantity"`
	AvgEntryPrice  float64 `json:"avg_entry_price"`
	CurrentPrice   float64 `json:"current_price"`
	UnrealizedPnL  float64 `json:"unrealized_pnl"`
	RealizedPnL    float64 `json:"realized_pnl"`
	CostBasis      float64 `json:"cost_basis"`
	Exchange       string  `json:"exchange"`
	Status         string  `json:"status"`
	OpenedAt       int64   `json:"opened_at"`
	ClosedAt       int64   `json:"closed_at"`
	UpdatedAt      int64   `json:"updated_at"`
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
	args, where := buildFilter(filter)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY updated_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
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

// ── Signal Repository ──

type SignalRecord struct {
	ID             int     `json:"id"`
	Symbol         string  `json:"symbol"`
	Direction      string  `json:"direction"`
	Strength       float64 `json:"strength"`
	Strategy       string  `json:"strategy"`
	Reason         string  `json:"reason"`
	EntryPrice     float64 `json:"entry_price"`
	StopLoss       float64 `json:"stop_loss"`
	TakeProfit     float64 `json:"take_profit"`
	PositionSize   float64 `json:"position_size"`
	Status         string  `json:"status"`
	ExecutedOrderID string `json:"executed_order_id"`
	CreatedAt      int64   `json:"created_at"`
}

type SignalRepo struct{ mu sync.RWMutex }

func NewSignalRepo() *SignalRepo { return &SignalRepo{} }

func (r *SignalRepo) Create(s *SignalRecord) error {
	if s.Status == "" {
		s.Status = "PENDING"
	}
	if s.CreatedAt == 0 {
		s.CreatedAt = time.Now().UnixMilli()
	}
	res, err := db.Exec(
		`INSERT INTO xt_signals (symbol, direction, strength, strategy, reason, entry_price, stop_loss, take_profit, position_size, status, executed_order_id, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.Symbol, s.Direction, s.Strength, s.Strategy, s.Reason, s.EntryPrice, s.StopLoss, s.TakeProfit, s.PositionSize, s.Status, s.ExecutedOrderID, s.CreatedAt,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	s.ID = int(id)
	return nil
}

func (r *SignalRepo) GetByID(id string) (*SignalRecord, error) {
	row := db.QueryRow(`SELECT id, symbol, direction, strength, strategy, reason, entry_price, stop_loss, take_profit, position_size, status, executed_order_id, created_at FROM xt_signals WHERE id=?`, id)
	var s SignalRecord
	err := row.Scan(&s.ID, &s.Symbol, &s.Direction, &s.Strength, &s.Strategy, &s.Reason, &s.EntryPrice, &s.StopLoss, &s.TakeProfit, &s.PositionSize, &s.Status, &s.ExecutedOrderID, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SignalRepo) List(filter map[string]any, limit int) ([]*SignalRecord, error) {
	query := "SELECT id, symbol, direction, strength, strategy, reason, entry_price, stop_loss, take_profit, position_size, status, executed_order_id, created_at FROM xt_signals"
	args, where := buildFilter(filter)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*SignalRecord
	for rows.Next() {
		var s SignalRecord
		if err := rows.Scan(&s.ID, &s.Symbol, &s.Direction, &s.Strength, &s.Strategy, &s.Reason, &s.EntryPrice, &s.StopLoss, &s.TakeProfit, &s.PositionSize, &s.Status, &s.ExecutedOrderID, &s.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, &s)
	}
	return result, nil
}

func (r *SignalRepo) Update(s *SignalRecord) error {
	_, err := db.Exec(
		`UPDATE xt_signals SET status=?, executed_order_id=? WHERE id=?`,
		s.Status, s.ExecutedOrderID, s.ID,
	)
	return err
}

func (r *SignalRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_signals WHERE id=?", id)
	return err
}

// ── Risk Event Repository ──

type RiskEventRecord struct {
	ID        int    `json:"id"`
	Level     string `json:"level"`
	CheckName string `json:"check_name"`
	Message   string `json:"message"`
	Symbol    string `json:"symbol"`
	Context   string `json:"context"`
	Timestamp int64  `json:"timestamp"`
}

type RiskEventRepo struct{ mu sync.RWMutex }

func NewRiskEventRepo() *RiskEventRepo { return &RiskEventRepo{} }

func (r *RiskEventRepo) Create(e *RiskEventRecord) error {
	if e.Timestamp == 0 {
		e.Timestamp = time.Now().UnixMilli()
	}
	if e.Context == "" {
		e.Context = "{}"
	}
	res, err := db.Exec(
		`INSERT INTO risk_events (level, check_name, message, symbol, context_json, timestamp) VALUES (?,?,?,?,?,?)`,
		e.Level, e.CheckName, e.Message, e.Symbol, e.Context, e.Timestamp,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	e.ID = int(id)
	return nil
}

func (r *RiskEventRepo) List(filter map[string]any, limit int) ([]*RiskEventRecord, error) {
	query := "SELECT id, level, check_name, message, symbol, context_json, timestamp FROM risk_events"
	args, where := buildFilter(filter)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY timestamp DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*RiskEventRecord
	for rows.Next() {
		var e RiskEventRecord
		if err := rows.Scan(&e.ID, &e.Level, &e.CheckName, &e.Message, &e.Symbol, &e.Context, &e.Timestamp); err != nil {
			return nil, err
		}
		result = append(result, &e)
	}
	return result, nil
}

func (r *RiskEventRepo) GetByID(id string) (*RiskEventRecord, error) {
	row := db.QueryRow(`SELECT id, level, check_name, message, symbol, context_json, timestamp FROM risk_events WHERE id=?`, id)
	var e RiskEventRecord
	err := row.Scan(&e.ID, &e.Level, &e.CheckName, &e.Message, &e.Symbol, &e.Context, &e.Timestamp)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *RiskEventRepo) Update(e *RiskEventRecord) error { return nil }
func (r *RiskEventRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM risk_events WHERE id=?", id)
	return err
}

// ── Market Data Repository ──

type MarketBarRecord struct {
	Symbol    string  `json:"symbol"`
	Interval  string  `json:"interval"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	Timestamp int64   `json:"timestamp"`
}

type MarketDataRepo struct{ mu sync.RWMutex }

func NewMarketDataRepo() *MarketDataRepo { return &MarketDataRepo{} }

func (r *MarketDataRepo) Create(b *MarketBarRecord) error {
	_, err := db.Exec(
		`INSERT OR REPLACE INTO market_bars (symbol, interval, open, high, low, close, volume, timestamp) VALUES (?,?,?,?,?,?,?,?)`,
		b.Symbol, b.Interval, b.Open, b.High, b.Low, b.Close, b.Volume, b.Timestamp,
	)
	return err
}

func (r *MarketDataRepo) BatchCreate(bars []MarketBarRecord) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO market_bars (symbol, interval, open, high, low, close, volume, timestamp) VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, b := range bars {
		if _, err := stmt.Exec(b.Symbol, b.Interval, b.Open, b.High, b.Low, b.Close, b.Volume, b.Timestamp); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (r *MarketDataRepo) GetBars(symbol, interval string, startTime, endTime int64, limit int) ([]*MarketBarRecord, error) {
	query := "SELECT symbol, interval, open, high, low, close, volume, timestamp FROM market_bars WHERE symbol=? AND interval=?"
	args := []any{symbol, interval}
	if startTime > 0 {
		query += " AND timestamp >= ?"
		args = append(args, startTime)
	}
	if endTime > 0 {
		query += " AND timestamp <= ?"
		args = append(args, endTime)
	}
	query += " ORDER BY timestamp ASC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*MarketBarRecord
	for rows.Next() {
		var b MarketBarRecord
		if err := rows.Scan(&b.Symbol, &b.Interval, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume, &b.Timestamp); err != nil {
			return nil, err
		}
		result = append(result, &b)
	}
	return result, nil
}

func (r *MarketDataRepo) GetByID(id string) (*MarketBarRecord, error) {
	row := db.QueryRow(`SELECT symbol, interval, open, high, low, close, volume, timestamp FROM market_bars WHERE id=?`, id)
	var b MarketBarRecord
	err := row.Scan(&b.Symbol, &b.Interval, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume, &b.Timestamp)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *MarketDataRepo) List(filter map[string]any, limit int) ([]*MarketBarRecord, error) { return nil, nil }
func (r *MarketDataRepo) Update(b *MarketBarRecord) error { return r.Create(b) }
func (r *MarketDataRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM market_bars WHERE id=?", id)
	return err
}

// ── Backtest Repository ──

type BacktestRecord struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Strategy    string `json:"strategy"`
	Symbol      string `json:"symbol"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	DurationMs  int64  `json:"duration_ms"`
	Status      string `json:"status"`
	ReportJSON  string `json:"report_json"`
	CreatedAt   int64  `json:"created_at"`
	CompletedAt int64  `json:"completed_at"`
}

type BacktestRepo struct{ mu sync.RWMutex }

func NewBacktestRepo() *BacktestRepo { return &BacktestRepo{} }

func (r *BacktestRepo) Create(b *BacktestRecord) error {
	if b.Status == "" {
		b.Status = "PENDING"
	}
	if b.CreatedAt == 0 {
		b.CreatedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(
		`INSERT INTO xt_backtests (id, name, strategy, symbol, start_time, end_time, duration_ms, status, report_json, created_at, completed_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.Name, b.Strategy, b.Symbol, b.StartTime, b.EndTime, b.DurationMs, b.Status, b.ReportJSON, b.CreatedAt, b.CompletedAt,
	)
	return err
}

func (r *BacktestRepo) GetByID(id string) (*BacktestRecord, error) {
	row := db.QueryRow(`SELECT id, name, strategy, symbol, start_time, end_time, duration_ms, status, report_json, created_at, completed_at FROM xt_backtests WHERE id=?`, id)
	var b BacktestRecord
	err := row.Scan(&b.ID, &b.Name, &b.Strategy, &b.Symbol, &b.StartTime, &b.EndTime, &b.DurationMs, &b.Status, &b.ReportJSON, &b.CreatedAt, &b.CompletedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *BacktestRepo) List(filter map[string]any, limit int) ([]*BacktestRecord, error) {
	query := "SELECT id, name, strategy, symbol, start_time, end_time, duration_ms, status, report_json, created_at, completed_at FROM xt_backtests"
	args, where := buildFilter(filter)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*BacktestRecord
	for rows.Next() {
		var b BacktestRecord
		if err := rows.Scan(&b.ID, &b.Name, &b.Strategy, &b.Symbol, &b.StartTime, &b.EndTime, &b.DurationMs, &b.Status, &b.ReportJSON, &b.CreatedAt, &b.CompletedAt); err != nil {
			return nil, err
		}
		result = append(result, &b)
	}
	return result, nil
}

func (r *BacktestRepo) Update(b *BacktestRecord) error {
	_, err := db.Exec(
		`UPDATE xt_backtests SET status=?, report_json=?, duration_ms=?, completed_at=? WHERE id=?`,
		b.Status, b.ReportJSON, b.DurationMs, b.CompletedAt, b.ID,
	)
	return err
}

func (r *BacktestRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_backtests WHERE id=?", id)
	return err
}

// ── Portfolio Snapshot Repository ──

type PortfolioSnapshotRepo struct{ mu sync.RWMutex }

func NewPortfolioSnapshotRepo() *PortfolioSnapshotRepo { return &PortfolioSnapshotRepo{} }

func (r *PortfolioSnapshotRepo) Save(totalEquity, availableBalance, marginUsed, drawdown, netExposure float64, positions, balances any) error {
	posJSON, _ := json.Marshal(positions)
	balJSON, _ := json.Marshal(balances)
	now := time.Now().UnixMilli()
	_, err := db.Exec(
		`INSERT INTO xt_portfolio_snapshots (total_equity, available_balance, margin_used, drawdown, net_exposure, positions_json, balances_json, timestamp)
		 VALUES (?,?,?,?,?,?,?,?)`,
		totalEquity, availableBalance, marginUsed, drawdown, netExposure, string(posJSON), string(balJSON), now,
	)
	return err
}

func (r *PortfolioSnapshotRepo) GetRecent(limit int) ([]map[string]any, error) {
	rows, err := db.Query(`SELECT total_equity, available_balance, margin_used, drawdown, net_exposure, timestamp FROM xt_portfolio_snapshots ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var equity, avail, margin, dd, exposure float64
		var ts int64
		rows.Scan(&equity, &avail, &margin, &dd, &exposure, &ts)
		result = append(result, map[string]any{
			"total_equity": equity, "available_balance": avail, "margin_used": margin,
			"drawdown": dd, "net_exposure": exposure, "timestamp": ts,
		})
	}
	return result, nil
}

// ── Agent Token Repository ──

type AgentTokenRecord struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	TokenHash   string `json:"token_hash"`
	TokenPrefix string `json:"token_prefix"`
	Scopes      string `json:"scopes"`
	RateLimitRPS int   `json:"rate_limit_rps"`
	IsActive    int    `json:"is_active"`
	ExpiresAt   int64  `json:"expires_at"`
	LastUsedAt  int64  `json:"last_used_at"`
	CreatedAt   int64  `json:"created_at"`
}

type AgentTokenRepo struct{ mu sync.RWMutex }

func NewAgentTokenRepo() *AgentTokenRepo { return &AgentTokenRepo{} }

func (r *AgentTokenRepo) Create(t *AgentTokenRecord) error {
	if t.CreatedAt == 0 {
		t.CreatedAt = time.Now().UnixMilli()
	}
	if t.Scopes == "" {
		t.Scopes = "read"
	}
	if t.RateLimitRPS <= 0 {
		t.RateLimitRPS = 10
	}
	res, err := db.Exec(
		`INSERT INTO agent_tokens (name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		t.Name, t.TokenHash, t.TokenPrefix, t.Scopes, t.RateLimitRPS, t.IsActive, t.ExpiresAt, t.LastUsedAt, t.CreatedAt,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	t.ID = int(id)
	return nil
}

func (r *AgentTokenRepo) GetByTokenHash(hash string) (*AgentTokenRecord, error) {
	row := db.QueryRow(`SELECT id, name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at FROM agent_tokens WHERE token_hash=?`, hash)
	var t AgentTokenRecord
	err := row.Scan(&t.ID, &t.Name, &t.TokenHash, &t.TokenPrefix, &t.Scopes, &t.RateLimitRPS, &t.IsActive, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *AgentTokenRepo) GetByID(id string) (*AgentTokenRecord, error) {
	row := db.QueryRow(`SELECT id, name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at FROM agent_tokens WHERE id=?`, id)
	var t AgentTokenRecord
	err := row.Scan(&t.ID, &t.Name, &t.TokenHash, &t.TokenPrefix, &t.Scopes, &t.RateLimitRPS, &t.IsActive, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *AgentTokenRepo) UpdateLastUsed(id int) error {
	_, err := db.Exec("UPDATE agent_tokens SET last_used_at=? WHERE id=?", time.Now().UnixMilli(), id)
	return err
}

func (r *AgentTokenRepo) Revoke(id int) error {
	_, err := db.Exec("UPDATE agent_tokens SET is_active=0 WHERE id=?", id)
	return err
}

func (r *AgentTokenRepo) List(filter map[string]any, limit int) ([]*AgentTokenRecord, error) {
	query := "SELECT id, name, token_hash, token_prefix, scopes, rate_limit_rps, is_active, expires_at, last_used_at, created_at FROM agent_tokens"
	args, where := buildFilter(filter)
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*AgentTokenRecord
	for rows.Next() {
		var t AgentTokenRecord
		if err := rows.Scan(&t.ID, &t.Name, &t.TokenHash, &t.TokenPrefix, &t.Scopes, &t.RateLimitRPS, &t.IsActive, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, &t)
	}
	return result, nil
}

func (r *AgentTokenRepo) Update(t *AgentTokenRecord) error { return nil }
func (r *AgentTokenRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM agent_tokens WHERE id=?", id)
	return err
}

// ── Agent Audit Repository ──

type AuditRecord struct {
	ID            int    `json:"id"`
	TokenID       int    `json:"token_id"`
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	Method        string `json:"method"`
	ParamsSummary string `json:"params_summary"`
	StatusCode    int    `json:"status_code"`
	IP            string `json:"ip"`
	UserAgent     string `json:"user_agent"`
	Timestamp     int64  `json:"timestamp"`
}

type AuditRepo struct{ mu sync.RWMutex }

func NewAuditRepo() *AuditRepo { return &AuditRepo{} }

func (r *AuditRepo) Log(record *AuditRecord) error {
	if record.Timestamp == 0 {
		record.Timestamp = time.Now().UnixMilli()
	}
	if record.Method == "" {
		record.Method = "POST"
	}
	res, err := db.Exec(
		`INSERT INTO agent_audit_log (token_id, name, endpoint, method, params_summary, status_code, ip, user_agent, timestamp)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		record.TokenID, record.Name, record.Endpoint, record.Method, record.ParamsSummary, record.StatusCode, record.IP, record.UserAgent, record.Timestamp,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	record.ID = int(id)
	return nil
}

func (r *AuditRepo) GetRecent(limit int) ([]*AuditRecord, error) {
	rows, err := db.Query(`SELECT id, token_id, name, endpoint, method, params_summary, status_code, ip, user_agent, timestamp FROM agent_audit_log ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*AuditRecord
	for rows.Next() {
		var a AuditRecord
		if err := rows.Scan(&a.ID, &a.TokenID, &a.Name, &a.Endpoint, &a.Method, &a.ParamsSummary, &a.StatusCode, &a.IP, &a.UserAgent, &a.Timestamp); err != nil {
			return nil, err
		}
		result = append(result, &a)
	}
	return result, nil
}

func (r *AuditRepo) GetByTokenID(tokenID int, limit int) ([]*AuditRecord, error) {
	rows, err := db.Query(`SELECT id, token_id, name, endpoint, method, params_summary, status_code, ip, user_agent, timestamp FROM agent_audit_log WHERE token_id=? ORDER BY timestamp DESC LIMIT ?`, tokenID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*AuditRecord
	for rows.Next() {
		var a AuditRecord
		if err := rows.Scan(&a.ID, &a.TokenID, &a.Name, &a.Endpoint, &a.Method, &a.ParamsSummary, &a.StatusCode, &a.IP, &a.UserAgent, &a.Timestamp); err != nil {
			return nil, err
		}
		result = append(result, &a)
	}
	return result, nil
}

func (r *AuditRepo) GetByID(id string) (*AuditRecord, error) {
	row := db.QueryRow(`SELECT id, token_id, name, endpoint, method, params_summary, status_code, ip, user_agent, timestamp FROM agent_audit_log WHERE id=?`, id)
	var a AuditRecord
	err := row.Scan(&a.ID, &a.TokenID, &a.Name, &a.Endpoint, &a.Method, &a.ParamsSummary, &a.StatusCode, &a.IP, &a.UserAgent, &a.Timestamp)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *AuditRepo) List(filter map[string]any, limit int) ([]*AuditRecord, error) {
	return r.GetRecent(limit)
}
func (r *AuditRepo) Update(a *AuditRecord) error { return nil }
func (r *AuditRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM agent_audit_log WHERE id=?", id)
	return err
}

// ── Helpers ──

func buildFilter(filter map[string]any) ([]any, string) {
	if len(filter) == 0 {
		return nil, ""
	}
	var clauses []string
	var args []any
	for key, val := range filter {
		clauses = append(clauses, fmt.Sprintf("%s=?", key))
		args = append(args, val)
	}
	return args, strings.Join(clauses, " AND ")
}
