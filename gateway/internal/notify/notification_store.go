package notify

import (
	"sync"
	"time"
)

// Notification is a persisted user-facing notification.
type Notification struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Level     string `json:"level"` // INFO, WARN, CRITICAL
	Category  string `json:"category"` // signal, risk, trade, system
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"created_at"`
}

// NotificationStore provides in-memory notification persistence.
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
	return count
}

// Clear removes all notifications.
func (s *NotificationStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = s.items[:0]
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
