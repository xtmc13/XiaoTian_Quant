package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// StrategyTemplateRecord is the SQLite-backed representation of a strategy template.
type StrategyTemplateRecord struct {
	ID               string `json:"id"`
	UserID           int64  `json:"user_id"`
	Name             string `json:"name"`
	Category         string `json:"category"`
	StrategyType     string `json:"strategy_type"`
	Description      string `json:"description"`
	DefaultConfigJSON string `json:"default_config_json"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
}

// StrategyTemplateRepo provides typed CRUD for strategy_templates.
type StrategyTemplateRepo struct {
	mu sync.RWMutex
}

func NewStrategyTemplateRepo() *StrategyTemplateRepo { return &StrategyTemplateRepo{} }

// Create inserts a new strategy template.
func (r *StrategyTemplateRepo) Create(item *StrategyTemplateRecord) error {
	if item.ID == "" {
		item.ID = generateShortID()
	}
	now := time.Now().UnixMilli()
	if item.CreatedAt == 0 {
		item.CreatedAt = now
	}
	if item.UpdatedAt == 0 {
		item.UpdatedAt = now
	}
	if item.DefaultConfigJSON == "" {
		item.DefaultConfigJSON = "{}"
	}
	_, err := db.Exec(`INSERT INTO strategy_templates (
		id, user_id, name, category, strategy_type, description, default_config_json, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?)`,
		item.ID, item.UserID, item.Name, item.Category, item.StrategyType, item.Description,
		item.DefaultConfigJSON, item.CreatedAt, item.UpdatedAt,
	)
	return err
}

// GetByID returns a template by ID.
func (r *StrategyTemplateRepo) GetByID(id string) (*StrategyTemplateRecord, error) {
	row := db.QueryRow(`SELECT id, user_id, name, category, strategy_type, description,
		default_config_json, created_at, updated_at FROM strategy_templates WHERE id=?`, id)
	return scanStrategyTemplate(row)
}

// List returns templates for a user, optionally filtered by category.
func (r *StrategyTemplateRepo) List(userID int64, category string, limit int) ([]*StrategyTemplateRecord, error) {
	query := `SELECT id, user_id, name, category, strategy_type, description,
		default_config_json, created_at, updated_at FROM strategy_templates WHERE user_id=?`
	args := []any{userID}
	if category != "" {
		query += " AND category=?"
		args = append(args, category)
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

	var result []*StrategyTemplateRecord
	for rows.Next() {
		item, err := scanStrategyTemplate(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

// Delete removes a template if it belongs to the user.
func (r *StrategyTemplateRepo) Delete(id string, userID int64) (bool, error) {
	res, err := db.Exec("DELETE FROM strategy_templates WHERE id=? AND user_id=?", id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MigrateFromJSON imports legacy JSON-file templates into the DB with the given userID.
func (r *StrategyTemplateRepo) MigrateFromJSON(path string, userID int64) error {
	data, err := readFileIfExists(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	var rawItems []map[string]any
	if err := json.Unmarshal(data, &rawItems); err != nil {
		return fmt.Errorf("migrate strategy templates: %w", err)
	}
	for _, m := range rawItems {
		rec := StrategyTemplateRecordFromMap(m)
		if rec.UserID == 0 {
			rec.UserID = userID
		}
		if err := r.Create(rec); err != nil {
			return err
		}
	}
	return nil
}

// StrategyTemplateRecordFromMap builds a record from the legacy in-memory map.
func StrategyTemplateRecordFromMap(m map[string]any) *StrategyTemplateRecord {
	r := &StrategyTemplateRecord{
		ID:                getString(m, "id", ""),
		UserID:            getInt64(m, "user_id", 0),
		Name:              getString(m, "name", ""),
		Category:          getString(m, "category", "spot"),
		StrategyType:      getString(m, "strategy_type", ""),
		Description:       getString(m, "description", ""),
		DefaultConfigJSON: normalizeConfigJSON(m["default_config"]),
		CreatedAt:         getTimestampMillis(m, "created_at"),
		UpdatedAt:         getTimestampMillis(m, "updated_at"),
	}
	if r.DefaultConfigJSON == "" {
		r.DefaultConfigJSON = "{}"
	}
	return r
}

// ToMap converts the record to the frontend-facing map shape.
func (r *StrategyTemplateRecord) ToMap() map[string]any {
	m := map[string]any{
		"id":              r.ID,
		"user_id":         r.UserID,
		"name":            r.Name,
		"category":        r.Category,
		"strategy_type":   r.StrategyType,
		"description":     r.Description,
		"default_config":  json.RawMessage(r.DefaultConfigJSON),
		"created_at":      float64(r.CreatedAt),
		"updated_at":      float64(r.UpdatedAt),
	}
	return m
}

func scanStrategyTemplate(scanner interface {
	Scan(dest ...any) error
}) (*StrategyTemplateRecord, error) {
	var r StrategyTemplateRecord
	err := scanner.Scan(
		&r.ID, &r.UserID, &r.Name, &r.Category, &r.StrategyType, &r.Description,
		&r.DefaultConfigJSON, &r.CreatedAt, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
