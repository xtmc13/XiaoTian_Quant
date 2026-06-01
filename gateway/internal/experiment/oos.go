package experiment

import (
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── OOS (Out-of-Sample) Validation ────────────────────────────────

// OOSSplit splits bar data into In-Sample and Out-of-Sample sets.
// ratio: fraction for OOS (e.g., 0.3 = last 30% is OOS).
func OOSSplit(bars []model.Bar, ratio float64) (is, oos []model.Bar) {
	if ratio <= 0 || ratio >= 1 {
		return bars, nil
	}
	splitIdx := int(float64(len(bars)) * (1 - ratio))
	if splitIdx < 1 {
		splitIdx = len(bars) / 2
	}
	if splitIdx >= len(bars) {
		return bars, nil
	}
	return bars[:splitIdx], bars[splitIdx:]
}

// OOSSplitByTime splits bars by absolute time boundary.
func OOSSplitByTime(bars []model.Bar, boundary int64) (is, oos []model.Bar) {
	for _, b := range bars {
		if b.Time < boundary {
			is = append(is, b)
		} else {
			oos = append(oos, b)
		}
	}
	return
}

// OOSValidation holds IS + OOS backtest results.
type OOSValidation struct {
	ISResult    *backtest.RunResult    `json:"is_result"`
	OOSResult   *backtest.RunResult    `json:"oos_result"`
	ISScore     *ScoreResult           `json:"is_score"`
	OOSScore    *ScoreResult           `json:"oos_score"`
	Passed      bool                   `json:"passed"`
	FailReason  string                 `json:"fail_reason,omitempty"`
}

// ValidateOOS runs backtest on both IS and OOS data and compares.
func ValidateOOS(
	isBars, oosBars []model.Bar,
	strategy backtest.BacktestStrategy,
	cfg backtest.RunnerConfig,
	scoreCfg ScoreConfig,
) *OOSValidation {
	v := &OOSValidation{}

	// IS backtest
	isRunner := backtest.NewRunner(cfg)
	isRunner.LoadBars(strategy.Symbol(), isBars)
	isRes, err := isRunner.Run(strategy)
	if err != nil {
		v.FailReason = "IS backtest failed: " + err.Error()
		return v
	}
	v.ISResult = isRes
	v.ISScore = Score(isRes, nil, scoreCfg)

	if len(oosBars) == 0 {
		v.FailReason = "no OOS data"
		return v
	}

	// OOS backtest
	oosRunner := backtest.NewRunner(cfg)
	oosRunner.LoadBars(strategy.Symbol(), oosBars)
	oosRes, err := oosRunner.Run(strategy)
	if err != nil {
		v.FailReason = "OOS backtest failed: " + err.Error()
		return v
	}
	v.OOSResult = oosRes
	v.OOSScore = Score(oosRes, nil, scoreCfg)

	// Combined score with OOS penalty
	combinedScore := Score(isRes, oosRes, scoreCfg)
	v.Passed = combinedScore.PassedOOS
	if !v.Passed {
		v.FailReason = "OOS performance degraded significantly (overfit detected)"
	}

	return v
}

// WalkForwardConfig configures rolling walk-forward validation.
type WalkForwardConfig struct {
	WindowSize   int     `json:"window_size"`   // IS window bars
	TestSize     int     `json:"test_size"`     // OOS window bars
	StepSize     int     `json:"step_size"`     // step between folds
	MinFoldScore float64 `json:"min_fold_score"` // minimum score per fold
}

// WalkForwardResult holds results from walk-forward analysis.
type WalkForwardResult struct {
	Folds      []*OOSValidation `json:"folds"`
	AvgScore   float64          `json:"avg_score"`
	StdScore   float64          `json:"std_score"`
	Consistency float64         `json:"consistency"` // 1.0 = all folds identical
	PassedFolds int             `json:"passed_folds"`
}

// WalkForward runs rolling walk-forward validation.
func WalkForward(
	bars []model.Bar,
	strategy backtest.BacktestStrategy,
	cfg backtest.RunnerConfig,
	scoreCfg ScoreConfig,
	wfCfg WalkForwardConfig,
) *WalkForwardResult {
	result := &WalkForwardResult{Folds: make([]*OOSValidation, 0)}

	if wfCfg.StepSize <= 0 {
		wfCfg.StepSize = wfCfg.TestSize
	}

	for start := 0; start+wfCfg.WindowSize+wfCfg.TestSize <= len(bars); start += wfCfg.StepSize {
		isBars := bars[start : start+wfCfg.WindowSize]
		oosBars := bars[start+wfCfg.WindowSize : start+wfCfg.WindowSize+wfCfg.TestSize]

		fold := ValidateOOS(isBars, oosBars, strategy, cfg, scoreCfg)
		result.Folds = append(result.Folds, fold)
		if fold.Passed {
			result.PassedFolds++
		}
	}

	if len(result.Folds) == 0 {
		return result
	}

	// Compute average and std of total scores
	var sum, sumSq float64
	for _, fold := range result.Folds {
		if fold.ISScore != nil {
			s := fold.ISScore.TotalScore
			sum += s
			sumSq += s * s
		}
	}
	n := float64(len(result.Folds))
	result.AvgScore = sum / n
	variance := sumSq/n - result.AvgScore*result.AvgScore
	if variance < 0 {
		variance = 0
	}
	result.StdScore = variance

	// Consistency = 1 - CV (coefficient of variation)
	if result.AvgScore > 0 {
		cv := result.StdScore / result.AvgScore
		result.Consistency = 1 - cv
		if result.Consistency < 0 {
			result.Consistency = 0
		}
	}

	return result
}
