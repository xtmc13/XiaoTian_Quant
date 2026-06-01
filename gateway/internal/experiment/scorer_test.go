package experiment

import (
	"math"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/backtest"
)

func makeEquityCurve(values []float64) []backtest.EquityPoint {
	pts := make([]backtest.EquityPoint, len(values))
	for i, v := range values {
		pts[i] = backtest.EquityPoint{Equity: v}
	}
	return pts
}

func TestClampScore(t *testing.T) {
	if clampScore(-10) != 0 {
		t.Error("clampScore(-10) should be 0")
	}
	if clampScore(50) != 50 {
		t.Error("clampScore(50) should be 50")
	}
	if clampScore(150) != 100 {
		t.Error("clampScore(150) should be 100")
	}
}

func TestComputeStability(t *testing.T) {
	// Perfectly linear upward
	pts := make([]backtest.EquityPoint, 10)
	for i := range pts {
		pts[i] = backtest.EquityPoint{Equity: float64(i) * 100}
	}
	stability := computeStability(pts)
	if math.Abs(stability-1.0) > 1e-6 {
		t.Errorf("perfect linear stability = %v, want 1.0", stability)
	}

	// Downward trend should give 0
	downPts := make([]backtest.EquityPoint, 10)
	for i := range downPts {
		downPts[i] = backtest.EquityPoint{Equity: float64(10-i) * 100}
	}
	if computeStability(downPts) != 0 {
		t.Error("downward trend should give 0")
	}

	// Less than 3 points
	if computeStability([]backtest.EquityPoint{{}, {}}) != 0 {
		t.Error("<3 points should give 0")
	}
}

func TestScore_MinTradesFilter(t *testing.T) {
	cfg := DefaultScoreConfig()
	cfg.MinTrades = 10

	result := &backtest.RunResult{TotalTrades: 5}
	sr := Score(result, nil, cfg)
	if sr.TotalScore != 0 {
		t.Error("expected 0 score when below min trades")
	}
}

func TestScore_MinSharpeFilter(t *testing.T) {
	cfg := DefaultScoreConfig()
	cfg.MinSharpe = 0.5

	result := &backtest.RunResult{TotalTrades: 20, SharpeRatio: 0.3}
	sr := Score(result, nil, cfg)
	if sr.TotalScore != 0 {
		t.Error("expected 0 score when below min sharpe")
	}
}

func TestScore_MaxDrawdownFilter(t *testing.T) {
	cfg := DefaultScoreConfig()
	cfg.MaxDrawdown = 20

	result := &backtest.RunResult{TotalTrades: 20, SharpeRatio: 1.0, MaxDrawdownPct: 30}
	sr := Score(result, nil, cfg)
	if sr.TotalScore != 0 {
		t.Error("expected 0 score when above max drawdown")
	}
}

func TestScore_FullScoring(t *testing.T) {
	cfg := DefaultScoreConfig()
	result := &backtest.RunResult{
		TotalTrades:    50,
		SharpeRatio:    2.0,
		MaxDrawdownPct: 10,
		TotalReturnPct: 25,
		WinRate:        60,
		ProfitFactor:   2.5,
		EquityCurve:    makeEquityCurve([]float64{10000, 10200, 10100, 10500, 10800, 11000, 11200, 11500, 11800, 12000}),
	}

	sr := Score(result, nil, cfg)
	if sr.TotalScore <= 0 {
		t.Fatalf("expected positive total score, got %v", sr.TotalScore)
	}

	// Check factor scores exist
	expectedFactors := []string{"return", "sharpe", "drawdown", "win_rate", "profit_factor", "stability", "overfit"}
	for _, f := range expectedFactors {
		if _, ok := sr.FactorScores[f]; !ok {
			t.Errorf("missing factor score: %s", f)
		}
	}

	// Return: 25% * 2 = 50
	if math.Abs(sr.FactorScores["return"]-50) > 1 {
		t.Errorf("return score = %v, expected ~50", sr.FactorScores["return"])
	}
	// Sharpe: 2.0 * 25 = 50
	if math.Abs(sr.FactorScores["sharpe"]-50) > 1 {
		t.Errorf("sharpe score = %v, expected ~50", sr.FactorScores["sharpe"])
	}
	// Drawdown: 100 - 10*2 = 80
	if math.Abs(sr.FactorScores["drawdown"]-80) > 1 {
		t.Errorf("drawdown score = %v, expected ~80", sr.FactorScores["drawdown"])
	}
	// Win rate: 60
	if math.Abs(sr.FactorScores["win_rate"]-60) > 1 {
		t.Errorf("win_rate score = %v, expected ~60", sr.FactorScores["win_rate"])
	}
	// Profit factor: 2.5/3 * 100 = 83.33
	if math.Abs(sr.FactorScores["profit_factor"]-83.33) > 2 {
		t.Errorf("profit_factor score = %v, expected ~83", sr.FactorScores["profit_factor"])
	}
}

func TestScore_WithOOS(t *testing.T) {
	cfg := DefaultScoreConfig()
	cfg.OOSSharpeRatio = 0.5

	is := &backtest.RunResult{
		TotalTrades:    50,
		SharpeRatio:    2.0,
		MaxDrawdownPct: 10,
		EquityCurve:    makeEquityCurve([]float64{10000, 11000, 12000}),
	}
	oos := &backtest.RunResult{
		TotalTrades:    20,
		SharpeRatio:    1.5, // ratio = 0.75
		MaxDrawdownPct: 15,
		EquityCurve:    makeEquityCurve([]float64{12000, 13000, 14000}),
	}

	sr := Score(is, oos, cfg)
	if !sr.PassedOOS {
		t.Error("expected OOS to pass")
	}
	// Overfit = clamp(0.75 * 100) = 75
	if math.Abs(sr.FactorScores["overfit"]-75) > 1 {
		t.Errorf("overfit score = %v, expected ~75", sr.FactorScores["overfit"])
	}
}

func TestScore_OOSFail(t *testing.T) {
	cfg := DefaultScoreConfig()
	cfg.OOSSharpeRatio = 0.5

	is := &backtest.RunResult{
		TotalTrades:    50,
		SharpeRatio:    2.0,
		MaxDrawdownPct: 10,
		EquityCurve:    makeEquityCurve([]float64{10000, 11000, 12000}),
	}
	oos := &backtest.RunResult{
		TotalTrades:    20,
		SharpeRatio:    0.5, // ratio = 0.25 < 0.5
		MaxDrawdownPct: 15,
		EquityCurve:    makeEquityCurve([]float64{12000, 12500, 13000}),
	}

	sr := Score(is, oos, cfg)
	if sr.PassedOOS {
		t.Error("expected OOS to fail")
	}
	if sr.FactorScores["overfit"] >= 50 {
		t.Errorf("overfit score = %v, expected < 50", sr.FactorScores["overfit"])
	}
}

func TestScore_BothNegativeSharpe(t *testing.T) {
	cfg := DefaultScoreConfig()
	is := &backtest.RunResult{
		TotalTrades:    20,
		SharpeRatio:    -0.5,
		MaxDrawdownPct: 20,
		EquityCurve:    makeEquityCurve([]float64{10000, 9500, 9000}),
	}
	oos := &backtest.RunResult{
		TotalTrades:    20,
		SharpeRatio:    -0.3,
		MaxDrawdownPct: 15,
		EquityCurve:    makeEquityCurve([]float64{9000, 8700, 8500}),
	}

	sr := Score(is, oos, cfg)
	if !sr.PassedOOS {
		t.Error("expected OOS to pass when both negative")
	}
	if sr.FactorScores["overfit"] != 60 {
		t.Errorf("overfit = %v, want 60", sr.FactorScores["overfit"])
	}
}

func TestScore_NoOOS(t *testing.T) {
	cfg := DefaultScoreConfig()
	result := &backtest.RunResult{
		TotalTrades:    20,
		SharpeRatio:    1.0,
		MaxDrawdownPct: 15,
		EquityCurve:    makeEquityCurve([]float64{10000, 10500, 11000}),
	}

	sr := Score(result, nil, cfg)
	if sr.PassedOOS {
		t.Error("expected PassedOOS = false when no OOS")
	}
	if sr.FactorScores["overfit"] != 50 {
		t.Errorf("overfit = %v, want 50 (default when no OOS)", sr.FactorScores["overfit"])
	}
}

func TestDefaultScoreConfig(t *testing.T) {
	cfg := DefaultScoreConfig()
	if cfg.MinTrades != 10 {
		t.Errorf("MinTrades = %d", cfg.MinTrades)
	}
	if cfg.Weights == nil {
		t.Fatal("Weights should not be nil")
	}
	sum := 0.0
	for _, v := range cfg.Weights {
		sum += v
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("weights sum = %v, want 1.0", sum)
	}
}
