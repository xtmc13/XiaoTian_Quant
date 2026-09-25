package dataprovider

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── 假时钟 ──

type fakeClock struct{ t atomic.Int64 } // unix 秒

func newFakeClock() *fakeClock { c := &fakeClock{}; c.t.Store(time.Now().Unix()); return c }
func (c *fakeClock) now() time.Time {
	return time.Unix(c.t.Load(), 0)
}
func (c *fakeClock) advance(d time.Duration) { c.t.Add(int64(d.Seconds())) }

// ── 限流器 ──

func TestRateLimiterAllow(t *testing.T) {
	clk := newFakeClock()
	l := newRateLimiter(5*time.Second, clk.now)

	if !l.Allow() {
		t.Fatal("first call should be allowed")
	}
	if l.Allow() {
		t.Fatal("second call within interval should be rejected")
	}
	clk.advance(3 * time.Second)
	if l.Allow() {
		t.Fatal("call before min interval should be rejected")
	}
	clk.advance(2 * time.Second)
	if !l.Allow() {
		t.Fatal("call after min interval should be allowed")
	}
}

func TestRateLimiterWait(t *testing.T) {
	clk := newFakeClock()
	l := newRateLimiter(50*time.Millisecond, clk.now)
	if !l.Allow() {
		t.Fatal("first allow should pass")
	}
	// ctx 取消时 Wait 返回错误
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	// fake clock 不前进 → 永远等不到 → ctx 超时
	if err := l.Wait(ctx); err == nil {
		t.Fatal("Wait should return ctx error on cancel")
	}
}

func TestRateLimiterWaitRealClock(t *testing.T) {
	l := newRateLimiter(30*time.Millisecond, time.Now)
	if !l.Allow() {
		t.Fatal("first allow should pass")
	}
	start := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond {
		t.Fatalf("Wait returned too early: %v", elapsed)
	}
}

// ── 熔断器 ──

func TestCircuitBreakerOpenAfterThreshold(t *testing.T) {
	clk := newFakeClock()
	b := newCircuitBreaker(3, 5*time.Minute, clk.now)

	for i := 0; i < 2; i++ {
		b.RecordFailure()
		if b.State() != "closed" {
			t.Fatalf("state after %d failures should be closed, got %s", i+1, b.State())
		}
	}
	b.RecordFailure()
	if b.State() != "open" {
		t.Fatalf("state after threshold should be open, got %s", b.State())
	}
	if b.Allow() {
		t.Fatal("open breaker should reject calls")
	}

	// 冷却期内仍拒绝
	clk.advance(time.Minute)
	if b.Allow() {
		t.Fatal("open breaker within cooldown should reject")
	}

	// 冷却结束 → 半开放行
	clk.advance(4*time.Minute + time.Second)
	if !b.Allow() {
		t.Fatal("breaker should half-open after cooldown")
	}
	if b.State() != "half_open" {
		t.Fatalf("expected half_open, got %s", b.State())
	}

	// 半开成功 → 复位
	b.RecordSuccess()
	if b.State() != "closed" || b.Failures() != 0 {
		t.Fatalf("half-open success should reset: state=%s failures=%d", b.State(), b.Failures())
	}
}

func TestCircuitBreakerHalfOpenFailureReopens(t *testing.T) {
	clk := newFakeClock()
	b := newCircuitBreaker(1, time.Minute, clk.now)

	b.RecordFailure() // threshold=1 → 立即开闸
	if b.State() != "open" {
		t.Fatalf("expected open, got %s", b.State())
	}
	clk.advance(61 * time.Second)
	if !b.Allow() {
		t.Fatal("should half-open after cooldown")
	}
	b.RecordFailure() // 半开失败 → 重新开闸
	if b.State() != "open" {
		t.Fatalf("half-open failure should reopen, got %s", b.State())
	}
	if b.Allow() {
		t.Fatal("reopened breaker should reject")
	}
}

// ── 脱敏 ──

func TestSanitizeSecrets(t *testing.T) {
	msg := `Get "https://api.stlouisfed.org/fred/series/observations?series_id=FEDFUNDS&api_key=SECRETKEY123&file_type=json": dial tcp: timeout`
	got := sanitizeSecrets(msg, []string{"SECRETKEY123", ""})
	if got == msg {
		t.Fatal("secret should be scrubbed")
	}
	if !strings.Contains(got, "api_key=***") {
		t.Fatalf("expected scrubbed api_key in message, got: %s", got)
	}
	if strings.Contains(got, "SECRETKEY123") {
		t.Fatalf("secret still present: %s", got)
	}
}
