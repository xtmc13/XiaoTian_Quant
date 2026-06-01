package backtest

import (
	"math"
)

// EdgeResult holds the statistical edge analysis of a trading strategy.
type EdgeResult struct {
	WinRate          float64 `json:"win_rate"`
	LossRate         float64 `json:"loss_rate"`
	AvgWin           float64 `json:"avg_win"`
	AvgLoss          float64 `json:"avg_loss"`
	Expectancy       float64 `json:"expectancy"`
	ExpectancyRatio  float64 `json:"expectancy_ratio"`
	ProfitFactor     float64 `json:"profit_factor"`
	RiskRewardRatio  float64 `json:"risk_reward_ratio"`
	KellyFraction    float64 `json:"kelly_fraction"`
	OptimalF         float64 `json:"optimal_f"`         // Optimal fixed fraction
	EdgeRatio        float64 `json:"edge_ratio"`
	Score            float64 `json:"score"`              // Composite score 0-100
	Rating           string  `json:"rating"`             // "Excellent", "Good", "Fair", "Poor"
	ConfidenceLevel  float64 `json:"confidence_level"`   // Statistical confidence
	RequiredTrades   int     `json:"required_trades"`     // Trades needed for statistical significance
}

// ── Edge Calculator ────────────────────────────────────────────

// CalculateEdge computes statistical edge from trade history.
func CalculateEdge(trades []Position) *EdgeResult {
	if len(trades) == 0 {
		return nil
	}

	var wins, losses int
	var totalWin, totalLoss float64
	var winAmounts, lossAmounts []float64

	for _, t := range trades {
		if t.RealizedPnL > 0 {
			wins++
			totalWin += t.RealizedPnL
			winAmounts = append(winAmounts, t.RealizedPnL)
		} else if t.RealizedPnL < 0 {
			losses++
			totalLoss += math.Abs(t.RealizedPnL)
			lossAmounts = append(lossAmounts, math.Abs(t.RealizedPnL))
		}
	}

	total := wins + losses
	if total == 0 {
		return nil
	}

	wr := float64(wins) / float64(total)
	lr := 1 - wr
	avgWin := 0.0
	if wins > 0 {
		avgWin = totalWin / float64(wins)
	}
	avgLoss := 0.0
	if losses > 0 {
		avgLoss = totalLoss / float64(losses)
	}

	// Expectancy: average profit per trade
	expectancy := wr*avgWin - lr*avgLoss

	// Expectancy ratio (expectancy / avg loss)
	expectancyRatio := 0.0
	if avgLoss > 0 {
		expectancyRatio = expectancy / avgLoss
	}

	// Profit factor
	pf := 1.0
	if totalLoss > 0 {
		pf = totalWin / totalLoss
	}

	// Risk/Reward ratio
	rr := 0.0
	if avgLoss > 0 {
		rr = avgWin / avgLoss
	}

	// Kelly fraction: f* = (p*b - q) / b
	// where p = win rate, q = loss rate, b = avg_win/avg_loss
	kelly := 0.0
	if avgLoss > 0 {
		b := avgWin / avgLoss
		if b > 0 {
			kelly = (wr*b - lr) / b
			if kelly < 0 {
				kelly = 0
			}
		}
	}

	// Optimal F (half-Kelly for safety)
	optimalF := kelly / 2

	// Edge ratio: (win_rate * avg_win) / (loss_rate * avg_loss) - 1
	edgeRatio := 0.0
	if lr*avgLoss > 0 {
		edgeRatio = (wr*avgWin)/(lr*avgLoss) - 1
	}

	// Composite score (0-100)
	score := calculateScore(wr, pf, rr, expectancyRatio, total)

	// Rating
	rating := "Poor"
	switch {
	case score >= 80:
		rating = "Excellent"
	case score >= 60:
		rating = "Good"
	case score >= 40:
		rating = "Fair"
	}

	// Statistical confidence (Z-test approximation)
	confidence := statisticalConfidence(wr, total)

	// Trades needed for significance
	requiredTrades := requiredTradesForSignificance(wr, total)

	return &EdgeResult{
		WinRate:         wr * 100,
		LossRate:        lr * 100,
		AvgWin:          avgWin,
		AvgLoss:         avgLoss,
		Expectancy:      expectancy,
		ExpectancyRatio: expectancyRatio,
		ProfitFactor:    pf,
		RiskRewardRatio: rr,
		KellyFraction:   kelly * 100, // percentage
		OptimalF:        optimalF * 100,
		EdgeRatio:       edgeRatio,
		Score:           math.Round(score*10) / 10,
		Rating:          rating,
		ConfidenceLevel: math.Round(confidence*10) / 10,
		RequiredTrades:  requiredTrades,
	}
}

func calculateScore(wr, pf, rr, er float64, totalTrades int) float64 {
	// Weighted composite: win rate (30%) + profit factor (30%) + R:R (20%) + trades (20%)
	wrScore := math.Min(wr*150, 30)              // 50% wr → 15, 66% wr → 30
	pfScore := math.Min(pf*15, 30)              // pf=1 → 15, pf=2+ → 30
	rrScore := math.Min(rr*15, 20)              // R:R=1 → 15, 1.5+ → 20
	tradeScore := math.Min(float64(totalTrades)/10, 20) // 200+ trades → 20

	return wrScore + pfScore + rrScore + tradeScore
}

func statisticalConfidence(wr float64, n int) float64 {
	// Binomial proportion confidence (simplified)
	if n < 10 {
		return 0
	}
	// Standard error of proportion
	se := math.Sqrt(wr * (1 - wr) / float64(n))
	// Z-score: how many std devs from 0.5
	z := math.Abs(wr-0.5) / se
	// Convert to confidence level (approximation)
	conf := math.Erf(z / math.Sqrt2)
	return conf * 100
}

func requiredTradesForSignificance(wr float64, n int) int {
	// How many trades needed for p < 0.05 significance
	// Assuming we want 95% confidence that wr != 0.5
	se := math.Sqrt(wr * (1 - wr) / float64(n))
	if se == 0 {
		return 999999
	}
	required := math.Pow(1.96*math.Sqrt(wr*(1-wr))/0.05, 2)
	needed := int(required) - n
	if needed < 0 {
		return 0
	}
	return needed
}
