package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// ── AI 经验库 Repository ──
// 表 xt_ai_experiences 由 EnsureAIExperienceSchema 幂等创建（自包含 Ensure 模式，
// 与 ai_signal_repo 同写法，独立于 schema.go，避免与迁移流程耦合）。
// ai_auto_trader 策略在每笔平仓后异步复盘（LLM 生成教训，失败走规则兜底），
// 把可执行经验落入此表；下一轮开仓前的 LLM 闸门会读取最近经验辅助风控决策。

// AIExperienceRecord 是 xt_ai_experiences 的行记录。
// Tag 取值：entry_timing / risk_management / market_regime / false_signal；
// Outcome 取值：win / loss。
type AIExperienceRecord struct {
	ID          int64   `json:"id"`
	InstanceID  string  `json:"instance_id"` // 策略实例标识（ai_auto_trader:SYMBOL）
	UserID      int64   `json:"user_id"`
	Symbol      string  `json:"symbol"`
	Lesson      string  `json:"lesson"` // 经验教训（一两句可执行教训）
	Tag         string  `json:"tag"`
	Outcome     string  `json:"outcome"` // win / loss
	PnL         float64 `json:"pnl"`
	ContextJSON string  `json:"context_json"` // 交易快照（入场信号/gate 结论/区间高低等）
	CreatedAt   int64   `json:"created_at"`   // 秒级 Unix
}

// EnsureAIExperienceSchema 幂等创建 xt_ai_experiences 表（含常用查询索引）。
func EnsureAIExperienceSchema() error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS xt_ai_experiences (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		instance_id TEXT NOT NULL,
		user_id INTEGER NOT NULL DEFAULT 0,
		symbol TEXT NOT NULL DEFAULT '',
		lesson TEXT NOT NULL DEFAULT '',
		tag TEXT NOT NULL DEFAULT '',
		outcome TEXT NOT NULL DEFAULT '',
		pnl REAL NOT NULL DEFAULT 0,
		context_json TEXT NOT NULL DEFAULT '{}',
		created_at INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_ai_experiences_instance_time
		ON xt_ai_experiences (instance_id, created_at DESC)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_ai_experiences_user_time
		ON xt_ai_experiences (user_id, created_at DESC)`)
	return err
}

// AIExperienceRepo provides typed CRUD for xt_ai_experiences。
type AIExperienceRepo struct {
	mu         sync.RWMutex
	ensureOnce sync.Once
}

// NewAIExperienceRepo 构造独立 repo 实例。
func NewAIExperienceRepo() *AIExperienceRepo { return &AIExperienceRepo{} }

var (
	aiExperienceRepoOnce sync.Once
	aiExperienceRepoInst *AIExperienceRepo
)

// DefaultAIExperienceRepo 返回进程级共享 repo（懒 Ensure schema）。
func DefaultAIExperienceRepo() *AIExperienceRepo {
	aiExperienceRepoOnce.Do(func() {
		aiExperienceRepoInst = NewAIExperienceRepo()
	})
	return aiExperienceRepoInst
}

// ensure 幂等建表（无视 db 为 nil 的情况，由调用方错误处理兜底）。
func (r *AIExperienceRepo) ensure() {
	r.ensureOnce.Do(func() {
		_ = EnsureAIExperienceSchema()
	})
}

// aiExperienceLimitPerInstance 单实例经验条数上限：超限删最旧（见 Insert 内裁剪）。
const aiExperienceLimitPerInstance = 200

const aiExperienceColumns = `id, instance_id, user_id, symbol, lesson, tag, outcome, pnl, context_json, created_at`

func scanAIExperience(s rowScanner) (*AIExperienceRecord, error) {
	var rec AIExperienceRecord
	err := s.Scan(&rec.ID, &rec.InstanceID, &rec.UserID, &rec.Symbol, &rec.Lesson,
		&rec.Tag, &rec.Outcome, &rec.PnL, &rec.ContextJSON, &rec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// Insert 插入一条经验；补时间戳，并在单实例超限时裁剪最旧记录（保留最新 200 条）。
func (r *AIExperienceRepo) Insert(rec *AIExperienceRecord) error {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return sql.ErrConnDone
	}
	if rec.InstanceID == "" {
		return fmt.Errorf("instance_id required")
	}
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().Unix()
	}
	if rec.ContextJSON == "" {
		rec.ContextJSON = "{}"
	}
	_, err := db.Exec(`INSERT INTO xt_ai_experiences (`+aiExperienceColumns+`) VALUES (NULL,?,?,?,?,?,?,?,?,?)`,
		rec.InstanceID, rec.UserID, rec.Symbol, rec.Lesson, rec.Tag, rec.Outcome, rec.PnL, rec.ContextJSON, rec.CreatedAt)
	if err != nil {
		return err
	}
	// 单实例条数保护：超 200 条删最旧。
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM xt_ai_experiences WHERE instance_id=?`, rec.InstanceID).Scan(&n); err == nil && n > aiExperienceLimitPerInstance {
		excess := n - aiExperienceLimitPerInstance
		_, _ = db.Exec(`DELETE FROM xt_ai_experiences WHERE instance_id=? AND id IN (
			SELECT id FROM xt_ai_experiences WHERE instance_id=? ORDER BY created_at ASC, id ASC LIMIT ?)`,
			rec.InstanceID, rec.InstanceID, excess)
	}
	return nil
}

// ListRecent 拉取某实例的最近经验：同 symbol 优先（时间倒序），不足 limit 时
// 用同实例其他 symbol 的经验按时间倒序补足；全局最多 limit 条。
func (r *AIExperienceRepo) ListRecent(instanceID, symbol string, limit int) ([]*AIExperienceRecord, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, sql.ErrConnDone
	}
	if limit <= 0 {
		return []*AIExperienceRecord{}, nil
	}
	out := make([]*AIExperienceRecord, 0, limit)
	// 第一批：同 symbol。
	if symbol != "" {
		rows, err := db.Query(`SELECT `+aiExperienceColumns+` FROM xt_ai_experiences
			WHERE instance_id=? AND symbol=? ORDER BY created_at DESC, id DESC LIMIT ?`,
			instanceID, symbol, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			rec, err := scanAIExperience(rows)
			if err != nil {
				rows.Close()
				return out, err
			}
			out = append(out, rec)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return out, err
		}
		rows.Close()
	}
	// 第二批：其他 symbol 补足（排除已取到的同 symbol 记录）。
	if remain := limit - len(out); remain > 0 {
		rows, err := db.Query(`SELECT `+aiExperienceColumns+` FROM xt_ai_experiences
			WHERE instance_id=? AND symbol<>? ORDER BY created_at DESC, id DESC LIMIT ?`,
			instanceID, symbol, remain)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			rec, err := scanAIExperience(rows)
			if err != nil {
				rows.Close()
				return out, err
			}
			out = append(out, rec)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return out, err
		}
		rows.Close()
	}
	return out, nil
}

// CountByInstance 统计某实例的经验条数。
func (r *AIExperienceRepo) CountByInstance(instanceID string) (int, error) {
	r.ensure()
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, sql.ErrConnDone
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM xt_ai_experiences WHERE instance_id=?`, instanceID).Scan(&n)
	return n, err
}
