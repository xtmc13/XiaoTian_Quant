package experiment

import (
	"math"

	"github.com/xiaotian-quant/gateway/internal/backtest"
)

// ── Seven-Factor Score ────────────────────────────────────────────

// ScoreWeights defines the weight of each factor (should sum to 1.0).
var DefaultScoreWeights = map[string]float64{
	"return":        0.15,
	"sharpe":        0.20,
	"drawdown":      0.20,
	"win_rate":      0.10,
	"profit_factor": 0.10,
	"stability":     0.15,
	"overfit":       0.10,
}

// ScoreResult holds the comprehensive 7-factor score.
type ScoreResult struct {
	TotalScore     float64            `json:"total_score"`     // 0-100
	RawScore       float64            `json:"raw_score"`       // weighted sum before clamp
	FactorScores   map[string]float64 `json:"factor_scores"`   // each 0-100
	FactorDetails  map[string]float64 `json:"factor_details"`  // raw metric values
	PassedOOS      bool               `json:"passed_oos"`
	OverfitPenalty float64            `json:"overfit_penalty"` // 0-1
}

// ScoreConfig configures the scoring behavior.
type ScoreConfig struct {
	Weights      map[string]float64 `json:"weights"`
	MinTrades    int                `json:"min_trades"`     // minimum trades to be valid
	MinSharpe    float64            `json:"min_sharpe"`     // reject if below
	MaxDrawdown  float64            `json:"max_drawdown"`   // reject if above (pct)
	OOSSharpeRatio float64          `json:"oos_sharpe_ratio"` // min IS/OOS sharpe ratio
}

func DefaultScoreConfig() ScoreConfig {
	return ScoreConfig{
		Weights:        copyWeights(DefaultScoreWeights),
		MinTrades:      10,
		MinSharpe:      -999,
		MaxDrawdown:    100,
		OOSSharpeRatio: 0.5,
	}
}

// Score evaluates a backtest result with optional OOS comparison.
func Score(result *backtest.RunResult, oosResult *backtest.RunResult, cfg ScoreConfig) *ScoreResult {
	sr := &ScoreResult{
		FactorScores:  make(map[string]float64),
		FactorDetails: make(map[string]float64),
	}

	// Check minimum thresholds
	if result.TotalTrades < cfg.MinTrades {
		return sr // all zeros = invalid
	}
	if result.SharpeRatio < cfg.MinSharpe {
		return sr
	}
	if result.MaxDrawdownPct > cfg.MaxDrawdown {
		return sr
	}

	// ── Factor 1: Return (annualized, normalized) ──
	// Assume backtest covers ~1 year for 500 bars of 1h; scale accordingly
	annualReturn := result.TotalReturnPct
	sr.FactorDetails["return"] = annualReturn
	sr.FactorScores["return"] = clampScore(annualReturn * 2) // 50% = 100pts

	// ── Factor 2: Sharpe Ratio ──
	sharpe := result.SharpeRatio
	sr.FactorDetails["sharpe"] = sharpe
	sr.FactorScores["sharpe"] = clampScore(sharpe * 25) // 4.0 = 100pts

	// ── Factor 3: Drawdown (lower is better, inverted) ──
	dd := result.MaxDrawdownPct
	sr.FactorDetails["drawdown"] = dd
	// 0% DD = 100pts, 50% DD = 0pts
	sr.FactorScores["drawdown"] = clampScore(100 - dd*2)

	// ── Factor 4: Win Rate ──
	wr := result.WinRate
	sr.FactorDetails["win_rate"] = wr
	sr.FactorScores["win_rate"] = clampScore(wr) // already 0-100

	// ── Factor 5: Profit Factor ──
	pf := result.ProfitFactor
	sr.FactorDetails["profit_factor"] = pf
	// 3.0 = 100pts
	sr.FactorScores["profit_factor"] = clampScore(pf / 3.0 * 100)

	// ── Factor 6: Stability (R² of equity curve vs linear trend) ──
	stability := computeStability(result.EquityCurve)
	sr.FactorDetails["stability"] = stability
	sr.FactorScores["stability"] = clampScore(stability * 100)

	// ── Factor 7: Overfit (IS vs OOS consistency) ──
	overfitScore := 100.0
	if oosResult != nil && oosResult.TotalTrades >= cfg.MinTrades {
		isSharpe := result.SharpeRatio
		oosSharpe := oosResult.SharpeRatio
		if isSharpe > 0 && oosSharpe >= 0 {
			 ratio := oosSharpe / isSharpe
			 sr.FactorDetails["oos_sharpe_ratio"] = ratio
			 if ratio >= cfg.OOSSharpeRatio {
				 // Good: ratio close to 1.0 is best
				 overfitScore = clampScore(ratio * 100)
				 sr.PassedOOS = true
			 } else {
				 overfitScore = clampScore(ratio * 100)
				 sr.PassedOOS = false
			 }
		} else if isSharpe <= 0 && oosSharpe <= 0 {
			// Both negative = consistent (but bad)
			overfitScore = 60.0
			sr.PassedOOS = true
		} else if oosSharpe < isSharpe {
			overfitScore = 30.0
			sr.PassedOOS = false
		}
	} else {
		// No OOS data = assume worst case for overfit
		overfitScore = 50.0
		sr.PassedOOS = false
	}
	sr.FactorScores["overfit"] = overfitScore
	sr.FactorDetails["overfit"] = overfitScore

	// ── Weighted total ──
	weights := cfg.Weights
	if weights == nil {
		weights = DefaultScoreWeights
	}
	total := 0.0
	for factor, score := range sr.FactorScores {
		w := weights[factor]
		total += score * w
	}
	sr.RawScore = total
	sr.TotalScore = clampScore(total)

	return sr
}

// clampScore clamps a score to [0, 100].
func clampScore(s float64) float64 {
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return s
}

// computeStability calculates R² of equity curve against a linear trend.
// Returns value in [0, 1], where 1.0 = perfectly linear upward trend.
func computeStability(equity []backtest.EquityPoint) float64 {
	if len(equity) < 3 {
		return 0
	}

	n := float64(len(equity))
	var sumX, sumY, sumXY, sumX2, sumY2 float64
	for i, pt := range equity {
		x := float64(i)
		y := pt.Equity
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
		sumY2 += y * y
	}

	denominator := (n*sumX2 - sumX*sumX) * (n*sumY2 - sumY*sumY)
	if denominator <= 0 {
		return 0
	}

	r := (n*sumXY - sumX*sumY) / math.Sqrt(denominator)
	// R² = r*r, but we also want to penalize downward trends
	if r < 0 {
		return 0
	}
	return r * r
}

func copyWeights(src map[string]float64) map[string]float64 {
	dst := make(map[string]float64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
