package notify

import (
	"fmt"
	"strings"
	"time"
)

// ── Event Types ─────────────────────────────────────────────────

const (
	EventSignal       = "signal"        // 交易信号
	EventTrade        = "trade"         // 成交
	EventRisk         = "risk"          // 风险告警
	EventProtection   = "protection"    // Protection 触发
	EventBacktest     = "backtest"      // 回测完成
	EventHyperopt     = "hyperopt"      // 优化完成
	EventSystem       = "system"        // 系统事件
	EventPairlist     = "pairlist"      // Pairlist 更新
	EventOrder        = "order"         // 订单状态
	EventDailyReport  = "daily_report"  // 日报
)

// ── Notification Templates ─────────────────────────────────────

// Template generates a formatted notification for a specific event.
type Template struct {
	EventType string
	Title     string
	Content   string
	Level     string
	Tags      map[string]string
}

// NewSignalTemplate creates a trade signal notification.
func NewSignalTemplate(symbol, side, strategy string, price float64, params map[string]any) Template {
	emoji := "🟢"
	if side == "SELL" || side == "SHORT" {
		emoji = "🔴"
	}

	var paramLines []string
	for k, v := range params {
		paramLines = append(paramLines, fmt.Sprintf("• %s: %v", k, v))
	}

	content := fmt.Sprintf("%s **%s** %s @ %.2f\n\nStrategy: %s", emoji, side, symbol, price, strategy)
	if len(paramLines) > 0 {
		content += "\n\n**Parameters:**\n" + strings.Join(paramLines, "\n")
	}

	return Template{
		EventType: EventSignal,
		Title:     fmt.Sprintf("Signal: %s %s", side, symbol),
		Content:   content,
		Level:     "INFO",
		Tags:      map[string]string{"symbol": symbol, "side": side, "strategy": strategy},
	}
}

// NewTradeTemplate creates a trade execution notification.
func NewTradeTemplate(symbol, side string, price, qty, pnl float64) Template {
	emoji := "✅"
	pnlStr := ""
	if pnl != 0 {
		pnlEmoji := "📈"
		if pnl < 0 {
			pnlEmoji = "📉"
		}
		pnlStr = fmt.Sprintf("\nPnL: %s %.2f", pnlEmoji, pnl)
	}

	return Template{
		EventType: EventTrade,
		Title:     fmt.Sprintf("Trade: %s %s", side, symbol),
		Content:   fmt.Sprintf("%s **%s** %.4f @ %.2f%s", emoji, side, qty, price, pnlStr),
		Level:     "INFO",
		Tags:      map[string]string{"symbol": symbol, "side": side},
	}
}

// NewRiskTemplate creates a risk alert notification.
func NewRiskTemplate(level, alertType, message string, metrics map[string]float64) Template {
	emoji := "⚠️"
	if level == "CRITICAL" {
		emoji = "🚨"
	}

	var metricLines []string
	for k, v := range metrics {
		metricLines = append(metricLines, fmt.Sprintf("• %s: %.2f", k, v))
	}

	content := fmt.Sprintf("%s **%s**\n\n%s", emoji, alertType, message)
	if len(metricLines) > 0 {
		content += "\n\n**Metrics:**\n" + strings.Join(metricLines, "\n")
	}

	return Template{
		EventType: EventRisk,
		Title:     fmt.Sprintf("Risk Alert: %s", alertType),
		Content:   content,
		Level:     level,
		Tags:      map[string]string{"alert_type": alertType},
	}
}

// NewProtectionTemplate creates a protection trigger notification.
func NewProtectionTemplate(protectionName, symbol, action string, reason string, cooldown time.Duration) Template {
	return Template{
		EventType: EventProtection,
		Title:     fmt.Sprintf("Protection: %s", protectionName),
		Content: fmt.Sprintf(
			"🛡️ **%s** triggered for %s\n\nAction: %s\nReason: %s\nCooldown: %s",
			protectionName, symbol, action, reason, cooldown,
		),
		Level: "WARN",
		Tags:  map[string]string{"protection": protectionName, "symbol": symbol, "action": action},
	}
}

// NewBacktestTemplate creates a backtest completion notification.
func NewBacktestTemplate(symbol, strategy string, result map[string]any, duration time.Duration) Template {
	returnOK := "✅"
	if totalReturn, ok := result["total_return_pct"].(float64); ok && totalReturn < 0 {
		returnOK = "❌"
	}

	content := fmt.Sprintf(
		"%s **Backtest Complete**\n\n"+
			"Symbol: %s\nStrategy: %s\nDuration: %s\n\n"+
			"**Results:**\n"+
			"• Total Return: %.2f%%\n"+
			"• Sharpe: %.2f\n"+
			"• Max Drawdown: %.2f%%\n"+
			"• Win Rate: %.1f%%\n"+
			"• Trades: %v",
		returnOK, symbol, strategy, duration.Round(time.Second),
		result["total_return_pct"],
		result["sharpe_ratio"],
		result["max_drawdown_pct"],
		result["win_rate_pct"],
		result["total_trades"],
	)

	return Template{
		EventType: EventBacktest,
		Title:     fmt.Sprintf("Backtest: %s %s", strategy, symbol),
		Content:   content,
		Level:     "INFO",
		Tags:      map[string]string{"symbol": symbol, "strategy": strategy},
	}
}

// NewHyperoptTemplate creates a hyperopt completion notification.
func NewHyperoptTemplate(strategy string, bestParams map[string]any, bestMetrics map[string]float64, evals int, duration time.Duration) Template {
	var paramLines []string
	for k, v := range bestParams {
		paramLines = append(paramLines, fmt.Sprintf("• %s: %v", k, v))
	}

	content := fmt.Sprintf(
		"🎯 **Hyperopt Complete**\n\n"+
			"Strategy: %s\nEvaluations: %d\nDuration: %s\n\n"+
			"**Best Metrics:**\n"+
			"• Sharpe: %.2f\n"+
			"• Return: %.2f%%\n"+
			"• Max DD: %.2f%%\n"+
			"• Win Rate: %.1f%%\n\n"+
			"**Best Parameters:**\n%s",
		strategy, evals, duration.Round(time.Second),
		bestMetrics["sharpe_ratio"],
		bestMetrics["total_return_pct"],
		bestMetrics["max_drawdown_pct"],
		bestMetrics["win_rate"],
		strings.Join(paramLines, "\n"),
	)

	return Template{
		EventType: EventHyperopt,
		Title:     fmt.Sprintf("Hyperopt: %s", strategy),
		Content:   content,
		Level:     "INFO",
		Tags:      map[string]string{"strategy": strategy},
	}
}

// NewSystemTemplate creates a system event notification.
func NewSystemTemplate(event, message string) Template {
	level := "INFO"
	if strings.Contains(event, "ERROR") || strings.Contains(event, "FAIL") {
		level = "CRITICAL"
	} else if strings.Contains(event, "WARN") {
		level = "WARN"
	}

	return Template{
		EventType: EventSystem,
		Title:     fmt.Sprintf("System: %s", event),
		Content:   message,
		Level:     level,
		Tags:      map[string]string{"event": event},
	}
}

// NewDailyReportTemplate creates a daily summary notification.
func NewDailyReportTemplate(equity, dailyPnL, drawdown float64, trades int, winRate float64) Template {
	emoji := "📈"
	if dailyPnL < 0 {
		emoji = "📉"
	}

	return Template{
		EventType: EventDailyReport,
		Title:     "Daily Report",
		Content: fmt.Sprintf(
			"%s **Daily Summary**\n\n"+
				"Equity: $%.2f\n"+
				"Daily PnL: $%.2f\n"+
				"Drawdown: %.1f%%\n"+
				"Trades: %d\n"+
				"Win Rate: %.1f%%",
			emoji, equity, dailyPnL, drawdown, trades, winRate,
		),
		Level: "INFO",
		Tags:  map[string]string{"type": "daily"},
	}
}

// NewPairlistTemplate creates a pairlist update notification.
func NewPairlistTemplate(pairs []string, removed []string) Template {
	content := fmt.Sprintf("📋 **Pairlist Updated**\n\nActive pairs: %d", len(pairs))
	if len(removed) > 0 {
		content += fmt.Sprintf("\nRemoved: %s", strings.Join(removed, ", "))
	}
	if len(pairs) > 0 && len(pairs) <= 20 {
		content += fmt.Sprintf("\n\nPairs: %s", strings.Join(pairs, ", "))
	}

	return Template{
		EventType: EventPairlist,
		Title:     "Pairlist Update",
		Content:   content,
		Level:     "INFO",
		Tags:      map[string]string{"count": fmt.Sprintf("%d", len(pairs))},
	}
}

// NewOrderTemplate creates an order status notification.
func NewOrderTemplate(symbol, side, orderType, status string, price, qty float64) Template {
	emoji := "📝"
	if status == "FILLED" {
		emoji = "✅"
	} else if status == "CANCELED" || status == "REJECTED" {
		emoji = "❌"
	}

	return Template{
		EventType: EventOrder,
		Title:     fmt.Sprintf("Order %s: %s", status, symbol),
		Content:   fmt.Sprintf("%s **%s** %s %s %.4f @ %.2f", emoji, status, side, symbol, qty, price),
		Level:     "INFO",
		Tags:      map[string]string{"symbol": symbol, "status": status},
	}
}

// ToMessage converts a Template to a notify.Message.
func (t Template) ToMessage() Message {
	return Message{
		Title:     t.Title,
		Content:   t.Content,
		Level:     t.Level,
		Tags:      t.Tags,
		Timestamp: time.Now().UnixMilli(),
	}
}
