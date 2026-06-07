package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ═══════════════════════════════════════════════════════════════════
// Pipeline Registration Tests
// ═══════════════════════════════════════════════════════════════════

func TestNewPipeline(t *testing.T) {
	p := NewPipeline()
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}
	if p.agents == nil {
		t.Fatal("expected agents map to be initialized")
	}
	if p.maxRetries != 3 {
		t.Errorf("maxRetries = %d, want 3", p.maxRetries)
	}
	if len(p.agents) != 7 {
		t.Errorf("agents = %d, want 7", len(p.agents))
	}

	expectedAgents := []string{
		"technical_analyst", "onchain_analyst", "sentiment_analyst", "risk_analyst",
		"bull_advocate", "bear_advocate", "trader",
	}
	for _, name := range expectedAgents {
		if _, ok := p.agents[name]; !ok {
			t.Errorf("missing agent %q", name)
		}
	}
}

func TestPipeline_Register(t *testing.T) {
	p := NewPipeline()
	p.Register(Agent{
		Name:     "custom_agent",
		Role:     "test role",
		Provider: "deepseek",
	})

	if _, ok := p.agents["custom_agent"]; !ok {
		t.Error("expected custom_agent to be registered")
	}
	if p.agents["custom_agent"].Role != "test role" {
		t.Errorf("role = %q, want test role", p.agents["custom_agent"].Role)
	}
}

func TestPipeline_Register_Overwrite(t *testing.T) {
	p := NewPipeline()
	p.Register(Agent{
		Name:     "technical_analyst",
		Role:     "overwritten role",
		Provider: "openai",
	})

	if p.agents["technical_analyst"].Role != "overwritten role" {
		t.Errorf("role = %q, want overwritten role", p.agents["technical_analyst"].Role)
	}
	if p.agents["technical_analyst"].Provider != "openai" {
		t.Errorf("provider = %q, want openai", p.agents["technical_analyst"].Provider)
	}
}

// ═══════════════════════════════════════════════════════════════════
// Agent Tests
// ═══════════════════════════════════════════════════════════════════

func TestAgent_Fields(t *testing.T) {
	a := Agent{
		Name:         "test_agent",
		Role:         "You are a test agent",
		Provider:     "deepseek",
		SystemPrompt: "system prompt content",
	}
	if a.Name != "test_agent" {
		t.Errorf("name = %q, want test_agent", a.Name)
	}
	if a.Role != "You are a test agent" {
		t.Errorf("role = %q", a.Role)
	}
	if a.Provider != "deepseek" {
		t.Errorf("provider = %q, want deepseek", a.Provider)
	}
	if a.SystemPrompt != "system prompt content" {
		t.Errorf("systemPrompt = %q", a.SystemPrompt)
	}
}

// ═══════════════════════════════════════════════════════════════════
// AgentResult Tests
// ═══════════════════════════════════════════════════════════════════

func TestAgentResult_JSON(t *testing.T) {
	result := AgentResult{
		Agent:      "technical_analyst",
		Content:    "Bullish divergence on 4h",
		Confidence: 0.82,
		DurationMs: 1250,
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var back AgentResult
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Agent != "technical_analyst" {
		t.Errorf("agent = %q", back.Agent)
	}
	if back.Confidence != 0.82 {
		t.Errorf("confidence = %f, want 0.82", back.Confidence)
	}
	if back.DurationMs != 1250 {
		t.Errorf("durationMs = %d, want 1250", back.DurationMs)
	}
}

// ═══════════════════════════════════════════════════════════════════
// BuildInputPrompt Tests
// ═══════════════════════════════════════════════════════════════════

func TestBuildInputPrompt(t *testing.T) {
	input := MarketInput{
		Symbol:         "BTCUSDT",
		CurrentPrice:   68000.5,
		Change24h:      2.35,
		Volume24h:      1.5e10,
		High24h:        69000,
		Low24h:         67000,
		RSI:            58.5,
		MACD:           "BULLISH",
		Volatility:     0.045,
		FundingRate:    0.0001,
		OpenInterest:   2.5e9,
		NetFlows:       -500.5,
		ActiveAddrs:    850000,
		TVL:            45.2e9,
		FearGreedIndex: 72,
		NewsHeadlines:  "Bitcoin ETF inflows continue",
	}

	prompt := BuildInputPrompt(input)
	checks := []string{
		"BTCUSDT", "68000.5000", "2.35", "BULLISH", "58.5",
		"Bitcoin ETF inflows continue", "72",
		"Net Flows", "Active Addresses", "TVL",
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildInputPrompt_ZeroValues(t *testing.T) {
	input := MarketInput{
		Symbol:       "ETHUSDT",
		CurrentPrice: 0,
		RSI:          0,
		MACD:         "",
	}

	prompt := BuildInputPrompt(input)
	if !strings.Contains(prompt, "ETHUSDT") {
		t.Error("prompt missing symbol")
	}
	if !strings.Contains(prompt, "0.0000") {
		t.Error("prompt missing zero price")
	}
}

// ═══════════════════════════════════════════════════════════════════
// Pipeline Progress Callback
// ═══════════════════════════════════════════════════════════════════

func TestPipeline_OnProgress(t *testing.T) {
	p := NewPipeline()

	var phases []string
	var agents []string
	p.OnProgress = func(phase, agent string) {
		phases = append(phases, phase)
		agents = append(agents, agent)
	}

	// Simulate calling progress (normally done inside Run)
	p.OnProgress("phase_1", "technical_analyst")
	p.OnProgress("phase_1", "onchain_analyst")
	p.OnProgress("phase_2", "bull_advocate")
	p.OnProgress("phase_3", "trader")

	if len(phases) != 4 {
		t.Errorf("phases = %d, want 4", len(phases))
	}
	if phases[0] != "phase_1" {
		t.Errorf("first phase = %q, want phase_1", phases[0])
	}
	if agents[3] != "trader" {
		t.Errorf("last agent = %q, want trader", agents[3])
	}
}

// ═══════════════════════════════════════════════════════════════════
// MarketInput Serialization
// ═══════════════════════════════════════════════════════════════════

func TestMarketInput_JSON(t *testing.T) {
	input := MarketInput{
		Symbol:         "SOLUSDT",
		CurrentPrice:   145.5,
		Change24h:      -3.2,
		Volume24h:      8e8,
		High24h:        150,
		Low24h:         140,
		RSI:            45,
		MACD:           "BEARISH",
		Volatility:     0.06,
		FundingRate:    -0.0005,
		OpenInterest:   1.2e9,
		NetFlows:       1200,
		ActiveAddrs:    450000,
		TVL:            12.5e9,
		FearGreedIndex: 35,
		NewsHeadlines:  "Solana network upgrade delayed",
	}

	b, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}

	var back MarketInput
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Symbol != "SOLUSDT" {
		t.Errorf("symbol = %q, want SOLUSDT", back.Symbol)
	}
	if back.CurrentPrice != 145.5 {
		t.Errorf("currentPrice = %f, want 145.5", back.CurrentPrice)
	}
	if back.ActiveAddrs != 450000 {
		t.Errorf("activeAddrs = %d, want 450000", back.ActiveAddrs)
	}
	if back.FearGreedIndex != 35 {
		t.Errorf("fearGreedIndex = %d, want 35", back.FearGreedIndex)
	}
}

// ═══════════════════════════════════════════════════════════════════
// Pipeline Config / Cache
// ═══════════════════════════════════════════════════════════════════

func TestPipeline_EnableCache(t *testing.T) {
	p := NewPipeline()
	if p.enableCache {
		t.Error("expected cache to be disabled by default")
	}
	p.enableCache = true
	if !p.enableCache {
		t.Error("expected cache to be enabled after setting")
	}
}

func TestPipeline_MaxRetries(t *testing.T) {
	p := NewPipeline()
	if p.maxRetries != 3 {
		t.Errorf("maxRetries = %d, want 3", p.maxRetries)
	}
	p.maxRetries = 5
	if p.maxRetries != 5 {
		t.Errorf("maxRetries = %d, want 5", p.maxRetries)
	}
}

// ═══════════════════════════════════════════════════════════════════
// Concurrent Safety (smoke test)
// ═══════════════════════════════════════════════════════════════════

func TestPipeline_ConcurrentRegister(t *testing.T) {
	p := NewPipeline()

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			p.Register(Agent{
				Name:     fmt.Sprintf("agent_%d", idx),
				Role:     "concurrent test",
				Provider: "deepseek",
			})
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Original 7 + 10 new = 17
	if len(p.agents) != 17 {
		t.Errorf("agents = %d, want 17", len(p.agents))
	}
}

// ═══════════════════════════════════════════════════════════════════
// Duration / Timing Helpers
// ═══════════════════════════════════════════════════════════════════

func TestAgentResult_Duration(t *testing.T) {
	start := time.Now()
	time.Sleep(10 * time.Millisecond)
	duration := time.Since(start).Milliseconds()

	result := AgentResult{
		Agent:      "timing_test",
		Content:    "ok",
		Confidence: 1.0,
		DurationMs: duration,
	}

	if result.DurationMs < 5 {
		t.Errorf("duration = %d ms, expected >= 5", result.DurationMs)
	}
}
