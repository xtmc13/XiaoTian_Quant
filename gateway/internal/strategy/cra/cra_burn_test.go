package cra

import (
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── F 片：燃烧斩仓（币富名词解释 #41，仅合约）──
//
// 币富原语义（对向燃烧=并行顺势对向单盈利抵消首单浮亏；全局燃烧=跨币种盈利
// 消耗浮亏）在本引擎每循环单侧持仓模型下不可行（执行/平仓/账本重建三层论证
// 见 cra_strategy.go F 片注释），按任务授权的诚实语义实现并在此锁定：
//   对向燃烧 = 补仓达 N 次，本循环一次：市价斩首单档（浮亏最深档），亏损真实
//              实现，换均价下移+保证金释放；
//   全局燃烧 = 补仓达 M 次，本循环一次：市价斩持仓 50%（FIFO 从首档），无跨
//              实例盈利资金源的保守版升级斩仓。

// burnAddLadder 构造 order 1..7 的补仓阶梯（小差价/零回调/等倍，便于价格剧本）。
func burnAddLadder() []any {
	out := make([]any, 0, 7)
	for i := 1; i <= 7; i++ {
		out = append(out, map[string]any{"order": i, "multiplier": 1, "spread": 0.01, "callback": 0})
	}
	return out
}

// startBurnContract 启动合约 CRA：静态止盈 50%/零回调（隔离常规出场）、止损关、
// 7 档等倍小差价补仓梯。extra 覆盖/追加参数（燃烧开关、止盈比例等）。
func startBurnContract(t *testing.T, extra map[string]any) *BaseCRAStrategy {
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
		"enable_add_position": true,
		"order_count":         7,
		"add_positions":       burnAddLadder(),
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

// burnAddOnce 驱动一次补仓成交：同价第一根挂起（差价达标），第二根零回调触发，
// 按信号方向成交回执。多仓剧本价格须低于入场参考，空仓高于。
func burnAddOnce(t *testing.T, s *BaseCRAStrategy, price float64) {
	t.Helper()
	if sig, _ := s.OnBar(bar(price), nil); sig != nil {
		t.Fatalf("add pending bar: unexpected signal %+v", sig)
	}
	sig, err := s.OnBar(bar(price), nil)
	if err != nil || sig == nil {
		t.Fatalf("add trigger: sig=%v err=%v", sig, err)
	}
	if sig.Direction == "CLOSE" {
		t.Fatalf("add trigger returned close: %+v", sig)
	}
	if sig.Direction == "SHORT" {
		fillSell(t, s, price, sig.Qty)
	} else {
		fillBuy(t, s, price, sig.Qty)
	}
}

// 对向燃烧触发：补仓达 threshold 后下一根 K 线发出 CLOSE（量=首单档）、记在途
// 标记与 fired、仓位不乐观变更；成交确认按 FIFO 斩掉首档，均价/总量/档数重算。
func TestCRABurnDualTriggersAtThreshold(t *testing.T) {
	s := startBurnContract(t, map[string]any{
		"burn_dual_enabled":   true,
		"burn_dual_threshold": 2,
	})
	enterLong(t, s, 100) // 首单 0.1 @100
	burnAddOnce(t, s, 98)
	// 补仓 1 次 < 阈值 2：不触发（99.5 差价 0.5% 不挂起补仓，信号只可能是燃烧）。
	if sig, _ := s.OnBar(bar(99.5), nil); sig != nil {
		t.Fatalf("below threshold must not burn: %+v", sig)
	}
	burnAddOnce(t, s, 96) // 补仓 2 次达标：0.104167 @96

	// 触发：CLOSE 量=首档 0.1，在途 burn_dual，fired 置位，仓位原样。
	sig, _ := s.OnBar(bar(95), nil)
	if sig == nil || sig.Direction != "CLOSE" || !almostEq(sig.Qty, 0.1) {
		t.Fatalf("dual burn = %+v, want CLOSE qty 0.1（首单档）", sig)
	}
	if !strings.Contains(sig.Reason, "dual burn") {
		t.Fatalf("reason = %q, want dual burn tag", sig.Reason)
	}
	st := s.state
	if !st.InPosition || st.PendingCloseKind != "burn_dual" || !st.BurnDualFired {
		t.Fatalf("state must stay in position with pending burn_dual fired: %+v", st)
	}
	if !almostEq(st.TotalQty, 0.306208) {
		t.Fatalf("total qty before fill = %v, want 0.306208（未乐观变更）", st.TotalQty)
	}

	// 在途期间：不再发任何信号（防重复出场）。
	if sig, _ := s.OnBar(bar(95), nil); sig != nil {
		t.Fatalf("pending burn must block new signals: %+v", sig)
	}

	// 成交确认（合约镜像平仓单 0.1 @95）：FIFO 斩掉首档 {100,0.1}，
	// 剩余 {98,0.102041}+{96,0.104167}，档数 2、总量/均价重算。
	fillClose(t, s, 95, 0.1, true)
	if st.InPosition != true || st.PendingCloseKind != "" {
		t.Fatalf("partial burn must keep position, clear pending: %+v", st)
	}
	if len(st.Lots) != 2 || !almostEq(st.Lots[0].Price, 98) || !almostEq(st.Lots[1].Price, 96) {
		t.Fatalf("lots after burn = %+v, want head lot consumed (98/96 remain)", st.Lots)
	}
	if st.PositionCount != 2 || !almostEq(st.TotalQty, 0.206208) {
		t.Fatalf("accounting after burn: count=%d qty=%v, want 2 / 0.206208", st.PositionCount, st.TotalQty)
	}
	if want := (98*0.102041 + 96*0.104167) / 0.206208; !almostEq(st.AvgEntryPrice, want) {
		t.Fatalf("avg after burn = %v, want %v（斩首档均价下移）", st.AvgEntryPrice, want)
	}
	if st.AvgEntryPrice >= 97 {
		t.Fatalf("burning the 100-cost head lot must drop avg below 97, got %v", st.AvgEntryPrice)
	}
}

// 每循环一次 + 次循环重置：燃烧成交后补仓阶梯继续运行，再达阈值不再燃烧
// （fired 锁定）；止盈全平结束循环后，新循环达阈值可再次燃烧。
func TestCRABurnOncePerLoopAndResetNextLoop(t *testing.T) {
	s := startBurnContract(t, map[string]any{
		"burn_dual_enabled":   true,
		"burn_dual_threshold": 2,
		"take_profit_ratio":   0.02,
	})
	st := s.state
	enterLong(t, s, 100)
	burnAddOnce(t, s, 98)
	burnAddOnce(t, s, 96) // 补仓 2 次达标
	sig, _ := s.OnBar(bar(95), nil)
	if sig == nil || !strings.Contains(sig.Reason, "dual burn") {
		t.Fatalf("first burn = %+v", sig)
	}
	fillClose(t, s, 95, 0.1, true) // 斩首档，剩余 2 档

	// 补仓阶梯继续：再补 1 次（94）→ 补仓 2 次又达阈值。
	burnAddOnce(t, s, 94)
	// fired 锁定：下一根燃烧不再触发，放行的是补仓 #4 信号（若 fired 失效，
	// 优先级更高的燃烧 CLOSE 会抢在补仓前面）。
	sig, _ = s.OnBar(bar(93), nil) // 挂起补仓 #4
	if sig != nil {
		t.Fatalf("expect add-arming bar, got %+v", sig)
	}
	sig, _ = s.OnBar(bar(93), nil)
	if sig == nil || sig.Direction != "LONG" || !strings.Contains(sig.Reason, "add position") {
		t.Fatalf("fired burn must stay silent, ladder continues: %+v", sig)
	}
	// （补仓 #4 信号不成交，留在途，不影响下方止盈分支——止盈优先于补仓。）

	// 止盈全平（均价≈95.97，100 盈利≈4.2% ≥ 2%）：循环结束，fired 随重置。
	sig, _ = s.OnBar(bar(100), nil)
	if sig == nil || sig.Reason != "cra take profit" || sig.Qty != 0 {
		t.Fatalf("take profit exit = %+v", sig)
	}
	if st.InPosition || st.LoopExecuted != 1 || st.BurnDualFired {
		t.Fatalf("loop must end with fired reset: inPos=%v loops=%d fired=%v",
			st.InPosition, st.LoopExecuted, st.BurnDualFired)
	}

	// 新循环：达阈值再次燃烧（fired 已随循环重置）。
	enterLong(t, s, 100)
	burnAddOnce(t, s, 98)
	burnAddOnce(t, s, 96)
	sig, _ = s.OnBar(bar(95), nil)
	if sig == nil || !strings.Contains(sig.Reason, "dual burn") {
		t.Fatalf("next loop must burn again: %+v", sig)
	}
	if !st.BurnDualFired {
		t.Fatal("next loop burn must set fired again")
	}
}

// 全局燃烧触发：量=持仓 50%（FIFO 从首档核销）；与对向同根同时达标时全局优先；
// 全局成交后若对向阈值仍达标且未 fired，同循环对向可再触发一次（两机制独立）。
func TestCRABurnGlobalTriggersAtThreshold(t *testing.T) {
	s := startBurnContract(t, map[string]any{
		"burn_dual_enabled":    true,
		"burn_dual_threshold":  2,
		"burn_global_enabled":  true,
		"burn_global_threshold": 2,
	})
	enterLong(t, s, 100)
	burnAddOnce(t, s, 98)
	burnAddOnce(t, s, 96) // 补仓 2 次：对向(≥2)与全局(≥2)同根达标

	st := s.state
	// 全局优先：量=RoundQty(0.306208×0.5)=0.153104，fired 只置全局。
	sig, _ := s.OnBar(bar(95), nil)
	if sig == nil || sig.Direction != "CLOSE" || !almostEq(sig.Qty, 0.153104) {
		t.Fatalf("global burn = %+v, want CLOSE qty 0.153104（持仓 50%%）", sig)
	}
	if !strings.Contains(sig.Reason, "global burn") {
		t.Fatalf("reason = %q, want global burn tag", sig.Reason)
	}
	if st.PendingCloseKind != "burn_global" || !st.BurnGlobalFired || st.BurnDualFired {
		t.Fatalf("pending = %q global=%v dual=%v, want burn_global fired only",
			st.PendingCloseKind, st.BurnGlobalFired, st.BurnDualFired)
	}

	// 成交确认：FIFO 斩首档 0.1 + 次档 0.053104 → 次档余 0.048937，档数 2。
	fillClose(t, s, 95, 0.153104, true)
	if len(st.Lots) != 2 || !almostEq(st.Lots[0].Qty, 0.048937) || !almostEq(st.Lots[1].Qty, 0.104167) {
		t.Fatalf("lots after global burn = %+v, want partial head lot 0.048937 + 0.104167", st.Lots)
	}
	if st.PositionCount != 2 || !almostEq(st.TotalQty, 0.153104) {
		t.Fatalf("accounting after global burn: count=%d qty=%v, want 2 / 0.153104", st.PositionCount, st.TotalQty)
	}

	// 两机制独立：再补 1 次（94）→ 补仓 2 次又达对向阈值；全局已 fired 不再
	// 触发，对向未 fired 触发一次，量=当前首档 0.048937。
	burnAddOnce(t, s, 94)
	sig, _ = s.OnBar(bar(93), nil)
	if sig == nil || !strings.Contains(sig.Reason, "dual burn") || !almostEq(sig.Qty, 0.048937) {
		t.Fatalf("dual burn after global = %+v, want dual burn qty 0.048937", sig)
	}
	if !st.BurnDualFired {
		t.Fatal("dual burn must set its own fired")
	}
}

// 拒单重试：燃烧平仓单被拒 → 在途与 fired 同步清除重新武装，仓位不动，
// 下一根 K 线重新发出同样的燃烧信号（币富备注：卖出被拒不许静默假成功）。
func TestCRABurnRejectRearms(t *testing.T) {
	s := startBurnContract(t, map[string]any{
		"burn_dual_enabled":   true,
		"burn_dual_threshold": 1,
	})
	enterLong(t, s, 100)
	burnAddOnce(t, s, 98)

	st := s.state
	sig, _ := s.OnBar(bar(97), nil)
	if sig == nil || st.PendingCloseKind != "burn_dual" || !st.BurnDualFired {
		t.Fatalf("burn must be pending+fired: sig=%+v state=%+v", sig, st)
	}
	// 平仓单被拒（多仓减仓方向 SELL）→ 在途清除、fired 清除、仓位原样。
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusRejected,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if st.PendingCloseKind != "" || st.BurnDualFired || !st.InPosition || !almostEq(st.TotalQty, 0.202041) {
		t.Fatalf("reject must re-arm without touching position: %+v", st)
	}
	// 下一根同价 K 线重新发出对向燃烧（量仍为首档 0.1）。
	sig, _ = s.OnBar(bar(97), nil)
	if sig == nil || sig.Direction != "CLOSE" || !almostEq(sig.Qty, 0.1) || !strings.Contains(sig.Reason, "dual burn") {
		t.Fatalf("re-armed burn = %+v, want dual burn CLOSE qty 0.1", sig)
	}
	if !st.BurnDualFired {
		t.Fatal("re-armed burn must set fired again")
	}
}

// 做空镜像：空仓补仓（价格上涨）达阈值同样触发；减仓方向 BUY 成交确认斩首档。
func TestCRABurnShortMirror(t *testing.T) {
	s := startBurnContract(t, map[string]any{
		"direction":           "short",
		"burn_dual_enabled":   true,
		"burn_dual_threshold": 1,
	})
	// 首单 SHORT @100。
	sig, err := s.OnBar(bar(100), nil)
	if err != nil || sig == nil || sig.Direction != "SHORT" {
		t.Fatalf("first order: sig=%+v err=%v", sig, err)
	}
	fillSell(t, s, 100, sig.Qty) // 0.1 @100
	burnAddOnce(t, s, 101)       // 空仓补仓：价格上涨 1% 达标，0.09901 @101

	// 触发燃烧：CLOSE 量=首档 0.1。
	sig, _ = s.OnBar(bar(100), nil)
	if sig == nil || sig.Direction != "CLOSE" || !almostEq(sig.Qty, 0.1) || !strings.Contains(sig.Reason, "dual burn") {
		t.Fatalf("short dual burn = %+v, want CLOSE qty 0.1", sig)
	}
	st := s.state
	if st.PendingCloseKind != "burn_dual" || !st.BurnDualFired {
		t.Fatalf("pending = %q fired=%v", st.PendingCloseKind, st.BurnDualFired)
	}
	// 空仓减仓方向 BUY 成交确认 → FIFO 斩首档 {100,0.1}，余 {101,0.09901}。
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: 0.1, Quantity: 0.1, AvgFillPrice: 100,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if len(st.Lots) != 1 || !almostEq(st.Lots[0].Price, 101) || !almostEq(st.TotalQty, 0.09901) {
		t.Fatalf("lots after short burn = %+v qty=%v, want head consumed", st.Lots, st.TotalQty)
	}
}

// 优先级：止盈达线时常规止盈全平优先于燃烧（燃烧不抢跑、不置 fired）。
func TestCRABurnPriorityAfterTakeProfit(t *testing.T) {
	s := startBurnContract(t, map[string]any{
		"burn_dual_enabled":   true,
		"burn_dual_threshold": 1,
		"take_profit_ratio":   0.02,
	})
	enterLong(t, s, 100)
	burnAddOnce(t, s, 98) // 均价≈98.99
	// 101 盈利≈2.03% 达线，同根 K 线燃烧阈值也达标 → 止盈分支在前：全平，
	// 不经在途标记，fired 不置位。
	sig, _ := s.OnBar(bar(101), nil)
	if sig == nil || sig.Reason != "cra take profit" || sig.Qty != 0 {
		t.Fatalf("priority = %+v, want regular full take profit", sig)
	}
	st := s.state
	if st.InPosition || st.PendingCloseKind != "" || st.BurnDualFired {
		t.Fatalf("tp must close directly without burn: %+v", st)
	}
}

// 阈值守卫：threshold<1 视为关闭（防 enabled+0 首单后即触发）；fired 标记在
// evaluateBurn 层直接锁定（同循环重复调用不触发）。
func TestCRABurnThresholdGuards(t *testing.T) {
	for _, extra := range []map[string]any{
		{"burn_dual_enabled": true, "burn_dual_threshold": 0},
		{"burn_dual_enabled": true, "burn_dual_threshold": -2},
		{"burn_global_enabled": true, "burn_global_threshold": 0},
	} {
		s := startBurnContract(t, extra)
		enterLong(t, s, 100)
		burnAddOnce(t, s, 98)
		burnAddOnce(t, s, 96)
		for i := 0; i < 3; i++ {
			if sig, _ := s.OnBar(bar(99.5), nil); sig != nil {
				t.Fatalf("threshold<1 must disable burn: extra=%v sig=%+v", extra, sig)
			}
		}
		if s.state.BurnDualFired || s.state.BurnGlobalFired || s.state.PendingCloseKind != "" {
			t.Fatalf("no burn state allowed: extra=%v state=%+v", extra, s.state)
		}
	}

	// fired 标记单元级锁定：达阈值但已 fired → evaluateBurn 不触发（两机制）。
	s := startBurnContract(t, map[string]any{
		"burn_dual_enabled":    true,
		"burn_dual_threshold":  1,
		"burn_global_enabled":  true,
		"burn_global_threshold": 1,
	})
	seedLongPosition(s, 100, 0.1)
	s.state.RecordFill(98, 0.102041, SideBuy)
	s.state.PositionCount = 2
	s.state.BurnDualFired = true
	s.state.BurnGlobalFired = true
	if ok, _, _ := s.evaluateBurn(); ok {
		t.Fatal("fired flags must block retrigger at threshold")
	}
}

// 默认关闭零影响回归：不带燃烧参数（及显式 false）的合约策略在被套场景下无
// 任何燃烧信号/状态；现货策略即使配了燃烧参数也不消费（仅合约）。
func TestCRABurnDisabledByDefault(t *testing.T) {
	trap := func(t *testing.T, s *BaseCRAStrategy) {
		enterLong(t, s, 100)
		burnAddOnce(t, s, 98)
		burnAddOnce(t, s, 96)
		// 99.5：差价 0.5% < 补仓差价 1%，补仓不挂起——任何信号只能是出场类。
		for i := 0; i < 3; i++ {
			if sig, _ := s.OnBar(bar(99.5), nil); sig != nil {
				t.Fatalf("burn off, must not fire: %+v", sig)
			}
		}
		st := s.state
		if st.BurnDualFired || st.BurnGlobalFired || st.PendingCloseKind != "" {
			t.Fatalf("burn off, no burn state allowed: %+v", st)
		}
		// RuntimeStatus 默认不新增燃烧键（严格零行为变化）。
		rs := s.RuntimeStatus()
		if _, ok := rs["burn_dual_fired"]; ok {
			t.Fatal("default off must not expose burn_dual_fired")
		}
		if _, ok := rs["burn_global_fired"]; ok {
			t.Fatal("default off must not expose burn_global_fired")
		}
	}

	t.Run("合约默认关闭", func(t *testing.T) {
		trap(t, startBurnContract(t, nil))
	})
	t.Run("合约显式关闭", func(t *testing.T) {
		trap(t, startBurnContract(t, map[string]any{
			"burn_dual_enabled":    false,
			"burn_dual_threshold":  1,
			"burn_global_enabled":  false,
			"burn_global_threshold": 1,
		}))
	})

	t.Run("现货不消费燃烧参数", func(t *testing.T) {
		s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":                "BTCUSDT",
			"first_order_amount":    10,
			"tp_mode":               "static",
			"take_profit_ratio":     0.5,
			"profit_callback":       0,
			"enable_add_position":   false,
			"burn_dual_enabled":     true,
			"burn_dual_threshold":   1,
			"burn_global_enabled":   true,
			"burn_global_threshold": 1,
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		enterLong(t, s, 100)
		fillBuy(t, s, 98, 0.102041) // 直挂两笔"补仓"成交 → PositionCount=3
		fillBuy(t, s, 96, 0.104167)
		if sig, _ := s.OnBar(bar(95), nil); sig != nil {
			t.Fatalf("spot must ignore burn params: %+v", sig)
		}
		if s.state.BurnDualFired || s.state.BurnGlobalFired {
			t.Fatal("spot must never set burn fired flags")
		}
	})
}

// RuntimeStatus 透出：开关启用时暴露对应 fired 键（默认关闭不新增键，见上）。
func TestCRABurnRuntimeStatus(t *testing.T) {
	s := startBurnContract(t, map[string]any{
		"burn_dual_enabled":   true,
		"burn_dual_threshold": 1,
	})
	enterLong(t, s, 100)
	rs := s.RuntimeStatus()
	if v, ok := rs["burn_dual_fired"]; !ok || v.(bool) {
		t.Fatalf("burn_dual_fired = %v/%v, want present false", v, ok)
	}
	if _, ok := rs["burn_global_fired"]; ok {
		t.Fatal("global not enabled must not expose burn_global_fired")
	}
	burnAddOnce(t, s, 98)
	if sig, _ := s.OnBar(bar(97), nil); sig == nil || !strings.Contains(sig.Reason, "dual burn") {
		t.Fatalf("burn must fire: %+v", sig)
	}
	if v := s.RuntimeStatus()["burn_dual_fired"].(bool); !v {
		t.Fatal("burn_dual_fired must be true after trigger")
	}
}
