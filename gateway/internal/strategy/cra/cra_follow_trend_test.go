package cra

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── E 片：顺势而为（follow_trend，币富名词解释 #13/#40）──
//
// 币富原语义前提是"多空同时持仓"（被套仓锚定对侧顺势再开仓放大）；本引擎为
// 每循环单侧模型（resolveContractSide 循环起点 EMA20 选边，InPosition 期间不
// 开新首单），按可达语义实现并在此锁定：dual 模式下上一循环以被套状态结束
// （峰值补仓 N 次）、新一轮换向开仓时，首单名义放大 min(N+1, 5)；同向新开、
// 上轮干净结束（0 补仓清零锚点）、非 dual、非合约、开关关闭一律不放大。

// ftAddLadder 构造 order 1..7 的补仓阶梯（小额差价/零回调，便于价格剧本驱动）。
func ftAddLadder() []any {
	out := make([]any, 0, 7)
	for i := 1; i <= 7; i++ {
		out = append(out, map[string]any{"order": i, "multiplier": 1, "spread": 0.01, "callback": 0})
	}
	return out
}

// ftBaseParams 顺势测试公共配置：合约 dual、静态止盈 2%/零回调、补仓阶梯 7 档。
func ftBaseParams(extra map[string]any) map[string]any {
	base := map[string]any{
		"symbol":                 "BTCUSDT",
		"market_type":            "swap",
		"leverage":               5,
		"direction":              "dual",
		"follow_trend":           true,
		"first_order_amount":     10,
		"first_order_multiplier": 1,
		"tp_mode":                "static",
		"take_profit_ratio":      0.02,
		"profit_callback":        0,
		"enable_add_position":    true,
		"order_count":            7,
		"add_positions":          ftAddLadder(),
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

func ftBar(close float64) model.Bar {
	return model.Bar{Symbol: "BTCUSDT", Close: close, High: close, Low: close}
}

func ftFill(t *testing.T, s *BaseCRAStrategy, side model.OrderSide, price float64, qty float64) {
	t.Helper()
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: side,
		Filled: qty, AvgFillPrice: price, Status: model.StatusFilled,
	}, nil); err != nil {
		t.Fatalf("fill: %v", err)
	}
}

// ftTrappedLongThenFlip 跑"多仓被套 len(addPrices) 次→止盈出场→翻空/翻多"剧本：
// 100 开多（首单 0.5=10U×5 杠杆 @100），横盘铺垫喂够 EMA20 窗口，逐级补仓，
// 103 止盈全平，flipPrice 换向新开。返回策略实例与换向后的首单信号。
func ftTrappedLongThenFlip(t *testing.T, extra map[string]any, addPrices []float64, flipPrice float64) (*BaseCRAStrategy, *model.Signal) {
	t.Helper()
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(ftBaseParams(extra)); err != nil {
		t.Fatalf("start: %v", err)
	}
	// 首单 LONG（EMA20 窗口不足时 dual 默认多）。
	sig, err := s.OnBar(ftBar(100), nil)
	if err != nil || sig == nil {
		t.Fatalf("first order: sig=%v err=%v", sig, err)
	}
	if sig.Direction != "LONG" {
		t.Fatalf("first loop direction = %s, want LONG", sig.Direction)
	}
	// 首循环无锚点不放大；open_double 只放大首单名义（D 片口径）。
	wantFirst := 10 * 1 * 5 / 100.0
	if od, _ := extra["open_double"].(bool); od {
		wantFirst *= 2
	}
	if want := RoundQty(wantFirst); sig.Qty != want {
		t.Fatalf("trapped loop first qty = %v, want %v（首循环无锚点不放大）", sig.Qty, want)
	}
	ftFill(t, s, model.SideBuy, 100, sig.Qty)
	// 横盘铺垫：喂够 ≥20 根的 EMA20 窗口（价格不出补仓差价、不碰止盈）。
	for i := 0; i < 15; i++ {
		if sig, _ := s.OnBar(ftBar(99.5), nil); sig != nil {
			t.Fatalf("warmup bar %d: unexpected signal %+v", i, sig)
		}
	}
	// 逐级被套补仓：触发价挂起（差价达标），同价再喂一根（零回调）即触发。
	for i, price := range addPrices {
		if sig, _ := s.OnBar(ftBar(price), nil); sig != nil {
			t.Fatalf("add %d pending bar: unexpected signal %+v", i+1, sig)
		}
		sig, err := s.OnBar(ftBar(price), nil)
		if err != nil || sig == nil {
			t.Fatalf("add %d trigger: sig=%v err=%v", i+1, sig, err)
		}
		ftFill(t, s, model.SideBuy, price, sig.Qty)
	}
	// 止盈全平：均价远低于 103，盈利 2% 达标零回调即触发。
	sig, err = s.OnBar(ftBar(103), nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" {
		t.Fatalf("take profit exit: sig=%+v err=%v", sig, err)
	}
	// 换向新开（flipPrice 决定 EMA20 选边）。
	sig, err = s.OnBar(ftBar(flipPrice), nil)
	if err != nil {
		t.Fatalf("flip bar: %v", err)
	}
	if sig == nil {
		t.Fatal("expected flipped first order signal")
	}
	return s, sig
}

// TestCRAFollowTrendBoostAfterOppositeTrapped 对侧被套 2 次→本侧首单 3 倍
// （币富 #13：顺势首单放大倍数 = 被套仓补仓次数+1）。
func TestCRAFollowTrendBoostAfterOppositeTrapped(t *testing.T) {
	_, sig := ftTrappedLongThenFlip(t, nil, []float64{98.5, 97}, 90)
	if sig.Direction != "SHORT" {
		t.Fatalf("flipped direction = %s, want SHORT", sig.Direction)
	}
	// 10U ×1 ×3（2 次被套+1）×5 杠杆 @90。
	if want := RoundQty(10 * 3 * 5 / 90.0); sig.Qty != want {
		t.Fatalf("boosted qty = %v, want %v (3×)", sig.Qty, want)
	}
	// 对照：默认关闭同剧本零放大（绝对值锁定；RoundQty 分档精度不同，不做
	// 四舍五入后数量的倍数交叉断言）。
	_, ctrl := ftTrappedLongThenFlip(t, map[string]any{"follow_trend": false}, []float64{98.5, 97}, 90)
	if want := RoundQty(10 * 1 * 5 / 90.0); ctrl.Qty != want {
		t.Fatalf("control qty = %v, want %v (unboosted)", ctrl.Qty, want)
	}
}

// TestCRAFollowTrendCapFive 逆势补仓 6 次（≥4）时顺势首单只能开 5 倍
// （币富 #13 cap=5 硬上限）。
func TestCRAFollowTrendCapFive(t *testing.T) {
	_, sig := ftTrappedLongThenFlip(t, nil, []float64{98.5, 97, 95.5, 94, 92.5, 91}, 90)
	want := RoundQty(10 * followTrendMaxMultiplier * 5 / 90.0)
	if sig.Qty != want {
		t.Fatalf("capped qty = %v, want %v (cap %d×, 6+1=7 被钳制)", sig.Qty, want, followTrendMaxMultiplier)
	}
}

// TestCRAFollowTrendAnchorClearedAfterCleanLoop 清零语义：被放大的空仓循环
// 干净止盈（0 补仓）后锚点重写为 0，再换向开多不放大。
func TestCRAFollowTrendAnchorClearedAfterCleanLoop(t *testing.T) {
	s, sig := ftTrappedLongThenFlip(t, nil, []float64{98.5, 97}, 90)
	if sig.Direction != "SHORT" {
		t.Fatalf("flipped direction = %s, want SHORT", sig.Direction)
	}
	ftFill(t, s, model.SideSell, 90, sig.Qty)
	// 空仓首单直接止盈（盈利 3% ≥ 2%），本循环 0 补仓 → 锚点清零。
	exit, err := s.OnBar(ftBar(87.3), nil)
	if err != nil || exit == nil || exit.Direction != "CLOSE" {
		t.Fatalf("short take profit: sig=%+v err=%v", exit, err)
	}
	// 翻多新开：100 高于窗口内全部均价 → EMA20 选多；上轮干净结束不放大。
	next, err := s.OnBar(ftBar(100), nil)
	if err != nil || next == nil {
		t.Fatalf("next loop: sig=%v err=%v", next, err)
	}
	if next.Direction != "LONG" {
		t.Fatalf("next direction = %s, want LONG", next.Direction)
	}
	if want := RoundQty(10 * 1 * 5 / 100.0); next.Qty != want {
		t.Fatalf("qty after clean loop = %v, want %v（锚点已清零不放大）", next.Qty, want)
	}
}

// TestCRAFollowTrendSameSideNoBoost 同向新开不放大：多仓被套 2 次出场后趋势
// 仍向上（EMA20 选多），新多仓首单为原量。
func TestCRAFollowTrendSameSideNoBoost(t *testing.T) {
	_, sig := ftTrappedLongThenFlip(t, nil, []float64{98.5, 97}, 105)
	if sig.Direction != "LONG" {
		t.Fatalf("same-side direction = %s, want LONG", sig.Direction)
	}
	if want := RoundQty(10 * 1 * 5 / 105.0); sig.Qty != want {
		t.Fatalf("same-side qty = %v, want %v（同向不放大）", sig.Qty, want)
	}
}

// TestCRAFollowTrendStacksWithOpenDouble 与 open_double 叠加：
// 首单 = 首单金额 ×(open_double?2:1) ×顺势倍数；补仓阶梯基数不变（D 片口径）。
func TestCRAFollowTrendStacksWithOpenDouble(t *testing.T) {
	_, sig := ftTrappedLongThenFlip(t, map[string]any{"open_double": true}, []float64{98.5, 97}, 90)
	// 10U ×2（open_double）×3（2 次被套+1）×5 杠杆 @90。
	if want := RoundQty(10 * 2 * 3 * 5 / 90.0); sig.Qty != want {
		t.Fatalf("open_double+follow_trend qty = %v, want %v (2×3)", sig.Qty, want)
	}
}

// TestCRAFollowTrendDisabledByDefault 默认关闭（缺省/显式 false）零行为变化。
func TestCRAFollowTrendDisabledByDefault(t *testing.T) {
	_, absent := ftTrappedLongThenFlip(t, map[string]any{"follow_trend": nil}, []float64{98.5, 97}, 90)
	_, explicit := ftTrappedLongThenFlip(t, map[string]any{"follow_trend": false}, []float64{98.5, 97}, 90)
	want := RoundQty(10 * 1 * 5 / 90.0)
	if absent.Qty != want || explicit.Qty != want {
		t.Fatalf("default off qty absent=%v explicit=%v, want %v", absent.Qty, explicit.Qty, want)
	}
}

// TestCRAFollowTrendMultiplierGates 三条件门控（仅合约+dual+开关）与同向/
// 零补仓不放大——直接锁定 followTrendMultiplier 判定矩阵。
func TestCRAFollowTrendMultiplierGates(t *testing.T) {
	newWith := func(contract bool, extra map[string]any) *BaseCRAStrategy {
		var s *BaseCRAStrategy
		if contract {
			s = NewCRAContractStrategy("cra_contract", "BTCUSDT")
		} else {
			s = NewCRASpotStrategy("cra_spot", "BTCUSDT")
		}
		if err := s.Start(ftBaseParams(extra)); err != nil {
			t.Fatalf("start: %v", err)
		}
		return s
	}
	arm := func(s *BaseCRAStrategy, side PositionSide, adds int) {
		s.state.PrevLoopSide = side
		s.state.PrevLoopTrappedAdds = adds
	}

	// 生效基准：合约+dual+开关+对侧被套 2 次 → 3 倍。
	s := newWith(true, nil)
	arm(s, SideShort, 2)
	if got := s.followTrendMultiplier(SideLong); got != 3 {
		t.Fatalf("dual opposite trapped 2: mult = %v, want 3", got)
	}
	// 上限：被套 6 次 → 钳 5 倍。
	arm(s, SideShort, 6)
	if got := s.followTrendMultiplier(SideLong); got != followTrendMaxMultiplier {
		t.Fatalf("cap: mult = %v, want %d", got, followTrendMaxMultiplier)
	}
	// 同向不放大。
	if got := s.followTrendMultiplier(SideShort); got != 1 {
		t.Fatalf("same side: mult = %v, want 1", got)
	}
	// 零补仓锚点不放大。
	arm(s, SideShort, 0)
	if got := s.followTrendMultiplier(SideLong); got != 1 {
		t.Fatalf("zero adds: mult = %v, want 1", got)
	}
	// 非 dual（long/short 单向）不生效。
	for _, dir := range []string{"long", "short"} {
		sd := newWith(true, map[string]any{"direction": dir})
		arm(sd, SideShort, 3)
		if got := sd.followTrendMultiplier(SideLong); got != 1 {
			t.Fatalf("direction=%s: mult = %v, want 1（非 dual 零行为变化）", dir, got)
		}
	}
	// 开关关闭不生效。
	soff := newWith(true, map[string]any{"follow_trend": false})
	arm(soff, SideShort, 3)
	if got := soff.followTrendMultiplier(SideLong); got != 1 {
		t.Fatalf("follow_trend off: mult = %v, want 1", got)
	}
	// 非合约不生效（即便参数带 dual+follow_trend）。
	spot := newWith(false, nil)
	arm(spot, SideShort, 3)
	if got := spot.followTrendMultiplier(SideLong); got != 1 {
		t.Fatalf("spot: mult = %v, want 1（现货零行为变化）", got)
	}
}

// TestCRAFollowTrendAnchorRebuiltOnRestart 重启存续：restored_fills 逐笔重放
// 重建顺势锚点（最近全平循环的 {方向, 峰值补仓}）与当前循环峰值补仓下限；
// 恢复持仓继续被套补仓、止盈出场后换向新开照常放大。
func TestCRAFollowTrendAnchorRebuiltOnRestart(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	params := ftBaseParams(nil)
	// 账本：循环1 多仓 3 档（被套 2 次）全平 → 循环2 空仓 1 档未平。
	params["restored_fills"] = []any{
		map[string]any{"side": "BUY", "qty": 0.5, "price": 100},
		map[string]any{"side": "BUY", "qty": 0.5, "price": 98},
		map[string]any{"side": "BUY", "qty": 0.5, "price": 96},
		map[string]any{"side": "SELL", "qty": 1.5, "price": 99},
		map[string]any{"side": "SELL", "qty": 0.5, "price": 95},
	}
	if err := s.Start(params); err != nil {
		t.Fatalf("start: %v", err)
	}
	st := s.state
	if st.Side != SideShort || st.PositionCount != 1 {
		t.Fatalf("restored position = %s×%d, want short×1", st.Side, st.PositionCount)
	}
	if st.PrevLoopSide != SideLong || st.PrevLoopTrappedAdds != 2 {
		t.Fatalf("restored anchor = %s/%d, want long/2（循环1 被套 2 次）", st.PrevLoopSide, st.PrevLoopTrappedAdds)
	}
	if st.PeakAddCount != 0 {
		t.Fatalf("restored current loop peak adds = %d, want 0（单档）", st.PeakAddCount)
	}
	// 恢复的空仓再被套 1 次：96.5 挂起（涨幅≥差价）→ 同价触发（零回调）。
	if sig, _ := s.OnBar(ftBar(96.5), nil); sig != nil {
		t.Fatalf("add pending: unexpected signal %+v", sig)
	}
	sig, err := s.OnBar(ftBar(96.5), nil)
	if err != nil || sig == nil || sig.Direction != "SHORT" {
		t.Fatalf("add trigger: sig=%+v err=%v", sig, err)
	}
	ftFill(t, s, model.SideSell, 96.5, sig.Qty)
	if st.PeakAddCount != 1 {
		t.Fatalf("peak adds after add = %d, want 1", st.PeakAddCount)
	}
	// 空仓止盈全平（均价≈95.76，93.8 盈利≈2.05%）：锚点重写为 {short,1}。
	exit, err := s.OnBar(ftBar(93.8), nil)
	if err != nil || exit == nil || exit.Direction != "CLOSE" {
		t.Fatalf("short take profit: sig=%+v err=%v", exit, err)
	}
	if st.PrevLoopSide != SideShort || st.PrevLoopTrappedAdds != 1 {
		t.Fatalf("anchor after exit = %s/%d, want short/1", st.PrevLoopSide, st.PrevLoopTrappedAdds)
	}
	// 换向开多（窗口不足 20 根默认多）：放大 min(1+1,5)=2 倍——重启后存续生效。
	next, err := s.OnBar(ftBar(100), nil)
	if err != nil || next == nil {
		t.Fatalf("next loop: sig=%v err=%v", next, err)
	}
	if next.Direction != "LONG" {
		t.Fatalf("next direction = %s, want LONG", next.Direction)
	}
	if want := RoundQty(10 * 2 * 5 / 100.0); next.Qty != want {
		t.Fatalf("post-restart boosted qty = %v, want %v (2×)", next.Qty, want)
	}
}
