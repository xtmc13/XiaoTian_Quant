package agentcron

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// Job 定时任务记录（xt_agent_cron_jobs）。
type Job struct {
	ID         string `json:"id"`
	UserID     int64  `json:"user_id"`
	Name       string `json:"name"`
	Prompt     string `json:"prompt"`
	Schedule   string `json:"schedule"`
	Timezone   string `json:"timezone"`
	Channel    string `json:"channel"`
	Enabled    bool   `json:"enabled"`
	NextRunAt  int64  `json:"next_run_at"`
	LastRunAt  int64  `json:"last_run_at"`
	LastStatus string `json:"last_status"`
	LastResult string `json:"last_result"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// Repo 定时任务存储。
type Repo struct{}

// NewRepo 基于网关默认 SQLite。
func NewRepo() *Repo { return &Repo{} }

func (r *Repo) db() (*sql.DB, error) {
	db := store.GetDB()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	return db, nil
}

// NewID 生成任务 id。
func NewID() string {
	return fmt.Sprintf("cr_%d", time.Now().UnixNano())
}

// Create 插入任务；nextRunAt 需预先算好。
func (r *Repo) Create(j *Job) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	j.CreatedAt, j.UpdatedAt = now, now
	if j.Timezone == "" {
		j.Timezone = "Asia/Shanghai"
	}
	if j.Channel == "" {
		j.Channel = "web"
	}
	_, err = db.Exec(`INSERT INTO xt_agent_cron_jobs
		(id, user_id, name, prompt, schedule, timezone, channel, enabled, next_run_at, last_run_at, last_status, last_result, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.UserID, j.Name, j.Prompt, j.Schedule, j.Timezone, j.Channel, boolInt(j.Enabled), j.NextRunAt,
		j.LastRunAt, j.LastStatus, j.LastResult, j.CreatedAt, j.UpdatedAt)
	return err
}

const jobCols = "id, user_id, name, prompt, schedule, timezone, channel, enabled, next_run_at, last_run_at, last_status, last_result, created_at, updated_at"

func scanJob(row interface{ Scan(...any) error }) (*Job, error) {
	var j Job
	var enabled int
	err := row.Scan(&j.ID, &j.UserID, &j.Name, &j.Prompt, &j.Schedule, &j.Timezone, &j.Channel, &enabled,
		&j.NextRunAt, &j.LastRunAt, &j.LastStatus, &j.LastResult, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}
	j.Enabled = enabled != 0
	return &j, nil
}

// ListByUser 按用户列出（更新时间倒序）。
func (r *Repo) ListByUser(userID int64) ([]*Job, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT `+jobCols+` FROM xt_agent_cron_jobs WHERE user_id = ? ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Get 按 id 取（含 userID 校验）。
func (r *Repo) Get(id string, userID int64) (*Job, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	return scanJob(db.QueryRow(`SELECT `+jobCols+` FROM xt_agent_cron_jobs WHERE id = ? AND user_id = ?`, id, userID))
}

// SetEnabled 启停；禁用时 last_* 不动。
func (r *Repo) SetEnabled(id string, userID int64, enabled bool) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_agent_cron_jobs SET enabled = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		boolInt(enabled), time.Now().Unix(), id, userID)
	return err
}

// Delete 删除。
func (r *Repo) Delete(id string, userID int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM xt_agent_cron_jobs WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// UpdateSchedule 修改 schedule/时区并重算下次触发。
func (r *Repo) UpdateSchedule(id string, userID int64, schedule, timezone string, nextRunAt int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_agent_cron_jobs SET schedule = ?, timezone = ?, next_run_at = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		schedule, timezone, nextRunAt, time.Now().Unix(), id, userID)
	return err
}

// DueJobs 取已到点且启用的任务。
func (r *Repo) DueJobs(now int64, limit int) ([]*Job, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT `+jobCols+` FROM xt_agent_cron_jobs WHERE enabled = 1 AND next_run_at <= ? ORDER BY next_run_at ASC LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkRun 记录一次执行结果并写下次触发时间。
func (r *Repo) MarkRun(id string, status, result string, lastRunAt, nextRunAt int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_agent_cron_jobs SET last_run_at = ?, last_status = ?, last_result = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		lastRunAt, status, result, nextRunAt, time.Now().Unix(), id)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
