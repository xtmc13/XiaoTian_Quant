package dca

import (
	"testing"
	"time"
)

var testStart = time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)

func testConfig() Config {
	return Config{
		Symbol:          "BTCUSDT",
		QuoteAmount:     100,
		IntervalMinutes: 60,
		MaxOrders:       0,
		PeriodBudget:    0,
		TakeProfitPct:   0.05,
		StopLossPct:     0.10,
		TrailingEnabled: false,
	}
}

// 创建后首单立即触发（无 lastBuyAt 约束），随后间隔未到不重复买入。
func TestEngineFirstBuyImmediateThenIntervalGate(t *testing.T) {
	e := NewEngine(testConfig(), 100)
	a := e.Tick(100, testStart)
	if a.Buy == nil {
		t.Fatalf("first tick should request a buy, got %+v", a)
	}
	if a.Buy.QuoteAmount != 100 {
		t.Fatalf("buy quote amount: got %v, want 100", a.Buy.QuoteAmount)
	}
	// 未成交前不推进：间隔未到也不会再次买入（first buy intent already emitted）。
	if err := e.ApplyBuyFill(1, 100, testStart); err != nil {
		t.Fatalf("apply buy fill: %v", err)
	}
	a = e.Tick(100, testStart.Add(30*time.Minute))
	if a.Buy != nil || a.Sell != nil || a.Done {
		t.Fatalf("interval not elapsed should not act, got %+v", a)
	}
	// 间隔满后再次买入。
	a = e.Tick(100, testStart.Add(60*time.Minute))
	if a.Buy == nil {
		t.Fatalf("interval elapsed should buy again, got %+v", a)
	}
}

// max_orders 达到后停止买入；period_budget 达到后拒绝再加。
func TestEngineMaxOrdersAndBudgetStopBuying(t *testing.T) {
	cfg := testConfig()
	cfg.MaxOrders = 2
	cfg.PeriodBudget = 250
	e := NewEngine(cfg, 100)

	// 第 1 单：立即。
	if a := e.Tick(100, testStart); a.Buy == nil {
		t.Fatalf("order #1 should buy")
	}
	if err := e.ApplyBuyFill(1, 100, testStart); err != nil {
		t.Fatal(err)
	}
	// 第 2 单：预算内（100+100<=250）。
	if a := e.Tick(100, testStart.Add(time.Hour)); a.Buy == nil {
		t.Fatalf("order #2 should buy within budget")
	}
	if err := e.ApplyBuyFill(1, 100, testStart.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// 已投 200，再加 100 将超预算 250 → 拒绝加仓（持仓中继续监控 TP/SL）。
	a := e.Tick(100, testStart.Add(2*time.Hour))
	if a.Buy != nil {
		t.Fatalf("budget exceeded must not buy, got %+v", a)
	}
	if a.Done {
		t.Fatalf("holding engine with exhausted limits is not Done, got %+v", a)
	}
	if e.FilledOrders() != 2 {
		t.Fatalf("filled orders: got %d, want 2", e.FilledOrders())
	}
	// 止盈卖出清仓后 → 下一 Tick 到达终态。
	_ = e.ApplySellFill(2, 110)
	if a := e.Tick(110, testStart.Add(3*time.Hour)); !a.Done {
		t.Fatalf("flat engine with exhausted limits should be Done, got %+v", a)
	}

	// max_orders 路径：2 单后停。
	cfg2 := testConfig()
	cfg2.MaxOrders = 1
	e2 := NewEngine(cfg2, 100)
	_ = e2.Tick(100, testStart)
	_ = e2.ApplyBuyFill(1, 100, testStart)
	a2 := e2.Tick(100, testStart.Add(24*time.Hour))
	if a2.Buy != nil {
		t.Fatalf("max_orders reached must not buy, got %+v", a2)
	}
}

// 止盈：均价之上 take_profit_pct 触发整体卖出（非追踪模式）。
func TestEngineTakeProfitSell(t *testing.T) {
	cfg := testConfig()
	cfg.IntervalMinutes = 7 * 24 * 60 // 拉长定投间隔，隔离买入干扰
	e := NewEngine(cfg, 100)
	_ = e.Tick(100, testStart)
	if err := e.ApplyBuyFill(2, 100, testStart); err != nil {
		t.Fatal(err)
	}
	// 均价 100，+4.9% 不卖。
	if a := e.Tick(104.9, testStart.Add(time.Hour)); a.Sell != nil {
		t.Fatalf("below TP should hold, got %+v", a)
	}
	// +5% 触发止盈。
	a := e.Tick(105, testStart.Add(time.Hour))
	if a.Sell == nil || a.Sell.Reason != "take_profit" {
		t.Fatalf("TP should sell with reason take_profit, got %+v", a)
	}
	if err := e.ApplySellFill(2, 105); err != nil {
		t.Fatal(err)
	}
	if e.RealizedPnL() != 10 {
		t.Fatalf("realized pnl: got %v, want 10", e.RealizedPnL())
	}
	if e.InPosition() {
		t.Fatal("position should be flat after sell fill")
	}
}

// 硬止损：均价之下 stop_loss_pct 无条件卖出（止损优先于定投买入）。
func TestEngineStopLossSell(t *testing.T) {
	cfg := testConfig()
	cfg.IntervalMinutes = 7 * 24 * 60 // 隔离买入干扰
	e := NewEngine(cfg, 100)
	_ = e.Tick(100, testStart)
	if err := e.ApplyBuyFill(1, 100, testStart); err != nil {
		t.Fatal(err)
	}
	if a := e.Tick(89, testStart.Add(time.Hour)); a.Sell == nil || a.Sell.Reason != "stop_loss" {
		t.Fatalf("SL should sell with reason stop_loss, got %+v", a)
	}
}

// 追踪止盈：进入盈利区后不卖在回撤，按最高点回撤止盈。
func TestEngineTrailingTakeProfit(t *testing.T) {
	cfg := testConfig()
	cfg.TrailingEnabled = true
	cfg.IntervalMinutes = 7 * 24 * 60 // 隔离买入干扰
	e := NewEngine(cfg, 100)
	_ = e.Tick(100, testStart)
	if err := e.ApplyBuyFill(1, 100, testStart); err != nil {
		t.Fatal(err)
	}
	// 涨到 120（+20%，激活追踪），未回撤 → 不卖。
	if a := e.Tick(120, testStart.Add(time.Hour)); a.Sell != nil {
		t.Fatalf("no retracement yet should hold, got %+v", a)
	}
	// 从 120 回撤 5%（=114）→ 追踪止盈卖出。
	a := e.Tick(114, testStart.Add(2*time.Hour))
	if a.Sell == nil || a.Sell.Reason != "trailing_take_profit" {
		t.Fatalf("retracement from high should trigger trailing TP, got %+v", a)
	}
}

// 未成交不推进：只有 ApplyBuyFill/ApplySellFill 才更新状态。
func TestEngineNoFillNoAdvance(t *testing.T) {
	e := NewEngine(testConfig(), 100)
	_ = e.Tick(100, testStart) // 产生买入意图，但不回报成交
	if e.FilledOrders() != 0 || e.TotalInvested() != 0 {
		t.Fatalf("state must not advance without fill: orders=%d invested=%v",
			e.FilledOrders(), e.TotalInvested())
	}
	// 即使价格后来到止盈区，空仓也不能卖出。
	if a := e.Tick(200, testStart.Add(time.Hour)); a.Sell != nil {
		t.Fatalf("flat engine must not sell, got %+v", a)
	}
	// 成交回报后状态推进。
	if err := e.ApplyBuyFill(1, 100, testStart); err != nil {
		t.Fatal(err)
	}
	if e.FilledOrders() != 1 {
		t.Fatalf("filled orders should advance after fill, got %d", e.FilledOrders())
	}
}

// 参数校验：非法配置被拒绝。
func TestEngineConfigValidate(t *testing.T) {
	bad := []Config{
		{QuoteAmount: 0, IntervalMinutes: 60},
		{QuoteAmount: 100, IntervalMinutes: 0},
		{QuoteAmount: 100, IntervalMinutes: 60, TakeProfitPct: 1.5},
		{QuoteAmount: 100, IntervalMinutes: 60, MaxOrders: -1},
	}
	for i, cfg := range bad {
		if msg := cfg.Validate(); msg == "" {
			t.Fatalf("case %d: expected validation error for %+v", i, cfg)
		}
	}
	if msg := testConfig().Validate(); msg != "" {
		t.Fatalf("valid config rejected: %s", msg)
	}
}

// Restore 恢复的运行状态与全新引擎一致对待（间隔/限额不回退）。
func TestEngineRestoreKeepsSchedule(t *testing.T) {
	e := Restore(testConfig(), 1, 100, 1, 100, 0, testStart.UnixMilli(), 100, 100)
	if e.FilledOrders() != 1 || e.TotalInvested() != 100 || !e.InPosition() {
		t.Fatalf("restored state mismatch: %+v", e)
	}
	// 恢复后间隔未到 → 不买。
	if a := e.Tick(100, testStart.Add(30*time.Minute)); a.Buy != nil {
		t.Fatalf("restored engine must respect last_buy_at, got %+v", a)
	}
	if a := e.Tick(100, testStart.Add(61*time.Minute)); a.Buy == nil {
		t.Fatalf("restored engine should buy after interval, got %+v", a)
	}
}
