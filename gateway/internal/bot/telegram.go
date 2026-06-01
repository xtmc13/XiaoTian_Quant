// Package bot implements Telegram bot integration for remote trading control.
// Uses long-polling to receive commands and send notifications.
package bot

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Types ──────────────────────────────────────────────────────

// BotConfig configures the Telegram bot.
type BotConfig struct {
	Token  string `json:"token"`
	ChatID int64  `json:"chat_id"`
	Enabled bool  `json:"enabled"`
}

// BotStateProvider provides trading state for command responses.
type BotStateProvider interface {
	GetEquity() float64
	GetDailyPnL() float64
	GetOpenPositions() []PositionInfo
	GetOpenOrders() []OrderInfo
	GetRecentTrades(limit int) []TradeInfo
	GetWhitelist() []string
	GetRiskStatus() RiskStatus
	GetStrategies() []StrategyInfo
}

type PositionInfo struct {
	Symbol    string
	Side      string
	EntryPrice float64
	MarkPrice  float64
	Quantity  float64
	PnL       float64
	PnLPct    float64
}

type OrderInfo struct {
	ID       string
	Symbol   string
	Side     string
	Type     string
	Price    float64
	Quantity float64
	Status   string
}

type TradeInfo struct {
	Symbol    string
	Side      string
	Price     float64
	Quantity  float64
	PnL       float64
	Time      int64
}

type RiskStatus struct {
	DrawdownPct     float64
	DailyOrders     int
	CircuitBreaker  string
	ActiveLocks     []string
}

type StrategyInfo struct {
	Name    string
	Symbol  string
	Running bool
}

// Commands defines callbacks for bot commands.
type Commands struct {
	OnStart    func() error
	OnStop     func() error
	OnPause    func() error
	OnResume   func() error
	OnAddBlacklist func(symbol string) error
	OnRemoveBlacklist func(symbol string) error
	OnForceEntry func(symbol string) error
	OnForceExit  func(symbol string) error
}

// ── Bot ────────────────────────────────────────────────────────

// Bot is the Telegram bot instance.
type Bot struct {
	config   BotConfig
	provider BotStateProvider
	commands Commands
	client   *http.Client
	offset   int64
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
}

// New creates a new Telegram bot.
func New(cfg BotConfig, provider BotStateProvider, cmds Commands) *Bot {
	if cfg.Token == "" {
		return nil
	}
	return &Bot{
		config:   cfg,
		provider: provider,
		commands: cmds,
		client:   &http.Client{Timeout: 30 * time.Second},
		stopCh:   make(chan struct{}),
	}
}

// Start begins long-polling for updates.
func (b *Bot) Start() {
	if b == nil || b.running {
		return
	}
	b.running = true
	go b.poll()
	log.Printf("[telegram] bot started")
}

// Stop stops the bot.
func (b *Bot) Stop() {
	if b == nil || !b.running {
		return
	}
	b.running = false
	close(b.stopCh)
	log.Printf("[telegram] bot stopped")
}

// Send sends a message to the configured chat.
func (b *Bot) Send(text string) error {
	if b == nil || b.config.Token == "" {
		return nil
	}
	return b.apiCall("sendMessage", map[string]any{
		"chat_id":    b.config.ChatID,
		"text":       text,
		"parse_mode": "HTML",
	})
}

// NotifyRisk sends a risk alert.
func (b *Bot) NotifyRisk(level, message string) {
	emoji := "⚠️"
	if level == "CRITICAL" {
		emoji = "🚨"
	}
	b.Send(fmt.Sprintf("%s <b>[%s]</b> %s", emoji, level, message))
}

// NotifyTrade sends a trade notification.
func (b *Bot) NotifyTrade(symbol, side string, price, qty float64) {
	emoji := "🟢"
	if side == "SELL" || side == "SHORT" {
		emoji = "🔴"
	}
	b.Send(fmt.Sprintf("%s <b>Trade</b>: %s %s %.4f @ %.2f",
		emoji, side, symbol, qty, price))
}

// ── Polling ────────────────────────────────────────────────────

func (b *Bot) poll() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.fetchUpdates()
		}
	}
}

func (b *Bot) fetchUpdates() {
	b.mu.Lock()
	offset := b.offset
	b.mu.Unlock()

	params := map[string]any{
		"timeout": 1,
		"offset":  offset,
	}

	result, err := b.apiCallRaw("getUpdates", params)
	if err != nil {
		return
	}

	updates, ok := result["result"].([]any)
	if !ok {
		return
	}

	for _, u := range updates {
		update, ok := u.(map[string]any)
		if !ok {
			continue
		}

		updateID, _ := update["update_id"].(float64)

		b.mu.Lock()
		if int64(updateID) >= b.offset {
			b.offset = int64(updateID) + 1
		}
		b.mu.Unlock()

		msg, ok := update["message"].(map[string]any)
		if !ok {
			continue
		}

		text, _ := msg["text"].(string)
		chat, _ := msg["chat"].(map[string]any)
		chatID, _ := chat["id"].(float64)

		if int64(chatID) != b.config.ChatID && b.config.ChatID != 0 {
			continue // only respond to configured chat
		}

		if text != "" {
			b.handleCommand(text, int64(chatID))
		}
	}
}

// ── Command Handler ────────────────────────────────────────────

func (b *Bot) handleCommand(text string, chatID int64) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return
	}

	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	var response string

	switch cmd {
	case "/start", "/help":
		response = b.cmdHelp()
	case "/status":
		response = b.cmdStatus()
	case "/profit":
		response = b.cmdProfit()
	case "/daily":
		response = b.cmdDaily()
	case "/balance":
		response = b.cmdBalance()
	case "/positions":
		response = b.cmdPositions()
	case "/orders":
		response = b.cmdOrders()
	case "/trades":
		response = b.cmdTrades()
	case "/whitelist":
		response = b.cmdWhitelist()
	case "/strategies":
		response = b.cmdStrategies()
	case "/pause", "/stop":
		if b.commands.OnPause != nil {
			b.commands.OnPause()
		}
		response = "⏸️ Trading paused"
	case "/resume", "/start_trading":
		if b.commands.OnResume != nil {
			b.commands.OnResume()
		}
		response = "▶️ Trading resumed"
	case "/blacklist":
		if len(args) > 0 && b.commands.OnAddBlacklist != nil {
			b.commands.OnAddBlacklist(strings.ToUpper(args[0]))
			response = fmt.Sprintf("🚫 %s added to blacklist", strings.ToUpper(args[0]))
		} else {
			response = "Usage: /blacklist SYMBOL"
		}
	case "/unblacklist":
		if len(args) > 0 && b.commands.OnRemoveBlacklist != nil {
			b.commands.OnRemoveBlacklist(strings.ToUpper(args[0]))
			response = fmt.Sprintf("✅ %s removed from blacklist", strings.ToUpper(args[0]))
		} else {
			response = "Usage: /unblacklist SYMBOL"
		}
	case "/enter":
		if len(args) > 0 && b.commands.OnForceEntry != nil {
			b.commands.OnForceEntry(strings.ToUpper(args[0]))
			response = fmt.Sprintf("📈 Force entry: %s", strings.ToUpper(args[0]))
		}
	case "/exit":
		if len(args) > 0 && b.commands.OnForceExit != nil {
			b.commands.OnForceExit(strings.ToUpper(args[0]))
			response = fmt.Sprintf("📉 Force exit: %s", strings.ToUpper(args[0]))
		}
	default:
		response = fmt.Sprintf("Unknown command: %s. Type /help for available commands.", cmd)
	}

	if response != "" {
		b.apiCall("sendMessage", map[string]any{
			"chat_id":    chatID,
			"text":       response,
			"parse_mode": "HTML",
		})
	}
}

// ── Command Implementations ────────────────────────────────────

func (b *Bot) cmdHelp() string {
	return `<b>🤖 XiaoTianQuant Bot</b>

<b>Trading Control</b>
/status — System status
/pause — Pause trading
/resume — Resume trading
/blacklist SYMBOL — Block a pair
/unblacklist SYMBOL — Unblock a pair
/enter SYMBOL — Force entry
/exit SYMBOL — Force exit

<b>Information</b>
/profit — Overall performance
/daily — Today's summary
/balance — Account balance
/positions — Open positions
/orders — Pending orders
/trades — Recent trades
/whitelist — Trading pairs
/strategies — Active strategies`
}

func (b *Bot) cmdStatus() string {
	if b.provider == nil {
		return "⚠️ Provider not connected"
	}

	rs := b.provider.GetRiskStatus()
	equity := b.provider.GetEquity()

	status := "🟢 RUNNING"
	cb := "CLOSED"
	if rs.CircuitBreaker == "OPEN" {
		status = "🔴 STOPPED"
		cb = "OPEN"
	} else if rs.CircuitBreaker == "HALF_OPEN" {
		status = "🟡 RECOVERING"
		cb = "HALF_OPEN"
	}

	return fmt.Sprintf(`<b>📊 Status</b>
Status: %s
Equity: $%.2f
Drawdown: %.1f%%
Circuit Breaker: %s
Daily Orders: %d`,
		status, equity, rs.DrawdownPct, cb, rs.DailyOrders)
}

func (b *Bot) cmdProfit() string {
	if b.provider == nil {
		return "⚠️ No data"
	}
	equity := b.provider.GetEquity()
	trades := b.provider.GetRecentTrades(50)

	totalPnL := 0.0
	wins := 0
	for _, t := range trades {
		totalPnL += t.PnL
		if t.PnL > 0 {
			wins++
		}
	}
	winRate := 0.0
	if len(trades) > 0 {
		winRate = float64(wins) / float64(len(trades)) * 100
	}

	return fmt.Sprintf(`<b>💰 Performance</b>
Equity: $%.2f
Total PnL: $%.2f
Trades (recent 50): %d
Win Rate: %.1f%%`,
		equity, totalPnL, len(trades), winRate)
}

func (b *Bot) cmdDaily() string {
	if b.provider == nil {
		return "⚠️ No data"
	}
	dailyPnL := b.provider.GetDailyPnL()
	rs := b.provider.GetRiskStatus()

	emoji := "📈"
	if dailyPnL < 0 {
		emoji = "📉"
	}

	return fmt.Sprintf(`<b>%s Today</b>
PnL: $%.2f
Orders: %d
Drawdown: %.1f%%`,
		emoji, dailyPnL, rs.DailyOrders, rs.DrawdownPct)
}

func (b *Bot) cmdBalance() string {
	if b.provider == nil {
		return "⚠️ No data"
	}
	equity := b.provider.GetEquity()

	return fmt.Sprintf(`<b>💎 Balance</b>
Total Equity: $%.2f`, equity)
}

func (b *Bot) cmdPositions() string {
	if b.provider == nil {
		return "⚠️ No data"
	}
	positions := b.provider.GetOpenPositions()
	if len(positions) == 0 {
		return "📭 No open positions"
	}

	var lines []string
	lines = append(lines, "<b>📌 Open Positions</b>")
	for _, p := range positions {
		side := "🟢 LONG"
		if p.Side == "SHORT" {
			side = "🔴 SHORT"
		}
		pnlSign := "+"
		if p.PnL < 0 {
			pnlSign = ""
		}
		lines = append(lines, fmt.Sprintf(
			"%s %s | Entry: %.2f | Mark: %.2f | Qty: %.4f | PnL: %s%.2f (%.1f%%)",
			side, p.Symbol, p.EntryPrice, p.MarkPrice, p.Quantity, pnlSign, p.PnL, p.PnLPct,
		))
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) cmdOrders() string {
	if b.provider == nil {
		return "⚠️ No data"
	}
	orders := b.provider.GetOpenOrders()
	if len(orders) == 0 {
		return "📭 No pending orders"
	}

	var lines []string
	lines = append(lines, "<b>📝 Pending Orders</b>")
	for _, o := range orders {
		side := "🟢 BUY"
		if o.Side == "SELL" {
			side = "🔴 SELL"
		}
		lines = append(lines, fmt.Sprintf(
			"%s %s %s @ %.2f x %.4f [%s]",
			side, o.Symbol, o.Type, o.Price, o.Quantity, o.Status,
		))
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) cmdTrades() string {
	if b.provider == nil {
		return "⚠️ No data"
	}
	trades := b.provider.GetRecentTrades(10)
	if len(trades) == 0 {
		return "📭 No recent trades"
	}

	var lines []string
	lines = append(lines, "<b>🔄 Recent Trades</b>")
	for _, t := range trades {
		pnlSign := "+"
		if t.PnL < 0 {
			pnlSign = ""
		}
		lines = append(lines, fmt.Sprintf(
			"%s %s @ %.2f | PnL: %s%.2f",
			t.Symbol, t.Side, t.Price, pnlSign, t.PnL,
		))
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) cmdWhitelist() string {
	if b.provider == nil {
		return "⚠️ No data"
	}
	pairs := b.provider.GetWhitelist()
	if len(pairs) == 0 {
		return "📭 No whitelist configured"
	}
	return fmt.Sprintf("<b>📋 Whitelist (%d pairs)</b>\n%s", len(pairs), strings.Join(pairs, ", "))
}

func (b *Bot) cmdStrategies() string {
	if b.provider == nil {
		return "⚠️ No data"
	}
	strategies := b.provider.GetStrategies()
	if len(strategies) == 0 {
		return "📭 No active strategies"
	}

	var lines []string
	lines = append(lines, "<b>🧠 Strategies</b>")
	for _, s := range strategies {
		status := "🟢"
		if !s.Running {
			status = "🔴"
		}
		lines = append(lines, fmt.Sprintf("%s %s (%s)", status, s.Name, s.Symbol))
	}
	return strings.Join(lines, "\n")
}

// ── API Helpers ────────────────────────────────────────────────

func (b *Bot) apiCall(method string, params map[string]any) error {
	_, err := b.apiCallRaw(method, params)
	return err
}

func (b *Bot) apiCallRaw(method string, params map[string]any) (map[string]any, error) {
	u := fmt.Sprintf("https://api.telegram.org/bot%s/%s", b.config.Token, method)

	// Build URL with query params
	values := url.Values{}
	for k, v := range params {
		values.Set(k, fmt.Sprintf("%v", v))
	}

	resp, err := b.client.PostForm(u, values)
	if err != nil {
		return nil, fmt.Errorf("telegram api: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("telegram parse: %w", err)
	}

	if ok, _ := result["ok"].(bool); !ok {
		desc, _ := result["description"].(string)
		return result, fmt.Errorf("telegram error: %s", desc)
	}

	return result, nil
}

// Format helpers
func FmtMoney(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

func FmtPct(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64) + "%"
}
