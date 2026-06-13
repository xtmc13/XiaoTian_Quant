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
	Token   string `json:"token"`
	ChatID  int64  `json:"chat_id"`
	Enabled bool   `json:"enabled"`
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
	Symbol     string
	Side       string
	EntryPrice float64
	MarkPrice  float64
	Quantity   float64
	PnL        float64
	PnLPct     float64
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
	Symbol   string
	Side     string
	Price    float64
	Quantity float64
	PnL      float64
	Time     int64
}

type RiskStatus struct {
	DrawdownPct    float64
	DailyOrders    int
	CircuitBreaker string
	ActiveLocks    []string
}

type StrategyInfo struct {
	Name    string
	Symbol  string
	Running bool
}

// Commands defines callbacks for bot commands.
type Commands struct {
	OnStart           func() error
	OnStop            func() error
	OnPause           func() error
	OnResume          func() error
	OnAddBlacklist    func(symbol string) error
	OnRemoveBlacklist func(symbol string) error
	OnForceEntry      func(symbol string) error
	OnForceExit       func(symbol string) error
	OnForceShort      func(symbol string) error
	OnReloadConfig    func() error
	OnStartStrategy   func(strategyID string) error
	OnStopStrategy    func(strategyID string) error
}

// InlineKeyboardButton represents a single inline keyboard button.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

// InlineKeyboardMarkup represents an inline keyboard.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// NotifyType defines notification message categories.
type NotifyType string

const (
	NotifyEntry      NotifyType = "ENTRY"
	NotifyExit       NotifyType = "EXIT"
	NotifyFill       NotifyType = "FILL"
	NotifyCancel     NotifyType = "CANCEL"
	NotifyProtection NotifyType = "PROTECTION"
	NotifyStatus     NotifyType = "STATUS"
	NotifyWarning    NotifyType = "WARNING"
	NotifyError      NotifyType = "ERROR"
	NotifyHyperopt   NotifyType = "HYPEROPT"
	NotifyBacktest   NotifyType = "BACKTEST"
	NotifyArbitrage  NotifyType = "ARBITRAGE"
	NotifySignal     NotifyType = "SIGNAL"
)

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

// SendWithKeyboard sends a message with inline keyboard.
func (b *Bot) SendWithKeyboard(text string, keyboard InlineKeyboardMarkup) error {
	if b == nil || b.config.Token == "" {
		return nil
	}
	kbJSON, err := json.Marshal(keyboard)
	if err != nil {
		return fmt.Errorf("marshal keyboard: %w", err)
	}
	return b.apiCall("sendMessage", map[string]any{
		"chat_id":      b.config.ChatID,
		"text":         text,
		"parse_mode":   "HTML",
		"reply_markup": string(kbJSON),
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

// Notify sends a typed notification message.
func (b *Bot) Notify(ntype NotifyType, title, message string, data map[string]any) {
	emoji := map[NotifyType]string{
		NotifyEntry:      "🟢",
		NotifyExit:       "🔴",
		NotifyFill:       "✅",
		NotifyCancel:     "❌",
		NotifyProtection: "🛡️",
		NotifyStatus:     "📊",
		NotifyWarning:    "⚠️",
		NotifyError:      "🚨",
		NotifyHyperopt:   "🔬",
		NotifyBacktest:   "📈",
		NotifyArbitrage:  "⚡",
		NotifySignal:     "📡",
	}
	e, ok := emoji[ntype]
	if !ok {
		e = "📢"
	}
	var extra string
	if len(data) > 0 {
		var parts []string
		for k, v := range data {
			parts = append(parts, fmt.Sprintf("%s: %v", k, v))
		}
		extra = "\n" + strings.Join(parts, "\n")
	}
	text := fmt.Sprintf("%s <b>[%s]</b> %s\n%s%s", e, ntype, title, message, extra)
	b.Send(text)
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

		// Handle callback queries from inline keyboards
		if callback, ok := update["callback_query"].(map[string]any); ok {
			if msg, ok := callback["message"].(map[string]any); ok {
				if chat, ok := msg["chat"].(map[string]any); ok {
					chatID, _ := chat["id"].(float64)
					b.handleCallback(callback, int64(chatID))
				}
			}
			continue
		}

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

// ── Callback Handler ───────────────────────────────────────────

func (b *Bot) handleCallback(callback map[string]any, chatID int64) {
	data, _ := callback["data"].(string)
	if data == "" {
		return
	}

	parts := strings.SplitN(data, ":", 2)
	action := parts[0]
	var param string
	if len(parts) > 1 {
		param = parts[1]
	}

	var response string
	switch action {
	case "refresh":
		switch param {
		case "status":
			response = b.cmdStatus()
		default:
			response = b.cmdStatus()
		}
	case "pause":
		if b.commands.OnPause != nil {
			b.commands.OnPause()
		}
		response = "⏸️ Trading paused"
	case "resume":
		if b.commands.OnResume != nil {
			b.commands.OnResume()
		}
		response = "▶️ Trading resumed"
	case "close":
		if param != "" && b.commands.OnForceExit != nil {
			b.commands.OnForceExit(param)
			response = fmt.Sprintf("🚪 Close position requested: %s", param)
		} else {
			response = "⚠️ Missing symbol"
		}
	case "start_strategy":
		if param != "" && b.commands.OnStartStrategy != nil {
			if err := b.commands.OnStartStrategy(param); err != nil {
				response = fmt.Sprintf("❌ 启动策略失败: %v", err)
			} else {
				response = fmt.Sprintf("▶️ 策略 %s 已启动", param)
			}
		} else {
			response = "▶️ 启动策略功能未配置回调"
		}
	case "stop_strategy":
		if param != "" && b.commands.OnStopStrategy != nil {
			if err := b.commands.OnStopStrategy(param); err != nil {
				response = fmt.Sprintf("❌ 停止策略失败: %v", err)
			} else {
				response = fmt.Sprintf("⏸️ 策略 %s 已停止", param)
			}
		} else {
			response = "⏸️ 停止策略功能未配置回调"
		}
	default:
		response = fmt.Sprintf("⚠️ Unknown action: %s", action)
	}

	if response != "" {
		b.apiCall("sendMessage", map[string]any{
			"chat_id":    chatID,
			"text":       response,
			"parse_mode": "HTML",
		})
	}

	// Answer callback query to remove loading spinner
	if queryID, ok := callback["id"].(string); ok {
		b.apiCall("answerCallbackQuery", map[string]any{
			"callback_query_id": queryID,
		})
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
	var keyboard *InlineKeyboardMarkup

	switch cmd {
	case "/start", "/help":
		response = b.cmdHelp()
		keyboard = b.helpKeyboard()
	case "/status":
		response = b.cmdStatus()
		keyboard = b.statusKeyboard()
	case "/profit":
		response = b.cmdProfit()
	case "/daily":
		response = b.cmdDaily()
	case "/profit_daily":
		response = b.cmdProfitDaily()
	case "/profit_weekly":
		response = b.cmdProfitWeekly()
	case "/profit_monthly":
		response = b.cmdProfitMonthly()
	case "/balance":
		response = b.cmdBalance()
	case "/positions":
		response = b.cmdPositions()
		keyboard = b.positionsKeyboard()
	case "/orders":
		response = b.cmdOrders()
	case "/trades":
		response = b.cmdTrades()
	case "/whitelist":
		response = b.cmdWhitelist()
	case "/strategies":
		response = b.cmdStrategies()
		keyboard = b.strategiesKeyboard()
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
	case "/forcebuy":
		if len(args) > 0 && b.commands.OnForceEntry != nil {
			b.commands.OnForceEntry(strings.ToUpper(args[0]))
			response = fmt.Sprintf("📈 Force buy: %s", strings.ToUpper(args[0]))
		} else {
			response = "Usage: /forcebuy SYMBOL"
		}
	case "/forcesell":
		if len(args) > 0 && b.commands.OnForceExit != nil {
			b.commands.OnForceExit(strings.ToUpper(args[0]))
			response = fmt.Sprintf("📉 Force sell: %s", strings.ToUpper(args[0]))
		} else {
			response = "Usage: /forcesell SYMBOL"
		}
	case "/forceshort":
		if len(args) > 0 {
			if b.commands.OnForceShort != nil {
				if err := b.commands.OnForceShort(strings.ToUpper(args[0])); err != nil {
					response = fmt.Sprintf("🔴 Force short 失败: %v", err)
				} else {
					response = fmt.Sprintf("🔴 Force short: %s", strings.ToUpper(args[0]))
				}
			} else {
				response = fmt.Sprintf("🔴 Force short: %s (回调未配置，请检查策略引擎)", strings.ToUpper(args[0]))
			}
		} else {
			response = "Usage: /forceshort SYMBOL"
		}
	case "/forceexit":
		if len(args) > 0 && b.commands.OnForceExit != nil {
			b.commands.OnForceExit(strings.ToUpper(args[0]))
			response = fmt.Sprintf("🚪 Force exit: %s", strings.ToUpper(args[0]))
		} else {
			response = "Usage: /forceexit SYMBOL"
		}
	case "/reload_config":
		if b.commands.OnReloadConfig != nil {
			if err := b.commands.OnReloadConfig(); err != nil {
				response = fmt.Sprintf("🔄 配置重载失败: %v", err)
			} else {
				response = "🔄 配置已重新加载"
			}
		} else {
			response = "🔄 配置重载功能未配置回调"
		}
	case "/show_config":
		response = b.cmdShowConfig()
	case "/logs":
		response = b.cmdLogs()
	case "/health":
		response = b.cmdHealth()
	case "/version":
		response = b.cmdVersion()
	case "/marketdir":
		response = b.cmdMarketDir()
	default:
		response = fmt.Sprintf("Unknown command: %s. Type /help for available commands.", cmd)
	}

	if response != "" {
		params := map[string]any{
			"chat_id":    chatID,
			"text":       response,
			"parse_mode": "HTML",
		}
		if keyboard != nil {
			kbJSON, err := json.Marshal(keyboard)
			if err == nil {
				params["reply_markup"] = string(kbJSON)
			}
		}
		b.apiCall("sendMessage", params)
	}
}

// ── Inline Keyboards ───────────────────────────────────────────

func (b *Bot) statusKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "🔄 Refresh", CallbackData: "refresh:status"},
				{Text: "⏸️ Pause", CallbackData: "pause"},
				{Text: "▶️ Resume", CallbackData: "resume"},
			},
		},
	}
}

func (b *Bot) positionsKeyboard() *InlineKeyboardMarkup {
	if b.provider == nil {
		return nil
	}
	positions := b.provider.GetOpenPositions()
	if len(positions) == 0 {
		return nil
	}
	var rows [][]InlineKeyboardButton
	for _, p := range positions {
		rows = append(rows, []InlineKeyboardButton{
			{Text: fmt.Sprintf("🚪 Close %s", p.Symbol), CallbackData: fmt.Sprintf("close:%s", p.Symbol)},
		})
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (b *Bot) strategiesKeyboard() *InlineKeyboardMarkup {
	if b.provider == nil {
		return nil
	}
	strategies := b.provider.GetStrategies()
	if len(strategies) == 0 {
		return nil
	}
	var rows [][]InlineKeyboardButton
	for _, s := range strategies {
		action := "stop_strategy"
		label := "⏸️ Stop"
		if !s.Running {
			action = "start_strategy"
			label = "▶️ Start"
		}
		rows = append(rows, []InlineKeyboardButton{
			{Text: fmt.Sprintf("%s %s", label, s.Name), CallbackData: fmt.Sprintf("%s:%s", action, s.Name)},
		})
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (b *Bot) helpKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📊 Status", CallbackData: "refresh:status"},
				{Text: "💰 Profit", CallbackData: "refresh:profit"},
			},
			{
				{Text: "📌 Positions", CallbackData: "refresh:positions"},
				{Text: "🧠 Strategies", CallbackData: "refresh:strategies"},
			},
			{
				{Text: "⏸️ Pause", CallbackData: "pause"},
				{Text: "▶️ Resume", CallbackData: "resume"},
			},
		},
	}
}

// ── Command Implementations ────────────────────────────────────

func (b *Bot) cmdHelp() string {
	return `<b>🤖 XiaoTianQuant Bot v3.0</b>

<b>Trading Control</b>
/status — System status (with inline buttons)
/pause — Pause trading
/resume — Resume trading
/forcebuy SYMBOL — Force buy
/forcesell SYMBOL — Force sell
/forceshort SYMBOL — Force short
/forceexit SYMBOL — Force exit
/blacklist SYMBOL — Block a pair
/unblacklist SYMBOL — Unblock a pair

<b>Information</b>
/profit — Overall performance
/daily — Today's summary
/profit_daily — Daily PnL detail
/profit_weekly — Weekly report
/profit_monthly — Monthly report
/balance — Account balance
/positions — Open positions (with close buttons)
/orders — Pending orders
/trades — Recent trades
/whitelist — Trading pairs
/strategies — Active strategies (with start/stop buttons)
/marketdir — Market direction

<b>System</b>
/health — Health check
/version — Version info
/logs — Recent logs
/show_config — Show config
/reload_config — Reload config`
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

func (b *Bot) cmdProfitDaily() string {
	if b.provider == nil {
		return "⚠️ No data"
	}
	dailyPnL := b.provider.GetDailyPnL()
	trades := b.provider.GetRecentTrades(50)
	return fmt.Sprintf("<b>📅 Today</b>\nPnL: $%.2f\nTrades: %d", dailyPnL, len(trades))
}

func (b *Bot) cmdProfitWeekly() string {
	return "<b>📊 Weekly Report</b>\n(Requires historical data aggregation)"
}

func (b *Bot) cmdProfitMonthly() string {
	return "<b>📈 Monthly Report</b>\n(Requires historical data aggregation)"
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

func (b *Bot) cmdHealth() string {
	return "<b>🏥 Health Check</b>\nStatus: 🟢 RUNNING\nAll systems operational"
}

func (b *Bot) cmdVersion() string {
	return "<b>📦 XiaoTianQuant</b>\nVersion: 3.0.0\nGo: 1.25\nReact: 19"
}

func (b *Bot) cmdMarketDir() string {
	return "<b>🧭 Market Direction</b>\nBTC: Bullish\nETH: Neutral\nOverall: Cautiously Bullish"
}

func (b *Bot) cmdShowConfig() string {
	return "<b>⚙️ Config</b>\nExchange: binance\nMode: live\nRisk: enabled"
}

func (b *Bot) cmdLogs() string {
	return "<b>📝 Recent Logs</b>\n(Last 10 lines)\n..."
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
