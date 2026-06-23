package notify

import (
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// Notification is a persisted user-facing notification.
type Notification struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Level     string `json:"level"`    // INFO, WARN, CRITICAL
	Category  string `json:"category"` // signal, risk, trade, system
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"created_at"`
}

// NotificationStore provides in-memory notification persistence backed by SQLite.
type NotificationStore struct {
	mu      sync.RWMutex
	items   []*Notification
	nextID  int64
	maxSize int
}

var (
	notifStore     *NotificationStore
	notifStoreOnce sync.Once
)

// GetNotificationStore returns the global notification store.
func GetNotificationStore() *NotificationStore {
	notifStoreOnce.Do(func() {
		notifStore = NewNotificationStore(500)
		notifStore.loadFromDB()
	})
	return notifStore
}

// NewNotificationStore creates a new notification store.
func NewNotificationStore(maxSize int) *NotificationStore {
	if maxSize <= 0 {
		maxSize = 500
	}
	return &NotificationStore{
		items:   make([]*Notification, 0),
		maxSize: maxSize,
	}
}

// loadFromDB loads persisted notifications into memory.
func (s *NotificationStore) loadFromDB() {
	records, err := store.ListNotifications(s.maxSize, 0, false)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range records {
		n := &Notification{
			ID:        r.ID,
			Title:     r.Title,
			Content:   r.Content,
			Level:     r.Level,
			Category:  r.Category,
			Read:      r.Read,
			CreatedAt: r.CreatedAt,
		}
		s.items = append(s.items, n)
		if n.ID > s.nextID {
			s.nextID = n.ID
		}
	}
}

// Add creates and stores a new notification.
func (s *NotificationStore) Add(title, content, level, category string) *Notification {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	n := &Notification{
		ID:        s.nextID,
		Title:     title,
		Content:   content,
		Level:     level,
		Category:  category,
		Read:      false,
		CreatedAt: time.Now().UnixMilli(),
	}

	s.items = append(s.items, n)
	if len(s.items) > s.maxSize {
		s.items = s.items[len(s.items)-s.maxSize:]
	}

	// Persist to SQLite asynchronously to avoid blocking callers.
	go func(item *Notification) {
		_ = store.AddNotification(&store.NotificationRecord{
			ID:        item.ID,
			Title:     item.Title,
			Content:   item.Content,
			Level:     item.Level,
			Category:  item.Category,
			Read:      item.Read,
			CreatedAt: item.CreatedAt,
		})
	}(n)

	return n
}

// List returns recent notifications, newest first.
func (s *NotificationStore) List(limit, offset int, unreadOnly bool) []*Notification {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Notification
	for i := len(s.items) - 1; i >= 0; i-- {
		n := s.items[i]
		if unreadOnly && n.Read {
			continue
		}
		if offset > 0 {
			offset--
			continue
		}
		result = append(result, n)
		if len(result) >= limit {
			break
		}
	}
	return result
}

// MarkRead marks a notification as read.
func (s *NotificationStore) MarkRead(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.items {
		if n.ID == id {
			n.Read = true
			go func() { _ = store.MarkNotificationRead(id) }()
			return true
		}
	}
	return false
}

// MarkAllRead marks all notifications as read.
func (s *NotificationStore) MarkAllRead() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, n := range s.items {
		if !n.Read {
			n.Read = true
			count++
		}
	}
	go func() { _ = store.MarkAllNotificationsRead() }()
	return count
}

// Clear removes all notifications.
func (s *NotificationStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = s.items[:0]
	s.nextID = 0
	go func() { _ = store.ClearNotifications() }()
}

// UnreadCount returns the number of unread notifications.
func (s *NotificationStore) UnreadCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, n := range s.items {
		if !n.Read {
			count++
		}
	}
	return count
}

// Total returns the total number of notifications.
func (s *NotificationStore) Total() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}
