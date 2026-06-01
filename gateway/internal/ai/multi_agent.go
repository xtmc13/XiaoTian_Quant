package ai

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// ── Agent ──

// Agent is a specialized AI agent in the multi-agent pipeline.
type Agent struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Provider string `json:"provider"`
	SystemPrompt string `json:"-"`
}

// ── Agent Result ──

// AgentResult is the output from a single agent.
type AgentResult struct {
	Agent     string  `json:"agent"`
	Content   string  `json:"content"`
	Confidence float64 `json:"confidence"`
	DurationMs int64   `json:"duration_ms"`
}

// ── Multi-Agent Pipeline ──

// Pipeline orchestrates a 7-agent AI trading analysis pipeline.
//
// Phase 1 (parallel):
//   - Technical Analyst: price action, indicators, patterns
//   - On-Chain Analyst: blockchain metrics, flows
//   - Sentiment Analyst: news, social media sentiment
//   - Risk Analyst: volatility, VaR, drawdown risk
//
// Phase 2 (debate):
//   - Bull Advocate: argues for long position
//   - Bear Advocate: argues for short position
//
// Phase 3 (decision):
//   - Trader: synthesizes all inputs, generates strategy code
type Pipeline struct {
	agents       map[string]*Agent
	enableCache  bool
	maxRetries   int
	OnProgress   func(phase string, agent string)
}

func NewPipeline() *Pipeline {
	p := &Pipeline{
		agents:      make(map[string]*Agent),
		maxRetries:  3,
	}

	// Phase 1 - Parallel analysis agents
	p.Register(Agent{
		Name:   "technical_analyst",
		Role:   "You are a technical analysis expert. Analyze price action, patterns, and indicators to determine market direction. Provide specific entry/exit levels.",
		Provider: "deepseek",
	})
	p.Register(Agent{
		Name:   "onchain_analyst",
		Role:   "You are a blockchain data analyst. Analyze on-chain metrics, wallet flows, staking data, and network activity.",
		Provider: "deepseek",
	})
	p.Register(Agent{
		Name:   "sentiment_analyst",
		Role:   "You are a market sentiment analyst. Analyze market sentiment from news, social media, and fear/greed indicators.",
		Provider: "deepseek",
	})
	p.Register(Agent{
		Name:   "risk_analyst",
		Role:   "You are a risk management expert. Evaluate volatility, VaR, drawdown risk, and recommend position sizing.",
		Provider: "deepseek",
	})

	// Phase 2 - Debate agents
	p.Register(Agent{
		Name:   "bull_advocate",
		Role:   "You are the bullish trader. Make the strongest possible case for going LONG. Challenge bearish assumptions. Find every reason prices could rise.",
		Provider: "deepseek",
	})
	p.Register(Agent{
		Name:   "bear_advocate",
		Role:   "You are the bearish trader. Make the strongest possible case for going SHORT. Challenge bullish assumptions. Find every reason prices could fall.",
		Provider: "deepseek",
	})

	// Phase 3 - Decision maker
	p.Register(Agent{
		Name:   "trader",
		Role:   "You are the head trader. Synthesize all analysts' and debaters' inputs. Make the final trading decision and generate strategy code. If no clear edge exists, recommend staying out.",
		Provider: "deepseek",
	})

	return p
}

// Register adds an agent to the pipeline.
func (p *Pipeline) Register(a Agent) {
	p.agents[a.Name] = &a
}

// ── Market Input ──

// MarketInput is the data fed into the pipeline.
type MarketInput struct {
	Symbol       string  `json:"symbol"`
	CurrentPrice float64 `json:"current_price"`
	Change24h    float64 `json:"change_24h"`
	Volume24h    float64 `json:"volume_24h"`
	High24h      float64 `json:"high_24h"`
	Low24h       float64 `json:"low_24h"`
	RSI          float64 `json:"rsi"`
	MACD         string  `json:"macd"`
	Volatility   float64 `json:"volatility"`
	FundingRate  float64 `json:"funding_rate"`
	OpenInterest float64 `json:"open_interest"`

	// On-chain (optional)
	NetFlows    float64 `json:"net_flows"`
	ActiveAddrs int64   `json:"active_addresses"`
	TVL         float64 `json:"tvl"`

	// Sentiment
	FearGreedIndex int    `json:"fear_greed_index"`
	NewsHeadlines  string `json:"news_headlines"`
}

// BuildInputPrompt converts market data to a string for agent consumption.
func BuildInputPrompt(input MarketInput) string {
	return fmt.Sprintf(`Market Data:
Symbol: %s
Current Price: %.4f
24h Change: %.2f%%
24h Volume: %.2f
24h Range: %.4f - %.4f
RSI (14): %.1f
MACD: %s
Volatility (24h): %.4f%%
Funding Rate: %.4f%%
Open Interest: %.2f

On-Chain:
Net Flows (24h): %.2f
Active Addresses: %d
TVL: %.2f

Sentiment:
Fear & Greed Index: %d
News: %s`,
		input.Symbol, input.CurrentPrice, input.Change24h, input.Volume24h,
		input.Low24h, input.High24h, input.RSI, input.MACD, input.Volatility,
		input.FundingRate, input.OpenInterest,
		input.NetFlows, input.ActiveAddrs, input.TVL,
		input.FearGreedIndex, input.NewsHeadlines,
	)
}

// ── Pipeline Execution ──

// Decision is the final output of the multi-agent pipeline.
type Decision struct {
	Direction    string          `json:"direction"`     // LONG, SHORT, NONE
	Confidence   float64         `json:"confidence"`
	EntryPrice   float64         `json:"entry_price"`
	StopLoss     float64         `json:"stop_loss"`
	TakeProfit   float64         `json:"take_profit"`
	PositionSize float64         `json:"position_size"`
	Reason       string          `json:"reason"`
	StrategyCode string          `json:"strategy_code"`
	Results      []AgentResult   `json:"agent_results"`
	DebateResult []AgentResult   `json:"debate_results"`
	Consensus    float64         `json:"consensus"`
	HasConsensus bool            `json:"has_consensus"`
}

// Run executes the full multi-agent pipeline.
func (p *Pipeline) Run(input MarketInput) (*Decision, error) {
	marketPrompt := BuildInputPrompt(input)

	// ── Phase 1: Parallel Analysis ──
	p.reportProgress("Phase 1: Analysis", "parallel")
	phase1 := []string{"technical_analyst", "onchain_analyst", "sentiment_analyst", "risk_analyst"}
	phase1Results := p.runParallel(phase1, marketPrompt)

	// ── Phase 2: Debate ──
	p.reportProgress("Phase 2: Debate", "bull_advocate")
	debatePrompt := buildDebatePrompt(marketPrompt, phase1Results)
	phase2Results := p.runParallel([]string{"bull_advocate", "bear_advocate"}, debatePrompt)

	// ── Phase 3: Decision ──
	p.reportProgress("Phase 3: Decision", "trader")
	decisionPrompt := buildDecisionPrompt(marketPrompt, phase1Results, phase2Results)
	traderResult := p.runAgent("trader", decisionPrompt)

	// ── Synthesize ──
	decision := p.synthesize(input, phase1Results, phase2Results, *traderResult)
	decision.Results = phase1Results
	decision.DebateResult = phase2Results

	return decision, nil
}

func (p *Pipeline) runParallel(agentNames []string, prompt string) []AgentResult {
	var wg sync.WaitGroup
	results := make([]AgentResult, len(agentNames))
	var mu sync.Mutex

	for i, name := range agentNames {
		wg.Add(1)
		go func(idx int, agentName string) {
			defer wg.Done()
			result := p.runAgent(agentName, prompt)
			mu.Lock()
			results[idx] = *result
			mu.Unlock()
		}(i, name)
	}
	wg.Wait()
	return results
}

func (p *Pipeline) runAgent(name, prompt string) *AgentResult {
	agent, ok := p.agents[name]
	if !ok {
		return &AgentResult{Agent: name, Content: "agent not found", Confidence: 0}
	}

	provider := GetProvider(agent.Provider)
	if provider == nil {
		return &AgentResult{Agent: name, Content: "provider not found", Confidence: 0}
	}

	start := time.Now()

	resp, err := provider.ChatCompletion(CompletionRequest{
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: agent.Role},
			{Role: RoleUser, Content: prompt},
		},
		MaxTokens:   2048,
		Temperature: 0.4,
	})
	if err != nil {
		return &AgentResult{Agent: name, Content: fmt.Sprintf("error: %v", err), Confidence: 0}
	}

	content := ""
	confidence := 0.5
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}

	// Estimate confidence from response
	contentLower := strings.ToLower(content)
	if strings.Contains(contentLower, "confident") || strings.Contains(contentLower, "strong signal") {
		confidence = 0.8
	} else if strings.Contains(contentLower, "uncertain") || strings.Contains(contentLower, "mixed") {
		confidence = 0.3
	}

	return &AgentResult{
		Agent:      name,
		Content:    content,
		Confidence: confidence,
		DurationMs: time.Since(start).Milliseconds(),
	}
}

// ── Prompt Builders ──

func buildDebatePrompt(marketPrompt string, phase1 []AgentResult) string {
	var sb strings.Builder
	sb.WriteString("Market Context:\n")
	sb.WriteString(marketPrompt)
	sb.WriteString("\n\nAnalysis from other agents:\n")
	for _, r := range phase1 {
		sb.WriteString(fmt.Sprintf("\n--- %s (confidence: %.1f) ---\n%s\n", r.Agent, r.Confidence, truncate(r.Content, 500)))
	}
	return sb.String()
}

func buildDecisionPrompt(marketPrompt string, phase1, phase2 []AgentResult) string {
	var sb strings.Builder
	sb.WriteString("Market Context:\n")
	sb.WriteString(marketPrompt)
	sb.WriteString("\n\nAll Analysis:\n")

	sb.WriteString("\n=== Phase 1: Analysis ===\n")
	for _, r := range phase1 {
		sb.WriteString(fmt.Sprintf("\n--- %s (confidence: %.1f) ---\n%s\n", r.Agent, r.Confidence, truncate(r.Content, 400)))
	}

	sb.WriteString("\n=== Phase 2: Debate ===\n")
	for _, r := range phase2 {
		sb.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", r.Agent, truncate(r.Content, 400)))
	}

	sb.WriteString("\nMake your final trading decision. Output format:")
	sb.WriteString("\nDirection: LONG | SHORT | NONE")
	sb.WriteString("\nEntry: <price>")
	sb.WriteString("\nStop Loss: <price>")
	sb.WriteString("\nTake Profit: <price>")
	sb.WriteString("\nPosition Size: <% of equity>")
	sb.WriteString("\nReason: <explanation>")
	sb.WriteString("\nThen provide a Go strategy code block: ```go ... ```")

	return sb.String()
}

// ── Synthesis ──

func (p *Pipeline) synthesize(input MarketInput, phase1, phase2 []AgentResult, trader AgentResult) *Decision {
	decision := &Decision{
		Direction:  "NONE",
		Confidence: 0,
		Reason:     trader.Content,
	}

	// Determine direction from bullish vs bearish
	bullScore, bearScore := 0.0, 0.0
	for _, r := range phase1 {
		lower := strings.ToLower(r.Content)
		if strings.Contains(lower, "bullish") || strings.Contains(lower, "long") || strings.Contains(lower, "upward") {
			bullScore += r.Confidence
		}
		if strings.Contains(lower, "bearish") || strings.Contains(lower, "short") || strings.Contains(lower, "downward") {
			bearScore += r.Confidence
		}
	}

	// Debate scores
	for _, r := range phase2 {
		if r.Agent == "bull_advocate" {
			bullScore += r.Confidence
		} else {
			bearScore += r.Confidence
		}
	}

	// Determine consensus
	total := bullScore + bearScore
	if total > 0 {
		decision.Consensus = math.Abs(bullScore-bearScore) / total
		decision.HasConsensus = decision.Consensus > 0.3
		if bullScore > bearScore && decision.HasConsensus {
			decision.Direction = "LONG"
		} else if bearScore > bullScore && decision.HasConsensus {
			decision.Direction = "SHORT"
		}
	}

	// Override from trader if present
	lower := strings.ToLower(trader.Content)
	if strings.Contains(lower, "direction: long") {
		decision.Direction = "LONG"
	} else if strings.Contains(lower, "direction: short") {
		decision.Direction = "SHORT"
	} else if strings.Contains(lower, "direction: none") {
		decision.Direction = "NONE"
	}

	// Extract strategy code
	decision.StrategyCode = extractCodeBlock(trader.Content)
	decision.Confidence = (bullScore + bearScore) / float64(len(phase1)+len(phase2))

	// Default risk parameters
	if decision.Direction != "NONE" {
		decision.EntryPrice = input.CurrentPrice
		stopDist := input.CurrentPrice * input.Volatility / 100 * 2
		if decision.Direction == "LONG" {
			decision.StopLoss = input.CurrentPrice - stopDist
			decision.TakeProfit = input.CurrentPrice + stopDist*2.5
		} else {
			decision.StopLoss = input.CurrentPrice + stopDist
			decision.TakeProfit = input.CurrentPrice - stopDist*2.5
		}
		decision.PositionSize = input.CurrentPrice * 0.02
	}

	return decision
}

// ── Multi-Model Analysis ──

// MultiModel runs the same prompt across multiple providers and returns consensus.
func MultiModel(prompt string, providers []string) ([]AgentResult, float64, error) {
	var wg sync.WaitGroup
	results := make([]AgentResult, len(providers))
	var mu sync.Mutex

	for i, provName := range providers {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			prov := GetProvider(name)
			if prov == nil {
				mu.Lock()
				results[idx] = AgentResult{Agent: name, Content: "unavailable", Confidence: 0}
				mu.Unlock()
				return
			}
			start := time.Now()
			resp, err := prov.ChatCompletion(CompletionRequest{
				Messages: []ChatMessage{
					{Role: RoleUser, Content: prompt},
				},
				MaxTokens:   1024,
				Temperature: 0.3,
			})
			content := ""
			confidence := 0.5
			if err == nil && len(resp.Choices) > 0 {
				content = resp.Choices[0].Message.Content
			}

			// Vote counting
			lower := strings.ToLower(content)
			if strings.Contains(lower, "long") || strings.Contains(lower, "bullish") {
				confidence = 0.7
			} else if strings.Contains(lower, "short") || strings.Contains(lower, "bearish") {
				confidence = 0.3
			}

			mu.Lock()
			results[idx] = AgentResult{
				Agent: name, Content: content, Confidence: confidence,
				DurationMs: time.Since(start).Milliseconds(),
			}
			mu.Unlock()
		}(i, provName)
	}
	wg.Wait()

	// Compute consensus
	var totalConfidence float64
	longVotes := 0
	for _, r := range results {
		totalConfidence += r.Confidence
		if r.Confidence > 0.5 {
			longVotes++
		}
	}

	consensus := float64(longVotes) / float64(len(results))
	return results, consensus, nil
}

// ── Helpers ──

func (p *Pipeline) reportProgress(phase, agent string) {
	if p.OnProgress != nil {
		p.OnProgress(phase, agent)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// MultiModelAnalyze runs MultiModel and checks for divergence.
func MultiModelAnalyze(prompt string) (map[string]string, float64, bool) {
	providerList := ListProviders()
	if len(providerList) == 0 {
		return nil, 0, false
	}

	// Limit to available providers with keys
	var available []string
	for _, name := range providerList {
		prov := GetProvider(name)
		if prov != nil && prov.APIKey != "" {
			available = append(available, name)
		}
	}

	if len(available) == 0 {
		return nil, 0, false
	}
	if len(available) > 3 {
		available = available[:3]
	}

	results, consensus, err := MultiModel(prompt, available)
	if err != nil {
		return nil, 0, false
	}

	output := make(map[string]string)
	for _, r := range results {
		output[r.Agent] = r.Content
	}

	// Divergence: if consensus is near 0.5, models disagree
	divergent := math.Abs(consensus-0.5) < 0.3
	return output, consensus, divergent
}
