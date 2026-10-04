package analysis

import (
	"math"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// 构造一根 bar。
func mkBar(t int64, o, h, l, c, v float64) model.Bar {
	return model.Bar{Symbol: "TESTUSDT", Interval: "1h", Time: t, Open: o, High: h, Low: l, Close: c, Volume: v}
}

// synthLifecycle 程序化生成完整主力生命周期：
// 派发(顶背离) → 下跌+抛售高潮(SC) → 吸筹区间(缩量回踩 ST + 弹簧线) →
// 放量突破拉升 → 顶部买入高潮(BC)。返回 bars 与关键节点索引。
func synthLifecycle() (bars []model.Bar) {
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	step := int64(time.Hour / time.Millisecond)
	ts := func(i int) int64 { return t0 + int64(i)*step }
	base := 100.0
	i := 0
	push := func(o, h, l, c, v float64) {
		bars = append(bars, mkBar(ts(i), o, h, l, c, v))
		i++
	}

	// ① 派发段：价格新高但量能背离（新高 + 巨量滞涨 + 回落）。
	// 起点已经在高位（首根低点 120 > 后续恐慌低 100.5）——真实顶部结构。
	for k := 0; k < 20; k++ {
		h := base + 20 + float64(k)*0.6
		push(base+20+float64(k)*0.5, h, base+20+float64(k)*0.3, h-0.4, 80)
	}
	// BC：新高巨量窄实体（顶部）。
	push(base+12.4, base+14.5, base+11.8, base+12.2, 400)
	for k := 0; k < 8; k++ {
		push(base+12, base+12.2, base+11.5-float64(k)*0.4, base+11.6-float64(k)*0.4, 90)
	}

	// ② 下跌 → SC：加速下探 + 抛售高潮（巨量 + 长下影回收）。
	for k := 0; k < 12; k++ {
		c := base + 7 - float64(k)*0.35
		push(c+0.3, c+0.4, c-0.5, c-0.3, 100)
	}
	// SC 棒：新低 + 巨量 + 收在下影上部。
	push(base+2.6, base+3.0, base+0.5, base+2.6, 420)

	// ③ 吸筹区间：AR + 缩量横盘 + ST + 弹簧线（窄幅 ≥40 根）。
	push(base+0.8, base+2.0, base+0.7, base+1.9, 180) // AR
	lo, hi := base+2.0, base+5.8
	for k := 0; k < 45; k++ {
		c := lo + 1 + math.Sin(float64(k)*0.4)*1.2
		push(c-0.2, c+0.5, c-0.6, c+0.1, 60) // 低量横盘
	}
	// ST：回踩 SC 低附近 + 缩量。
	push(lo+0.2, lo+0.7, base+0.55, lo+0.6, 45)
	for k := 0; k < 6; k++ {
		c := lo + 1.5 + math.Sin(float64(k))*0.8
		push(c-0.2, c+0.4, c-0.5, c+0.1, 60)
	}
	// 弹簧线：跌破区间低后同根收回。
	push(lo+0.8, lo+1.1, base+0.3, lo+1.2, 150)
	for k := 0; k < 5; k++ {
		c := lo + 1.8
		push(c-0.3, c+0.3, c-0.6, c+0.1, 65)
	}

	// ④ 拉升：放量突破区间高，多头排列上行。
	for k := 0; k < 20; k++ {
		c := hi + 0.3 + float64(k)*0.5
		push(c-0.5, c+0.4, c-0.9, c, 160) // 持续放量
	}

	// ⑤ 顶部 BC：新高 + 巨量 + 收在下半部（滞涨）。
	top := hi + 10.3
	push(top-0.6, top+0.5, top-1.2, top-0.9, 450)
	for k := 0; k < 6; k++ {
		push(top-1-float64(k)*0.3, top-0.8, top-1.8-float64(k)*0.3, top-1.6-float64(k)*0.3, 110)
	}
	return bars
}

// TestAnalyzeLifecycle：合成完整生命周期，断言状态机依次识别出
// 建仓/吸筹/拉升/出货的主线（洗盘段可短到被迟滞吸收），且最终阶段为出货。
func TestAnalyzeLifecycle(t *testing.T) {
	bars := synthLifecycle()
	t.Logf("bars=%d, bars[0].Time=%d, bars[73].Time=%d, bars[%d].Time=%d",
		len(bars), bars[0].Time, bars[73].Time, len(bars)-1, bars[len(bars)-1].Time)
	// 调试：关键 bar 的单点分类
	st := computeStats(bars)
	ev := scanEvents(bars, st)
	for _, i := range []int{50, 73, 90, 100, 103, 110, 120, 127} {
		rHi, rLo := swingRange(bars, rangeFrom(bars, i), i)
		ph, cf, _ := classifyAt(bars, st, ev, i)
		t.Logf("  bar %3d: close=%.2f rHi=%.2f rLo=%.2f → %s(%.2f) scIdx=%d",
			i, bars[i].Close, rHi, rLo, ph, cf, ev.scIdx)
	}
	res := Analyze(bars, "TESTUSDT", "1h")
	if len(res.Segments) == 0 {
		t.Fatal("no segments")
	}
	t.Logf("当前阶段: %s 置信度 %.0f%%", PhaseMeta[res.Phase].Label, res.Confidence)
	for _, s := range res.Segments {
		t.Logf("  段: %-12s %d → %d (%s → %s, %.0f%%)", PhaseMeta[s.Phase].Label,
			s.From, s.To,
			time.UnixMilli(s.From).Format("01-02 15:04"), time.UnixMilli(s.To).Format("01-02 15:04"), s.Confidence)
	}
	for _, e := range res.Evidence {
		t.Logf("  证据: [%s] %s (w=%.2f)", map[bool]string{true: "多", false: "空"}[e.Bullish], e.Label, e.Weight)
	}

	// 主线断言：必须出现过吸筹、拉升、出货三段，且顺序正确。
	var order []Phase
	for _, s := range res.Segments {
		if len(order) == 0 || order[len(order)-1] != s.Phase {
			order = append(order, s.Phase)
		}
	}
	idx := func(p Phase) int {
		for i, x := range order {
			if x == p {
				return i
			}
		}
		return -1
	}
	ia, im, id := idx(PhaseAccum), idx(PhaseMarkup), idx(PhaseDistrib)
	if ia == -1 {
		t.Fatalf("未识别吸筹段，主线=%v", order)
	}
	if im == -1 {
		t.Fatalf("未识别拉升段，主线=%v", order)
	}
	if !(ia < im) {
		t.Fatalf("阶段顺序错误：吸筹(%d)应在拉升(%d)前，主线=%v", ia, im, order)
	}
	if res.Phase != PhaseDistrib && id == -1 {
		t.Fatalf("末端应为出货（BC 滞涨+回落），当前=%s", res.Phase)
	}
	if res.Levels["range_low"] <= 0 || res.Levels["range_high"] <= res.Levels["range_low"] {
		t.Fatalf("关键价位异常: %v", res.Levels)
	}
}

// TestAnalyzeInsufficient：K 线不足时降级而不是胡编。
func TestAnalyzeInsufficient(t *testing.T) {
	bars := []model.Bar{mkBar(1, 1, 1.1, 0.9, 1.05, 10), mkBar(2, 1.05, 1.1, 1, 1.08, 12)}
	res := Analyze(bars, "X", "15m")
	if res.Phase != PhaseNone || len(res.Evidence) == 0 {
		t.Fatalf("insufficient 处理错误: %+v", res)
	}
}
