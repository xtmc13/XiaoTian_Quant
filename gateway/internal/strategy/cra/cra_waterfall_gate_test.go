package cra

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── M6（2026-10-10）：防瀑布覆盖任意币种 + 暂停期挡首单/新循环 ──
//
// 修复前双重漏洞：tick 唯一来源是 WS 启动列表的 5 个硬编码币种（其它
// symbol 的 waterfall_* 参数永不触发），且 WaterfallPaused 只挡补仓分支，
// 首单/新循环在瀑布暂停期照样入场接刀。此处用硬编码列表外的币种 +
// 直接 OnTick 喂价（mock tick 源）验证端到端行为。

func TestCRAWaterfallBlocksFirstOrderAnySymbol(t *testing.T) {
	start := func() *BaseCRAStrategy {
		s := NewCRASpotStrategy("martin_trend", "PEPEUSDT")
		if err := s.Start(map[string]any{
			"symbol":               "PEPEUSDT",
			"first_order_amount":   100,
			"tp_mode":              "static",
			"take_profit_ratio":    0.5, // 远离测试价格区间，止盈不干扰
			"waterfall_enabled":    true,
			"waterfall_protection": 0.02,
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	bar := model.Bar{Symbol: "PEPEUSDT", Interval: "15m", Close: 97, High: 97, Low: 97}
	// FlashCrashDetector 窗口语义：只保留 ts-windowSeconds 以内的点
	// （window=1m → 60 个时间单位），同批 tick 时间戳须落在 60 以内。
	tick := func(price float64, ts int64) model.Tick {
		return model.Tick{Symbol: "PEPEUSDT", Last: price, Timestamp: ts}
	}

	// 瀑布未触发：首根 K 线按存量行为直接开仓（无指标门槛）。
	s := start()
	if sig, err := s.OnBar(bar, nil); err != nil || sig == nil || sig.Direction != "LONG" {
		t.Fatalf("pre-crash entry = %+v err=%v, want LONG", sig, err)
	}

	// 一分钟内 100→97（-3% ≥ 2% 阈值）：非硬编码币种的 waterfall 也要触发。
	s = start()
	for i, p := range []float64{100, 99, 97} {
		if _, err := s.OnTick(tick(p, int64(1000+i*10)), nil); err != nil {
			t.Fatalf("ontick: %v", err)
		}
	}
	if st := s.RuntimeStatus(); st["waterfall_paused"] != true {
		t.Fatalf("waterfall_paused = %v, want true（非五币种触发）", st["waterfall_paused"])
	}
	// 暂停期首单分支被挡（修复前此处照样 EnterPosition+发单）。
	if sig, err := s.OnBar(bar, nil); err != nil || sig != nil {
		t.Fatalf("entry during waterfall pause = %+v err=%v, want nil", sig, err)
	}
	if st := s.RuntimeStatus(); st["in_position"] == true {
		t.Fatal("waterfall pause must not open first order")
	}

	// 恢复：旧高点被窗口驱逐（时间戳推进 200 > 60），新窗内跌幅 < 阈值一半。
	for i, p := range []float64{97, 97.2, 97.1} {
		if _, err := s.OnTick(tick(p, int64(1200+i*10)), nil); err != nil {
			t.Fatalf("ontick resume: %v", err)
		}
	}
	if st := s.RuntimeStatus(); st["waterfall_paused"] != false {
		t.Fatalf("waterfall_paused = %v, want false（恢复）", st["waterfall_paused"])
	}
	if sig, err := s.OnBar(bar, nil); err != nil || sig == nil || sig.Direction != "LONG" {
		t.Fatalf("post-resume entry = %+v err=%v, want LONG", sig, err)
	}
}
