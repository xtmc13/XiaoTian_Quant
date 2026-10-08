package cra

import (
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── C 片：反向止盈/反向止损（币富名词解释 #35/#36，仅合约）──

// risingThenCrossDownSeries 是 decliningThenCrossSeries 的仿射镜像
// （x' = 200 - x：40 根缓涨 100→115.6 后接 3 根 -0.2 回落至 115.4）——
// EMA/MACD 对仿射变换等变，默认 MACD(12,26,9) 在最后一根柱值由正变负
// （死叉），与金叉序列严格对偶。
func risingThenCrossDownSeries() []float64 {
	closes := make([]float64, 0, 43)
	p := 100.0
	for i := 0; i < 40; i++ {
		closes = append(closes, p)
		p += 0.4
	}
	for i := 0; i < 3; i++ {
		p -= 0.2
		closes = append(closes, p)
	}
	return closes
}

// startReverseContract 启动合约 CRA：静态止盈、止盈比例 50%/止损关闭
// （隔离常规出场分支，只观察反向止盈/止损）、补仓关闭（补仓场景由测试
// 直接以成交回执模拟）。extra 覆盖/追加参数。
func startReverseContract(t *testing.T, extra map[string]any) *BaseCRAStrategy {
	t.Helper()
	params := map[string]any{
		"symbol":              "BTCUSDT",
		"timeframe":           "15m",
		"direction":           "long",
		"first_order_amount":  10,
		"leverage":            1,
		"tp_mode":             "static",
		"take_profit_ratio":   0.5,
		"profit_callback":     0,
		"stop_loss_enabled":   false,
		"enable_add_position": false,
	}
	for k, v := range extra {
		params[k] = v
	}
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(params); err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

// fillSell 做空入场成交回执（空仓的减仓方向是 BUY，SELL 计入入场）。
func fillSell(t *testing.T, s *BaseCRAStrategy, price, qty float64) {
	t.Helper()
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusFilled,
		Filled: qty, AvgFillPrice: price,
	}, nil); err != nil {
		t.Fatalf("fill sell: %v", err)
	}
}

// seedLongPosition 直挂多仓状态（跳过首单信号链路，精确控制成本与档数）。
func seedLongPosition(s *BaseCRAStrategy, entry, qty float64) {
	s.state.EnterPosition(entry, SideLong)
	s.state.RecordFill(entry, qty, SideBuy)
	s.state.PositionCount = 1
}

// enterLong 走 OnBar 首单信号链路开多并成交，返回成交数量。
func enterLong(t *testing.T, s *BaseCRAStrategy, price float64) float64 {
	t.Helper()
	sig, err := s.OnBar(bar(price), nil)
	if err != nil || sig == nil || sig.Direction != "LONG" {
		t.Fatalf("first order: sig=%+v err=%v", sig, err)
	}
	fillBuy(t, s, price, sig.Qty)
	return sig.Qty
}

// 反向止盈触发：未补仓+浮盈+判定周期（5m 副周期供给）死叉 → 全平 CLOSE，
// 经 PendingClose 在途标记（不乐观 ExitPosition），成交确认后才收尾。
func TestCRAReverseTakeProfitTriggers(t *testing.T) {
	// 序列自检：镜像序列末根死叉（默认 12/26/9）。
	if !MACDBearish(makeBars(risingThenCrossDownSeries()...)) {
		t.Fatal("series sanity: default MACD should cross bearish at last bar")
	}

	s := startReverseContract(t, map[string]any{
		"reverse_take_profit_period": "5m",
	})
	// 5m 判定周期供给：先给无交叉的稳涨序列（死叉尚未出现）。
	feed := &fakeBarProvider{series: map[string][]model.Bar{
		"BTCUSDT|5m": makeBars(risingThenCrossDownSeries()[:40]...),
	}}
	s.SetBarProvider(feed)

	enterLong(t, s, 100) // 0.1 @100

	// 浮盈 1% 但 5m 无反向信号 → 不触发（常规止盈 50% 也未达线）。
	if sig, _ := s.OnBar(bar(101), nil); sig != nil {
		t.Fatalf("no reverse signal yet, must not fire: %+v", sig)
	}

	// 5m 供给更新为末根死叉的序列 → 下一根工作 K 线（仍浮盈 1%）触发反向
	// 止盈：全平 CLOSE（不带数量）、记在途标记、仓位未乐观清退。
	feed.series["BTCUSDT|5m"] = makeBars(risingThenCrossDownSeries()...)
	sig, _ := s.OnBar(bar(101), nil)
	if sig == nil || sig.Direction != "CLOSE" || sig.Qty != 0 {
		t.Fatalf("reverse tp = %+v, want full CLOSE qty 0", sig)
	}
	if !strings.Contains(sig.Reason, "reverse take profit") {
		t.Fatalf("reason = %q, want reverse take profit tag", sig.Reason)
	}
	st := s.state
	if !st.InPosition || st.PendingCloseKind != "reverse_tp" {
		t.Fatalf("state must stay in position with pending reverse_tp close: %+v", st)
	}

	// 在途期间：同价 K 线不再发任何信号（防重复出场）。
	if sig, _ := s.OnBar(bar(101), nil); sig != nil {
		t.Fatalf("pending reverse close must block new signals: %+v", sig)
	}

	// 成交确认（合约镜像平仓单）：全量成交 → 全平收尾、循环计数。
	fillClose(t, s, 101, 0.1, true)
	if st.InPosition || st.LoopExecuted != 1 || st.PendingCloseKind != "" {
		t.Fatalf("reverse tp fill must close the loop: inPos=%v loops=%d pending=%q",
			st.InPosition, st.LoopExecuted, st.PendingCloseKind)
	}
}

// 反向止盈不触发：已补仓 / 浮亏 / 无反向信号三种否决路径，且已补仓时回落
// 原有止盈方式（币富 #35："若已补仓则自动执行原有止盈方式"）。
func TestCRAReverseTakeProfitNotTriggered(t *testing.T) {
	t.Run("已补仓回落常规止盈", func(t *testing.T) {
		s := startReverseContract(t, map[string]any{
			"reverse_take_profit_period": "5m",
		})
		s.SetBarProvider(&fakeBarProvider{series: map[string][]model.Bar{
			"BTCUSDT|5m": makeBars(risingThenCrossDownSeries()...),
		}})
		enterLong(t, s, 100)
		fillBuy(t, s, 99, 0.10101) // 补仓第二档（直接成交回执模拟）→ PositionCount=2

		// 浮盈（均价≈99.5，101 浮盈 1.5%）+5m 死叉，但已补仓 → 反向止盈不生效。
		if sig, _ := s.OnBar(bar(101), nil); sig != nil {
			t.Fatalf("added position must disable reverse tp: %+v", sig)
		}
		// 回落原有止盈方式：均价 +50% 达线 → 常规全仓止盈全平。
		sig, _ := s.OnBar(bar(150), nil)
		if sig == nil || sig.Direction != "CLOSE" || !strings.Contains(sig.Reason, "cra take profit") {
			t.Fatalf("fallback to regular take profit = %+v", sig)
		}
		if s.state.InPosition || s.state.PendingCloseKind != "" {
			t.Fatal("regular full tp must close position directly, no pending kind")
		}
	})

	t.Run("浮亏不触发", func(t *testing.T) {
		s := startReverseContract(t, map[string]any{
			"reverse_take_profit_period": "5m",
			// 反向止损保持关闭：隔离验证"浮亏时反向止盈不触发"（币富 #35：
			// 反向信号触发点浮亏则执行原有止盈方式，反向止盈不在亏损时触发）。
		})
		s.SetBarProvider(&fakeBarProvider{series: map[string][]model.Bar{
			"BTCUSDT|5m": makeBars(risingThenCrossDownSeries()...),
		}})
		enterLong(t, s, 100)
		// 浮亏 1%+5m 死叉 → 不触发（也不误成全平）。
		if sig, _ := s.OnBar(bar(99), nil); sig != nil {
			t.Fatalf("reverse tp must not fire at floating loss: %+v", sig)
		}
		if !s.state.InPosition {
			t.Fatal("position must survive")
		}
	})

	t.Run("无反向信号不触发", func(t *testing.T) {
		s := startReverseContract(t, map[string]any{
			"reverse_take_profit_period": "5m",
		})
		// 供给不足（10 根 < 37）→ 降级工作周期且工作序列无死叉，双重无信号。
		s.SetBarProvider(&fakeBarProvider{series: map[string][]model.Bar{
			"BTCUSDT|5m": makeBars(1, 2, 3, 4, 5, 6, 7, 8, 9, 10),
		}})
		enterLong(t, s, 100)
		if sig, _ := s.OnBar(bar(101), nil); sig != nil {
			t.Fatalf("no reverse cross anywhere, must not fire: %+v", sig)
		}
	})
}

// 反向止盈做空镜像：死叉开空 → 金叉为反向；空仓浮盈（价格低于均价）时
// 判定周期金叉触发全平。
func TestCRAReverseTakeProfitShortMirror(t *testing.T) {
	s := startReverseContract(t, map[string]any{
		"direction": "short",
	})
	// 判定周期=工作周期（period=close 时反向止盈关闭，故用显式 15m=工作周期，
	// indicatorBars 命中"等于工作周期"分支直接用工作序列）。
	s.params.ReverseTakeProfitPeriod = "15m"
	// 空仓成本 100（直挂状态）；工作序列=金叉序列（100→84.4→84.6），末根
	// 金叉时价格 84.6，空仓浮盈 15.4%。
	seedShort := func() {
		s.state.EnterPosition(100, SideShort)
		s.state.RecordFill(100, 0.1, SideBuy)
		s.state.PositionCount = 1
	}
	seedShort()
	var sig *model.Signal
	for _, c := range decliningThenCrossSeries() {
		r, err := s.OnBar(bar(c), nil)
		if err != nil {
			t.Fatalf("onbar: %v", err)
		}
		if r != nil {
			sig = r
		}
	}
	if sig == nil || sig.Direction != "CLOSE" || !strings.Contains(sig.Reason, "reverse take profit") {
		t.Fatalf("short reverse tp = %+v, want reverse take profit CLOSE", sig)
	}
	if s.state.PendingCloseKind != "reverse_tp" {
		t.Fatalf("pending = %q, want reverse_tp", s.state.PendingCloseKind)
	}
}

// 反向止损触发：浮亏+反向信号 → 全平（币富 #36，不限制补仓）。period=close
// 时反向止损独立生效——无专属周期配置，降级工作周期 MACD 判定。
func TestCRAReverseStopLossTriggers(t *testing.T) {
	s := startReverseContract(t, map[string]any{
		"reverse_stop_loss": true,
		// reverse_take_profit_period 缺省 close → 工作周期（15m）判定。
	})
	// 空仓成本 116（直挂）；工作序列缓涨 100→115.6 后回落至 115.4：全程浮亏，
	// 末根死叉（反向信号）→ 触发反向止损。
	seedLongPosition(s, 116, 0.1)
	var sig *model.Signal
	for _, c := range risingThenCrossDownSeries() {
		r, err := s.OnBar(bar(c), nil)
		if err != nil {
			t.Fatalf("onbar: %v", err)
		}
		if r != nil {
			sig = r
		}
	}
	if sig == nil || sig.Direction != "CLOSE" || sig.Qty != 0 {
		t.Fatalf("reverse sl = %+v, want full CLOSE qty 0", sig)
	}
	if !strings.Contains(sig.Reason, "reverse stop loss") {
		t.Fatalf("reason = %q, want reverse stop loss tag", sig.Reason)
	}
	if !s.state.InPosition || s.state.PendingCloseKind != "reverse_sl" {
		t.Fatalf("state must stay in position with pending reverse_sl close: %+v", s.state)
	}
	// 成交确认 → 全平收尾。
	fillClose(t, s, 115, 0.1, true)
	if s.state.InPosition || s.state.LoopExecuted != 1 {
		t.Fatalf("reverse sl fill must close the loop: %+v", s.state)
	}
}

// 反向止损做空镜像：金叉开空 → 金叉即反向…空仓的反向信号是金叉；价格高于
// 均价（空仓浮亏）时触发。
func TestCRAReverseStopLossShortMirror(t *testing.T) {
	s := startReverseContract(t, map[string]any{
		"direction":         "short",
		"reverse_stop_loss": true,
	})
	// 空仓成本 84.4（金叉序列谷底）；序列 100→84.4→84.6，末根金叉时价格
	// 84.6 > 84.4，空仓浮亏 0.24% → 反向止损触发。之前的 bar 价格低于成本
	//（浮盈）→ 不触发。
	s.state.EnterPosition(84.4, SideShort)
	s.state.RecordFill(84.4, 0.1, SideBuy)
	s.state.PositionCount = 1
	var sig *model.Signal
	for _, c := range decliningThenCrossSeries() {
		r, _ := s.OnBar(bar(c), nil)
		if r != nil {
			sig = r
		}
	}
	if sig == nil || !strings.Contains(sig.Reason, "reverse stop loss") {
		t.Fatalf("short reverse sl = %+v, want reverse stop loss CLOSE", sig)
	}
}

// 反向止损不触发：浮盈时即使有反向信号也不止损（#36 的"判断错误"前提即
// 浮亏）；反向信号缺失时浮亏也不触发。
func TestCRAReverseStopLossNotTriggered(t *testing.T) {
	t.Run("浮盈不触发", func(t *testing.T) {
		s := startReverseContract(t, map[string]any{
			"reverse_stop_loss": true,
		})
		// 成本 100，序列末根死叉时价格 115.4 → 浮盈 15.4%：反向止损绝不触发。
		seedLongPosition(s, 100, 0.1)
		for _, c := range risingThenCrossDownSeries() {
			if sig, _ := s.OnBar(bar(c), nil); sig != nil {
				t.Fatalf("reverse sl must not fire at floating profit: %+v", sig)
			}
		}
	})
	t.Run("无反向信号不触发", func(t *testing.T) {
		s := startReverseContract(t, map[string]any{
			"reverse_stop_loss": true,
		})
		// 成本 116，稳涨序列（无死叉）全程浮亏 → 无反向信号，不触发。
		seedLongPosition(s, 116, 0.1)
		for _, c := range risingThenCrossDownSeries()[:40] {
			if sig, _ := s.OnBar(bar(c), nil); sig != nil {
				t.Fatalf("no reverse cross, must not fire: %+v", sig)
			}
		}
	})
}

// 优先级：常规止损线 > 反向止损/反向止盈 > 常规止盈。
func TestCRAReverseExitPriority(t *testing.T) {
	t.Run("止损线优先于反向止损", func(t *testing.T) {
		s := startReverseContract(t, map[string]any{
			"stop_loss_enabled": true,
			"stop_loss_type":    "ratio",
			"stop_loss_ratio":   0.005, // 0.5%：只在末根（浮亏 0.517%）与死叉同根满足
			"reverse_stop_loss": true,
		})
		// 预填 40 根暖机 bar（未持仓不进 OnBar，避免首单抢跑/止损误触发），
		// 再挂成本 116 的仓位，只把末 3 根走 OnBar——前两根浮亏 <0.5% 且死叉
		// 未现，末根（115.4）浮亏 0.517%（止损达线）与死叉（反向信号）同根成立。
		series := risingThenCrossDownSeries()
		s.bars = makeBars(series[:40]...)
		seedLongPosition(s, 116, 0.1)
		var sig *model.Signal
		for _, c := range series[40:] {
			r, _ := s.OnBar(bar(c), nil)
			if r != nil {
				sig = r
			}
		}
		// 同根 K 线两个条件都成立 → 止损分支在前，先触发；且止损全平不经
		// 在途标记（历史出场形态）。
		if sig == nil || sig.Reason != "cra stop loss" {
			t.Fatalf("priority = %+v, want cra stop loss", sig)
		}
		if s.state.InPosition || s.state.PendingCloseKind != "" {
			t.Fatalf("stop loss must close directly: %+v", s.state)
		}
	})

	t.Run("反向止盈优先于常规止盈", func(t *testing.T) {
		s := startReverseContract(t, map[string]any{
			"take_profit_ratio":          0.005, // 0.5%：浮盈 1% 时常规止盈也达线
			"reverse_take_profit_period": "5m",
		})
		s.SetBarProvider(&fakeBarProvider{series: map[string][]model.Bar{
			"BTCUSDT|5m": makeBars(risingThenCrossDownSeries()...),
		}})
		enterLong(t, s, 100)
		sig, _ := s.OnBar(bar(101), nil)
		// 两个分支同根满足 → 反向分支在前：reason 是反向止盈、记在途标记
		// （常规全仓止盈会立即 ExitPosition 且无 pending）。
		if sig == nil || !strings.Contains(sig.Reason, "reverse take profit") {
			t.Fatalf("priority = %+v, want reverse take profit", sig)
		}
		if !s.state.InPosition || s.state.PendingCloseKind != "reverse_tp" {
			t.Fatalf("reverse branch must win via pending path: %+v", s.state)
		}
	})
}

// 拒单重触发：反向止盈平仓单被拒 → 清除在途标记、仓位原样，下一根 K 线
// 条件仍满足重新发全平信号（与 B 片部分平仓回补同口径）。
func TestCRAReverseExitRejectRearms(t *testing.T) {
	s := startReverseContract(t, map[string]any{
		"reverse_take_profit_period": "5m",
	})
	s.SetBarProvider(&fakeBarProvider{series: map[string][]model.Bar{
		"BTCUSDT|5m": makeBars(risingThenCrossDownSeries()...),
	}})
	enterLong(t, s, 100)

	sig, _ := s.OnBar(bar(101), nil)
	if sig == nil || s.state.PendingCloseKind != "reverse_tp" {
		t.Fatal("reverse tp must be pending")
	}
	// 平仓单被拒（减仓方向 SELL）→ 在途清除、仓位不动。
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusRejected,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if s.state.PendingCloseKind != "" || !s.state.InPosition || !almostEq(s.state.TotalQty, 0.1) {
		t.Fatalf("reject must re-arm without touching position: %+v", s.state)
	}
	// 下一根同价 K 线重新发出反向止盈全平。
	sig, _ = s.OnBar(bar(101), nil)
	if sig == nil || sig.Direction != "CLOSE" || !strings.Contains(sig.Reason, "reverse take profit") {
		t.Fatalf("re-armed reverse tp = %+v, want reverse take profit CLOSE", sig)
	}
}

// 反向信号判定周期接入 A2 副周期订阅：reverse_take_profit_period=5m ≠ 工作
// 周期 → Timeframes 声明 5m；close（关闭）不声明。
func TestCRAReverseTimeframesDeclaration(t *testing.T) {
	s := startReverseContract(t, map[string]any{
		"reverse_take_profit_period": "5m",
	})
	tfs := s.Timeframes()
	if len(tfs) != 1 || tfs[0] != "5m" {
		t.Fatalf("Timeframes = %v, want [5m]", tfs)
	}

	s2 := startReverseContract(t, map[string]any{
		"reverse_stop_loss": true, // period=close：工作周期判定，无需副周期
	})
	if tfs := s2.Timeframes(); len(tfs) != 0 {
		t.Fatalf("Timeframes with period=close = %v, want empty", tfs)
	}
}

// 判定周期扩档（G2-1，前端下拉补齐 30m/1h/4h/8h——币富反向止盈本就适合大周期：
// 4h 金叉开多 1h 死叉卖）：引擎经 A 片 IsFeedablePeriod 已支持全档，此处锁定
// Timeframes 订阅声明与 4h 端到端触发，防 reversePeriodArmed/Timeframes 回归。
func TestCRAReverseTakeProfitExtendedPeriods(t *testing.T) {
	// 判定周期 ≠ 工作周期（15m）的全档副周期都经 Timeframes 声明订阅。
	for _, period := range []string{"5m", "30m", "1h", "4h", "8h"} {
		s := startReverseContract(t, map[string]any{
			"reverse_take_profit_period": period,
		})
		if tfs := s.Timeframes(); len(tfs) != 1 || tfs[0] != period {
			t.Fatalf("period %s: Timeframes = %v, want [%s]", period, tfs, period)
		}
	}
	// 判定周期 = 工作周期（15m）：数据随 OnBar 供给，无需副周期声明。
	s := startReverseContract(t, map[string]any{
		"reverse_take_profit_period": "15m",
	})
	if tfs := s.Timeframes(); len(tfs) != 0 {
		t.Fatalf("period == working timeframe: Timeframes = %v, want empty", tfs)
	}

	// 4h 端到端：4h 副周期供给末根死叉 + 未补仓浮盈 → 触发反向止盈全平。
	s = startReverseContract(t, map[string]any{
		"reverse_take_profit_period": "4h",
	})
	feed := &fakeBarProvider{series: map[string][]model.Bar{
		"BTCUSDT|4h": makeBars(risingThenCrossDownSeries()[:40]...),
	}}
	s.SetBarProvider(feed)
	enterLong(t, s, 100)
	// 浮盈 1% 但 4h 无交叉 → 不触发。
	if sig, _ := s.OnBar(bar(101), nil); sig != nil {
		t.Fatalf("4h feed without cross must not fire: %+v", sig)
	}
	feed.series["BTCUSDT|4h"] = makeBars(risingThenCrossDownSeries()...)
	sig, _ := s.OnBar(bar(101), nil)
	if sig == nil || sig.Direction != "CLOSE" || !strings.Contains(sig.Reason, "reverse take profit") {
		t.Fatalf("4h reverse tp = %+v, want reverse take profit CLOSE", sig)
	}
	if s.state.PendingCloseKind != "reverse_tp" {
		t.Fatalf("pending = %q, want reverse_tp", s.state.PendingCloseKind)
	}
}

// 默认关闭零影响回归：不带反向参数的合约策略在"浮盈+死叉"场景下无任何
// 反向出场（常规分支行为不变）；现货策略即使配了反向参数也不消费（仅合约）。
func TestCRAReverseExitDisabledByDefault(t *testing.T) {
	t.Run("合约默认关闭", func(t *testing.T) {
		s := startReverseContract(t, nil)
		// 成本 116，全程浮亏+末根死叉：反向止损默认关 → 无信号。
		seedLongPosition(s, 116, 0.1)
		for _, c := range risingThenCrossDownSeries() {
			if sig, _ := s.OnBar(bar(c), nil); sig != nil {
				t.Fatalf("reverse features off by default, must not fire: %+v", sig)
			}
		}
		// 常规止盈语义不变：浮盈 50% 达线 → 全仓止盈全平（不经在途标记）。
		sig, _ := s.OnBar(bar(175), nil)
		if sig == nil || sig.Reason != "cra take profit" || sig.Qty != 0 {
			t.Fatalf("regular tp regression = %+v", sig)
		}
		if s.state.InPosition || s.state.PendingCloseKind != "" {
			t.Fatal("regular tp must close directly")
		}
	})

	t.Run("现货不消费反向参数", func(t *testing.T) {
		s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":                     "BTCUSDT",
			"first_order_amount":         10,
			"tp_mode":                    "static",
			"take_profit_ratio":          0.5,
			"profit_callback":            0,
			"enable_add_position":        false,
			"reverse_take_profit_period": "5m",
			"reverse_stop_loss":          true,
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		s.SetBarProvider(&fakeBarProvider{series: map[string][]model.Bar{
			"BTCUSDT|5m": makeBars(risingThenCrossDownSeries()...),
		}})
		enterLong(t, s, 100)
		// 浮亏+5m 死叉：现货不消费反向参数 → 无信号。
		if sig, _ := s.OnBar(bar(99), nil); sig != nil {
			t.Fatalf("spot must ignore reverse stop loss: %+v", sig)
		}
		// 浮盈+5m 死叉：现货同样不触发反向止盈。
		if sig, _ := s.OnBar(bar(101), nil); sig != nil {
			t.Fatalf("spot must ignore reverse take profit: %+v", sig)
		}
	})
}
