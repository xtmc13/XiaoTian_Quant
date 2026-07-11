package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// StrategyConfigRecord is the SQLite-backed representation of a strategy config.
type StrategyConfigRecord struct {
	ID                 string  `json:"id"`
	UserID             int64   `json:"user_id"`
	Name               string  `json:"name"`
	Category           string  `json:"category"`
	StrategyType       string  `json:"strategy_type"`
	Symbol             string  `json:"symbol"`
	Coin               string  `json:"coin"`
	Direction          string  `json:"direction"`
	Leverage           float64 `json:"leverage"`
	MarketType         string  `json:"market_type"`
	MarginMode         string  `json:"margin_mode"`
	Timeframe          string  `json:"timeframe"`
	ExecutionMode      string  `json:"execution_mode"`
	InitialCapital     float64 `json:"initial_capital"`
	CurrentEquity      float64 `json:"current_equity"`
	TotalPnL           float64 `json:"total_pnl"`
	TotalPnLPercent    float64 `json:"total_pnl_percent"`
	Status             string  `json:"status"`
	ConfigJSON         string  `json:"config_json"`
	NotificationConfig string  `json:"notification_config"`
	CreatedAt          int64   `json:"created_at"`
	UpdatedAt          int64   `json:"updated_at"`
}

// StrategyConfigRepo provides typed CRUD for strategy_configs.
type StrategyConfigRepo struct {
	mu sync.RWMutex
}

func NewStrategyConfigRepo() *StrategyConfigRepo { return &StrategyConfigRepo{} }

// Create inserts a new strategy config. It assigns ID/timestamps if missing.
func (r *StrategyConfigRepo) Create(item *StrategyConfigRecord) error {
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
	if item.Status == "" {
		item.Status = "stopped"
	}
	if item.NotificationConfig == "" {
		item.NotificationConfig = "{}"
	}
	_, err := db.Exec(`INSERT INTO strategy_configs (
		id, user_id, name, category, strategy_type, symbol, coin, direction, leverage,
		market_type, margin_mode, timeframe, execution_mode, initial_capital, current_equity,
		total_pnl, total_pnl_percent, status, config_json, notification_config, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.UserID, item.Name, item.Category, item.StrategyType, item.Symbol, item.Coin,
		item.Direction, item.Leverage, item.MarketType, item.MarginMode, item.Timeframe,
		item.ExecutionMode, item.InitialCapital, item.CurrentEquity, item.TotalPnL,
		item.TotalPnLPercent, item.Status, item.ConfigJSON, item.NotificationConfig,
		item.CreatedAt, item.UpdatedAt,
	)
	return err
}

// GetByID returns a strategy config by ID.
func (r *StrategyConfigRepo) GetByID(id string) (*StrategyConfigRecord, error) {
	row := db.QueryRow(`SELECT id, user_id, name, category, strategy_type, symbol, coin, direction, leverage,
		market_type, margin_mode, timeframe, execution_mode, initial_capital, current_equity,
		total_pnl, total_pnl_percent, status, config_json, notification_config, created_at, updated_at
		FROM strategy_configs WHERE id=?`, id)
	return scanStrategyConfig(row)
}

// List returns strategy configs matching the filter. Use limit=0 for unlimited.
func (r *StrategyConfigRepo) List(filter map[string]any, limit int) ([]*StrategyConfigRecord, error) {
	query := `SELECT id, user_id, name, category, strategy_type, symbol, coin, direction, leverage,
		market_type, margin_mode, timeframe, execution_mode, initial_capital, current_equity,
		total_pnl, total_pnl_percent, status, config_json, notification_config, created_at, updated_at
		FROM strategy_configs`
	allowedCols := map[string]bool{
		"id": true, "user_id": true, "category": true, "status": true,
		"symbol": true, "strategy_type": true, "market_type": true,
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

	var result []*StrategyConfigRecord
	for rows.Next() {
		item, err := scanStrategyConfig(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

// Update modifies an existing strategy config.
func (r *StrategyConfigRepo) Update(item *StrategyConfigRecord) error {
	item.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(`UPDATE strategy_configs SET
		user_id=?, name=?, category=?, strategy_type=?, symbol=?, coin=?, direction=?, leverage=?,
		market_type=?, margin_mode=?, timeframe=?, execution_mode=?, initial_capital=?, current_equity=?,
		total_pnl=?, total_pnl_percent=?, status=?, config_json=?, notification_config=?, updated_at=?
		WHERE id=?`,
		item.UserID, item.Name, item.Category, item.StrategyType, item.Symbol, item.Coin,
		item.Direction, item.Leverage, item.MarketType, item.MarginMode, item.Timeframe,
		item.ExecutionMode, item.InitialCapital, item.CurrentEquity, item.TotalPnL,
		item.TotalPnLPercent, item.Status, item.ConfigJSON, item.NotificationConfig,
		item.UpdatedAt, item.ID,
	)
	return err
}

// Delete removes a strategy config.
func (r *StrategyConfigRepo) Delete(id string) error {
	_, err := db.Exec("DELETE FROM strategy_configs WHERE id=?", id)
	return err
}

// UpsertAll writes all provided records, inserting or replacing by ID.
func (r *StrategyConfigRepo) UpsertAll(items []*StrategyConfigRecord) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO strategy_configs (
		id, user_id, name, category, strategy_type, symbol, coin, direction, leverage,
		market_type, margin_mode, timeframe, execution_mode, initial_capital, current_equity,
		total_pnl, total_pnl_percent, status, config_json, notification_config, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, item := range items {
		_, err := stmt.Exec(
			item.ID, item.UserID, item.Name, item.Category, item.StrategyType, item.Symbol, item.Coin,
			item.Direction, item.Leverage, item.MarketType, item.MarginMode, item.Timeframe,
			item.ExecutionMode, item.InitialCapital, item.CurrentEquity, item.TotalPnL,
			item.TotalPnLPercent, item.Status, item.ConfigJSON, item.NotificationConfig,
			item.CreatedAt, item.UpdatedAt,
		)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// MigrateFromJSON imports legacy JSON-file configs into the DB with the given userID.
func (r *StrategyConfigRepo) MigrateFromJSON(path string, userID int64) error {
	data, err := readFileIfExists(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	var rawItems []map[string]any
	if err := json.Unmarshal(data, &rawItems); err != nil {
		return fmt.Errorf("migrate strategy configs: %w", err)
	}
	var items []*StrategyConfigRecord
	for _, m := range rawItems {
		rec := StrategyConfigRecordFromMap(m)
		if rec.UserID == 0 {
			rec.UserID = userID
		}
		items = append(items, rec)
	}
	return r.UpsertAll(items)
}

// StrategyConfigRecordFromMap builds a record from the in-memory map representation.
func StrategyConfigRecordFromMap(m map[string]any) *StrategyConfigRecord {
	r := &StrategyConfigRecord{
		ID:                 getString(m, "id", ""),
		UserID:             getInt64(m, "user_id", 0),
		Name:               getString(m, "name", ""),
		Category:           getString(m, "category", "spot"),
		StrategyType:       getString(m, "strategy_type", getString(m, "type", "")),
		Symbol:             getString(m, "symbol", ""),
		Coin:               getString(m, "coin", ""),
		Direction:          getString(m, "direction", "long"),
		Leverage:           getFloat(m, "leverage", 1),
		MarketType:         getString(m, "market_type", "spot"),
		MarginMode:         getString(m, "margin_mode", "cross"),
		Timeframe:          getString(m, "timeframe", "15m"),
		ExecutionMode:      getString(m, "execution_mode", getString(m, "mode", "signal")),
		InitialCapital:     getFloat(m, "initial_capital", 0),
		CurrentEquity:      getFloat(m, "current_equity", getFloat(m, "initial_capital", 0)),
		TotalPnL:           getFloat(m, "total_pnl", getFloat(m, "pnl", 0)),
		TotalPnLPercent:    getFloat(m, "total_pnl_percent", 0),
		Status:             getString(m, "status", "stopped"),
		ConfigJSON:         normalizeConfigJSON(m["config_json"]),
		NotificationConfig: normalizeConfigJSON(m["notification_config"]),
		CreatedAt:          getTimestampMillis(m, "created_at"),
		UpdatedAt:          getTimestampMillis(m, "updated_at"),
	}
	if r.StrategyType == "" {
		r.StrategyType = getString(m, "strategy_name", "")
	}
	if r.Symbol == "" {
		r.Symbol = getString(m, "name", "")
	}
	if r.ConfigJSON == "" {
		r.ConfigJSON = "{}"
	}
	if r.NotificationConfig == "" {
		r.NotificationConfig = "{}"
	}
	return r
}

// ToMap converts the record back to the in-memory map representation used by handlers.
func (r *StrategyConfigRecord) ToMap() map[string]any {
	m := map[string]any{
		"id":                  r.ID,
		"user_id":             r.UserID,
		"name":                r.Name,
		"category":            r.Category,
		"strategy_type":       r.StrategyType,
		"type":                r.StrategyType,
		"strategy_name":       r.StrategyType,
		"symbol":              r.Symbol,
		"coin":                r.Coin,
		"direction":           r.Direction,
		"trade_direction":     r.Direction,
		"leverage":            r.Leverage,
		"market_type":         r.MarketType,
		"margin_mode":         r.MarginMode,
		"timeframe":           r.Timeframe,
		"execution_mode":      r.ExecutionMode,
		"mode":                r.ExecutionMode,
		"strategy_mode":       r.ExecutionMode,
		"initial_capital":     r.InitialCapital,
		"current_equity":      r.CurrentEquity,
		"total_pnl":           r.TotalPnL,
		"total_pnl_percent":   r.TotalPnLPercent,
		"pnl":                 r.TotalPnL,
		"status":              r.Status,
		"config_json":         r.ConfigJSON,
		"notification_config": r.NotificationConfig,
		"created_at":          float64(r.CreatedAt),
		"updated_at":          float64(r.UpdatedAt),
	}
	return m
}

// scanStrategyConfig scans a single row into a StrategyConfigRecord.
func scanStrategyConfig(scanner interface {
	Scan(dest ...any) error
}) (*StrategyConfigRecord, error) {
	var r StrategyConfigRecord
	err := scanner.Scan(
		&r.ID, &r.UserID, &r.Name, &r.Category, &r.StrategyType, &r.Symbol, &r.Coin,
		&r.Direction, &r.Leverage, &r.MarketType, &r.MarginMode, &r.Timeframe,
		&r.ExecutionMode, &r.InitialCapital, &r.CurrentEquity, &r.TotalPnL,
		&r.TotalPnLPercent, &r.Status, &r.ConfigJSON, &r.NotificationConfig,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func readFileIfExists(path string) ([]byte, error) {
	data, err := osReadFile(path)
	if err != nil {
		if strings.Contains(err.Error(), "no such file") {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// osReadFile is a thin wrapper for OS reads so tests can stub it if needed.
var osReadFile = os.ReadFile

// generateShortID returns a short random hex ID for new records.
func generateShortID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
