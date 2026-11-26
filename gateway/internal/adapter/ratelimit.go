package adapter

import (
	"sync"
	"time"
)

// Throttler 进程内最小请求间隔限流器：串行化同一交易所的出站请求，
// 避免突发打爆交易所 weight 预算。多实例部署时需配合交易所侧限频与
// 服务端配额，本组件只做单机节流。
type Throttler struct {
	minInterval time.Duration
	mu          sync.Mutex
	last        time.Time
}

// NewThrottler 创建限流器；minInterval 为两次请求之间的最小间隔。
func NewThrottler(minInterval time.Duration) *Throttler {
	return &Throttler{minInterval: minInterval}
}

// Wait 阻塞到距上次请求已满足最小间隔；首次调用立即放行。
func (t *Throttler) Wait() {
	if t == nil || t.minInterval <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if !t.last.IsZero() {
		if wait := t.minInterval - now.Sub(t.last); wait > 0 {
			time.Sleep(wait)
			now = time.Now()
		}
	}
	t.last = now
}
