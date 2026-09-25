// Package dataprovider 统一外部数据生态适配层（对标 QuantDinger data_providers /
// data_sources）：情绪（Fear&Greed、Coinglass 衍生品）、宏观（FRED）、新闻
// （CryptoCompare）、热力图（自算，无需 key）、经济日历（ForexFactory 周历 XML
// 镜像）。所有源共享同一套韧性件：每源限流（最小间隔）+ 熔断（连续失败开闸，
// 冷却后半开试探）+ TTL 缓存（内存 + SQLite 落地）+ 健康状态上报。
//
// Key 全部走环境变量；未配置的源状态 = not_configured，API 返回降级结构而不是
// 报错。Key 绝不进日志与响应：错误串统一过 sanitizeSecrets 抹除。
package dataprovider

import (
	"context"
	"strings"
	"sync"
	"time"
)

// ── 限流器（每源最小请求间隔） ──────────────────────────────────

// rateLimiter 保证同一源的两次上游请求至少间隔 minInterval。
// Allow 是非阻塞版（HTTP 请求路径用，被限流时回旧数据/refreshing）；
// Wait 是阻塞版（后台调度用，带 ctx 取消）。
type rateLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	last        time.Time
	now         func() time.Time
}

func newRateLimiter(minInterval time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{minInterval: minInterval, now: now}
}

// Allow 距上次请求满 minInterval 才放行并记录本次时间。
func (l *rateLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.now().Sub(l.last) < l.minInterval {
		return false
	}
	l.last = l.now()
	return true
}

// Wait 阻塞到放行或 ctx 取消。
func (l *rateLimiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		remain := l.minInterval - l.now().Sub(l.last)
		if remain <= 0 {
			l.last = l.now()
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		t := time.NewTimer(remain)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// ── 熔断器（closed → open → half_open） ────────────────────────

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

func (s breakerState) String() string {
	switch s {
	case breakerOpen:
		return "open"
	case breakerHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}

// circuitBreaker 连续失败 threshold 次开闸，冷却 cooldown 后进半开，
// 半开期间一次成功复位、一次失败重新开闸（对标 QuantDinger circuit_breaker.py）。
type circuitBreaker struct {
	mu          sync.Mutex
	threshold   int
	cooldown    time.Duration
	state       breakerState
	failures    int
	lastFailure time.Time
	now         func() time.Time
}

func newCircuitBreaker(threshold int, cooldown time.Duration, now func() time.Time) *circuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	return &circuitBreaker{threshold: threshold, cooldown: cooldown, now: now}
}

// Allow 熔断期间拒绝；冷却到点自动转半开放行一次试探。
func (b *circuitBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerClosed:
		return true
	case breakerOpen:
		if b.now().Sub(b.lastFailure) >= b.cooldown {
			b.state = breakerHalfOpen
			return true
		}
		return false
	case breakerHalfOpen:
		return true
	}
	return true
}

func (b *circuitBreaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = breakerClosed
	b.failures = 0
}

func (b *circuitBreaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	b.lastFailure = b.now()
	if b.state == breakerHalfOpen || b.failures >= b.threshold {
		b.state = breakerOpen
	}
}

func (b *circuitBreaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state.String()
}

func (b *circuitBreaker) Failures() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures
}

// ── 密钥脱敏 ────────────────────────────────────────────────────

// sanitizeSecrets 把错误串里出现的密钥值替换为 "***"——上游 http 错误
// （*url.Error）会带完整 URL（含 ?api_key=...），绝不能原样进日志/响应。
func sanitizeSecrets(s string, secrets []string) string {
	for _, k := range secrets {
		if k != "" {
			s = strings.ReplaceAll(s, k, "***")
		}
	}
	return s
}
