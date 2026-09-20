package store

import (
	"database/sql"
	"sync"
	"time"
)

// ── 指标信号告警任务 Repository ──
// 表结构见 migrations/sql/0022_indicator_alerts.sql。

// IndicatorAlertRecord 是 xt_indicator_alerts 的行记录。
type IndicatorAlertRecord struct {
	ID              string  `json:"id"`
	UserID          int64   `json:"user_id"`
	Name            string  `json:"name"`
	Symbol          string  `json:"symbol"`
	Interval        string  `json:"interval"`
	ConditionExpr   string  `json:"condition_expr"`
	Message         string  `json:"message"`
	CooldownMinutes int     `json:"cooldown_minutes"`
	LastTriggeredAt int64   `json:"last_triggered_at"`
	LastValue       float64 `json:"last_value"`
	Active          bool    `json:"active"`
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`
}

// IndicatorAlertRepo provides typed CRUD for xt_indicator_alerts。
type IndicatorAlertRepo struct{ mu sync.RWMutex }

func NewIndicatorAlertRepo() *IndicatorAlertRepo { return &IndicatorAlertRepo{} }

const indicatorAlertColumns = `id, user_id, name, symbol, interval, condition_expr, message,
	cooldown_minutes, last_triggered_at, last_value, active, created_at, updated_at`

func scanIndicatorAlert(s rowScanner) (*IndicatorAlertRecord, error) {
	var rec IndicatorAlertRecord
	var active int
	err := s.Scan(&rec.ID, &rec.UserID, &rec.Name, &rec.Symbol, &rec.Interval,
		&rec.ConditionExpr, &rec.Message, &rec.CooldownMinutes,
		&rec.LastTriggeredAt, &rec.LastValue, &active, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.Active = active != 0
	return &rec, nil
}

// Create 插入新告警任务；补 id/时间戳/默认值。记录创建后 rec 携带落库值。
func (r *IndicatorAlertRepo) Create(rec *IndicatorAlertRecord) error {
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
	active := 0
	if rec.Active {
		active = 1
	}
	_, err := db.Exec(`INSERT INTO xt_indicator_alerts (`+indicatorAlertColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.UserID, rec.Name, rec.Symbol, rec.Interval, rec.ConditionExpr,
		rec.Message, rec.CooldownMinutes, rec.LastTriggeredAt, rec.LastValue,
		active, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// GetByID 按 id 取任务；不存在返回 (nil, nil)。
func (r *IndicatorAlertRepo) GetByID(id string) (*IndicatorAlertRecord, error) {
	row := db.QueryRow(`SELECT `+indicatorAlertColumns+` FROM xt_indicator_alerts WHERE id = ?`, id)
	return scanIndicatorAlert(row)
}

// List 按过滤条件列表；limit=0 表示不限。user_id/symbol/interval/active 为允许过滤列。
func (r *IndicatorAlertRepo) List(filter map[string]any, limit int) ([]*IndicatorAlertRecord, error) {
	query := `SELECT ` + indicatorAlertColumns + ` FROM xt_indicator_alerts`
	allowedCols := map[string]bool{"user_id": true, "symbol": true, "interval": true, "active": true}
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
	var out []*IndicatorAlertRecord
	for rows.Next() {
		rec, err := scanIndicatorAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// ListActive 返回所有参与周期扫描的任务（active=1）。
func (r *IndicatorAlertRepo) ListActive() ([]*IndicatorAlertRecord, error) {
	return r.List(map[string]any{"active": 1}, 0)
}

// Update 全字段更新（命中字段除外），自动刷新 updated_at。
func (r *IndicatorAlertRepo) Update(rec *IndicatorAlertRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec.UpdatedAt = time.Now().UnixMilli()
	active := 0
	if rec.Active {
		active = 1
	}
	_, err := db.Exec(`UPDATE xt_indicator_alerts SET
		name=?, symbol=?, interval=?, condition_expr=?, message=?, cooldown_minutes=?,
		active=?, updated_at=? WHERE id=?`,
		rec.Name, rec.Symbol, rec.Interval, rec.ConditionExpr, rec.Message,
		rec.CooldownMinutes, active, rec.UpdatedAt, rec.ID)
	return err
}

// SetActive 启停开关（高频路径，避免全行写）。
func (r *IndicatorAlertRepo) SetActive(id string, active bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := 0
	if active {
		v = 1
	}
	_, err := db.Exec(`UPDATE xt_indicator_alerts SET active=?, updated_at=? WHERE id=?`,
		v, time.Now().UnixMilli(), id)
	return err
}

// MarkTriggered 记录一次命中：刷新最近触发时间与求值当前值。
func (r *IndicatorAlertRepo) MarkTriggered(id string, value float64, atMs int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := db.Exec(`UPDATE xt_indicator_alerts SET last_triggered_at=?, last_value=?, updated_at=? WHERE id=?`,
		atMs, value, atMs, id)
	return err
}

// Delete 删除任务。
func (r *IndicatorAlertRepo) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := db.Exec(`DELETE FROM xt_indicator_alerts WHERE id = ?`, id)
	return err
}
