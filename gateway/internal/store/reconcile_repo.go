package store

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"
)

// ── Reconcile Repository（A8 对账体系）────────────────────────
//
// 四张表的读写入口：
//   - reconcile_diffs       持仓/资金费对账差异
//   - reconcile_deviations  实盘偏差（滑点/未成交超时）
//   - reconcile_funding     资金费流水本地镜像（唯一键天然去重）
//   - reconcile_settings    配置覆盖（键值）

// ReconcileDiff 一条对账差异。
type ReconcileDiff struct {
	ID              int64   `json:"id"`
	UserID          int64   `json:"user_id"`
	Exchange        string  `json:"exchange"`
	Symbol          string  `json:"symbol"`
	DiffType        string  `json:"diff_type"` // position_quantity|position_missing_local|position_missing_exchange|funding
	LocalQty        float64 `json:"local_qty"`
	ExchangeQty     float64 `json:"exchange_qty"`
	LocalEntryPrice float64 `json:"local_entry_price"`
	ExchangeEntryPx float64 `json:"exchange_entry_price"`
	Asset           string  `json:"asset"`
	Amount          float64 `json:"amount"`
	Detail          string  `json:"detail"`
	Status          string  `json:"status"` // open|resolved
	Resolution      string  `json:"resolution"`
	ResolvedBy      string  `json:"resolved_by"`
	CreatedAt       int64   `json:"created_at"`
	ResolvedAt      int64   `json:"resolved_at"`
}

// ReconcileDeviation 一条实盘偏差记录。
type ReconcileDeviation struct {
	ID            int64   `json:"id"`
	OrderID       string  `json:"order_id"`
	UserID        int64   `json:"user_id"`
	Symbol        string  `json:"symbol"`
	Exchange      string  `json:"exchange"`
	Kind          string  `json:"kind"` // slippage|stuck
	ExpectedPrice float64 `json:"expected_price"`
	AvgPrice      float64 `json:"avg_price"`
	SlippagePct   float64 `json:"slippage_pct"`
	Detail        string  `json:"detail"`
	Status        string  `json:"status"`
	ResolvedBy    string  `json:"resolved_by"`
	CreatedAt     int64   `json:"created_at"`
	ResolvedAt    int64   `json:"resolved_at"`
}

// ReconcileFundingRecord 资金费流水镜像行。
type ReconcileFundingRecord struct {
	ID         int64   `json:"id"`
	Exchange   string  `json:"exchange"`
	Symbol     string  `json:"symbol"`
	IncomeType string  `json:"income_type"`
	Asset      string  `json:"asset"`
	Amount     float64 `json:"amount"`
	IncomeTime int64   `json:"income_time"`
	Extra      string  `json:"extra"`
	Notified   bool    `json:"notified"`
	CreatedAt  int64   `json:"created_at"`
}

// ReconcileAuditRecord 自动修正审计日志行。
type ReconcileAuditRecord struct {
	ID        int64  `json:"id"`
	Action    string `json:"action"`
	Exchange  string `json:"exchange"`
	Symbol    string `json:"symbol"`
	UserID    int64  `json:"user_id"`
	Detail    string `json:"detail"`
	CreatedAt int64  `json:"created_at"`
}

var errReconcileNoRows = errors.New("reconcile record not found")

type ReconcileRepo struct{ mu sync.Mutex }

func NewReconcileRepo() *ReconcileRepo { return &ReconcileRepo{} }

const reconcileDiffCols = `id, user_id, exchange, symbol, diff_type, local_qty, exchange_qty, local_entry_price, exchange_entry_price, asset, amount, detail, status, resolution, resolved_by, created_at, resolved_at`

func scanReconcileDiff(row interface{ Scan(...any) error }) (*ReconcileDiff, error) {
	var d ReconcileDiff
	err := row.Scan(&d.ID, &d.UserID, &d.Exchange, &d.Symbol, &d.DiffType, &d.LocalQty, &d.ExchangeQty,
		&d.LocalEntryPrice, &d.ExchangeEntryPx, &d.Asset, &d.Amount, &d.Detail, &d.Status,
		&d.Resolution, &d.ResolvedBy, &d.CreatedAt, &d.ResolvedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// CreateDiff 落一条差异。同一 (exchange,symbol,diff_type) 的 open 差异已存在时
// 更新数量与 detail（避免漂移持续期间重复刷通知），返回 (id, created, err)：
// created=false 表示复用了已有 open 差异。
func (r *ReconcileRepo) CreateDiff(d *ReconcileDiff) (int64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var existingID int64
	err := db.QueryRow(`SELECT id FROM reconcile_diffs
		WHERE exchange=? AND symbol=? AND diff_type=? AND asset=? AND status='open'`,
		d.Exchange, d.Symbol, d.DiffType, d.Asset).Scan(&existingID)
	if err == nil {
		_, uerr := db.Exec(`UPDATE reconcile_diffs SET local_qty=?, exchange_qty=?, local_entry_price=?,
			exchange_entry_price=?, amount=?, detail=?, created_at=? WHERE id=?`,
			d.LocalQty, d.ExchangeQty, d.LocalEntryPrice, d.ExchangeEntryPx,
			d.Amount, d.Detail, time.Now().UnixMilli(), existingID)
		return existingID, false, uerr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	if d.CreatedAt == 0 {
		d.CreatedAt = time.Now().UnixMilli()
	}
	res, err := db.Exec(`INSERT INTO reconcile_diffs
		(user_id, exchange, symbol, diff_type, local_qty, exchange_qty, local_entry_price, exchange_entry_price, asset, amount, detail, status, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,'open',?)`,
		d.UserID, d.Exchange, d.Symbol, d.DiffType, d.LocalQty, d.ExchangeQty,
		d.LocalEntryPrice, d.ExchangeEntryPx, d.Asset, d.Amount, d.Detail, d.CreatedAt)
	if err != nil {
		return 0, false, err
	}
	id, _ := res.LastInsertId()
	return id, true, nil
}

// ResolveDiff 人工解决差异：仅 open 可流转，带并发保护。
func (r *ReconcileRepo) ResolveDiff(id int64, resolution, resolvedBy string) (*ReconcileDiff, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, err := db.Exec(`UPDATE reconcile_diffs SET status='resolved', resolution=?, resolved_by=?, resolved_at=?
		WHERE id=? AND status='open'`, resolution, resolvedBy, time.Now().UnixMilli(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errReconcileNoRows
	}
	return r.getDiff(id)
}

func (r *ReconcileRepo) getDiff(id int64) (*ReconcileDiff, error) {
	return scanReconcileDiff(db.QueryRow(`SELECT `+reconcileDiffCols+` FROM reconcile_diffs WHERE id=?`, id))
}

// GetDiff 按 id 查差异。
func (r *ReconcileRepo) GetDiff(id int64) (*ReconcileDiff, error) {
	return r.getDiff(id)
}

// ListDiffs 列差异：status/diffType/exchange 过滤，userID>0 时只取该用户（+无属主）。
func (r *ReconcileRepo) ListDiffs(userID int64, status, diffType, exchange string, limit, offset int) ([]*ReconcileDiff, error) {
	query := `SELECT ` + reconcileDiffCols + ` FROM reconcile_diffs WHERE 1=1`
	var args []any
	if userID > 0 {
		query += ` AND (user_id=? OR user_id=0)`
		args = append(args, userID)
	}
	if status != "" {
		query += ` AND status=?`
		args = append(args, status)
	}
	if diffType != "" {
		query += ` AND diff_type=?`
		args = append(args, diffType)
	}
	if exchange != "" {
		query += ` AND exchange=?`
		args = append(args, exchange)
	}
	query += ` ORDER BY created_at DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
		if offset > 0 {
			query += ` OFFSET ?`
			args = append(args, offset)
		}
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReconcileDiff
	for rows.Next() {
		d, err := scanReconcileDiff(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// CountOpenDiffs 未解决差异数（status 概览用）。
func (r *ReconcileRepo) CountOpenDiffs() (int64, error) {
	var n int64
	err := db.QueryRow(`SELECT COUNT(*) FROM reconcile_diffs WHERE status='open'`).Scan(&n)
	return n, err
}

// ── Deviations ──

const reconcileDevCols = `id, order_id, user_id, symbol, exchange, kind, expected_price, avg_price, slippage_pct, detail, status, resolved_by, created_at, resolved_at`

func scanReconcileDeviation(row interface{ Scan(...any) error }) (*ReconcileDeviation, error) {
	var d ReconcileDeviation
	err := row.Scan(&d.ID, &d.OrderID, &d.UserID, &d.Symbol, &d.Exchange, &d.Kind,
		&d.ExpectedPrice, &d.AvgPrice, &d.SlippagePct, &d.Detail, &d.Status,
		&d.ResolvedBy, &d.CreatedAt, &d.ResolvedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// CreateDeviation 记录一条偏差；(order_id,kind) 唯一索引冲突视为已记录（幂等告警）。
// 返回 (id, created, err)。
func (r *ReconcileRepo) CreateDeviation(d *ReconcileDeviation) (int64, bool, error) {
	if d.CreatedAt == 0 {
		d.CreatedAt = time.Now().UnixMilli()
	}
	res, err := db.Exec(`INSERT INTO reconcile_deviations
		(order_id, user_id, symbol, exchange, kind, expected_price, avg_price, slippage_pct, detail, status, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,'open',?)`,
		d.OrderID, d.UserID, d.Symbol, d.Exchange, d.Kind, d.ExpectedPrice, d.AvgPrice,
		d.SlippagePct, d.Detail, d.CreatedAt)
	if err != nil {
		// 唯一键冲突 → 已告警过，直接忽略（不报错）。
		if isUniqueConflict(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	id, _ := res.LastInsertId()
	return id, true, nil
}

// ResolveDeviation 人工确认一条偏差。
func (r *ReconcileRepo) ResolveDeviation(id int64, resolvedBy string) (*ReconcileDeviation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, err := db.Exec(`UPDATE reconcile_deviations SET status='resolved', resolved_by=?, resolved_at=?
		WHERE id=? AND status='open'`, resolvedBy, time.Now().UnixMilli(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errReconcileNoRows
	}
	return scanReconcileDeviation(db.QueryRow(`SELECT `+reconcileDevCols+` FROM reconcile_deviations WHERE id=?`, id))
}

// GetDeviation 按 id 查偏差。
func (r *ReconcileRepo) GetDeviation(id int64) (*ReconcileDeviation, error) {
	return scanReconcileDeviation(db.QueryRow(`SELECT `+reconcileDevCols+` FROM reconcile_deviations WHERE id=?`, id))
}

// ListDeviations 列偏差：kind/status/exchange 过滤，userID>0 时只取该用户（+无属主）。
func (r *ReconcileRepo) ListDeviations(userID int64, kind, status, exchange string, limit, offset int) ([]*ReconcileDeviation, error) {
	query := `SELECT ` + reconcileDevCols + ` FROM reconcile_deviations WHERE 1=1`
	var args []any
	if userID > 0 {
		query += ` AND (user_id=? OR user_id=0)`
		args = append(args, userID)
	}
	if kind != "" {
		query += ` AND kind=?`
		args = append(args, kind)
	}
	if status != "" {
		query += ` AND status=?`
		args = append(args, status)
	}
	if exchange != "" {
		query += ` AND exchange=?`
		args = append(args, exchange)
	}
	query += ` ORDER BY created_at DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
		if offset > 0 {
			query += ` OFFSET ?`
			args = append(args, offset)
		}
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReconcileDeviation
	for rows.Next() {
		d, err := scanReconcileDeviation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// ── Funding mirror ──

// InsertFundingMirror 镜像一行资金费流水；唯一键 (exchange,symbol,income_type,income_time,amount)
// 冲突表示本地已记录过。返回 (inserted, err)。
func (r *ReconcileRepo) InsertFundingMirror(rec *ReconcileFundingRecord) (bool, error) {
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	res, err := db.Exec(`INSERT OR IGNORE INTO reconcile_funding
		(exchange, symbol, income_type, asset, amount, income_time, extra, notified, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		rec.Exchange, rec.Symbol, rec.IncomeType, rec.Asset, rec.Amount, rec.IncomeTime, rec.Extra, 0, rec.CreatedAt)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkFundingNotified 标记镜像行已通知。
func (r *ReconcileRepo) MarkFundingNotified(id int64) error {
	_, err := db.Exec(`UPDATE reconcile_funding SET notified=1 WHERE id=?`, id)
	return err
}

// ListFundingMirror 列资金费镜像（管理/调试用）。
func (r *ReconcileRepo) ListFundingMirror(exchange string, limit int) ([]*ReconcileFundingRecord, error) {
	query := `SELECT id, exchange, symbol, income_type, asset, amount, income_time, extra, notified, created_at FROM reconcile_funding`
	var args []any
	if exchange != "" {
		query += ` WHERE exchange=?`
		args = append(args, exchange)
	}
	query += ` ORDER BY income_time DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReconcileFundingRecord
	for rows.Next() {
		var f ReconcileFundingRecord
		var notified int
		if err := rows.Scan(&f.ID, &f.Exchange, &f.Symbol, &f.IncomeType, &f.Asset, &f.Amount, &f.IncomeTime, &f.Extra, &notified, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.Notified = notified != 0
		out = append(out, &f)
	}
	return out, nil
}

// ── Settings ──

// GetSetting 读配置值（不存在返回空串）。
func (r *ReconcileRepo) GetSetting(key string) string {
	var v string
	if err := db.QueryRow(`SELECT value FROM reconcile_settings WHERE key=?`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}

// SetSetting 写配置值（upsert）。
func (r *ReconcileRepo) SetSetting(key, value string) error {
	_, err := db.Exec(`INSERT INTO reconcile_settings (key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, time.Now().UnixMilli())
	return err
}

// ── Audit ──

// LogAudit 写一条自动修正审计日志。
func (r *ReconcileRepo) LogAudit(action, exchange, symbol string, userID int64, detail string) error {
	_, err := db.Exec(`INSERT INTO reconcile_audit (action, exchange, symbol, user_id, detail, created_at)
		VALUES (?,?,?,?,?,?)`, action, exchange, symbol, userID, detail, time.Now().UnixMilli())
	return err
}

// ListAudit 列最近审计日志（新→旧）。
func (r *ReconcileRepo) ListAudit(limit int) ([]*ReconcileAuditRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, action, exchange, symbol, user_id, detail, created_at
		FROM reconcile_audit ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReconcileAuditRecord
	for rows.Next() {
		var a ReconcileAuditRecord
		if err := rows.Scan(&a.ID, &a.Action, &a.Exchange, &a.Symbol, &a.UserID, &a.Detail, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, nil
}

// isUniqueConflict 判断 SQLite 唯一约束冲突（modernc.org/sqlite 错误信息含 "UNIQUE constraint failed"）。
func isUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
