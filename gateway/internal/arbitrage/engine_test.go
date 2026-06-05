package arbitrage

import (
	"testing"
	"time"
)

// mockClient is a test exchange client.
type mockClient struct {
	name  string
	price float64
}

func (m *mockClient) Name() string { return m.name }
func (m *mockClient) GetTicker(symbol string) (map[string]any, error) {
	return map[string]any{"lastPrice": m.price}, nil
}
func (m *mockClient) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	return map[string]any{"order_id": "mock-" + side}, nil
}
func (m *mockClient) GetBalance() ([]map[string]any, error) {
	return nil, nil
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
	cfg := DefaultEngineConfig()
	cfg.PollInterval = 100 * time.Millisecond
	engine := NewEngine(cfg)

	// Need at least 2 exchanges
	err := engine.Start()
	if err == nil {
		t.Error("expected error with no exchanges")
	}

	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 101})

	err = engine.Start()
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !engine.IsRunning() {
		t.Error("expected running")
	}

	time.Sleep(150 * time.Millisecond)
	engine.Stop()
	if engine.IsRunning() {
		t.Error("expected stopped")
	}
}

func TestFindBestOpportunity(t *testing.T) {
	engine := NewEngine(DefaultEngineConfig())
	prices := map[string]float64{
		"binance": 68000,
		"okx":     68500,
		"mexc":    67900,
	}

	opp := engine.findBestOpportunity(prices)
	if opp == nil {
		t.Fatal("expected opportunity")
	}
	if opp.BuyExchange != "mexc" {
		t.Errorf("expected buy at mexc, got %s", opp.BuyExchange)
	}
	if opp.SellExchange != "okx" {
		t.Errorf("expected sell at okx, got %s", opp.SellExchange)
	}
	expectedSpread := (68500.0 - 67900.0) / 67900.0 * 100
	if opp.SpreadPct != expectedSpread {
		t.Errorf("expected spread %.4f, got %.4f", expectedSpread, opp.SpreadPct)
	}
}

func TestOpportunityIsProfitable(t *testing.T) {
	opp := Opportunity{SpreadPct: 0.5}
	if !opp.IsProfitable(0.3, 0.001, 0.001) {
		t.Error("expected profitable")
	}
	if opp.IsProfitable(0.5, 0.001, 0.001) {
		t.Error("expected not profitable (spread equals fees)")
	}
}

func TestFetchPrices(t *testing.T) {
	engine := NewEngine(DefaultEngineConfig())
	engine.RegisterExchange("a", &mockClient{name: "a", price: 100})
	engine.RegisterExchange("b", &mockClient{name: "b", price: 102})

	prices := engine.fetchPrices()
	if len(prices) != 2 {
		t.Errorf("expected 2 prices, got %d", len(prices))
	}
	if prices["a"] != 100 {
		t.Errorf("expected price 100, got %f", prices["a"])
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
	// This is in order package, but let's test the helper functions here
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
