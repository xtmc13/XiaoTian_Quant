package store

import (
	"sync"
	"time"
)

// ── Signal Repository ──

// SignalRecord 是信号机器人（SignalExecutor）收到的原始信号。
// webhook 接收后落库，执行结果经 ExecutedOrderID / RealizedPnL /
// ClosedAt 回填，status 机：PENDING → EXECUTED → CLOSED / FAILED。
type SignalRecord struct {
	ID              int     `json:"id"`
	Symbol          string  `json:"symbol"`
	Direction       string  `json:"direction"`
	Strength        float64 `json:"strength"`
	Strategy        string  `json:"strategy"`
	Reason          string  `json:"reason"`
	EntryPrice      float64 `json:"entry_price"`
	StopLoss        float64 `json:"stop_loss"`
	TakeProfit      float64 `json:"take_profit"`
	PositionSize    float64 `json:"position_size"`
	Status          string  `json:"status"`
	ExecutedOrderID string  `json:"executed_order_id"`
	CreatedAt       int64   `json:"created_at"`

	// ── 0024 信号机器人真实化新增 ──
	SourceID    string  `json:"source_id"`   // 信号源 id（xt_signal_sources）
	MarginMode  string  `json:"margin_mode"` // cross | isolated | hedge
	TP1         float64 `json:"tp1"`         // 阶梯止盈三档价格（0=未指定）
	TP2         float64 `json:"tp2"`
	TP3         float64 `json:"tp3"`
	TP1Pct      float64 `json:"tp1_pct"` // 各档分批平仓比例
	TP2Pct      float64 `json:"tp2_pct"`
	TP3Pct      float64 `json:"tp3_pct"`
	RealizedPnL float64 `json:"realized_pnl"` // 整条信号最终盈亏（CLOSED 时回填）
	ClosedAt    int64   `json:"closed_at"`
}

type SignalRepo struct{ mu sync.RWMutex }

func NewSignalRepo() *SignalRepo { return &SignalRepo{} }

const signalCols = `id, symbol, direction, strength, strategy, reason, entry_price, stop_loss, take_profit, position_size, status, executed_order_id, created_at, source_id, margin_mode, tp1, tp2, tp3, tp1_pct, tp2_pct, tp3_pct, realized_pnl, closed_at`

func (r *SignalRepo) Create(s *SignalRecord) error {
	if s.Status == "" {
		s.Status = "PENDING"
	}
	if s.CreatedAt == 0 {
		s.CreatedAt = time.Now().UnixMilli()
	}
	res, err := db.Exec(
		`INSERT INTO xt_signals (symbol, direction, strength, strategy, reason, entry_price, stop_loss, take_profit, position_size, status, executed_order_id, created_at, source_id, margin_mode, tp1, tp2, tp3, tp1_pct, tp2_pct, tp3_pct, realized_pnl, closed_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.Symbol, s.Direction, s.Strength, s.Strategy, s.Reason, s.EntryPrice, s.StopLoss, s.TakeProfit, s.PositionSize, s.Status, s.ExecutedOrderID, s.CreatedAt,
		s.SourceID, s.MarginMode, s.TP1, s.TP2, s.TP3, s.TP1Pct, s.TP2Pct, s.TP3Pct, s.RealizedPnL, s.ClosedAt,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	s.ID = int(id)
	return nil
}

func scanSignal(row interface{ Scan(...any) error }) (*SignalRecord, error) {
	var s SignalRecord
	err := row.Scan(&s.ID, &s.Symbol, &s.Direction, &s.Strength, &s.Strategy, &s.Reason, &s.EntryPrice, &s.StopLoss, &s.TakeProfit, &s.PositionSize, &s.Status, &s.ExecutedOrderID, &s.CreatedAt,
		&s.SourceID, &s.MarginMode, &s.TP1, &s.TP2, &s.TP3, &s.TP1Pct, &s.TP2Pct, &s.TP3Pct, &s.RealizedPnL, &s.ClosedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SignalRepo) GetByID(id string) (*SignalRecord, error) {
	return scanSignal(db.QueryRow(`SELECT `+signalCols+` FROM xt_signals WHERE id=?`, id))
}

func (r *SignalRepo) List(filter map[string]any, limit int) ([]*SignalRecord, error) {
	query := "SELECT " + signalCols + " FROM xt_signals"
	allowedCols := map[string]bool{
		"id": true, "symbol": true, "direction": true, "strength": true, "strategy": true,
		"status": true, "executed_order_id": true, "created_at": true, "source_id": true,
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
	var result []*SignalRecord
	for rows.Next() {
		s, err := scanSignal(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

// ListSince 返回 created_at >= sinceMs 的信号（时间升序，供统计曲线聚合）。
func (r *SignalRepo) ListSince(sinceMs int64, limit int) ([]*SignalRecord, error) {
	query := `SELECT ` + signalCols + ` FROM xt_signals WHERE created_at>=? ORDER BY created_at ASC`
	args := []any{sinceMs}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*SignalRecord
	for rows.Next() {
		s, err := scanSignal(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

// CountByStatusSince 统计 sinceMs 之后各状态的信号数（status 传空统计全部）。
func (r *SignalRepo) CountByStatusSince(status string, sinceMs int64) int {
	var n int
	if status == "" {
		_ = db.QueryRow(`SELECT COUNT(1) FROM xt_signals WHERE created_at>=?`, sinceMs).Scan(&n)
	} else {
		_ = db.QueryRow(`SELECT COUNT(1) FROM xt_signals WHERE status=? AND created_at>=?`, status, sinceMs).Scan(&n)
	}
	return n
}

// CountBySourceSince 统计某信号源 sinceMs 之后的信号数（source_id 为空统计全部）。
func (r *SignalRepo) CountBySourceSince(sourceID string, sinceMs int64) int {
	var n int
	if sourceID == "" {
		_ = db.QueryRow(`SELECT COUNT(1) FROM xt_signals WHERE created_at>=?`, sinceMs).Scan(&n)
	} else {
		_ = db.QueryRow(`SELECT COUNT(1) FROM xt_signals WHERE source_id=? AND created_at>=?`, sourceID, sinceMs).Scan(&n)
	}
	return n
}

func (r *SignalRepo) Update(s *SignalRecord) error {
	_, err := db.Exec(
		`UPDATE xt_signals SET status=?, executed_order_id=?, realized_pnl=?, closed_at=? WHERE id=?`,
		s.Status, s.ExecutedOrderID, s.RealizedPnL, s.ClosedAt, s.ID,
	)
	return err
}

func (r *SignalRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_signals WHERE id=?", id)
	return err
}
