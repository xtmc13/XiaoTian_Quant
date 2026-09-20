package store

import (
	"errors"
	"sync"
	"time"
)

// ── ML Retrain Repository（自动滚动重训，对标 FreqAI live_retrain_hours）──
//
// 两张表的读写入口：
//   - xt_ml_retrain_jobs  重训任务（间隔驱动，last_* 记录最近一轮结果）
//   - xt_ml_retrain_runs  运行记录（最近运行历史 + 连续失败计数来源）

// MLRetrainJob 一条重训任务。
type MLRetrainJob struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"user_id"`
	ModelName       string `json:"model_name"`
	FeatureSet      string `json:"feature_set"` // JSON: symbol/interval/model_type/task_type/lookback_days/feature_periods/label_horizon/model_params
	IntervalMinutes int    `json:"interval_minutes"`
	LastRunAt       int64  `json:"last_run_at"`
	LastStatus      string `json:"last_status"` // success | failed | ""
	LastError       string `json:"last_error"`
	CreatedAt       int64  `json:"created_at"`
	Active          bool   `json:"active"`
}

// MLRetrainRun 一条重训运行记录。
type MLRetrainRun struct {
	ID         int64  `json:"id"`
	JobID      int64  `json:"job_id"`
	Status     string `json:"status"` // success | failed
	Error      string `json:"error"`
	DurationMs int64  `json:"duration_ms"`
	CreatedAt  int64  `json:"created_at"`
}

// MLRetrainJobPatch 任务的可更新字段（nil 表示不更新）。
type MLRetrainJobPatch struct {
	ModelName       *string
	FeatureSet      *string
	IntervalMinutes *int
	Active          *bool
}

type MLRetrainRepo struct{ mu sync.Mutex }

func NewMLRetrainRepo() *MLRetrainRepo { return &MLRetrainRepo{} }

const mlRetrainJobCols = `id, user_id, model_name, feature_set, interval_minutes, last_run_at, last_status, last_error, created_at, active`

func scanMLRetrainJob(row interface{ Scan(...any) error }) (*MLRetrainJob, error) {
	var j MLRetrainJob
	var active int
	err := row.Scan(&j.ID, &j.UserID, &j.ModelName, &j.FeatureSet, &j.IntervalMinutes,
		&j.LastRunAt, &j.LastStatus, &j.LastError, &j.CreatedAt, &active)
	if err != nil {
		return nil, err
	}
	j.Active = active != 0
	return &j, nil
}

// CreateJob 落一条重训任务，返回自增 id。
func (r *MLRetrainRepo) CreateJob(userID int64, modelName, featureSet string, intervalMinutes int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, errors.New("store not initialized")
	}
	if intervalMinutes <= 0 {
		intervalMinutes = 1440
	}
	res, err := db.Exec(`INSERT INTO xt_ml_retrain_jobs
		(user_id, model_name, feature_set, interval_minutes, created_at, active)
		VALUES (?,?,?,?,?,1)`,
		userID, modelName, featureSet, intervalMinutes, time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetJob 按 id 取任务。
func (r *MLRetrainRepo) GetJob(id int64) (*MLRetrainJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, errors.New("store not initialized")
	}
	return scanMLRetrainJob(db.QueryRow(`SELECT `+mlRetrainJobCols+` FROM xt_ml_retrain_jobs WHERE id=?`, id))
}

// UpdateJob 应用补丁（先读整行再整行写回，字段少、调用低频）。
func (r *MLRetrainRepo) UpdateJob(id int64, patch MLRetrainJobPatch) (*MLRetrainJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, errors.New("store not initialized")
	}
	j, err := scanMLRetrainJob(db.QueryRow(`SELECT `+mlRetrainJobCols+` FROM xt_ml_retrain_jobs WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	if patch.ModelName != nil && *patch.ModelName != "" {
		j.ModelName = *patch.ModelName
	}
	if patch.FeatureSet != nil {
		j.FeatureSet = *patch.FeatureSet
	}
	if patch.IntervalMinutes != nil && *patch.IntervalMinutes > 0 {
		j.IntervalMinutes = *patch.IntervalMinutes
	}
	if patch.Active != nil {
		j.Active = *patch.Active
	}
	active := 0
	if j.Active {
		active = 1
	}
	_, err = db.Exec(`UPDATE xt_ml_retrain_jobs SET model_name=?, feature_set=?, interval_minutes=?, active=? WHERE id=?`,
		j.ModelName, j.FeatureSet, j.IntervalMinutes, active, id)
	if err != nil {
		return nil, err
	}
	return j, nil
}

// ListJobs 列出任务。userID>0 时只看本人 + 历史无属主（与 reconcile 惯例一致）；
// activeOnly=true 只列启用任务（调度器用）。
func (r *MLRetrainRepo) ListJobs(userID int64, activeOnly bool) ([]*MLRetrainJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, errors.New("store not initialized")
	}
	q := `SELECT ` + mlRetrainJobCols + ` FROM xt_ml_retrain_jobs`
	var args []any
	if userID > 0 {
		q += ` WHERE (user_id=? OR user_id=0)`
		args = append(args, userID)
	}
	if activeOnly {
		if len(args) > 0 {
			q += ` AND`
		} else {
			q += ` WHERE`
		}
		q += ` active=1`
	}
	q += ` ORDER BY id`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []*MLRetrainJob
	for rows.Next() {
		j, err := scanMLRetrainJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// SetJobActive 启停任务（自动暂停/管理接口共用）。
func (r *MLRetrainRepo) SetJobActive(id int64, active bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return errors.New("store not initialized")
	}
	v := 0
	if active {
		v = 1
	}
	_, err := db.Exec(`UPDATE xt_ml_retrain_jobs SET active=? WHERE id=?`, v, id)
	return err
}

// RecordRun 记一条运行记录并回写任务 last_run_at/last_status/last_error（同一锁内完成）。
func (r *MLRetrainRepo) RecordRun(jobID int64, status, errMsg string, durationMs int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, errors.New("store not initialized")
	}
	now := time.Now().UnixMilli()
	res, err := db.Exec(`INSERT INTO xt_ml_retrain_runs (job_id, status, error, duration_ms, created_at)
		VALUES (?,?,?,?,?)`, jobID, status, errMsg, durationMs, now)
	if err != nil {
		return 0, err
	}
	runID, _ := res.LastInsertId()
	if _, err := db.Exec(`UPDATE xt_ml_retrain_jobs SET last_run_at=?, last_status=?, last_error=? WHERE id=?`,
		now, status, errMsg, jobID); err != nil {
		return runID, err
	}
	return runID, nil
}

// ListRuns 取任务最近运行记录（新的在前）。
func (r *MLRetrainRepo) ListRuns(jobID int64, limit int) ([]*MLRetrainRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil, errors.New("store not initialized")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := db.Query(`SELECT id, job_id, status, error, duration_ms, created_at
		FROM xt_ml_retrain_runs WHERE job_id=? ORDER BY id DESC LIMIT ?`, jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*MLRetrainRun
	for rows.Next() {
		var run MLRetrainRun
		if err := rows.Scan(&run.ID, &run.JobID, &run.Status, &run.Error, &run.DurationMs, &run.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, &run)
	}
	return runs, rows.Err()
}

// ConsecutiveFailures 最近 limit 次运行里的连续失败数（从最新往前数，
// 遇到 success 即停）。limit 传最大连续失败阈值即可。
func (r *MLRetrainRepo) ConsecutiveFailures(jobID int64, limit int) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, errors.New("store not initialized")
	}
	if limit <= 0 {
		limit = 3
	}
	rows, err := db.Query(`SELECT status FROM xt_ml_retrain_runs
		WHERE job_id=? ORDER BY id DESC LIMIT ?`, jobID, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	fails := 0
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return 0, err
		}
		if status != "failed" {
			break
		}
		fails++
	}
	return fails, rows.Err()
}

// GetSetting 读 ML 配置覆盖（ml_settings 键值表），不存在返回 ""。
func (r *MLRetrainRepo) GetSetting(key string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return ""
	}
	var v string
	if err := db.QueryRow(`SELECT value FROM ml_settings WHERE key=?`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}

// SetSetting 写 ML 配置覆盖。
func (r *MLRetrainRepo) SetSetting(key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return errors.New("store not initialized")
	}
	_, err := db.Exec(`INSERT INTO ml_settings (key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, time.Now().UnixMilli())
	return err
}
