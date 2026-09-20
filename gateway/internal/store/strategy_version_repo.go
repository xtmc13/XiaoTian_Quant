package store

import (
	"database/sql"
	"sync"
	"time"
)

// ── 策略配置版本快照 Repository ──
// 表结构见 migrations/sql/0014_strategy_versions.sql。
// 快照不可变：没有 Update/Delete，恢复 = 把历史 payload 写回 strategy_configs
// 并追加一条新版本（见 handler/strategy_version.go）。

type StrategyVersionRecord struct {
	ID         string `json:"id"`
	UserID     int64  `json:"user_id"`
	StrategyID string `json:"strategy_id"`
	Version    int    `json:"version"`
	Payload    string `json:"payload"`
	Note       string `json:"note"`
	CreatedAt  int64  `json:"created_at"`
}

type StrategyVersionRepo struct{ mu sync.Mutex }

func NewStrategyVersionRepo() *StrategyVersionRepo { return &StrategyVersionRepo{} }

// Create 打一条版本快照：version 取同策略 max(version)+1（首次为 1）。
// 记录创建后 rec.Version 为分配的版本号。
func (r *StrategyVersionRepo) Create(rec *StrategyVersionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec.ID == "" {
		rec.ID = generateShortID()
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	var maxVersion int
	if err := db.QueryRow(
		`SELECT COALESCE(MAX(version), 0) FROM xt_strategy_versions WHERE strategy_id = ?`,
		rec.StrategyID,
	).Scan(&maxVersion); err != nil {
		return err
	}
	rec.Version = maxVersion + 1
	_, err := db.Exec(
		`INSERT INTO xt_strategy_versions (id, user_id, strategy_id, version, payload, note, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.StrategyID, rec.Version, rec.Payload, rec.Note, rec.CreatedAt,
	)
	return err
}

const strategyVersionColumns = `id, user_id, strategy_id, version, payload, note, created_at`

func (r *StrategyVersionRepo) scan(s rowScanner) (*StrategyVersionRecord, error) {
	var rec StrategyVersionRecord
	err := s.Scan(&rec.ID, &rec.UserID, &rec.StrategyID, &rec.Version, &rec.Payload, &rec.Note, &rec.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// ListByStrategy 返回该策略的全部版本，version 倒序（最新在前）。
func (r *StrategyVersionRepo) ListByStrategy(strategyID string) ([]*StrategyVersionRecord, error) {
	rows, err := db.Query(
		`SELECT `+strategyVersionColumns+` FROM xt_strategy_versions WHERE strategy_id = ? ORDER BY version DESC`,
		strategyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*StrategyVersionRecord
	for rows.Next() {
		rec, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// Get 取该策略的指定版本号；不存在返回 (nil, nil)。
func (r *StrategyVersionRepo) Get(strategyID string, version int) (*StrategyVersionRecord, error) {
	row := db.QueryRow(
		`SELECT `+strategyVersionColumns+` FROM xt_strategy_versions WHERE strategy_id = ? AND version = ?`,
		strategyID, version,
	)
	rec, err := r.scan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rec, err
}

// GetLatest 取该策略最新版本；一条都没有返回 (nil, nil)。
func (r *StrategyVersionRepo) GetLatest(strategyID string) (*StrategyVersionRecord, error) {
	row := db.QueryRow(
		`SELECT `+strategyVersionColumns+` FROM xt_strategy_versions WHERE strategy_id = ? ORDER BY version DESC LIMIT 1`,
		strategyID,
	)
	rec, err := r.scan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rec, err
}

// FindByID 按版本记录 id 取单条（属主/归属校验由 handler 做）。
func (r *StrategyVersionRepo) FindByID(id string) (*StrategyVersionRecord, error) {
	row := db.QueryRow(
		`SELECT `+strategyVersionColumns+` FROM xt_strategy_versions WHERE id = ?`, id,
	)
	rec, err := r.scan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rec, err
}
