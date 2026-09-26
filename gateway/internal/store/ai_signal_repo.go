package store

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"
)

// ── AI 决策信号 Repository ──
// 表 xt_ai_signals 由 EnsureAISignalsSchema 幂等创建（见文件内，独立于 schema.go，
// 避免与迁移流程耦合）。信号由 AI 决策引擎（fast/deep 两档）产出，
// AI 机器人策略与 paper 模拟器消费此表驱动真实开平仓。

// AISignalRecord 是 xt_ai_signals 的行记录。
// Signal 取值：long / short / neutral；Confidence 归一化到 0-100。
type AISignalRecord struct {
	ID              string   `json:"id"`
	UserID          int64    `json:"user_id"`
	Symbol          string   `json:"symbol"`
	Signal          string   `json:"signal"`
	Confidence      float64  `json:"confidence"`
	Reason          string   `json:"reason"`
	Filters         []string `json:"filters"`
	MarketCondition string   `json:"market_condition"`
	Mode            string   `json:"mode"`
	Provider        string   `json:"provider"`
	CreatedAt       int64    `json:"created_at"` // 秒级 Unix
}

// EnsureAISignalsSchema 幂等创建 xt_ai_signals 表（含常用查询索引）。
func EnsureAISignalsSchema() error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS xt_ai_signals (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL DEFAULT 0,
		symbol TEXT NOT NULL,
		signal TEXT NOT NULL DEFAULT 'neutral',
		confidence REAL NOT NULL DEFAULT 0,
		reason TEXT NOT NULL DEFAULT '',
		filters TEXT NOT NULL DEFAULT '[]',
		market_condition TEXT NOT NULL DEFAULT '',
		mode TEXT NOT NULL DEFAULT 'fast',
		provider TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_ai_signals_user_time
		ON xt_ai_signals (user_id, created_at DESC)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_ai_signals_user_symbol_time
		ON xt_ai_signals (user_id, symbol, created_at DESC)`)
	return err
}

// AISignalRepo provides typed CRUD for xt_ai_signals。
type AISignalRepo struct {
	mu         sync.RWMutex
	ensureOnce sync.Once
}

func NewAISignalRepo() *AISignalRepo { return &AISignalRepo{} }

var (
	aiSignalRepoOnce sync.Once
	aiSignalRepoInst *AISignalRepo
)

// DefaultAISignalRepo 返回进程级共享 repo（懒Ensure schema）。
func DefaultAISignalRepo() *AISignalRepo {
	aiSignalRepoOnce.Do(func() {
		aiSignalRepoInst = NewAISignalRepo()
	})
	return aiSignalRepoInst
}

// ensure 幂等建表（无视 db 为 nil 的情况，由调用方错误处理兜底）。
func (r *AISignalRepo) ensure() {
	r.ensureOnce.Do(func() {
		_ = EnsureAISignalsSchema()
	})
}

const aiSignalColumns = `id, user_id, symbol, signal, confidence, reason,
	filters, market_condition, mode, provider, created_at`

func scanAISignal(s rowScanner) (*AISignalRecord, error) {
	var rec AISignalRecord
	var filtersJSON string
	err := s.Scan(&rec.ID, &rec.UserID, &rec.Symbol, &rec.Signal, &rec.Confidence,
		&rec.Reason, &filtersJSON, &rec.MarketCondition, &rec.Mode, &rec.Provider,
		&rec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Filters = []string{}
	if filtersJSON != "" {
		_ = json.Unmarshal([]byte(filtersJSON), &rec.Filters)
	}
	return &rec, nil
}

// Insert 插入一条信号；补 id/时间戳。created_at 为秒级 Unix。
func (r *AISignalRepo) Insert(rec *AISignalRecord) error {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return sql.ErrConnDone
	}
	if rec.ID == "" {
		rec.ID = generateShortID()
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().Unix()
	}
	filtersJSON, _ := json.Marshal(rec.Filters)
	_, err := db.Exec(`INSERT INTO xt_ai_signals (`+aiSignalColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.Symbol, rec.Signal, rec.Confidence, rec.Reason,
		string(filtersJSON), rec.MarketCondition, rec.Mode, rec.Provider, rec.CreatedAt)
	return err
}

// ListByUser 按用户拉取信号；symbol 为空表示全部，since 为秒级时间下限（0 不限），
// 按时间倒序，最多 limit 条（<=0 时默认 200）。
func (r *AISignalRepo) ListByUser(userID int64, symbol string, since int64, limit int) ([]*AISignalRecord, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	if limit <= 0 {
		limit = 200
	}
	query := `SELECT ` + aiSignalColumns + ` FROM xt_ai_signals WHERE user_id=?`
	args := []any{userID}
	if symbol != "" {
		query += ` AND symbol=?`
		args = append(args, symbol)
	}
	if since > 0 {
		query += ` AND created_at>=?`
		args = append(args, since)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*AISignalRecord, 0, limit)
	for rows.Next() {
		rec, err := scanAISignal(rows)
		if err != nil {
			return out, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// LatestByUserSymbol 取某用户某品种的最新一条信号；无记录返回 (nil, nil)。
func (r *AISignalRepo) LatestByUserSymbol(userID int64, symbol string) (*AISignalRecord, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	row := db.QueryRow(`SELECT `+aiSignalColumns+` FROM xt_ai_signals
		WHERE user_id=? AND symbol=? ORDER BY created_at DESC LIMIT 1`, userID, symbol)
	return scanAISignal(row)
}

// CountByUserSince 统计某用户 since（秒）以来的信号数（可选只统计某品种）。
func (r *AISignalRepo) CountByUserSince(userID int64, symbol string, since int64) (int, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, sql.ErrConnDone
	}
	query := `SELECT COUNT(*) FROM xt_ai_signals WHERE user_id=? AND created_at>=?`
	args := []any{userID, since}
	if symbol != "" {
		query += ` AND symbol=?`
		args = append(args, symbol)
	}
	var n int
	err := db.QueryRow(query, args...).Scan(&n)
	return n, err
}

// ── AI 机器人配置 Repository ──
// 每用户一行配置（JSON），前端 AIRobotPanel 读写 /ai-robot/config。

// EnsureAIRobotConfigSchema 幂等创建 xt_ai_robot_configs 表。
func EnsureAIRobotConfigSchema() error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS xt_ai_robot_configs (
		user_id INTEGER PRIMARY KEY,
		config_json TEXT NOT NULL DEFAULT '{}',
		created_at INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0
	)`)
	return err
}

// AIRobotConfigRepo 提供按用户的 AI 机器人配置读写。
type AIRobotConfigRepo struct {
	mu         sync.RWMutex
	ensureOnce sync.Once
}

// ensure 幂等建表。
func (r *AIRobotConfigRepo) ensure() {
	r.ensureOnce.Do(func() {
		_ = EnsureAIRobotConfigSchema()
	})
}

func NewAIRobotConfigRepo() *AIRobotConfigRepo { return &AIRobotConfigRepo{} }

var (
	aiRobotCfgOnce sync.Once
	aiRobotCfgInst *AIRobotConfigRepo
)

// DefaultAIRobotConfigRepo 返回进程级共享 repo（懒Ensure schema）。
func DefaultAIRobotConfigRepo() *AIRobotConfigRepo {
	aiRobotCfgOnce.Do(func() {
		aiRobotCfgInst = NewAIRobotConfigRepo()
	})
	return aiRobotCfgInst
}

// Get 读取某用户配置；未保存过返回 (nil, nil)。
func (r *AIRobotConfigRepo) Get(userID int64) (map[string]any, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	var raw string
	err := db.QueryRow(`SELECT config_json FROM xt_ai_robot_configs WHERE user_id=?`, userID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save 全量覆盖某用户配置（upsert）。
func (r *AIRobotConfigRepo) Save(userID int64, cfg map[string]any) error {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return sql.ErrConnDone
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO xt_ai_robot_configs (user_id, config_json, created_at, updated_at)
		VALUES (?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET config_json=excluded.config_json, updated_at=excluded.updated_at`,
		userID, string(raw), now, now)
	return err
}

// stringSliceFromAny 已移除：handler 侧有同名本地实现（stringSliceFromAnyLocal），
// store 不再提供该工具，避免双份维护。
