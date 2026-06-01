package bot

import (
	"strings"
	"testing"
)

type mockProvider struct {
	equity     float64
	dailyPnL   float64
	positions  []PositionInfo
	orders     []OrderInfo
	trades     []TradeInfo
	whitelist  []string
	risk       RiskStatus
	strategies []StrategyInfo
}

func (m *mockProvider) GetEquity() float64               { return m.equity }
func (m *mockProvider) GetDailyPnL() float64             { return m.dailyPnL }
func (m *mockProvider) GetOpenPositions() []PositionInfo { return m.positions }
func (m *mockProvider) GetOpenOrders() []OrderInfo       { return m.orders }
func (m *mockProvider) GetRecentTrades(int) []TradeInfo  { return m.trades }
func (m *mockProvider) GetWhitelist() []string           { return m.whitelist }
func (m *mockProvider) GetRiskStatus() RiskStatus        { return m.risk }
func (m *mockProvider) GetStrategies() []StrategyInfo    { return m.strategies }

func newTestBot() *Bot {
	return &Bot{
		config: BotConfig{Token: "mock", Enabled: true},
		provider: &mockProvider{
			equity:   100000,
			dailyPnL: 500,
			risk:     RiskStatus{DrawdownPct: 2.5, DailyOrders: 12, CircuitBreaker: "CLOSED"},
			whitelist: []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"},
			strategies: []StrategyInfo{
				{Name: "breakout", Symbol: "BTCUSDT", Running: true},
				{Name: "grid", Symbol: "ETHUSDT", Running: false},
			},
			positions: []PositionInfo{
				{Symbol: "BTC/USDT", Side: "LONG", EntryPrice: 50000, MarkPrice: 51000, Quantity: 0.1, PnL: 100, PnLPct: 2.0},
			},
			orders: []OrderInfo{
				{ID: "1", Symbol: "ETH/USDT", Side: "BUY", Type: "LIMIT", Price: 3000, Quantity: 0.5, Status: "OPEN"},
			},
			trades: []TradeInfo{
				{Symbol: "BTC/USDT", Side: "BUY", Price: 50000, Quantity: 0.1, PnL: 100},
				{Symbol: "SOL/USDT", Side: "SELL", Price: 150, Quantity: 10, PnL: -50},
			},
		},
	}
}

func TestCmdHelp(t *testing.T) {
	r := newTestBot().cmdHelp()
	if !strings.Contains(r, "/status") {
		t.Fatal("help should list /status")
	}
}

func TestCmdStatus(t *testing.T) {
	r := newTestBot().cmdStatus()
	if !strings.Contains(r, "100000") {
		t.Fatal("should show equity")
	}
}

func TestCmdProfit(t *testing.T) {
	r := newTestBot().cmdProfit()
	if !strings.Contains(r, "100000") {
		t.Fatal("should show equity")
	}
}

func TestCmdDaily(t *testing.T) {
	r := newTestBot().cmdDaily()
	if !strings.Contains(r, "500") {
		t.Fatal("should show daily PnL")
	}
}

func TestCmdPositions(t *testing.T) {
	r := newTestBot().cmdPositions()
	if !strings.Contains(r, "BTC/USDT") {
		t.Fatal("should show BTC position")
	}
}

func TestCmdPositionsEmpty(t *testing.T) {
	b := newTestBot()
	b.provider.(*mockProvider).positions = nil
	r := b.cmdPositions()
	if !strings.Contains(r, "No open positions") {
		t.Fatal("should show empty")
	}
}

func TestCmdOrders(t *testing.T) {
	r := newTestBot().cmdOrders()
	if !strings.Contains(r, "ETH/USDT") {
		t.Fatal("should show ETH order")
	}
}

func TestCmdTrades(t *testing.T) {
	r := newTestBot().cmdTrades()
	if !strings.Contains(r, "BTC/USDT") {
		t.Fatal("should show BTC trade")
	}
}

func TestCmdWhitelist(t *testing.T) {
	r := newTestBot().cmdWhitelist()
	if !strings.Contains(r, "3 pairs") {
		t.Fatal("should show count")
	}
}

func TestCmdStrategies(t *testing.T) {
	r := newTestBot().cmdStrategies()
	if !strings.Contains(r, "breakout") {
		t.Fatal("should list strategies")
	}
}

func TestCmdBalance(t *testing.T) {
	r := newTestBot().cmdBalance()
	if !strings.Contains(r, "100000") {
		t.Fatal("should show equity")
	}
}

func TestBotNewEmptyToken(t *testing.T) {
	b := New(BotConfig{Token: ""}, nil, Commands{})
	if b != nil {
		t.Fatal("empty token should return nil")
	}
}

func TestBotNewWithToken(t *testing.T) {
	b := New(BotConfig{Token: "test", Enabled: true}, nil, Commands{})
	if b == nil {
		t.Fatal("valid token should create bot")
	}
}

func TestBotProviderNil(t *testing.T) {
	r := (&Bot{config: BotConfig{Token: "x"}, provider: nil}).cmdStatus()
	if !strings.Contains(r, "not connected") {
		t.Fatal("nil provider should warn")
	}
}
