package store

import (
	"database/sql"
	"time"
)

// ComboConfigRecord is the SQLite-backed representation of a strategy combo
// config (xt_combo_configs, migration 0056). MembersJSON 为
// strategy.ComboMember 数组 JSON——store 包不依赖 strategy 包，序列化/
// 反序列化由 handler 层完成（与 StrategyConfigRecord.ConfigJSON 同模式）。
type ComboConfigRecord struct {
	ID              string `json:"id"`
	UserID          int64  `json:"user_id"`
	Name            string `json:"name"`
	Symbol          string `json:"symbol"`
	MembersJSON     string `json:"members"`
	AggregationMode string `json:"aggregation_mode"`
	Status          string `json:"status"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

// ComboConfigRepo provides typed CRUD for xt_combo_configs.
type ComboConfigRepo struct{}

func NewComboConfigRepo() *ComboConfigRepo { return &ComboConfigRepo{} }

const comboConfigCols = `id, user_id, name, symbol, members, aggregation_mode, status, created_at, updated_at`

// Create inserts a new combo config. It assigns ID/timestamps if missing.
func (r *ComboConfigRepo) Create(rec *ComboConfigRecord) error {
	if rec.ID == "" {
		rec.ID = generateShortID()
	}
	now := time.Now().UnixMilli()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt == 0 {
		rec.UpdatedAt = now
	}
	if rec.Status == "" {
		rec.Status = "stopped"
	}
	if rec.AggregationMode == "" {
		rec.AggregationMode = "vote"
	}
	if rec.MembersJSON == "" {
		rec.MembersJSON = "[]"
	}
	_, err := db.Exec(`INSERT INTO xt_combo_configs (
		id, user_id, name, symbol, members, aggregation_mode, status, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.Name, rec.Symbol, rec.MembersJSON, rec.AggregationMode,
		rec.Status, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// GetByID returns a combo config by ID (nil, nil when missing).
func (r *ComboConfigRepo) GetByID(id string) (*ComboConfigRecord, error) {
	row := db.QueryRow(`SELECT `+comboConfigCols+` FROM xt_combo_configs WHERE id=?`, id)
	return scanComboConfig(row)
}

// List returns all combo configs (combos are few; no filter needed).
func (r *ComboConfigRepo) List() ([]*ComboConfigRecord, error) {
	rows, err := db.Query(`SELECT ` + comboConfigCols + ` FROM xt_combo_configs ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*ComboConfigRecord
	for rows.Next() {
		rec, err := scanComboConfig(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, rec)
	}
	return result, nil
}

// Update modifies an existing combo config.
func (r *ComboConfigRepo) Update(rec *ComboConfigRecord) error {
	rec.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(`UPDATE xt_combo_configs SET
		user_id=?, name=?, symbol=?, members=?, aggregation_mode=?, status=?, updated_at=?
		WHERE id=?`,
		rec.UserID, rec.Name, rec.Symbol, rec.MembersJSON, rec.AggregationMode,
		rec.Status, rec.UpdatedAt, rec.ID,
	)
	return err
}

// Delete removes a combo config.
func (r *ComboConfigRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM xt_combo_configs WHERE id=?", id)
	return err
}

// scanComboConfig scans a single row into a ComboConfigRecord.
func scanComboConfig(scanner interface {
	Scan(dest ...any) error
}) (*ComboConfigRecord, error) {
	var rec ComboConfigRecord
	err := scanner.Scan(
		&rec.ID, &rec.UserID, &rec.Name, &rec.Symbol, &rec.MembersJSON,
		&rec.AggregationMode, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}
