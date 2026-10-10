package cra

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 2026-10-10 生产实锤：信号单风控拒单（net exposure 超限）幽灵持仓 ──
// 事故形态：首根 K 线发 LONG 首单 → OMS 风控拒单（策略 67a503f2 cra_contract
// SOLUSDT dual 100x）→ 策略在信号时已 EnterPosition，拒单终态无人消费 →
// InPosition=true/均价=信号价/TotalQty=0 永久卡住：新入场被 in_position 挡，
// 平仓信号也因账户无仓失败。修复：入场单终态未成交回滚乐观记账 + 当根拦截
// 重发 + RuntimeStatus qty>0 门。

// startEntryRejectContract 启动合约 dual CRA（事故策略同型：SOLUSDT 100x，
// 首根 K 线无门槛发首单——dual 少样本默认做多）。
func startEntryRejectContract(t *testing.T) *BaseCRAStrategy {
	t.Helper()
	s := NewCRAContractStrategy("cra_contract", "SOLUSDT")
	if err := s.Start(map[string]any{
		"symbol":             "SOLUSDT",
		"direction":          "dual",
		"leverage":           100,
		"first_order_amount": 100,
		"tp_mode":            "static",
		"take_profit_ratio":  0.5, // 远离测试价格区间，止盈不干扰
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

func firstOrderBar(close float64, ts int64) model.Bar {
	return model.Bar{Symbol: "SOLUSDT", Interval: "1m", Close: close, High: close, Low: close, Time: ts}
}

// TestCRAFirstOrderRejectedRollsBack 首单被拒：乐观入场记账完全复位，
// 重复拒单事件幂等；RuntimeStatus 全程不报幽灵持仓（qty>0 门）。
func TestCRAFirstOrderRejectedRollsBack(t *testing.T) {
	s := startEntryRejectContract(t)

	sig, err := s.OnBar(firstOrderBar(110.61, 1000), nil)
	if err != nil || sig == nil {
		t.Fatalf("first order signal missing: sig=%v err=%v", sig, err)
	}
	if sig.Direction != "LONG" {
		t.Fatalf("direction = %v, want LONG（dual 少样本默认做多）", sig.Direction)
	}
	// 信号发出即乐观记账（事故起点）。
	if !s.state.InPosition || s.state.EntryPrice != 110.61 {
		t.Fatalf("optimistic entry missing: %+v", s.state)
	}
	// 成交未确认窗口：面板按空仓透出（qty>0 门，事故面板显示"做多/均价/在仓时长"）。
	if st := s.RuntimeStatus(); st["in_position"] != false {
		t.Fatalf("in_position before fill = %v, want false（数量 0 即空仓态）", st["in_position"])
	}
	if st := s.RuntimeStatus(); st["direction"] != nil || st["avg_entry_price"] != nil || st["position_qty"] != nil {
		t.Fatalf("flat panel must not expose direction/avg/qty: %v", st)
	}

	// 风控拒单终态回传（OMS 拒绝路径广播的同型事件）。
	rej := model.OrderData{Symbol: "SOLUSDT", Side: model.SideBuy, Status: model.StatusRejected,
		ClientOID: "sig:67a503f2:1"}
	if _, err := s.OnOrderUpdate(rej, nil); err != nil {
		t.Fatalf("reject update: %v", err)
	}
	// 重复投递（OMS fire + 兜底补发的双事件情形）幂等。
	if _, err := s.OnOrderUpdate(rej, nil); err != nil {
		t.Fatalf("duplicate reject update: %v", err)
	}

	st := s.state
	if st.InPosition || st.EntryPrice != 0 || st.AvgEntryPrice != 0 || st.TotalQty != 0 ||
		st.PositionCount != 0 || st.PendingAddCount != 0 || st.PendingAdd || len(st.Lots) != 0 {
		t.Fatalf("entry must fully roll back after rejection: %+v", st)
	}
	if st.LoopExecuted != 0 {
		t.Fatalf("rejected loop must not consume loop count: %v", st.LoopExecuted)
	}
	if st.EntryRejectedBarTime != 1000 {
		t.Fatalf("EntryRejectedBarTime = %v, want 1000（触发 bar 归因）", st.EntryRejectedBarTime)
	}
	rs := s.RuntimeStatus()
	if rs["in_position"] != false || rs["direction"] != nil {
		t.Fatalf("runtime status after rollback = %v, want flat", rs)
	}
}

// TestCRAFirstOrderRejectedSameBarGate 拒单后同一根 bar 不重发首单
// （防拒单-重发风暴），下一根 bar 条件满足允许重试。
func TestCRAFirstOrderRejectedSameBarGate(t *testing.T) {
	s := startEntryRejectContract(t)

	sig, err := s.OnBar(firstOrderBar(110.61, 1000), nil)
	if err != nil || sig == nil {
		t.Fatalf("first order signal missing: sig=%v err=%v", sig, err)
	}
	if _, err := s.OnOrderUpdate(model.OrderData{Symbol: "SOLUSDT", Side: model.SideBuy,
		Status: model.StatusRejected, ClientOID: "sig:67a503f2:1"}, nil); err != nil {
		t.Fatalf("reject update: %v", err)
	}

	// 同根 bar 重投（feeder 重复供给/调度补发）：不得再发首单。
	for i := 0; i < 3; i++ {
		if sig, err := s.OnBar(firstOrderBar(110.61, 1000), nil); err != nil || sig != nil {
			t.Fatalf("same-bar re-entry must be gated: sig=%v err=%v", sig, err)
		}
	}
	// 下一根 bar：恢复评估，允许重试入场。
	sig2, err := s.OnBar(firstOrderBar(110.60, 2000), nil)
	if err != nil || sig2 == nil {
		t.Fatalf("next bar must retry entry: sig=%v err=%v", sig2, err)
	}
	if s.state.EntryRejectedBarTime != 0 {
		t.Fatalf("gate must reset with the new entry signal: %v", s.state.EntryRejectedBarTime)
	}

	// 重试单成交：正常入档，计数推进。
	if _, err := s.OnOrderUpdate(model.OrderData{Symbol: "SOLUSDT", Side: model.SideBuy,
		Status: model.StatusFilled, Filled: sig2.Qty, AvgFillPrice: 110.60,
		ClientOID: "sig:67a503f2:2"}, nil); err != nil {
		t.Fatalf("retry fill: %v", err)
	}
	if !s.state.InPosition || s.state.PositionCount != 1 || s.state.TotalQty != sig2.Qty {
		t.Fatalf("retry fill bookkeeping wrong: %+v", s.state)
	}
	if st := s.RuntimeStatus(); st["in_position"] != true || st["direction"] != "long" {
		t.Fatalf("runtime status after retry fill = %v, want in_position/long", st)
	}
}

// TestCRAFirstOrderShortRejectedRollsBack 空单首单被拒同样回滚（方向判定
// 对称性：SELL 入场单不得被误判为减仓方向）。
func TestCRAFirstOrderShortRejectedRollsBack(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "SOLUSDT")
	if err := s.Start(map[string]any{
		"symbol": "SOLUSDT", "direction": "short", "leverage": 100,
		"first_order_amount": 100, "tp_mode": "static", "take_profit_ratio": 0.5,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	sig, err := s.OnBar(firstOrderBar(110.61, 1000), nil)
	if err != nil || sig == nil || sig.Direction != "SHORT" {
		t.Fatalf("short first order signal missing: sig=%v err=%v", sig, err)
	}
	if _, err := s.OnOrderUpdate(model.OrderData{Symbol: "SOLUSDT", Side: model.SideSell,
		Status: model.StatusRejected, ClientOID: "sig:67a503f2:1"}, nil); err != nil {
		t.Fatalf("reject update: %v", err)
	}
	if s.state.InPosition || s.state.EntryPrice != 0 {
		t.Fatalf("short entry must roll back after rejection: %+v", s.state)
	}
}

// TestCRAAddOrderRejectedKeepsPosition 补仓单被拒：只回补在途计数，已成交
// 持仓不动；下一根 bar 条件满足可重新触发该档。
func TestCRAAddOrderRejectedKeepsPosition(t *testing.T) {
	s := startSpotWithLadder(t) // BTCUSDT 现货，order=1 档 spread/callback=0
	qty0 := enterPosition(t, s, 100)

	// spread=0/callback=0：首根挂起、次根触发。
	if sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 100, High: 100, Low: 100, Time: 1000}, nil); sig != nil {
		t.Fatalf("first bar should only arm the add trigger, got %+v", sig)
	}
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 100, High: 100, Low: 100, Time: 2000}, nil)
	if err != nil || sig == nil {
		t.Fatalf("add signal missing: sig=%v err=%v", sig, err)
	}
	if s.state.PendingAddCount != 1 {
		t.Fatalf("PendingAddCount = %v, want 1（信号发出记在途）", s.state.PendingAddCount)
	}

	if _, err := s.OnOrderUpdate(model.OrderData{Symbol: "BTCUSDT", Side: model.SideBuy,
		Status: model.StatusRejected, ClientOID: "sig:t5:2"}, nil); err != nil {
		t.Fatalf("reject update: %v", err)
	}
	if s.state.PendingAddCount != 0 {
		t.Fatalf("PendingAddCount = %v, want 0（拒单回补）", s.state.PendingAddCount)
	}
	if !s.state.InPosition || s.state.TotalQty != qty0 || s.state.PositionCount != 1 ||
		s.state.EntryPrice != 100 {
		t.Fatalf("position must survive add rejection: %+v", s.state)
	}

	// 下一根 bar 条件仍满足：该档重新触发（既有拒单重触发语义）。
	if sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 100, High: 100, Low: 100, Time: 3000}, nil); sig != nil {
		t.Fatalf("re-arm bar should only arm, got %+v", sig)
	}
	sig3, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 100, High: 100, Low: 100, Time: 4000}, nil)
	if err != nil || sig3 == nil {
		t.Fatalf("add must re-trigger after rejection: sig=%v err=%v", sig3, err)
	}
	if s.state.PendingAddCount != 1 {
		t.Fatalf("PendingAddCount after re-trigger = %v, want 1", s.state.PendingAddCount)
	}
}
