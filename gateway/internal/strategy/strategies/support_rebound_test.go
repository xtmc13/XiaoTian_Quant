package strategies

import (
	"math"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 支撑回踩反弹策略测试 ────────────────────────────────────────
//
// 合成 4h model.Bar 序列驱动 OnBar 推进状态机。基准场景：
// 120 根预热 K 线构成双底（114 号与 118 号低点 98.2/98.0 两次触及支撑），
// 支撑带 = [96.53, 99.47]；随后 120 号 bar 冲高 112，121 号放量暴跌
//（Low 97.8、量 1000 vs 均量 100，窗口内最低）→ ARMED；122 号反弹收 104，
// 123 号回踩（Low 97.2 / Close 102.5），124 号放量阳线（Open 102.5 /
// Close 106 / 量 300）→ LONG 入场，entry=106、stop=94.5994、target=112。

func srTestBar(open, high, low, close, vol float64) model.Bar {
	return model.Bar{
		Symbol:   "BTCUSDT",
		Interval: "4h",
		Open:     open,
		High:     high,
		Low:      low,
		Close:    close,
		Volume:   vol,
	}
}

// feedSupportReboundBars 逐根喂 K 线，返回所有非 nil 信号（按时间序）。
func feedSupportReboundBars(s *SupportReboundStrategy, bars ...model.Bar) []*model.Signal {
	var sigs []*model.Signal
	for _, b := range bars {
		if sig, _ := s.OnBar(b, nil); sig != nil {
			sigs = append(sigs, sig)
		}
	}
	return sigs
}

// newStartedSupportRebound 启动一个默认参数的 support_rebound 策略。
func newStartedSupportRebound(t *testing.T, overrides map[string]any) *SupportReboundStrategy {
	t.Helper()
	s := NewSupportReboundStrategy()
	params := map[string]any{"symbol": "BTCUSDT", "timeframe": "4h"}
	for k, v := range overrides {
		params[k] = v
	}
	if err := s.Start(params); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !s.IsRunning() {
		t.Fatal("strategy should be running after Start")
	}
	return s
}

// supportReboundBaseBars 构造 120 根双底预热 K 线：
// 0~113 高位横盘（Low 103），114 与 118 两次下探 98 附近，119 企稳。
func supportReboundBaseBars() []model.Bar {
	bars := make([]model.Bar, 0, 120)
	for i := 0; i < 114; i++ {
		bars = append(bars, srTestBar(104, 105, 103, 104, 100))
	}
	bars = append(bars, srTestBar(102, 102.5, 98.2, 99.0, 100))   // 114: 第一次探底
	bars = append(bars, srTestBar(100, 104, 99.8, 103, 100))      // 115: 回抽
	bars = append(bars, srTestBar(103, 104.5, 102, 104, 100))     // 116
	bars = append(bars, srTestBar(104, 104.8, 102.5, 103.5, 100)) // 117
	bars = append(bars, srTestBar(100, 100.5, 98.0, 99.2, 100))   // 118: 第二次探底（最低点）
	bars = append(bars, srTestBar(99.2, 100.3, 98.6, 99.8, 100))  // 119: 第三次触及
	return bars
}

// driveSupportReboundToSupport 喂完 120 根预热，断言支撑已确认。
func driveSupportReboundToSupport(t *testing.T, s *SupportReboundStrategy) {
	t.Helper()
	base := supportReboundBaseBars()
	if len(base) != 120 {
		t.Fatalf("base bars = %d, want 120", len(base))
	}
	// 预热完成前不得评估。
	feedSupportReboundBars(s, base[:119]...)
	if s.state != srStateIdle {
		t.Fatalf("state before warmup complete = %s, want IDLE", s.state)
	}
	feedSupportReboundBars(s, base[119])
	if s.state != srStateSupport {
		t.Fatalf("state after 120 bars = %s, want SUPPORT", s.state)
	}
	if math.Abs(s.minLow-98.0) > 1e-9 {
		t.Fatalf("minLow = %f, want 98.0", s.minLow)
	}
	if math.Abs(s.supportBottom-98.0*0.985) > 1e-9 {
		t.Fatalf("supportBottom = %f, want %.10f", s.supportBottom, 98.0*0.985)
	}
	if math.Abs(s.supportTop-98.0*1.015) > 1e-9 {
		t.Fatalf("supportTop = %f, want %.10f", s.supportTop, 98.0*1.015)
	}
}

// driveSupportReboundToEntry 完整走通四步入场，返回入场信号。
func driveSupportReboundToEntry(t *testing.T, s *SupportReboundStrategy) *model.Signal {
	t.Helper()
	driveSupportReboundToSupport(t, s)
	sigs := feedSupportReboundBars(s,
		srTestBar(108, 112, 107.5, 111, 100),      // 120: refHigh=112
		srTestBar(108, 108.5, 97.8, 97.9, 1000),   // 121: 放量暴跌 → ARMED
		srTestBar(97.8, 104.2, 97.6, 104.0, 100),  // 122: 反弹确认
		srTestBar(103.5, 103.8, 97.2, 102.5, 100), // 123: 回踩支撑带
		srTestBar(102.5, 107, 102.3, 106, 300),    // 124: 放量阳线入场
	)
	if len(sigs) != 1 {
		t.Fatalf("signals = %d, want 1 (entry)", len(sigs))
	}
	return sigs[0]
}

// TestSupportReboundSupportIdentification 用例 1：双底 + 多次触及 → supportActive。
func TestSupportReboundSupportIdentification(t *testing.T) {
	s := newStartedSupportRebound(t, nil)
	driveSupportReboundToSupport(t, s)

	// 触及次数：114/118/119 三根 Low 落入支撑带 [96.53, 99.47]。
	if s.state != srStateSupport {
		t.Fatalf("state = %s, want SUPPORT", s.state)
	}
	// 支撑确认后不应有任何信号。
	if sigs := feedSupportReboundBars(s, srTestBar(99.8, 100.2, 98.8, 99.9, 100)); len(sigs) != 0 {
		t.Fatalf("unexpected signal during SUPPORT wait: %+v", sigs[0])
	}
}

// TestSupportReboundCrashDetection 用例 2：放量暴跌 → ARMED；无量暴跌 → 不 ARMED。
func TestSupportReboundCrashDetection(t *testing.T) {
	// 子用例 A：急跌 12%+ 巨量 → ARMED。
	s := newStartedSupportRebound(t, nil)
	driveSupportReboundToSupport(t, s)
	sigs := feedSupportReboundBars(s,
		srTestBar(108, 112, 107.5, 111, 100),
		srTestBar(108, 108.5, 97.8, 97.9, 1000),
	)
	if len(sigs) != 0 {
		t.Fatalf("unexpected signal on crash bar: %+v", sigs[0])
	}
	if s.state != srStateArmed {
		t.Fatalf("state = %s, want ARMED", s.state)
	}
	if math.Abs(s.crashLow-97.8) > 1e-9 || math.Abs(s.crashRef-112) > 1e-9 {
		t.Fatalf("crashLow/crashRef = %f/%f, want 97.8/112", s.crashLow, s.crashRef)
	}

	// 子用例 B：同样 12%+ 急跌但成交量不足（150 < 100×2.0）→ 不 ARMED。
	s2 := newStartedSupportRebound(t, nil)
	driveSupportReboundToSupport(t, s2)
	feedSupportReboundBars(s2,
		srTestBar(108, 112, 107.5, 111, 100),
		srTestBar(108, 108.5, 97.8, 97.9, 150),
	)
	if s2.state != srStateSupport {
		t.Fatalf("state = %s, want SUPPORT (no volume spike, not armed)", s2.state)
	}
}

// TestSupportReboundEntrySignal 用例 3：反弹 + 回踩 + 放量阳线 → LONG 信号，
// stake/target/stop 全部正确。
func TestSupportReboundEntrySignal(t *testing.T) {
	s := newStartedSupportRebound(t, nil)
	sig := driveSupportReboundToEntry(t, s)

	if sig.Direction != "LONG" {
		t.Fatalf("direction = %s, want LONG", sig.Direction)
	}
	if sig.Symbol != "BTCUSDT" || sig.Strategy != "support_rebound" {
		t.Fatalf("signal symbol/strategy = %s/%s", sig.Symbol, sig.Strategy)
	}
	if !s.inPosition || s.state != srStatePosition {
		t.Fatalf("inPosition/state = %v/%s, want true/POSITION", s.inPosition, s.state)
	}
	entry := 106.0
	if math.Abs(s.entryPrice-entry) > 1e-9 {
		t.Fatalf("entryPrice = %f, want %f", s.entryPrice, entry)
	}
	// stop = supportBottom×(1−2%) = 98×0.985×0.98。
	wantStop := 98.0 * 0.985 * 0.98
	if math.Abs(s.stopPrice-wantStop) > 1e-9 {
		t.Fatalf("stopPrice = %f, want %f", s.stopPrice, wantStop)
	}
	// target = min(crashRef=112, entry×1.15=121.9) = 112。
	if math.Abs(s.targetPrice-112) > 1e-9 {
		t.Fatalf("targetPrice = %f, want 112", s.targetPrice)
	}
	// stake = position_size = 500 USDT（CustomStakeAmount 折算）。
	if stake := s.CustomStakeAmount(10000, sig); stake != 500 {
		t.Fatalf("CustomStakeAmount = %f, want 500", stake)
	}
	// 余额不足时退化为全额可用。
	if stake := s.CustomStakeAmount(300, sig); stake != 300 {
		t.Fatalf("CustomStakeAmount(300) = %f, want 300", stake)
	}
	// 持仓状态快照可供运行监控。
	rs := s.RuntimeStatus()
	if rs["state"] != srStatePosition || rs["in_position"] != true {
		t.Fatalf("RuntimeStatus = %v", rs)
	}
	if rs["entry_price"] != entry {
		t.Fatalf("RuntimeStatus entry_price = %v", rs["entry_price"])
	}
}

// TestSupportReboundSupportBreakInvalidates 用例 4：armed 期间收盘跌破支撑
// 下沿 → 支撑失效回 IDLE，后续不再产生信号。
func TestSupportReboundSupportBreakInvalidates(t *testing.T) {
	s := newStartedSupportRebound(t, nil)
	driveSupportReboundToSupport(t, s)
	sigs := feedSupportReboundBars(s,
		srTestBar(108, 112, 107.5, 111, 100),
		srTestBar(108, 108.5, 97.8, 97.9, 1000), // ARMED
		srTestBar(97.8, 98.2, 96.0, 95.0, 100),  // Close 95 < 96.53 → 支撑失效
	)
	if len(sigs) != 0 {
		t.Fatalf("unexpected signal on support break: %+v", sigs[0])
	}
	if s.state != srStateIdle || s.isArmed() || s.inPosition {
		t.Fatalf("state = %s, want IDLE after support break", s.state)
	}
	// 后续即使走「反弹+回踩+放量阳线」形态也不得有信号。
	sigs = feedSupportReboundBars(s,
		srTestBar(95.5, 104, 95.2, 104, 100),
		srTestBar(103.5, 103.8, 97.2, 102.5, 100),
		srTestBar(102.5, 107, 102.3, 106, 300),
	)
	if len(sigs) != 0 {
		t.Fatalf("signal after invalidation, want none: %+v", sigs[0])
	}
	if s.inPosition {
		t.Fatal("inPosition = true, want false")
	}
}

// TestSupportReboundExitPaths 用例 5：触止损 / 触目标 / 超时三条平仓路径。
func TestSupportReboundExitPaths(t *testing.T) {
	t.Run("触止损", func(t *testing.T) {
		s := newStartedSupportRebound(t, nil)
		driveSupportReboundToEntry(t, s)
		sigs := feedSupportReboundBars(s, srTestBar(95, 95.2, 93.8, 94.0, 100))
		if len(sigs) != 1 || sigs[0].Direction != "CLOSE" {
			t.Fatalf("exit signal = %+v, want one CLOSE", sigs)
		}
		if !strings.Contains(sigs[0].Reason, "止损") {
			t.Fatalf("reason = %q, want 止损", sigs[0].Reason)
		}
		if s.inPosition || s.state != srStateIdle {
			t.Fatalf("after stop exit: inPosition/state = %v/%s", s.inPosition, s.state)
		}
	})

	t.Run("触目标", func(t *testing.T) {
		s := newStartedSupportRebound(t, nil)
		driveSupportReboundToEntry(t, s)
		sigs := feedSupportReboundBars(s, srTestBar(111, 112.5, 110.8, 112.5, 100))
		if len(sigs) != 1 || sigs[0].Direction != "CLOSE" {
			t.Fatalf("exit signal = %+v, want one CLOSE", sigs)
		}
		if !strings.Contains(sigs[0].Reason, "止盈") {
			t.Fatalf("reason = %q, want 止盈", sigs[0].Reason)
		}
		if s.inPosition || s.state != srStateIdle {
			t.Fatalf("after target exit: inPosition/state = %v/%s", s.inPosition, s.state)
		}
	})

	t.Run("超时离场", func(t *testing.T) {
		s := newStartedSupportRebound(t, nil)
		driveSupportReboundToEntry(t, s)
		// 89 根横盘（stop < Close < target，不触发保本移损），第 90 根超时平仓。
		flat := make([]model.Bar, 0, 90)
		for i := 0; i < 89; i++ {
			flat = append(flat, srTestBar(105, 105.2, 104.8, 105, 100))
		}
		if sigs := feedSupportReboundBars(s, flat...); len(sigs) != 0 {
			t.Fatalf("unexpected early exit: %+v", sigs[0])
		}
		if s.holdBars != 89 {
			t.Fatalf("holdBars = %d, want 89", s.holdBars)
		}
		sigs := feedSupportReboundBars(s, srTestBar(105, 105.2, 104.8, 105, 100))
		if len(sigs) != 1 || sigs[0].Direction != "CLOSE" {
			t.Fatalf("exit signal = %+v, want one CLOSE", sigs)
		}
		if !strings.Contains(sigs[0].Reason, "超时") {
			t.Fatalf("reason = %q, want 超时", sigs[0].Reason)
		}
	})
}

// TestSupportReboundBreakevenStop 用例 6：浮盈 ≥ rebound_pct 后止损上移到
// entry×1.002（保本），回落触发保本离场。
func TestSupportReboundBreakevenStop(t *testing.T) {
	s := newStartedSupportRebound(t, nil)
	driveSupportReboundToEntry(t, s)

	wantStop := 98.0 * 0.985 * 0.98
	if math.Abs(s.stopPrice-wantStop) > 1e-9 {
		t.Fatalf("initial stop = %f, want %f", s.stopPrice, wantStop)
	}
	// Close=111.5 → 浮盈 5.19% ≥ 5% → 保本移损到 106×1.002=106.212（< target 112 不离场）。
	sigs := feedSupportReboundBars(s, srTestBar(110, 111.5, 109.8, 111.5, 100))
	if len(sigs) != 0 {
		t.Fatalf("unexpected exit before target: %+v", sigs[0])
	}
	if !s.stopMovedToBreakeven {
		t.Fatal("stopMovedToBreakeven = false, want true")
	}
	wantBE := 106.0 * 1.002
	if math.Abs(s.stopPrice-wantBE) > 1e-9 {
		t.Fatalf("breakeven stop = %f, want %f", s.stopPrice, wantBE)
	}
	// 回落到 106.1 ≤ 106.212 → 保本止损离场。
	sigs = feedSupportReboundBars(s, srTestBar(107, 107.2, 106.0, 106.1, 100))
	if len(sigs) != 1 || sigs[0].Direction != "CLOSE" || !strings.Contains(sigs[0].Reason, "止损") {
		t.Fatalf("breakeven exit signal = %+v", sigs)
	}
	if s.inPosition {
		t.Fatal("inPosition = true after breakeven exit")
	}
}

// TestSupportReboundOnTickExit 持仓期间 tick 触发止损（OnTick 管理路径）。
func TestSupportReboundOnTickExit(t *testing.T) {
	s := newStartedSupportRebound(t, nil)
	driveSupportReboundToEntry(t, s)
	sig, err := s.OnTick(model.Tick{Symbol: "BTCUSDT", Last: 93.0, Timestamp: 1}, nil)
	if err != nil || sig == nil {
		t.Fatalf("OnTick exit = %+v, err=%v", sig, err)
	}
	if sig.Direction != "CLOSE" {
		t.Fatalf("direction = %s, want CLOSE", sig.Direction)
	}
	if s.inPosition {
		t.Fatal("inPosition = true after tick stop exit")
	}
	// 空仓后 tick 不再产信号。
	if sig, _ := s.OnTick(model.Tick{Symbol: "BTCUSDT", Last: 93.0}, nil); sig != nil {
		t.Fatalf("unexpected tick signal after flat: %+v", sig)
	}
}

// TestSupportReboundParams 参数注册表：数量/默认值/范围校验/ApplyParams 同步。
func TestSupportReboundParams(t *testing.T) {
	s := NewSupportReboundStrategy()
	reg := s.GetParameters()
	if reg == nil {
		t.Fatal("GetParameters() = nil")
	}
	if reg.Count() != 14 {
		t.Fatalf("params = %d, want 14", reg.Count())
	}
	if p := reg.Get("crash_drop_pct"); p == nil || math.Abs(p.GetFloat()-0.12) > 1e-12 {
		t.Fatalf("crash_drop_pct default wrong: %+v", p)
	}
	if err := reg.Set("crash_drop_pct", 0.02); err == nil {
		t.Fatal("expected range error for crash_drop_pct=0.02")
	}
	if err := reg.Set("lookback_bars", 10); err == nil {
		t.Fatal("expected range error for lookback_bars=10")
	}

	// ApplyParams 从 map 同步（含 symbol/timeframe 引擎级键）。
	err := s.ApplyParams(map[string]any{
		"symbol":                      "ethusdt",
		"timeframe":                   "1h",
		"lookback_bars":               200,
		"crash_drop_pct":              0.2,
		"position_size":               800.0,
		"volume_sma_period":           30.0,
		"min_support_touches":         3.0,
		"max_hold_bars":               120.0,
		"pullback_bars":               8.0,
		"stop_buffer_pct":             0.01,
		"take_profit_pct":             0.1,
		"rebound_pct":                 0.03,
		"volume_spike_mult":           2.5,
		"entry_volume_mult":           2.0,
		"crash_lookback_bars":         12.0,
		"support_touch_tolerance_pct": 0.01,
	})
	if err != nil {
		t.Fatalf("ApplyParams: %v", err)
	}
	if s.symbol != "ETHUSDT" || s.timeframe != "1h" {
		t.Fatalf("symbol/timeframe = %s/%s", s.symbol, s.timeframe)
	}
	if s.lookbackBars != 200 || math.Abs(s.crashDropPct-0.2) > 1e-12 || s.positionSize != 800 {
		t.Fatalf("params not synced: %d/%f/%f", s.lookbackBars, s.crashDropPct, s.positionSize)
	}
	// 超范围值在 ApplyParams 阶段即报错（注册表校验）。
	if err := s.ApplyParams(map[string]any{"rebound_pct": 0.5}); err == nil {
		t.Fatal("expected error for rebound_pct=0.5 (out of range)")
	}
	// PrimaryTimeframe 声明与 PrimaryTimeframer 接口。
	if s.PrimaryTimeframe() != "1h" {
		t.Fatalf("PrimaryTimeframe = %s", s.PrimaryTimeframe())
	}
	// ParamDefs 供前端渲染。
	if defs := s.ParamDefs(); len(defs) != 14 {
		t.Fatalf("ParamDefs = %d, want 14", len(defs))
	}
}

// TestSupportReboundLifecycle 生命周期：Stop 清空状态，重启重新预热。
func TestSupportReboundLifecycle(t *testing.T) {
	s := newStartedSupportRebound(t, nil)
	driveSupportReboundToEntry(t, s)
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if s.IsRunning() || s.inPosition || len(s.bars) != 0 {
		t.Fatalf("after Stop: running/inPosition/bars = %v/%v/%d", s.IsRunning(), s.inPosition, len(s.bars))
	}
	// 重启后需重新预热，旧状态不残留。
	if err := s.Start(map[string]any{"symbol": "BTCUSDT"}); err != nil {
		t.Fatalf("re-Start: %v", err)
	}
	if len(s.bars) != 0 || s.state != srStateIdle {
		t.Fatalf("after re-Start: bars/state = %d/%s", len(s.bars), s.state)
	}
	if sigs := feedSupportReboundBars(s, srTestBar(104, 105, 103, 104, 100)); len(sigs) != 0 {
		t.Fatalf("signal during warmup, want none")
	}
}

// ── CRA 仓位管理（补仓梯度 + 移动止盈）测试 ─────────────────────
//
// CRA 模式由参数中 presence 任一 CRA 专属键触发（first_order_amount/
// add_positions/take_profit_method/moving_take_profit_tiers/stop_loss_ratio），
// 启用后入场/补仓/止盈/止损联动 cra.CRAState，未启用时保持原固定止损行为。

// srCRAOverrides 构造基础 CRA 参数：首单 100U、3 单补仓梯度
// （#2 跌 5% 补 2 倍、#3 跌 8% 补 4 倍）、静态止盈 full +3%。
func srCRAOverrides() map[string]any {
	return map[string]any{
		"first_order_amount":  100.0,
		"tp_mode":             "static", // 用静态档过关 Validate（moving 要求恰好 4 档）
		"take_profit_method":  "full",
		"take_profit_ratio":   0.03,
		"profit_callback":     0.0,
		"enable_add_position": true,
		"order_count":         3,
		"add_positions": []any{
			map[string]any{"order": 1, "multiplier": 1.0, "spread": 0.0, "callback": 0.0},
			map[string]any{"order": 2, "multiplier": 2.0, "spread": 0.05, "callback": 0.005},
			map[string]any{"order": 3, "multiplier": 4.0, "spread": 0.08, "callback": 0.005},
		},
	}
}

// srCRAFillBuy 模拟一笔买单成交回填（CRA 模式 OnOrderUpdate 路径）。
func srCRAFillBuy(t *testing.T, s *SupportReboundStrategy, price, qty float64) {
	t.Helper()
	sig, err := s.OnOrderUpdate(model.OrderData{
		Symbol:       "BTCUSDT",
		Side:         model.SideBuy,
		Status:       model.StatusFilled,
		AvgFillPrice: price,
		Filled:       qty,
	}, nil)
	if err != nil || sig != nil {
		t.Fatalf("OnOrderUpdate buy fill = %+v, err=%v", sig, err)
	}
}

// TestSupportReboundNoCRAKeysBackwardCompatible 回归：无 CRA 键时
// craEnabled=false，固定止损/目标/超时/保本移损行为逐字不变。
func TestSupportReboundNoCRAKeysBackwardCompatible(t *testing.T) {
	s := newStartedSupportRebound(t, nil)
	if s.craEnabled || s.craParams != nil || s.craState != nil {
		t.Fatalf("CRA fields should stay zero without CRA keys: enabled=%v", s.craEnabled)
	}
	sig := driveSupportReboundToEntry(t, s)
	if strings.Contains(sig.Reason, "[CRA]") {
		t.Fatalf("entry reason should not carry [CRA] tag: %q", sig.Reason)
	}
	// stake 仍走 position_size=500；固定 stop = 98×0.985×0.98。
	if stake := s.CustomStakeAmount(10000, sig); stake != 500 {
		t.Fatalf("CustomStakeAmount = %f, want 500", stake)
	}
	wantStop := 98.0 * 0.985 * 0.98
	if math.Abs(s.stopPrice-wantStop) > 1e-9 {
		t.Fatalf("stopPrice = %f, want fixed %f", s.stopPrice, wantStop)
	}
	// 固定止损路径：收盘跌破 stop → 止损离场（CRA 未启用不产出 cra 原因）。
	sigs := feedSupportReboundBars(s, srTestBar(95, 95.2, 93.8, 94.0, 100))
	if len(sigs) != 1 || sigs[0].Direction != "CLOSE" || !strings.Contains(sigs[0].Reason, "止损") {
		t.Fatalf("fixed stop exit = %+v", sigs)
	}
	if strings.Contains(sigs[0].Reason, "cra") {
		t.Fatalf("non-CRA exit reason should not mention cra: %q", sigs[0].Reason)
	}
}

// TestSupportReboundCRAAddLadder CRA 补仓梯度：首单 stake=100；跌 5% 后
// 回调触发 #2（Qty=200/price），跌 8% 后回调触发 #3（Qty=400/price）。
func TestSupportReboundCRAAddLadder(t *testing.T) {
	s := newStartedSupportRebound(t, srCRAOverrides())
	if !s.craEnabled || s.craParams == nil || s.craState == nil {
		t.Fatal("CRA mode should be enabled with CRA keys")
	}
	sig := driveSupportReboundToEntry(t, s)
	if !strings.Contains(sig.Reason, "[CRA]") {
		t.Fatalf("entry reason should carry [CRA] tag: %q", sig.Reason)
	}
	// 首单金额 = first_order_amount = 100 USDT。
	if stake := s.CustomStakeAmount(10000, sig); stake != 100 {
		t.Fatalf("CustomStakeAmount = %f, want 100", stake)
	}

	// 首单在 106 成交 → CRAState 入场，基准价 = 106。
	srCRAFillBuy(t, s, 106, 100.0/106.0)
	if !s.craState.InPosition || s.craState.PositionCount != 1 {
		t.Fatalf("cra state after first fill: inPosition/count = %v/%d",
			s.craState.InPosition, s.craState.PositionCount)
	}
	if math.Abs(s.craState.AvgEntryPrice-106) > 1e-9 {
		t.Fatalf("cra avg entry = %f, want 106", s.craState.AvgEntryPrice)
	}

	// 补仓 #2：跌穿 5%（收 100.6，−5.1%）激活，再创低 99.0 后回调 ≥0.5% 触发。
	if sigs := feedSupportReboundBars(s, srTestBar(101, 101.5, 100.4, 100.6, 100)); len(sigs) != 0 {
		t.Fatalf("signal on spread-touch bar, want none (arming): %+v", sigs[0])
	}
	if !s.craState.PendingAdd {
		t.Fatal("PendingAdd should be armed after 5% drop")
	}
	if sigs := feedSupportReboundBars(s, srTestBar(100, 100.2, 99.0, 99.0, 100)); len(sigs) != 0 {
		t.Fatalf("signal on deeper low, want none (below callback): %+v", sigs[0])
	}
	sigs := feedSupportReboundBars(s, srTestBar(99.0, 99.7, 98.9, 99.55, 100))
	if len(sigs) != 1 || sigs[0].Direction != "LONG" {
		t.Fatalf("add #2 signal = %+v", sigs)
	}
	if !strings.Contains(sigs[0].Reason, "cra add position #2") {
		t.Fatalf("add #2 reason = %q", sigs[0].Reason)
	}
	if want := 200.0 / 99.55; math.Abs(sigs[0].Qty-math.Round(want*1000)/1000) > 1e-9 {
		t.Fatalf("add #2 qty = %f, want RoundQty(200/99.55)=%f", sigs[0].Qty, math.Round(want*1000)/1000)
	}
	if s.craState.PendingAddCount != 1 {
		t.Fatalf("PendingAddCount = %d, want 1 after add signal", s.craState.PendingAddCount)
	}

	// #2 成交回填后再跌 8% 触发 #3。
	srCRAFillBuy(t, s, 99.55, 200.0/99.55)
	if s.craState.PositionCount != 2 || s.craState.PendingAddCount != 0 {
		t.Fatalf("after add #2 fill: count/pending = %d/%d", s.craState.PositionCount, s.craState.PendingAddCount)
	}
	if sigs := feedSupportReboundBars(s, srTestBar(98.0, 98.2, 96.4, 97.4, 100)); len(sigs) != 0 {
		t.Fatalf("signal on 8%% spread touch, want none (arming): %+v", sigs[0])
	}
	if sigs := feedSupportReboundBars(s, srTestBar(96.5, 96.6, 96.0, 96.0, 100)); len(sigs) != 0 {
		t.Fatalf("signal on deeper low, want none (below callback): %+v", sigs[0])
	}
	sigs = feedSupportReboundBars(s, srTestBar(96.0, 96.8, 95.9, 96.5, 100))
	if len(sigs) != 1 || sigs[0].Direction != "LONG" || !strings.Contains(sigs[0].Reason, "cra add position #3") {
		t.Fatalf("add #3 signal = %+v", sigs)
	}
	if want := 400.0 / 96.5; math.Abs(sigs[0].Qty-math.Round(want*1000)/1000) > 1e-9 {
		t.Fatalf("add #3 qty = %f, want RoundQty(400/96.5)=%f", sigs[0].Qty, math.Round(want*1000)/1000)
	}
}

// TestSupportReboundCRAMovingTakeProfit 移动止盈：tier {profit 5%,
// callback 1%}；涨到 +5.19% 激活档位，回撤 ≥1% → CLOSE("cra take profit")。
func TestSupportReboundCRAMovingTakeProfit(t *testing.T) {
	overrides := srCRAOverrides()
	overrides["enable_add_position"] = false
	overrides["order_count"] = 1
	overrides["add_positions"] = nil
	overrides["moving_take_profit_tiers"] = []any{
		map[string]any{"ratio": 0.05, "drawback": 0.01},
	}
	s := newStartedSupportRebound(t, overrides)
	driveSupportReboundToEntry(t, s)
	srCRAFillBuy(t, s, 106, 100.0/106.0)

	// 涨到 111.5（+5.19%）→ 激活 tier1，但未回撤，不离场。
	sigs := feedSupportReboundBars(s, srTestBar(110, 111.5, 109.8, 111.5, 100))
	if len(sigs) != 0 {
		t.Fatalf("unexpected exit on tier activation: %+v", sigs[0])
	}
	if rs := s.RuntimeStatus(); rs["cra_moving_tp_tier"] != 1 {
		t.Fatalf("RuntimeStatus cra_moving_tp_tier = %v, want 1", rs["cra_moving_tp_tier"])
	}
	// 回撤到 110.4（较高点回撤 1.04% ≥ 1%）→ 移动止盈离场。
	sigs = feedSupportReboundBars(s, srTestBar(111, 111.2, 110.2, 110.4, 100))
	if len(sigs) != 1 || sigs[0].Direction != "CLOSE" {
		t.Fatalf("moving TP exit = %+v", sigs)
	}
	if !strings.Contains(sigs[0].Reason, "cra take profit") {
		t.Fatalf("exit reason = %q, want cra take profit", sigs[0].Reason)
	}
	if s.craState.InPosition || s.craState.LoopExecuted != 1 || s.inPosition {
		t.Fatalf("after cra TP exit: inPosition/loop/inPosition = %v/%d/%v",
			s.craState.InPosition, s.craState.LoopExecuted, s.inPosition)
	}
}

// TestSupportReboundCRAStaticTakeProfit CRA 静态止盈 full：+3% → CLOSE。
func TestSupportReboundCRAStaticTakeProfit(t *testing.T) {
	overrides := srCRAOverrides()
	overrides["enable_add_position"] = false
	overrides["order_count"] = 1
	overrides["add_positions"] = nil
	s := newStartedSupportRebound(t, overrides)
	driveSupportReboundToEntry(t, s)
	srCRAFillBuy(t, s, 106, 100.0/106.0)

	// +2% 未达标。
	if sigs := feedSupportReboundBars(s, srTestBar(107, 108.2, 106.8, 108.1, 100)); len(sigs) != 0 {
		t.Fatalf("unexpected TP at +2%%: %+v", sigs[0])
	}
	// +3.1% ≥ 3% → 止盈（profit_callback=0，无回撤要求）。
	sigs := feedSupportReboundBars(s, srTestBar(108, 109.5, 108.0, 109.3, 100))
	if len(sigs) != 1 || sigs[0].Direction != "CLOSE" || !strings.Contains(sigs[0].Reason, "cra take profit") {
		t.Fatalf("static TP exit = %+v", sigs)
	}
}

// TestSupportReboundCRAStopLoss 比例止损：stop_loss_ratio=10%，
// 通过 OnTick 最新价判定 → CLOSE("cra stop loss")。
func TestSupportReboundCRAStopLoss(t *testing.T) {
	overrides := srCRAOverrides()
	overrides["enable_add_position"] = false
	overrides["order_count"] = 1
	overrides["add_positions"] = nil
	overrides["stop_loss_ratio"] = 0.10
	s := newStartedSupportRebound(t, overrides)
	driveSupportReboundToEntry(t, s)
	srCRAFillBuy(t, s, 106, 100.0/106.0)

	// tick 94：亏损 (106−94)/106 ≈ 11.3% ≥ 10% → 止损。
	out, err := s.OnTick(model.Tick{Symbol: "BTCUSDT", Last: 94.0, Timestamp: 1}, nil)
	if err != nil || out == nil || out.Direction != "CLOSE" {
		t.Fatalf("OnTick stop loss = %+v, err=%v", out, err)
	}
	if !strings.Contains(out.Reason, "cra stop loss") {
		t.Fatalf("reason = %q, want cra stop loss", out.Reason)
	}
	if s.craState.InPosition || s.inPosition {
		t.Fatal("position should be flat after cra stop loss")
	}
}

// TestSupportReboundCRAOnOrderUpdateBackfill 成交回填：两笔买单后
// PositionCount/均价/数量正确，RuntimeStatus 暴露 CRA 快照。
func TestSupportReboundCRAOnOrderUpdateBackfill(t *testing.T) {
	s := newStartedSupportRebound(t, srCRAOverrides())
	driveSupportReboundToEntry(t, s)

	// 第一笔：106 成交 1 个。
	srCRAFillBuy(t, s, 106, 1.0)
	if !s.craState.InPosition || s.craState.PositionCount != 1 {
		t.Fatalf("after fill #1: inPosition/count = %v/%d", s.craState.InPosition, s.craState.PositionCount)
	}
	if math.Abs(s.craState.AvgEntryPrice-106) > 1e-9 {
		t.Fatalf("avg after fill #1 = %f, want 106", s.craState.AvgEntryPrice)
	}

	// 第二笔：99.55 成交 2 个 → 均价 = (106×1 + 99.55×2)/3 = 101.7。
	srCRAFillBuy(t, s, 99.55, 2.0)
	if s.craState.PositionCount != 2 {
		t.Fatalf("PositionCount = %d, want 2", s.craState.PositionCount)
	}
	if math.Abs(s.craState.TotalQty-3.0) > 1e-9 {
		t.Fatalf("TotalQty = %f, want 3", s.craState.TotalQty)
	}
	if math.Abs(s.craState.AvgEntryPrice-101.7) > 1e-9 {
		t.Fatalf("AvgEntryPrice = %f, want 101.7", s.craState.AvgEntryPrice)
	}
	// 下一档（#3）补仓触发价 = 基准价 106×(1−8%) = 97.52。
	rs := s.RuntimeStatus()
	if rs["cra_enabled"] != true || rs["cra_position_count"] != 2 {
		t.Fatalf("RuntimeStatus CRA fields = %v", rs)
	}
	if math.Abs(rs["cra_avg_entry_price"].(float64)-101.7) > 1e-9 {
		t.Fatalf("RuntimeStatus avg = %v, want 101.7", rs["cra_avg_entry_price"])
	}
	if rs["cra_next_add_order"] != 3 || math.Abs(rs["cra_next_add_price"].(float64)-97.52) > 1e-9 {
		t.Fatalf("RuntimeStatus next add = %v/%v, want 3/97.52",
			rs["cra_next_add_order"], rs["cra_next_add_price"])
	}

	// 清仓单成交 → ExitPosition，状态归零。
	sig, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusFilled, ClosePosition: true,
	}, nil)
	if err != nil || sig != nil {
		t.Fatalf("OnOrderUpdate close fill = %+v, err=%v", sig, err)
	}
	if s.craState.InPosition || s.craState.PositionCount != 0 || s.craState.AvgEntryPrice != 0 {
		t.Fatalf("after close fill: %+v", s.craState)
	}
}

// TestSupportReboundCRATimeoutSafety CRA 模式下超时平仓仍是最后安全闸：
// 未触止损/止盈时持仓满 max_hold_bars 根 → CLOSE("timeout (cra safety)")。
func TestSupportReboundCRATimeoutSafety(t *testing.T) {
	overrides := srCRAOverrides()
	overrides["enable_add_position"] = false
	overrides["order_count"] = 1
	overrides["add_positions"] = nil
	s := newStartedSupportRebound(t, overrides)
	driveSupportReboundToEntry(t, s)
	srCRAFillBuy(t, s, 106, 100.0/106.0)

	// 89 根横盘（−0.94% 不触止盈，不触补仓），第 90 根超时平仓。
	flat := make([]model.Bar, 0, 90)
	for i := 0; i < 89; i++ {
		flat = append(flat, srTestBar(105, 105.2, 104.8, 105, 100))
	}
	if sigs := feedSupportReboundBars(s, flat...); len(sigs) != 0 {
		t.Fatalf("unexpected early exit: %+v", sigs[0])
	}
	if s.holdBars != 89 {
		t.Fatalf("holdBars = %d, want 89", s.holdBars)
	}
	sigs := feedSupportReboundBars(s, srTestBar(105, 105.2, 104.8, 105, 100))
	if len(sigs) != 1 || sigs[0].Direction != "CLOSE" {
		t.Fatalf("timeout exit = %+v", sigs)
	}
	if !strings.Contains(sigs[0].Reason, "timeout (cra safety)") {
		t.Fatalf("reason = %q, want timeout (cra safety)", sigs[0].Reason)
	}
	if s.craState.InPosition || s.craState.LoopExecuted != 1 || s.inPosition || s.state != srStateIdle {
		t.Fatalf("after cra timeout exit: state/inPosition = %s/%v", s.state, s.inPosition)
	}
}

// TestSupportReboundCRAInvalidParamsStartFails CRA 参数非法（如
// first_order_amount 超界）→ Start 报错且不进入运行态。
func TestSupportReboundCRAInvalidParamsStartFails(t *testing.T) {
	s := NewSupportReboundStrategy()
	err := s.Start(map[string]any{
		"symbol":             "BTCUSDT",
		"first_order_amount": 0.5, // Validate 要求 1~10000
		"tp_mode":            "static",
	})
	if err == nil {
		t.Fatal("Start should fail on invalid CRA params")
	}
	if s.IsRunning() || s.craEnabled {
		t.Fatalf("strategy should not run with invalid CRA params: running=%v enabled=%v",
			s.IsRunning(), s.craEnabled)
	}
}
