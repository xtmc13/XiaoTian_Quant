package arbitrage

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

func initTestBus() {
	app.Get().EventBus = event.NewEventBus(1000, 2)
}

// mockClient is a test exchange client.
type mockClient struct {
	name     string
	price    float64
	balances []map[string]any
	bus      *event.EventBus
}

func (m *mockClient) Name() string { return m.name }
func (m *mockClient) GetTicker(symbol string) (map[string]any, error) {
	return map[string]any{"lastPrice": m.price}, nil
}
func (m *mockClient) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	return map[string]any{"order_id": "mock-" + side}, nil
}
func (m *mockClient) GetBalance() ([]map[string]any, error) {
	if m.balances != nil {
		return m.balances, nil
	}
	return []map[string]any{
		{"asset": "USDT", "free": "100000"},
		{"asset": "BTC", "free": "100"},
	}, nil
}
func (m *mockClient) StartMarketStream(symbols []string) error { return nil }
func (m *mockClient) StopStream() error                        { return nil }
func (m *mockClient) WireToEventBus(bus *event.EventBus) {
	m.bus = bus
}

func (m *mockClient) publishTick(symbol string, bid, ask float64) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(event.Event{
		Type:   event.TypeTick,
		Symbol: symbol,
		Data: model.Tick{
			Symbol:   symbol,
			Exchange: m.name,
			Bid:      bid,
			Ask:      ask,
			Last:     (bid + ask) / 2,
		},
	})
}

func (m *mockClient) publishOrderBook(symbol string, bids, asks [][2]float64) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(event.Event{
		Type:   event.TypeOrderBook,
		Symbol: symbol,
		Data: model.OrderBookData{
			Symbol:   symbol,
			Exchange: m.name,
			Bids:     bids,
			Asks:     asks,
		},
	})
}

func TestNewEngine(t *testing.T) {
	cfg := DefaultEngineConfig()
	engine := NewEngine(cfg)
	if engine == nil {
		t.Fatal("expected engine")
	}
	if engine.IsRunning() {
		t.Error("expected not running")
	}
}

func TestEngineStartStop(t *testing.T) {
	initTestBus()
	engine := NewEngine(DefaultEngineConfig())

	if err := engine.Start(); err == nil {
		t.Error("expected error with no exchanges")
	}

	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 101})

	if err := engine.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !engine.IsRunning() {
		t.Error("expected running")
	}

	engine.Stop()
	if engine.IsRunning() {
		t.Error("expected stopped")
	}
}

func TestOpportunityIsProfitable(t *testing.T) {
	opp := Opportunity{
		SpreadPct:           0.5,
		Viable:              true,
		ExecutableBuyPrice:  100,
		ExecutableSellPrice: 100.5,
	}
	if !opp.IsProfitable(0.3, 0.001, 0.001) {
		t.Error("expected profitable")
	}
	if opp.IsProfitable(0.5, 0.001, 0.001) {
		t.Error("expected not profitable (spread equals fees)")
	}
}

func TestExecuteDryRun(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = true
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102})

	opp := Opportunity{
		Symbol:       "BTCUSDT",
		BuyExchange:  "a",
		SellExchange: "b",
		BuyPrice:     100,
		SellPrice:    102,
		SpreadPct:    2.0,
	}

	engine.Execute(opp)

	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	trade := history[0]
	if trade.Status != "dry_run" {
		t.Errorf("expected dry_run, got %s", trade.Status)
	}
	if trade.Quantity != 10 { // 1000 / 100
		t.Errorf("expected qty 10, got %f", trade.Quantity)
	}
	if trade.NetProfit <= 0 {
		t.Errorf("expected positive net profit, got %f", trade.NetProfit)
	}
}

func TestGetStats(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = true
	engine := NewEngine(cfg)
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102})

	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}
	engine.Execute(opp)

	stats := engine.GetStats()
	if stats["total_trades"] != 1 {
		t.Errorf("expected 1 trade, got %v", stats["total_trades"])
	}
	if stats["win_count"] != 1 {
		t.Errorf("expected 1 win, got %v", stats["win_count"])
	}
}

func TestGetPositions(t *testing.T) {
	engine := NewEngine(DefaultEngineConfig())
	positions := engine.GetPositions()
	if len(positions) != 0 {
		t.Errorf("expected 0 positions, got %d", len(positions))
	}
}

func TestGetHistoryEmpty(t *testing.T) {
	engine := NewEngine(DefaultEngineConfig())
	history := engine.GetHistory(10)
	if len(history) != 0 {
		t.Errorf("expected 0 history, got %d", len(history))
	}
}

func TestExtractPrice(t *testing.T) {
	tests := []struct {
		key   string
		value any
		want  float64
	}{
		{"lastPrice", "68000.5", 68000.5},
		{"last", 68000.0, 68000.0},
		{"price", "68000", 68000},
		{"close", 68000.5, 68000.5},
		{"unknown", 0, 0},
	}
	for _, tt := range tests {
		ticker := map[string]any{tt.key: tt.value}
		got := extractPrice(ticker)
		if got != tt.want {
			t.Errorf("extractPrice(%s=%v) = %f, want %f", tt.key, tt.value, got, tt.want)
		}
	}
}

func TestCalculateBracketPrices(t *testing.T) {
	if round(1.23456, 2) != 1.23 {
		t.Errorf("round failed")
	}
	if winRate(3, 1) != 75.0 {
		t.Errorf("winRate failed")
	}
}

func TestGetConfig(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.Symbol = "ETHUSDT"
	engine := NewEngine(cfg)
	got := engine.GetConfig()
	if got.Symbol != "ETHUSDT" {
		t.Errorf("expected ETHUSDT, got %s", got.Symbol)
	}
}

func TestSetDryRun(t *testing.T) {
	engine := NewEngine(DefaultEngineConfig())
	engine.SetDryRun(false)
	if engine.GetConfig().DryRun {
		t.Error("expected dry_run false")
	}
}

func TestGetClientCount(t *testing.T) {
	engine := NewEngine(DefaultEngineConfig())
	if engine.GetClientCount() != 0 {
		t.Error("expected 0 clients")
	}
	engine.RegisterExchange("a", &mockClient{name: "a"})
	if engine.GetClientCount() != 1 {
		t.Error("expected 1 client")
	}
}

func TestExecuteInsufficientBalance(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = false
	engine := NewEngine(cfg)
	lowBalance := []map[string]any{{"asset": "USDT", "free": "0"}, {"asset": "BTC", "free": "0"}}
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100, balances: lowBalance})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102, balances: lowBalance})

	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}
	engine.Execute(opp)

	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Status != "failed" {
		t.Errorf("expected failed due to balance, got %s", history[0].Status)
	}
}

func TestExecuteSellFailsLeavesOpenPosition(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = false
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)

	buyClient := &mockClient{name: "a", price: 100}
	sellClient := &failingSellClient{mockClient: mockClient{name: "b", price: 102}}
	engine.RegisterExchange("a", buyClient)
	engine.RegisterExchange("b", sellClient)

	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}
	engine.Execute(opp)

	positions := engine.GetPositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 open position, got %d", len(positions))
	}
	if positions[0].Status != "open" {
		t.Errorf("expected open status, got %s", positions[0].Status)
	}

	// Replace the failing sell client with a working one so manual close can place a real sell order.
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102})

	if err := engine.ClosePosition(positions[0].ID, 103); err != nil {
		t.Fatalf("close position failed: %v", err)
	}
	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry after close, got %d", len(history))
	}
	if history[0].Status != "completed" {
		t.Errorf("expected completed after close, got %s", history[0].Status)
	}
	if history[0].SellOrderID == "" {
		t.Errorf("expected sell_order_id to be set after close")
	}
}

func TestCloseAndFailPosition(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = true
	engine := NewEngine(cfg)
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102})

	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}
	engine.Execute(opp)

	openID := "test-open"
	engine.mu.Lock()
	engine.positions = append(engine.positions, &TradePair{
		ID: openID, Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b",
		BuyPrice: 100, SellPrice: 102, Quantity: 1, Status: "open", OpenedAt: time.Now().UnixMilli(),
	})
	engine.mu.Unlock()

	if err := engine.FailPosition(openID); err != nil {
		t.Fatalf("fail position failed: %v", err)
	}
	if len(engine.GetPositions()) != 0 {
		t.Error("expected 0 positions after fail")
	}
	history := engine.GetHistory(10)
	found := false
	for _, h := range history {
		if h.ID == openID && h.Status == "failed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected failed position in history")
	}
}

func TestDepthCalculation(t *testing.T) {
	buyOB := model.OrderBookData{
		Asks: [][2]float64{{100, 1}, {101, 2}, {102, 5}},
	}
	sellOB := model.OrderBookData{
		Bids: [][2]float64{{105, 1}, {104, 2}, {103, 5}},
	}
	execBuy, execSell, buyDepth, sellDepth, slipBuy, slipSell, maxQty, viable :=
		calculateDepthMetrics(buyOB, sellOB, 2.5)

	if !viable {
		t.Error("expected viable")
	}
	wantBuy := (100*1 + 101*1.5) / 2.5
	if execBuy != wantBuy {
		t.Errorf("expected execBuy %.4f, got %.4f", wantBuy, execBuy)
	}
	wantSell := (105*1 + 104*1.5) / 2.5
	if execSell != wantSell {
		t.Errorf("expected execSell %.4f, got %.4f", wantSell, execSell)
	}
	if buyDepth != 8 {
		t.Errorf("expected buyDepth 8, got %.4f", buyDepth)
	}
	if sellDepth != 8 {
		t.Errorf("expected sellDepth 8, got %.4f", sellDepth)
	}
	if maxQty != 2.5 {
		t.Errorf("expected maxQty 2.5, got %.4f", maxQty)
	}
	if slipBuy <= 0 {
		t.Errorf("expected positive buy slippage, got %.4f", slipBuy)
	}
	if slipSell <= 0 {
		t.Errorf("expected positive sell slippage, got %.4f", slipSell)
	}
}

func TestDepthCalculationInsufficientLiquidity(t *testing.T) {
	buyOB := model.OrderBookData{Asks: [][2]float64{{100, 1}}}
	sellOB := model.OrderBookData{Bids: [][2]float64{{105, 1}}}
	_, _, _, _, _, _, _, viable := calculateDepthMetrics(buyOB, sellOB, 2)
	if viable {
		t.Error("expected not viable due to insufficient liquidity")
	}
}

func TestFindBestOpportunityFromState(t *testing.T) {
	initTestBus()
	bus := app.Get().EventBus

	cfg := DefaultEngineConfig()
	cfg.MinSpreadPct = 0.1
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)

	a := &mockClient{name: "a", price: 100}
	b := &mockClient{name: "b", price: 102}
	engine.RegisterExchange("a", a)
	engine.RegisterExchange("b", b)

	if err := engine.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer engine.Stop()

	// Wire was called during Start; manually store bus reference for mock publishing.
	a.bus = bus
	b.bus = bus

	// Publish order books and ticks.
	a.publishOrderBook("BTCUSDT", [][2]float64{{99, 10}}, [][2]float64{{100, 10}})
	b.publishOrderBook("BTCUSDT", [][2]float64{{101, 10}}, [][2]float64{{102, 10}})
	time.Sleep(50 * time.Millisecond)
	a.publishTick("BTCUSDT", 99, 100)
	b.publishTick("BTCUSDT", 101, 102)

	time.Sleep(100 * time.Millisecond)

	opp := engine.GetLastOpportunity()
	if opp == nil {
		t.Fatal("expected opportunity")
	}
	if opp.BuyExchange != "a" {
		t.Errorf("expected buy at a, got %s", opp.BuyExchange)
	}
	if opp.SellExchange != "b" {
		t.Errorf("expected sell at b, got %s", opp.SellExchange)
	}
	if !opp.Viable {
		t.Error("expected viable opportunity")
	}
	if opp.ExecutableBuyPrice <= 0 || opp.ExecutableSellPrice <= 0 {
		t.Error("expected executable prices")
	}
}

// failingSellClient succeeds on BUY, fails on SELL.
type failingSellClient struct {
	mockClient
}

func (f *failingSellClient) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	if side == "SELL" {
		return nil, fmt.Errorf("sell rejected")
	}
	return f.mockClient.PlaceOrder(symbol, side, orderType, price, quantity)
}

// ── Adaptive Quantity Tests ─────────────────────────────────────

func TestAdaptiveQtyEnabled_ReducesQty(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.AdaptiveQtyEnabled = true
	cfg.OrderSize = 1000
	cfg.MaxSlippagePct = 2.0
	cfg.MinOrderQty = 0.001
	cfg.MinOrderValue = 10.0

	engine := NewEngine(cfg)
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 105})

	buyOB := model.OrderBookData{Asks: [][2]float64{{100, 1}, {101, 2}, {102, 5}}, Bids: [][2]float64{{99, 10}}}
	sellOB := model.OrderBookData{Bids: [][2]float64{{105, 1}, {104, 2}, {103, 5}}, Asks: [][2]float64{{106, 10}}}

	execQty, execBuy, execSell, _, _, slipBuy, slipSell, maxQty, viable :=
		resolveQuantity(buyOB, sellOB, 10.0, 100.0, cfg)

	if !viable {
		t.Fatalf("expected viable opportunity")
	}
	if maxQty != 8.0 {
		t.Errorf("expected maxQty 8.0, got %.4f", maxQty)
	}
	if execQty != 8.0 {
		t.Errorf("expected adjusted qty 8.0, got %.4f", execQty)
	}
	if execBuy <= 0 || execSell <= 0 {
		t.Errorf("expected positive exec prices, got buy=%.4f sell=%.4f", execBuy, execSell)
	}
	if slipBuy > cfg.MaxSlippagePct || slipSell > cfg.MaxSlippagePct {
		t.Errorf("slippage exceeded limit: buy=%.4f sell=%.4f", slipBuy, slipSell)
	}

	// Drive through the full opportunity evaluation path.
	engine.mu.Lock()
	engine.marketState["BTCUSDT"] = map[string]*exchangeMarketState{
		"a": {
			tick:      model.Tick{Symbol: "BTCUSDT", Exchange: "a", Bid: 99, Ask: 100},
			orderBook: buyOB,
			obTime:    time.Now().UnixMilli(),
		},
		"b": {
			tick:      model.Tick{Symbol: "BTCUSDT", Exchange: "b", Bid: 105, Ask: 106},
			orderBook: sellOB,
			obTime:    time.Now().UnixMilli(),
		},
	}
	engine.config.Symbols = []string{"BTCUSDT"}
	engine.config.MinSpreadPct = 0.1
	engine.mu.Unlock()

	opp := engine.findBestOpportunityLocked()
	if opp == nil {
		t.Fatal("expected opportunity from findBestOpportunityLocked")
	}
	if opp.AdjustedQty != 8.0 {
		t.Errorf("expected opp.AdjustedQty 8.0, got %.4f", opp.AdjustedQty)
	}
}

func TestAdaptiveQtyEnabled_RejectsIfSlippageTooHigh(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.AdaptiveQtyEnabled = true
	cfg.OrderSize = 1000
	cfg.MaxSlippagePct = 0.01
	cfg.MinOrderQty = 1.0 // enforce floor so it cannot shrink to zero

	buyOB := model.OrderBookData{Asks: [][2]float64{{100, 0.1}, {110, 10}}}
	sellOB := model.OrderBookData{Bids: [][2]float64{{100, 0.1}, {90, 10}}}

	_, _, _, _, _, _, _, _, viable := resolveQuantity(buyOB, sellOB, 10.0, 100.0, cfg)
	if viable {
		t.Error("expected not viable because slippage exceeds limit above MinOrderQty")
	}
}

func TestAdaptiveQtyEnabled_RespectsMinOrderQty(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.AdaptiveQtyEnabled = true
	cfg.OrderSize = 1000
	cfg.MaxSlippagePct = 1.0
	cfg.MinOrderQty = 5.0

	buyOB := model.OrderBookData{Asks: [][2]float64{{100, 2}}}
	sellOB := model.OrderBookData{Bids: [][2]float64{{100, 2}}}

	_, _, _, _, _, _, _, _, viable := resolveQuantity(buyOB, sellOB, 10.0, 100.0, cfg)
	if viable {
		t.Error("expected not viable because adjusted qty below MinOrderQty")
	}
}

func TestAdaptiveQtyEnabled_RespectsMinOrderValue(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.AdaptiveQtyEnabled = true
	cfg.OrderSize = 1000
	cfg.MaxSlippagePct = 1.0
	cfg.MinOrderQty = 0.001
	cfg.MinOrderValue = 1000.0

	buyOB := model.OrderBookData{Asks: [][2]float64{{100, 5}}}
	sellOB := model.OrderBookData{Bids: [][2]float64{{100, 5}}}

	_, _, _, _, _, _, _, _, viable := resolveQuantity(buyOB, sellOB, 10.0, 100.0, cfg)
	if viable {
		t.Error("expected not viable because adjusted value below MinOrderValue")
	}
}

func TestAdaptiveQtyEnabled_ReevaluatesSpread(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.AdaptiveQtyEnabled = true
	cfg.OrderSize = 1000
	cfg.MinSpreadPct = 0.3
	cfg.MaxSlippagePct = 1.0

	engine := NewEngine(cfg)
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 100.6})

	// Only 1 unit is available at the tight spread; beyond that prices touch.
	buyOB := model.OrderBookData{Asks: [][2]float64{{100, 1}}, Bids: [][2]float64{{99, 10}}}
	sellOB := model.OrderBookData{Bids: [][2]float64{{100.6, 1}}, Asks: [][2]float64{{101, 10}}}

	engine.mu.Lock()
	engine.marketState["BTCUSDT"] = map[string]*exchangeMarketState{
		"a": {
			tick:      model.Tick{Symbol: "BTCUSDT", Exchange: "a", Bid: 99, Ask: 100},
			orderBook: buyOB,
			obTime:    time.Now().UnixMilli(),
		},
		"b": {
			tick:      model.Tick{Symbol: "BTCUSDT", Exchange: "b", Bid: 100.6, Ask: 101},
			orderBook: sellOB,
			obTime:    time.Now().UnixMilli(),
		},
	}
	engine.config.Symbols = []string{"BTCUSDT"}
	engine.mu.Unlock()

	opp := engine.findBestOpportunityLocked()
	if opp == nil {
		t.Fatal("expected opportunity after spread re-evaluation")
	}
	if opp.AdjustedQty != 1.0 {
		t.Errorf("expected adjusted qty 1.0, got %.4f", opp.AdjustedQty)
	}
	net := opp.NetSpreadPct(cfg.FeeA, cfg.FeeB)
	if net < cfg.MinSpreadPct {
		t.Errorf("expected net spread >= %.2f, got %.4f", cfg.MinSpreadPct, net)
	}
}

func TestMaxSlippagePct_HardRejectWhenAdaptiveOff(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.AdaptiveQtyEnabled = false
	cfg.OrderSize = 1000
	cfg.MaxSlippagePct = 0.01

	engine := NewEngine(cfg)
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 110})

	buyOB := model.OrderBookData{Asks: [][2]float64{{100, 0.1}, {110, 10}}}
	sellOB := model.OrderBookData{Bids: [][2]float64{{110, 0.1}, {100, 10}}}

	engine.mu.Lock()
	engine.marketState["BTCUSDT"] = map[string]*exchangeMarketState{
		"a": {orderBook: buyOB, obTime: time.Now().UnixMilli()},
		"b": {orderBook: sellOB, obTime: time.Now().UnixMilli()},
	}
	engine.config.Symbols = []string{"BTCUSDT"}
	engine.config.MinSpreadPct = 0.1
	engine.mu.Unlock()

	opp := engine.findBestOpportunityLocked()
	if opp != nil {
		t.Error("expected no opportunity when slippage exceeds global limit with adaptive disabled")
	}
}

func TestExecuteUsesAdjustedQty(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = true
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102})

	opp := Opportunity{
		Symbol:       "BTCUSDT",
		BuyExchange:  "a",
		SellExchange: "b",
		BuyPrice:     100,
		SellPrice:    102,
		AdjustedQty:  5.0,
		SpreadPct:    2.0,
	}

	engine.Execute(opp)

	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Quantity != 5.0 {
		t.Errorf("expected qty 5.0, got %f", history[0].Quantity)
	}
}

func TestCheckBalanceUsesAdjustedQty(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = false
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)

	// Default qty would be 10; we set balance just enough for adjusted qty of 5.
	balances := []map[string]any{
		{"asset": "USDT", "free": "600"},
		{"asset": "BTC", "free": "5"},
	}
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100, balances: balances})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102, balances: balances})

	opp := Opportunity{
		Symbol:       "BTCUSDT",
		BuyExchange:  "a",
		SellExchange: "b",
		BuyPrice:     100,
		SellPrice:    102,
		AdjustedQty:  5.0,
		SpreadPct:    2.0,
	}

	engine.Execute(opp)

	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Status != "completed" {
		t.Errorf("expected completed, got %s (balance check may have ignored AdjustedQty)", history[0].Status)
	}
	if history[0].Quantity != 5.0 {
		t.Errorf("expected qty 5.0, got %f", history[0].Quantity)
	}
}

func TestCheckBalanceFailsWithDefaultQty(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = false
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)

	// Balance enough for 5 but not for default 10.
	balances := []map[string]any{
		{"asset": "USDT", "free": "600"},
		{"asset": "BTC", "free": "5"},
	}
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100, balances: balances})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102, balances: balances})

	opp := Opportunity{
		Symbol:       "BTCUSDT",
		BuyExchange:  "a",
		SellExchange: "b",
		BuyPrice:     100,
		SellPrice:    102,
		SpreadPct:    2.0,
		// AdjustedQty is zero, so execute falls back to OrderSize/buyPrice = 10.
	}

	engine.Execute(opp)

	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Status != "failed" {
		t.Errorf("expected failed due to insufficient balance, got %s", history[0].Status)
	}
}

// fillMockClient returns realistic exchange PlaceOrder responses.
type fillMockClient struct {
	mockClient
	buyResponse  map[string]any
	sellResponse map[string]any
}

func (f *fillMockClient) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	if side == "BUY" && f.buyResponse != nil {
		return f.buyResponse, nil
	}
	if side == "SELL" && f.sellResponse != nil {
		return f.sellResponse, nil
	}
	return f.mockClient.PlaceOrder(symbol, side, orderType, price, quantity)
}

func TestExecuteUsesActualFillPrice(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = false
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)

	buyClient := &fillMockClient{
		mockClient: mockClient{name: "a", price: 100},
		buyResponse: map[string]any{
			"order_id": "buy-1",
			"status":   "FILLED",
			"fills": []any{
				map[string]any{"qty": "5", "price": "100.1", "commission": "0.05"},
				map[string]any{"qty": "5", "price": "100.2", "commission": "0.05"},
			},
		},
	}
	sellClient := &fillMockClient{
		mockClient: mockClient{name: "b", price: 102},
		sellResponse: map[string]any{
			"order_id":    "sell-1",
			"status":      "FILLED",
			"filled_total": "1019.5",
			"amount":      "10",
			"fee":         "0.2",
		},
	}

	engine.RegisterExchange("a", buyClient)
	engine.RegisterExchange("b", sellClient)

	opp := Opportunity{
		Symbol:       "BTCUSDT",
		BuyExchange:  "a",
		SellExchange: "b",
		BuyPrice:     100,
		SellPrice:    102,
		SpreadPct:    2.0,
	}
	engine.Execute(opp)

	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	trade := history[0]
	if trade.Status != "completed" {
		t.Fatalf("expected completed, got %s", trade.Status)
	}
	if trade.BuyAvgPrice != 100.15 {
		t.Errorf("expected buy avg price 100.15, got %f", trade.BuyAvgPrice)
	}
	if trade.SellAvgPrice != 101.95 {
		t.Errorf("expected sell avg price 101.95, got %f", trade.SellAvgPrice)
	}
	if trade.BuyFee != 0.1 {
		t.Errorf("expected buy fee 0.1, got %f", trade.BuyFee)
	}
	if trade.SellFee != 0.2 {
		t.Errorf("expected sell fee 0.2, got %f", trade.SellFee)
	}
	if trade.BuyFilledQty != 10 {
		t.Errorf("expected buy filled qty 10, got %f", trade.BuyFilledQty)
	}
	if trade.SellFilledQty != 10 {
		t.Errorf("expected sell filled qty 10, got %f", trade.SellFilledQty)
	}
}

// flakySellClient fails SELL once, then succeeds.
type flakySellClient struct {
	mockClient
	failCount int
}

func (f *flakySellClient) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	if side == "SELL" {
		if f.failCount > 0 {
			f.failCount--
			return nil, fmt.Errorf("sell transient error")
		}
	}
	return f.mockClient.PlaceOrder(symbol, side, orderType, price, quantity)
}

func TestExecuteSellFailRetry(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = false
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)

	buyClient := &mockClient{name: "a", price: 100}
	sellClient := &flakySellClient{mockClient: mockClient{name: "b", price: 102}, failCount: 1}
	engine.RegisterExchange("a", buyClient)
	engine.RegisterExchange("b", sellClient)

	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}
	engine.Execute(opp)

	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if history[0].Status != "completed" {
		t.Errorf("expected completed after retry, got %s", history[0].Status)
	}
}

func TestExecuteLiveDoesNotRace(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = true
	cfg.AutoExecute = true
	cfg.MinSpreadPct = 0.1
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)

	// Use a client that publishes its own tick/orderbook so auto-execute triggers.
	a := &mockClient{name: "a", price: 100}
	b := &mockClient{name: "b", price: 102}
	engine.RegisterExchange("a", a)
	engine.RegisterExchange("b", b)

	initTestBus()
	bus := app.Get().EventBus
	a.WireToEventBus(bus)
	b.WireToEventBus(bus)

	if err := engine.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer engine.Stop()

	a.bus = bus
	b.bus = bus

	// Trigger auto-evaluation in background.
	a.publishOrderBook("BTCUSDT", [][2]float64{{99, 10}}, [][2]float64{{100, 10}})
	b.publishOrderBook("BTCUSDT", [][2]float64{{101, 10}}, [][2]float64{{102, 10}})
	time.Sleep(50 * time.Millisecond)
	a.publishTick("BTCUSDT", 99, 100)
	b.publishTick("BTCUSDT", 101, 102)

	// Concurrent manual live executions.
	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}
	for i := 0; i < 10; i++ {
		go engine.ExecuteLive(opp)
	}

	time.Sleep(200 * time.Millisecond)

	// Global DryRun config must remain true.
	if !engine.GetConfig().DryRun {
		t.Error("global DryRun was mutated by concurrent ExecuteLive")
	}
}

func TestGetStatsChecksAndExecutions(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = true
	engine := NewEngine(cfg)
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102})

	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}
	engine.Execute(opp)
	engine.Execute(opp)

	stats := engine.GetStats()
	if stats["checks"].(int64) != 0 {
		t.Errorf("expected 0 auto checks, got %v", stats["checks"])
	}
	if stats["executions"].(int64) != 0 {
		t.Errorf("expected 0 live executions, got %v", stats["executions"])
	}
	if stats["total_trades"] != 2 {
		t.Errorf("expected 2 total trades, got %v", stats["total_trades"])
	}
}

func TestParseFillMetrics(t *testing.T) {
	// Binance style with fills array.
	binanceResult := map[string]any{
		"status": "FILLED",
		"fills": []any{
			map[string]any{"qty": "4", "price": "100", "commission": "0.04"},
			map[string]any{"qty": "6", "price": "101", "commission": "0.06"},
		},
	}
	qty, avg, fee, status := parseFillMetrics(binanceResult, "BUY")
	if status != "FILLED" {
		t.Errorf("expected FILLED, got %s", status)
	}
	if qty != 10 {
		t.Errorf("expected qty 10, got %f", qty)
	}
	if avg != 100.6 {
		t.Errorf("expected avg 100.6, got %f", avg)
	}
	if fee != 0.1 {
		t.Errorf("expected fee 0.1, got %f", fee)
	}

	// Gate.io style.
	gateResult := map[string]any{
		"status":       "FILLED",
		"amount":       "5",
		"filled_total": "505",
		"fee":          "0.5",
	}
	qty, avg, fee, status = parseFillMetrics(gateResult, "SELL")
	if status != "FILLED" {
		t.Errorf("expected FILLED, got %s", status)
	}
	if qty != 5 {
		t.Errorf("expected qty 5, got %f", qty)
	}
	if avg != 101 {
		t.Errorf("expected avg 101, got %f", avg)
	}
	if fee != 0.5 {
		t.Errorf("expected fee 0.5, got %f", fee)
	}
}

func TestClosePositionPlacesSellOrder(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = false
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)

	buyClient := &mockClient{name: "a", price: 100}
	sellClient := &failingSellClient{mockClient: mockClient{name: "b", price: 102}}
	engine.RegisterExchange("a", buyClient)
	engine.RegisterExchange("b", sellClient)

	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}
	engine.Execute(opp)

	positions := engine.GetPositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 open position, got %d", len(positions))
	}

	// Swap to a working sell client for the close.
	engine.RegisterExchange("b", &mockClient{name: "b", price: 103})

	if err := engine.ClosePosition(positions[0].ID, 103); err != nil {
		t.Fatalf("close position failed: %v", err)
	}

	history := engine.GetHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	trade := history[0]
	if trade.Status != "completed" {
		t.Errorf("expected completed, got %s", trade.Status)
	}
	if trade.SellOrderID == "" {
		t.Errorf("expected sell_order_id set after close")
	}
	if trade.SellFilledQty != 10 {
		t.Errorf("expected sell filled qty 10, got %f", trade.SellFilledQty)
	}
}

func TestClosePositionSellFailsStaysOpen(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = false
	cfg.OrderSize = 1000
	engine := NewEngine(cfg)

	buyClient := &mockClient{name: "a", price: 100}
	sellClient := &failingSellClient{mockClient: mockClient{name: "b", price: 102}}
	engine.RegisterExchange("a", buyClient)
	engine.RegisterExchange("b", sellClient)

	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}
	engine.Execute(opp)

	positions := engine.GetPositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 open position, got %d", len(positions))
	}

	if err := engine.ClosePosition(positions[0].ID, 103); err == nil {
		t.Fatal("expected close position to fail when sell order fails")
	}

	positions = engine.GetPositions()
	if len(positions) != 1 || positions[0].Status != "open" {
		t.Errorf("expected position to remain open after failed close, got %d positions with status %s", len(positions), positions[0].Status)
	}
}

func TestMaxPositionsEnforcedUnderConcurrency(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = false
	cfg.OrderSize = 1000
	cfg.MaxPositions = 2
	engine := NewEngine(cfg)

	// slowClient delays PlaceOrder so multiple goroutines can race into execute.
	slow := &slowMockClient{mockClient: mockClient{name: "x", price: 100}, delay: 50 * time.Millisecond}
	engine.RegisterExchange("a", slow)
	engine.RegisterExchange("b", slow)

	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			engine.Execute(opp)
		}()
	}
	wg.Wait()

	history := engine.GetHistory(100)
	if len(history) > cfg.MaxPositions {
		t.Errorf("expected at most %d completed trades, got %d", cfg.MaxPositions, len(history))
	}
}

func TestHistorySizeLimit(t *testing.T) {
	cfg := DefaultEngineConfig()
	cfg.DryRun = true
	engine := NewEngine(cfg)
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102})

	opp := Opportunity{Symbol: "BTCUSDT", BuyExchange: "a", SellExchange: "b", BuyPrice: 100, SellPrice: 102, SpreadPct: 2.0}
	for i := 0; i < maxArbitrageHistorySize+50; i++ {
		engine.Execute(opp)
	}

	history := engine.GetHistory(maxArbitrageHistorySize * 2)
	if len(history) > maxArbitrageHistorySize {
		t.Errorf("expected history capped at %d, got %d", maxArbitrageHistorySize, len(history))
	}
}

// slowMockClient wraps a mock client with a configurable PlaceOrder delay.
type slowMockClient struct {
	mockClient
	delay time.Duration
}

func (s *slowMockClient) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	time.Sleep(s.delay)
	return s.mockClient.PlaceOrder(symbol, side, orderType, price, quantity)
}
