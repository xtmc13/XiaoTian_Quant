package cra

import (
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── H1 片：合约空单（short）重启仓位重建 ──
//
// 覆盖：纯空单逐笔重放全生命周期、超量反向成交翻向口径、恢复空单后的
// 补仓/止盈（尾单部分平+全平）/止损/反向止损/燃烧斩仓端到端、聚合兜底
// 合成空单档、E 片顺势锚点在空单循环的重写一致性。

// 纯空单序列逐笔重放：开空→补空→部分平空（尾单形态）→全平。
func TestRebuildLotsFromFillsShortLifecycle(t *testing.T) {
	fills := []FillRecord{
		{Side: "SELL", Qty: 1, Price: 100}, // 开空
		{Side: "SELL", Qty: 2, Price: 104}, // 补空
		{Side: "BUY", Qty: 2, Price: 101},  // ≈尾档量 → 尾单止盈平尾档
		{Side: "BUY", Qty: 1, Price: 98},   // ≈剩余总量 → 全平
	}
	// 前缀 1：开空后 = 空单 1 档。
	lots, side := RebuildLotsFromFills(fills[:1])
	if side != SideShort || len(lots) != 1 || !almostEq(lots[0].Price, 100) {
		t.Fatalf("开空重建 = %v/%+v, want short [{100,1}]", side, lots)
	}
	// 前缀 2：补空后 = 空单 2 档。
	lots, side = RebuildLotsFromFills(fills[:2])
	if side != SideShort || len(lots) != 2 || !almostEq(lots[1].Price, 104) || !almostEq(lots[1].Qty, 2) {
		t.Fatalf("补空重建 = %v/%+v, want short [{100,1},{104,2}]", side, lots)
	}
	// 前缀 3：部分平空（≈尾档）后 = 剩首档。
	lots, side = RebuildLotsFromFills(fills[:3])
	if side != SideShort || len(lots) != 1 || !almostEq(lots[0].Price, 100) || !almostEq(lots[0].Qty, 1) {
		t.Fatalf("部分平空重建 = %v/%+v, want short [{100,1}]", side, lots)
	}
	// 全序列：全平 → 空仓；顺势锚点 = 本循环 short/峰值补仓 1 次。
	lots, side = RebuildLotsFromFills(fills)
	if len(lots) != 0 {
		t.Fatalf("全平后 lots = %+v, want empty", lots)
	}
	prevSide, prevAdds := RebuildLoopMemoryFromFills(fills)
	if prevSide != SideShort || prevAdds != 1 {
		t.Fatalf("空单循环锚点 = %s/%d, want short/1", prevSide, prevAdds)
	}
}

// 超量反向成交 = 全平并翻向开仓（净额口径）：多仓 2 被 SELL 5 超平 → 空单 3@90，
// 顺势锚点记录多仓循环（long/1）；再 BUY 10 超平 → 翻多 7@95，锚点重写 short/0。
// 与 NetFilledByStrategy 带符号净额严格一致（闸门方向与重放方向不再矛盾）。
func TestRebuildLotsFromFillsOverCloseFlip(t *testing.T) {
	fills := []FillRecord{
		{Side: "BUY", Qty: 1, Price: 100},
		{Side: "BUY", Qty: 1, Price: 98},
		{Side: "SELL", Qty: 5, Price: 90}, // 超量：全平 2 + 翻空 3
	}
	lots, side := RebuildLotsFromFills(fills)
	if side != SideShort || len(lots) != 1 || !almostEq(lots[0].Price, 90) || !almostEq(lots[0].Qty, 3) {
		t.Fatalf("翻空重建 = %v/%+v, want short [{90,3}]", side, lots)
	}
	prevSide, prevAdds := RebuildLoopMemoryFromFills(fills)
	if prevSide != SideLong || prevAdds != 1 {
		t.Fatalf("翻向锚点 = %s/%d, want long/1（多仓循环被套 1 次后结束）", prevSide, prevAdds)
	}

	fills = append(fills, FillRecord{Side: "BUY", Qty: 10, Price: 95}) // 再超平翻多 7
	lots, side = RebuildLotsFromFills(fills)
	if side != SideLong || len(lots) != 1 || !almostEq(lots[0].Price, 95) || !almostEq(lots[0].Qty, 7) {
		t.Fatalf("翻多重建 = %v/%+v, want long [{95,7}]", side, lots)
	}
	prevSide, prevAdds = RebuildLoopMemoryFromFills(fills)
	if prevSide != SideShort || prevAdds != 0 {
		t.Fatalf("再翻向锚点 = %s/%d, want short/0（空单循环单档干净结束）", prevSide, prevAdds)
	}
}

// 燃烧标记推断的翻向口径：旧循环已燃烧（首档量减仓形态）后被超量平仓翻向 →
// 标记随旧循环清零；新循环再出现首档量减仓形态可重新识别。
func TestRebuildBurnMarksFromFillsOverCloseFlip(t *testing.T) {
	fills := []FillRecord{
		{Side: "BUY", Qty: 1, Price: 100},
		{Side: "BUY", Qty: 2, Price: 98},
		{Side: "SELL", Qty: 1, Price: 99}, // ≈首档量且不≈尾档/全量 → 对向燃烧形态
		{Side: "SELL", Qty: 5, Price: 95}, // 超量：全平余 2 + 翻空 3@95，标记清零
		{Side: "SELL", Qty: 1, Price: 96}, // 补空（档集 [3@95,1@96]，自动档 2）
		{Side: "BUY", Qty: 3, Price: 90},  // ≈首档量 → 新循环对向燃烧形态
	}
	dual, global := RebuildBurnMarksFromFills(fills, 1, 3, 0.5)
	if !dual || global {
		t.Fatalf("burn marks = dual:%v global:%v, want dual:true global:false（翻向清零后重新识别）", dual, global)
	}
}

// 恢复空单端到端：restored_fills 重建空仓 → 价格上涨触发补空（SHORT 信号）→
// 下跌达止盈全平 → 顺势锚点重写为 short/1 → 下一循环照常开空。
func TestCRARestoreShortEndToEnd(t *testing.T) {
	// 以重启注入参数启动（handler 生产路径同形态：fills 优先于聚合）。
	s2 := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	params := map[string]any{
		"symbol":              "BTCUSDT",
		"timeframe":           "15m",
		"direction":           "short",
		"first_order_amount":  10,
		"leverage":            1,
		"tp_mode":             "static",
		"take_profit_ratio":   0.02,
		"profit_callback":     0,
		"stop_loss_enabled":   false,
		"enable_add_position": true,
		"order_count":         7,
		"add_positions":       burnAddLadder(),
		// 重启前持仓：开空 1.0@100。
		"restored_position_qty":  1.0,
		"restored_position_vwap": 100.0,
		"restored_position_side": "short",
		"restored_fills": []any{
			map[string]any{"side": "SELL", "qty": 1.0, "price": 100.0},
		},
	}
	if err := s2.Start(params); err != nil {
		t.Fatalf("start: %v", err)
	}
	st := s2.state
	if !st.InPosition || st.Side != SideShort || !almostEq(st.TotalQty, 1.0) || !almostEq(st.AvgEntryPrice, 100) {
		t.Fatalf("restored short = %+v", st)
	}
	if st.PositionCount != 1 || st.EntryPrice != 100 {
		t.Fatalf("count/entry = %d/%v, want 1/100", st.PositionCount, st.EntryPrice)
	}

	// 补仓=价格上涨触发（空单被套方向）：102 挂起（涨幅 2%≥差价 1%），同价
	// 零回调触发 → SHORT 补仓信号。
	if sig, _ := s2.OnBar(bar(102), nil); sig != nil {
		t.Fatalf("add pending: unexpected signal %+v", sig)
	}
	sig, err := s2.OnBar(bar(102), nil)
	if err != nil || sig == nil || sig.Direction != "SHORT" {
		t.Fatalf("short add must fire on price rise: sig=%+v err=%v", sig, err)
	}
	fillSell(t, s2, 102, sig.Qty) // ≈0.098039
	if st.PositionCount != 2 || st.PeakAddCount != 1 {
		t.Fatalf("after add: count=%d peak=%d, want 2/1", st.PositionCount, st.PeakAddCount)
	}

	// 止盈=下跌盈利：均价 ≈100.18，97 盈利 ≈3.2% ≥ 2% → 全平 CLOSE（不带量）。
	sig, err = s2.OnBar(bar(97), nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" || sig.Qty != 0 {
		t.Fatalf("short take profit: sig=%+v err=%v", sig, err)
	}
	if st.InPosition || st.LoopExecuted != 1 {
		t.Fatalf("loop must complete: inPos=%v loops=%d", st.InPosition, st.LoopExecuted)
	}
	// E 片锚点：空单循环以 1 次补仓结束。
	if st.PrevLoopSide != SideShort || st.PrevLoopTrappedAdds != 1 {
		t.Fatalf("anchor = %s/%d, want short/1", st.PrevLoopSide, st.PrevLoopTrappedAdds)
	}
	// 下一循环照常开空（direction=short）。
	sig, err = s2.OnBar(bar(97), nil)
	if err != nil || sig == nil || sig.Direction != "SHORT" {
		t.Fatalf("next loop first order: sig=%+v err=%v", sig, err)
	}
}

// 恢复空单的尾单止盈：价格下跌使尾档（高价开空档）盈利达线 → 只平尾档
// （BUY 减仓成交确认后核销尾档）；余档再达线 → 退化全平。
func TestCRARestoredShortTailTakeProfit(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":              "BTCUSDT",
		"timeframe":           "15m",
		"direction":           "short",
		"first_order_amount":  10,
		"leverage":            1,
		"tp_mode":             "static",
		"take_profit_method":  "tail",
		"take_profit_ratio":   0.02,
		"profit_callback":     0,
		"enable_add_position": false,
		// 重启前持仓：开空 1@100 + 补空 1@104（尾档成本 104）。
		"restored_position_qty":  2.0,
		"restored_position_vwap": 102.0,
		"restored_position_side": "short",
		"restored_fills": []any{
			map[string]any{"side": "SELL", "qty": 1.0, "price": 100.0},
			map[string]any{"side": "SELL", "qty": 1.0, "price": 104.0},
		},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	st := s.state
	if st.Side != SideShort || len(st.Lots) != 2 || st.PositionCount != 2 {
		t.Fatalf("restored lots = %+v", st.Lots)
	}
	// 尾档成本 104，101 盈利 2.88% 达线（均价 102 仅 0.98%，全仓不触发）→
	// 只平尾档 1.0，记在途。
	sig, err := s.OnBar(bar(101), nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" || !almostEq(sig.Qty, 1.0) {
		t.Fatalf("short tail tp = %+v, want CLOSE qty 1.0", sig)
	}
	if !strings.Contains(sig.Reason, "tail") || st.PendingCloseKind != "tail" {
		t.Fatalf("reason/pending = %q/%q", sig.Reason, st.PendingCloseKind)
	}
	// BUY 减仓成交确认 → 核销尾档，余首档 1@100。
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: 1.0, Quantity: 1.0, AvgFillPrice: 101,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if !st.InPosition || len(st.Lots) != 1 || !almostEq(st.Lots[0].Price, 100) || st.PositionCount != 1 {
		t.Fatalf("after tail close: lots=%+v count=%d", st.Lots, st.PositionCount)
	}
	// 余档（成本 100）97.9 盈利 2.1% 达线 → 仅剩一档退化全平（不带量 CLOSE）。
	sig, err = s.OnBar(bar(97.9), nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" || sig.Qty != 0 {
		t.Fatalf("final close = %+v, want full CLOSE qty 0", sig)
	}
	if st.InPosition || st.LoopExecuted != 1 {
		t.Fatalf("loop must complete: %+v", st)
	}
}

// 恢复空单的止损：价格上涨亏损达线 → 全平（合约止损分支）。
func TestCRARestoredShortStopLoss(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":                 "BTCUSDT",
		"timeframe":              "15m",
		"direction":              "short",
		"first_order_amount":     10,
		"leverage":               1,
		"tp_mode":                "static",
		"take_profit_ratio":      0.5,
		"stop_loss_enabled":      true,
		"stop_loss_type":         "ratio",
		"stop_loss_ratio":        0.01,
		"enable_add_position":    false,
		"restored_position_qty":  1.0,
		"restored_position_vwap": 100.0,
		"restored_position_side": "short",
		"restored_fills": []any{
			map[string]any{"side": "SELL", "qty": 1.0, "price": 100.0},
		},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	// 100.8 亏损 0.8% < 1% 不触发；101.5 亏损 1.5% ≥ 1% → 止损全平。
	if sig, _ := s.OnBar(bar(100.8), nil); sig != nil {
		t.Fatalf("below sl must not fire: %+v", sig)
	}
	sig, err := s.OnBar(bar(101.5), nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" || sig.Reason != "cra stop loss" {
		t.Fatalf("short stop loss = %+v err=%v", sig, err)
	}
	if s.state.InPosition || s.state.LoopExecuted != 1 {
		t.Fatalf("sl must close the loop: %+v", s.state)
	}
	if s.state.PrevLoopSide != SideShort {
		t.Fatalf("anchor side = %s, want short", s.state.PrevLoopSide)
	}
}

// 恢复空单的反向止损（C 片链路在恢复态生效）：空仓浮亏 + 判定序列金叉 → 全平，
// 经 PendingClose 在途标记，BUY 成交确认收尾。
func TestCRARestoredShortReverseExit(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":              "BTCUSDT",
		"timeframe":           "15m",
		"direction":           "short",
		"first_order_amount":  10,
		"leverage":            1,
		"tp_mode":             "static",
		"take_profit_ratio":   0.5,
		"profit_callback":     0,
		"stop_loss_enabled":   false,
		"enable_add_position": false,
		"reverse_stop_loss":   true,
		// 重启前持仓：开空 0.1@84.4。
		"restored_position_qty":  0.1,
		"restored_position_vwap": 84.4,
		"restored_position_side": "short",
		"restored_fills": []any{
			map[string]any{"side": "SELL", "qty": 0.1, "price": 84.4},
		},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !s.state.InPosition || s.state.Side != SideShort || s.state.PositionCount != 1 {
		t.Fatalf("restored short = %+v", s.state)
	}
	// 工作序列=金叉序列（100→84.4→84.6）：末根金叉时价格 84.6 > 成本 84.4
	// （浮亏 0.24%）→ 反向止损触发；此前的 bar 浮盈或无交叉不触发。
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
	if sig == nil || sig.Direction != "CLOSE" || !strings.Contains(sig.Reason, "reverse stop loss") {
		t.Fatalf("restored short reverse sl = %+v, want reverse stop loss CLOSE", sig)
	}
	if s.state.PendingCloseKind != "reverse_sl" || !s.state.InPosition {
		t.Fatalf("pending reverse_sl expected: %+v", s.state)
	}
	// BUY 平仓成交确认 → 全平收尾。
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: 0.1, Quantity: 0.1, AvgFillPrice: 84.6, ClosePosition: true,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if s.state.InPosition || s.state.LoopExecuted != 1 {
		t.Fatalf("reverse sl fill must close the loop: %+v", s.state)
	}
}

// 恢复空单的燃烧斩仓（F 片链路在恢复态生效）：重建的 2 档空单补仓数达阈值 →
// 斩首档（浮亏最深的首起档），BUY 成交确认核销。
func TestCRARestoredShortBurn(t *testing.T) {
	// 以重启注入重建：开空 0.1@100 + 补空 0.09901@101（自动档 2 = 补仓 1 次）。
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":                 "BTCUSDT",
		"timeframe":              "15m",
		"direction":              "short",
		"first_order_amount":     10,
		"leverage":               1,
		"tp_mode":                "static",
		"take_profit_ratio":      0.5,
		"profit_callback":        0,
		"stop_loss_enabled":      false,
		"enable_add_position":    true,
		"order_count":            7,
		"add_positions":          burnAddLadder(),
		"burn_dual_enabled":      true,
		"burn_dual_threshold":    1,
		"restored_position_qty":  0.19901,
		"restored_position_vwap": 100.5,
		"restored_position_side": "short",
		"restored_fills": []any{
			map[string]any{"side": "SELL", "qty": 0.1, "price": 100.0},
			map[string]any{"side": "SELL", "qty": 0.09901, "price": 101.0},
		},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	st := s.state
	if st.Side != SideShort || st.PositionCount != 2 || len(st.Lots) != 2 {
		t.Fatalf("restored short lots = %+v count=%d", st.Lots, st.PositionCount)
	}
	// 补仓数 1 ≥ 阈值 1 → 下一根 K 线斩首档（CLOSE 量=首档 0.1），置 fired。
	sig, err := s.OnBar(bar(100), nil)
	if err != nil || sig == nil || sig.Direction != "CLOSE" || !almostEq(sig.Qty, 0.1) ||
		!strings.Contains(sig.Reason, "dual burn") {
		t.Fatalf("restored short burn = %+v err=%v", sig, err)
	}
	if st.PendingCloseKind != "burn_dual" || !st.BurnDualFired {
		t.Fatalf("pending/fired = %q/%v", st.PendingCloseKind, st.BurnDualFired)
	}
	// BUY 减仓成交确认 → FIFO 斩首档 {100,0.1}，余 {101,0.09901}。
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: 0.1, Quantity: 0.1, AvgFillPrice: 100,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if len(st.Lots) != 1 || !almostEq(st.Lots[0].Price, 101) || !st.InPosition {
		t.Fatalf("lots after burn = %+v, want [{101,0.09901}]", st.Lots)
	}
}

// 聚合兜底合成空单档：无逐笔明细时按 restored_position_side=short 合成
// （EntryPrice=开仓 VWAP）——dual 配置的空单不再被误恢复为多单（修复前
// 方向取参数 direction，dual 恒落多）。
func TestCRARestoreShortAggregateFallback(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":                 "BTCUSDT",
		"timeframe":              "15m",
		"direction":              "dual", // dual 也必须尊重注入方向
		"first_order_amount":     10,
		"leverage":               1,
		"tp_mode":                "static",
		"take_profit_method":     "tail",
		"take_profit_ratio":      0.02,
		"profit_callback":        0,
		"enable_add_position":    false,
		"restored_position_qty":  0.7,
		"restored_position_vwap": 99.5,
		"restored_position_side": "short",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	st := s.state
	if !st.InPosition || st.Side != SideShort {
		t.Fatalf("aggregate short restore side = %v, want short", st.Side)
	}
	if len(st.Lots) != 1 || !almostEq(st.Lots[0].Price, 99.5) || !almostEq(st.Lots[0].Qty, 0.7) {
		t.Fatalf("aggregate short lot = %+v, want [{99.5,0.7}]", st.Lots)
	}
	if st.EntryPrice != 99.5 || !almostEq(st.AvgEntryPrice, 99.5) {
		t.Fatalf("entry/avg = %v/%v, want 99.5/99.5", st.EntryPrice, st.AvgEntryPrice)
	}
	// 档级止盈方向正确：97.1 盈利 2.4% 达线触发；101 亏损不触发。
	if ok, _ := st.CheckTailTakeProfit(101, 0.02, 0); ok {
		t.Fatal("short lot at loss must not trigger tail tp")
	}
	if ok, qty := st.CheckTailTakeProfit(97.1, 0.02, 0); !ok || !almostEq(qty, 0.7) {
		t.Fatalf("short aggregate tail tp = (%v, %v), want (true, 0.7)", ok, qty)
	}
}
