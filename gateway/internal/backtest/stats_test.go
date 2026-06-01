package backtest

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeConsecutive(t *testing.T) {
	trades := []Position{
		{RealizedPnL: 100},
		{RealizedPnL: 200},
		{RealizedPnL: -50},
		{RealizedPnL: -30},
		{RealizedPnL: -10},
		{RealizedPnL: 50},
	}
	maxWins, maxLoss := computeConsecutive(trades)
	if maxWins != 2 {
		t.Errorf("maxWins = %d, want 2", maxWins)
	}
	if maxLoss != 3 {
		t.Errorf("maxLoss = %d, want 3", maxLoss)
	}
}

func TestEquityToReturns(t *testing.T) {
	equity := []EquityPoint{
		{Equity: 100},
		{Equity: 110},
		{Equity: 105},
	}
	returns := equityToReturns(equity)
	if len(returns) != 2 {
		t.Fatalf("len(returns) = %d, want 2", len(returns))
	}
	if math.Abs(returns[0]-0.1) > 1e-9 {
		t.Errorf("returns[0] = %v, want 0.1", returns[0])
	}
	if math.Abs(returns[1]-(-0.045454545)) > 1e-6 {
		t.Errorf("returns[1] = %v, want ~-0.04545", returns[1])
	}
}

func TestEquityToReturns_Insufficient(t *testing.T) {
	if equityToReturns(nil) != nil {
		t.Error("expected nil for nil input")
	}
	if equityToReturns([]EquityPoint{{Equity: 100}}) != nil {
		t.Error("expected nil for single point")
	}
}

func TestValueAtRisk(t *testing.T) {
	returns := []float64{-0.05, -0.02, -0.01, 0.01, 0.02, 0.03, 0.04}
	var95 := valueAtRisk(returns, 0.95)
	// 95% VaR should be around the 5th percentile worst return
	if var95 <= 0 {
		t.Errorf("VaR95 = %v, expected positive (representing loss)", var95)
	}
}

func TestConditionalVaR(t *testing.T) {
	// Need enough points so that idx > 0 for 95% confidence
	returns := make([]float64, 100)
	for i := range returns {
		if i < 5 {
			returns[i] = -0.05 - float64(i)*0.01
		} else {
			returns[i] = float64(i) * 0.001
		}
	}
	cvar := conditionalVaR(returns, 0.95)
	if cvar <= 0 {
		t.Error("expected positive CVaR")
	}
	// CVaR should be >= VaR
	var95 := valueAtRisk(returns, 0.95)
	if cvar < var95 {
		t.Errorf("CVaR (%v) should be >= VaR (%v)", cvar, var95)
	}
}

func TestAnnualizedVolatility(t *testing.T) {
	returns := []float64{0.01, -0.01, 0.005, -0.005, 0.002}
	vol := annualizedVolatility(returns)
	if vol <= 0 {
		t.Error("expected positive volatility")
	}
}

func TestMonthlyReturns(t *testing.T) {
	// Each month needs at least 2 points for a return calculation
	equity := []EquityPoint{
		{Timestamp: 1704067200000, Equity: 100}, // 2024-01-01
		{Timestamp: 1706659200000, Equity: 110}, // 2024-01-31
		{Timestamp: 1706745600000, Equity: 110}, // 2024-02-01
		{Timestamp: 1709164800000, Equity: 105}, // 2024-02-29
		{Timestamp: 1709251200000, Equity: 105}, // 2024-03-01
		{Timestamp: 1711843200000, Equity: 115}, // 2024-03-31
	}
	m := monthlyReturns(equity)
	if len(m) != 3 {
		t.Errorf("len(monthly) = %d, want 3", len(m))
	}
	if m["2024-01"] != 10.0 {
		t.Errorf("Jan return = %v, want 10", m["2024-01"])
	}
	if m["2024-02"] != -4.545454545454546 {
		t.Errorf("Feb return = %v", m["2024-02"])
	}
	if m["2024-03"] != 9.523809523809524 {
		t.Errorf("Mar return = %v", m["2024-03"])
	}
}

func TestYearlyReturns(t *testing.T) {
	// Each year needs at least 2 points
	equity := []EquityPoint{
		{Timestamp: 1672531200000, Equity: 100}, // 2023-01-01
		{Timestamp: 1675209600000, Equity: 120}, // 2023-02-01
		{Timestamp: 1704067200000, Equity: 150}, // 2024-01-01
		{Timestamp: 1706745600000, Equity: 180}, // 2024-02-01
	}
	y := yearlyReturns(equity)
	if len(y) != 2 {
		t.Errorf("len(yearly) = %d, want 2", len(y))
	}
	if y["2023"] != 20.0 {
		t.Errorf("2023 return = %v, want 20", y["2023"])
	}
	if y["2024"] != 20.0 {
		t.Errorf("2024 return = %v, want 20", y["2024"])
	}
}

func TestSampleEquity(t *testing.T) {
	equity := make([]EquityPoint, 1000)
	for i := range equity {
		equity[i] = EquityPoint{Equity: float64(i)}
	}
	sampled := sampleEquity(equity, 500)
	if len(sampled) != 500 {
		t.Errorf("len(sampled) = %d, want 500", len(sampled))
	}
	// First and last should match
	if sampled[0].Equity != equity[0].Equity {
		t.Error("first point mismatch")
	}
	if sampled[len(sampled)-1].Equity != equity[len(equity)-1].Equity {
		t.Error("last point mismatch")
	}

	// If input <= maxPoints, should return as-is
	small := []EquityPoint{{Equity: 1}, {Equity: 2}}
	if len(sampleEquity(small, 500)) != 2 {
		t.Error("expected pass-through for small input")
	}
}

func TestGenerateReport(t *testing.T) {
	result := &RunResult{
		TotalReturn:    500,
		TotalReturnPct: 5,
		MaxDrawdown:    100,
		MaxDrawdownPct: 1,
		SharpeRatio:    1.5,
		SortinoRatio:   1.8,
		CalmarRatio:    5,
		WinRate:        55,
		ProfitFactor:   2.0,
		TotalTrades:    20,
		WinningTrades:  11,
		LosingTrades:   9,
		AvgWin:         50,
		AvgLoss:        -20,
		BestTrade:      100,
		WorstTrade:     -50,
		EquityCurve: []EquityPoint{
			{Timestamp: 0, Equity: 10000},
			{Timestamp: 86400000, Equity: 10500},
			{Timestamp: 172800000, Equity: 10300},
			{Timestamp: 259200000, Equity: 10500},
		},
		Trades: []Position{
			{RealizedPnL: 50, EntryTime: 0, ExitTime: 86400000},
			{RealizedPnL: -30, EntryTime: 86400000, ExitTime: 172800000},
			{RealizedPnL: 60, EntryTime: 172800000, ExitTime: 259200000},
		},
	}

	report := GenerateReport(result, "test-strat", "BTCUSDT")
	if report.Strategy != "test-strat" {
		t.Errorf("Strategy = %q", report.Strategy)
	}
	if report.Symbol != "BTCUSDT" {
		t.Errorf("Symbol = %q", report.Symbol)
	}
	if report.TotalTrades != 20 {
		t.Errorf("TotalTrades = %d", report.TotalTrades)
	}
	if report.MaxConsecWins != 1 {
		t.Errorf("MaxConsecWins = %d, want 1", report.MaxConsecWins)
	}
	if report.MaxConsecLoss != 1 {
		t.Errorf("MaxConsecLoss = %d, want 1", report.MaxConsecLoss)
	}
	if report.AvgHoldingMs != 86400000 {
		t.Errorf("AvgHoldingMs = %d, want 86400000", report.AvgHoldingMs)
	}
	if len(report.EquitySampled) > 0 && report.EquitySampled[0].Equity != 10000 {
		t.Error("EquitySampled first point wrong")
	}
}

func TestPerformanceReport_SaveJSON(t *testing.T) {
	report := &PerformanceReport{
		RunID:       "bt_test",
		Strategy:    "s",
		Symbol:      "BTCUSDT",
		TotalReturn: 100,
	}
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "report.json")
	if err := report.SaveJSON(path); err != nil {
		t.Fatalf("SaveJSON error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !contains(string(data), `"bt_test"`) {
		t.Error("JSON missing run_id")
	}
}

func TestPerformanceReport_Summary(t *testing.T) {
	report := &PerformanceReport{
		RunID:          "bt_1",
		Strategy:       "S",
		Symbol:         "BTCUSDT",
		TotalReturnPct: 10,
		SharpeRatio:    1.5,
		TotalTrades:    10,
		WinningTrades:  6,
		WinRate:        60,
		MaxConsecWins:  3,
		MaxConsecLoss:  2,
	}
	summary := report.Summary()
	if !contains(summary, "Performance Report") {
		t.Error("summary missing header")
	}
	if !contains(summary, "10.00%") {
		t.Error("summary missing return")
	}
}

func TestCompareReports(t *testing.T) {
	a := &PerformanceReport{Strategy: "A", TotalReturnPct: 10, SharpeRatio: 1.5, TotalTrades: 20}
	b := &PerformanceReport{Strategy: "B", TotalReturnPct: 5, SharpeRatio: 1.2, TotalTrades: 15}
	cmp := CompareReports(a, b)
	if !contains(cmp, "A") || !contains(cmp, "B") {
		t.Error("comparison missing strategy names")
	}
}

func contains(s, substr string) bool {
	return containsString(s, substr)
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
