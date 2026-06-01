package data

import (
	"fmt"
	"math"
)

// ── Validation Result ─────────────────────────────────────────

// ValidationIssue describes a single data quality issue.
type ValidationIssue struct {
	Symbol    string `json:"symbol"`
	Interval  string `json:"interval"`
	Type      string `json:"type"`      // "gap", "anomaly", "zero_price", "negative_volume"
	Timestamp int64  `json:"timestamp"`  // bar time where issue found
	Message   string `json:"message"`
}

// ValidationResult holds the results of a data validation run.
type ValidationResult struct {
	TotalBars   int               `json:"total_bars"`
	PassedBars  int               `json:"passed_bars"`
	IssueCount  int               `json:"issue_count"`
	Issues      []ValidationIssue `json:"issues"`
	GapsFound   int               `json:"gaps_found"`
	Anomalies   int               `json:"anomalies"`
}

// ── Validator ──────────────────────────────────────────────────

// Validator checks OHLCV data for quality issues.
type Validator struct {
	maxGapMinutes int     // max allowed gap between bars (in minutes)
	maxPriceMove  float64 // max allowed bar-to-bar price move (ratio)
	minPrice      float64 // minimum allowed price
}

// NewValidator creates a new data validator.
func NewValidator(maxGapMin int, maxPriceMove float64) *Validator {
	if maxGapMin <= 0 {
		maxGapMin = 5
	}
	if maxPriceMove <= 0 {
		maxPriceMove = 0.25 // 25% move
	}
	return &Validator{
		maxGapMinutes: maxGapMin,
		maxPriceMove:  maxPriceMove,
		minPrice:      1e-10,
	}
}

// Validate checks a slice of OHLCV bars for quality issues.
func (v *Validator) Validate(bars []OHLCV) *ValidationResult {
	result := &ValidationResult{
		TotalBars: len(bars),
	}
	if len(bars) == 0 {
		return result
	}

	intervalMs := intervalToMs(bars[0].Interval)
	expectedGapMs := int64(v.maxGapMinutes) * 60000

	for i, bar := range bars {
		valid := true

		// Check for zero/negative prices
		if bar.Open <= v.minPrice || bar.High <= v.minPrice || bar.Low <= v.minPrice || bar.Close <= v.minPrice {
			result.Issues = append(result.Issues, ValidationIssue{
				Symbol:    bar.Symbol,
				Interval:  bar.Interval,
				Type:      "zero_price",
				Timestamp: bar.Time,
				Message:   fmt.Sprintf("zero or negative price: O=%.8f H=%.8f L=%.8f C=%.8f", bar.Open, bar.High, bar.Low, bar.Close),
			})
			valid = false
		}

		// Check for negative volume
		if bar.Volume < 0 {
			result.Issues = append(result.Issues, ValidationIssue{
				Symbol:    bar.Symbol,
				Interval:  bar.Interval,
				Type:      "negative_volume",
				Timestamp: bar.Time,
				Message:   fmt.Sprintf("negative volume: %.6f", bar.Volume),
			})
			valid = false
		}

		// Check for OHLC consistency
		if bar.High < bar.Open || bar.High < bar.Close || bar.Low > bar.Open || bar.Low > bar.Close {
			result.Issues = append(result.Issues, ValidationIssue{
				Symbol:    bar.Symbol,
				Interval:  bar.Interval,
				Type:      "anomaly",
				Timestamp: bar.Time,
				Message:   fmt.Sprintf("OHLC inconsistency: O=%.2f H=%.2f L=%.2f C=%.2f", bar.Open, bar.High, bar.Low, bar.Close),
			})
			valid = false
			result.Anomalies++
		}

		// Check for gap from previous bar
		if i > 0 {
			prevBar := bars[i-1]
			gap := bar.Time - prevBar.Time
			expectedGap := intervalMs

			if gap > expectedGap+expectedGapMs {
				missingBars := (gap - expectedGap) / intervalMs
				result.Issues = append(result.Issues, ValidationIssue{
					Symbol:    bar.Symbol,
					Interval:  bar.Interval,
					Type:      "gap",
					Timestamp: prevBar.Time + intervalMs,
					Message:   fmt.Sprintf("missing ~%d bars between %d and %d", missingBars, prevBar.Time, bar.Time),
				})
				result.GapsFound++
				valid = false
			}

			// Check for extreme price move
			if prevBar.Close > 0 {
				move := math.Abs(bar.Close-prevBar.Close) / prevBar.Close
				if move > v.maxPriceMove {
					result.Issues = append(result.Issues, ValidationIssue{
						Symbol:    bar.Symbol,
						Interval:  bar.Interval,
						Type:      "anomaly",
						Timestamp: bar.Time,
						Message:   fmt.Sprintf("extreme price move: %.1f%% (%.2f → %.2f)", move*100, prevBar.Close, bar.Close),
					})
					result.Anomalies++
					valid = false
				}
			}
		}

		if valid {
			result.PassedBars++
		}
	}

	result.IssueCount = len(result.Issues)
	return result
}

// QuickCheck does a fast pass to check if data exists for a range.
func (v *Validator) QuickCheck(bars []OHLCV, expectedStart, expectedEnd int64) []string {
	var warnings []string

	if len(bars) == 0 {
		warnings = append(warnings, "no data at all")
		return warnings
	}

	if bars[0].Time > expectedStart {
		gap := (bars[0].Time - expectedStart) / intervalToMs(bars[0].Interval)
		warnings = append(warnings, fmt.Sprintf("first bar at %d, expected %d (gap of ~%d bars)", bars[0].Time, expectedStart, gap))
	}

	if bars[len(bars)-1].Time < expectedEnd {
		gap := (expectedEnd - bars[len(bars)-1].Time) / intervalToMs(bars[0].Interval)
		warnings = append(warnings, fmt.Sprintf("last bar at %d, expected %d (gap of ~%d bars)", bars[len(bars)-1].Time, expectedEnd, gap))
	}

	return warnings
}
