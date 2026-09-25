package store

import (
	"database/sql"
	"sync"
	"time"
)

// ── Alertmanager 告警事件 Repository ──
// 表结构见 migrations/sql/0033_alert_events.sql。
// fingerprint 唯一约束承载跨重启去重状态（notified_firing/notified_resolved）。

// AlertEventRecord 是 xt_alert_events 的行记录。
type AlertEventRecord struct {
	ID               int64  `json:"id"`
	Fingerprint      string `json:"fingerprint"`
	AlertName        string `json:"alertname"`
	Status           string `json:"status"` // firing | resolved
	Severity         string `json:"severity"`
	Summary          string `json:"summary"`
	Description      string `json:"description"`
	LabelsJSON       string `json:"-"`
	AnnotationsJSON  string `json:"-"`
	StartsAt         int64  `json:"starts_at"`
	EndsAt           int64  `json:"ends_at"`
	NotifiedFiring   bool   `json:"notified_firing"`
	NotifiedResolved bool   `json:"notified_resolved"`
	FirstSeen        int64  `json:"first_seen"`
	LastSeen         int64  `json:"last_seen"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
}

// AlertEventRepo provides typed access to xt_alert_events。
type AlertEventRepo struct{ mu sync.RWMutex }

func NewAlertEventRepo() *AlertEventRepo { return &AlertEventRepo{} }

const alertEventColumns = `id, fingerprint, alertname, status, severity, summary, description,
	labels_json, annotations_json, starts_at, ends_at, notified_firing, notified_resolved,
	first_seen, last_seen, created_at, updated_at`

func scanAlertEvent(s rowScanner) (*AlertEventRecord, error) {
	var rec AlertEventRecord
	var nf, nr int
	err := s.Scan(&rec.ID, &rec.Fingerprint, &rec.AlertName, &rec.Status, &rec.Severity,
		&rec.Summary, &rec.Description, &rec.LabelsJSON, &rec.AnnotationsJSON,
		&rec.StartsAt, &rec.EndsAt, &nf, &nr,
		&rec.FirstSeen, &rec.LastSeen, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.NotifiedFiring = nf != 0
	rec.NotifiedResolved = nr != 0
	return &rec, nil
}

// GetByFingerprint 按 fingerprint 取事件；不存在返回 (nil, nil)。
func (r *AlertEventRepo) GetByFingerprint(fp string) (*AlertEventRecord, error) {
	row := db.QueryRow(`SELECT `+alertEventColumns+` FROM xt_alert_events WHERE fingerprint = ?`, fp)
	return scanAlertEvent(row)
}

// Upsert 按 fingerprint 插入或更新事件（唯一索引承载并发去重），自动刷新 updated_at。
func (r *AlertEventRepo) Upsert(rec *AlertEventRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	nf, nr := 0, 0
	if rec.NotifiedFiring {
		nf = 1
	}
	if rec.NotifiedResolved {
		nr = 1
	}
	_, err := db.Exec(`INSERT INTO xt_alert_events (`+alertEventColumns+`)
		VALUES (NULL,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			alertname=excluded.alertname, status=excluded.status, severity=excluded.severity,
			summary=excluded.summary, description=excluded.description,
			labels_json=excluded.labels_json, annotations_json=excluded.annotations_json,
			starts_at=excluded.starts_at, ends_at=excluded.ends_at,
			notified_firing=excluded.notified_firing, notified_resolved=excluded.notified_resolved,
			last_seen=excluded.last_seen, updated_at=excluded.updated_at`,
		rec.Fingerprint, rec.AlertName, rec.Status, rec.Severity, rec.Summary, rec.Description,
		rec.LabelsJSON, rec.AnnotationsJSON, rec.StartsAt, rec.EndsAt, nf, nr,
		rec.FirstSeen, rec.LastSeen, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// ListActive 返回当前仍在 firing 的事件（已推 firing 且未推 resolved）。
func (r *AlertEventRepo) ListActive(limit int) ([]*AlertEventRecord, error) {
	return r.listWhere(`WHERE notified_firing = 1 AND notified_resolved = 0`, limit)
}

// ListRecent 按 last_seen 倒序返回最近事件流水（firing 与 resolved 均在列）。
func (r *AlertEventRepo) ListRecent(limit int) ([]*AlertEventRecord, error) {
	return r.listWhere(``, limit)
}

func (r *AlertEventRepo) listWhere(where string, limit int) ([]*AlertEventRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(`SELECT `+alertEventColumns+` FROM xt_alert_events `+where+`
		ORDER BY last_seen DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AlertEventRecord
	for rows.Next() {
		rec, err := scanAlertEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
