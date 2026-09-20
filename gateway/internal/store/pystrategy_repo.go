package store

import (
	"database/sql"
	"sync"
	"time"
)

// ── 用户 Python 策略 Repository ──
// 表结构见 migrations/sql/0021_pystrategies.sql。
// 代码版本快照复用 xt_strategy_versions（0014），strategy_id = PyStrategyRecord.ID。

// PyStrategyStatus 常量（draft/active/paused/error）。
const (
	PyStratStatusDraft  = "draft"
	PyStratStatusActive = "active"
	PyStratStatusPaused = "paused"
	PyStratStatusError  = "error"
)

// PyStrategyRecord 是 xt_pystrategies 的行记录。
// Market/Leverage/MarginMode 为 v1.1 合约执行声明（0023），manifest 的
// risk.leverage/risk.margin_mode 是策略侧覆盖，运行时优先于本表列。
type PyStrategyRecord struct {
	ID         string `json:"id"`
	UserID     int64  `json:"user_id"`
	Name       string `json:"name"`
	Symbol     string `json:"symbol"`
	Interval   string `json:"interval"`
	Direction  string `json:"direction"`
	ParamsJSON string `json:"params_json"`
	Code       string `json:"code"`
	Version    int    `json:"version"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	Paper      bool   `json:"paper"`
	BotID      string `json:"bot_id"`
	Market     string `json:"market"`
	Leverage   int    `json:"leverage"`
	MarginMode string `json:"margin_mode"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// PyStrat 合约执行声明默认值（0023 列默认值保持一致）。
const (
	PyStratMarketSpot     = "spot"
	PyStratMarketFutures  = "futures"
	PyStratMarginCross    = "cross"
	PyStratMarginIsolated = "isolated"
	// PyStratMaxLeverage 是合约杠杆上限（与主流交易所 U 本位永续上限对齐）。
	PyStratMaxLeverage = 125
)

// PyStrategyRepo provides typed CRUD for xt_pystrategies。
type PyStrategyRepo struct{ mu sync.RWMutex }

func NewPyStrategyRepo() *PyStrategyRepo { return &PyStrategyRepo{} }

const pyStrategyColumns = `id, user_id, name, symbol, interval, direction, params_json,
	code, version, status, error, paper, bot_id, market, leverage, margin_mode, created_at, updated_at`

func scanPyStrategy(s rowScanner) (*PyStrategyRecord, error) {
	var rec PyStrategyRecord
	var paper int
	err := s.Scan(&rec.ID, &rec.UserID, &rec.Name, &rec.Symbol, &rec.Interval, &rec.Direction,
		&rec.ParamsJSON, &rec.Code, &rec.Version, &rec.Status, &rec.Error, &paper,
		&rec.BotID, &rec.Market, &rec.Leverage, &rec.MarginMode, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Paper = paper != 0
	return &rec, nil
}

// Create 插入新策略；补 id/时间戳/状态默认值。记录创建后 rec 携带落库值。
func (r *PyStrategyRepo) Create(rec *PyStrategyRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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
		rec.Status = PyStratStatusDraft
	}
	if rec.ParamsJSON == "" {
		rec.ParamsJSON = "{}"
	}
	if rec.Market == "" {
		rec.Market = PyStratMarketSpot
	}
	if rec.Leverage <= 0 {
		rec.Leverage = 1
	}
	if rec.MarginMode == "" {
		rec.MarginMode = PyStratMarginCross
	}
	paper := 0
	if rec.Paper {
		paper = 1
	}
	_, err := db.Exec(`INSERT INTO xt_pystrategies (`+pyStrategyColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.Name, rec.Symbol, rec.Interval, rec.Direction,
		rec.ParamsJSON, rec.Code, rec.Version, rec.Status, rec.Error, paper,
		rec.BotID, rec.Market, rec.Leverage, rec.MarginMode, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// GetByID 按 id 取策略；不存在返回 (nil, nil)。
func (r *PyStrategyRepo) GetByID(id string) (*PyStrategyRecord, error) {
	row := db.QueryRow(`SELECT `+pyStrategyColumns+` FROM xt_pystrategies WHERE id = ?`, id)
	return scanPyStrategy(row)
}

// List 按过滤条件列表；limit=0 表示不限。user_id/status 为允许过滤列。
func (r *PyStrategyRepo) List(filter map[string]any, limit int) ([]*PyStrategyRecord, error) {
	query := `SELECT ` + pyStrategyColumns + ` FROM xt_pystrategies`
	allowedCols := map[string]bool{"user_id": true, "status": true, "symbol": true}
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
	var out []*PyStrategyRecord
	for rows.Next() {
		rec, err := scanPyStrategy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// Update 全字段更新（code/params 也走这里），自动刷新 updated_at。
func (r *PyStrategyRepo) Update(rec *PyStrategyRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec.UpdatedAt = time.Now().UnixMilli()
	paper := 0
	if rec.Paper {
		paper = 1
	}
	if rec.Market == "" {
		rec.Market = PyStratMarketSpot
	}
	if rec.Leverage <= 0 {
		rec.Leverage = 1
	}
	if rec.MarginMode == "" {
		rec.MarginMode = PyStratMarginCross
	}
	_, err := db.Exec(`UPDATE xt_pystrategies SET
		name=?, symbol=?, interval=?, direction=?, params_json=?, code=?,
		version=?, status=?, error=?, paper=?, bot_id=?, market=?, leverage=?,
		margin_mode=?, updated_at=? WHERE id=?`,
		rec.Name, rec.Symbol, rec.Interval, rec.Direction, rec.ParamsJSON, rec.Code,
		rec.Version, rec.Status, rec.Error, paper, rec.BotID, rec.Market, rec.Leverage,
		rec.MarginMode, rec.UpdatedAt, rec.ID)
	return err
}

// UpdateStatus 只刷状态与错误（运行控制高频路径，避免全行写）。
func (r *PyStrategyRepo) UpdateStatus(id, status, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := db.Exec(`UPDATE xt_pystrategies SET status=?, error=?, updated_at=? WHERE id=?`,
		status, errMsg, time.Now().UnixMilli(), id)
	return err
}

// Delete 删除策略（版本快照保留在 xt_strategy_versions，历史可追溯）。
func (r *PyStrategyRepo) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := db.Exec(`DELETE FROM xt_pystrategies WHERE id = ?`, id)
	return err
}
