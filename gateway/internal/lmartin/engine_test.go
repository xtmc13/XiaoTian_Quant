package lmartin

import (
	"testing"
)

func testConfig() Config {
	return Config{
		Symbol:        "BTCUSDT",
		DeviationPct:  0.03, // 每深 3% 加一层
		TakeProfitPct: 0.05,
		StopLossPct:   0.10,
		Groups: []GroupConfig{
			{QuoteAmount: 100, Multiplier: 2, MaxLayers: 3, BudgetCap: 0},
		},
	}
}

func fillLayer(t *testing.T, e *Engine, group, layer int, price float64) {
	t.Helper()
	if err := e.MarkPending(group, layer); err != nil {
		t.Fatalf("mark pending layer %d: %v", layer, err)
	}
	quote := e.Group(group).LayerQuoteAmount(layer)
	if err := e.ApplyBuyFill(group, layer, quote/price, price); err != nil {
		t.Fatalf("apply buy fill layer %d: %v", layer, err)
	}
}

// 首层立即建仓：首个 Tick 对所有组发出首层买入。
func TestEngineFirstLayerImmediateAllGroups(t *testing.T) {
	cfg := testConfig()
	cfg.Groups = append(cfg.Groups, GroupConfig{QuoteAmount: 50, Multiplier: 3, MaxLayers: 2, BudgetCap: 0})
	e := NewEngine(cfg, 100)
	a := e.Tick(100)
	if len(a.Buys) != 2 {
		t.Fatalf("two groups should each emit first-layer buy, got %+v", a.Buys)
	}
	for i, b := range a.Buys {
		if b.Group != i || b.Layer != 1 {
			t.Fatalf("buy[%d] = %+v, want group %d layer 1", i, b, i)
		}
	}
	want := []float64{100, 50}
	for i, b := range a.Buys {
		if b.QuoteAmount != want[i] {
			t.Fatalf("buy[%d] quote = %v, want %v", i, b.QuoteAmount, want[i])
		}
	}
}

// 分层触发：价格每较首层参考价深跌 deviation*(层-1) 触发下一层，倍投金额。
func TestEngineLayerTriggerOnDeviation(t *testing.T) {
	e := NewEngine(testConfig(), 100)
	fillLayer(t, e, 0, 1, 100) // 首层 100U @100

	// -2.9%：未触发第 2 层（触发位 97）。
	if a := e.Tick(97.2); len(a.Buys) != 0 {
		t.Fatalf("layer 2 should not trigger above 3%% drop, got %+v", a.Buys)
	}
	// -3%：触发第 2 层，金额 200U（2 倍投）。
	a := e.Tick(97)
	if len(a.Buys) != 1 || a.Buys[0].Layer != 2 || a.Buys[0].QuoteAmount != 200 {
		t.Fatalf("layer 2 should trigger at -3%% with 200U, got %+v", a.Buys)
	}
	fillLayer(t, e, 0, 2, 97)

	// 第 3 层触发位 = 100*(1-0.03*2) = 94。
	if a := e.Tick(94.5); len(a.Buys) != 0 {
		t.Fatalf("layer 3 should not trigger above 94, got %+v", a.Buys)
	}
	a = e.Tick(94)
	if len(a.Buys) != 1 || a.Buys[0].Layer != 3 || a.Buys[0].QuoteAmount != 400 {
		t.Fatalf("layer 3 should trigger at 94 with 400U, got %+v", a.Buys)
	}
	// 组内均价校验：(100 + 200 + 400) / (1 + 200/97 + 400/94)。
	fillLayer(t, e, 0, 3, 94)
	gs := e.Group(0)
	qty := 100.0/100 + 200.0/97 + 400.0/94
	wantAvg := 700 / qty
	if gs.AvgPrice()-wantAvg > 1e-9 || wantAvg-gs.AvgPrice() > 1e-9 {
		t.Fatalf("avg price = %v, want %v", gs.AvgPrice(), wantAvg)
	}
	if gs.Layer() != 3 {
		t.Fatalf("layer = %d, want 3", gs.Layer())
	}
}

// 层数硬上限：达到 max_layers 后停止加仓（即使价格继续深跌）。
func TestEngineMaxLayersHardCap(t *testing.T) {
	cfg := testConfig()
	cfg.Groups[0].MaxLayers = 2
	e := NewEngine(cfg, 100)
	fillLayer(t, e, 0, 1, 100)
	fillLayer(t, e, 0, 2, 97)
	// 深跌远超第 3 层触发位，但层数已封顶。
	if a := e.Tick(80); len(a.Buys) != 0 {
		t.Fatalf("max_layers reached must not add, got %+v", a.Buys)
	}
	if e.Group(0).Layer() != 2 {
		t.Fatalf("layer = %d, want 2", e.Group(0).Layer())
	}
	// 封顶后仍监控止盈/止损（均价 ≈97.98，+5% ≈102.88）。
	if a := e.Tick(103); len(a.Sells) != 1 || a.Sells[0].Reason != "take_profit" {
		t.Fatalf("capped group should still monitor TP, got %+v", a.Sells)
	}
}

// 组预算硬限：累计投入达到 budget_cap 后拒绝再加仓。
func TestEngineBudgetCapRejectsAdd(t *testing.T) {
	cfg := testConfig()
	cfg.Groups[0].BudgetCap = 250 // 首层 100 + 第 2 层 200 = 300 > 250
	e := NewEngine(cfg, 100)
	fillLayer(t, e, 0, 1, 100)
	// 第 2 层会超预算 → 拒绝。
	if a := e.Tick(97); len(a.Buys) != 0 {
		t.Fatalf("budget exceeded must reject add, got %+v", a.Buys)
	}
	if e.Group(0).TotalInvested() != 100 {
		t.Fatalf("total invested = %v, want 100", e.Group(0).TotalInvested())
	}
	// 恰好不超限的预算则允许：200 → 第 2 层 200 恰好 = 300？用 300 验证允许。
	cfg2 := testConfig()
	cfg2.Groups[0].BudgetCap = 300
	e2 := NewEngine(cfg2, 100)
	fillLayer(t, e2, 0, 1, 100)
	if a := e2.Tick(97); len(a.Buys) != 1 {
		t.Fatalf("exact-fit budget should allow add, got %+v", a.Buys)
	}
}

// 未成交不推进：在途层（pending）存在时不发新单，成交确认前层数不变。
func TestEngineNoFillNoLayerAdvance(t *testing.T) {
	e := NewEngine(testConfig(), 100)
	fillLayer(t, e, 0, 1, 100)

	// 第 2 层触发：MarkPending 后、成交回报前，重复 Tick 不得再发单。
	a := e.Tick(97)
	if len(a.Buys) != 1 {
		t.Fatalf("layer 2 should trigger, got %+v", a.Buys)
	}
	if err := e.MarkPending(0, a.Buys[0].Layer); err != nil {
		t.Fatalf("mark pending: %v", err)
	}
	if again := e.Tick(90); len(again.Buys) != 0 {
		t.Fatalf("pending order must block new adds, got %+v", again.Buys)
	}
	if e.Group(0).Layer() != 1 {
		t.Fatalf("layer advanced without fill: %d", e.Group(0).Layer())
	}
	if e.Group(0).TotalInvested() != 100 {
		t.Fatalf("invested advanced without fill: %v", e.Group(0).TotalInvested())
	}
	// 成交回报后推进到第 2 层，随后可触发第 3 层。
	if err := e.ApplyBuyFill(0, 2, 200/97, 97); err != nil {
		t.Fatalf("apply fill: %v", err)
	}
	if e.Group(0).Layer() != 2 || e.Group(0).PendingLayer() != 0 {
		t.Fatalf("layer/pending = %d/%d, want 2/0", e.Group(0).Layer(), e.Group(0).PendingLayer())
	}
}

// TP/SL：组内持仓整体止盈/硬止损卖出，组到达终态。
func TestEngineTakeProfitAndStopLoss(t *testing.T) {
	e := NewEngine(testConfig(), 100)
	fillLayer(t, e, 0, 1, 100)
	if a := e.Tick(104.9); len(a.Sells) != 0 {
		t.Fatalf("below TP should hold, got %+v", a.Sells)
	}
	a := e.Tick(105)
	if len(a.Sells) != 1 || a.Sells[0].Reason != "take_profit" {
		t.Fatalf("TP should sell all, got %+v", a.Sells)
	}
	if err := e.ApplySellFill(0, 1, 105); err != nil {
		t.Fatalf("sell fill: %v", err)
	}
	gs := e.Group(0)
	if !gs.Finished() || gs.InPosition() {
		t.Fatalf("group should be finished & flat: %+v", gs)
	}
	if e.RealizedPnL() != 5 { // (105-100)*1
		t.Fatalf("realized pnl = %v, want 5", e.RealizedPnL())
	}

	// SL 路径（新引擎）。
	e2 := NewEngine(testConfig(), 100)
	fillLayer(t, e2, 0, 1, 100)
	if a := e2.Tick(89); len(a.Sells) != 1 || a.Sells[0].Reason != "stop_loss" {
		t.Fatalf("SL should sell all, got %+v", a.Sells)
	}
	_ = e2.ApplySellFill(0, 1, 89)
	if e2.RealizedPnL() != -11 {
		t.Fatalf("SL realized pnl = %v, want -11", e2.RealizedPnL())
	}
}

// 追踪止盈 + 多组独立：组 0 追踪止盈退出，组 1 独立运行互不影响。
func TestEngineTrailingAndMultiGroupIndependent(t *testing.T) {
	cfg := testConfig()
	cfg.TrailingEnabled = true
	cfg.Groups = append(cfg.Groups, GroupConfig{QuoteAmount: 100, Multiplier: 2, MaxLayers: 3, BudgetCap: 0})
	e := NewEngine(cfg, 100)
	fillLayer(t, e, 0, 1, 100)
	fillLayer(t, e, 1, 1, 110) // 组 1 在更高价位建仓，114 未进其盈利区

	// 两组都观察 120（激活组 0 追踪），组 0 回撤到 114 → 追踪止盈退出；
	// 组 1 均价 110，120/114 均未达 +5% 盈利区 → 不受影响。
	if a := e.Tick(120); len(a.Sells) != 0 {
		t.Fatalf("no retracement yet, got %+v", a.Sells)
	}
	a := e.Tick(114)
	if len(a.Sells) != 1 || a.Sells[0].Group != 0 || a.Sells[0].Reason != "trailing_take_profit" {
		t.Fatalf("group 0 trailing TP expected, got %+v", a.Sells)
	}
	_ = e.ApplySellFill(0, 1, 114)

	// 组 1 未退出且继续按自己的阶梯加仓（价格 114 较其首层 100 深跌两层触发位 94？未到）。
	if e.Group(1).Finished() {
		t.Fatal("group 1 must be unaffected by group 0 exit")
	}
	if e.Group(1).Layer() != 1 {
		t.Fatalf("group 1 layer = %d, want 1", e.Group(1).Layer())
	}
	if e.AllFinished() {
		t.Fatal("not all groups finished yet")
	}
}

// 参数校验与首层预算校验。
func TestEngineConfigValidate(t *testing.T) {
	if msg := testConfig().Validate(); msg != "" {
		t.Fatalf("valid config rejected: %s", msg)
	}
	bad := []Config{
		{DeviationPct: 0, Groups: []GroupConfig{{QuoteAmount: 100}}},
		{DeviationPct: 0.03, Groups: nil},
		{DeviationPct: 0.03, Groups: []GroupConfig{{QuoteAmount: 0}}},
		{DeviationPct: 0.03, Groups: []GroupConfig{{QuoteAmount: 100, MaxLayers: -1}}},
	}
	for i, cfg := range bad {
		if msg := cfg.Validate(); msg == "" {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

// 首层即超预算：首层买入就被 budget_cap 拒绝（quote_amount > cap）。
func TestEngineFirstLayerOverBudget(t *testing.T) {
	cfg := testConfig()
	cfg.Groups[0].BudgetCap = 50 // 首层 100 > 50
	e := NewEngine(cfg, 100)
	if a := e.Tick(100); len(a.Buys) != 0 {
		t.Fatalf("first layer over budget must be rejected, got %+v", a.Buys)
	}
	// 组未 finished（可改参数重启），但也永远不买入 → Runner 依赖 TP/SL 或人工停止。
	if e.Group(0).Layer() != 0 {
		t.Fatalf("layer = %d, want 0", e.Group(0).Layer())
	}
}
