package store

import (
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
}

type NotificationRepo struct{ mu sync.RWMutex }

func NewNotificationRepo() *NotificationRepo { return &NotificationRepo{} }

func ListNotifications(limit, offset int, unreadOnly bool) ([]NotificationRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	query := `SELECT id, title, content, level, category, read, created_at FROM notifications WHERE 1=1`
	var args []any
	if unreadOnly {
		query += ` AND read = 0`
	}
	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
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
		if err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.Level, &n.Category, &readInt, &n.CreatedAt); err != nil {
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
	res, err := db.Exec(
		`INSERT INTO notifications (title, content, level, category, read, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		record.Title, record.Content, record.Level, record.Category, boolToInt(record.Read), record.CreatedAt,
	)
	if err != nil {
		return err
	}
	record.ID, _ = res.LastInsertId()
	return nil
}

func MarkNotificationRead(id int64) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE notifications SET read = 1 WHERE id = ?`, id)
	return err
}

func MarkAllNotificationsRead() error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE notifications SET read = 1`)
	return err
}

func ClearNotifications() error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`DELETE FROM notifications`)
	return err
}

func CountUnreadNotifications() (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database not initialized")
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE read = 0`).Scan(&count)
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
	Name      string   `json:"name"`
	Events    []string `json:"events"`
	Levels    []string `json:"levels"`
	Channels  []string `json:"channels"`
	Enabled   bool     `json:"enabled"`
	MinReturn float64  `json:"min_return_pct"`
}

func ListNotificationRoutes() ([]NotificationRouteRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := db.Query(`SELECT id, name, events, levels, channels, enabled, min_return_pct FROM notification_routes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NotificationRouteRecord
	for rows.Next() {
		var r NotificationRouteRecord
		var events, levels, channels string
		var enabled int
		if err := rows.Scan(&r.ID, &r.Name, &events, &levels, &channels, &enabled, &r.MinReturn); err != nil {
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
		`INSERT OR REPLACE INTO notification_routes (id, name, events, levels, channels, enabled, min_return_pct) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		route.ID, route.Name, string(events), string(levels), string(channels), boolToInt(route.Enabled), route.MinReturn,
	)
	return err
}

func DeleteNotificationRoute(id string) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`DELETE FROM notification_routes WHERE id = ?`, id)
	return err
}
