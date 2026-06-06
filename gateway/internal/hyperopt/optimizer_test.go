package hyperopt

import (
	"math"
	"testing"
	"time"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func htAssert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func htAssertFloat(t *testing.T, got, want float64, msg string) {
	t.Helper()
	const eps = 0.001
	if got < want-eps || got > want+eps {
		t.Fatalf("%s: got %.6f, want %.6f", msg, got, want)
	}
}

func htAssertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

// mockEvaluator returns metrics based on parameters for deterministic testing.
func mockEvaluator(params map[string]any) (*BacktestMetrics, error) {
	lookback, _ := params["lookback"].(int)
	stopLoss, _ := params["stop_loss_pct"].(float64)

	// Simulate: larger lookback = more stable, tighter stoploss = more trades but worse win rate
	sharpe := 0.5 + float64(lookback)*0.05 - stopLoss*10.0
	totalReturn := float64(lookback)*2.0 - stopLoss*50.0
	drawdown := 5.0 + stopLoss*20.0 - float64(lookback)*0.5
	trades := int(10 + stopLoss*100)
	winRate := 60.0 - stopLoss*50.0 + float64(lookback)*2.0

	return &BacktestMetrics{
		TotalReturnPct: totalReturn,
		MaxDrawdownPct: drawdown,
		SharpeRatio:    sharpe,
		SortinoRatio:   sharpe * 1.2,
		CalmarRatio:    totalReturn / drawdown,
		WinRate:        winRate,
		ProfitFactor:   1.5,
		TotalTrades:    trades,
		AvgTradePct:    totalReturn / float64(trades),
	}, nil
}

/* ── ParamSpace Tests ────────────────────────────────────────── */

func TestExpandParamInt(t *testing.T) {
	o := NewGridOptimizer(DefaultOptimizerConfig(), nil, nil)
	space := ParamSpace{Name: "period", Type: ParamInt, Min: 10, Max: 20, Step: 5}
	vals := o.expandParam(space)
	htAssertEq(t, len(vals), 3, "3 values: 10, 15, 20")
	htAssertEq(t, vals[0].(int), 10, "first")
	htAssertEq(t, vals[2].(int), 20, "last")
}

func TestExpandParamFloat(t *testing.T) {
	o := NewGridOptimizer(DefaultOptimizerConfig(), nil, nil)
	space := ParamSpace{Name: "stop", Type: ParamFloat, Min: 0.01, Max: 0.05, Step: 0.02}
	vals := o.expandParam(space)
	htAssertEq(t, len(vals), 3, "3 values: 0.01, 0.03, 0.05")
}

func TestExpandParamCategorical(t *testing.T) {
	o := NewGridOptimizer(DefaultOptimizerConfig(), nil, nil)
	space := ParamSpace{Name: "mode", Type: ParamCategorical, Options: []string{"aggressive", "conservative"}}
	vals := o.expandParam(space)
	htAssertEq(t, len(vals), 2, "2 categories")
	htAssertEq(t, vals[0].(string), "aggressive", "first")
}

/* ── Grid Generation Tests ───────────────────────────────────── */

func TestGridSize(t *testing.T) {
	spaces := []ParamSpace{
		{Name: "fast", Type: ParamInt, Min: 5, Max: 15, Step: 5},     // 3 values
		{Name: "slow", Type: ParamInt, Min: 20, Max: 30, Step: 10},     // 2 values
		{Name: "stop", Type: ParamFloat, Min: 0.01, Max: 0.03, Step: 0.02}, // 2 values
	}
	o := NewGridOptimizer(DefaultOptimizerConfig(), spaces, mockEvaluator)
	htAssertEq(t, o.GridSize(), 12, "3*2*2 = 12 combinations")
}

func TestGridCartesianProduct(t *testing.T) {
	spaces := []ParamSpace{
		{Name: "a", Type: ParamInt, Min: 1, Max: 2, Step: 1},    // 1, 2
		{Name: "b", Type: ParamCategorical, Options: []string{"x", "y"}}, // x, y
	}
	o := NewGridOptimizer(DefaultOptimizerConfig(), spaces, mockEvaluator)
	htAssertEq(t, o.GridSize(), 4, "2*2 = 4")

	grid := o.generateGrid()
	htAssertEq(t, len(grid), 4, "4 combinations")

	// Verify all combinations exist
	seen := make(map[string]bool)
	for _, g := range grid {
		key := ""
		key += string(rune(g["a"].(int) + '0'))
		key += g["b"].(string)
		seen[key] = true
	}
	htAssert(t, seen["1x"], "1x present")
	htAssert(t, seen["1y"], "1y present")
	htAssert(t, seen["2x"], "2x present")
	htAssert(t, seen["2y"], "2y present")
}

/* ── Optimizer Run Tests ─────────────────────────────────────── */

func TestOptimizerRun(t *testing.T) {
	spaces := []ParamSpace{
		{Name: "lookback", Type: ParamInt, Min: 10, Max: 20, Step: 10},
		{Name: "stop_loss_pct", Type: ParamFloat, Min: 0.01, Max: 0.03, Step: 0.02},
	}
	cfg := DefaultOptimizerConfig()
	cfg.LossFunc = LossSharpe
	cfg.LossName = "sharpe"

	o := NewGridOptimizer(cfg, spaces, mockEvaluator)
	results, err := o.Run()
	htAssert(t, err == nil, "run should not error")
	htAssertEq(t, len(results), 4, "2*2=4 trials")

	// Results should be sorted by score ascending (best first)
	htAssert(t, results[0].Score <= results[1].Score, "sorted ascending")
}

func TestOptimizerBest(t *testing.T) {
	spaces := []ParamSpace{
		{Name: "lookback", Type: ParamInt, Min: 10, Max: 20, Step: 10},
		{Name: "stop_loss_pct", Type: ParamFloat, Min: 0.01, Max: 0.03, Step: 0.02},
	}
	cfg := DefaultOptimizerConfig()
	cfg.LossFunc = LossSharpe

	o := NewGridOptimizer(cfg, spaces, mockEvaluator)
	o.Run()

	best := o.Best()
	htAssert(t, best != nil, "best should exist")
	// Higher lookback + lower stoploss = better sharpe in our mock
	htAssertEq(t, best.Params["lookback"].(int), 20, "larger lookback wins")
}

func TestOptimizerMaxTrials(t *testing.T) {
	spaces := []ParamSpace{
		{Name: "p1", Type: ParamInt, Min: 1, Max: 10, Step: 1},  // 10 values
		{Name: "p2", Type: ParamInt, Min: 1, Max: 10, Step: 1},  // 10 values = 100 total
	}
	cfg := DefaultOptimizerConfig()
	cfg.MaxTrials = 5

	o := NewGridOptimizer(cfg, spaces, mockEvaluator)
	results, err := o.Run()
	htAssert(t, err == nil, "run should not error")
	htAssert(t, len(results) <= 5, "capped at max trials")
}

func TestOptimizerTimeout(t *testing.T) {
	spaces := []ParamSpace{
		{Name: "p1", Type: ParamInt, Min: 1, Max: 100, Step: 1}, // 100 trials, each slow
	}
	cfg := DefaultOptimizerConfig()
	cfg.Timeout = 1 * time.Millisecond
	cfg.Concurrent = 1

	// Very slow evaluator
	slowEval := func(params map[string]any) (*BacktestMetrics, error) {
		time.Sleep(500 * time.Millisecond)
		return mockEvaluator(params)
	}

	o := NewGridOptimizer(cfg, spaces, slowEval)
	results, err := o.Run()
	htAssert(t, err == nil, "run should timeout gracefully")
	// With 500ms per trial and 1ms timeout, most trials should be skipped
	htAssert(t, len(results) < 100, "timeout should prevent completing all trials")
}

/* ── Loss Function Tests ─────────────────────────────────────── */

func TestLossSharpe(t *testing.T) {
	m := &BacktestMetrics{SharpeRatio: 2.0, TotalTrades: 10}
	htAssertFloat(t, LossSharpe(m), -2.0, "sharpe loss = -sharpe")
}

func TestLossSharpeNoTrades(t *testing.T) {
	m := &BacktestMetrics{SharpeRatio: 2.0, TotalTrades: 0}
	htAssert(t, math.IsInf(LossSharpe(m), 1), "no trades = infinite loss")
}

func TestLossMaxDrawdown(t *testing.T) {
	m := &BacktestMetrics{MaxDrawdownPct: 15.0, TotalTrades: 5}
	htAssertFloat(t, LossMaxDrawdown(m), 15.0, "drawdown loss = drawdown itself")
}

func TestLossProfit(t *testing.T) {
	m := &BacktestMetrics{TotalReturnPct: 25.0, TotalTrades: 5}
	htAssertFloat(t, LossProfit(m), -25.0, "profit loss = -return")
}

func TestLossMultiMetric(t *testing.T) {
	m := &BacktestMetrics{TotalReturnPct: 20.0, MaxDrawdownPct: 5.0, TotalTrades: 5}
	// Score = -20 + 2*5 = -10
	htAssertFloat(t, LossMultiMetric(m), -10.0, "multi metric")
}

func TestLossProfitDrawdown(t *testing.T) {
	m := &BacktestMetrics{TotalReturnPct: 30.0, MaxDrawdownPct: 10.0, TotalTrades: 5}
	htAssertFloat(t, LossProfitDrawdown(m), -3.0, "-profit/drawdown = -3")
}

func TestLossProfitDrawdownZeroDrawdown(t *testing.T) {
	m := &BacktestMetrics{TotalReturnPct: 30.0, MaxDrawdownPct: 0, TotalTrades: 5}
	htAssert(t, math.IsInf(LossProfitDrawdown(m), 1), "zero drawdown = infinite loss")
}

func TestLossProfitFactor(t *testing.T) {
	m := &BacktestMetrics{GrossProfit: 2000, GrossLoss: -1000, TotalTrades: 50}
	// PF = 2000/1000 = 2.0, loss = -2.0
	htAssertFloat(t, LossProfitFactor(m), -2.0, "profit factor")
}

func TestLossProfitFactorFallback(t *testing.T) {
	// Without GrossProfit/GrossLoss, use ProfitFactor field directly
	m := &BacktestMetrics{ProfitFactor: 1.8, TotalTrades: 30, AvgTradePct: 0.5, WinRate: 55}
	htAssertFloat(t, LossProfitFactor(m), -1.8, "profit factor fallback")
}

func TestLossProfitFactorNoTrades(t *testing.T) {
	m := &BacktestMetrics{GrossProfit: 100, TotalTrades: 0}
	htAssert(t, math.IsInf(LossProfitFactor(m), 1), "no trades = inf")
}

func TestLossRiskReward(t *testing.T) {
	m := &BacktestMetrics{AvgWinPct: 2.5, AvgLossPct: -1.0, TotalTrades: 40}
	// R:R = 2.5/1.0 = 2.5, loss = -2.5
	htAssertFloat(t, LossRiskReward(m), -2.5, "risk reward")
}

func TestLossRiskRewardFallback(t *testing.T) {
	// Without AvgWin/AvgLoss, derive from ProfitFactor and WinRate
	// PF = 2.0, W = 0.5 => r = PF*(1-W)/W = 2.0*0.5/0.5 = 2.0
	m := &BacktestMetrics{ProfitFactor: 2.0, WinRate: 50, AvgTradePct: 0.5, TotalTrades: 30}
	htAssertFloat(t, LossRiskReward(m), -2.0, "risk reward fallback")
}

func TestLossSQN(t *testing.T) {
	m := &BacktestMetrics{TotalTrades: 100, AvgTradePct: 0.5, TradeStdDev: 1.0}
	// SQN = sqrt(100) * (0.5/1.0) = 10 * 0.5 = 5.0, loss = -5.0
	htAssertFloat(t, LossSQN(m), -5.0, "sqn")
}

func TestLossSQNFallback(t *testing.T) {
	// Without TradeStdDev, approximate from Sharpe
	m := &BacktestMetrics{TotalTrades: 64, AvgTradePct: 0.8, SharpeRatio: 2.0}
	// stdDev ≈ avgTrade / sharpe = 0.8/2.0 = 0.4
	// SQN = sqrt(64) * (0.8/0.4) = 8 * 2.0 = 16.0
	htAssertFloat(t, LossSQN(m), -16.0, "sqn fallback")
}

func TestLossSQNZeroStdDev(t *testing.T) {
	m := &BacktestMetrics{TotalTrades: 10, AvgTradePct: 0.5, TradeStdDev: 0, SharpeRatio: 0}
	htAssert(t, math.IsInf(LossSQN(m), 1), "zero stddev = inf")
}

func TestGetLossFunc(t *testing.T) {
	names := LossFuncNames()
	htAssert(t, len(names) == 16, "16 loss functions")

	for _, name := range names {
		fn := GetLossFunc(name)
		htAssert(t, fn != nil, "loss func "+name+" exists")
	}
}

func TestGetLossFuncUnknown(t *testing.T) {
	fn := GetLossFunc("unknown_func")
	htAssert(t, fn != nil, "unknown defaults to sharpe")
}

/* ── Edge Case Tests ─────────────────────────────────────────── */

func TestOptimizerNoEvaluator(t *testing.T) {
	o := NewGridOptimizer(DefaultOptimizerConfig(), []ParamSpace{
		{Name: "p", Type: ParamInt, Min: 1, Max: 2, Step: 1},
	}, nil)
	_, err := o.Run()
	htAssert(t, err != nil, "no evaluator should error")
}

func TestOptimizerNoSpaces(t *testing.T) {
	o := NewGridOptimizer(DefaultOptimizerConfig(), nil, mockEvaluator)
	_, err := o.Run()
	htAssert(t, err != nil, "no spaces should error")
}

func TestOptimizerEmptyGrid(t *testing.T) {
	spaces := []ParamSpace{
		{Name: "p", Type: ParamFloat, Min: 1.0, Max: 0.5, Step: 0.1}, // invalid range
	}
	o := NewGridOptimizer(DefaultOptimizerConfig(), spaces, mockEvaluator)
	_, err := o.Run()
	htAssert(t, err != nil, "empty grid should error")
}

func TestOptimizerNoResults(t *testing.T) {
	spaces := []ParamSpace{
		{Name: "p", Type: ParamInt, Min: 1, Max: 2, Step: 1},
	}
	cfg := DefaultOptimizerConfig()
	o := NewGridOptimizer(cfg, spaces, mockEvaluator)

	// Before Run(), Best() should return nil
	best := o.Best()
	htAssert(t, best == nil, "no best before run")
}

func TestParamTypeString(t *testing.T) {
	htAssert(t, ParamInt.String() == "INT", "int string")
	htAssert(t, ParamFloat.String() == "FLOAT", "float string")
	htAssert(t, ParamCategorical.String() == "CATEGORICAL", "categorical string")
}

/* ── Concurrent Tests ────────────────────────────────────────── */

func TestOptimizerConcurrent(t *testing.T) {
	spaces := []ParamSpace{
		{Name: "p1", Type: ParamInt, Min: 1, Max: 4, Step: 1}, // 4 values
		{Name: "p2", Type: ParamInt, Min: 1, Max: 3, Step: 1}, // 3 values = 12 total
	}
	cfg := DefaultOptimizerConfig()
	cfg.Concurrent = 3

	o := NewGridOptimizer(cfg, spaces, mockEvaluator)
	results, err := o.Run()
	htAssert(t, err == nil, "concurrent run should not error")
	htAssertEq(t, len(results), 12, "all 12 trials completed")
}

/* ── Benchmark ────────────────────────────────────────────────── */

func BenchmarkGridGeneration(b *testing.B) {
	spaces := []ParamSpace{
		{Name: "a", Type: ParamInt, Min: 1, Max: 10, Step: 1},
		{Name: "b", Type: ParamFloat, Min: 0.01, Max: 0.05, Step: 0.01},
		{Name: "c", Type: ParamCategorical, Options: []string{"x", "y", "z"}},
	}
	cfg := DefaultOptimizerConfig()
	o := NewGridOptimizer(cfg, spaces, mockEvaluator)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		o.generateGrid()
	}
}
