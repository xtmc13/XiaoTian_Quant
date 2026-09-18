package bot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeProvider struct{}

func (fakeProvider) GetEquity() float64               { return 100000 }
func (fakeProvider) GetDailyPnL() float64             { return 0 }
func (fakeProvider) GetOpenPositions() []PositionInfo { return nil }
func (fakeProvider) GetOpenOrders() []OrderInfo       { return nil }
func (fakeProvider) GetRecentTrades(int) []TradeInfo  { return nil }
func (fakeProvider) GetWhitelist() []string           { return nil }
func (fakeProvider) GetRiskStatus() RiskStatus        { return RiskStatus{} }
func (fakeProvider) GetStrategies() []StrategyInfo    { return nil }

// TestTradingCommandsDisabledWithoutChatID：ChatID=0 时交易命令被拒绝、不执行回调。
func TestTradingCommandsDisabledWithoutChatID(t *testing.T) {
	var sent []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// apiCallRaw 以 POST form 发送。
		_ = r.ParseForm()
		if text := r.PostForm.Get("text"); text != "" {
			sent = append(sent, text)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	origBase := telegramAPIBase
	telegramAPIBase = server.URL
	t.Cleanup(func() { telegramAPIBase = origBase })

	pauseCalled := false
	b := New(BotConfig{Token: "test-token", ChatID: 0, Enabled: true}, fakeProvider{}, Commands{
		OnPause: func() error { pauseCalled = true; return nil },
	})
	if b == nil {
		t.Fatal("bot must be constructed")
	}
	if b.tradingCommandsEnabled() {
		t.Fatal("trading commands must be disabled when ChatID == 0")
	}
	for cmd := range []string{} {
		_ = cmd
	}
	for _, cmd := range []string{"/pause", "/forcebuy", "/forceshort", "/blacklist"} {
		if !tradingCommandSet[cmd] {
			t.Fatalf("%s must be in tradingCommandSet", cmd)
		}
	}

	b.handleCommand("/pause", 999)
	if pauseCalled {
		t.Fatal("OnPause must not run when ChatID == 0")
	}
	found := false
	for _, text := range sent {
		if strings.Contains(text, "机器人未配置 ChatID，交易命令已禁用") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected disabled reply, sent=%v", sent)
	}

	// 只读命令不受限：/status 正常响应（不含禁用文案）。
	b.handleCommand("/status", 999)
	for _, text := range sent {
		if strings.Contains(text, "交易命令已禁用") && strings.Contains(text, "Status") {
			t.Fatal("readonly command must not be blocked")
		}
	}
}

// TestTradingCommandsEnabledWithChatID：ChatID 已配置 → 交易命令正常执行。
func TestTradingCommandsEnabledWithChatID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	origBase := telegramAPIBase
	telegramAPIBase = server.URL
	t.Cleanup(func() { telegramAPIBase = origBase })

	pauseCalled := false
	b := New(BotConfig{Token: "test-token", ChatID: 42, Enabled: true}, fakeProvider{}, Commands{
		OnPause: func() error { pauseCalled = true; return nil },
	})
	if !b.tradingCommandsEnabled() {
		t.Fatal("trading commands must be enabled when ChatID set")
	}
	b.handleCommand("/pause", 42)
	if !pauseCalled {
		t.Fatal("OnPause must run when ChatID configured")
	}
}
