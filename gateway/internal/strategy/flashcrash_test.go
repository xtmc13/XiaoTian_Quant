package strategy

import (
	"testing"
	"time"
)

func TestFlashCrashDetectorIgnoresNonPositivePrices(t *testing.T) {
	d := NewFlashCrashDetector()
	now := time.Now().UnixMilli()
	d.AddPrice(100, now-1000)
	d.AddPrice(0, now-500)
	d.AddPrice(-5, now-400)
	d.AddPrice(99.5, now)
	if d.IsFlashCrash() {
		t.Fatal("non-positive frames must not read as a crash")
	}
	if got := len(d.History()); got != 2 {
		t.Fatalf("history must only keep positive prices, got %d", got)
	}
}

// 回归：滑动窗口驱逐曾把 Window(60s) 误用为 60ms（Seconds() 直接 int64），
// 检测窗口实际只有 60 毫秒。修复后按文档语义保留一分钟。
func TestFlashCrashDetectorWindowIsOneMinuteNotMillis(t *testing.T) {
	d := NewFlashCrashDetector()
	now := time.Now().UnixMilli()
	d.AddPrice(100, now-59_000)
	d.AddPrice(99, now-1_000)
	d.AddPrice(98, now)
	if got := len(d.History()); got != 3 {
		t.Fatalf("one-minute window must keep 3 points, got %d", got)
	}
	d.AddPrice(97, now+61_000)
	if got := len(d.History()); got != 1 {
		t.Fatalf("points older than window must be evicted, got %d", got)
	}
}
