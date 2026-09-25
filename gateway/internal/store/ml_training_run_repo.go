package store

import (
	"database/sql"
	"errors"
	"sync"
	"time"
)

// ── ML Training Run Repository（训练闭环运行档案）──────────────
//
// 表结构见 migrations/sql/0032_ml_training_loop.sql。
// 与 MLRetrainRepo（任务级成败流水）分工：本仓库记录每次闭环训练的完整产物
// —— 样本/特征/指标/模型版本/训练后端，供训练历史 API 与闭环状态汇总查询。

// MLTrainingRun 一次闭环训练的运行档案。
type MLTrainingRun struct {
	ID           int64  `json:"id"`
	JobID        int64  `json:"job_id"`     // 0 = 非任务触发（手动训练等）
	ModelName    string `json:"model_name"` // 即 model_id
	TriggerSrc   string `json:"trigger"`    // schedule | manual | drift
	Trainer      string `json:"trainer"`    // python | go_fallback
	Status       string `json:"status"`     // success | failed | skipped
	Error        string `json:"error"`
	Symbol       string `json:"symbol"`
	Interval     string `json:"interval"`
	BarsLoaded   int    `json:"bars_loaded"`
	TrainSamples int    `json:"train_samples"`
	TestSamples  int    `json:"test_samples"`
	FeatureCount int    `json:"feature_count"`
	MetricsJSON  string `json:"metrics_json"`
	ModelVersion string `json:"model_version"`
	DurationMs   int64  `json:"duration_ms"`
	CreatedAt    int64  `json:"created_at"`
}

type MLTrainingRunRepo struct{ mu sync.Mutex }

func NewMLTrainingRunRepo() *MLTrainingRunRepo { return &MLTrainingRunRepo{} }

const mlTrainingRunCols = `id, job_id, model_name, trigger_src, trainer, status, error, symbol, interval,
	bars_loaded, train_samples, test_samples, feature_count, metrics_json, model_version, duration_ms, created_at`

func scanMLTrainingRun(row interface{ Scan(...any) error }) (*MLTrainingRun, error) {
	var r MLTrainingRun
	err := row.Scan(&r.ID, &r.JobID, &r.ModelName, &r.TriggerSrc, &r.Trainer, &r.Status, &r.Error,
		&r.Symbol, &r.Interval, &r.BarsLoaded, &r.TrainSamples, &r.TestSamples, &r.FeatureCount,
		&r.MetricsJSON, &r.ModelVersion, &r.DurationMs, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Record 落一条训练运行档案，返回自增 id。
func (r *MLTrainingRunRepo) Record(run *MLTrainingRun) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, errors.New("store not initialized")
	}
	if run.CreatedAt == 0 {
		run.CreatedAt = time.Now().UnixMilli()
	}
	res, err := db.Exec(`INSERT INTO xt_ml_training_runs
		(job_id, model_name, trigger_src, trainer, status, error, symbol, interval,
		 bars_loaded, train_samples, test_samples, feature_count, metrics_json, model_version,
		 duration_ms, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.JobID, run.ModelName, run.TriggerSrc, run.Trainer, run.Status, run.Error,
		run.Symbol, run.Interval, run.BarsLoaded, run.TrainSamples, run.TestSamples,
		run.FeatureCount, run.MetricsJSON, run.ModelVersion, run.DurationMs, run.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// List 取训练运行历史（新的在前）。modelName 非空按模型过滤。
// userID>0 时只看本人任务（job_id 关联 xt_ml_retrain_jobs）+ job_id=0 的公共运行，
// 与 MLRetrainRepo.ListJobs 的属主惯例一致。
func (r *MLTrainingRunRepo) List(userID int64, modelName string, limit int) ([]*MLTrainingRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, errors.New("store not initialized")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT ` + mlTrainingRunCols + ` FROM xt_ml_training_runs t`
	var args []any
	where := ""
	if userID > 0 {
		where += ` (t.job_id=0 OR t.job_id IN (SELECT id FROM xt_ml_retrain_jobs WHERE user_id=? OR user_id=0))`
		args = append(args, userID)
	}
	if modelName != "" {
		if where != "" {
			where += " AND"
		}
		where += ` t.model_name=?`
		args = append(args, modelName)
	}
	if where != "" {
		q += ` WHERE` + where
	}
	q += ` ORDER BY t.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*MLTrainingRun
	for rows.Next() {
		run, err := scanMLTrainingRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// Latest 取最近一条运行（modelName 为空 = 不限模型）；无记录返回 sql.ErrNoRows。
func (r *MLTrainingRunRepo) Latest(modelName string) (*MLTrainingRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, errors.New("store not initialized")
	}
	q := `SELECT ` + mlTrainingRunCols + ` FROM xt_ml_training_runs`
	var args []any
	if modelName != "" {
		q += ` WHERE model_name=?`
		args = append(args, modelName)
	}
	q += ` ORDER BY id DESC LIMIT 1`
	return scanMLTrainingRun(db.QueryRow(q, args...))
}

// LatestSuccessPerModel 取每个模型最近一次成功运行（闭环状态汇总用）。
func (r *MLTrainingRunRepo) LatestSuccessPerModel() ([]*MLTrainingRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, errors.New("store not initialized")
	}
	rows, err := db.Query(`SELECT ` + mlTrainingRunCols + ` FROM xt_ml_training_runs t
		WHERE t.status='success' AND t.id IN (
			SELECT MAX(id) FROM xt_ml_training_runs WHERE status='success' GROUP BY model_name
		) ORDER BY t.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*MLTrainingRun
	for rows.Next() {
		run, err := scanMLTrainingRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// 编译期断言：Latest 的 ErrNoRows 语义供 handler 判空。
var _ = sql.ErrNoRows
