package cra

import (
	"github.com/xiaotian-quant/gateway/internal/model"
)

// EMA computes the exponential moving average over closes.
func EMA(closes []float64, period int) []float64 {
	if period <= 0 || len(closes) == 0 {
		return nil
	}
	multiplier := 2.0 / float64(period+1)
	out := make([]float64, len(closes))
	out[0] = closes[0]
	for i := 1; i < len(closes); i++ {
		out[i] = (closes[i]-out[i-1])*multiplier + out[i-1]
	}
	return out
}

// SMA computes simple moving average.
func SMA(closes []float64, period int) []float64 {
	if period <= 0 || len(closes) < period {
		return nil
	}
	out := make([]float64, len(closes)-period+1)
	var sum float64
	for i := 0; i < period; i++ {
		sum += closes[i]
	}
	out[0] = sum / float64(period)
	for i := period; i < len(closes); i++ {
		sum += closes[i] - closes[i-period]
		out[i-period+1] = sum / float64(period)
	}
	return out
}

// MACD returns the MACD line, signal line and histogram for the given closes.
func MACD(closes []float64, fast, slow, signal int) ([]float64, []float64, []float64) {
	emaFast := EMA(closes, fast)
	emaSlow := EMA(closes, slow)
	if len(emaFast) != len(emaSlow) || len(emaFast) == 0 {
		return nil, nil, nil
	}
	macd := make([]float64, len(emaFast))
	for i := range emaFast {
		macd[i] = emaFast[i] - emaSlow[i]
	}
	signalLine := EMA(macd, signal)
	offset := len(macd) - len(signalLine)
	hist := make([]float64, len(signalLine))
	for i := range signalLine {
		hist[i] = macd[offset+i] - signalLine[i]
	}
	return macd, signalLine, hist
}

// BarsToCloses extracts close prices from bars in chronological order.
func BarsToCloses(bars []model.Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Close
	}
	return out
}

// EMACrossDetect returns true if the fast EMA crossed above the slow EMA at the last bar.
func EMACrossDetect(bars []model.Bar, fast, slow int) bool {
	closes := BarsToCloses(bars)
	emaFast := EMA(closes, fast)
	emaSlow := EMA(closes, slow)
	if len(emaFast) < 2 || len(emaSlow) < 2 {
		return false
	}
	last := len(emaFast) - 1
	return emaFast[last-1] <= emaSlow[last-1] && emaFast[last] > emaSlow[last]
}

// MACDBullish returns true if the MACD histogram turned positive at the last bar.
func MACDBullish(bars []model.Bar) bool {
	closes := BarsToCloses(bars)
	_, _, hist := MACD(closes, 12, 26, 9)
	if len(hist) < 2 {
		return false
	}
	last := len(hist) - 1
	return hist[last-1] <= 0 && hist[last] > 0
}

// MACDBearish returns true if the MACD histogram turned negative at the last bar.
func MACDBearish(bars []model.Bar) bool {
	closes := BarsToCloses(bars)
	_, _, hist := MACD(closes, 12, 26, 9)
	if len(hist) < 2 {
		return false
	}
	last := len(hist) - 1
	return hist[last-1] >= 0 && hist[last] < 0
}

// IndicatorConfirmed decides whether an entry/add-position is allowed by the configured indicator switch.
func IndicatorConfirmed(bars []model.Bar, enabled bool, period string, indicatorType string, side PositionSide) bool {
	if !enabled || period == "close" {
		return true
	}
	// Map frontend periods to approximate bar counts. The gateway bar feed resolution
	// is assumed to match the configured timeframe; here we simply run the indicator
	// on the available bar history with standard lookback windows.
	switch indicatorType {
	case "ema":
		// Trend EMA: bullish for long, bearish for short.
		if side == SideShort {
			return EMACrossDetect(bars, 5, 15) == false && len(bars) > 0 && bars[len(bars)-1].Close < EMA(BarsToCloses(bars), 15)[len(bars)-1]
		}
		return EMACrossDetect(bars, 5, 15) || (len(bars) > 0 && bars[len(bars)-1].Close > EMA(BarsToCloses(bars), 15)[len(bars)-1])
	case "macd":
		if side == SideShort {
			return MACDBearish(bars)
		}
		return MACDBullish(bars)
	}
	return true
}

// PeriodToBars maps a frontend period string to a recommended number of bars for indicator lookback.
func PeriodToBars(period string) int {
	switch period {
	case "5m":
		return 20
	case "15m":
		return 20
	default:
		return 50
	}
}
