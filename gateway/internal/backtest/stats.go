package backtest

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"time"
)

// PerformanceReport is a comprehensive backtest performance report.
type PerformanceReport struct {
	RunID          string  `json:"run_id"`
	Strategy       string  `json:"strategy"`
	Symbol         string  `json:"symbol"`
	StartTime      int64   `json:"start_time"`
	EndTime        int64   `json:"end_time"`
	DurationMs     int64   `json:"duration_ms"`

	TotalReturn    float64 `json:"total_return"`
	TotalReturnPct float64 `json:"total_return_pct"`
	MaxDrawdown    float64 `json:"max_drawdown"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	SharpeRatio    float64 `json:"sharpe_ratio"`
	SortinoRatio   float64 `json:"sortino_ratio"`
	CalmarRatio    float64 `json:"calmar_ratio"`
	WinRate        float64 `json:"win_rate"`
	ProfitFactor   float64 `json:"profit_factor"`
	RecoveryFactor float64 `json:"recovery_factor"`

	TotalTrades    int     `json:"total_trades"`
	WinningTrades  int     `json:"winning_trades"`
	LosingTrades   int     `json:"losing_trades"`
	AvgWin         float64 `json:"avg_win"`
	AvgLoss        float64 `json:"avg_loss"`
	BestTrade      float64 `json:"best_trade"`
	WorstTrade     float64 `json:"worst_trade"`
	AvgHoldingMs   int64   `json:"avg_holding_ms"`

	MaxConsecWins int `json:"max_consec_wins"`
	MaxConsecLoss int `json:"max_consec_loss"`

	InitialBalance float64 `json:"initial_balance"`
	FinalEquity    float64 `json:"final_equity"`

	// Monthly breakdown
	MonthlyReturns map[string]float64 `json:"monthly_returns"`
	YearlyReturns  map[string]float64 `json:"yearly_returns"`

	// Risk metrics
	VaR95    float64 `json:"var_95"`   // 95% Value at Risk
	CVaR95   float64 `json:"cvar_95"`  // 95% Conditional VaR
	Volatility float64 `json:"volatility"` // Annualized volatility

	// Equity curve (sampled for display)
	EquitySampled []EquityPoint `json:"equity_sampled,omitempty"`
}

// GenerateReport creates a full performance report from a RunResult.
func GenerateReport(result *RunResult, strategy, symbol string) *PerformanceReport {
	report := &PerformanceReport{
		RunID:       fmt.Sprintf("bt_%d", time.Now().UnixMilli()),
		Strategy:    strategy,
		Symbol:      symbol,
		DurationMs:  result.DurationMs,

		TotalReturn:    result.TotalReturn,
		TotalReturnPct: result.TotalReturnPct,
		MaxDrawdown:    result.MaxDrawdown,
		MaxDrawdownPct: result.MaxDrawdownPct,
		SharpeRatio:    result.SharpeRatio,
		SortinoRatio:   result.SortinoRatio,
		CalmarRatio:    result.CalmarRatio,
		WinRate:        result.WinRate,
		ProfitFactor:   result.ProfitFactor,

		TotalTrades:   result.TotalTrades,
		WinningTrades: result.WinningTrades,
		LosingTrades:  result.LosingTrades,
		AvgWin:        result.AvgWin,
		AvgLoss:       result.AvgLoss,
		BestTrade:     result.BestTrade,
		WorstTrade:    result.WorstTrade,
	}

	if len(result.EquityCurve) > 0 {
		report.StartTime = result.EquityCurve[0].Timestamp
		report.EndTime = result.EquityCurve[len(result.EquityCurve)-1].Timestamp
		report.FinalEquity = result.EquityCurve[len(result.EquityCurve)-1].Equity
		report.InitialBalance = result.EquityCurve[0].Equity
	}

	// Consecutive wins/losses
	report.MaxConsecWins, report.MaxConsecLoss = computeConsecutive(result.Trades)

	// Average holding time
	if len(result.Trades) > 0 {
		var totalHolding int64
		for _, t := range result.Trades {
			totalHolding += t.ExitTime - t.EntryTime
		}
		report.AvgHoldingMs = totalHolding / int64(len(result.Trades))
	}

	// Recovery factor
	if report.MaxDrawdown > 0 {
		report.RecoveryFactor = report.TotalReturn / report.MaxDrawdown
	}

	// Monthly returns
	report.MonthlyReturns = monthlyReturns(result.EquityCurve)
	report.YearlyReturns = yearlyReturns(result.EquityCurve)

	// VaR / CVaR
	returns := equityToReturns(result.EquityCurve)
	report.VaR95 = valueAtRisk(returns, 0.95)
	report.CVaR95 = conditionalVaR(returns, 0.95)
	report.Volatility = annualizedVolatility(returns)

	// Sample equity curve for display (max 500 points)
	report.EquitySampled = sampleEquity(result.EquityCurve, 500)

	return report
}

// SaveJSON writes the report to a JSON file.
func (r *PerformanceReport) SaveJSON(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Summary returns a multi-line text summary of the report.
func (r *PerformanceReport) Summary() string {
	return fmt.Sprintf(`Performance Report: %s
Symbol: %s | Strategy: %s
Period: %s → %s (%d ms)

Returns
  Total Return:    %.2f (%.2f%%)
  Max Drawdown:    %.2f%%
  Sharpe Ratio:    %.2f
  Sortino Ratio:   %.2f
  Calmar Ratio:    %.2f
  Profit Factor:   %.2f
  Recovery Factor: %.2f

Trades
  Total:           %d
  Winning:         %d (%.1f%%)
  Losing:          %d
  Avg Win:         %.2f
  Avg Loss:        %.2f
  Best Trade:      %.2f
  Worst Trade:     %.2f
  Max Consec Wins: %d | Losses: %d

Risk
  VaR (95%%):       %.2f%%
  CVaR (95%%):      %.2f%%
  Annual Vol:      %.2f%%
`,
		r.Strategy, r.Symbol, r.RunID,
		time.UnixMilli(r.StartTime).Format("2006-01-02"),
		time.UnixMilli(r.EndTime).Format("2006-01-02"),
		r.DurationMs,
		r.TotalReturn, r.TotalReturnPct,
		r.MaxDrawdownPct, r.SharpeRatio, r.SortinoRatio,
		r.CalmarRatio, r.ProfitFactor, r.RecoveryFactor,
		r.TotalTrades, r.WinningTrades, r.WinRate,
		r.LosingTrades, r.AvgWin, r.AvgLoss,
		r.BestTrade, r.WorstTrade,
		r.MaxConsecWins, r.MaxConsecLoss,
		r.VaR95*100, r.CVaR95*100, r.Volatility*100,
	)
}

// CompareReports compares two reports side-by-side.
func CompareReports(a, b *PerformanceReport) string {
	return fmt.Sprintf(`Metric           | %-20s | %-20s
Total Return     | %18.2f%% | %18.2f%%
Sharpe Ratio     | %18.2f  | %18.2f
Max Drawdown     | %18.2f%% | %18.2f%%
Win Rate         | %18.1f%% | %18.1f%%
Profit Factor    | %18.2f  | %18.2f
Total Trades     | %18d  | %18d
`,
		a.Strategy, b.Strategy,
		a.TotalReturnPct, b.TotalReturnPct,
		a.SharpeRatio, b.SharpeRatio,
		a.MaxDrawdownPct, b.MaxDrawdownPct,
		a.WinRate, b.WinRate,
		a.ProfitFactor, b.ProfitFactor,
		a.TotalTrades, b.TotalTrades,
	)
}

// ── Internal helpers ──

func computeConsecutive(trades []Position) (int, int) {
	maxWins, maxLoss := 0, 0
	curWins, curLoss := 0, 0
	for _, t := range trades {
		if t.RealizedPnL > 0 {
			curWins++
			curLoss = 0
			if curWins > maxWins {
				maxWins = curWins
			}
		} else {
			curLoss++
			curWins = 0
			if curLoss > maxLoss {
				maxLoss = curLoss
			}
		}
	}
	return maxWins, maxLoss
}

func equityToReturns(equity []EquityPoint) []float64 {
	if len(equity) < 2 {
		return nil
	}
	returns := make([]float64, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if equity[i-1].Equity > 0 {
			returns[i-1] = (equity[i].Equity - equity[i-1].Equity) / equity[i-1].Equity
		}
	}
	return returns
}

func valueAtRisk(returns []float64, confidence float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	sorted := make([]float64, len(returns))
	copy(sorted, returns)
	sort.Float64s(sorted)
	idx := int(float64(len(sorted)) * (1 - confidence))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return -sorted[idx]
}

func conditionalVaR(returns []float64, confidence float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	sorted := make([]float64, len(returns))
	copy(sorted, returns)
	sort.Float64s(sorted)
	idx := int(float64(len(sorted)) * (1 - confidence))
	if idx <= 0 {
		return 0
	}
	sum := 0.0
	for i := 0; i < idx; i++ {
		sum += sorted[i]
	}
	return -sum / float64(idx)
}

func annualizedVolatility(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	avg := 0.0
	for _, r := range returns {
		avg += r
	}
	avg /= float64(len(returns))
	variance := 0.0
	for _, r := range returns {
		variance += (r - avg) * (r - avg)
	}
	return math.Sqrt(variance/float64(len(returns)-1)) * math.Sqrt(252)
}

func monthlyReturns(equity []EquityPoint) map[string]float64 {
	result := make(map[string]float64)
	if len(equity) < 2 {
		return result
	}

	monthBuckets := make(map[string][]float64)
	for _, e := range equity {
		t := time.UnixMilli(e.Timestamp)
		key := t.Format("2006-01")
		monthBuckets[key] = append(monthBuckets[key], e.Equity)
	}

	for key, vals := range monthBuckets {
		if len(vals) >= 2 && vals[0] > 0 {
			result[key] = (vals[len(vals)-1] - vals[0]) / vals[0] * 100
		}
	}
	return result
}

func yearlyReturns(equity []EquityPoint) map[string]float64 {
	result := make(map[string]float64)
	if len(equity) < 2 {
		return result
	}

	yearBuckets := make(map[string][]float64)
	for _, e := range equity {
		t := time.UnixMilli(e.Timestamp)
		key := t.Format("2006")
		yearBuckets[key] = append(yearBuckets[key], e.Equity)
	}

	for key, vals := range yearBuckets {
		if len(vals) >= 2 && vals[0] > 0 {
			result[key] = (vals[len(vals)-1] - vals[0]) / vals[0] * 100
		}
	}
	return result
}

func sampleEquity(equity []EquityPoint, maxPoints int) []EquityPoint {
	if len(equity) <= maxPoints {
		return equity
	}
	step := float64(len(equity)-1) / float64(maxPoints-1)
	sampled := make([]EquityPoint, maxPoints)
	for i := 0; i < maxPoints; i++ {
		idx := int(float64(i) * step)
		if idx >= len(equity) {
			idx = len(equity) - 1
		}
		sampled[i] = equity[idx]
	}
	return sampled
}
