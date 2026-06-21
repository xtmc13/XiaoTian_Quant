package backtest

import (
	"fmt"
	"math"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// LookaheadReport contains the results of a lookahead-bias check.
type LookaheadReport struct {
	HasLookahead bool     `json:"has_lookahead"`
	Signals      []string `json:"signals"`
	Details      []string `json:"details"`
}

// CheckLookaheadBias scans raw trade signals for obvious lookahead bias.
func CheckLookaheadBias(result *RunResult, bars []model.Bar) *LookaheadReport {
	report := &LookaheadReport{
		Signals: []string{},
		Details: []string{},
	}
	if len(result.Trades) == 0 || len(bars) == 0 {
		return report
	}

	// Build time -> index map for bars.
	barIndexByTime := make(map[int64]int)
	for i, b := range bars {
		barIndexByTime[b.Time] = i
	}

	window := 5
	suspicious := 0
	for _, t := range result.Trades {
		idx, ok := barIndexByTime[t.EntryTime]
		if !ok || idx+window >= len(bars) {
			continue
		}
		entryClose := bars[idx].Close
		minFuture := math.MaxFloat64
		for i := idx + 1; i <= idx+window && i < len(bars); i++ {
			if bars[i].Close < minFuture {
				minFuture = bars[i].Close
			}
		}
		// If entry price is within 0.05% of the future local minimum, flag it.
		if minFuture > 0 && math.Abs(entryClose-minFuture)/minFuture < 0.0005 {
			suspicious++
			report.Details = append(report.Details, fmt.Sprintf("trade entry at %d (%.2f) near local min %.2f", t.EntryTime, entryClose, minFuture))
		}
	}

	if len(result.Trades) > 0 {
		pct := float64(suspicious) / float64(len(result.Trades)) * 100
		if pct > 20 {
			report.HasLookahead = true
			report.Signals = append(report.Signals, fmt.Sprintf("%.1f%% of entries occur near future local minima — possible lookahead bias", pct))
		}
	}
	return report
}

// RecursiveReport contains the results of a recursive-formula check.
type RecursiveReport struct {
	HasRecursion bool     `json:"has_recursion"`
	Signals      []string `json:"signals"`
}

// CheckRecursiveBias checks whether the equity curve or trade PnL shows
// pathological recursive dependency (e.g., unrealistically smooth growth).
// A very high Sharpe with near-zero drawdown is a common symptom of
// recursive/forward-looking indicators.
func CheckRecursiveBias(report *PerformanceReport) *RecursiveReport {
	r := &RecursiveReport{Signals: []string{}}
	if report.SharpeRatio > 5 && report.MaxDrawdownPct < 1 {
		r.HasRecursion = true
		r.Signals = append(r.Signals, "Sharpe ratio extremely high with negligible drawdown — check for recursive self-reference")
	}
	if report.ProfitFactor > 10 && report.TotalTrades > 20 {
		r.HasRecursion = true
		r.Signals = append(r.Signals, "Profit factor abnormally high — verify indicator does not use future data")
	}
	if report.WinRate > 90 && report.TotalTrades > 20 {
		r.Signals = append(r.Signals, "Win rate above 90% — unusual for non-recursive strategies")
	}
	return r
}

// OverfitReport contains overfitting risk metrics.
type OverfitReport struct {
	RiskLevel string   `json:"risk_level"` // low / medium / high
	Signals   []string `json:"signals"`
}

// CheckOverfitRisk estimates overfitting risk from the number of trades,
// parameters, and performance metrics.
func CheckOverfitRisk(report *PerformanceReport, paramCount int) *OverfitReport {
	r := &OverfitReport{Signals: []string{}}
	if report.TotalTrades < 50 {
		r.Signals = append(r.Signals, "Very few trades — results may not be statistically significant")
	}
	if paramCount > 0 && report.TotalTrades > 0 {
		tradesPerParam := float64(report.TotalTrades) / float64(paramCount)
		if tradesPerParam < 10 {
			r.Signals = append(r.Signals, fmt.Sprintf("Only %.1f trades per parameter — high overfit risk", tradesPerParam))
		}
	}
	if report.TotalTrades > 50 && report.SharpeRatio > 3 {
		r.Signals = append(r.Signals, "High Sharpe with sufficient trades — validate on out-of-sample data")
	}

	switch {
	case len(r.Signals) >= 2:
		r.RiskLevel = "high"
	case len(r.Signals) == 1:
		r.RiskLevel = "medium"
	default:
		r.RiskLevel = "low"
	}
	return r
}

// AnalysisBundle groups all diagnostic reports for a backtest.
type AnalysisBundle struct {
	Lookahead  *LookaheadReport  `json:"lookahead"`
	Recursive  *RecursiveReport  `json:"recursive"`
	Overfit    *OverfitReport    `json:"overfit"`
	Summary    string            `json:"summary"`
}

// RunDiagnostics runs all diagnostic checks and returns a bundle.
func RunDiagnostics(result *RunResult, report *PerformanceReport, bars []model.Bar, paramCount int) *AnalysisBundle {
	bundle := &AnalysisBundle{
		Lookahead: CheckLookaheadBias(result, bars),
		Recursive: CheckRecursiveBias(report),
		Overfit:   CheckOverfitRisk(report, paramCount),
	}
	var parts []string
	if bundle.Lookahead.HasLookahead {
		parts = append(parts, "疑似前视偏差")
	}
	if bundle.Recursive.HasRecursion {
		parts = append(parts, "疑似递归公式/未来函数")
	}
	if bundle.Overfit.RiskLevel == "high" {
		parts = append(parts, "高过拟合风险")
	}
	if len(parts) == 0 {
		bundle.Summary = "未发现明显问题"
	} else {
		bundle.Summary = strings.Join(parts, "；")
	}
	return bundle
}
