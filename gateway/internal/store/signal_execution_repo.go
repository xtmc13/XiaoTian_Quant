package store

import (
	"fmt"
	"sync"
	"time"
)

// ── Signal Execution Repository ──

// SignalExecution 单条信号的执行全过程记录（对齐 THREE_BOTS_DESIGN.md
// 的 ExecutionRecord）：入场 → TP1/TP2/TP3 阶梯止盈 → 止损/追踪，
// 每步推进都落库，executor 统计端点据此聚合成功率/达成率/盈亏曲线。
type SignalExecution struct {
	ID           string `json:"id"`
	SignalID     int    `json:"signal_id"`
	SourceID     string `json:"source_id"`
	Symbol       string `json:"symbol"`
	Direction    string `json:"direction"` // LONG | SHORT
	MarginMode   string `json:"margin_mode"`
	PositionSide string `json:"position_side"` // LONG | SHORT（hedge 双腿标识）
	PositionID   string `json:"position_id"`   // 组合持仓 id（symbol-side）
	Status       string `json:"status"`        // pending | active | closed

	// 入场
	EntryOrderID string  `json:"entry_order_id"`
	EntryPrice   float64 `json:"entry_price"`
	EntryQty     float64 `json:"entry_qty"`
	EntryTime    int64   `json:"entry_time"`

	// 阶梯止盈（每档：价格 / 平仓数量 / 触发时间 / 是否已触发）
	TP1Price  float64 `json:"tp1_price"`
	TP1Qty    float64 `json:"tp1_qty"`
	TP1Time   int64   `json:"tp1_time"`
	TP1Filled bool    `json:"tp1_filled"`
	TP2Price  float64 `json:"tp2_price"`
	TP2Qty    float64 `json:"tp2_qty"`
	TP2Time   int64   `json:"tp2_time"`
	TP2Filled bool    `json:"tp2_filled"`
	TP3Price  float64 `json:"tp3_price"`
	TP3Qty    float64 `json:"tp3_qty"`
	TP3Time   int64   `json:"tp3_time"`
	TP3Filled bool    `json:"tp3_filled"`

	// 止损与移动止损（TP1 触发后把 SL 上移/下移到 move_sl_to）
	SLPrice     float64 `json:"sl_price"`
	SLTriggered bool    `json:"sl_triggered"`
	SLTime      int64   `json:"sl_time"`
	MoveSLAfter float64 `json:"move_sl_after"`
	MoveSLTo    float64 `json:"move_sl_to"`

	// 追踪止盈（trailing_pct 回撤比例，trailing_peak 触发 TP1 后的最高价）
	TrailingActive bool    `json:"trailing_active"`
	TrailingPct    float64 `json:"trailing_pct"`
	TrailingPeak   float64 `json:"trailing_peak"`

	// 当前有效 TP/SL 与剩余持仓
	CurrentTP    float64 `json:"current_tp"`
	CurrentSL    float64 `json:"current_sl"`
	RemainingQty float64 `json:"remaining_qty"`

	// 结果
	RealizedPnL float64 `json:"realized_pnl"`
	CloseReason string  `json:"close_reason"` // tp1 | tp2 | tp3 | sl | manual
	ClosedAt    int64   `json:"closed_at"`
	CreatedAt   int64   `json:"created_at"`
}

type SignalExecutionRepo struct{ mu sync.RWMutex }

func NewSignalExecutionRepo() *SignalExecutionRepo { return &SignalExecutionRepo{} }

const sigExecCols = `id, signal_id, source_id, symbol, direction, margin_mode, position_side, position_id, status, entry_order_id, entry_price, entry_qty, entry_time, tp1_price, tp1_qty, tp1_time, tp1_filled, tp2_price, tp2_qty, tp2_time, tp2_filled, tp3_price, tp3_qty, tp3_time, tp3_filled, sl_price, sl_triggered, sl_time, move_sl_after, move_sl_to, trailing_active, trailing_pct, trailing_peak, current_tp, current_sl, remaining_qty, realized_pnl, close_reason, closed_at, created_at`

func scanSignalExecution(row interface{ Scan(...any) error }) (*SignalExecution, error) {
	var e SignalExecution
	err := row.Scan(&e.ID, &e.SignalID, &e.SourceID, &e.Symbol, &e.Direction, &e.MarginMode, &e.PositionSide, &e.PositionID, &e.Status, &e.EntryOrderID, &e.EntryPrice, &e.EntryQty, &e.EntryTime,
		&e.TP1Price, &e.TP1Qty, &e.TP1Time, &e.TP1Filled, &e.TP2Price, &e.TP2Qty, &e.TP2Time, &e.TP2Filled,
		&e.TP3Price, &e.TP3Qty, &e.TP3Time, &e.TP3Filled, &e.SLPrice, &e.SLTriggered, &e.SLTime,
		&e.MoveSLAfter, &e.MoveSLTo, &e.TrailingActive, &e.TrailingPct, &e.TrailingPeak,
		&e.CurrentTP, &e.CurrentSL, &e.RemainingQty, &e.RealizedPnL, &e.CloseReason, &e.ClosedAt, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *SignalExecutionRepo) Create(e *SignalExecution) error {
	if e.ID == "" {
		e.ID = fmt.Sprintf("sigexec_%d", time.Now().UnixNano())
	}
	if e.CreatedAt == 0 {
		e.CreatedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(
		`INSERT INTO xt_signal_executions (`+sigExecCols+`)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.SignalID, e.SourceID, e.Symbol, e.Direction, e.MarginMode, e.PositionSide, e.PositionID, e.Status, e.EntryOrderID, e.EntryPrice, e.EntryQty, e.EntryTime,
		e.TP1Price, e.TP1Qty, e.TP1Time, e.TP1Filled, e.TP2Price, e.TP2Qty, e.TP2Time, e.TP2Filled,
		e.TP3Price, e.TP3Qty, e.TP3Time, e.TP3Filled, e.SLPrice, e.SLTriggered, e.SLTime,
		e.MoveSLAfter, e.MoveSLTo, e.TrailingActive, e.TrailingPct, e.TrailingPeak,
		e.CurrentTP, e.CurrentSL, e.RemainingQty, e.RealizedPnL, e.CloseReason, e.ClosedAt, e.CreatedAt,
	)
	return err
}

func (r *SignalExecutionRepo) GetByID(id string) (*SignalExecution, error) {
	return scanSignalExecution(db.QueryRow(`SELECT `+sigExecCols+` FROM xt_signal_executions WHERE id=?`, id))
}

func (r *SignalExecutionRepo) List(filter map[string]any, limit int) ([]*SignalExecution, error) {
	query := `SELECT ` + sigExecCols + ` FROM xt_signal_executions`
	allowedCols := map[string]bool{
		"id": true, "signal_id": true, "source_id": true, "symbol": true, "direction": true,
		"margin_mode": true, "position_side": true, "position_id": true, "status": true,
		"close_reason": true, "created_at": true, "closed_at": true,
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
	var result []*SignalExecution
	for rows.Next() {
		e, err := scanSignalExecution(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

// ListActive 返回未平仓的执行记录（阶梯 TP/SL 监控对象），按创建时间升序。
func (r *SignalExecutionRepo) ListActive() ([]*SignalExecution, error) {
	rows, err := db.Query(`SELECT ` + sigExecCols + ` FROM xt_signal_executions WHERE status='active' ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*SignalExecution
	for rows.Next() {
		e, err := scanSignalExecution(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

// ListSince 返回 created_at >= sinceMs 的执行记录（时间升序，供统计聚合）。
func (r *SignalExecutionRepo) ListSince(sinceMs int64, limit int) ([]*SignalExecution, error) {
	query := `SELECT ` + sigExecCols + ` FROM xt_signal_executions WHERE created_at>=? ORDER BY created_at ASC`
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
	var result []*SignalExecution
	for rows.Next() {
		e, err := scanSignalExecution(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

// Update 全量回填执行记录（状态机每步推进后调用）。
func (r *SignalExecutionRepo) Update(e *SignalExecution) error {
	_, err := db.Exec(
		`UPDATE xt_signal_executions SET status=?, entry_order_id=?, entry_price=?, entry_qty=?, entry_time=?,
		 tp1_price=?, tp1_qty=?, tp1_time=?, tp1_filled=?, tp2_price=?, tp2_qty=?, tp2_time=?, tp2_filled=?,
		 tp3_price=?, tp3_qty=?, tp3_time=?, tp3_filled=?, sl_price=?, sl_triggered=?, sl_time=?,
		 move_sl_after=?, move_sl_to=?, trailing_active=?, trailing_pct=?, trailing_peak=?,
		 current_tp=?, current_sl=?, remaining_qty=?, realized_pnl=?, close_reason=?, closed_at=?
		 WHERE id=?`,
		e.Status, e.EntryOrderID, e.EntryPrice, e.EntryQty, e.EntryTime,
		e.TP1Price, e.TP1Qty, e.TP1Time, e.TP1Filled, e.TP2Price, e.TP2Qty, e.TP2Time, e.TP2Filled,
		e.TP3Price, e.TP3Qty, e.TP3Time, e.TP3Filled, e.SLPrice, e.SLTriggered, e.SLTime,
		e.MoveSLAfter, e.MoveSLTo, e.TrailingActive, e.TrailingPct, e.TrailingPeak,
		e.CurrentTP, e.CurrentSL, e.RemainingQty, e.RealizedPnL, e.CloseReason, e.ClosedAt, e.ID,
	)
	return err
}

func (r *SignalExecutionRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_signal_executions WHERE id=?", id)
	return err
}
