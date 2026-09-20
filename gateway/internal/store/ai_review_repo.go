package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// ── AI 复盘报告 Repository ──
// 表结构见 migrations/sql/0015_ai_review_reports.sql。

type AIReviewReportRecord struct {
	ID          string  `json:"id"`
	UserID      int64   `json:"user_id"`
	ScopeType   string  `json:"scope_type"`
	ScopeID     string  `json:"scope_id"`
	PeriodStart int64   `json:"period_start"`
	PeriodEnd   int64   `json:"period_end"`
	TradesCount int     `json:"trades_count"`
	TotalPnL    float64 `json:"total_pnl"`
	WinRate     float64 `json:"win_rate"`
	MaxDrawdown float64 `json:"max_drawdown"`
	ReportText  string  `json:"report_text"`
	Model       string  `json:"model"`
	Status      string  `json:"status"`
	Error       string  `json:"error"`
	CreatedAt   int64   `json:"created_at"`
}

type AIReviewReportRepo struct{ mu sync.RWMutex }

func NewAIReviewReportRepo() *AIReviewReportRepo { return &AIReviewReportRepo{} }

func (r *AIReviewReportRepo) Create(rec *AIReviewReportRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	if rec.ID == "" {
		rec.ID = fmt.Sprintf("aivr-%d", time.Now().UnixNano())
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(
		`INSERT INTO xt_ai_review_reports
			(id, user_id, scope_type, scope_id, period_start, period_end,
			 trades_count, total_pnl, win_rate, max_drawdown,
			 report_text, model, status, error, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.ScopeType, rec.ScopeID, rec.PeriodStart, rec.PeriodEnd,
		rec.TradesCount, rec.TotalPnL, rec.WinRate, rec.MaxDrawdown,
		rec.ReportText, rec.Model, rec.Status, rec.Error, rec.CreatedAt,
	)
	return err
}

const aiReviewReportColumns = `id, user_id, scope_type, scope_id, period_start, period_end,
	trades_count, total_pnl, win_rate, max_drawdown,
	report_text, model, status, error, created_at`

func (r *AIReviewReportRepo) scan(s rowScanner) (*AIReviewReportRecord, error) {
	var rec AIReviewReportRecord
	err := s.Scan(&rec.ID, &rec.UserID, &rec.ScopeType, &rec.ScopeID,
		&rec.PeriodStart, &rec.PeriodEnd, &rec.TradesCount, &rec.TotalPnL,
		&rec.WinRate, &rec.MaxDrawdown, &rec.ReportText, &rec.Model,
		&rec.Status, &rec.Error, &rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetByID 取单条（属主校验由 handler 做）。
func (r *AIReviewReportRepo) GetByID(id string) (*AIReviewReportRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	row := db.QueryRow(`SELECT `+aiReviewReportColumns+` FROM xt_ai_review_reports WHERE id = ?`, id)
	return r.scan(row)
}

// ListByUser 返回该用户的复盘报告（可按 scope 过滤），新→旧，limit<=0 时默认 50。
func (r *AIReviewReportRepo) ListByUser(userID int64, scopeType, scopeID string, limit int) ([]*AIReviewReportRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT ` + aiReviewReportColumns + ` FROM xt_ai_review_reports WHERE user_id = ?`
	args := []any{userID}
	if scopeType != "" {
		query += ` AND scope_type = ?`
		args = append(args, scopeType)
	}
	if scopeID != "" {
		query += ` AND scope_id = ?`
		args = append(args, scopeID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AIReviewReportRecord
	for rows.Next() {
		rec, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// ── Scope 成交加载 ──
//
// AI 复盘统一从"真实成交"出发：OMS 成交（paper/live）一律落 trades 表，
// 机器人经 xt_orders.client_oid="kind:botID" 归因；AI 机器人实例的台账在
// ai_bot_trades（每笔即一次完整开平仓，自带 pnl）。strategy scope 没有
// 订单归因字段，按策略配置的交易对过滤该用户的成交。

// ScopeTrade 复盘用的统一成交视图。
// PnL 仅在 ledger=true（AI 机器人台账，一笔即一次往返）时有值；
// ledger=false（OMS 原始成交流水）时由 handler 做 FIFO 配对推算往返盈亏。
type ScopeTrade struct {
	ID          string  `json:"id"`
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Price       float64 `json:"price"`
	Quantity    float64 `json:"quantity"`
	Fee         float64 `json:"fee"`
	PnL         float64 `json:"pnl"`
	CloseReason string  `json:"close_reason"`
	ClosedAt    int64   `json:"closed_at"`
}

const scopeTradeLoadLimit = 5000

// LoadScopeTrades 拉取指定 scope 在 [startMs, endMs] 窗口内的成交。
// 返回的 ledger=true 表示 PnL 已是整笔往返盈亏（AI 机器人台账），
// ledger=false 表示原始成交流水，PnL 未填，需要 FIFO 配对。
func LoadScopeTrades(scopeType, scopeID string, userID, startMs, endMs int64) (trades []ScopeTrade, ledger bool, err error) {
	if db == nil {
		return nil, false, fmt.Errorf("database not initialized")
	}
	switch scopeType {
	case "strategy":
		t, err := loadStrategyScopeTrades(scopeID, userID, startMs, endMs)
		return t, false, err
	case "bot":
		if isAIBotInstance(scopeID) {
			t, err := loadAIBotScopeTrades(scopeID, startMs, endMs)
			return t, true, err
		}
		t, err := loadBotScopeTrades(scopeID, userID, startMs, endMs)
		return t, false, err
	default:
		return nil, false, fmt.Errorf("scope_type 必须是 strategy|bot")
	}
}

func loadStrategyScopeTrades(scopeID string, userID, startMs, endMs int64) ([]ScopeTrade, error) {
	var symbol string
	err := db.QueryRow(`SELECT symbol FROM strategy_configs WHERE id = ?`, scopeID).Scan(&symbol)
	if err != nil {
		return nil, fmt.Errorf("strategy %s 不存在", scopeID)
	}
	if symbol == "" {
		return nil, fmt.Errorf("strategy %s 未配置交易对", scopeID)
	}
	rows, err := db.Query(
		`SELECT id, symbol, side, price, quantity, fee, created_at FROM trades
		 WHERE user_id = ? AND symbol = ? AND created_at >= ? AND created_at <= ?
		 ORDER BY created_at ASC LIMIT ?`,
		userID, symbol, startMs, endMs, scopeTradeLoadLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScopeTradeRows(rows)
}

func loadBotScopeTrades(scopeID string, userID, startMs, endMs int64) ([]ScopeTrade, error) {
	// OMS 机器人（dca/grid/lmartin）订单 client_oid 形如 "kind:botID"。
	rows, err := db.Query(
		`SELECT t.id, t.symbol, t.side, t.price, t.quantity, t.fee, t.created_at
		 FROM trades t JOIN xt_orders o ON t.order_id = o.id
		 WHERE t.user_id = ? AND t.created_at >= ? AND t.created_at <= ?
		   AND o.client_oid IN (?, ?, ?)
		 ORDER BY t.created_at ASC LIMIT ?`,
		userID, startMs, endMs,
		"dca:"+scopeID, "grid:"+scopeID, "lmartin:"+scopeID,
		scopeTradeLoadLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScopeTradeRows(rows)
}

func loadAIBotScopeTrades(scopeID string, startMs, endMs int64) ([]ScopeTrade, error) {
	// AI 机器人台账：一行即一次完整开平仓，closed_at 为平仓时间。
	rows, err := db.Query(
		`SELECT id, symbol, side, exit_price, quantity, pnl, close_reason, closed_at
		 FROM ai_bot_trades
		 WHERE bot_instance_id = ? AND closed_at > 0 AND closed_at >= ? AND closed_at <= ?
		 ORDER BY closed_at ASC LIMIT ?`,
		scopeID, startMs, endMs, scopeTradeLoadLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScopeTrade
	for rows.Next() {
		var t ScopeTrade
		var id int64
		if err := rows.Scan(&id, &t.Symbol, &t.Side, &t.Price, &t.Quantity, &t.PnL, &t.CloseReason, &t.ClosedAt); err != nil {
			return nil, err
		}
		t.ID = fmt.Sprintf("aibot-%d", id)
		out = append(out, t)
	}
	return out, nil
}

func scanScopeTradeRows(rows *sql.Rows) ([]ScopeTrade, error) {
	var out []ScopeTrade
	for rows.Next() {
		var t ScopeTrade
		if err := rows.Scan(&t.ID, &t.Symbol, &t.Side, &t.Price, &t.Quantity, &t.Fee, &t.ClosedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func isAIBotInstance(scopeID string) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(1) FROM ai_bot_instances WHERE id = ?`, scopeID).Scan(&n)
	return n > 0
}
