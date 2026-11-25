package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
)

// ── Notification Repository ──

type NotificationRecord struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Level     string `json:"level"`
	Category  string `json:"category"`
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"created_at"`
	UserID    int64  `json:"user_id"` // 属主用户（0=系统广播，所有人可见），H7 越权修复
}

type NotificationRepo struct{ mu sync.RWMutex }

func NewNotificationRepo() *NotificationRepo { return &NotificationRepo{} }

func ListNotifications(limit, offset int, unreadOnly bool) ([]NotificationRecord, error) {
	return listNotificationsFiltered(limit, offset, unreadOnly, 0, false)
}

// ListNotificationsForUser 返回当前用户可见的通知：系统广播(user_id=0) + 本人的。
func ListNotificationsForUser(limit, offset int, unreadOnly bool, userID int64) ([]NotificationRecord, error) {
	return listNotificationsFiltered(limit, offset, unreadOnly, userID, true)
}

func listNotificationsFiltered(limit, offset int, unreadOnly bool, userID int64, filterUser bool) ([]NotificationRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT id, title, content, level, category, read, created_at, user_id FROM notifications WHERE 1=1`
	var args []any
	if filterUser {
		query += ` AND (user_id = 0 OR user_id = $1)`
		args = append(args, userID)
	}
	if unreadOnly {
		query += ` AND read = 0`
	}
	query += ` ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	args = append(args, limit, offset)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NotificationRecord
	for rows.Next() {
		var n NotificationRecord
		var readInt int
		if err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.Level, &n.Category, &readInt, &n.CreatedAt, &n.UserID); err != nil {
			return nil, err
		}
		n.Read = readInt != 0
		result = append(result, n)
	}
	return result, nil
}

func AddNotification(record *NotificationRecord) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	err := db.QueryRow(
		`INSERT INTO notifications (title, content, level, category, read, created_at, user_id) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		record.Title, record.Content, record.Level, record.Category, boolToInt(record.Read), record.CreatedAt, record.UserID,
	).Scan(&record.ID)
	if err != nil {
		return err
	}
	return nil
}

func MarkNotificationRead(id int64) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE notifications SET read = 1 WHERE id = $1`, id)
	return err
}

// MarkNotificationReadForUser 只标记系统广播或本人通知已读（H7），
// 返回是否有行被更新。
func MarkNotificationReadForUser(id, userID int64) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("database not initialized")
	}
	res, err := db.Exec(`UPDATE notifications SET read = 1 WHERE id = $1 AND (user_id = 0 OR user_id = $2)`, id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func MarkAllNotificationsRead() error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE notifications SET read = 1`)
	return err
}

// MarkAllNotificationsReadForUser 只标记系统广播或本人通知已读（H7）。
func MarkAllNotificationsReadForUser(userID int64) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE notifications SET read = 1 WHERE user_id = 0 OR user_id = $1`, userID)
	return err
}

func ClearNotifications() error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`DELETE FROM notifications`)
	return err
}

// ClearNotificationsForUser 只删除本人通知（H7）；系统广播保留。
func ClearNotificationsForUser(userID int64) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`DELETE FROM notifications WHERE user_id = $1`, userID)
	return err
}

// CountUnreadNotificationsForUser 返回当前用户的未读数（系统广播 + 本人）。
func CountUnreadNotificationsForUser(userID int64) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database not initialized")
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE read = 0 AND (user_id = 0 OR user_id = $1)`, userID).Scan(&count)
	return count, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ── Notification Route Repository ──

type NotificationRouteRecord struct {
	ID        string   `json:"id"`
	UserID    int64    `json:"user_id"`
	Name      string   `json:"name"`
	Events    []string `json:"events"`
	Levels    []string `json:"levels"`
	Channels  []string `json:"channels"`
	Enabled   bool     `json:"enabled"`
	MinReturn float64  `json:"min_return_pct"`
}

// ListNotificationRoutes 返回系统级路由规则（user_id=0）。
// 通知广播引擎（notify.Router）只消费系统规则，保持全局路由语义不变。
func ListNotificationRoutes() ([]NotificationRouteRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := db.Query(`SELECT id, user_id, name, events, levels, channels, enabled, min_return_pct FROM notification_routes WHERE user_id = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNotificationRouteRows(rows)
}

// ListNotificationRoutesForUser 返回当前用户可见的规则：系统规则（user_id=0，
// 所有人可见）+ 本人创建的规则。
func ListNotificationRoutesForUser(userID int64) ([]NotificationRouteRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := db.Query(`SELECT id, user_id, name, events, levels, channels, enabled, min_return_pct FROM notification_routes WHERE user_id = 0 OR user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNotificationRouteRows(rows)
}

// GetNotificationRouteOwner 返回路由规则的属主（user_id=0 表示系统规则）。
func GetNotificationRouteOwner(id string) (int64, bool) {
	if db == nil {
		return 0, false
	}
	var userID int64
	err := db.QueryRow(`SELECT user_id FROM notification_routes WHERE id = $1`, id).Scan(&userID)
	if err != nil {
		return 0, false
	}
	return userID, true
}

func scanNotificationRouteRows(rows *sql.Rows) ([]NotificationRouteRecord, error) {
	var result []NotificationRouteRecord
	for rows.Next() {
		var r NotificationRouteRecord
		var events, levels, channels string
		var enabled int
		if err := rows.Scan(&r.ID, &r.UserID, &r.Name, &events, &levels, &channels, &enabled, &r.MinReturn); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		_ = json.Unmarshal([]byte(events), &r.Events)
		_ = json.Unmarshal([]byte(levels), &r.Levels)
		_ = json.Unmarshal([]byte(channels), &r.Channels)
		result = append(result, r)
	}
	return result, nil
}

func SaveNotificationRoute(route *NotificationRouteRecord) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	events, _ := json.Marshal(route.Events)
	levels, _ := json.Marshal(route.Levels)
	channels, _ := json.Marshal(route.Channels)
	_, err := db.Exec(
		`INSERT INTO notification_routes (id, user_id, name, events, levels, channels, enabled, min_return_pct) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(id) DO UPDATE SET user_id=excluded.user_id, name=excluded.name, events=excluded.events, levels=excluded.levels, channels=excluded.channels, enabled=excluded.enabled, min_return_pct=excluded.min_return_pct`,
		route.ID, route.UserID, route.Name, string(events), string(levels), string(channels), boolToInt(route.Enabled), route.MinReturn,
	)
	return err
}

func DeleteNotificationRoute(id string) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`DELETE FROM notification_routes WHERE id = $1`, id)
	return err
}
