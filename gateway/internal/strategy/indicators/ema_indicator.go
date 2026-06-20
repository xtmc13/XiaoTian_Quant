package indicators

import (
	"fmt"
	"math"
)

// EMAIndicator implements Exponential Moving Average analysis for trend
// detection, counter-trend signals, and inflection point identification.
type EMAIndicator struct {
	Period    int    // EMA period (default 60, also supports 4, 10)
	Timeframe string // "5m", "15m", "30m", "1h", "4h", "8h", "1d"
}

// NewEMAIndicator creates an EMA indicator with default parameters.
func NewEMAIndicator() *EMAIndicator {
	return &EMAIndicator{
		Period:    60,
		Timeframe: "1h",
	}
}

// NewEMAIndicatorWithParams creates an EMA indicator with custom parameters.
func NewEMAIndicatorWithParams(period int, timeframe string) *EMAIndicator {
	if period <= 0 {
		period = 60
	}
	if timeframe == "" {
		timeframe = "1h"
	}
	return &EMAIndicator{
		Period:    period,
		Timeframe: timeframe,
	}
}

// Validate checks that indicator parameters are valid.
func (e *EMAIndicator) Validate() error {
	if e.Period <= 0 {
		return fmt.Errorf("EMA period must be positive, got %d", e.Period)
	}
	return nil
}

// Calculate computes the EMA value for the given price series.
func (e *EMAIndicator) Calculate(closes []float64) float64 {
	return ema(closes, e.Period)
}

// IsCounterTrend implements EMA counter-trend logic:
// When price is above EMA60, the trend is up -> signal to go short (counter-trend).
// When price is below EMA60, the trend is down -> signal to go long (counter-trend).
func (e *EMAIndicator) IsCounterTrend(currentPrice, emaValue float64) bool {
	if emaValue <= 0 {
		return false
	}
	// Above EMA -> counter-trend suggests short
	// Below EMA -> counter-trend suggests long
	return currentPrice > emaValue*1.001 || currentPrice < emaValue*0.999
}

// CounterTrendSignal returns the counter-trend direction based on EMA position.
// Returns "short" when price is above EMA, "long" when below, "neutral" when near.
func (e *EMAIndicator) CounterTrendSignal(currentPrice, emaValue float64) string {
	if emaValue <= 0 {
		return "neutral"
	}
	// Define a small tolerance zone around EMA
	tolerance := emaValue * 0.0005 // 0.05% tolerance
	if currentPrice > emaValue+tolerance {
		return "short"
	}
	if currentPrice < emaValue-tolerance {
		return "long"
	}
	return "neutral"
}

// IsTrendFollowing implements EMA trend-following logic using EMA10 and EMA60:
// - EMA60 determines the major trend direction
// - EMA10 provides entry timing within the trend
// - Go long when EMA10 > EMA60 AND price > EMA10
// - Go short when EMA10 < EMA60 AND price < EMA10
func (e *EMAIndicator) IsTrendFollowing(currentPrice, ema10, ema60 float64) bool {
	if ema10 <= 0 || ema60 <= 0 {
		return false
	}

	// Strong uptrend: price above EMA10 and EMA10 above EMA60
	inUptrend := currentPrice > ema10 && ema10 > ema60
	// Strong downtrend: price below EMA10 and EMA10 below EMA60
	inDowntrend := currentPrice < ema10 && ema10 < ema60

	return inUptrend || inDowntrend
}

// TrendFollowingSignal returns the trend-following direction.
// Returns "long" for uptrend, "short" for downtrend, "neutral" otherwise.
func (e *EMAIndicator) TrendFollowingSignal(currentPrice, ema10, ema60 float64) string {
	if ema10 <= 0 || ema60 <= 0 {
		return "neutral"
	}
	if currentPrice > ema10 && ema10 > ema60 {
		return "long"
	}
	if currentPrice < ema10 && ema10 < ema60 {
		return "short"
	}
	return "neutral"
}

// IsInflectionPoint detects potential trend reversal points by analyzing
// price behavior around the EMA. An inflection point occurs when:
// 1. Price crosses the EMA (potential trend change)
// 2. Price diverges significantly from EMA after extended alignment
func (e *EMAIndicator) IsInflectionPoint(prices []float64) bool {
	if len(prices) < e.Period+3 {
		return false
	}

	emaVal := e.Calculate(prices)
	if emaVal <= 0 {
		return false
	}

	n := len(prices)
	prevPrice := prices[n-2]
	currPrice := prices[n-1]
	prev2Price := prices[n-3]

	// Check for EMA cross (prev price on one side, current on the other)
	prevAbove := prevPrice > emaVal
	currAbove := currPrice > emaVal

	// Inflection point: price crossed the EMA
	if prevAbove != currAbove {
		return true
	}

	// Check for double-touch pattern: price approaches EMA and bounces
	// prev2 was moving toward EMA, prev touched or crossed near, curr bounced away
	distPrev2 := math.Abs(prev2Price - emaVal)
	distPrev := math.Abs(prevPrice - emaVal)
	distCurr := math.Abs(currPrice - emaVal)

	// Price approached EMA and then moved away (bounce pattern)
	if distPrev < distPrev2 && distCurr > distPrev {
		return true
	}

	return false
}

// InflectionDirection returns the likely direction after an inflection point.
// Returns "up" for bullish reversal, "down" for bearish reversal, "" if not an inflection point.
func (e *EMAIndicator) InflectionDirection(prices []float64) string {
	if !e.IsInflectionPoint(prices) {
		return ""
	}

	n := len(prices)
	emaVal := e.Calculate(prices)
	currPrice := prices[n-1]

	// Price crossing above EMA = bullish
	if currPrice > emaVal {
		return "up"
	}
	return "down"
}

// Crossover detects EMA crossover events between two EMA periods.
// Returns "golden_cross" when fast crosses above slow, "death_cross" when fast crosses below,
// and "" when no crossover occurred.
func Crossover(closes []float64, fastPeriod, slowPeriod int) string {
	if len(closes) < slowPeriod+1 {
		return ""
	}

	fastCurrent := ema(closes, fastPeriod)
	slowCurrent := ema(closes, slowPeriod)

	fastPrev := ema(closes[:len(closes)-1], fastPeriod)
	slowPrev := ema(closes[:len(closes)-1], slowPeriod)

	if fastPrev <= slowPrev && fastCurrent > slowCurrent {
		return "golden_cross"
	}
	if fastPrev >= slowPrev && fastCurrent < slowCurrent {
		return "death_cross"
	}

	return ""
}

// CalculateDualEMA computes both EMA10 and EMA60 values for trend analysis.
func CalculateDualEMA(closes []float64) (ema10, ema60 float64) {
	if len(closes) < 60 {
		return 0, 0
	}
	return ema(closes, 10), ema(closes, 60)
}

// TrendStrength measures how strongly price follows the EMA trend.
// Returns a value between 0 and 1, where higher values indicate stronger trend alignment.
func (e *EMAIndicator) TrendStrength(prices []float64) float64 {
	if len(prices) < e.Period+5 {
		return 0
	}

	emaVal := e.Calculate(prices)
	if emaVal <= 0 {
		return 0
	}

	// Count how many of the last 5 candles are on the same side of EMA
	aligned := 0
	n := len(prices)
	for i := n - 5; i < n; i++ {
		if i < 0 {
			continue
		}
		if (prices[i] > emaVal && prices[n-1] > emaVal) || (prices[i] < emaVal && prices[n-1] < emaVal) {
			aligned++
		}
	}

	return float64(aligned) / 5.0
}
