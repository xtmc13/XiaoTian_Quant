package cra

import (
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── G1：运行时手动操控四件套（币富 #23/#24/#25/#28）引擎消费口径 ──
//
// 口径总览（详见 manual_action.go/state.go 头注）：
//   - 一键补仓（#24）成交入 Manual 档：计入 TotalQty/TotalCost/AvgEntryPrice，
//     但 PositionCount/PeakAddCount 不推进（自动阶梯不被手动单打乱）；
//   - 自定义减仓（#28）走 ApplyCloseFill FIFO 从首档核销；
//   - 清仓卖出（#23）= 市价全平 + EntryPaused 暂停新首单，策略保持 running；
//   - 关闭补仓（#25）只挡 OnBar 自动补仓分支，止盈照常。

// startSpotWithLadder 启动一个现货 CRA：首单 100 USDT，order_count=3，
// 补仓档 spread 5%/10%、callback 0，静态止盈 1.3% 无回调。
func startSpotWithLadder(t *testing.T) *BaseCRAStrategy {
	t.Helper()
	s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":             "BTCUSDT",
		"first_order_amount": 100,
		"enable_add_position": true,
		"order_count":        3,
		"add_positions": []map[string]any{
			{"order": 1, "multiplier": 1, "spread": 0, "callback": 0},
			{"order": 2, "multiplier": 1, "spread": 0.05, "callback": 0},
			{"order": 3, "multiplier": 2, "spread": 0.10, "callback": 0},
		},
		"tp_mode":            "static",
		"take_profit_ratio":  0.013,
		"take_profit_method": "full",
		"profit_callback":    0, // 达线即触发，测试不依赖回调形态
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

// enterPosition 喂一根 K 线开首单并把首单成交回报灌回策略（模拟 OMS 路由）。
// 返回首单成交 qty。
func enterPosition(t *testing.T, s *BaseCRAStrategy, price float64) float64 {
	t.Helper()
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: price, High: price, Low: price}, nil)
	if err != nil || sig == nil {
		t.Fatalf("first order signal missing: sig=%v err=%v", sig, err)
	}
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: sig.Qty, AvgFillPrice: price, ClientOID: "sig:t1:1",
	}, nil); err != nil {
		t.Fatalf("first fill: %v", err)
	}
	st := s.RuntimeStatus()
	if st["in_position"] != true {
		t.Fatalf("must be in position after first fill: %v", st)
	}
	return sig.Qty
}

// TestCRAManualAddPositionLotNoLadderAdvance 一键补仓：信号 qty 按金额折算，
// 成交入 Manual 档（总量/均价正确），但 PositionCount/PeakAddCount 不推进，
// 自动补仓下一档仍按原梯档序号推进。
func TestCRAManualAddPositionLotNoLadderAdvance(t *testing.T) {
	s := startSpotWithLadder(t)
	qty0 := enterPosition(t, s, 100) // 1 BTC
	if qty0 != 1 {
		t.Fatalf("first qty = %v, want 1", qty0)
	}

	// 手动补仓 50 USDT @100 → 0.5 BTC。
	sig, detail, err := s.ManualAction(map[string]any{"action": "add_position", "amount": 50.0})
	if err != nil {
		t.Fatalf("manual add: %v", err)
	}
	if sig == nil || sig.Direction != "LONG" || sig.Qty != 0.5 {
		t.Fatalf("manual add signal = %+v, want LONG 0.5", sig)
	}
	if sig.Tag != model.SignalTagManual {
		t.Fatalf("signal Tag = %q, want manual", sig.Tag)
	}
	if detail["message"] == nil || detail["message"] == "" {
		t.Fatal("detail must carry human-readable message")
	}

	// 信号发出≠成交：状态未变。
	if st := s.RuntimeStatus(); st["position_qty"] != 1.0 {
		t.Fatalf("qty changed before fill: %v", st["position_qty"])
	}

	// 成交回报（client_oid 带 :manual: 标记，与自动单精确区分）。
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: 0.5, AvgFillPrice: 100, ClientOID: "sig:t1:manual:2",
	}, nil); err != nil {
		t.Fatalf("manual fill: %v", err)
	}
	st := s.RuntimeStatus()
	if st["position_qty"] != 1.5 {
		t.Fatalf("position_qty = %v, want 1.5", st["position_qty"])
	}
	if st["filled_orders"] != 1 {
		t.Fatalf("filled_orders = %v, want 1（手动补仓不推自动阶梯）", st["filled_orders"])
	}
	if st["manual_add_count"] != 1 {
		t.Fatalf("manual_add_count = %v, want 1", st["manual_add_count"])
	}
	if s.state.PeakAddCount != 0 {
		t.Fatalf("PeakAddCount = %d, want 0（手动单不进顺势/燃烧锚点）", s.state.PeakAddCount)
	}
	if len(s.state.Lots) != 2 || !s.state.Lots[1].Manual {
		t.Fatalf("lots = %+v, want 2 lots with tail Manual", s.state.Lots)
	}

	// 自动阶梯接续口径：价格下跌 5% 触发的是自动补仓 #2（而非 #3）。
	addSig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 94, High: 94, Low: 94}, nil)
	if err != nil {
		t.Fatalf("onbar: %v", err)
	}
	addSig, err = s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 93, High: 93, Low: 93}, nil)
	if err != nil {
		t.Fatalf("onbar: %v", err)
	}
	if addSig == nil || !strings.Contains(addSig.Reason, "#2") {
		t.Fatalf("auto add signal = %+v, want 'cra add position #2'（梯档未被手动单顶掉）", addSig)
	}
}

// TestCRAManualAddPositionContractLeverage 合约一键补仓：qty=保证金×杠杆/价；
// 空仓方向时信号为 SHORT（与持仓同向加仓）。
func TestCRAManualAddPositionContractLeverage(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol": "BTCUSDT", "first_order_amount": 100, "leverage": 10,
		"direction": "short", "market_type": "swap",
		"tp_mode": "static", "take_profit_ratio": 0.013,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 100, High: 100, Low: 100}, nil)
	if sig == nil || sig.Direction != "SHORT" {
		t.Fatalf("first signal = %+v, want SHORT", sig)
	}
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusFilled,
		Filled: sig.Qty, AvgFillPrice: 100, ClientOID: "sig:t2:1",
	}, nil); err != nil {
		t.Fatalf("fill: %v", err)
	}

	mSig, _, err := s.ManualAction(map[string]any{"action": "add_position", "amount": 50.0})
	if err != nil {
		t.Fatalf("manual add: %v", err)
	}
	// 50 USDT ×10 杠杆 / 100 = 5 BTC，空仓方向 → SHORT 信号。
	if mSig.Direction != "SHORT" || mSig.Qty != 5 {
		t.Fatalf("contract manual add = %+v, want SHORT 5", mSig)
	}
}

// TestCRAManualActionRequiresPosition 空仓拒绝补仓/减仓/清仓（报错信息指引）。
func TestCRAManualActionRequiresPosition(t *testing.T) {
	s := startSpotWithLadder(t)
	// 先喂一根 K 线有行情但不开仓（不消费信号=未成交，仍为空仓）。
	if _, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 100, High: 100, Low: 100}, nil); err != nil {
		t.Fatalf("onbar: %v", err)
	}
	s.state.ResetForNextLoop() // 清掉信号乐观态，回到纯空仓
	for _, a := range []string{"add_position", "reduce_position", "close_all"} {
		req := map[string]any{"action": a, "amount": 10.0, "ratio": 0.5}
		if a == "reduce_position" {
			delete(req, "amount")
		}
		if _, _, err := s.ManualAction(req); err == nil {
			t.Fatalf("%s on flat position must fail", a)
		}
	}
	// toggle 不依赖持仓。
	if _, d, err := s.ManualAction(map[string]any{"action": "toggle_add_position"}); err != nil || d["add_position_enabled"] != false {
		t.Fatalf("toggle on flat: d=%v err=%v", d, err)
	}
}

// TestCRAManualAddPositionValidation 参数与状态校验：金额/行情/在途平仓。
func TestCRAManualAddPositionValidation(t *testing.T) {
	s := startSpotWithLadder(t)
	enterPosition(t, s, 100)
	if _, _, err := s.ManualAction(map[string]any{"action": "add_position", "amount": 0.0}); err == nil {
		t.Fatal("amount=0 must fail")
	}
	// 在途平仓中拒绝补仓（与平仓单竞争）。
	s.state.PendingCloseKind = "tail"
	if _, _, err := s.ManualAction(map[string]any{"action": "add_position", "amount": 10.0}); err == nil {
		t.Fatal("add during pending close must fail")
	}
	s.state.PendingCloseKind = ""
	// 无行情（未收 K 线）拒绝折算。
	s2 := NewCRASpotStrategy("cra_spot", "BTCUSDT")
	if err := s2.Start(map[string]any{"symbol": "BTCUSDT", "first_order_amount": 100, "tp_mode": "static"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	s2.state.EnterPosition(100, SideLong)
	if _, _, err := s2.ManualAction(map[string]any{"action": "add_position", "amount": 10.0}); err == nil {
		t.Fatal("add without price feed must fail")
	}
	if _, _, err := s.ManualAction(map[string]any{"action": "bogus"}); err == nil {
		t.Fatal("unknown action must fail")
	}
}

// TestCRAManualReducePosition 自定义减仓：数量与比例两种输入，CLOSE+qty 信号，
// 成交走 ApplyCloseFill FIFO 从首档核销，PositionCount 对齐剩余自动档。
func TestCRAManualReducePosition(t *testing.T) {
	s := startSpotWithLadder(t)
	enterPosition(t, s, 100)                       // 首单 1 BTC @100
	s.state.RecordFill(90, 1, SideBuy)             // 自动补仓档 1 BTC @90
	s.state.PositionCount = 2
	s.state.RecordManualFill(80, 0.5)              // 手动档 0.5 BTC @80
	// 当前：总量 2.5，自动档 2（1@100 + 1@90），手动档 1（0.5@80）。

	// 数量路径：减 1.5 → FIFO 核销首档全部 + 第二档 0.5。
	sig, detail, err := s.ManualAction(map[string]any{"action": "reduce_position", "qty": 1.5})
	if err != nil {
		t.Fatalf("manual reduce: %v", err)
	}
	if sig == nil || sig.Direction != "CLOSE" || sig.Qty != 1.5 {
		t.Fatalf("reduce signal = %+v, want CLOSE 1.5", sig)
	}
	if sig.Tag != model.SignalTagManual {
		t.Fatalf("reduce Tag = %q, want manual", sig.Tag)
	}
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusFilled,
		Filled: 1.5, AvgFillPrice: 85, ClientOID: "sig:t3:manual:9",
	}, nil); err != nil {
		t.Fatalf("reduce fill: %v", err)
	}
	if s.state.TotalQty != 1.0 {
		t.Fatalf("TotalQty = %v, want 1.0", s.state.TotalQty)
	}
	if s.state.PositionCount != 1 {
		t.Fatalf("PositionCount = %v, want 1（剩余自动档：0.5@90）", s.state.PositionCount)
	}
	if len(s.state.Lots) != 2 || s.state.Lots[0].Qty != 0.5 || !s.state.Lots[1].Manual {
		t.Fatalf("lots after FIFO consume = %+v", s.state.Lots)
	}
	if detail["remaining"] != 1.0 {
		t.Fatalf("detail remaining = %v, want 1.0", detail["remaining"])
	}

	// 比例路径：再减剩余 50%（0.5）→ FIFO 核销自动档剩余 0.5。
	sig2, _, err := s.ManualAction(map[string]any{"action": "reduce_position", "ratio": 0.5})
	if err != nil {
		t.Fatalf("manual reduce by ratio: %v", err)
	}
	if sig2.Qty != 0.5 {
		t.Fatalf("ratio reduce qty = %v, want 0.5", sig2.Qty)
	}
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusFilled,
		Filled: 0.5, AvgFillPrice: 85, ClientOID: "sig:t3:manual:10",
	}, nil); err != nil {
		t.Fatalf("reduce fill 2: %v", err)
	}
	if s.state.PositionCount != 0 || s.state.TotalQty != 0.5 {
		t.Fatalf("after second reduce: PositionCount=%d TotalQty=%v, want 0 / 0.5（仅剩手动档）",
			s.state.PositionCount, s.state.TotalQty)
	}
	if !s.state.InPosition {
		t.Fatal("still holding manual lot, must stay in position")
	}
}

// TestCRAManualReduceValidation 减仓校验：qty/ratio 互斥、范围、在途平仓。
func TestCRAManualReduceValidation(t *testing.T) {
	s := startSpotWithLadder(t)
	q0 := enterPosition(t, s, 100)
	if _, _, err := s.ManualAction(map[string]any{"action": "reduce_position", "qty": 0.1, "ratio": 0.5}); err == nil {
		t.Fatal("qty+ratio together must fail")
	}
	if _, _, err := s.ManualAction(map[string]any{"action": "reduce_position", "ratio": 1.0}); err == nil {
		t.Fatal("ratio>=1 must fail")
	}
	if _, _, err := s.ManualAction(map[string]any{"action": "reduce_position", "qty": q0}); err == nil {
		t.Fatal("qty>=total must fail (use close_all)")
	}
	if _, _, err := s.ManualAction(map[string]any{"action": "reduce_position"}); err == nil {
		t.Fatal("neither qty nor ratio must fail")
	}
	s.state.PendingCloseKind = "reverse_sl"
	if _, _, err := s.ManualAction(map[string]any{"action": "reduce_position", "ratio": 0.5}); err == nil {
		t.Fatal("reduce during pending close must fail")
	}
}

// TestCRAManualCloseAllPausesEntry 清仓卖出：CLOSE 全平信号 + EntryPaused；
// 成交后空仓、策略保持 running、后续 K 线不再自动开新循环。
func TestCRAManualCloseAllPausesEntry(t *testing.T) {
	s := startSpotWithLadder(t)
	q0 := enterPosition(t, s, 100)

	sig, detail, err := s.ManualAction(map[string]any{"action": "close_all"})
	if err != nil {
		t.Fatalf("close_all: %v", err)
	}
	if sig == nil || sig.Direction != "CLOSE" || sig.Qty != 0 {
		t.Fatalf("close_all signal = %+v, want bare CLOSE（全平）", sig)
	}
	if !s.state.EntryPaused {
		t.Fatal("EntryPaused must be set at signal time（币富 #23 先暂停订单）")
	}
	// 平仓成交确认 → 空仓。
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusFilled,
		Filled: q0, AvgFillPrice: 101, ClientOID: "sig:t4:manual:3",
	}, nil); err != nil {
		t.Fatalf("close fill: %v", err)
	}
	st := s.RuntimeStatus()
	if st["in_position"] != false {
		t.Fatalf("must be flat after close fill: %v", st)
	}
	if st["entry_paused"] != true {
		t.Fatalf("entry_paused must surface in RuntimeStatus: %v", st)
	}
	if st["running"] != true {
		t.Fatal("strategy must stay running after close_all")
	}
	if st["loops_executed"] != 1 {
		t.Fatalf("loops_executed = %v, want 1", st["loops_executed"])
	}
	// 后续 K 线：EntryPaused 挡住新首单（空仓等待，不自动再入场）。
	sig2, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 95, High: 95, Low: 95}, nil)
	if err != nil {
		t.Fatalf("onbar: %v", err)
	}
	if sig2 != nil {
		t.Fatalf("no new entry expected while entry_paused, got %+v", sig2)
	}
	if detail["message"] == nil {
		t.Fatal("detail message missing")
	}
}

// TestCRAManualCloseRejectedRearm 清仓单被拒：在途标记清除可重试，EntryPaused
// 保持置位（币富暂停是独立状态，RuntimeStatus 如实透出）。
func TestCRAManualCloseRejectedRearm(t *testing.T) {
	s := startSpotWithLadder(t)
	enterPosition(t, s, 100)
	if _, _, err := s.ManualAction(map[string]any{"action": "close_all"}); err != nil {
		t.Fatalf("close_all: %v", err)
	}
	// 重复清仓：在途标记未清 → 拒绝。
	if _, _, err := s.ManualAction(map[string]any{"action": "close_all"}); err == nil {
		t.Fatal("second close_all during pending close must fail")
	}
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusRejected,
		ClientOID: "sig:t5:manual:4",
	}, nil); err != nil {
		t.Fatalf("reject update: %v", err)
	}
	if s.state.PendingCloseKind != "" {
		t.Fatal("pending close must be cleared on rejection")
	}
	if !s.state.EntryPaused {
		t.Fatal("EntryPaused stays set after rejection（与币富暂停语义一致）")
	}
	// 可重试。
	sig, _, err := s.ManualAction(map[string]any{"action": "close_all"})
	if err != nil || sig == nil {
		t.Fatalf("close_all retry after rejection must work: sig=%v err=%v", sig, err)
	}
}

// TestCRAToggleAddPosition 关闭补仓：自动补仓分支被跳过，止盈照常；
// 再开启后补仓恢复。开关跨循环存续、重启（Start）清零。
func TestCRAToggleAddPosition(t *testing.T) {
	s := startSpotWithLadder(t)
	enterPosition(t, s, 100)

	// 关闭补仓。
	if _, d, err := s.ManualAction(map[string]any{"action": "toggle_add_position"}); err != nil || d["add_position_enabled"] != false {
		t.Fatalf("toggle off: d=%v err=%v", d, err)
	}
	if st := s.RuntimeStatus(); st["add_position_enabled"] != false {
		t.Fatalf("RuntimeStatus add_position_enabled = %v, want false", st["add_position_enabled"])
	}
	// 跌穿补仓档（5%/10%）两根：补仓本应触发，被开关挡住。
	for _, px := range []float64{94, 89, 88} {
		sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: px, High: px, Low: px}, nil)
		if err != nil {
			t.Fatalf("onbar: %v", err)
		}
		if sig != nil {
			t.Fatalf("add disabled but got signal @%v: %+v", px, sig)
		}
	}
	// 止盈照常：价格回到均价上方 1.3%+ → 全平。
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 102, High: 102, Low: 102}, nil)
	if err != nil {
		t.Fatalf("onbar: %v", err)
	}
	if sig == nil || sig.Direction != "CLOSE" {
		t.Fatalf("take profit must still fire with add disabled, got %+v", sig)
	}
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusFilled,
		Filled: 1, AvgFillPrice: 102, ClientOID: "sig:t6:5",
	}, nil); err != nil {
		t.Fatalf("tp fill: %v", err)
	}

	// 开关跨循环存续：新循环开仓后补仓仍是关闭状态。
	q1 := enterPosition(t, s, 100)
	_ = q1
	if !s.state.AddPositionDisabled {
		t.Fatal("AddPositionDisabled must survive loop reset")
	}
	// 再开启 → 补仓恢复。
	if _, d, err := s.ManualAction(map[string]any{"action": "toggle_add_position", "enabled": true}); err != nil || d["add_position_enabled"] != true {
		t.Fatalf("toggle on: d=%v err=%v", d, err)
	}
	var addSig *model.Signal
	for _, px := range []float64{94, 93} {
		addSig, err = s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: px, High: px, Low: px}, nil)
		if err != nil {
			t.Fatalf("onbar: %v", err)
		}
	}
	if addSig == nil || !strings.Contains(addSig.Reason, "add position") {
		t.Fatalf("add must resume after re-enable, got %+v", addSig)
	}
}

// TestCRAManualRebuildKeepsManualLots 重启重建口径：账本重放保留 Manual 标记，
// 自动阶梯档数只数非手动档（手动补仓重启后依然不推阶梯）。
func TestCRAManualRebuildKeepsManualLots(t *testing.T) {
	fills := []FillRecord{
		{Side: "BUY", Qty: 1, Price: 100},                // 首单（自动）
		{Side: "BUY", Qty: 0.5, Price: 95, Manual: true}, // 手动补仓
		{Side: "BUY", Qty: 1, Price: 90},                 // 自动补仓 #2
		{Side: "SELL", Qty: 1, Price: 92, Manual: true},  // 自定义减仓（":manual:" 标记 → FIFO 核销首档）
	}
	lots, side, prevSide, prevAdds := replayFills(fills)
	if side != SideLong || prevSide != "" || prevAdds != 0 {
		t.Fatalf("side=%v prev=%v/%d", side, prevSide, prevAdds)
	}
	if len(lots) != 2 || !lots[0].Manual {
		t.Fatalf("lots = %+v, want [manual 0.5, auto 1]", lots)
	}
	if n := countAutoLots(lots); n != 1 {
		t.Fatalf("auto lots = %d, want 1", n)
	}

	// parseRestoredLots 全链路：restored_fills 带 manual 键 → PositionCount 只数自动档。
	s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol": "BTCUSDT", "first_order_amount": 100, "tp_mode": "static",
		"restored_position_qty": 1.5, "restored_position_vwap": 91.66666666666667,
		"restored_fills": []any{
			map[string]any{"side": "BUY", "qty": 1.0, "price": 100.0},
			map[string]any{"side": "BUY", "qty": 0.5, "price": 95.0, "manual": true},
			map[string]any{"side": "BUY", "qty": 1.0, "price": 90.0},
			map[string]any{"side": "SELL", "qty": 1.0, "price": 92.0, "manual": true},
		},
	}); err != nil {
		t.Fatalf("start with restore: %v", err)
	}
	st := s.RuntimeStatus()
	if st["filled_orders"] != 1 {
		t.Fatalf("restored filled_orders = %v, want 1（手动档不计自动阶梯）", st["filled_orders"])
	}
	if st["position_qty"] != 1.5 {
		t.Fatalf("restored position_qty = %v, want 1.5", st["position_qty"])
	}
}

// TestCRAManualFillUnmarkedKeepsLegacy 回归：无 :manual: 标记的入场成交仍按
// 自动口径推进阶梯（既有行为零变化）。
func TestCRAManualFillUnmarkedKeepsLegacy(t *testing.T) {
	s := startSpotWithLadder(t)
	enterPosition(t, s, 100)
	s.state.PendingAddCount = 1 // 模拟自动补仓信号在途
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: 1, AvgFillPrice: 94, ClientOID: "sig:t8:7",
	}, nil); err != nil {
		t.Fatalf("fill: %v", err)
	}
	if s.state.PositionCount != 2 || s.state.PendingAddCount != 0 {
		t.Fatalf("auto fill must advance ladder: PositionCount=%d PendingAdd=%d",
			s.state.PositionCount, s.state.PendingAddCount)
	}
	if len(s.state.Lots) != 2 || s.state.Lots[1].Manual {
		t.Fatalf("auto lot must not be manual: %+v", s.state.Lots)
	}
}
