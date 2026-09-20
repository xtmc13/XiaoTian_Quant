package pystrat

import "testing"

func TestRingLogOverwrite(t *testing.T) {
	r := newRingLog(3)
	r.append(LogEntry{TS: 1, Message: "a"})
	r.append(LogEntry{TS: 2, Message: "b"})
	r.append(LogEntry{TS: 3, Message: "c"})
	r.append(LogEntry{TS: 4, Message: "d"}) // 覆盖 a

	snap := r.snapshot()
	if len(snap) != 3 {
		t.Fatalf("len=%d want 3", len(snap))
	}
	if snap[0].Message != "d" || snap[1].Message != "c" || snap[2].Message != "b" {
		t.Fatalf("order wrong (newest first): %+v", snap)
	}
}

func TestRingLogPartial(t *testing.T) {
	r := newRingLog(5)
	r.append(LogEntry{TS: 1, Message: "x"})
	snap := r.snapshot()
	if len(snap) != 1 || snap[0].Message != "x" {
		t.Fatalf("partial snapshot wrong: %+v", snap)
	}
}
