package community

import (
	"testing"
)

func TestPickLocalized(t *testing.T) {
	// Empty payload
	if got := PickLocalized("hello", "", "zh-CN", "en-US"); got != "hello" {
		t.Errorf("empty payload: got %q", got)
	}

	// Exact match
	payload := `{"zh-CN":"你好","en-US":"hello"}`
	if got := PickLocalized("fallback", payload, "zh-CN", "en-US"); got != "你好" {
		t.Errorf("exact match: got %q", got)
	}

	// Prefix match (zh-HK -> zh-CN)
	if got := PickLocalized("fallback", payload, "zh-HK", "en-US"); got != "你好" {
		t.Errorf("prefix match: got %q", got)
	}

	// English fallback
	if got := PickLocalized("fallback", payload, "ja-JP", "en-US"); got != "hello" {
		t.Errorf("en fallback: got %q", got)
	}

	// Source fallback
	payload2 := `{"de-DE":"Hallo"}`
	if got := PickLocalized("fallback", payload2, "ja-JP", "de-DE"); got != "Hallo" {
		t.Errorf("source fallback: got %q", got)
	}

	// Invalid JSON
	if got := PickLocalized("fallback", "invalid", "zh-CN", "en-US"); got != "fallback" {
		t.Errorf("invalid json: got %q", got)
	}
}

func TestDetectSourceLanguage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "en-US"},
		{"Hello world", "en-US"},
		{"你好世界", "zh-CN"},
		{"こんにちは", "ja-JP"},
		{"안녕하세요", "ko-KR"},
		{"مرحبا", "ar-SA"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := DetectSourceLanguage(tt.input); got != tt.want {
				t.Errorf("DetectSourceLanguage(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCoalesceString(t *testing.T) {
	if got := coalesceString("", "b", "c"); got != "b" {
		t.Errorf("coalesce = %q, want b", got)
	}
	if got := coalesceString("a", "b"); got != "a" {
		t.Errorf("coalesce = %q, want a", got)
	}
	if got := coalesceString("", ""); got != "" {
		t.Errorf("coalesce = %q, want empty", got)
	}
}

func TestMaxMin(t *testing.T) {
	if max(1, 2) != 2 {
		t.Error("max(1,2) should be 2")
	}
	if min(1, 2) != 1 {
		t.Error("min(1,2) should be 1")
	}
}

func TestExtractUniqueAuthorIDs(t *testing.T) {
	items := []MarketIndicator{
		{AuthorID: 1},
		{AuthorID: 2},
		{AuthorID: 1},
		{AuthorID: 3},
	}
	ids := extractUniqueAuthorIDs(items)
	if len(ids) != 3 {
		t.Fatalf("len(ids) = %d, want 3", len(ids))
	}
	seen := make(map[int]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate id: %d", id)
		}
		seen[id] = true
	}
}

func TestComputeWilsonScore(t *testing.T) {
	// Perfect 5-star with 100 ratings
	score := ComputeWilsonScore(5.0, 100)
	if score < 4.5 {
		t.Errorf("perfect rating with 100 reviews should have high Wilson score, got %.2f", score)
	}

	// 3-star with 10 ratings (less reliable)
	score2 := ComputeWilsonScore(3.0, 10)
	if score2 > 3.5 {
		t.Errorf("3-star with few reviews should have lower Wilson score, got %.2f", score2)
	}

	// No ratings
	score3 := ComputeWilsonScore(0, 0)
	if score3 != 0 {
		t.Errorf("no ratings should score 0, got %.2f", score3)
	}

	// 4-star with 1000 ratings (very reliable)
	score4 := ComputeWilsonScore(4.0, 1000)
	if score4 < 3.8 || score4 > 4.0 {
		t.Errorf("4-star with 1000 reviews should be close to 4.0, got %.2f", score4)
	}
}

func TestRevenueShareConfig(t *testing.T) {
	cfg := DefaultRevenueShareConfig()
	if cfg.AuthorPct != 0.70 {
		t.Errorf("expected author 70%%, got %.2f", cfg.AuthorPct)
	}
	if cfg.PlatformPct != 0.30 {
		t.Errorf("expected platform 30%%, got %.2f", cfg.PlatformPct)
	}
}

func TestReviewStatusConstants(t *testing.T) {
	if ReviewPending != "pending" {
		t.Error("ReviewPending should be 'pending'")
	}
	if ReviewApproved != "approved" {
		t.Error("ReviewApproved should be 'approved'")
	}
	if ReviewRejected != "rejected" {
		t.Error("ReviewRejected should be 'rejected'")
	}
}
