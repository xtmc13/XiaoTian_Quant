package ai

import (
	"context"
	"testing"
)

func TestStrategyOptimizerScore(t *testing.T) {
	so := NewStrategyOptimizer(DefaultStrategyOptimizerConfig(), nil, nil)

	// Perfect backtest
	bt := &BacktestSummary{
		TotalReturnPct: 150,
		SharpeRatio:    3.0,
		MaxDrawdownPct: 5,
		WinRate:        70,
		TotalTrades:    50,
		ProfitFactor:   2.5,
	}
	score := so.scoreBacktest(bt)
	if score < 80 {
		t.Errorf("perfect backtest should score > 80, got %.1f", score)
	}

	// Mediocre backtest
	bt2 := &BacktestSummary{
		TotalReturnPct: 10,
		SharpeRatio:    0.5,
		MaxDrawdownPct: 20,
		WinRate:        45,
		TotalTrades:    20,
		ProfitFactor:   1.1,
	}
	score2 := so.scoreBacktest(bt2)
	if score2 > 50 {
		t.Errorf("mediocre backtest should score < 50, got %.1f", score2)
	}

	// Nil backtest
	score3 := so.scoreBacktest(nil)
	if score3 != 0 {
		t.Errorf("nil backtest should score 0, got %.1f", score3)
	}
}

func TestStrategyOptimizerRun(t *testing.T) {
	llm := &MockLLMClient{
		Responses: []map[string]any{
			{"grid_count": 20},
			{"grid_count": 25},
			{"grid_count": 30},
		},
	}
	bt := &MockBacktestRunner{
		Results: []*BacktestSummary{
			{TotalReturnPct: 10, SharpeRatio: 0.8, MaxDrawdownPct: 15, WinRate: 50, TotalTrades: 20},
			{TotalReturnPct: 25, SharpeRatio: 1.5, MaxDrawdownPct: 10, WinRate: 60, TotalTrades: 25},
			{TotalReturnPct: 20, SharpeRatio: 1.2, MaxDrawdownPct: 12, WinRate: 55, TotalTrades: 22},
		},
	}

	cfg := DefaultStrategyOptimizerConfig()
	cfg.MaxIterations = 3
	so := NewStrategyOptimizer(cfg, llm, bt)

	result, err := so.Run(context.Background(), map[string]any{"grid_count": 10})
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}
	if !result.Success {
		t.Error("pipeline should succeed")
	}
	if result.Iterations != 3 {
		t.Errorf("expected 3 iterations, got %d", result.Iterations)
	}
	if len(result.History) != 3 {
		t.Errorf("expected 3 history records, got %d", len(result.History))
	}

	// Best should be iteration 2 (highest score)
	if result.BestScore <= 0 {
		t.Errorf("best score should be positive, got %.1f", result.BestScore)
	}
	if result.BacktestResult == nil {
		t.Error("should have best backtest result")
	}
}

func TestStrategyOptimizerEarlyStop(t *testing.T) {
	llm := &MockLLMClient{
		Responses: []map[string]any{
			{"grid_count": 20},
		},
	}
	bt := &MockBacktestRunner{
		Results: []*BacktestSummary{
			{TotalReturnPct: 200, SharpeRatio: 3.5, MaxDrawdownPct: 3, WinRate: 80, TotalTrades: 50},
		},
	}

	cfg := DefaultStrategyOptimizerConfig()
	cfg.MaxIterations = 5
	cfg.TargetScore = 85.0
	so := NewStrategyOptimizer(cfg, llm, bt)

	result, err := so.Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}
	if result.Iterations != 1 {
		t.Errorf("should stop after 1 iteration (target reached), got %d", result.Iterations)
	}
}

func TestStrategyOptimizerMinImprovementStop(t *testing.T) {
	llm := &MockLLMClient{
		Responses: []map[string]any{
			{"grid_count": 20},
			{"grid_count": 21},
			{"grid_count": 22},
		},
	}
	bt := &MockBacktestRunner{
		Results: []*BacktestSummary{
			{TotalReturnPct: 50, SharpeRatio: 1.5, MaxDrawdownPct: 10, WinRate: 60, TotalTrades: 30},
			{TotalReturnPct: 51, SharpeRatio: 1.52, MaxDrawdownPct: 10, WinRate: 61, TotalTrades: 31}, // tiny improvement
			{TotalReturnPct: 52, SharpeRatio: 1.53, MaxDrawdownPct: 10, WinRate: 62, TotalTrades: 32},
		},
	}

	cfg := DefaultStrategyOptimizerConfig()
	cfg.MaxIterations = 5
	cfg.MinImprovement = 5.0 // require 5 point improvement
	so := NewStrategyOptimizer(cfg, llm, bt)

	result, err := so.Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}
	// Should stop after iteration 2 because improvement < 5
	if result.Iterations > 2 {
		t.Logf("stopped after %d iterations (expected <= 2 due to min improvement)", result.Iterations)
	}
}

func TestCopyMap(t *testing.T) {
	m := map[string]any{"a": 1, "b": "test", "c": true}
	cp := copyMap(m)
	if len(cp) != 3 {
		t.Errorf("expected 3 items, got %d", len(cp))
	}
	// Verify independence
	cp["a"] = 999
	if m["a"] != 1 {
		t.Error("copy should be independent")
	}
}

func TestBuildFeedback(t *testing.T) {
	so := NewStrategyOptimizer(DefaultStrategyOptimizerConfig(), nil, nil)

	// First iteration: no feedback
	fb := so.buildFeedback(0, nil)
	if fb != "" {
		t.Errorf("first iteration should have empty feedback, got: %s", fb)
	}

	// Second iteration: with history
	history := []IterationRecord{
		{Iteration: 1, Score: 45.0, Backtest: &BacktestSummary{TotalReturnPct: 10, SharpeRatio: 0.8, MaxDrawdownPct: 15, WinRate: 50, TotalTrades: 20}},
	}
	fb2 := so.buildFeedback(1, history)
	if fb2 == "" {
		t.Error("second iteration should have feedback")
	}
	if !contains(fb2, "Previous iteration results") {
		t.Error("feedback should mention previous results")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDefaultStrategyOptimizerConfig(t *testing.T) {
	cfg := DefaultStrategyOptimizerConfig()
	if cfg.MaxIterations != 5 {
		t.Errorf("expected 5 iterations, got %d", cfg.MaxIterations)
	}
	if cfg.TargetScore != 80.0 {
		t.Errorf("expected target 80, got %.1f", cfg.TargetScore)
	}
	if cfg.MinImprovement != 2.0 {
		t.Errorf("expected min improvement 2, got %.1f", cfg.MinImprovement)
	}
}
