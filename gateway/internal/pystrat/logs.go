package pystrat

import "sync"

// LogEntry 是一条运行日志（内存环形缓冲条目，不落表）。
type LogEntry struct {
	TS      int64  `json:"ts"`
	Level   string `json:"level"` // info / error / action
	Message string `json:"message"`
}

// ringLog 是每策略内存环形缓冲（容量固定，覆盖最旧），供 GET /logs 使用。
type ringLog struct {
	mu   sync.Mutex
	buf  []LogEntry
	size int
	next int
	full bool
}

func newRingLog(size int) *ringLog {
	if size <= 0 {
		size = 200
	}
	return &ringLog{buf: make([]LogEntry, size), size: size}
}

func (r *ringLog) append(e LogEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = e
	r.next = (r.next + 1) % r.size
	if r.next == 0 {
		r.full = true
	}
}

// snapshot 返回最新→最旧的所有条目。
func (r *ringLog) snapshot() []LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.next
	if r.full {
		n = r.size
	}
	out := make([]LogEntry, 0, n)
	for i := 0; i < n; i++ {
		idx := (r.next - 1 - i + r.size) % r.size
		out = append(out, r.buf[idx])
	}
	return out
}
