package cra

import (
	"math"
	"strings"

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
	return MACDBullishWithTunables(bars, MACDTunables{})
}

// MACDBearish returns true if the MACD histogram turned negative at the last bar.
func MACDBearish(bars []model.Bar) bool {
	return MACDBearishWithTunables(bars, MACDTunables{})
}

// MACDTunables carries user-configured MACD periods (A3); zero fields fall
// back to the engine's historical hardcoded defaults (12/26/9).
type MACDTunables struct {
	Fast   int
	Slow   int
	Signal int
}

func (t MACDTunables) withDefaults() MACDTunables {
	if t.Fast < 1 {
		t.Fast = 12
	}
	if t.Slow < 1 {
		t.Slow = 26
	}
	if t.Signal < 1 {
		t.Signal = 9
	}
	return t
}

// EMATunables carries user-configured dual-EMA periods (A3); zero fields fall
// back to the engine's historical hardcoded defaults (5/15).
type EMATunables struct {
	Fast int
	Slow int
}

func (t EMATunables) withDefaults() EMATunables {
	if t.Fast < 1 {
		t.Fast = 5
	}
	if t.Slow < 1 {
		t.Slow = 15
	}
	return t
}

// MACDBullishWithTunables 是 MACDBullish 的参数化版本（金叉=柱值由负变正，
// 币富语义不变，仅周期可配）。
func MACDBullishWithTunables(bars []model.Bar, t MACDTunables) bool {
	t = t.withDefaults()
	closes := BarsToCloses(bars)
	_, _, hist := MACD(closes, t.Fast, t.Slow, t.Signal)
	if len(hist) < 2 {
		return false
	}
	last := len(hist) - 1
	return hist[last-1] <= 0 && hist[last] > 0
}

// MACDBearishWithTunables 是 MACDBearish 的参数化版本（死叉=柱值由正变负）。
func MACDBearishWithTunables(bars []model.Bar, t MACDTunables) bool {
	t = t.withDefaults()
	closes := BarsToCloses(bars)
	_, _, hist := MACD(closes, t.Fast, t.Slow, t.Signal)
	if len(hist) < 2 {
		return false
	}
	last := len(hist) - 1
	return hist[last-1] >= 0 && hist[last] < 0
}

// InflectionUp detects an upward turning point on closes: the latest close
// rebounds while the previous one was still falling or flat
// (close[-1] > close[-2] && close[-2] <= close[-3]).
func InflectionUp(closes []float64) bool {
	n := len(closes)
	if n < 3 {
		return false
	}
	return closes[n-1] > closes[n-2] && closes[n-2] <= closes[n-3]
}

// BollingerTunables carries user-configured Bollinger band parameters; zero
// fields fall back to the standard defaults (period 20, std multiplier 2).
type BollingerTunables struct {
	Period int
	Std    float64
}

func (t BollingerTunables) withDefaults() BollingerTunables {
	if t.Period < 2 {
		t.Period = 20
	}
	if t.Std <= 0 {
		t.Std = 2
	}
	return t
}

// BollingerBands returns upper/mid/lower bands (SMA ± stdMult·population
// stddev). Output index j corresponds to closes index j+period-1; nil when the
// series is shorter than period.
func BollingerBands(closes []float64, period int, stdMult float64) (upper, mid, lower []float64) {
	if period < 1 || len(closes) < period || stdMult <= 0 {
		return nil, nil, nil
	}
	n := len(closes) - period + 1
	upper = make([]float64, n)
	mid = make([]float64, n)
	lower = make([]float64, n)
	var sum, sumSq float64
	for i := 0; i < period; i++ {
		sum += closes[i]
		sumSq += closes[i] * closes[i]
	}
	fill := func(j int) {
		mean := sum / float64(period)
		variance := sumSq/float64(period) - mean*mean
		if variance < 0 { // 滚动累加的浮点误差钳位
			variance = 0
		}
		sd := math.Sqrt(variance)
		mid[j] = mean
		upper[j] = mean + stdMult*sd
		lower[j] = mean - stdMult*sd
	}
	fill(0)
	for i := period; i < len(closes); i++ {
		sum += closes[i] - closes[i-period]
		sumSq += closes[i]*closes[i] - closes[i-period]*closes[i-period]
		fill(i - period + 1)
	}
	return upper, mid, lower
}

// BollingerConfirmed 布林带开仓门槛（与既有 EMA/MACD 门槛同风格，参数化
// period/std）：做多 = 上一根收盘跌破下轨、末根收回轨内（假跌破反转），或
// 末根收盘仍低于下轨且收盘序列出现向上拐点；做空镜像（突破上轨后收回轨内，
// 或收盘高于上轨出现向下拐点）。序列不足 period+1 时不确认。
func BollingerConfirmed(bars []model.Bar, t BollingerTunables, side PositionSide) bool {
	t = t.withDefaults()
	closes := BarsToCloses(bars)
	if len(closes) < t.Period+1 {
		return false
	}
	upper, _, lower := BollingerBands(closes, t.Period, t.Std)
	if len(upper) < 2 {
		return false
	}
	// 带序列下标 j 对齐收盘价下标 j+period-1：末根/前一根对应带序列末两项。
	lb := len(upper) - 1
	lastC, prevC := closes[len(closes)-1], closes[len(closes)-2]
	if side == SideShort {
		recoverIn := prevC > upper[lb-1] && lastC < upper[lb]
		return recoverIn || (lastC > upper[lb] && InflectionDown(closes))
	}
	recoverIn := prevC < lower[lb-1] && lastC > lower[lb]
	return recoverIn || (lastC < lower[lb] && InflectionUp(closes))
}

// InflectionDown detects a downward turning point (mirror of InflectionUp).
func InflectionDown(closes []float64) bool {
	n := len(closes)
	if n < 3 {
		return false
	}
	return closes[n-1] < closes[n-2] && closes[n-2] >= closes[n-3]
}

// TrendEmaConfirmed 顺势 EMA 门槛（A4，顺向确认；币富"均线以上+快线拐点"
// 语义的确定性实现）：做多要求快线 > 慢线且收盘价站稳慢线之上；做空镜像。
func TrendEmaConfirmed(bars []model.Bar, t EMATunables, side PositionSide) bool {
	t = t.withDefaults()
	closes := BarsToCloses(bars)
	if len(closes) == 0 {
		return false
	}
	emaFast := EMA(closes, t.Fast)
	emaSlow := EMA(closes, t.Slow)
	last := len(closes) - 1
	if side == SideShort {
		return emaFast[last] < emaSlow[last] && closes[last] < emaSlow[last]
	}
	return emaFast[last] > emaSlow[last] && closes[last] > emaSlow[last]
}

// CounterEmaConfirmed 逆势 EMA 门槛（A4，反转确认；币富"EMA 标准线以上做空/
// 以下做多、振幅决定时机"语义）：逆势做多要求收盘价低于慢线（偏离）且出现
// 向上拐点（回归）；逆势做空镜像（高于慢线 + 向下拐点）。
func CounterEmaConfirmed(bars []model.Bar, t EMATunables, side PositionSide) bool {
	t = t.withDefaults()
	closes := BarsToCloses(bars)
	if len(closes) < 3 {
		return false
	}
	emaSlow := EMA(closes, t.Slow)
	last := len(closes) - 1
	if side == SideShort {
		return closes[last] > emaSlow[last] && InflectionDown(closes)
	}
	return closes[last] < emaSlow[last] && InflectionUp(closes)
}

// legacyEmaConfirmed 是旧版共享 EMA 门槛（add_ema 补仓门槛沿用，行为不变）：
// 做多=金叉(快/慢)或收盘价站上慢线；做空=无金叉且收于慢线之下。
func legacyEmaConfirmed(bars []model.Bar, t EMATunables, side PositionSide) bool {
	t = t.withDefaults()
	if len(bars) == 0 {
		return false
	}
	closes := BarsToCloses(bars)
	emaSlow := EMA(closes, t.Slow)
	last := len(bars) - 1
	if side == SideShort {
		return !EMACrossDetect(bars, t.Fast, t.Slow) && bars[last].Close < emaSlow[last]
	}
	return EMACrossDetect(bars, t.Fast, t.Slow) || bars[last].Close > emaSlow[last]
}

// IndicatorConfirmed decides whether an entry/add-position is allowed by the configured indicator switch.
func IndicatorConfirmed(bars []model.Bar, enabled bool, period string, indicatorType string, side PositionSide) bool {
	return IndicatorConfirmedWithTunables(bars, enabled, period, indicatorType, side, MACDTunables{}, EMATunables{})
}

// IndicatorConfirmedWithTunables 是 IndicatorConfirmed 的参数化版本（A3/A4）：
// macd/ema 周期缺省维持历史硬编码；indicatorType 额外支持 "ema_trend"
// （顺势 EMA，A4 顺向确认）与 "ema_counter"（逆势 EMA，A4 反转确认），
// 旧 "ema" 共享语义保留给补仓门槛。
func IndicatorConfirmedWithTunables(bars []model.Bar, enabled bool, period string, indicatorType string, side PositionSide, macd MACDTunables, ema EMATunables) bool {
	if !enabled || period == "close" {
		return true
	}
	// The gateway bar feed resolution is matched to the configured timeframe by
	// the strategy (A2 multi-timeframe feed); the indicator runs on the bar
	// history supplied by the caller with standard lookback windows.
	switch indicatorType {
	case "ema":
		return legacyEmaConfirmed(bars, ema, side)
	case "ema_trend":
		return TrendEmaConfirmed(bars, ema, side)
	case "ema_counter":
		return CounterEmaConfirmed(bars, ema, side)
	case "macd":
		if side == SideShort {
			return MACDBearishWithTunables(bars, macd)
		}
		return MACDBullishWithTunables(bars, macd)
	}
	return true
}

// IsFeedablePeriod 报告 period 是否为可经 kline_feeder 订阅的指标周期档位
// （币富 5m/15m/30m/1h/4h/8h，均为 Binance 支持的 interval）。"close"/空串/
// 未知值不可订阅，由调用方降级为工作周期。
func IsFeedablePeriod(period string) bool {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "5m", "15m", "30m", "1h", "4h", "8h":
		return true
	}
	return false
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
