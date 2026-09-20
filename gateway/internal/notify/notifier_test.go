package notify

import (
	"bytes"
	"log"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeChannel is an in-memory Channel used to observe manager delivery.
type fakeChannel struct {
	name    string
	enabled bool
	calls   int32
}

func (f *fakeChannel) Name() string    { return f.name }
func (f *fakeChannel) IsEnabled() bool { return f.enabled }
func (f *fakeChannel) Send(msg Message) error {
	atomic.AddInt32(&f.calls, 1)
	return nil
}

func (f *fakeChannel) count() int { return int(atomic.LoadInt32(&f.calls)) }

func newTestManager(chs ...Channel) *Manager {
	m := &Manager{channels: make(map[string]Channel), queue: make(chan Message, 16)}
	for _, ch := range chs {
		m.Register(ch)
	}
	return m
}

func TestSendSyncChannelsTagSingleChannel(t *testing.T) {
	a := &fakeChannel{name: "a", enabled: true}
	b := &fakeChannel{name: "b", enabled: true}
	c := &fakeChannel{name: "c", enabled: true}
	m := newTestManager(a, b, c)

	msg := Message{Title: "t", Content: "c", Level: "INFO", Tags: map[string]string{"_channels": "b"}}
	if errs := m.SendSync(msg); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	if b.count() != 1 {
		t.Fatalf("b should receive exactly 1, got %d", b.count())
	}
	if a.count() != 0 || c.count() != 0 {
		t.Fatalf("a/c must not receive: a=%d c=%d", a.count(), c.count())
	}
}

func TestSendSyncChannelsTagSubset(t *testing.T) {
	a := &fakeChannel{name: "a", enabled: true}
	b := &fakeChannel{name: "b", enabled: true}
	c := &fakeChannel{name: "c", enabled: true}
	d := &fakeChannel{name: "d", enabled: true}
	m := newTestManager(a, b, c, d)

	msg := Message{Title: "t", Content: "c", Level: "INFO", Tags: map[string]string{"_channels": "a,c"}}
	if errs := m.SendSync(msg); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	if a.count() != 1 || c.count() != 1 {
		t.Fatalf("a,c should each receive 1: a=%d c=%d", a.count(), c.count())
	}
	if b.count() != 0 || d.count() != 0 {
		t.Fatalf("b,d must not receive: b=%d d=%d", b.count(), d.count())
	}
}

func TestSendSyncChannelsTagEmptyIntersectionWarns(t *testing.T) {
	a := &fakeChannel{name: "a", enabled: true}
	m := newTestManager(a)

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	msg := Message{Title: "ghost-msg", Content: "c", Level: "INFO", Tags: map[string]string{"_channels": "ghost"}}
	if errs := m.SendSync(msg); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	if a.count() != 0 {
		t.Fatalf("nothing should be delivered, got %d", a.count())
	}
	out := buf.String()
	if !strings.Contains(out, "WARN") || !strings.Contains(out, "_channels") {
		t.Fatalf("expected WARN log mentioning _channels, got %q", out)
	}
}

func TestSendSyncNoTagAllChannels(t *testing.T) {
	a := &fakeChannel{name: "a", enabled: true}
	b := &fakeChannel{name: "b", enabled: true}
	off := &fakeChannel{name: "off", enabled: false}
	m := newTestManager(a, b, off)

	// nil tags
	if errs := m.SendSync(Message{Title: "t", Content: "c", Level: "INFO"}); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// tags without _channels
	if errs := m.SendSync(Message{Title: "t", Content: "c", Level: "INFO", Tags: map[string]string{"symbol": "BTC"}}); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	if a.count() != 2 || b.count() != 2 {
		t.Fatalf("a,b should each receive 2: a=%d b=%d", a.count(), b.count())
	}
	if off.count() != 0 {
		t.Fatalf("disabled channel must not receive: off=%d", off.count())
	}
}

func TestSendSyncChannelsTagIgnoresUnregisteredNames(t *testing.T) {
	a := &fakeChannel{name: "a", enabled: true}
	b := &fakeChannel{name: "b", enabled: true}
	m := newTestManager(a, b)

	msg := Message{Title: "t", Content: "c", Level: "INFO", Tags: map[string]string{"_channels": "ghost,b, phantom "}}
	if errs := m.SendSync(msg); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	if b.count() != 1 {
		t.Fatalf("b should receive 1, got %d", b.count())
	}
	if a.count() != 0 {
		t.Fatalf("a must not receive: a=%d", a.count())
	}
}

func TestWorkerAppliesChannelsTag(t *testing.T) {
	a := &fakeChannel{name: "a", enabled: true}
	b := &fakeChannel{name: "b", enabled: true}
	m := newTestManager(a, b)
	m.wg.Add(1)
	go m.worker()
	defer close(m.queue)

	m.Send(Message{Title: "t", Content: "c", Level: "INFO", Tags: map[string]string{"_channels": "b"}})

	deadline := time.Now().Add(2 * time.Second)
	for b.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if b.count() != 1 {
		t.Fatalf("b should receive exactly 1 via async worker, got %d", b.count())
	}
	if a.count() != 0 {
		t.Fatalf("a must not receive: a=%d", a.count())
	}
}

func TestWorkerChannelsTagEmptyIntersectionSkips(t *testing.T) {
	a := &fakeChannel{name: "a", enabled: true}
	m := newTestManager(a)

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	m.wg.Add(1)
	go m.worker()
	defer close(m.queue)

	m.Send(Message{Title: "ghost-msg", Content: "c", Level: "INFO", Tags: map[string]string{"_channels": "ghost"}})

	// 等待 worker 处理完队列项
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(buf.String(), "WARN") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if a.count() != 0 {
		t.Fatalf("nothing should be delivered, got %d", a.count())
	}
	if !strings.Contains(buf.String(), "_channels") {
		t.Fatalf("expected WARN log mentioning _channels, got %q", buf.String())
	}
}
