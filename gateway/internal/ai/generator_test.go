package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ═══════════════════════════════════════════════════════════════════
// Generator Tests
// ═══════════════════════════════════════════════════════════════════

func TestNewGenerator(t *testing.T) {
	// Register a test provider first
	RegisterProvider(Provider{
		Name:     "test-gen",
		BaseURL:  "https://test.example.com",
		APIKey:   "key",
		Model:    "m",
		TimeoutS: 30,
	})

	g := NewGenerator("test-gen")
	if g == nil {
		t.Fatal("expected non-nil generator")
	}
	if g.provider == nil {
		t.Fatal("expected provider to be set")
	}
	if g.provider.Name != "test-gen" {
		t.Errorf("provider name = %q, want test-gen", g.provider.Name)
	}
}

func TestNewGenerator_MissingProvider(t *testing.T) {
	g := NewGenerator("nonexistent")
	if g == nil {
		t.Fatal("expected non-nil generator even for missing provider")
	}
	if g.provider != nil {
		t.Error("expected nil provider for nonexistent name")
	}
}

func TestGenerator_Generate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := CompletionResponse{
			ID:    "gen-1",
			Model: "m",
			Choices: []Choice{{
				Message: ChatMessage{
					Role:    RoleAssistant,
					Content: "```json\n{\"name\":\"test\"}\n```\n```go\nfunc OnBar(bar, state) { return 0 }\nfunc Stop() {}\n```",
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	RegisterProvider(Provider{
		Name:     "gen-provider",
		BaseURL:  server.URL,
		APIKey:   "sk",
		Model:    "m",
		TimeoutS: 30,
		client:   &http.Client{Timeout: 5 * time.Second},
	})

	g := NewGenerator("gen-provider")
	result, err := g.Generate(GenerateRequest{
		Context: MarketContext{
			Symbol:         "BTCUSDT",
			Interval:       "1h",
			CurrentPrice:   50000,
			PriceChange24h: 2.5,
			Volume24h:      1e9,
			RecentHigh:     51000,
			RecentLow:      49000,
			Trend:          "UPTREND",
			Volatility:     0.03,
			RSI:            65,
			MACDSignal:     "BULLISH",
		},
		Style:     "trend_following",
		RiskLevel: "moderate",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.SourceCode == "" {
		t.Error("expected extracted source code")
	}
	if result.ConfigJSON == "" {
		t.Error("expected extracted JSON config")
	}
	if result.Confidence <= 0 {
		t.Error("expected positive confidence")
	}
}

func TestGenerator_Generate_NoProvider(t *testing.T) {
	g := NewGenerator("nonexistent")
	_, err := g.Generate(GenerateRequest{})
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
	if !strings.Contains(err.Error(), "no AI provider configured") {
		t.Errorf("error = %q, want no AI provider configured", err.Error())
	}
}

func TestBuildStrategyPrompt(t *testing.T) {
	req := GenerateRequest{
		Context: MarketContext{
			Symbol:         "ETHUSDT",
			Interval:       "4h",
			CurrentPrice:   3000,
			PriceChange24h: -1.2,
			Volume24h:      5e8,
			RecentHigh:     3100,
			RecentLow:      2900,
			Trend:          "DOWNTREND",
			Volatility:     0.025,
			RSI:            42,
			MACDSignal:     "BEARISH",
			SupportLevels:  []float64{2900, 2800},
			ResistanceLevels: []float64{3100, 3200},
		},
		Style:       "mean_reversion",
		RiskLevel:   "conservative",
		Constraints: []string{"max_positions: 3", "no_weekend_trading"},
	}

	prompt := buildStrategyPrompt(req)
	checks := []string{
		"ETHUSDT", "4h", "mean_reversion", "conservative",
		"3000", "-1.2", "DOWNTREND", "BEARISH",
		"max_positions: 3", "no_weekend_trading",
		"2900", "3100", // support/resistance
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════
// Response Parsing Tests
// ═══════════════════════════════════════════════════════════════════

func TestExtractCodeBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "go block",
			input:    "Some text\n```go\nfunc main() {}\n```\nMore text",
			expected: "func main() {}",
		},
		{
			name:     "generic code block",
			input:    "```\nplain code\n```",
			expected: "plain code",
		},
		{
			name:     "no block",
			input:    "just plain text",
			expected: "",
		},
		{
			name:     "empty block",
			input:    "```go\n```",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCodeBlock(tt.input)
			if got != tt.expected {
				t.Errorf("extractCodeBlock() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractJSONBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid json block",
			input:    "```json\n{\"key\":\"value\"}\n```",
			expected: `{"key":"value"}`,
		},
		{
			name:     "no json block",
			input:    "just text",
			expected: "",
		},
		{
			name:     "empty json block",
			input:    "```json\n```",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONBlock(tt.input)
			if got != tt.expected {
				t.Errorf("extractJSONBlock() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════════
// Code Validation Tests
// ═══════════════════════════════════════════════════════════════════

func TestValidateGeneratedCode(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		wantWarn []string
	}{
		{
			name:     "valid code",
			code:     "func OnBar(bar, state) { stopLoss := 0.02 }\nfunc Stop() {}",
			wantWarn: nil,
		},
		{
			name:     "empty code",
			code:     "",
			wantWarn: []string{"empty code"},
		},
		{
			name:     "missing OnBar",
			code:     "func Stop() {}",
			wantWarn: []string{"missing OnBar handler"},
		},
		{
			name:     "missing Stop",
			code:     "func OnBar(bar, state) { stopLoss := 0.02 }",
			wantWarn: []string{"missing Stop method"},
		},
		{
			name:     "missing stop loss",
			code:     "func OnBar(bar, state) {}\nfunc Stop() {}",
			wantWarn: []string{"no stop loss defined"},
		},
		{
			name:     "forbidden pattern os.Exec",
			code:     "func OnBar(bar, state) { stopLoss := 0.02 }\nfunc Stop() {}\nos.Exec(\"rm -rf /\")",
			wantWarn: []string{"forbidden pattern detected: os\\.Exec"},
		},
		{
			name:     "forbidden pattern http.Get",
			code:     "func OnBar(bar, state) { stopLoss := 0.02 }\nfunc Stop() {}\nhttp.Get(\"http://evil.com\")",
			wantWarn: []string{"forbidden pattern detected: http\\.(Get|Post|Do)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateGeneratedCode(tt.code)
			if tt.wantWarn == nil {
				if len(got) > 0 {
					t.Errorf("expected no warnings, got %v", got)
				}
				return
			}
			for _, want := range tt.wantWarn {
				found := false
				for _, g := range got {
					if strings.Contains(g, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("missing warning containing %q in %v", want, got)
				}
			}
		})
	}
}

// ═══════════════════════════════════════════════════════════════════
// GenerateResult Serialization
// ═══════════════════════════════════════════════════════════════════

func TestGenerateResult_JSON(t *testing.T) {
	result := GenerateResult{
		ConfigJSON:  `{"name":"test"}`,
		SourceCode:  "func OnBar() {}",
		Explanation: "A test strategy",
		Confidence:  0.85,
		Warnings:    []string{"warn1"},
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var back GenerateResult
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Confidence != 0.85 {
		t.Errorf("confidence = %f, want 0.85", back.Confidence)
	}
	if len(back.Warnings) != 1 || back.Warnings[0] != "warn1" {
		t.Errorf("warnings = %v, want [warn1]", back.Warnings)
	}
}

func TestMarketContext_JSON(t *testing.T) {
	ctx := MarketContext{
		Symbol:   "BTCUSDT",
		Interval: "1h",
		RSI:      55.5,
		Trend:    "RANGE",
		SupportLevels:    []float64{48000, 47000},
		ResistanceLevels: []float64{52000, 53000},
	}

	b, err := json.Marshal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var back MarketContext
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Symbol != "BTCUSDT" {
		t.Errorf("symbol = %q, want BTCUSDT", back.Symbol)
	}
	if len(back.SupportLevels) != 2 {
		t.Errorf("support levels = %d, want 2", len(back.SupportLevels))
	}
}
