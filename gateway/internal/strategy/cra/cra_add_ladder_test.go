package cra

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── H3（2026-10-10）：补仓档位 off-by-one 修复口径锁定 ──
//
// 统一口径：order=N 的 add_positions 配置对应第 N 次补仓；order_count =
// 补仓次数（前端"补仓次数"标签同源），单循环总单数上限 = order_count+1。
// 修复前引擎 nextOrder=PositionCount+PendingAddCount+1：order=1 档永不消费
// （首次补仓直接吃 order=2），且补仓次数比配置少一次。

// ladderContract 启动合约 CRA：首单 10U×5 倍，静态止盈 50%（隔离止盈干扰），
// 补仓阶梯/次数由调用方给全。
func ladderContract(t *testing.T, orderCount int, ladder []any) *BaseCRAStrategy {
	t.Helper()
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":              "BTCUSDT",
		"market_type":         "swap",
		"leverage":            5,
		"direction":           "long",
		"first_order_amount":  10,
		"tp_mode":             "static",
		"take_profit_ratio":   0.5,
		"profit_callback":     0,
		"enable_add_position": true,
		"order_count":         orderCount,
		"add_positions":       ladder,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

// ladderEnter 首单信号 + 成交回执（@price）。
func ladderEnter(t *testing.T, s *BaseCRAStrategy, price float64) {
	t.Helper()
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: price, High: price, Low: price}, nil)
	if err != nil || sig == nil {
		t.Fatalf("first order: sig=%v err=%v", sig, err)
	}
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: sig.Qty, AvgFillPrice: price,
	}, nil); err != nil {
		t.Fatalf("first fill: %v", err)
	}
}

// ladderDriveAdd 驱动一次补仓成交：arm 价挂起（差价达标不触发）、bounce 价
// 反弹触发，按信号量成交回执；返回触发信号。
func ladderDriveAdd(t *testing.T, s *BaseCRAStrategy, armPrice, bouncePrice float64) *model.Signal {
	t.Helper()
	if sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: armPrice, High: armPrice, Low: armPrice}, nil); sig != nil {
		t.Fatalf("arm bar @%v: unexpected signal %+v", armPrice, sig)
	}
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: bouncePrice, High: bouncePrice, Low: armPrice}, nil)
	if err != nil || sig == nil {
		t.Fatalf("add trigger @%v: sig=%v err=%v", bouncePrice, sig, err)
	}
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: sig.Qty, AvgFillPrice: bouncePrice,
	}, nil); err != nil {
		t.Fatalf("add fill: %v", err)
	}
	return sig
}

// TestCRAAddLadderConsumesOrderOneFirst off-by-one 复现：order_count=1、只配
// order=1 档（mult 3）。修复前该档永不下单（首单成交后 gate 1<1 直接锁死，
// 一笔补仓都没有）；修复后首次补仓消费 order=1。
func TestCRAAddLadderConsumesOrderOneFirst(t *testing.T) {
	s := ladderContract(t, 1, []any{
		map[string]any{"order": 1, "multiplier": 3, "spread": 0.03, "callback": 0.003},
	})
	ladderEnter(t, s, 100)

	// 跌 3.5%（≥3%）挂起；反弹 0.31%（≥0.3%）触发。
	sig := ladderDriveAdd(t, s, 96.5, 96.8)
	if sig.Direction != "LONG" || !strings.Contains(sig.Reason, "cra add position #1") {
		t.Fatalf("first add must consume order=1 tier: %+v", sig)
	}
	// qty = 首单金额 10 × order1 倍数 3 × 杠杆 5 / 96.8（order=1 档确实被消费）。
	if want := RoundQty(10 * 3 * 5 / 96.8); sig.Qty != want {
		t.Fatalf("add qty = %v, want %v (order=1 multiplier)", sig.Qty, want)
	}
}

// TestCRAAddLadderCapEqualsAddCount 上限语义：order_count=补仓次数。order_count=2
// 时恰好补 2 次（消费 order 1、2），第 3 次永不触发；总单数=order_count+1。
// 修复前上限 PositionCount+Pending<OrderCount 使补仓次数少一次（只能补 1 次）。
func TestCRAAddLadderCapEqualsAddCount(t *testing.T) {
	s := ladderContract(t, 2, []any{
		map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003},
		map[string]any{"order": 2, "multiplier": 2, "spread": 0.06, "callback": 0.003},
	})
	ladderEnter(t, s, 100)

	sig := ladderDriveAdd(t, s, 96.5, 96.8) // 补仓 #1（order=1，差价 3%）
	if !strings.Contains(sig.Reason, "#1") {
		t.Fatalf("add #1 reason = %q", sig.Reason)
	}
	// 补仓 #2（order=2，差价 6%）：93.5 跌 6.5% 挂起，93.83 反弹 0.35% 触发
	// （触发价仍须满足差价 ≥6%：ShouldAddPosition 每根都重检 dropPct）。
	sig = ladderDriveAdd(t, s, 93.5, 93.5*1.0035)
	if !strings.Contains(sig.Reason, "#2") {
		t.Fatalf("add #2 reason = %q", sig.Reason)
	}
	if want := RoundQty(10 * 2 * 5 / (93.5 * 1.0035)); sig.Qty != want {
		t.Fatalf("add #2 qty = %v, want %v (order=2 multiplier)", sig.Qty, want)
	}

	// 补仓次数已用尽（2/2）：再跌 10%+ 也不得发单。
	for _, px := range []float64{90, 88, 85} {
		if sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: px, High: px, Low: px}, nil); sig != nil {
			t.Fatalf("ladder exhausted at order_count=2 adds, got signal @%v: %+v", px, sig)
		}
	}
	st := s.RuntimeStatus()
	if st["filled_orders"] != 3 || st["add_positions_triggered"] != 2 {
		t.Fatalf("filled/triggered = %v/%v, want 3/2（首单+2 次补仓）",
			st["filled_orders"], st["add_positions_triggered"])
	}
}

// TestCRAAddLadderRestoreContinuesTier 重启重建口径一致：restored_fills 重放
// 2 个自动档（首单+补仓 #1）→ PositionCount=2，重启后下一档消费 order=2
// （不重吃 order=1、不跳档），补满 order_count=2 后锁死。
func TestCRAAddLadderRestoreContinuesTier(t *testing.T) {
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":              "BTCUSDT",
		"market_type":         "swap",
		"leverage":            5,
		"direction":           "long",
		"first_order_amount":  10,
		"tp_mode":             "static",
		"take_profit_ratio":   0.5,
		"profit_callback":     0,
		"enable_add_position": true,
		"order_count":         2,
		"add_positions": []any{
			map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003},
			map[string]any{"order": 2, "multiplier": 2, "spread": 0.06, "callback": 0.003},
		},
		// 运行轨迹重放：首单 0.5@100 + 补仓 #1 0.51@96（成交时间升序）。
		"restored_position_qty":  1.01,
		"restored_position_vwap": (100.0*0.5 + 96.0*0.51) / 1.01,
		"restored_fills": []any{
			map[string]any{"side": "BUY", "qty": 0.5, "price": 100.0},
			map[string]any{"side": "BUY", "qty": 0.51, "price": 96.0},
		},
	}); err != nil {
		t.Fatalf("start with restore: %v", err)
	}
	if st := s.RuntimeStatus(); st["filled_orders"] != 2 || st["add_positions_triggered"] != 1 {
		t.Fatalf("restored ladder state = %v, want filled 2 / triggered 1", st)
	}

	// 下一档必须消费 order=2（差价 6% 自 EntryPrice=100）：93.5 挂起、反弹触发。
	sig := ladderDriveAdd(t, s, 93.5, 93.5*1.0035)
	if !strings.Contains(sig.Reason, "#2") {
		t.Fatalf("restored next add must be order=2, reason = %q", sig.Reason)
	}
	if want := RoundQty(10 * 2 * 5 / (93.5 * 1.0035)); sig.Qty != want {
		t.Fatalf("restored add qty = %v, want %v (order=2 multiplier)", sig.Qty, want)
	}
	// 补满：再跌不再发单。
	if sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 90, High: 90, Low: 90}, nil); sig != nil {
		t.Fatalf("ladder must be exhausted after restore+1 add, got %+v", sig)
	}
}

// TestCRAGridLadderDeepestTierIsUserConfig 现货网格 5 格场景：修复后的前端
// 生成 order 1..5 全量档位（order_count=5=格数=补仓次数），最深档是用户网格
// 配置（等差 1×），不再是 fillMissingAddPositions 补出的马丁默认档
// （修复前只生成 4 行，第 5 档被补成 16×/11% 差价巨单）。
func TestCRAGridLadderDeepestTierIsUserConfig(t *testing.T) {
	// 模拟修复后前端 buildSpotGridLadder(40000,50000,5) 的提交形态
	// （stepPct=4%，UI 百分数 → 后端小数）。
	ladder := make([]any, 0, 5)
	for i := 1; i <= 5; i++ {
		ladder = append(ladder, map[string]any{
			"order": i, "multiplier": 1, "spread": 0.04 * float64(i), "callback": 0.003,
		})
	}
	params := map[string]any{
		"symbol":              "BTCUSDT",
		"first_order_amount":  50,
		"tp_mode":             "static",
		"take_profit_ratio":   0.04,
		"profit_callback":     0,
		"enable_add_position": true,
		"order_count":         5,
		"add_positions":       ladder,
	}
	data, _ := json.Marshal(params)
	p, err := ParseCRAParams(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 档档齐全：fillMissingAddPositions 不得追加任何默认档（尤其 16× 马丁档）。
	if len(p.AddPositions) != 5 {
		t.Fatalf("add_positions len = %d, want 5（无默认补齐）", len(p.AddPositions))
	}
	deepest := p.AddPositionForOrder(5)
	if deepest == nil || deepest.Multiplier != 1 || deepest.Spread != 0.20 {
		t.Fatalf("deepest tier = %+v, want user grid config 1×/20%%（非马丁 16×/11%%）", deepest)
	}

	// 端到端：5 档全部按用户配置消费（等倍、等差触发），补满后锁死。
	s := NewCRASpotStrategy("cra_spot", "BTCUSDT")
	if err := s.Start(params); err != nil {
		t.Fatalf("start: %v", err)
	}
	ladderEnter(t, s, 100)
	// 逐档：arm 略深于格线（×0.996），bounce 反弹 0.35%（≥callback 0.3%）
	// 且仍压在格线之下（触发时 dropPct 仍须 ≥spread，ShouldAddPosition 逐根重检）。
	for i := 1; i <= 5; i++ {
		arm := 100 * (1 - 0.04*float64(i)) * 0.996
		bounce := arm * 1.0035
		sig := ladderDriveAdd(t, s, arm, bounce)
		if !strings.Contains(sig.Reason, "cra add position #"+strconv.Itoa(i)) {
			t.Fatalf("grid add #%d reason = %q", i, sig.Reason)
		}
		if want := RoundQty(50 / bounce); sig.Qty != want {
			t.Fatalf("grid add #%d qty = %v, want %v（等倍 1×）", i, sig.Qty, want)
		}
	}
	if st := s.RuntimeStatus(); st["filled_orders"] != 6 || st["add_positions_triggered"] != 5 {
		t.Fatalf("grid ladder filled/triggered = %v/%v, want 6/5（首单+5 格补仓）",
			st["filled_orders"], st["add_positions_triggered"])
	}
	// 5 格补满（总单数=order_count+1=6）：跌破区间下沿也不再发单。
	if sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 78, High: 78, Low: 78}, nil); sig != nil {
		t.Fatalf("grid ladder exhausted at 5 adds, got %+v", sig)
	}
}
