package grid

import (
	"encoding/json"
	"testing"
)

// shortConfig 与 testConfig 同参数但为 short 模式。
func shortConfig() Config {
	cfg := testConfig()
	cfg.Mode = ModeShort
	return cfg
}

// TestNewEngineShortInitialState: short 模式启动建仓——建立等规模初始空头
// （baseQty 记负、费后卖出所得记保证金），上方档挂开空 SELL entry、
// 下方档挂平空 BUY exit（量均已知），启动价档不挂。
func TestNewEngineShortInitialState(t *testing.T) {
	e, orders, err := NewEngine(shortConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	baseTotal := testInvest * (1 - testFee) / testStartPrice
	if !approx(e.BaseQty(), -baseTotal) {
		t.Errorf("base_qty = %v, want %v (initial short)", e.BaseQty(), -baseTotal)
	}
	if !approx(e.QuoteBalance(), baseTotal*testStartPrice) {
		t.Errorf("quote_balance = %v, want %v", e.QuoteBalance(), baseTotal*testStartPrice)
	}
	if eq, _, _ := e.Snapshot(testStartPrice, 0); !approx(eq, 0) {
		t.Errorf("equity at start = %v, want 0", eq)
	}
	if len(orders) != 10 {
		t.Fatalf("initial orders = %d, want 10", len(orders))
	}
	basePerSlot := baseTotal / testGrids
	for _, o := range orders {
		wantSide := SideSell
		if o.Level < 5 {
			wantSide = SideBuy
		}
		if o.Side != wantSide {
			t.Errorf("level %d side = %s, want %s", o.Level, o.Side, wantSide)
		}
		if !approx(o.Qty, basePerSlot) {
			t.Errorf("level %d qty = %v, want %v (short-mode orders all carry qty)", o.Level, o.Qty, basePerSlot)
		}
	}
}

// TestShortModePriceDownCoverAndReentry: 价格下行平空 exit 成交——已实现盈亏
// = quotePerGrid - 回补花费 - 手续费，并在上一档挂回配对开空单；价格回升
// 开空 entry 成交只增加空头、不产生已实现盈亏。
func TestShortModePriceDownCoverAndReentry(t *testing.T) {
	e, _, err := NewEngine(shortConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	baseTotal := testInvest * (1 - testFee) / testStartPrice
	basePerSlot := baseTotal / testGrids

	// 价格跌到 104：BUY@104（exit）成交，在 105 挂回 SELL entry。
	fills := e.OnPriceTick(104, 1)
	if len(fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(fills))
	}
	f := fills[0]
	if f.Side != SideBuy || f.Level != 4 {
		t.Errorf("fill = %+v, want BUY@4 cover", f)
	}
	cost := basePerSlot * 104
	wantPnL := testInvest/testGrids - cost - cost*testFee
	if !approx(f.Pnl, wantPnL) {
		t.Errorf("cover pnl = %v, want %v", f.Pnl, wantPnL)
	}
	if !approx(e.RealizedPnL(), wantPnL) {
		t.Errorf("realized = %v, want %v", e.RealizedPnL(), wantPnL)
	}
	if !approx(e.BaseQty(), -baseTotal+basePerSlot) {
		t.Errorf("base_qty = %v, want %v", e.BaseQty(), -baseTotal+basePerSlot)
	}
	orders := orderSides(e)
	if orders[5] != SideSell {
		t.Errorf("level 5 after cover = %s, want SELL re-entry", orders[5])
	}
	if _, ok := orders[4]; ok {
		t.Errorf("level 4 still open after cover")
	}

	// 价格回升到 105：SELL@105（entry）成交，开空所得进保证金、pnl=0，
	// 并在 104 挂回配对平空 BUY。
	fills = e.OnPriceTick(105, 2)
	if len(fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(fills))
	}
	f = fills[0]
	if f.Side != SideSell || f.Level != 5 {
		t.Errorf("fill = %+v, want SELL@5 entry", f)
	}
	if f.Pnl != 0 {
		t.Errorf("entry pnl = %v, want 0", f.Pnl)
	}
	if !approx(e.RealizedPnL(), wantPnL) {
		t.Errorf("realized after entry = %v, want %v", e.RealizedPnL(), wantPnL)
	}
	if !approx(e.BaseQty(), -baseTotal) {
		t.Errorf("base_qty = %v, want %v (back to initial short)", e.BaseQty(), -baseTotal)
	}
	orders = orderSides(e)
	if orders[4] != SideBuy {
		t.Errorf("level 4 after entry = %s, want BUY paired cover", orders[4])
	}
}

// TestNewBotEngineNeutralInitialOrders: neutral 双腿初始挂单——同一组档位价
// 上 long 腿挂做多网格、short 腿挂做空网格，预算均分 Investment/2，互不干扰。
func TestNewBotEngineNeutralInitialOrders(t *testing.T) {
	b, orders, err := NewBotEngine(testConfig(), ModeNeutral)
	if err != nil {
		t.Fatalf("NewBotEngine: %v", err)
	}
	if b.Long() == nil || b.Short() == nil {
		t.Fatal("neutral engine must have both legs")
	}
	if len(orders) != 20 {
		t.Fatalf("initial orders = %d, want 20 (10 per leg)", len(orders))
	}
	legSides := map[string]map[int]Side{}
	for _, lo := range orders {
		if legSides[lo.Leg] == nil {
			legSides[lo.Leg] = map[int]Side{}
		}
		legSides[lo.Leg][lo.Level] = lo.Side
	}
	for _, leg := range []string{LegLong, LegShort} {
		sides, ok := legSides[leg]
		if !ok || len(sides) != 10 {
			t.Fatalf("leg %s orders = %d, want 10", leg, len(sides))
		}
		for lv := 0; lv <= testGrids; lv++ {
			want, exists := map[int]Side{0: SideBuy, 1: SideBuy, 2: SideBuy, 3: SideBuy, 4: SideBuy, 6: SideSell, 7: SideSell, 8: SideSell, 9: SideSell, 10: SideSell}[lv]
			if !exists {
				if _, open := sides[lv]; open {
					t.Errorf("leg %s level %d should be empty (start price level)", leg, lv)
				}
				continue
			}
			if sides[lv] != want {
				t.Errorf("leg %s level %d side = %s, want %s", leg, lv, sides[lv], want)
			}
		}
	}
	// 双腿各自均分预算：每腿 Investment/2。
	half := testInvest / 2
	if !approx(b.Long().QuotePerGrid(), half/testGrids) {
		t.Errorf("long quote_per_grid = %v, want %v", b.Long().QuotePerGrid(), half/testGrids)
	}
	if !approx(b.Short().QuotePerGrid(), half/testGrids) {
		t.Errorf("short quote_per_grid = %v, want %v", b.Short().QuotePerGrid(), half/testGrids)
	}
	// 初始净头寸 = long 多头 + short 空头 ≈ 0（费用内）。
	if np := b.NetPosition(); np > 1e-6 {
		t.Errorf("net position at start = %v, want ~0", np)
	}
}

// TestNeutralPriceUpLongLegExitShortLegEntry: 价格上行——long 腿 SELL exit 成交
// 并在下一档挂回 BUY；short 腿 SELL entry 成交（开空）并在下一档挂配对平空
// BUY。双腿各自产生成交（腿标识正确）。
func TestNeutralPriceUpLongLegExitShortLegEntry(t *testing.T) {
	b, _, err := NewBotEngine(testConfig(), ModeNeutral)
	if err != nil {
		t.Fatalf("NewBotEngine: %v", err)
	}
	fills := b.OnPriceTick(106, 1)
	if len(fills) != 2 {
		t.Fatalf("fills = %d, want 2 (one per leg)", len(fills))
	}
	byLeg := map[string]Fill{}
	for _, lf := range fills {
		byLeg[lf.Leg] = lf.Fill
	}
	lf, ok := byLeg[LegLong]
	if !ok || lf.Side != SideSell || lf.Level != 6 {
		t.Errorf("long leg fill = %+v, want SELL@6 exit", byLeg[LegLong])
	}
	if lf.Pnl <= 0 {
		t.Errorf("long leg exit pnl = %v, want > 0", lf.Pnl)
	}
	sf, ok := byLeg[LegShort]
	if !ok || sf.Side != SideSell || sf.Level != 6 {
		t.Errorf("short leg fill = %+v, want SELL@6 entry", byLeg[LegShort])
	}
	if sf.Pnl != 0 {
		t.Errorf("short leg entry pnl = %v, want 0", sf.Pnl)
	}
	// long 腿在 105 挂回 BUY；short 腿在 105 挂配对平空 BUY。
	longOrders := orderSides(b.Long())
	shortOrders := orderSides(b.Short())
	if longOrders[5] != SideBuy {
		t.Errorf("long level 5 = %s, want BUY re-open", longOrders[5])
	}
	if shortOrders[5] != SideBuy {
		t.Errorf("short level 5 = %s, want BUY paired cover", shortOrders[5])
	}
	// 上行后净头寸：long 腿卖出一档（-1）、short 腿开空一档（-1）→ 净空头两档。
	basePerSlot := testInvest / 2 * (1 - testFee) / testStartPrice / testGrids
	if !approx(b.NetPosition(), -2*basePerSlot) {
		t.Errorf("net position = %v, want %v", b.NetPosition(), -2*basePerSlot)
	}
}

// TestNeutralLegsIndependentPnL: 价格先上后下往返一格，双腿独立核算——
// long 腿实现卖出利润，short 腿尚未平仓（entry 无已实现盈亏），
// 双腿 realized 之和等于聚合 RealizedPnL，且互不用对方的账。
func TestNeutralLegsIndependentPnL(t *testing.T) {
	b, _, err := NewBotEngine(testConfig(), ModeNeutral)
	if err != nil {
		t.Fatalf("NewBotEngine: %v", err)
	}
	b.OnPriceTick(106, 1) // 上行：long exit + short entry
	b.OnPriceTick(105, 2) // 回到 105：两腿在 105 档的 BUY（long 重开 / short 平空）成交

	longRealized := b.Long().RealizedPnL()
	shortRealized := b.Short().RealizedPnL()
	if longRealized <= 0 {
		t.Errorf("long leg realized = %v, want > 0", longRealized)
	}
	// short 腿 106 entry → 105 cover：已实现 = quotePerGrid - cost - fee > 0。
	cover := testInvest / 2 / testGrids
	cost := (testInvest / 2 * (1 - testFee) / testStartPrice / testGrids) * 105
	wantShort := cover - cost - cost*testFee
	if !approx(shortRealized, wantShort) {
		t.Errorf("short leg realized = %v, want %v", shortRealized, wantShort)
	}
	if !approx(b.RealizedPnL(), longRealized+shortRealized) {
		t.Errorf("aggregate realized = %v, want %v", b.RealizedPnL(), longRealized+shortRealized)
	}
	// 独立核算：双腿盈亏数值不同源（long 腿卖出 106 / short 腿回补 105）。
	if approx(longRealized, shortRealized) {
		t.Errorf("legs should account independently, got equal pnl %v", longRealized)
	}
	// 净头寸回到 ~0。
	if np := b.NetPosition(); np > 1e-6 || np < -1e-6 {
		t.Errorf("net position after round trip = %v, want ~0", np)
	}
	// 往返后双腿 realized 均保留（不互相冲销）。
	if b.Long().RealizedPnL() <= 0 || b.Short().RealizedPnL() <= 0 {
		t.Errorf("both legs should keep their own realized pnl: long=%v short=%v",
			b.Long().RealizedPnL(), b.Short().RealizedPnL())
	}
}

// TestNeutralNetPosition: 净头寸 = 多头腿 base + 空头腿 base（负）；
// 价格持续上行净头寸转负（两腿同向开/减），持续下行转正。
func TestNeutralNetPosition(t *testing.T) {
	b, _, err := NewBotEngine(testConfig(), ModeNeutral)
	if err != nil {
		t.Fatalf("NewBotEngine: %v", err)
	}
	basePerSlot := testInvest / 2 * (1 - testFee) / testStartPrice / testGrids
	if !approx(b.NetPosition(), 0) {
		t.Fatalf("net position at start = %v, want 0", b.NetPosition())
	}
	// 上行两档（105→107 穿越 106/107）：每档 long 腿卖出一档、short 腿
	// 开空一档，净 -2 份/档 → 净 -4 份。
	b.OnPriceTick(107, 1)
	if !approx(b.NetPosition(), -4*basePerSlot) {
		t.Errorf("net position after up 2 = %v, want %v", b.NetPosition(), -4*basePerSlot)
	}
	// 下行四档回到 103（穿越 106/105/104/103）：short 腿平空 4 档（量固定，
	// +4·basePerSlot）；long 腿 BUY 回补 4 档（量随成交价定 = 花费(1-fee)/p）。
	// 净头寸 = -4·basePerSlot + 4·basePerSlot + Σ 50·0.999/p。
	b.OnPriceTick(103, 2)
	want := 0.0
	for _, p := range []float64{106, 105, 104, 103} {
		want += (testInvest / 2 / testGrids) * (1 - testFee) / p
	}
	if !approx(b.NetPosition(), want) {
		t.Errorf("net position after down to 103 = %v, want %v", b.NetPosition(), want)
	}
}

// TestBotEngineStateRoundTrip: neutral 状态导出→恢复一致（双腿挂单、
// 独立盈亏、净头寸），且聚合列与双腿之和一致。
func TestBotEngineStateRoundTrip(t *testing.T) {
	b, _, err := NewBotEngine(testConfig(), ModeNeutral)
	if err != nil {
		t.Fatalf("NewBotEngine: %v", err)
	}
	b.OnPriceTick(106, 1)
	b.OnPriceTick(103, 2)

	raw, err := json.Marshal(b.ExportState())
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	restored, err := LoadBotState(testConfig(), ModeNeutral, state)
	if err != nil {
		t.Fatalf("LoadBotState: %v", err)
	}
	if !approx(restored.NetPosition(), b.NetPosition()) {
		t.Errorf("restored net = %v, want %v", restored.NetPosition(), b.NetPosition())
	}
	if !approx(restored.RealizedPnL(), b.RealizedPnL()) {
		t.Errorf("restored realized = %v, want %v", restored.RealizedPnL(), b.RealizedPnL())
	}
	if got := len(restored.LegViews(105)); got != 2 {
		t.Errorf("restored legs = %d, want 2", got)
	}
	if restored.TotalTrades() != b.TotalTrades() {
		t.Errorf("restored trades = %d, want %d", restored.TotalTrades(), b.TotalTrades())
	}
}

// TestLoadBotStateLegacyFlatLong: mode 引入前的裸单腿存量状态可恢复为 long。
func TestLoadBotStateLegacyFlatLong(t *testing.T) {
	e, _, err := NewEngine(testConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.OnPriceTick(106, 1)
	state := e.ExportState() // 裸 Engine.ExportState（无 legs 包装）

	b, err := LoadBotState(testConfig(), ModeLong, state)
	if err != nil {
		t.Fatalf("LoadBotState legacy: %v", err)
	}
	if b.Long() == nil || b.Short() != nil {
		t.Fatalf("legacy state must restore as long-only, got %+v", b)
	}
	if !approx(b.RealizedPnL(), e.RealizedPnL()) {
		t.Errorf("realized = %v, want %v", b.RealizedPnL(), e.RealizedPnL())
	}
	if CountOpenOrders(state) != len(e.Orders()) {
		t.Errorf("CountOpenOrders = %d, want %d", CountOpenOrders(state), len(e.Orders()))
	}
}

// TestShortModeEquityCurve: short 腿权益曲线——价格下行总盈亏为正、
// 上行为负；TotalPnL = equity - equity0。
func TestShortModeEquityCurve(t *testing.T) {
	e, _, err := NewEngine(shortConfig())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if pnl := e.TotalPnL(testStartPrice); !approx(pnl, 0) {
		t.Errorf("pnl at start = %v, want 0", pnl)
	}
	if pnl := e.TotalPnL(95); pnl <= 0 {
		t.Errorf("pnl at lower price = %v, want > 0 (short profits)", pnl)
	}
	if pnl := e.TotalPnL(115); pnl >= 0 {
		t.Errorf("pnl at higher price = %v, want < 0 (short loses)", pnl)
	}
}
