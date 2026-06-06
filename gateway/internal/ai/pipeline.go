package ai

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// ── Strategy Generation Pipeline ───────────────────────────────

// PipelineResult holds the outcome of a full generate-backtest-optimize cycle.
type PipelineResult struct {
	Success       bool              `json:"success"`
	Iterations    int               `json:"iterations"`
	BestScore     float64           `json:"best_score"`
	FinalParams   map[string]any    `json:"final_params"`
	BacktestResult *BacktestSummary `json:"backtest_result,omitempty"`
	History       []IterationRecord  `json:"history"`
	Error         string            `json:"error,omitempty"`
}

// BacktestSummary is a simplified backtest result for the pipeline.
type BacktestSummary struct {
	TotalReturnPct float64 `json:"total_return_pct"`
	SharpeRatio    float64 `json:"sharpe_ratio"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	WinRate        float64 `json:"win_rate"`
	TotalTrades    int     `json:"total_trades"`
	ProfitFactor   float64 `json:"profit_factor"`
}

// IterationRecord tracks one generate-backtest cycle.
type IterationRecord struct {
	Iteration   int              `json:"iteration"`
	Params      map[string]any   `json:"params"`
	Backtest    *BacktestSummary `json:"backtest"`
	Score       float64          `json:"score"`
	Improvement float64          `json:"improvement"`
	DurationMs  int64            `json:"duration_ms"`
}

// StrategyOptimizerConfig configures the optimization pipeline.
type StrategyOptimizerConfig struct {
	MaxIterations   int     `json:"max_iterations"`    // default 5
	TargetScore     float64 `json:"target_score"`      // stop if score >= this
	MinImprovement  float64 `json:"min_improvement"`   // stop if improvement < this
	StrategyType    string  `json:"strategy_type"`
	Symbol          string  `json:"symbol"`
	Interval        string  `json:"interval"`
	InitialBalance  float64 `json:"initial_balance"`
}

func DefaultStrategyOptimizerConfig() StrategyOptimizerConfig {
	return StrategyOptimizerConfig{
		MaxIterations:  5,
		TargetScore:    80.0,
		MinImprovement: 2.0,
		StrategyType:   "grid",
		Symbol:         "BTCUSDT",
		Interval:       "1h",
		InitialBalance: 100000,
	}
}

// StrategyOptimizer runs an iterative generate-backtest-score-optimize loop.
type StrategyOptimizer struct {
	cfg       StrategyOptimizerConfig
	llmClient LLMClient
	backtest  BacktestRunner
}

// LLMClient abstracts the AI provider for strategy generation.
type LLMClient interface {
	GenerateStrategy(ctx context.Context, prompt string, currentParams map[string]any, feedback string) (map[string]any, string, error)
}

// BacktestRunner abstracts the backtest engine.
type BacktestRunner interface {
	Run(ctx context.Context, strategyType string, params map[string]any, symbol, interval string, balance float64) (*BacktestSummary, error)
}

// NewStrategyOptimizer creates an optimizer.
func NewStrategyOptimizer(cfg StrategyOptimizerConfig, llm LLMClient, bt BacktestRunner) *StrategyOptimizer {
	return &StrategyOptimizer{
		cfg:       cfg,
		llmClient: llm,
		backtest:  bt,
	}
}

// Run executes the full optimization pipeline.
func (so *StrategyOptimizer) Run(ctx context.Context, initialParams map[string]any) (*PipelineResult, error) {
	result := &PipelineResult{
		Success:    true,
		History:    make([]IterationRecord, 0, so.cfg.MaxIterations),
		FinalParams: make(map[string]any),
	}

	params := make(map[string]any)
	for k, v := range initialParams {
		params[k] = v
	}

	var bestScore float64
	var bestParams map[string]any
	var bestBacktest *BacktestSummary

	for i := 0; i < so.cfg.MaxIterations; i++ {
		start := time.Now()

		// Build feedback from previous iterations
		feedback := so.buildFeedback(i, result.History)

		// 1. Generate / optimize parameters via LLM
		newParams, explanation, err := so.llmClient.GenerateStrategy(ctx, so.cfg.StrategyType, params, feedback)
		if err != nil {
			result.Error = fmt.Sprintf("iteration %d: LLM error: %v", i+1, err)
			break
		}
		_ = explanation

		// Merge with current params
		for k, v := range newParams {
			params[k] = v
		}

		// 2. Run backtest
		btResult, err := so.backtest.Run(ctx, so.cfg.StrategyType, params, so.cfg.Symbol, so.cfg.Interval, so.cfg.InitialBalance)
		if err != nil {
			result.Error = fmt.Sprintf("iteration %d: backtest error: %v", i+1, err)
			break
		}

		// 3. Score the result
		score := so.scoreBacktest(btResult)

		// 4. Record
		improvement := 0.0
		if i > 0 && len(result.History) > 0 {
			improvement = score - result.History[len(result.History)-1].Score
		}

		record := IterationRecord{
			Iteration:   i + 1,
			Params:      copyMap(params),
			Backtest:    btResult,
			Score:       score,
			Improvement: improvement,
			DurationMs:  time.Since(start).Milliseconds(),
		}
		result.History = append(result.History, record)
		result.Iterations = i + 1

		// Track best
		if score > bestScore {
			bestScore = score
			bestParams = copyMap(params)
			bestBacktest = btResult
		}

		// Check stopping conditions
		if score >= so.cfg.TargetScore {
			break
		}
		if i > 0 && math.Abs(improvement) < so.cfg.MinImprovement {
			break
		}
	}

	result.BestScore = bestScore
	if bestParams != nil {
		result.FinalParams = bestParams
	}
	if bestBacktest != nil {
		result.BacktestResult = bestBacktest
	}

	return result, nil
}

// buildFeedback creates a text summary of previous iterations for the LLM.
func (so *StrategyOptimizer) buildFeedback(iteration int, history []IterationRecord) string {
	if iteration == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Previous iteration results:\n")
	for _, h := range history {
		sb.WriteString(fmt.Sprintf("- Iteration %d: score=%.1f, return=%.1f%%, sharpe=%.2f, drawdown=%.1f%%, trades=%d\n",
			h.Iteration, h.Score, h.Backtest.TotalReturnPct, h.Backtest.SharpeRatio, h.Backtest.MaxDrawdownPct, h.Backtest.TotalTrades))
	}
	sb.WriteString("\nPlease improve the strategy parameters to achieve a higher score. Focus on improving Sharpe ratio and reducing drawdown.")
	return sb.String()
}

// scoreBacktest computes a composite score from backtest metrics.
// Score = 30*return_score + 30*sharpe_score + 20*winrate_score + 20*drawdown_score
// Each component is normalized to 0-100.
func (so *StrategyOptimizer) scoreBacktest(bt *BacktestSummary) float64 {
	if bt == nil {
		return 0
	}

	// Return score: 0-100% return = 0-50 points, >100% = 50 + log scale
	returnScore := bt.TotalReturnPct
	if returnScore > 100 {
		returnScore = 50 + math.Log10(returnScore)*10
	}
	if returnScore > 100 {
		returnScore = 100
	}
	if returnScore < 0 {
		returnScore = 0
	}

	// Sharpe score: 0-3 = 0-100
	sharpeScore := bt.SharpeRatio / 3.0 * 100
	if sharpeScore > 100 {
		sharpeScore = 100
	}
	if sharpeScore < 0 {
		sharpeScore = 0
	}

	// Win rate score: 0-100% = 0-100
	winRateScore := bt.WinRate
	if winRateScore > 100 {
		winRateScore = 100
	}

	// Drawdown score: 0% drawdown = 100, 50% drawdown = 0
	drawdownScore := (50.0 - bt.MaxDrawdownPct) / 50.0 * 100
	if drawdownScore > 100 {
		drawdownScore = 100
	}
	if drawdownScore < 0 {
		drawdownScore = 0
	}

	// Weighted composite
	score := 0.30*returnScore + 0.30*sharpeScore + 0.20*winRateScore + 0.20*drawdownScore
	return math.Round(score*10) / 10
}

func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// ── Mock Implementations (for testing) ─────────────────────────

// MockLLMClient is a test double for LLMClient.
type MockLLMClient struct {
	Responses []map[string]any
	Index     int
}

func (m *MockLLMClient) GenerateStrategy(ctx context.Context, prompt string, currentParams map[string]any, feedback string) (map[string]any, string, error) {
	if m.Index >= len(m.Responses) {
		return map[string]any{}, "mock", nil
	}
	resp := m.Responses[m.Index]
	m.Index++
	return resp, "mock explanation", nil
}

// MockBacktestRunner is a test double for BacktestRunner.
type MockBacktestRunner struct {
	Results []*BacktestSummary
	Index   int
}

func (m *MockBacktestRunner) Run(ctx context.Context, strategyType string, params map[string]any, symbol, interval string, balance float64) (*BacktestSummary, error) {
	if m.Index >= len(m.Results) {
		return &BacktestSummary{TotalReturnPct: 10, SharpeRatio: 1.0, MaxDrawdownPct: 5, WinRate: 55, TotalTrades: 20, ProfitFactor: 1.5}, nil
	}
	resp := m.Results[m.Index]
	m.Index++
	return resp, nil
}
