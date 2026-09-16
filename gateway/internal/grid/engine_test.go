package grid

import (
	"encoding/json"
	"math"
	"testing"
)

// 公共测试参数：Lower=100, Upper=110, 10 格, 投资额 1000, 手续费 0.001,
// 启动价 105。每格预算 100 quote，启动买入 base = 1000*0.999/105。
const (
	testLower      = 100.0
	testUpper      = 110.0
	testGrids      = 10
	testInvest     = 1000.0
	testFee        = 0.001
	testStartPrice = 105.0
)

func testConfig() Config {
	return Config{
		Symbol:       "BTC/USDT",
		Lower:        testLower,
		Upper:        testUpper,
		GridCount:    testGrids,
		Investment:   testInvest,
		FeeRate:      testFee,
		CurrentPrice: testStartPrice,
	}
}

// basePerSlot 为启动建仓后每格均分的基础币数量。
func basePerSlot() float64 {
	return testInvest * (1 - testFee) / testStartPrice / testGrids
}

func approx(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9
}

func orderSides(e *Engine) map[int]Side {
	m := make(map[int]Side)
	for _, o := range e.Orders() {
		m[o.Level] = o.Side
	}
	return m
}

func TestNewEngineValidation(t *testing.T) {
	bad := []Config{
		{Lower: 110, Upper: 110, GridCount: 10, Investment: 1000, CurrentPrice: 110},               // Lower == Upper
		{Lower: 120, Upper: 110, GridCount: 10, Investment: 1000, CurrentPrice: 115},               // Lower > Upper
		{Lower: 100, Upper: 110, GridCount: 1, Investment: 1000, CurrentPrice: 105},                // GridCount < 2
		{Lower: 100, Upper: 110, GridCount: 10, Investment: 0, CurrentPrice: 105},                  // Investment <= 0
		{Lower: 100, Upper: 110, GridCount: 10, Investment: -1, CurrentPrice: 105},                 // Investment < 0
		{Lower: 100, Upper: 110, GridCount: 10, Investment: 1000, CurrentPrice: 99},                // 启动价低于区间
		{Lower: 100, Upper: 110, GridCount: 10, Investment: 1000, CurrentPrice: 111},               // 启动价高于区间
		{Lower: 100, Upper: 110, GridCount: 10, Investment: 1000, CurrentPrice: 105, FeeRate: 1.5}, // 费率非法
	}
	for i, cfg := range bad {
		if _, _, err := NewEngine(cfg); err == nil {
			t.Errorf("case %d: NewEngine(%+v) expected error, got nil", i, cfg)
		}
	}
}

func TestNewEngineInitialOrders(t *testing.T) {
	e, orders, err := NewEngine(testConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	// 启动价 105 恰为中间档位：100-104 挂 BUY，106-110 挂 SELL，105 不挂。
	if len(orders) != 10 {
		t.Fatalf("initial orders = %d, want 10", len(orders))
	}
	sides := orderSides(e)
	for i := 0; i <= testGrids; i++ {
		want, ok := map[int]Side{0: SideBuy, 1: SideBuy, 2: SideBuy, 3: SideBuy, 4: SideBuy, 6: SideSell, 7: SideSell, 8: SideSell, 9: SideSell, 10: SideSell}[i]
		if !ok {
			if _, exists := sides[i]; exists {
				t.Errorf("level %d: expected no order, got %v", i, sides[i])
			}
			continue
		}
		if sides[i] != want {
			t.Errorf("level %d: side = %v, want %v", i, sides[i], want)
		}
	}
	// SELL 单数量 = 每格均分 base；BUY 单数量占位 0。
	for _, o := range orders {
		if o.Side == SideSell && !approx(o.Qty, basePerSlot()) {
			t.Errorf("SELL level %d qty = %v, want %v", o.Level, o.Qty, basePerSlot())
		}
	}
	// 启动建仓：全部投资换成 base，手续费从 base 中扣除，余额为 0。
	if !approx(e.BaseQty(), testInvest*(1-testFee)/testStartPrice) {
		t.Errorf("baseQty = %v, want %v", e.BaseQty(), testInvest*(1-testFee)/testStartPrice)
	}
	if e.QuoteBalance() != 0 {
		t.Errorf("quoteBalance = %v, want 0", e.QuoteBalance())
	}
	if e.TotalTrades() != 0 || e.RealizedPnL() != 0 {
		t.Errorf("fresh engine trades=%d pnl=%v, want 0/0", e.TotalTrades(), e.RealizedPnL())
	}
}

func TestNewEngineWithExistingHoldings(t *testing.T) {
	cfg := testConfig()
	cfg.BaseQty, cfg.QuoteBalance = 5, 100
	e, orders, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if !approx(e.BaseQty(), 5) || !approx(e.QuoteBalance(), 100) {
		t.Errorf("holdings = %v base / %v quote, want 5/100", e.BaseQty(), e.QuoteBalance())
	}
	for _, o := range orders {
		if o.Side == SideSell && !approx(o.Qty, 0.5) { // 5 / 10 格
			t.Errorf("SELL level %d qty = %v, want 0.5", o.Level, o.Qty)
		}
	}
}

func TestUptrendSellsAll(t *testing.T) {
	e, _, err := NewEngine(testConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	slot := basePerSlot()
	var wantPnl, wantQuote float64
	for price := 106.0; price <= 110.0; price++ {
		fills := e.OnPriceTick(price, int64(price))
		if len(fills) != 1 {
			t.Fatalf("tick %v: fills = %d, want 1", price, len(fills))
		}
		f := fills[0]
		if f.Side != SideSell || f.Level != int(price-100) {
			t.Errorf("tick %v: fill = %+v, want SELL@%d", price, f, int(price-100))
		}
		wantGross := slot * price
		wantFee := wantGross * testFee
		if !approx(f.Qty, slot) || !approx(f.QuoteQty, wantGross) || !approx(f.Fee, wantFee) {
			t.Errorf("tick %v: qty/quote/fee = %v/%v/%v, want %v/%v/%v",
				price, f.Qty, f.QuoteQty, f.Fee, slot, wantGross, wantFee)
		}
		wantPnl += wantGross - wantFee - e.QuotePerGrid()
		wantQuote += wantGross - wantFee
		if !approx(e.RealizedPnL(), wantPnl) {
			t.Errorf("tick %v: realizedPnL = %v, want %v", price, e.RealizedPnL(), wantPnl)
		}
		// 对侧重挂：SELL@i 成交后 BUY@i-1 挂单。
		if got := orderSides(e)[int(price-100)-1]; got != SideBuy {
			t.Errorf("tick %v: level %d side = %v, want BUY", price, int(price-100)-1, got)
		}
	}
	// 5 格 SELL 全部成交：base 减半，SELL 挂单清零。
	if e.TotalTrades() != 5 {
		t.Errorf("totalTrades = %d, want 5", e.TotalTrades())
	}
	if !approx(e.BaseQty(), 5*slot) {
		t.Errorf("baseQty = %v, want %v", e.BaseQty(), 5*slot)
	}
	if !approx(e.QuoteBalance(), wantQuote) {
		t.Errorf("quoteBalance = %v, want %v", e.QuoteBalance(), wantQuote)
	}
	for i, s := range orderSides(e) {
		if s == SideSell {
			t.Errorf("level %d: SELL still open after full sell-out", i)
		}
	}
	// 权益 = 剩余 base 市值 + 净卖出所得（均由本测试独立推导的分量构成）。
	eq, _, open := e.Snapshot(110, 0)
	wantEquity := 5*slot*110 + wantQuote
	if !approx(eq, wantEquity) {
		t.Errorf("equity = %v, want %v", eq, wantEquity)
	}
	if open != len(e.Orders()) {
		t.Errorf("openOrders = %d, want %d", open, len(e.Orders()))
	}
}

func TestDowntrendBuysAll(t *testing.T) {
	e, _, err := NewEngine(testConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	base0 := e.BaseQty()
	var wantBase float64 = base0
	for price := 104.0; price >= 100.0; price-- {
		fills := e.OnPriceTick(price, int64(price))
		if len(fills) != 1 {
			t.Fatalf("tick %v: fills = %d, want 1", price, len(fills))
		}
		f := fills[0]
		if f.Side != SideBuy || f.Level != int(price-100) {
			t.Errorf("tick %v: fill = %+v, want BUY@%d", price, f, int(price-100))
		}
		wantQty := 100 * (1 - testFee) / price
		if !approx(f.Qty, wantQty) || !approx(f.QuoteQty, 100) || !approx(f.Fee, 100*testFee) {
			t.Errorf("tick %v: qty/quote/fee = %v/%v/%v, want %v/100/%v",
				price, f.Qty, f.QuoteQty, f.Fee, wantQty, 100*testFee)
		}
		wantBase += wantQty
		// 对侧重挂：BUY@i 成交后 SELL@i+1 挂单。
		if got := orderSides(e)[int(price-100)+1]; got != SideSell {
			t.Errorf("tick %v: level %d side = %v, want SELL", price, int(price-100)+1, got)
		}
	}
	if e.TotalTrades() != 5 {
		t.Errorf("totalTrades = %d, want 5", e.TotalTrades())
	}
	if !approx(e.BaseQty(), wantBase) {
		t.Errorf("baseQty = %v, want %v", e.BaseQty(), wantBase)
	}
	// 启动时全部投资已换成 base，BUY 腿花费使余额降为 -500（纸面撮合）。
	if !approx(e.QuoteBalance(), -500) {
		t.Errorf("quoteBalance = %v, want -500", e.QuoteBalance())
	}
	if e.RealizedPnL() != 0 {
		t.Errorf("realizedPnL = %v, want 0 (no SELL)", e.RealizedPnL())
	}
	// 全部 BUY 成交后：BUY 挂单清零，SELL 挂满 101-110。
	for i, s := range orderSides(e) {
		if s == SideBuy {
			t.Errorf("level %d: BUY still open after full buy-in", i)
		}
	}
}

func TestOscillationProfits(t *testing.T) {
	e, initial, err := NewEngine(testConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	const rounds = 10
	qtyB := 100 * (1 - testFee) / 104 // BUY@104 买入量
	// 单轮往返盈亏两种等价算法：
	//  1) 净卖出所得 - 槽成本:      qtyB*105*(1-fee) - 100
	//  2) 格差×量 - 买费 - 卖费:    qtyB*(105-104) - 100*fee - qtyB*105*fee
	pnlPerRound := qtyB*105*(1-testFee) - 100
	alt := qtyB*(105-104) - 100*testFee - qtyB*105*testFee
	if math.Abs(pnlPerRound-alt) > 1e-12 {
		t.Fatalf("fee identity broken: %v vs %v", pnlPerRound, alt)
	}
	if pnlPerRound <= 0 {
		t.Fatalf("pnlPerRound = %v, want > 0", pnlPerRound)
	}
	for r := 0; r < rounds; r++ {
		fills := e.OnPriceTick(104, int64(r*2))
		if len(fills) != 1 || fills[0].Side != SideBuy {
			t.Fatalf("round %d down: fills = %+v, want 1 BUY", r, fills)
		}
		if got := orderSides(e)[4]; got == SideBuy {
			t.Errorf("round %d: BUY@104 should be consumed", r)
		}
		if got := orderSides(e)[5]; got != SideSell {
			t.Errorf("round %d: SELL@105 should be placed, got %v", r, got)
		}
		fills = e.OnPriceTick(105, int64(r*2+1))
		if len(fills) != 1 || fills[0].Side != SideSell {
			t.Fatalf("round %d up: fills = %+v, want 1 SELL", r, fills)
		}
		if !approx(fills[0].Pnl, pnlPerRound) {
			t.Errorf("round %d: fill pnl = %v, want %v", r, fills[0].Pnl, pnlPerRound)
		}
	}
	if e.TotalTrades() != 2*rounds {
		t.Errorf("totalTrades = %d, want %d", e.TotalTrades(), 2*rounds)
	}
	// 往返 N 轮后：realizedPnL = N × 单轮盈亏，且挂单簿回到初始形态。
	if !approx(e.RealizedPnL(), rounds*pnlPerRound) {
		t.Errorf("realizedPnL = %v, want %v", e.RealizedPnL(), rounds*pnlPerRound)
	}
	want := make(map[int]Side, len(initial))
	for _, o := range initial {
		want[o.Level] = o.Side
	}
	if len(e.Orders()) != len(initial) {
		t.Fatalf("orders = %d, want %d (back to initial book)", len(e.Orders()), len(initial))
	}
	got := orderSides(e)
	for lv, s := range want {
		if got[lv] != s {
			t.Errorf("level %d side = %v, want %v (initial book)", lv, got[lv], s)
		}
	}
	// 权益同步增长：equity(105) = 初始权益 + realizedPnL。
	eq, realized, _ := e.Snapshot(105, 0)
	if !approx(eq, testInvest*(1-testFee)+realized) {
		t.Errorf("equity = %v, want %v + %v", eq, testInvest*(1-testFee), realized)
	}
}

func TestGapFillMultipleLevels(t *testing.T) {
	// 跳空上涨：一次 tick 穿越 106/107/108 三档，全部成交。
	e, _, err := NewEngine(testConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	slot := basePerSlot()
	fills := e.OnPriceTick(108.5, 1)
	if len(fills) != 3 {
		t.Fatalf("fills = %d, want 3: %+v", len(fills), fills)
	}
	for j, wantLevel := range []int{6, 7, 8} {
		if fills[j].Level != wantLevel || fills[j].Side != SideSell {
			t.Errorf("fill %d = %+v, want SELL@%d", j, fills[j], wantLevel)
		}
	}
	if e.TotalTrades() != 3 {
		t.Errorf("totalTrades = %d, want 3", e.TotalTrades())
	}
	var wantQuote float64
	for _, p := range []float64{106, 107, 108} {
		wantQuote += slot * p * (1 - testFee)
	}
	if !approx(e.BaseQty(), testInvest*(1-testFee)/testStartPrice-3*slot) {
		t.Errorf("baseQty = %v", e.BaseQty())
	}
	if !approx(e.QuoteBalance(), wantQuote) {
		t.Errorf("quoteBalance = %v, want %v", e.QuoteBalance(), wantQuote)
	}
	// 对侧 BUY 重挂 105/106/107。
	sides := orderSides(e)
	for _, lv := range []int{5, 6, 7} {
		if sides[lv] != SideBuy {
			t.Errorf("level %d side = %v, want BUY", lv, sides[lv])
		}
	}
	if sides[8] != "" {
		t.Errorf("level 8 should be empty after SELL fill, got %v", sides[8])
	}

	// 跳空下跌：一次 tick 穿越 104/103/102 三档。
	e2, _, err := NewEngine(testConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	fills = e2.OnPriceTick(101.5, 1)
	if len(fills) != 3 {
		t.Fatalf("gap-down fills = %d, want 3: %+v", len(fills), fills)
	}
	for j, wantLevel := range []int{4, 3, 2} {
		if fills[j].Level != wantLevel || fills[j].Side != SideBuy {
			t.Errorf("fill %d = %+v, want BUY@%d", j, fills[j], wantLevel)
		}
	}
	var wantBase float64 = testInvest * (1 - testFee) / testStartPrice
	for _, p := range []float64{104, 103, 102} {
		wantBase += 100 * (1 - testFee) / p
	}
	if !approx(e2.BaseQty(), wantBase) {
		t.Errorf("baseQty = %v, want %v", e2.BaseQty(), wantBase)
	}
	if !approx(e2.QuoteBalance(), -300) {
		t.Errorf("quoteBalance = %v, want -300", e2.QuoteBalance())
	}
	sides = orderSides(e2)
	for _, lv := range []int{3, 4, 5} {
		if sides[lv] != SideSell {
			t.Errorf("level %d side = %v, want SELL", lv, sides[lv])
		}
	}
	if sides[2] != "" {
		t.Errorf("level 2 should be empty after BUY fill, got %v", sides[2])
	}
}

func TestOutOfRangeNoFill(t *testing.T) {
	e, _, err := NewEngine(testConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	bookBefore := orderSides(e)
	baseBefore, quoteBefore := e.BaseQty(), e.QuoteBalance()

	for _, price := range []float64{99.9, 50, 110.1, 200} {
		if fills := e.OnPriceTick(price, 1); len(fills) != 0 {
			t.Errorf("tick %v: fills = %+v, want none (out of range)", price, fills)
		}
	}
	if e.TotalTrades() != 0 {
		t.Errorf("totalTrades = %d, want 0", e.TotalTrades())
	}
	if e.BaseQty() != baseBefore || e.QuoteBalance() != quoteBefore {
		t.Errorf("balances changed: %v/%v -> %v/%v", baseBefore, quoteBefore, e.BaseQty(), e.QuoteBalance())
	}
	for lv, s := range orderSides(e) {
		if bookBefore[lv] != s {
			t.Errorf("level %d side = %v, want %v", lv, s, bookBefore[lv])
		}
	}
	// 回区间后恢复正常撮合（价位在区间内且穿越单档，应各成交一笔）。
	if fills := e.OnPriceTick(106, 2); len(fills) != 1 || fills[0].Level != 6 {
		t.Errorf("tick 106: fills = %+v, want 1 SELL@6", fills)
	}
	if fills := e.OnPriceTick(104.5, 3); len(fills) != 1 || fills[0].Level != 5 {
		t.Errorf("tick 104.5: fills = %+v, want 1 BUY@5", fills)
	}
}

func TestExportLoadStateRoundTrip(t *testing.T) {
	e, _, err := NewEngine(testConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.OnPriceTick(106, 1)
	e.OnPriceTick(107, 2)
	e.OnPriceTick(103, 3)

	raw, err := json.Marshal(e.ExportState())
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	e2, err := LoadState(testConfig(), state)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if e2.BaseQty() != e.BaseQty() || e2.QuoteBalance() != e.QuoteBalance() ||
		e2.RealizedPnL() != e.RealizedPnL() || e2.TotalTrades() != e.TotalTrades() {
		t.Errorf("restored state mismatch: %v/%v/%v/%d vs %v/%v/%v/%d",
			e2.BaseQty(), e2.QuoteBalance(), e2.RealizedPnL(), e2.TotalTrades(),
			e.BaseQty(), e.QuoteBalance(), e.RealizedPnL(), e.TotalTrades())
	}
	got, want := orderSides(e2), orderSides(e)
	if len(got) != len(want) {
		t.Fatalf("orders = %d, want %d", len(got), len(want))
	}
	for lv, s := range want {
		if got[lv] != s {
			t.Errorf("level %d side = %v, want %v", lv, got[lv], s)
		}
	}
	// 恢复后的引擎继续撮合，与原引擎结果一致。
	f1 := e.OnPriceTick(108, 4)
	f2 := e2.OnPriceTick(108, 4)
	if len(f1) != len(f2) || e2.BaseQty() != e.BaseQty() || e2.QuoteBalance() != e.QuoteBalance() {
		t.Errorf("post-restore divergence: fills %d vs %d", len(f1), len(f2))
	}
}

func TestLoadStateRejectsBadInput(t *testing.T) {
	if _, err := LoadState(testConfig(), map[string]any{"orders": "junk"}); err == nil {
		t.Error("expected error for non-map orders")
	}
	if _, err := LoadState(testConfig(), map[string]any{
		"orders": map[string]any{"99": map[string]any{"side": "SELL", "qty": 1.0}},
	}); err == nil {
		t.Error("expected error for out-of-range level")
	}
	if _, err := LoadState(testConfig(), map[string]any{
		"orders": map[string]any{"3": map[string]any{"side": "HOLD", "qty": 1.0}},
	}); err == nil {
		t.Error("expected error for invalid side")
	}
	if _, err := LoadState(testConfig(), map[string]any{
		"orders": map[string]any{"3": map[string]any{"side": "SELL", "qty": 0.0}},
	}); err == nil {
		t.Error("expected error for SELL with zero qty")
	}
}

func TestFeePrecision(t *testing.T) {
	e, _, err := NewEngine(testConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	// BUY@104：花费 100，费 0.1，得 base = 99.9/104。
	buys := e.OnPriceTick(104, 1)
	if len(buys) != 1 {
		t.Fatalf("fills = %+v, want 1 BUY", buys)
	}
	b := buys[0]
	if math.Abs(b.Fee-100*testFee) > 1e-12 {
		t.Errorf("buy fee = %.17g, want %.17g", b.Fee, 100*testFee)
	}
	qtyB := 100 * (1 - testFee) / 104
	if math.Abs(b.Qty-qtyB) > 1e-12 {
		t.Errorf("buy qty = %.17g, want %.17g", b.Qty, qtyB)
	}
	// SELL@105：gross = qtyB*105，费 = gross*0.001，pnl = gross - fee - 100。
	sells := e.OnPriceTick(105, 2)
	if len(sells) != 1 {
		t.Fatalf("fills = %+v, want 1 SELL", sells)
	}
	s := sells[0]
	gross := qtyB * 105
	fee := gross * testFee
	if math.Abs(s.QuoteQty-gross) > 1e-12 || math.Abs(s.Fee-fee) > 1e-12 {
		t.Errorf("sell quote/fee = %.17g/%.17g, want %.17g/%.17g", s.QuoteQty, s.Fee, gross, fee)
	}
	if math.Abs(s.Pnl-(gross-fee-100)) > 1e-12 {
		t.Errorf("sell pnl = %.17g, want %.17g", s.Pnl, gross-fee-100)
	}
	if math.Abs(e.RealizedPnL()-(gross-fee-100)) > 1e-12 {
		t.Errorf("realizedPnL = %.17g, want %.17g", e.RealizedPnL(), gross-fee-100)
	}
	// 余额路径精确：quoteBalance = -100 + gross - fee（启动建仓后余额为 0）。
	if math.Abs(e.QuoteBalance()-(-100+gross-fee)) > 1e-12 {
		t.Errorf("quoteBalance = %.17g, want %.17g", e.QuoteBalance(), -100+gross-fee)
	}
}
