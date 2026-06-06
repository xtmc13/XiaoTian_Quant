package bot

import (
	"io"
	"net/http"
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

type mockRoundTripper struct{}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)),
		Header:     make(http.Header),
	}, nil
}

func newTestBot() *Bot {
	return &Bot{
		config: BotConfig{Token: "mock", Enabled: true},
		client: &http.Client{Transport: &mockRoundTripper{}},
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
	if !strings.Contains(r, "/forcebuy") {
		t.Fatal("help should list /forcebuy")
	}
	if !strings.Contains(r, "/health") {
		t.Fatal("help should list /health")
	}
	if !strings.Contains(r, "v3.0") {
		t.Fatal("help should show version")
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

func TestCmdProfitDaily(t *testing.T) {
	r := newTestBot().cmdProfitDaily()
	if !strings.Contains(r, "500") {
		t.Fatal("should show daily PnL")
	}
	if !strings.Contains(r, "Today") {
		t.Fatal("should show Today header")
	}
}

func TestCmdProfitWeekly(t *testing.T) {
	r := newTestBot().cmdProfitWeekly()
	if !strings.Contains(r, "Weekly") {
		t.Fatal("should show weekly header")
	}
}

func TestCmdProfitMonthly(t *testing.T) {
	r := newTestBot().cmdProfitMonthly()
	if !strings.Contains(r, "Monthly") {
		t.Fatal("should show monthly header")
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

func TestCmdHealth(t *testing.T) {
	r := newTestBot().cmdHealth()
	if !strings.Contains(r, "RUNNING") {
		t.Fatal("should show running status")
	}
}

func TestCmdVersion(t *testing.T) {
	r := newTestBot().cmdVersion()
	if !strings.Contains(r, "3.0.0") {
		t.Fatal("should show version")
	}
}

func TestCmdMarketDir(t *testing.T) {
	r := newTestBot().cmdMarketDir()
	if !strings.Contains(r, "Bullish") {
		t.Fatal("should show market direction")
	}
}

func TestCmdShowConfig(t *testing.T) {
	r := newTestBot().cmdShowConfig()
	if !strings.Contains(r, "binance") {
		t.Fatal("should show exchange")
	}
}

func TestCmdLogs(t *testing.T) {
	r := newTestBot().cmdLogs()
	if !strings.Contains(r, "Logs") {
		t.Fatal("should show logs header")
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

func TestNotify(t *testing.T) {
	b := newTestBot()
	// Should not panic with nil data
	b.Notify(NotifyEntry, "Test", "Message", nil)
	b.Notify(NotifyExit, "Test", "Message", map[string]any{"key": "value"})
	b.Notify(NotifyType("UNKNOWN"), "Test", "Message", nil)
}

func TestSendWithKeyboard(t *testing.T) {
	b := newTestBot()
	kb := InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "A", CallbackData: "a"}},
		},
	}
	// With mock token it will fail network but should not panic
	_ = b.SendWithKeyboard("test", kb)
}

func TestStatusKeyboard(t *testing.T) {
	b := newTestBot()
	kb := b.statusKeyboard()
	if kb == nil {
		t.Fatal("status keyboard should not be nil")
	}
	if len(kb.InlineKeyboard) == 0 {
		t.Fatal("status keyboard should have rows")
	}
	if kb.InlineKeyboard[0][0].CallbackData != "refresh:status" {
		t.Fatal("first button should be refresh:status")
	}
}

func TestPositionsKeyboard(t *testing.T) {
	b := newTestBot()
	kb := b.positionsKeyboard()
	if kb == nil {
		t.Fatal("positions keyboard should not be nil when positions exist")
	}
	if len(kb.InlineKeyboard) == 0 {
		t.Fatal("positions keyboard should have rows")
	}
	if !strings.Contains(kb.InlineKeyboard[0][0].CallbackData, "close:BTC/USDT") {
		t.Fatal("first button should close BTC/USDT")
	}
}

func TestPositionsKeyboardEmpty(t *testing.T) {
	b := newTestBot()
	b.provider.(*mockProvider).positions = nil
	kb := b.positionsKeyboard()
	if kb != nil {
		t.Fatal("positions keyboard should be nil when no positions")
	}
}

func TestStrategiesKeyboard(t *testing.T) {
	b := newTestBot()
	kb := b.strategiesKeyboard()
	if kb == nil {
		t.Fatal("strategies keyboard should not be nil when strategies exist")
	}
	if len(kb.InlineKeyboard) == 0 {
		t.Fatal("strategies keyboard should have rows")
	}
	foundStop := false
	for _, row := range kb.InlineKeyboard {
		if strings.Contains(row[0].CallbackData, "stop_strategy:breakout") {
			foundStop = true
		}
	}
	if !foundStop {
		t.Fatal("should have stop button for running strategy")
	}
}

func TestStrategiesKeyboardStart(t *testing.T) {
	b := newTestBot()
	b.provider.(*mockProvider).strategies = []StrategyInfo{
		{Name: "meanrev", Symbol: "SOLUSDT", Running: false},
	}
	kb := b.strategiesKeyboard()
	if !strings.Contains(kb.InlineKeyboard[0][0].CallbackData, "start_strategy:meanrev") {
		t.Fatal("should have start button for stopped strategy")
	}
}

func TestHelpKeyboard(t *testing.T) {
	b := newTestBot()
	kb := b.helpKeyboard()
	if kb == nil {
		t.Fatal("help keyboard should not be nil")
	}
	if len(kb.InlineKeyboard) < 2 {
		t.Fatal("help keyboard should have multiple rows")
	}
}

func TestHandleCallbackRefresh(t *testing.T) {
	b := newTestBot()
	b.handleCallback(map[string]any{"data": "refresh:status", "id": "q1"}, 12345)
}

func TestHandleCallbackPauseResume(t *testing.T) {
	b := newTestBot()
	b.handleCallback(map[string]any{"data": "pause", "id": "q2"}, 12345)
	b.handleCallback(map[string]any{"data": "resume", "id": "q3"}, 12345)
}

func TestHandleCallbackClose(t *testing.T) {
	b := newTestBot()
	b.commands = Commands{OnForceExit: func(symbol string) error { return nil }}
	b.handleCallback(map[string]any{"data": "close:BTCUSDT", "id": "q4"}, 12345)
}

func TestHandleCallbackUnknown(t *testing.T) {
	b := newTestBot()
	b.handleCallback(map[string]any{"data": "unknown:thing", "id": "q5"}, 12345)
}

func TestHandleCallbackEmptyData(t *testing.T) {
	b := newTestBot()
	b.handleCallback(map[string]any{"id": "q6"}, 12345)
}

func TestHandleCommandForceBuy(t *testing.T) {
	b := newTestBot()
	b.commands = Commands{OnForceEntry: func(symbol string) error { return nil }}
	b.handleCommand("/forcebuy BTC/USDT", 12345)
}

func TestHandleCommandForceSell(t *testing.T) {
	b := newTestBot()
	b.commands = Commands{OnForceExit: func(symbol string) error { return nil }}
	b.handleCommand("/forcesell BTC/USDT", 12345)
}

func TestHandleCommandForceShort(t *testing.T) {
	b := newTestBot()
	b.handleCommand("/forceshort BTC/USDT", 12345)
}

func TestHandleCommandForceExit(t *testing.T) {
	b := newTestBot()
	b.commands = Commands{OnForceExit: func(symbol string) error { return nil }}
	b.handleCommand("/forceexit BTC/USDT", 12345)
}

func TestHandleCommandSystem(t *testing.T) {
	b := newTestBot()
	b.handleCommand("/reload_config", 12345)
	b.handleCommand("/show_config", 12345)
	b.handleCommand("/logs", 12345)
	b.handleCommand("/health", 12345)
	b.handleCommand("/version", 12345)
	b.handleCommand("/marketdir", 12345)
	b.handleCommand("/profit_daily", 12345)
	b.handleCommand("/profit_weekly", 12345)
	b.handleCommand("/profit_monthly", 12345)
}

func TestHandleCommandUnknown(t *testing.T) {
	b := newTestBot()
	b.handleCommand("/unknown_cmd", 12345)
}

func TestHandleCommandNoPrefix(t *testing.T) {
	b := newTestBot()
	b.handleCommand("hello", 12345)
}
