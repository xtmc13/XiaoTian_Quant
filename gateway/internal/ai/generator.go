package ai

import (
	"fmt"
	"regexp"
	"strings"
)

// ── Strategy Generator ──

// Generator uses AI to create trading strategy code from market context.
type Generator struct {
	provider *Provider
}

func NewGenerator(providerName string) *Generator {
	return &Generator{
		provider: GetProvider(providerName),
	}
}

// MarketContext holds market data for strategy generation.
type MarketContext struct {
	Symbol           string   `json:"symbol"`
	Interval         string   `json:"interval"`
	CurrentPrice     float64  `json:"current_price"`
	PriceChange24h   float64  `json:"price_change_24h"`
	Volume24h        float64  `json:"volume_24h"`
	RecentHigh       float64  `json:"recent_high"`
	RecentLow        float64  `json:"recent_low"`
	Trend            string   `json:"trend"`        // UPTREND, DOWNTREND, RANGE
	Volatility       float64  `json:"volatility"`
	SupportLevels    []float64 `json:"support_levels"`
	ResistanceLevels []float64 `json:"resistance_levels"`
	RSI              float64  `json:"rsi"`
	MACDSignal       string   `json:"macd_signal"` // BULLISH, BEARISH
	OrderBookImb     float64  `json:"order_book_imb"`
	SpreadBps        float64  `json:"spread_bps"`
}

// GenerateRequest is the input for strategy generation.
type GenerateRequest struct {
	Context     MarketContext `json:"context"`
	Style       string        `json:"style"`    // trend_following, mean_reversion, breakout, grid, market_making
	RiskLevel   string        `json:"risk_level"` // conservative, moderate, aggressive
	Constraints []string      `json:"constraints"`
}

// GenerateResult holds the generated strategy.
type GenerateResult struct {
	ConfigJSON   string   `json:"config_json"`
	SourceCode   string   `json:"source_code"`
	Explanation  string   `json:"explanation"`
	Confidence   float64  `json:"confidence"`
	Warnings     []string `json:"warnings"`
}

// Generate creates a trading strategy based on market context.
func (g *Generator) Generate(req GenerateRequest) (*GenerateResult, error) {
	if g.provider == nil {
		return nil, fmt.Errorf("no AI provider configured")
	}

	prompt := buildStrategyPrompt(req)

	resp, err := g.provider.ChatCompletion(CompletionRequest{
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: strategySystemPrompt},
			{Role: RoleUser, Content: prompt},
		},
		MaxTokens:   4096,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from AI")
	}

	response := resp.Choices[0].Message.Content
	result := &GenerateResult{
		SourceCode:  extractCodeBlock(response),
		ConfigJSON:  extractJSONBlock(response),
		Explanation: response,
		Confidence:  0.7,
	}

	// Validate generated code
	warnings := validateGeneratedCode(result.SourceCode)
	result.Warnings = warnings

	return result, nil
}

// GenerateWithFeedback iterates to improve the strategy (up to 3 iterations).
func (g *Generator) GenerateWithFeedback(req GenerateRequest, backtestFeedback string) (*GenerateResult, error) {
	result, err := g.Generate(req)
	if err != nil {
		return nil, err
	}

	for iter := 0; iter < 2; iter++ {
		if backtestFeedback == "" {
			break
		}

		feedbackPrompt := fmt.Sprintf(
			"The backtest of this strategy had the following issues:\n%s\n\nPlease improve the strategy code. Original context:\n%s",
			backtestFeedback, buildStrategyPrompt(req),
		)

		resp, err := g.provider.ChatCompletion(CompletionRequest{
			Messages: []ChatMessage{
				{Role: RoleSystem, Content: strategySystemPrompt},
				{Role: RoleUser, Content: buildStrategyPrompt(req)},
				{Role: RoleAssistant, Content: result.SourceCode},
				{Role: RoleUser, Content: feedbackPrompt},
			},
			MaxTokens:   4096,
			Temperature: 0.3,
		})
		if err != nil {
			break
		}

		if len(resp.Choices) > 0 {
			code := extractCodeBlock(resp.Choices[0].Message.Content)
			if code != "" {
				result.SourceCode = code
				result.Explanation = resp.Choices[0].Message.Content
			}
		}
	}

	return result, nil
}

// ── Prompt Building ──

var strategySystemPrompt = `You are a quantitative trading strategy developer. Generate event-driven trading strategies.

Output format:
1. A JSON configuration block enclosed in ` + "```json" + ` ... ` + "```" + `
2. A Go source code block enclosed in ` + "```go" + ` ... ` + "```" + `

The strategy must implement:
- OnBar(bar, state) method for bar-driven signals
- Proper risk management (stop loss, take profit, position sizing)
- Entry and exit conditions
- No external network calls or file I/O
- Pure computation using only provided bar data

Always include:
- Entry condition with clear thresholds
- Stop loss based on ATR or percentage
- Take profit at 2:1 or better risk-reward
- Maximum position size as % of equity`

func buildStrategyPrompt(req GenerateRequest) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Create a %s trading strategy for %s on %s interval.\n\n", req.Style, req.Context.Symbol, req.Context.Interval))
	sb.WriteString("Market Context:\n")
	sb.WriteString(fmt.Sprintf("- Current Price: %.2f\n", req.Context.CurrentPrice))
	sb.WriteString(fmt.Sprintf("- 24h Change: %.2f%%\n", req.Context.PriceChange24h))
	sb.WriteString(fmt.Sprintf("- 24h Volume: %.2f\n", req.Context.Volume24h))
	sb.WriteString(fmt.Sprintf("- Recent Range: %.2f - %.2f\n", req.Context.RecentLow, req.Context.RecentHigh))
	sb.WriteString(fmt.Sprintf("- Trend: %s\n", req.Context.Trend))
	sb.WriteString(fmt.Sprintf("- Volatility: %.4f\n", req.Context.Volatility))
	sb.WriteString(fmt.Sprintf("- RSI: %.1f\n", req.Context.RSI))
	sb.WriteString(fmt.Sprintf("- MACD Signal: %s\n", req.Context.MACDSignal))
	if len(req.Context.SupportLevels) > 0 {
		sb.WriteString(fmt.Sprintf("- Supports: %v\n", req.Context.SupportLevels))
	}
	if len(req.Context.ResistanceLevels) > 0 {
		sb.WriteString(fmt.Sprintf("- Resistances: %v\n", req.Context.ResistanceLevels))
	}

	sb.WriteString(fmt.Sprintf("\nRisk Level: %s\n", req.RiskLevel))
	if len(req.Constraints) > 0 {
		sb.WriteString(fmt.Sprintf("Constraints: %s\n", strings.Join(req.Constraints, ", ")))
	}

	sb.WriteString("\nGenerate the strategy now.")
	return sb.String()
}

// ── Code Validation ──

var forbiddenPatterns = []string{
	`os\.Exec`, `os\.Open`, `os\.Create`, `exec\.Command`,
	`net\.(Dial|Listen|HTTP)`,
	`http\.(Get|Post|Do)`,
	`(ReadFile|WriteFile)`,
	`(Open|Create)\s*\(`,
	`(Remove|RemoveAll)\s*\(`,
	`runtime\.(GOOS|GOARCH)`,
	`unsafe\.`,
	`reflect\.(ValueOf|TypeOf)`,
	`syscall\.`,
	`(?i)(drop\s+table|delete\s+from|truncate)`,
	`(?i)(eval|exec|spawn)\s*\(`,
	`import\s+"[^"]*/(net|os|syscall|unsafe|reflect|runtime)"`,
}

func validateGeneratedCode(code string) []string {
	var warnings []string
	if code == "" {
		return []string{"empty code"}
	}

	for _, pattern := range forbiddenPatterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(code) {
			warnings = append(warnings, fmt.Sprintf("forbidden pattern detected: %s", pattern))
		}
	}

	// Check for required elements
	if !strings.Contains(code, "OnBar(") {
		warnings = append(warnings, "missing OnBar handler")
	}
	if !strings.Contains(code, "Stop()") {
		warnings = append(warnings, "missing Stop method")
	}
	if !strings.Contains(code, "stopLoss") && !strings.Contains(code, "stop_loss") {
		warnings = append(warnings, "no stop loss defined")
	}

	return warnings
}

// ── Response Parsing ──

func extractCodeBlock(response string) string {
	// Try Go code block first
	for _, marker := range []string{"```go", "```"} {
		start := strings.Index(response, marker)
		if start >= 0 {
			start += len(marker)
			if idx := strings.Index(response[start:], "```"); idx >= 0 {
				return strings.TrimSpace(response[start : start+idx])
			}
		}
	}
	return ""
}

func extractJSONBlock(response string) string {
	start := strings.Index(response, "```json")
	if start >= 0 {
		start += len("```json")
		if idx := strings.Index(response[start:], "```"); idx >= 0 {
			return strings.TrimSpace(response[start : start+idx])
		}
	}
	return ""
}
