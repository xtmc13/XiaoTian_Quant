package strategy

import (
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── A1.4 马丁格尔安全硬限测试（max_layers / max_total_budget）──
//
// 覆盖 MartinStrategy（martin_trend，aggressive/conservative 预设同款）：
//   - 到层数停：达到 max_layers 后停止加仓，止盈监控仍生效；
//   - 到预算拒：累计投入达到 max_total_budget 后拒绝再加；
//   - 未成交不推进：加仓信号发出但无成交回报时层数不推进、不重复加仓；
//   - 成交回报后才允许推进到下一层。

func bar(close, low float64) model.Bar {
	return model.Bar{Symbol: "BTCUSDT", Close: close, Low: low, High: close, Open: close, Time: 1700000000000}
}

func startMartin(t *testing.T, params map[string]any) *MartinStrategy {
	t.Helper()
	s := NewMartinStrategy()
	m := map[string]any{
		"symbol":                "BTCUSDT",
		"first_order_amount":    100.0,
		"order_count":           7,
		"add_position_spread":   0.03,
		"add_position_callback": 0.003,
		"take_profit_ratio":     0.013,
		"profit_callback":       0.003,
		"loop_type":             "cycle",
	}
	for k, v := range params {
		m[k] = v
	}
	if err := s.Start(m); err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

func buyFill(qty, price float64) model.OrderData {
	return model.OrderData{Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled, Filled: qty, AvgFillPrice: price}
}

// 常规流：首根 K 线开首单，回调确认后加仓信号发出，成交回报后层数推进。
func TestMartinCapsNormalFlowFillConfirmsLayer(t *testing.T) {
	s := startMartin(t, nil)

	sig, err := s.OnBar(bar(100, 100), nil)
	if err != nil || sig == nil || sig.Direction != "LONG" {
		t.Fatalf("first order signal expected, got %v %v", sig, err)
	}
	// 未成交回报前：任何深跌也不发加仓（pending=0、positionCount=0 → addCount=0，
	// 触发条件 target=entry 且 pendingAdd 置位 → 实际上信号会发但层数不推进；
	// 这里验证层数不推进）。
	if _, err := s.OnOrderUpdate(buyFill(1, 100), nil); err != nil {
		t.Fatal(err)
	}
	// 首单成交后第 1 层就绪：-3% 以上回调 → 加仓信号 #1（此时 target=entry*(1-0)=entry）。
	sig, _ = s.OnBar(bar(99, 98.5), nil)
	if sig == nil || !strings.Contains(sig.Reason, "add position #1") {
		t.Fatalf("add #1 signal expected, got %v", sig)
	}
	// 信号已发但未成交：再深的下跌也不发第 2 个加仓信号（在途不叠加）。
	if sig2, _ := s.OnBar(bar(90, 89), nil); sig2 != nil {
		t.Fatalf("no second add while one pending, got %v", sig2)
	}
	// 成交回报 → 层数推进，随后 -3% 处触发 #2。
	if _, err := s.OnOrderUpdate(buyFill(2, 99), nil); err != nil {
		t.Fatal(err)
	}
	if s.positionCount != 2 {
		t.Fatalf("positionCount = %d, want 2 after confirmed fill", s.positionCount)
	}
	// 第 2 层触发位 = 100*(1-0.03) = 97。
	if sig3, _ := s.OnBar(bar(96.5, 96), nil); sig3 == nil || !strings.Contains(sig3.Reason, "add position #2") {
		t.Fatalf("add #2 expected after fill-confirmed advance, got %v", sig3)
	}
}

// 到层数停：max_layers=2（首单+1 加仓）后停止加仓，深跌也不再加。
func TestMartinCapsMaxLayersStopsAdding(t *testing.T) {
	s := startMartin(t, map[string]any{"max_layers": 2})

	if _, err := s.OnBar(bar(100, 100), nil); err != nil {
		t.Fatal(err)
	}
	_, _ = s.OnOrderUpdate(buyFill(1, 100), nil)
	sig, _ := s.OnBar(bar(99, 98.5), nil)
	if sig == nil || !strings.Contains(sig.Reason, "add position #1") {
		t.Fatalf("add #1 expected within max_layers, got %v", sig)
	}
	_, _ = s.OnOrderUpdate(buyFill(2, 99), nil)

	// 已达 max_layers（首单+1 加仓 = 2 单）：-20% 深跌也不再加。
	if sig2, _ := s.OnBar(bar(80, 79), nil); sig2 != nil {
		t.Fatalf("max_layers reached must stop adding, got %v", sig2)
	}
	if s.addCount() != 1 {
		t.Fatalf("addCount = %d, want 1", s.addCount())
	}
	// 止盈监控仍生效：+1.3% 以上且从高点回撤 0.3% → CLOSE。
	// （持仓均价 99.33：1@100 + 2@99）
	if _, err := s.OnBar(bar(101.8, 101.0), nil); err != nil {
		t.Fatal(err)
	}
	if sig3, _ := s.OnBar(bar(101.4, 100.5), nil); sig3 == nil || sig3.Direction != "CLOSE" {
		t.Fatalf("TP monitoring should still work after cap, got %v", sig3)
	}
}

// 到预算拒：max_total_budget=150，首单 100 成交后，下一层计划 200 → 拒绝。
func TestMartinCapsBudgetRejectsAdd(t *testing.T) {
	s := startMartin(t, map[string]any{"max_total_budget": 150.0})

	if _, err := s.OnBar(bar(100, 100), nil); err != nil {
		t.Fatal(err)
	}
	_, _ = s.OnOrderUpdate(buyFill(1, 100), nil)
	if s.totalInvested != 100 {
		t.Fatalf("totalInvested = %v, want 100", s.totalInvested)
	}
	// 计划加仓 200（firstOrderAmount×2^1）> 剩余预算 50 → 拒绝，无信号。
	if sig, _ := s.OnBar(bar(99, 98.5), nil); sig != nil {
		t.Fatalf("budget exceeded must reject add, got %v", sig)
	}
	// 放宽预算 → 允许加仓（证明是预算闸而非层数闸拦截）。
	s.MaxTotalBudget = 400
	if sig, _ := s.OnBar(bar(99, 98.5), nil); sig == nil || !strings.Contains(sig.Reason, "add position #1") {
		t.Fatalf("add #1 expected after budget relaxed, got %v", sig)
	}
}

// 未成交不推进（第二路径）：加仓信号发出后订单被拒（非成交终态）→ 层数
// 不推进、pending 释放，可再次触发同层加仓。
func TestMartinCapsRejectedOrderDoesNotAdvance(t *testing.T) {
	s := startMartin(t, nil)

	_, _ = s.OnBar(bar(100, 100), nil)
	_, _ = s.OnOrderUpdate(buyFill(1, 100), nil)
	sig, _ := s.OnBar(bar(99, 98.5), nil)
	if sig == nil {
		t.Fatal("add #1 expected")
	}
	// 订单被拒（CANCELLED）：pending 释放、层数不推进。
	rejected := model.OrderData{Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusCancelled, Filled: 0, AvgFillPrice: 0}
	if _, err := s.OnOrderUpdate(rejected, nil); err != nil {
		t.Fatal(err)
	}
	if s.positionCount != 1 {
		t.Fatalf("positionCount = %d, want 1 (rejected add must not advance)", s.positionCount)
	}
	if s.pendingAddCount != 0 {
		t.Fatalf("pendingAddCount = %d, want 0 after rejection", s.pendingAddCount)
	}
	// 同层可再次触发。
	if sig2, _ := s.OnBar(bar(98.5, 98), nil); sig2 == nil || !strings.Contains(sig2.Reason, "add position #1") {
		t.Fatalf("same-layer retrigger expected after rejection, got %v", sig2)
	}
}

// max_layers 参数经参数注册表/ApplyParams 生效（配置层 → 运行时链路）。
func TestMartinCapsParamsRoundTrip(t *testing.T) {
	s := startMartin(t, map[string]any{"max_layers": 3, "max_total_budget": 500.0})
	if s.MaxLayers != 3 || s.MaxTotalBudget != 500 {
		t.Fatalf("params not applied: layers=%d budget=%v", s.MaxLayers, s.MaxTotalBudget)
	}
	p := s.Params()
	if p["max_layers"] != 3 || p["max_total_budget"] != 500.0 {
		t.Fatalf("Params() round-trip mismatch: %v", p)
	}
	// 未设置时默认为 0（不收紧）。
	s2 := startMartin(t, nil)
	if s2.MaxLayers != 0 || s2.MaxTotalBudget != 0 {
		t.Fatalf("defaults should be 0 (unset): %v %v", s2.MaxLayers, s2.MaxTotalBudget)
	}
	// 默认不加限：order_count=7 → 仍可发出多个加仓信号。
	_, _ = s2.OnBar(bar(100, 100), nil)
	_, _ = s2.OnOrderUpdate(buyFill(1, 100), nil)
	if sig, _ := s2.OnBar(bar(99, 98.5), nil); sig == nil {
		t.Fatal("default config should allow adds")
	}
}
