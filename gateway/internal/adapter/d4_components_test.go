package adapter

import (
	"errors"
	"testing"
	"time"
)

func TestRetryPolicyDelayGrowsAndCaps(t *testing.T) {
	p := DefaultRetryPolicy()
	d0 := p.Delay(0)
	if d0 < p.BaseDelay*9/10 || d0 > p.BaseDelay*11/10 {
		t.Fatalf("first delay should be ~base, got %v", d0)
	}
	d3 := p.Delay(3)
	if d3 > p.MaxDelay {
		t.Fatalf("delay should cap at %v, got %v", p.MaxDelay, d3)
	}
	// 未封顶时指数增长：delay(1) >= delay(0)。
	if p.Delay(1) < p.Delay(0) {
		t.Fatal("backoff should grow before cap")
	}
}

func TestRetryPolicyShouldRetry(t *testing.T) {
	p := DefaultRetryPolicy()
	if !p.ShouldRetry(errors.New("binance POST /order: HTTP 429 throttled")) {
		t.Fatal("429 should retry")
	}
	if !p.ShouldRetry(errors.New("binance POST /order: HTTP 503 service unavailable")) {
		t.Fatal("5xx should retry")
	}
	if p.ShouldRetry(errors.New("binance POST /order: HTTP 400 code=-2010 insufficient balance")) {
		t.Fatal("4xx must not retry")
	}
	if p.ShouldRetry(nil) {
		t.Fatal("nil must not retry")
	}
}

func TestThrottlerEnforcesMinInterval(t *testing.T) {
	th := NewThrottler(15 * time.Millisecond)
	th.Wait()
	start := time.Now()
	th.Wait()
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("second Wait should block at least 15ms, got %v", elapsed)
	}
}

func TestThrottlerNilSafe(t *testing.T) {
	var th *Throttler
	th.Wait() // 不应 panic
}

func TestParseServerTimeMs(t *testing.T) {
	body := []byte(`{"serverTime":1700000000123}`)
	ms, err := ParseServerTimeMs(body)
	if err != nil {
		t.Fatal(err)
	}
	if ms != 1700000000123 {
		t.Fatalf("got %d", ms)
	}
	ms0, err0 := ParseServerTimeMs([]byte(`{}`))
	if err0 != nil || ms0 != 0 {
		t.Fatalf("missing serverTime should parse to 0, got ms=%d err=%v", ms0, err0)
	}
}

func TestCheckClockSkew(t *testing.T) {
	// 本地慢 800ms，未超 1000ms 阈值。
	off, exceeded := CheckClockSkew(1700000000000, 1699999999200, 0)
	if off != 800 {
		t.Fatalf("offset=%d", off)
	}
	if exceeded {
		t.Fatal("800ms skew should not exceed threshold")
	}
	// 本地快 1500ms，超限。
	off, exceeded = CheckClockSkew(1699999998500, 1700000000000, 0)
	if off != -1500 || !exceeded {
		t.Fatalf("off=%d exceeded=%v", off, exceeded)
	}
}
