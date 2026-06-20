package indicators

import (
	"fmt"
)

// MACDIndicator implements the Moving Average Convergence Divergence (MACD)
// indicator for identifying trend direction and momentum changes.
// MACD = Fast EMA - Slow EMA
// Signal = EMA of MACD over signal period
type MACDIndicator struct {
	FastPeriod   int    // Fast EMA period (default 12)
	SlowPeriod   int    // Slow EMA period (default 26)
	SignalPeriod int    // Signal line EMA period (default 9)
	Timeframe    string // "5m", "15m", "30m", "1h", "4h", "8h"
}

// NewMACDIndicator creates a MACD indicator with default parameters.
func NewMACDIndicator() *MACDIndicator {
	return &MACDIndicator{
		FastPeriod:   12,
		SlowPeriod:   26,
		SignalPeriod: 9,
		Timeframe:    "1h",
	}
}

// NewMACDIndicatorWithParams creates a MACD indicator with custom parameters.
func NewMACDIndicatorWithParams(fast, slow, signal int, timeframe string) *MACDIndicator {
	if fast <= 0 {
		fast = 12
	}
	if slow <= 0 {
		slow = 26
	}
	if signal <= 0 {
		signal = 9
	}
	if timeframe == "" {
		timeframe = "1h"
	}
	return &MACDIndicator{
		FastPeriod:   fast,
		SlowPeriod:   slow,
		SignalPeriod: signal,
		Timeframe:    timeframe,
	}
}

// Validate checks that indicator parameters are valid.
func (m *MACDIndicator) Validate() error {
	if m.FastPeriod >= m.SlowPeriod {
		return fmt.Errorf("fast period (%d) must be less than slow period (%d)", m.FastPeriod, m.SlowPeriod)
	}
	if m.SignalPeriod <= 0 {
		return fmt.Errorf("signal period must be positive, got %d", m.SignalPeriod)
	}
	return nil
}

// Calculate computes the MACD line, signal line, and histogram values.
// Returns (macdLine, signalLine, histogram).
func (m *MACDIndicator) Calculate(closes []float64) (macdLine, signalLine, histogram float64) {
	if len(closes) < m.SlowPeriod+m.SignalPeriod {
		return 0, 0, 0
	}

	fastEMA := ema(closes, m.FastPeriod)
	slowEMA := ema(closes, m.SlowPeriod)
	macdLine = fastEMA - slowEMA

	// Compute signal line as EMA of MACD values
	macdVals := make([]float64, m.SignalPeriod+1)
	for i := 0; i <= m.SignalPeriod && i < len(closes); i++ {
		slice := closes[:len(closes)-i]
		if len(slice) >= m.SlowPeriod {
			macdVals[i] = ema(slice, m.FastPeriod) - ema(slice, m.SlowPeriod)
		}
	}
	signalLine = ema(macdVals, m.SignalPeriod)
	histogram = macdLine - signalLine

	return macdLine, signalLine, histogram
}

// IsGoldenCross returns true when MACD line crosses above the signal line.
// This is a bullish entry signal.
func (m *MACDIndicator) IsGoldenCross(closes []float64) bool {
	if len(closes) < m.SlowPeriod+m.SignalPeriod+1 {
		return false
	}

	prevMACD, prevSignal, _ := m.Calculate(closes[:len(closes)-1])
	currMACD, currSignal, _ := m.Calculate(closes)

	return prevMACD <= prevSignal && currMACD > currSignal
}

// IsDeathCross returns true when MACD line crosses below the signal line.
// This is a bearish entry signal.
func (m *MACDIndicator) IsDeathCross(closes []float64) bool {
	if len(closes) < m.SlowPeriod+m.SignalPeriod+1 {
		return false
	}

	prevMACD, prevSignal, _ := m.Calculate(closes[:len(closes)-1])
	currMACD, currSignal, _ := m.Calculate(closes)

	return prevMACD >= prevSignal && currMACD < currSignal
}

// GetSignal returns the current MACD signal as a string: "buy", "sell", or "neutral".
func (m *MACDIndicator) GetSignal(closes []float64) string {
	if len(closes) < m.SlowPeriod+m.SignalPeriod {
		return "neutral"
	}

	macdLine, signalLine, histogram := m.Calculate(closes)

	if histogram > 0 && macdLine > 0 {
		return "buy"
	}
	if histogram < 0 && macdLine < 0 {
		return "sell"
	}

	// Check for crossovers even when signs differ
	if macdLine > signalLine {
		return "buy"
	}
	if macdLine < signalLine {
		return "sell"
	}

	return "neutral"
}

// GetHistogram returns the MACD histogram value (MACD - Signal).
// Positive histogram = bullish momentum; negative = bearish momentum.
func (m *MACDIndicator) GetHistogram(closes []float64) float64 {
	_, _, hist := m.Calculate(closes)
	return hist
}

// Divergence detects bullish or bearish divergence between price and MACD.
// Returns "bullish", "bearish", or "" when no divergence is detected.
func (m *MACDIndicator) Divergence(closes []float64) string {
	if len(closes) < m.SlowPeriod*3 {
		return ""
	}

	// Split into two halves
	mid := len(closes) / 2
	firstHalf := closes[:mid]
	secondHalf := closes[mid:]

	// Price extremes
	firstLow, firstHigh := minMax(firstHalf)
	secondLow, secondHigh := minMax(secondHalf)

	// MACD extremes
	_, _, firstHist := m.Calculate(firstHalf)
	_, _, secondHist := m.Calculate(secondHalf)

	// Bullish divergence: price makes lower low but MACD makes higher low
	if secondLow < firstLow && secondHist > firstHist {
		return "bullish"
	}

	// Bearish divergence: price makes higher high but MACD makes lower high
	if secondHigh > firstHigh && secondHist < firstHist {
		return "bearish"
	}

	return ""
}

// ── Internal helpers ────────────────────────────────────────────

// ema computes the exponential moving average of the last 'period' values.
func ema(data []float64, period int) float64 {
	if len(data) == 0 {
		return 0
	}
	if period > len(data) {
		period = len(data)
	}
	if period == 1 {
		return data[len(data)-1]
	}

	alpha := 2.0 / float64(period+1)
	// Start with SMA of first 'period' points
	sum := 0.0
	start := len(data) - period
	if start < 0 {
		start = 0
	}
	for i := start; i < len(data) && i < start+period; i++ {
		sum += data[i]
	}
	e := sum / float64(period)

	// Apply EMA formula for remaining points
	for i := start + period; i < len(data); i++ {
		e = alpha*data[i] + (1-alpha)*e
	}
	return e
}

func minMax(data []float64) (min, max float64) {
	if len(data) == 0 {
		return 0, 0
	}
	min = data[0]
	max = data[0]
	for _, v := range data[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}
