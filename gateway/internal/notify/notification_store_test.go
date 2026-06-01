package notify

import (
	"testing"
)

func TestNotificationStoreNew(t *testing.T) {
	s := NewNotificationStore(100)
	if s == nil {
		t.Fatal("nil store")
	}
	if s.Total() != 0 {
		t.Fatal("empty initially")
	}
}

func TestNotificationStoreAdd(t *testing.T) {
	s := NewNotificationStore(100)
	n := s.Add("Test", "Content", "INFO", "system")
	if n == nil {
		t.Fatal("nil notification")
	}
	if n.ID != 1 {
		t.Fatal("first ID should be 1")
	}
	if n.Read {
		t.Fatal("new should be unread")
	}
	if n.Title != "Test" {
		t.Fatal("title mismatch")
	}
	if s.Total() != 1 {
		t.Fatal("total should be 1")
	}
	if s.UnreadCount() != 1 {
		t.Fatal("unread should be 1")
	}
}

func TestNotificationStoreList(t *testing.T) {
	s := NewNotificationStore(100)
	s.Add("A", "", "INFO", "")
	s.Add("B", "", "WARN", "")
	s.Add("C", "", "CRITICAL", "")

	items := s.List(10, 0, false)
	if len(items) != 3 {
		t.Fatalf("expected 3, got %d", len(items))
	}
	// Newest first
	if items[0].Title != "C" {
		t.Fatalf("expected C first, got %s", items[0].Title)
	}
	if items[2].Title != "A" {
		t.Fatalf("expected A last, got %s", items[2].Title)
	}
}

func TestNotificationStoreListUnreadOnly(t *testing.T) {
	s := NewNotificationStore(100)
	s.Add("A", "", "INFO", "")
	s.Add("B", "", "WARN", "")
	s.MarkRead(1) // Mark "A" as read

	items := s.List(10, 0, true) // unread only
	if len(items) != 1 {
		t.Fatalf("expected 1 unread, got %d", len(items))
	}
	if items[0].Title != "B" {
		t.Fatalf("expected B, got %s", items[0].Title)
	}
}

func TestNotificationStoreMarkRead(t *testing.T) {
	s := NewNotificationStore(100)
	s.Add("A", "", "INFO", "")

	if !s.MarkRead(1) {
		t.Fatal("mark read should return true")
	}
	if s.MarkRead(999) {
		t.Fatal("mark read non-existent should return false")
	}
	if s.UnreadCount() != 0 {
		t.Fatal("unread should be 0 after marking read")
	}
}

func TestNotificationStoreMarkAllRead(t *testing.T) {
	s := NewNotificationStore(100)
	s.Add("A", "", "", "")
	s.Add("B", "", "", "")
	s.Add("C", "", "", "")

	count := s.MarkAllRead()
	if count != 3 {
		t.Fatalf("expected 3 marked, got %d", count)
	}
	if s.UnreadCount() != 0 {
		t.Fatal("unread should be 0")
	}
}

func TestNotificationStoreClear(t *testing.T) {
	s := NewNotificationStore(100)
	s.Add("A", "", "", "")
	s.Add("B", "", "", "")
	s.Clear()

	if s.Total() != 0 {
		t.Fatal("total should be 0 after clear")
	}
	if len(s.List(10, 0, false)) != 0 {
		t.Fatal("list should be empty")
	}
}

func TestNotificationStoreMaxSize(t *testing.T) {
	s := NewNotificationStore(3)
	s.Add("1", "", "", "")
	s.Add("2", "", "", "")
	s.Add("3", "", "", "")
	s.Add("4", "", "", "") // should evict oldest

	if s.Total() != 3 {
		t.Fatalf("capped at 3, got %d", s.Total())
	}
	items := s.List(10, 0, false)
	if items[2].Title != "2" {
		t.Fatalf("oldest (1) evicted, got %s", items[2].Title)
	}
}

func TestNotificationStoreOffset(t *testing.T) {
	s := NewNotificationStore(100)
	s.Add("A", "", "", "")
	s.Add("B", "", "", "")
	s.Add("C", "", "", "")

	items := s.List(2, 0, false)
	if len(items) != 2 {
		t.Fatalf("limit 2, got %d", len(items))
	}

	items2 := s.List(2, 1, false) // skip 1
	if len(items2) != 2 {
		t.Fatalf("offset 1 limit 2, got %d", len(items2))
	}
}

func TestNotificationLevel(t *testing.T) {
	s := NewNotificationStore(100)
	n := s.Add("Critical Alert", "System failure", "CRITICAL", "risk")
	if n.Level != "CRITICAL" {
		t.Fatal("level should be CRITICAL")
	}
	if n.Category != "risk" {
		t.Fatal("category should be risk")
	}
}

func TestGlobalStore(t *testing.T) {
	s := GetNotificationStore()
	if s == nil {
		t.Fatal("global store should not be nil")
	}
}
