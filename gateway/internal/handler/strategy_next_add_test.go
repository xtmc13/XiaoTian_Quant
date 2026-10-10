package handler

import (
	"math"
	"testing"
)

// ── H3（2026-10-10）：下一档补仓距离与引擎新档位口径同源 ──
//
// 引擎口径：order=N 对应第 N 次补仓；filled_orders=PositionCount 含首单，
// 已消费档位是 order<filled，下一档=order==filled。修复前此处按
// order<=filled 跳过（把首单占的序号当成已消费的第 1 档），首单刚成交时
// 展示的"下一档"实际是 order=2——与前端第 1 档配置对不上。

func TestComputeNextAddDistanceNewLadderConvention(t *testing.T) {
	status := map[string]any{
		"in_position":     true,
		"filled_orders":   1.0, // 首单已成交，尚无补仓
		"avg_entry_price": 100.0,
		"direction":       "long",
	}
	config := map[string]any{
		"add_positions": []any{
			map[string]any{"order": 1.0, "spread": 0.05},
			map[string]any{"order": 2.0, "spread": 0.10},
		},
	}
	// 现价 97：order=1 档触发价 95（距离 ≈2.06%）是下一档；修复前跳过
	// order 1 会错报 order=2 档的 90（≈7.22%）。
	d, ok := computeNextAddDistance(status, config, 97)
	if !ok {
		t.Fatal("distance must be reported")
	}
	if want := (97 - 95.0) / 97 * 100; math.Abs(d-want) > 1e-9 {
		t.Fatalf("next add distance = %v, want %v（order=1 档，首单成交后第 1 次补仓）", d, want)
	}

	// 第 1 次补仓成交后（filled=2）：order=1 已消费，下一档是 order=2。
	status["filled_orders"] = 2.0
	d, ok = computeNextAddDistance(status, config, 97)
	if !ok {
		t.Fatal("distance must be reported after first add")
	}
	if want := (97 - 90.0) / 97 * 100; math.Abs(d-want) > 1e-9 {
		t.Fatalf("next add distance = %v, want %v（order=2 档）", d, want)
	}
}
