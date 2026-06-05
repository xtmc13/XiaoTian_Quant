package notify

import (
	"strings"
	"sync"
)

// ── Routing Rules ───────────────────────────────────────────────

// RouteRule defines which events go to which channels.
type RouteRule struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Events    []string `json:"events"`    // e.g., ["signal", "trade", "risk"]
	Levels    []string `json:"levels"`    // e.g., ["INFO", "WARN", "CRITICAL"]
	Channels  []string `json:"channels"`  // e.g., ["telegram", "discord", "lark"]
	Enabled   bool     `json:"enabled"`
	MinReturn float64  `json:"min_return_pct,omitempty"` // for backtest/hyperopt
}

// Router manages notification routing rules.
type Router struct {
	rules []RouteRule
	mu    sync.RWMutex
}

// NewRouter creates a notification router with default rules.
func NewRouter() *Router {
	return &Router{
		rules: []RouteRule{
			{
				ID:       "default-critical",
				Name:     "Critical Alerts",
				Events:   []string{}, // all events
				Levels:   []string{"CRITICAL"},
				Channels: []string{"telegram", "discord", "lark", "dingtalk", "email"},
				Enabled:  true,
			},
			{
				ID:       "risk-alerts",
				Name:     "Risk Alerts",
				Events:   []string{"risk", "protection"},
				Levels:   []string{"WARN", "CRITICAL"},
				Channels: []string{"telegram", "discord"},
				Enabled:  true,
			},
			{
				ID:       "trade-signals",
				Name:     "Trade Signals",
				Events:   []string{"signal", "trade"},
				Levels:   []string{"INFO", "WARN", "CRITICAL"},
				Channels: []string{"telegram"},
				Enabled:  true,
			},
			{
				ID:       "backtest-results",
				Name:     "Backtest Results",
				Events:   []string{"backtest", "hyperopt"},
				Levels:   []string{"INFO"},
				Channels: []string{"telegram", "discord"},
				Enabled:  true,
			},
			{
				ID:       "system-events",
				Name:     "System Events",
				Events:   []string{"system"},
				Levels:   []string{"WARN", "CRITICAL"},
				Channels: []string{"telegram", "lark"},
				Enabled:  true,
			},
		},
	}
}

// Match returns the channels that should receive a message.
func (r *Router) Match(eventType, level string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	channelSet := make(map[string]struct{})
	for _, rule := range r.rules {
		if !rule.Enabled {
			continue
		}
		if !matchEvents(rule.Events, eventType) {
			continue
		}
		if !matchLevels(rule.Levels, level) {
			continue
		}
		for _, ch := range rule.Channels {
			channelSet[ch] = struct{}{}
		}
	}

	result := make([]string, 0, len(channelSet))
	for ch := range channelSet {
		result = append(result, ch)
	}
	return result
}

// GetRules returns all routing rules.
func (r *Router) GetRules() []RouteRule {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]RouteRule, len(r.rules))
	copy(result, r.rules)
	return result
}

// UpdateRule updates or adds a routing rule.
func (r *Router) UpdateRule(rule RouteRule) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, existing := range r.rules {
		if existing.ID == rule.ID {
			r.rules[i] = rule
			return
		}
	}
	r.rules = append(r.rules, rule)
}

// DeleteRule removes a routing rule.
func (r *Router) DeleteRule(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, rule := range r.rules {
		if rule.ID == id {
			r.rules = append(r.rules[:i], r.rules[i+1:]...)
			return true
		}
	}
	return false
}

func matchEvents(patterns []string, event string) bool {
	if len(patterns) == 0 {
		return true // empty = match all
	}
	for _, p := range patterns {
		if strings.EqualFold(p, event) || p == "*" {
			return true
		}
	}
	return false
}

func matchLevels(patterns []string, level string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if strings.EqualFold(p, level) || p == "*" {
			return true
		}
	}
	return false
}

// ── Event Broadcaster ─────────────────────────────────────────

// Broadcaster sends templated notifications to the manager with routing.
type Broadcaster struct {
	manager *Manager
	router  *Router
	store   *NotificationStore
}

// NewBroadcaster creates a new event broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		manager: GetManager(),
		router:  NewRouter(),
		store:   GetNotificationStore(),
	}
}

// Broadcast sends a templated notification through the routed channels.
func (b *Broadcaster) Broadcast(tpl Template) {
	msg := tpl.ToMessage()

	// Persist to notification store
	b.store.Add(msg.Title, msg.Content, msg.Level, tpl.EventType)

	// Get target channels
	channels := b.router.Match(tpl.EventType, msg.Level)
	if len(channels) == 0 {
		return
	}

	// Add routing tags
	if msg.Tags == nil {
		msg.Tags = make(map[string]string)
	}
	msg.Tags["_channels"] = strings.Join(channels, ",")
	msg.Tags["_event"] = tpl.EventType

	// Send through manager
	b.manager.Send(msg)
}

// BroadcastSync sends synchronously and returns errors.
func (b *Broadcaster) BroadcastSync(tpl Template) []error {
	msg := tpl.ToMessage()
	b.store.Add(msg.Title, msg.Content, msg.Level, tpl.EventType)
	return b.manager.SendSync(msg)
}

// Convenience methods for common events

func (b *Broadcaster) Signal(symbol, side, strategy string, price float64, params map[string]any) {
	b.Broadcast(NewSignalTemplate(symbol, side, strategy, price, params))
}

func (b *Broadcaster) Trade(symbol, side string, price, qty, pnl float64) {
	b.Broadcast(NewTradeTemplate(symbol, side, price, qty, pnl))
}

func (b *Broadcaster) Risk(level, alertType, message string, metrics map[string]float64) {
	b.Broadcast(NewRiskTemplate(level, alertType, message, metrics))
}

func (b *Broadcaster) Protection(name, symbol, action, reason string, cooldown interface{}) {
	var d interface{}
	switch v := cooldown.(type) {
	case int:
		d = v
	case float64:
		d = int(v)
	default:
		d = cooldown
	}
	_ = d
	// Simplified - just pass empty duration for now
	b.Broadcast(NewProtectionTemplate(name, symbol, action, reason, 0))
}

func (b *Broadcaster) Backtest(symbol, strategy string, result map[string]any, duration interface{}) {
	var d interface{}
	switch v := duration.(type) {
	case int:
		d = v
	case float64:
		d = int(v)
	default:
		d = duration
	}
	_ = d
	b.Broadcast(NewBacktestTemplate(symbol, strategy, result, 0))
}

func (b *Broadcaster) Hyperopt(strategy string, bestParams map[string]any, bestMetrics map[string]float64, evals int, duration interface{}) {
	var d interface{}
	switch v := duration.(type) {
	case int:
		d = v
	case float64:
		d = int(v)
	default:
		d = duration
	}
	_ = d
	b.Broadcast(NewHyperoptTemplate(strategy, bestParams, bestMetrics, evals, 0))
}

func (b *Broadcaster) System(event, message string) {
	b.Broadcast(NewSystemTemplate(event, message))
}

func (b *Broadcaster) DailyReport(equity, dailyPnL, drawdown float64, trades int, winRate float64) {
	b.Broadcast(NewDailyReportTemplate(equity, dailyPnL, drawdown, trades, winRate))
}

func (b *Broadcaster) Pairlist(pairs, removed []string) {
	b.Broadcast(NewPairlistTemplate(pairs, removed))
}

func (b *Broadcaster) Order(symbol, side, orderType, status string, price, qty float64) {
	b.Broadcast(NewOrderTemplate(symbol, side, orderType, status, price, qty))
}
