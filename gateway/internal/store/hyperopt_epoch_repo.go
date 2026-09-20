package store

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"
)

// ── Hyperopt Epoch Repository ──
// 表结构见 migrations/sql/0018_hyperopt_epochs.sql。
// 每个优化任务（hyperopt job）的每一轮 trial 落一条 epoch 记录，
// 支撑过滤器查询与最优超参一键回写策略。

// HyperoptEpochRecord is the SQLite-backed representation of one hyperopt trial.
type HyperoptEpochRecord struct {
	ID          string  `json:"id"`
	UserID      int64   `json:"user_id"`
	JobID       string  `json:"job_id"`
	StrategyID  string  `json:"strategy_id"`
	TrialID     int     `json:"trial_id"`
	ParamsJSON  string  `json:"params"`  // JSON: map[string]any 超参
	MetricsJSON string  `json:"metrics"` // JSON: map[string]float64 回测指标
	Loss        float64 `json:"loss"`
	LossName    string  `json:"loss_name"`
	Applied     bool    `json:"applied"`
	AppliedAt   int64   `json:"applied_at,omitempty"`
	CreatedAt   int64   `json:"created_at"`
}

// HyperoptEpochFilter 组合过滤条件，全部可选；零值表示不限制。
// UserID>0 时额外对“历史无属主”(user_id=0) 的记录放行，与任务列表口径一致。
type HyperoptEpochFilter struct {
	JobID          string
	UserID         int64 // 0 = 不按属主过滤（admin/内部调用）
	LossMax        *float64
	SharpeMin      *float64
	TradeCountMin  *int
	MaxDrawdownMax *float64
	Limit          int // <=0 默认 200
}

type HyperoptEpochRepo struct{ mu sync.RWMutex }

// NewHyperoptEpochRepo creates a repo instance.
func NewHyperoptEpochRepo() *HyperoptEpochRepo { return &HyperoptEpochRepo{} }

const hyperoptEpochColumns = `id, user_id, job_id, strategy_id, trial_id, params, metrics,
	loss, loss_name, applied, applied_at, created_at`

// Create inserts a new epoch record. It assigns CreatedAt if missing.
func (r *HyperoptEpochRepo) Create(rec *HyperoptEpochRecord) error {
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	if rec.ParamsJSON == "" {
		rec.ParamsJSON = "{}"
	}
	if rec.MetricsJSON == "" {
		rec.MetricsJSON = "{}"
	}
	_, err := db.Exec(
		`INSERT INTO xt_hyperopt_epochs
			(id, user_id, job_id, strategy_id, trial_id, params, metrics, loss, loss_name, applied, applied_at, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.JobID, rec.StrategyID, rec.TrialID, rec.ParamsJSON, rec.MetricsJSON,
		rec.Loss, rec.LossName, boolToIntConv(rec.Applied), rec.AppliedAt, rec.CreatedAt,
	)
	return err
}

// GetByID returns a single epoch by ID (属主校验由 handler 做)。
func (r *HyperoptEpochRepo) GetByID(id string) (*HyperoptEpochRecord, error) {
	row := db.QueryRow(`SELECT `+hyperoptEpochColumns+` FROM xt_hyperopt_epochs WHERE id = ?`, id)
	return r.scan(row)
}

// List returns epochs matching the filter, in trial (created_at) order.
// Numeric filters (loss/sharpe/trade_count/max_drawdown) are applied in Go on
// the decoded metrics JSON — SQLite json_extract 语义随驱动版本波动，
// 统一在 Go 侧过滤更稳，且单任务 epoch 量级（<=500）完全够用。
func (r *HyperoptEpochRepo) List(f HyperoptEpochFilter) ([]*HyperoptEpochRecord, error) {
	if f.Limit <= 0 {
		f.Limit = 200
	}
	query := `SELECT ` + hyperoptEpochColumns + ` FROM xt_hyperopt_epochs WHERE 1=1`
	var args []any
	if f.JobID != "" {
		query += ` AND job_id = ?`
		args = append(args, f.JobID)
	}
	if f.UserID > 0 {
		query += ` AND (user_id = ? OR user_id = 0)`
		args = append(args, f.UserID)
	}
	query += ` ORDER BY created_at ASC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*HyperoptEpochRecord, 0, f.Limit)
	for rows.Next() {
		rec, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		if !epochMatchesFilter(rec, f) {
			continue
		}
		out = append(out, rec)
		if len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

// MarkApplied sets the applied flag on an epoch (回写策略成功后调用)。
func (r *HyperoptEpochRepo) MarkApplied(id string, appliedAt int64) error {
	if appliedAt == 0 {
		appliedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(`UPDATE xt_hyperopt_epochs SET applied = 1, applied_at = ? WHERE id = ?`, appliedAt, id)
	return err
}

func (r *HyperoptEpochRepo) scan(s rowScanner) (*HyperoptEpochRecord, error) {
	var rec HyperoptEpochRecord
	var applied int
	err := s.Scan(&rec.ID, &rec.UserID, &rec.JobID, &rec.StrategyID, &rec.TrialID,
		&rec.ParamsJSON, &rec.MetricsJSON, &rec.Loss, &rec.LossName, &applied, &rec.AppliedAt, &rec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Applied = applied != 0
	return &rec, nil
}

// epochMatchesFilter applies the optional numeric filters against the epoch's
// decoded metrics payload.
func epochMatchesFilter(rec *HyperoptEpochRecord, f HyperoptEpochFilter) bool {
	if f.LossMax != nil && !(rec.Loss <= *f.LossMax) {
		return false
	}
	if f.SharpeMin == nil && f.TradeCountMin == nil && f.MaxDrawdownMax == nil {
		return true
	}
	var metrics map[string]float64
	if err := json.Unmarshal([]byte(rec.MetricsJSON), &metrics); err != nil {
		return false
	}
	if f.SharpeMin != nil && !(metrics["sharpe_ratio"] >= *f.SharpeMin) {
		return false
	}
	if f.TradeCountMin != nil && !(metrics["total_trades"] >= float64(*f.TradeCountMin)) {
		return false
	}
	if f.MaxDrawdownMax != nil && !(metrics["max_drawdown_pct"] <= *f.MaxDrawdownMax) {
		return false
	}
	return true
}

func boolToIntConv(b bool) int {
	if b {
		return 1
	}
	return 0
}
