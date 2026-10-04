package cra

import (
	"math"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── helpers ──

// startTailSpot 启动 spot CRA：静态止盈、指定 method、首单 10U、默认补仓梯
// （order2: 5%/0.5%/2x、order3: 7%/0.5%/4x）、关回调。
func startTPSpot(t *testing.T, method string) *BaseCRAStrategy {
	t.Helper()
	s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":              "BTCUSDT",
		"first_order_amount":  10,
		"tp_mode":             "static",
		"take_profit_method":  method,
		"take_profit_ratio":   0.02,
		"profit_callback":     0,
		"enable_add_position": true,
		"order_count":         7,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

func fillBuy(t *testing.T, s *BaseCRAStrategy, price, qty float64) {
	t.Helper()
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: qty, AvgFillPrice: price,
	}, nil); err != nil {
		t.Fatalf("fill buy: %v", err)
	}
}

// fillClose 平仓成交回执。closeFlag=false 模拟现货账本兜底路径的裸 SELL 单
// （无 ClosePosition 标记，2026-10-01 出场链路形态）；true 模拟合约镜像平仓单。
func fillClose(t *testing.T, s *BaseCRAStrategy, price, qty float64, closeFlag bool) {
	t.Helper()
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusFilled,
		Filled: qty, Quantity: qty, AvgFillPrice: price, ClosePosition: closeFlag,
	}, nil); err != nil {
		t.Fatalf("fill close: %v", err)
	}
}

func bar(c float64) model.Bar {
	return model.Bar{Symbol: "BTCUSDT", Close: c, High: c, Low: c}
}

func almostEq(a, b float64) bool { return math.Abs(a-b) <= 1e-6*math.Max(math.Max(a, b), 1e-9) }

// ── 档级判定（state 层）──

// 尾单止盈：尾档自身达线即触发（全仓均价未达线——与 full 的语义分野），
// 平仓量=尾档量；回调序列：达线→记峰→回调出场。
func TestCheckTailTakeProfitTailLotOnly(t *testing.T) {
	st := &CRAState{}
	st.EnterPosition(100, SideLong)
	st.RecordFill(100, 1, SideBuy)
	st.RecordFill(90, 1, SideBuy)
	st.RecordFill(80, 1, SideBuy) // 尾档成本 80，均价 90

	// 尾档盈利 1.25% < 2%：不触发。
	if ok, _ := st.CheckTailTakeProfit(81, 0.02, 0.005); ok {
		t.Fatal("tail profit 1.25% must not trigger 2% ratio")
	}
	// 尾档盈利 10%（均价亏损中）：记峰不触发（等回调）。
	if ok, _ := st.CheckTailTakeProfit(88, 0.02, 0.005); ok {
		t.Fatal("tail at peak must not trigger before callback")
	}
	// 自峰值回调 1% ≥ 0.5%：触发，平仓量=尾档 1。
	// 此刻均价 90 仍亏 3.1%——全仓止盈绝不触发，正是币富"尾单盈利但不足以
	// 让全仓盈利时先减仓"的场景。
	ok, qty := st.CheckTailTakeProfit(87.12, 0.02, 0.005)
	if !ok {
		t.Fatal("tail tp must trigger after drawback from peak")
	}
	if !almostEq(qty, 1) {
		t.Fatalf("tail close qty = %v, want 1 (tail lot only)", qty)
	}
}

// 首尾止盈：首+尾同时达线才触发（较弱端 min 口径），平仓量=首+尾。
func TestCheckHeadTailTakeProfitBothEnds(t *testing.T) {
	st := &CRAState{}
	st.EnterPosition(100, SideLong)
	st.RecordFill(100, 1, SideBuy)
	st.RecordFill(90, 2, SideBuy)
	st.RecordFill(80, 1, SideBuy)

	// 尾档 +2.5% 但首档 -18.3%：min 未达线，不触发。
	if ok, _ := st.CheckHeadTailTakeProfit(82, 0.02, 0); ok {
		t.Fatal("head_tail must not trigger while head lot underwater")
	}
	// 首档 +2.1%、尾档 +27.6%：min 达线，callback=0 即触发，量=1+1=2。
	ok, qty := st.CheckHeadTailTakeProfit(102.1, 0.02, 0)
	if !ok {
		t.Fatal("head_tail must trigger when both ends reach ratio")
	}
	if !almostEq(qty, 2) {
		t.Fatalf("head_tail close qty = %v, want 2 (head+tail)", qty)
	}
}

// 仅剩一档：首=尾，数量不重复计（退化为全仓）。
func TestCheckHeadTailTakeProfitSingleLot(t *testing.T) {
	st := &CRAState{}
	st.EnterPosition(100, SideLong)
	st.RecordFill(100, 1.5, SideBuy)
	ok, qty := st.CheckHeadTailTakeProfit(102.1, 0.02, 0)
	if !ok || !almostEq(qty, 1.5) {
		t.Fatalf("single lot head_tail = (%v, %v), want (true, 1.5)", ok, qty)
	}
}

// 做空镜像：尾档（最高价加仓的空单）盈利判定方向正确。
func TestCheckTailTakeProfitShort(t *testing.T) {
	st := &CRAState{}
	st.EnterPosition(100, SideShort)
	st.RecordFill(100, 1, SideBuy)
	st.RecordFill(110, 1, SideBuy) // 尾档成本 110
	// 价格 107：尾档盈利 2.73% ≥ 2%，均价 105 亏损 1.9% → 尾单触发、全仓不触发。
	ok, qty := st.CheckTailTakeProfit(107, 0.02, 0)
	if !ok || !almostEq(qty, 1) {
		t.Fatalf("short tail = (%v, %v), want (true, 1)", ok, qty)
	}
}

// ── 部分平仓状态演进 ──

func TestApplyCloseFillTail(t *testing.T) {
	st := &CRAState{}
	st.EnterPosition(100, SideLong)
	st.RecordFill(100, 1, SideBuy)
	st.RecordFill(90, 2, SideBuy)
	st.RecordFill(80, 4, SideBuy)
	st.TailPeakProfitPct = 0.09 // 档级峰值应随档集合变更重置

	st.ApplyCloseFill(4, "tail")
	if len(st.Lots) != 2 {
		t.Fatalf("lots = %v, want 2 remaining", st.Lots)
	}
	if !almostEq(st.TotalQty, 3) || !almostEq(st.TotalCost, 100*1+90*2) {
		t.Fatalf("totals = qty %v cost %v, want 3/280", st.TotalQty, st.TotalCost)
	}
	if !almostEq(st.AvgEntryPrice, 280.0/3.0) {
		t.Fatalf("avg = %v, want 93.333", st.AvgEntryPrice)
	}
	if st.PositionCount != 2 {
		t.Fatalf("PositionCount = %d, want 2", st.PositionCount)
	}
	if st.TailPeakProfitPct != 0 {
		t.Fatal("tail peak must reset after lot set change")
	}
	if !st.InPosition {
		t.Fatal("position must survive partial close")
	}
}

func TestApplyCloseFillHeadTail(t *testing.T) {
	st := &CRAState{}
	st.EnterPosition(100, SideLong)
	st.RecordFill(100, 1, SideBuy)
	st.RecordFill(95, 1, SideBuy)
	st.RecordFill(90, 1, SideBuy)
	st.RecordFill(85, 1, SideBuy)

	st.ApplyCloseFill(2, "head_tail")
	if len(st.Lots) != 2 || !almostEq(st.Lots[0].Price, 95) || !almostEq(st.Lots[1].Price, 90) {
		t.Fatalf("lots = %+v, want mid [95,90]", st.Lots)
	}
	if !almostEq(st.AvgEntryPrice, 92.5) || !almostEq(st.TotalQty, 2) {
		t.Fatalf("avg/qty = %v/%v, want 92.5/2", st.AvgEntryPrice, st.TotalQty)
	}
	if st.PositionCount != 2 {
		t.Fatalf("PositionCount = %d, want 2", st.PositionCount)
	}
}

// 未知形态（手工减仓等）兜底 FIFO 从首档消耗，支持档内部分核销。
func TestApplyCloseFillFIFOFallback(t *testing.T) {
	st := &CRAState{}
	st.EnterPosition(100, SideLong)
	st.RecordFill(100, 1, SideBuy)
	st.RecordFill(90, 2, SideBuy)
	st.ApplyCloseFill(1.5, "")
	// 首档 1 全销 + 次档部分核销 0.5 → 剩 [{90, 1.5}]。
	if len(st.Lots) != 1 || !almostEq(st.Lots[0].Price, 90) || !almostEq(st.Lots[0].Qty, 1.5) {
		t.Fatalf("lots = %+v, want [{90,1.5}] (FIFO head consumed, second reduced)", st.Lots)
	}
	if st.PositionCount != 1 {
		t.Fatalf("PositionCount = %d, want 1", st.PositionCount)
	}
	if !almostEq(st.TotalQty, 1.5) || !almostEq(st.AvgEntryPrice, 90) {
		t.Fatalf("totals = %v@%v, want 1.5@90", st.TotalQty, st.AvgEntryPrice)
	}
}

// ── 重启分档重建（重放）──

func TestRebuildLotsFromFills(t *testing.T) {
	// 循环1：3 档买入 → 全平卖出清空；循环2：2 档买入 → 尾单止盈平尾档。
	fills := []FillRecord{
		{Side: "BUY", Qty: 1, Price: 100},
		{Side: "BUY", Qty: 1, Price: 90},
		{Side: "BUY", Qty: 1, Price: 80},
		{Side: "SELL", Qty: 3, Price: 95},  // ≈剩余总量 → 全平
		{Side: "BUY", Qty: 1, Price: 100},  // 新循环首单
		{Side: "BUY", Qty: 2, Price: 90},   // 补仓
		{Side: "BUY", Qty: 4, Price: 80},   // 补仓（尾档）
		{Side: "SELL", Qty: 4, Price: 100}, // ≈尾档量 → 尾单止盈
	}
	lots, side := RebuildLotsFromFills(fills)
	if side != SideLong {
		t.Fatalf("side = %v, want long", side)
	}
	if len(lots) != 2 || !almostEq(lots[0].Price, 100) || !almostEq(lots[0].Qty, 1) ||
		!almostEq(lots[1].Price, 90) || !almostEq(lots[1].Qty, 2) {
		t.Fatalf("lots = %+v, want [{100,1},{90,2}]", lots)
	}

	// 首尾止盈形态：首+尾量 → 剩中间档。
	fills2 := []FillRecord{
		{Side: "BUY", Qty: 1, Price: 100},
		{Side: "BUY", Qty: 2, Price: 90},
		{Side: "BUY", Qty: 4, Price: 80},
		{Side: "SELL", Qty: 5, Price: 100},
	}
	lots2, _ := RebuildLotsFromFills(fills2)
	if len(lots2) != 1 || !almostEq(lots2[0].Price, 90) || !almostEq(lots2[0].Qty, 2) {
		t.Fatalf("lots = %+v, want [{90,2}]", lots2)
	}

	// 做空循环：SELL 开空/加空、BUY 平尾档。
	fills3 := []FillRecord{
		{Side: "SELL", Qty: 1, Price: 100},
		{Side: "SELL", Qty: 1, Price: 110},
		{Side: "BUY", Qty: 1, Price: 105}, // ≈尾档（@110 的 1）→ 尾单止盈
	}
	lots3, side3 := RebuildLotsFromFills(fills3)
	if side3 != SideShort {
		t.Fatalf("side = %v, want short", side3)
	}
	if len(lots3) != 1 || !almostEq(lots3[0].Price, 100) {
		t.Fatalf("lots = %+v, want [{100,1}]", lots3)
	}

	// 未知减仓量 → FIFO 兜底。
	fills4 := []FillRecord{
		{Side: "BUY", Qty: 1, Price: 100},
		{Side: "BUY", Qty: 1, Price: 90},
		{Side: "SELL", Qty: 1.5, Price: 95},
	}
	lots4, _ := RebuildLotsFromFills(fills4)
	if len(lots4) != 1 || !almostEq(lots4[0].Qty, 0.5) || !almostEq(lots4[0].Price, 90) {
		t.Fatalf("lots = %+v, want [{90,0.5}] (FIFO)", lots4)
	}
}

// ── 策略级链路：三态分支信号 ──

// 尾单止盈全链路：触发时只发尾档量的 CLOSE 信号、仓位续存、均价未达线
// （full 不会触发）；在途期间不发新信号；成交确认后核销尾档、均价重算；
// 剩余档再次达线（仅剩一档）退化为全平。
func TestCRATailTakeProfitFlow(t *testing.T) {
	s := startTPSpot(t, "tail")

	// 首单 @100，10U → 0.1。
	sig, err := s.OnBar(bar(100), nil)
	if err != nil || sig == nil || sig.Direction != "LONG" {
		t.Fatalf("first order: sig=%+v err=%v", sig, err)
	}
	fillBuy(t, s, 100, sig.Qty) // 0.1

	// 补仓 #2（order2: spread 5% / callback 0.5% / mult 2）：94.5 挂起、
	// 94.98 反弹触发（回调触发时价格仍须 ≥5% 低于首单价，见 ShouldAddPosition）。
	if sig, _ := s.OnBar(bar(94.5), nil); sig != nil {
		t.Fatalf("add must arm, not fire: %+v", sig)
	}
	sig, _ = s.OnBar(bar(94.98), nil)
	if sig == nil || sig.Direction != "LONG" {
		t.Fatal("add position #2 must fire after bounce")
	}
	addQty := sig.Qty // 20/94.98 ≈ 0.210571
	fillBuy(t, s, 94.98, addQty)

	// 96.9：尾档盈利 2.02% 达线（均价 96.59 仅 +0.32%，full 不触发）。
	sig, _ = s.OnBar(bar(96.9), nil)
	if sig == nil || sig.Direction != "CLOSE" {
		t.Fatal("tail tp must emit CLOSE")
	}
	if !strings.Contains(sig.Reason, "tail") {
		t.Fatalf("reason = %q, want tail tag", sig.Reason)
	}
	if !almostEq(sig.Qty, addQty) {
		t.Fatalf("close qty = %v, want tail lot %v", sig.Qty, addQty)
	}
	st := s.state
	if !st.InPosition || st.PendingCloseKind != "tail" {
		t.Fatalf("state must stay in position with pending tail close: %+v", st)
	}

	// 在途期间：同价 K 线不再发信号（防重复出场）。
	if sig, _ := s.OnBar(bar(96.9), nil); sig != nil {
		t.Fatalf("pending close must block new signals: %+v", sig)
	}

	// 成交确认（现货形态：裸 SELL 无 ClosePosition 标记）：核销尾档，剩余
	// 首档继续，均价重算回首单成本 100。
	fillClose(t, s, 96.9, addQty, false)
	if !st.InPosition {
		t.Fatal("position must survive tail close fill")
	}
	if len(st.Lots) != 1 || !almostEq(st.Lots[0].Price, 100) || !almostEq(st.Lots[0].Qty, 0.1) {
		t.Fatalf("lots = %+v, want [{100,0.1}]", st.Lots)
	}
	if !almostEq(st.TotalQty, 0.1) || !almostEq(st.AvgEntryPrice, 100) {
		t.Fatalf("totals = %v@%v, want 0.1@100", st.TotalQty, st.AvgEntryPrice)
	}
	if st.PositionCount != 1 || st.PendingCloseKind != "" {
		t.Fatalf("count/pending = %d/%q, want 1/\"\"", st.PositionCount, st.PendingCloseKind)
	}

	// 仅剩一档再达线 → 退化全平（不带数量的 CLOSE，历史出场形态）。
	sig, _ = s.OnBar(bar(102.1), nil)
	if sig == nil || sig.Direction != "CLOSE" || sig.Qty != 0 {
		t.Fatalf("final close = %+v, want full CLOSE qty 0", sig)
	}
	if st.InPosition || st.LoopExecuted != 1 {
		t.Fatalf("loop must complete: inPos=%v loops=%d", st.InPosition, st.LoopExecuted)
	}
}

// 首尾止盈全链路：首档未达线时尾档再涨也不触发；两端同达线平首+尾两档，
// 中间档续存。
func TestCRAHeadTailTakeProfitFlow(t *testing.T) {
	s := startTPSpot(t, "head_tail")

	sig, _ := s.OnBar(bar(100), nil)
	if sig == nil {
		t.Fatal("first order expected")
	}
	fillBuy(t, s, 100, sig.Qty) // 0.1 @100

	// 补仓 #2 @94.98（20U）、补仓 #3 @92.97（40U，order3: 7%/0.5%/4x）。
	s.OnBar(bar(94.5), nil)
	sig, _ = s.OnBar(bar(94.98), nil)
	if sig == nil {
		t.Fatal("add #2 expected")
	}
	fillBuy(t, s, 94.98, sig.Qty)
	s.OnBar(bar(92.5), nil)
	sig, _ = s.OnBar(bar(92.97), nil)
	if sig == nil {
		t.Fatal("add #3 expected")
	}
	add3Qty := sig.Qty // ≈0.430247
	fillBuy(t, s, 92.97, add3Qty)

	// 101.5：首档 +1.5% 未达线（尾档 +9.2% 已达线）→ 不触发。
	if sig, _ := s.OnBar(bar(101.5), nil); sig != nil {
		t.Fatalf("head_tail must wait for head lot: %+v", sig)
	}
	// 102.1：首档 +2.1%、尾档 +9.8% 同达线 → 平首+尾（0.1+0.430247）。
	sig, _ = s.OnBar(bar(102.1), nil)
	if sig == nil || sig.Direction != "CLOSE" {
		t.Fatal("head_tail tp must emit CLOSE")
	}
	if !strings.Contains(sig.Reason, "head_tail") {
		t.Fatalf("reason = %q, want head_tail tag", sig.Reason)
	}
	wantQty := RoundQty(0.1 + add3Qty)
	if !almostEq(sig.Qty, wantQty) {
		t.Fatalf("close qty = %v, want %v (head+tail)", sig.Qty, wantQty)
	}

	// 成交确认（合约形态：镜像平仓单带 ClosePosition 标记）。
	fillClose(t, s, 102.1, sig.Qty, true)
	st := s.state
	if !st.InPosition || len(st.Lots) != 1 || !almostEq(st.Lots[0].Price, 94.98) {
		t.Fatalf("lots = %+v, want mid lot @94.98 surviving", st.Lots)
	}
	if !almostEq(st.AvgEntryPrice, 94.98) || st.PositionCount != 1 {
		t.Fatalf("avg/count = %v/%d, want 94.98/1", st.AvgEntryPrice, st.PositionCount)
	}
}

// 移动止盈开启后分仓止盈/首尾止盈失效（币富名词解释 #29）：method=tail 也按
// 全仓移动止盈执行——CLOSE 不带数量（全平），不经在途部分平仓。
func TestCRAMovingModeOverridesMethod(t *testing.T) {
	s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":             "BTCUSDT",
		"first_order_amount": 10,
		"tp_mode":            "moving",
		"take_profit_method": "tail",
		"moving_take_profit_tiers": []map[string]any{
			{"ratio": 0.02, "drawback": 0.005},
			{"ratio": 0.03, "drawback": 0.005},
			{"ratio": 0.04, "drawback": 0.005},
			{"ratio": 0.05, "drawback": 0.005},
		},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	sig, _ := s.OnBar(bar(100), nil)
	if sig == nil {
		t.Fatal("first order expected")
	}
	fillBuy(t, s, 100, sig.Qty)

	// 峰值 +2.5%（达档 1），回调 0.6% ≥ 0.5% → 全仓移动止盈全平。
	s.OnBar(bar(102.5), nil)
	sig, _ = s.OnBar(bar(101.9), nil)
	if sig == nil || sig.Direction != "CLOSE" || sig.Qty != 0 {
		t.Fatalf("moving tp must full-close despite method=tail: %+v", sig)
	}
	if s.state.InPosition || s.state.PendingCloseKind != "" {
		t.Fatal("moving tp must close full position, no partial pending")
	}
}

// full 回归：静态全仓止盈语义不变（均价达线 → 不带量 CLOSE 全平）。
func TestCRAFullTakeProfitRegression(t *testing.T) {
	s := startTPSpot(t, "full")
	sig, _ := s.OnBar(bar(100), nil)
	if sig == nil {
		t.Fatal("first order expected")
	}
	fillBuy(t, s, 100, sig.Qty)
	// 均价 +2.1% 达线（callback=0）→ 全平。
	sig, _ = s.OnBar(bar(102.1), nil)
	if sig == nil || sig.Direction != "CLOSE" || sig.Qty != 0 {
		t.Fatalf("full tp = %+v, want CLOSE qty 0", sig)
	}
	if s.state.InPosition || s.state.LoopExecuted != 1 {
		t.Fatal("full tp must close position and count the loop")
	}
}

// 在途部分平仓被拒：清除在途标记，下一根 K 线重新触发（状态未动无需回补）。
func TestCRAPartialCloseRejectRearms(t *testing.T) {
	s := startTPSpot(t, "tail")
	sig, _ := s.OnBar(bar(100), nil)
	if sig == nil {
		t.Fatal("first order expected")
	}
	fillBuy(t, s, 100, sig.Qty)
	s.OnBar(bar(94.5), nil)
	sig, _ = s.OnBar(bar(94.98), nil)
	if sig == nil {
		t.Fatal("add #2 expected")
	}
	fillBuy(t, s, 94.98, sig.Qty)

	sig, _ = s.OnBar(bar(96.9), nil)
	if sig == nil || s.state.PendingCloseKind != "tail" {
		t.Fatal("tail tp must be pending")
	}
	// 平仓单被拒（现货形态：裸 SELL 无 ClosePosition 标记）→ 在途清除；
	// 档位原样（核销只在成交确认）。
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideSell, Status: model.StatusRejected,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if s.state.PendingCloseKind != "" || len(s.state.Lots) != 2 {
		t.Fatalf("reject must re-arm without touching lots: %+v", s.state)
	}
	// 下一根同价 K 线重新发出尾单止盈。
	sig, _ = s.OnBar(bar(96.9), nil)
	if sig == nil || sig.Direction != "CLOSE" || !strings.Contains(sig.Reason, "tail") {
		t.Fatalf("re-armed tail tp = %+v, want tail CLOSE", sig)
	}
}

// ── 重启重建：各档成本仍在 ──

// Start 注入逐笔成交明细（restored_fills）重放：各档成本精确重建（含运行期
// 尾单止盈已平掉的档），档位/均价/方向续存。
func TestCRARestoreRebuildsPerLotCosts(t *testing.T) {
	s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	err := s.Start(map[string]any{
		"symbol":             "BTCUSDT",
		"tp_mode":            "static",
		"take_profit_method": "tail",
		// 运行轨迹：3 档买入后尾单止盈平掉尾档（4@80），剩 1@100 + 2@90。
		"restored_position_qty":  3.0,
		"restored_position_vwap": (100.0 + 180.0) / 3.0,
		"restored_fills": []any{
			map[string]any{"side": "BUY", "qty": 1.0, "price": 100.0},
			map[string]any{"side": "BUY", "qty": 2.0, "price": 90.0},
			map[string]any{"side": "BUY", "qty": 4.0, "price": 80.0},
			map[string]any{"side": "SELL", "qty": 4.0, "price": 96.0},
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	st := s.state
	if !st.InPosition || st.Side != SideLong {
		t.Fatalf("restored state = %+v", st)
	}
	if len(st.Lots) != 2 || !almostEq(st.Lots[0].Price, 100) || !almostEq(st.Lots[0].Qty, 1) ||
		!almostEq(st.Lots[1].Price, 90) || !almostEq(st.Lots[1].Qty, 2) {
		t.Fatalf("lots = %+v, want [{100,1},{90,2}]", st.Lots)
	}
	if !almostEq(st.AvgEntryPrice, 280.0/3.0) || !almostEq(st.TotalQty, 3) {
		t.Fatalf("avg/qty = %v/%v, want 93.333/3", st.AvgEntryPrice, st.TotalQty)
	}
	if st.EntryPrice != 100 || st.PositionCount != 2 {
		t.Fatalf("entry/count = %v/%d, want 100/2", st.EntryPrice, st.PositionCount)
	}
	// 重建后尾档（@90）达线可触发尾单止盈——档级成本确实存活。
	ok, qty := st.CheckTailTakeProfit(91.85, 0.02, 0)
	if !ok || !almostEq(qty, 2) {
		t.Fatalf("restored tail tp = (%v, %v), want (true, 2)", ok, qty)
	}
}

// 无逐笔明细（旧账本）：聚合净持仓合成单档兜底。
func TestCRARestoreAggregateFallback(t *testing.T) {
	s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":                 "BTCUSDT",
		"tp_mode":                "static",
		"restored_position_qty":  2.5,
		"restored_position_vwap": 96.0,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	st := s.state
	if !st.InPosition || len(st.Lots) != 1 || !almostEq(st.Lots[0].Price, 96) || !almostEq(st.Lots[0].Qty, 2.5) {
		t.Fatalf("aggregate restore = %+v, want single lot {96,2.5}", st.Lots)
	}

	// 逐笔明细与聚合净量对不上（账本残缺）→ 同样退化聚合单档。
	s2 := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	if err := s2.Start(map[string]any{
		"symbol":                 "BTCUSDT",
		"tp_mode":                "static",
		"restored_position_qty":  5.0,
		"restored_position_vwap": 96.0,
		"restored_fills": []any{
			map[string]any{"side": "BUY", "qty": 1.0, "price": 100.0},
		},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(s2.state.Lots) != 1 || !almostEq(s2.state.Lots[0].Qty, 5) {
		t.Fatalf("mismatch must fall back to aggregate: %+v", s2.state.Lots)
	}
}

// RestorePosition 直调路径（PositionRestorer 接口）：合成单档。
func TestCRARestorePositionInterface(t *testing.T) {
	s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
	if err := s.Start(map[string]any{"symbol": "BTCUSDT", "tp_mode": "static"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.RestorePosition(1.5, 99.5); err != nil {
		t.Fatal(err)
	}
	st := s.state
	if !st.InPosition || len(st.Lots) != 1 || !almostEq(st.Lots[0].Price, 99.5) {
		t.Fatalf("RestorePosition = %+v", st.Lots)
	}
}
