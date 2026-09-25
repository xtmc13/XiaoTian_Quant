package store

import (
	"database/sql"
	"sync"
	"time"
)

// ── 阶梯智能单 Repository ──
// 表结构见 migrations/sql/0030_ladder_orders.sql。
// spec_json/state_json 由 order 包的阶梯引擎编解码，store 层只做透传。

// LadderOrderRecord 是 xt_ladder_orders 的行记录。
type LadderOrderRecord struct {
	ID        string `json:"id"`
	UserID    int64  `json:"user_id"`
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`
	Exchange  string `json:"exchange"`
	Status    string `json:"status"`
	SpecJSON  string `json:"spec_json"`
	StateJSON string `json:"state_json"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// LadderOrderRepo provides typed CRUD for xt_ladder_orders。
type LadderOrderRepo struct{ mu sync.RWMutex }

func NewLadderOrderRepo() *LadderOrderRepo { return &LadderOrderRepo{} }

const ladderOrderColumns = `id, user_id, symbol, side, exchange, status, spec_json, state_json, created_at, updated_at`

func scanLadderOrder(s rowScanner) (*LadderOrderRecord, error) {
	var rec LadderOrderRecord
	err := s.Scan(&rec.ID, &rec.UserID, &rec.Symbol, &rec.Side, &rec.Exchange,
		&rec.Status, &rec.SpecJSON, &rec.StateJSON, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// Upsert 首写插入、状态翻转更新（与 OrderRepo.Upsert 同口径），自动刷新 updated_at。
func (r *LadderOrderRepo) Upsert(rec *LadderOrderRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	_, err := db.Exec(`INSERT INTO xt_ladder_orders (`+ladderOrderColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, spec_json=excluded.spec_json,
			state_json=excluded.state_json, updated_at=excluded.updated_at`,
		rec.ID, rec.UserID, rec.Symbol, rec.Side, rec.Exchange,
		rec.Status, rec.SpecJSON, rec.StateJSON, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// GetByID 按 id 取阶梯单；不存在返回 (nil, nil)。
func (r *LadderOrderRepo) GetByID(id string) (*LadderOrderRecord, error) {
	row := db.QueryRow(`SELECT `+ladderOrderColumns+` FROM xt_ladder_orders WHERE id = ?`, id)
	return scanLadderOrder(row)
}

// List 按过滤条件列表（user_id/symbol/status 为允许过滤列），按 updated_at 倒序。
func (r *LadderOrderRepo) List(filter map[string]any, limit int) ([]*LadderOrderRecord, error) {
	query := `SELECT ` + ladderOrderColumns + ` FROM xt_ladder_orders`
	allowedCols := map[string]bool{"user_id": true, "symbol": true, "status": true}
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
	var out []*LadderOrderRecord
	for rows.Next() {
		rec, err := scanLadderOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// ListActive 返回所有未终结的阶梯单（重启恢复扫描用）。
func (r *LadderOrderRepo) ListActive() ([]*LadderOrderRecord, error) {
	rows, err := db.Query(`SELECT ` + ladderOrderColumns + ` FROM xt_ladder_orders
		WHERE status IN ('active','stopping') ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*LadderOrderRecord
	for rows.Next() {
		rec, err := scanLadderOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}
