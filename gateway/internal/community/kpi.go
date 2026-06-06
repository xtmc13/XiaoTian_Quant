package community

import "math"

// KPIScore holds the composite KPI rating for a strategy.
type KPIScore struct {
	TotalScore      float64 `json:"total_score"`
	ReturnScore     float64 `json:"return_score"`
	SharpeScore     float64 `json:"sharpe_score"`
	StabilityScore  float64 `json:"stability_score"`
	PopularityScore float64 `json:"popularity_score"`
	OverfitPenalty  float64 `json:"overfit_penalty"`
}

// CalculateKPIScore computes the composite KPI score for a strategy.
func CalculateKPIScore(item *StrategyItem, overfit *OverfitResult) *KPIScore {
	// Return score: total_return > 50% = 30, >20% = 20, >0 = 10
	returnScore := 0.0
	if item.TotalReturn > 50 {
		returnScore = 30
	} else if item.TotalReturn > 20 {
		returnScore = 20
	} else if item.TotalReturn > 0 {
		returnScore = 10
	}

	// Sharpe score: sharpe > 2 = 25, >1 = 15, >0 = 5
	sharpeScore := 0.0
	if item.SharpeRatio > 2 {
		sharpeScore = 25
	} else if item.SharpeRatio > 1 {
		sharpeScore = 15
	} else if item.SharpeRatio > 0 {
		sharpeScore = 5
	}

	// Stability score: max_drawdown < 10% = 25, <20% = 15, <30% = 5
	dd := math.Abs(item.MaxDrawdown)
	stabilityScore := 0.0
	if dd < 10 {
		stabilityScore = 25
	} else if dd < 20 {
		stabilityScore = 15
	} else if dd < 30 {
		stabilityScore = 5
	}

	// Popularity score: (downloads*2 + ratings*3 + comments*5) / 100, cap 20
	popularityScore := (float64(item.DownloadCount)*2 + float64(item.RatingCount)*3 + float64(item.CommentCount)*5) / 100.0
	if popularityScore > 20 {
		popularityScore = 20
	}

	// Overfit penalty
	overfitPenalty := 0.0
	if overfit != nil {
		if overfit.Score > 60 {
			overfitPenalty = 20
		} else if overfit.Score > 30 {
			overfitPenalty = 10
		}
	}

	total := returnScore + sharpeScore + stabilityScore + popularityScore - overfitPenalty
	if total < 0 {
		total = 0
	}
	if total > 100 {
		total = 100
	}

	return &KPIScore{
		TotalScore:      math.Round(total*10) / 10,
		ReturnScore:     returnScore,
		SharpeScore:     sharpeScore,
		StabilityScore:  stabilityScore,
		PopularityScore: math.Round(popularityScore*10) / 10,
		OverfitPenalty:  overfitPenalty,
	}
}
