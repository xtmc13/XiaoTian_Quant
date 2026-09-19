package factors

import (
	"math"
	"sort"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 序列计算原语 ──
// 各函数输出与输入等长；窗口不足处填 NaN（评价层会跳过 NaN）。

func closesOf(bars []model.Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Close
	}
	return out
}

func highsOf(bars []model.Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.High
	}
	return out
}

func lowsOf(bars []model.Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Low
	}
	return out
}

func volumesOf(bars []model.Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Volume
	}
	return out
}

func nanSeries(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
	}
	return out
}

// smaSeries 简单移动平均序列（窗口内必须全是有效值才输出）。
func smaSeries(in []float64, period int) []float64 {
	out := nanSeries(len(in))
	if period <= 0 || len(in) < period {
		return out
	}
	sum := 0.0
	valid := 0
	for i, v := range in {
		if !math.IsNaN(v) {
			sum += v
			valid++
		}
		if i >= period {
			old := in[i-period]
			if !math.IsNaN(old) {
				sum -= old
				valid--
			}
		}
		if i >= period-1 && valid == period {
			out[i] = sum / float64(period)
		}
	}
	return out
}

// emaSeries 指数移动平均序列（seed 用首个非 NaN 值）。
func emaSeries(in []float64, period int) []float64 {
	out := nanSeries(len(in))
	if period <= 0 || len(in) == 0 {
		return out
	}
	k := 2.0 / (float64(period) + 1.0)
	seeded := false
	var prev float64
	for i, v := range in {
		if math.IsNaN(v) {
			continue
		}
		if !seeded {
			prev = v
			seeded = true
			out[i] = v
			continue
		}
		prev = (v-prev)*k + prev
		out[i] = prev
	}
	return out
}

// rocSeries (in[i]/in[i-period] - 1)。
func rocSeries(in []float64, period int) []float64 {
	out := nanSeries(len(in))
	if period <= 0 {
		return out
	}
	for i := period; i < len(in); i++ {
		if in[i-period] != 0 && !math.IsNaN(in[i-period]) && !math.IsNaN(in[i]) {
			out[i] = in[i]/in[i-period] - 1
		}
	}
	return out
}

// diffSeries in[i] - in[i-period]。
func diffSeries(in []float64, period int) []float64 {
	out := nanSeries(len(in))
	if period <= 0 {
		return out
	}
	for i := period; i < len(in); i++ {
		if !math.IsNaN(in[i]) && !math.IsNaN(in[i-period]) {
			out[i] = in[i] - in[i-period]
		}
	}
	return out
}

// pctRankSeries 当前值在过去 window 个值中的分位（0..1）。
func pctRankSeries(in []float64, window int) []float64 {
	out := nanSeries(len(in))
	if window <= 1 {
		return out
	}
	for i := window - 1; i < len(in); i++ {
		v := in[i]
		if math.IsNaN(v) {
			continue
		}
		rank := 0
		for j := i - window + 1; j <= i; j++ {
			if in[j] <= v {
				rank++
			}
		}
		out[i] = float64(rank) / float64(window)
	}
	return out
}

// rollingStdSeries 滚动标准差（总体 std，窗口含当前值）。
func rollingStdSeries(in []float64, window int) []float64 {
	out := nanSeries(len(in))
	if window <= 1 || len(in) < window {
		return out
	}
	for i := window - 1; i < len(in); i++ {
		sum, sq := 0.0, 0.0
		ok := true
		for j := i - window + 1; j <= i; j++ {
			if math.IsNaN(in[j]) {
				ok = false
				break
			}
			sum += in[j]
			sq += in[j] * in[j]
		}
		if !ok {
			continue
		}
		n := float64(window)
		variance := sq/n - (sum/n)*(sum/n)
		if variance < 0 {
			variance = 0
		}
		out[i] = math.Sqrt(variance)
	}
	return out
}

// rollingMeanSeries 滚动均值（窗口内有效值不足一半则跳过）。
func rollingMeanSeries(in []float64, window int) []float64 {
	out := nanSeries(len(in))
	if window <= 0 {
		return out
	}
	for i := 0; i < len(in); i++ {
		start := i - window + 1
		if start < 0 {
			start = 0
		}
		sum, cnt := 0.0, 0
		for j := start; j <= i; j++ {
			if !math.IsNaN(in[j]) {
				sum += in[j]
				cnt++
			}
		}
		if cnt >= (i-start+1)/2 && cnt > 0 {
			out[i] = sum / float64(cnt)
		}
	}
	return out
}

// rollingMaxSeries / rollingMinSeries 滚动最大/最小（窗口含当前值）。
func rollingMaxSeries(in []float64, window int) []float64 {
	out := nanSeries(len(in))
	if window <= 0 {
		return out
	}
	for i := 0; i < len(in); i++ {
		start := i - window + 1
		if start < 0 {
			start = 0
		}
		m := math.Inf(-1)
		for j := start; j <= i; j++ {
			if !math.IsNaN(in[j]) && in[j] > m {
				m = in[j]
			}
		}
		if !math.IsInf(m, -1) {
			out[i] = m
		}
	}
	return out
}

func rollingMinSeries(in []float64, window int) []float64 {
	out := nanSeries(len(in))
	if window <= 0 {
		return out
	}
	for i := 0; i < len(in); i++ {
		start := i - window + 1
		if start < 0 {
			start = 0
		}
		m := math.Inf(1)
		for j := start; j <= i; j++ {
			if !math.IsNaN(in[j]) && in[j] < m {
				m = in[j]
			}
		}
		if !math.IsInf(m, 1) {
			out[i] = m
		}
	}
	return out
}

// returnsSeries 简单收益率序列 r[i] = in[i]/in[i-1] - 1（首元素 NaN）。
func returnsSeries(in []float64) []float64 {
	out := nanSeries(len(in))
	for i := 1; i < len(in); i++ {
		if in[i-1] != 0 && !math.IsNaN(in[i]) && !math.IsNaN(in[i-1]) {
			out[i] = in[i]/in[i-1] - 1
		}
	}
	return out
}

// trueRangeSeries 真实波幅序列。
func trueRangeSeries(bars []model.Bar) []float64 {
	out := nanSeries(len(bars))
	for i := 1; i < len(bars); i++ {
		tr1 := bars[i].High - bars[i].Low
		tr2 := math.Abs(bars[i].High - bars[i-1].Close)
		tr3 := math.Abs(bars[i].Low - bars[i-1].Close)
		out[i] = math.Max(tr1, math.Max(tr2, tr3))
	}
	return out
}

// atrSeries ATR 序列（Wilder 平滑）。
func atrSeries(bars []model.Bar, period int) []float64 {
	tr := trueRangeSeries(bars)
	out := nanSeries(len(bars))
	if period <= 0 {
		return out
	}
	seeded := false
	var prev float64
	count := 0
	for i, v := range tr {
		if math.IsNaN(v) {
			continue
		}
		count++
		if !seeded {
			if count == period {
				// seed = TR 的 period 均值
				sum := 0.0
				for j := i - period + 1; j <= i; j++ {
					sum += tr[j]
				}
				prev = sum / float64(period)
				out[i] = prev
				seeded = true
			}
			continue
		}
		prev = (prev*float64(period-1) + v) / float64(period)
		out[i] = prev
	}
	return out
}

// rsiSeries RSI 序列（Wilder 平滑）。
func rsiSeries(closes []float64, period int) []float64 {
	out := nanSeries(len(closes))
	if period <= 0 || len(closes) < period+1 {
		return out
	}
	avgGain, avgLoss := 0.0, 0.0
	for i := 1; i < len(closes); i++ {
		ch := closes[i] - closes[i-1]
		gain := math.Max(ch, 0)
		loss := math.Max(-ch, 0)
		if i <= period {
			avgGain += gain / float64(period)
			avgLoss += loss / float64(period)
			if i == period {
				out[i] = rsiOf(avgGain, avgLoss)
			}
			continue
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		out[i] = rsiOf(avgGain, avgLoss)
	}
	return out
}

func rsiOf(avgGain, avgLoss float64) float64 {
	if avgLoss == 0 {
		return 100
	}
	return 100 - 100/(1+avgGain/avgLoss)
}

// stochSeries 随机指标 %K/%D。
func stochSeries(bars []model.Bar, kPeriod, dPeriod int) (kSeries, dSeries []float64) {
	kSeries = nanSeries(len(bars))
	highs := highsOf(bars)
	lows := lowsOf(bars)
	closes := closesOf(bars)
	for i := kPeriod - 1; i < len(bars); i++ {
		hh := rollingMaxSeries(highs[i-kPeriod+1:i+1], kPeriod)[kPeriod-1]
		ll := rollingMinSeries(lows[i-kPeriod+1:i+1], kPeriod)[kPeriod-1]
		if hh == ll {
			continue
		}
		kSeries[i] = (closes[i] - ll) / (hh - ll) * 100
	}
	dSeries = smaSeries(kSeries, dPeriod)
	return kSeries, dSeries
}

// adxSeries ADX 序列（返回 adx, plusDI, minusDI）。
func adxSeries(bars []model.Bar, period int) (adx, pDI, mDI []float64) {
	n := len(bars)
	adx, pDI, mDI = nanSeries(n), nanSeries(n), nanSeries(n)
	if period <= 0 || n < period*2+1 {
		return
	}
	plusDM := make([]float64, n)
	minusDM := make([]float64, n)
	tr := trueRangeSeries(bars)
	for i := 1; i < n; i++ {
		up := bars[i].High - bars[i-1].High
		down := bars[i-1].Low - bars[i].Low
		if up > down && up > 0 {
			plusDM[i] = up
		}
		if down > up && down > 0 {
			minusDM[i] = down
		}
	}
	smoothTR := wilderSmooth(tr, period)
	smoothPlus := wilderSmooth(plusDM, period)
	smoothMinus := wilderSmooth(minusDM, period)
	dx := make([]float64, n)
	for i := 0; i < n; i++ {
		if math.IsNaN(smoothTR[i]) || smoothTR[i] == 0 {
			continue
		}
		pDI[i] = 100 * smoothPlus[i] / smoothTR[i]
		mDI[i] = 100 * smoothMinus[i] / smoothTR[i]
		if pDI[i]+mDI[i] > 0 {
			dx[i] = 100 * math.Abs(pDI[i]-mDI[i]) / (pDI[i] + mDI[i])
		}
	}
	// ADX = DX 的 Wilder 平滑
	adx = wilderSmooth(dx, period)
	return
}

// wilderSmooth Wilder 平滑（seed 为前 period 个非 NaN 值的均值）。
func wilderSmooth(in []float64, period int) []float64 {
	out := nanSeries(len(in))
	if period <= 0 {
		return out
	}
	vals := make([]float64, 0, period) // 已积累的非 NaN 值
	prev := 0.0
	seeded := false
	for i := 0; i < len(in); i++ {
		v := in[i]
		if math.IsNaN(v) {
			continue
		}
		if !seeded {
			vals = append(vals, v)
			if len(vals) < period {
				continue
			}
			sum := 0.0
			for _, x := range vals {
				sum += x
			}
			prev = sum / float64(period)
			out[i] = prev
			seeded = true
			continue
		}
		prev = (prev*float64(period-1) + v) / float64(period)
		out[i] = prev
	}
	return out
}

// obvSeries 能量潮序列（累积量，首元素 0）。
func obvSeries(bars []model.Bar) []float64 {
	out := nanSeries(len(bars))
	if len(bars) == 0 {
		return out
	}
	out[0] = 0
	for i := 1; i < len(bars); i++ {
		switch {
		case bars[i].Close > bars[i-1].Close:
			out[i] = out[i-1] + bars[i].Volume
		case bars[i].Close < bars[i-1].Close:
			out[i] = out[i-1] - bars[i].Volume
		default:
			out[i] = out[i-1]
		}
	}
	return out
}

// cciSeries 顺势指标序列。
func cciSeries(bars []model.Bar, period int) []float64 {
	n := len(bars)
	out := nanSeries(n)
	if period <= 0 || n < period {
		return out
	}
	tp := make([]float64, n)
	for i, b := range bars {
		tp[i] = (b.High + b.Low + b.Close) / 3
	}
	for i := period - 1; i < n; i++ {
		mean := 0.0
		for j := i - period + 1; j <= i; j++ {
			mean += tp[j]
		}
		mean /= float64(period)
		dev := 0.0
		for j := i - period + 1; j <= i; j++ {
			dev += math.Abs(tp[j] - mean)
		}
		dev /= float64(period)
		if dev == 0 {
			continue
		}
		out[i] = (tp[i] - mean) / (0.015 * dev)
	}
	return out
}

// mfiSeries 资金流量指标序列。
func mfiSeries(bars []model.Bar, period int) []float64 {
	n := len(bars)
	out := nanSeries(n)
	if period <= 0 || n < period+1 {
		return out
	}
	tp := make([]float64, n)
	rmf := make([]float64, n) // raw money flow, 带符号：正=流入 负=流出
	for i, b := range bars {
		tp[i] = (b.High + b.Low + b.Close) / 3
		rmf[i] = tp[i] * b.Volume
		if i > 0 && tp[i] < tp[i-1] {
			rmf[i] = -rmf[i]
		} else if i > 0 && tp[i] == tp[i-1] {
			rmf[i] = 0
		}
	}
	for i := period; i < n; i++ {
		pos, neg := 0.0, 0.0
		for j := i - period + 1; j <= i; j++ {
			if rmf[j] > 0 {
				pos += rmf[j]
			} else {
				neg += -rmf[j]
			}
		}
		if neg == 0 {
			out[i] = 100
			continue
		}
		out[i] = 100 - 100/(1+pos/neg)
	}
	return out
}

// cmfSeries Chaikin 资金流量序列。
func cmfSeries(bars []model.Bar, period int) []float64 {
	n := len(bars)
	out := nanSeries(n)
	if period <= 0 || n < period {
		return out
	}
	mfv := make([]float64, n)
	for i, b := range bars {
		hl := b.High - b.Low
		if hl == 0 {
			mfv[i] = 0
			continue
		}
		mfv[i] = ((b.Close - b.Low) - (b.High - b.Close)) / hl * b.Volume
	}
	for i := period - 1; i < n; i++ {
		volSum, mfvSum := 0.0, 0.0
		for j := i - period + 1; j <= i; j++ {
			volSum += bars[j].Volume
			mfvSum += mfv[j]
		}
		if volSum > 0 {
			out[i] = mfvSum / volSum
		}
	}
	return out
}

// macdSeries MACD 线/信号线/柱序列（复用 strategy/indicators 的计算口径，
// 这里需要全序列，故本地展开同样的 EMA 递推）。
func macdSeries(closes []float64, fast, slow, signal int) (macdLine, signalLine, hist []float64) {
	ef := emaSeries(closes, fast)
	es := emaSeries(closes, slow)
	n := len(closes)
	macdLine = nanSeries(n)
	for i := 0; i < n; i++ {
		if !math.IsNaN(ef[i]) && !math.IsNaN(es[i]) {
			macdLine[i] = ef[i] - es[i]
		}
	}
	signalLine = emaSeries(macdLine, signal)
	hist = nanSeries(n)
	for i := 0; i < n; i++ {
		if !math.IsNaN(macdLine[i]) && !math.IsNaN(signalLine[i]) {
			hist[i] = macdLine[i] - signalLine[i]
		}
	}
	return
}

// slopeSeries 对序列做 period 窗口线性回归，输出斜率（每单位 x）。
func slopeSeries(in []float64, period int) []float64 {
	out := nanSeries(len(in))
	if period <= 1 || len(in) < period {
		return out
	}
	for i := period - 1; i < len(in); i++ {
		sumX, sumY, sumXY, sumXX := 0.0, 0.0, 0.0, 0.0
		ok := true
		for j := 0; j < period; j++ {
			v := in[i-period+1+j]
			if math.IsNaN(v) {
				ok = false
				break
			}
			x := float64(j)
			sumX += x
			sumY += v
			sumXY += x * v
			sumXX += x * x
		}
		if !ok {
			continue
		}
		n := float64(period)
		denom := n*sumXX - sumX*sumX
		if denom == 0 {
			continue
		}
		out[i] = (n*sumXY - sumX*sumY) / denom
	}
	return out
}

// rank 返回值的秩（1..n，升序，平级取平均秩）。原地不改输入。
func rank(values []float64) []float64 {
	n := len(values)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return values[idx[a]] < values[idx[b]] })
	r := make([]float64, n)
	i := 0
	for i < n {
		j := i
		for j+1 < n && values[idx[j+1]] == values[idx[i]] {
			j++
		}
		avg := float64(i+j+2) / 2 // 秩从 1 开始
		for k := i; k <= j; k++ {
			r[idx[k]] = avg
		}
		i = j + 1
	}
	return r
}

// safeDiv a/b，b 为 0 或非法时返回 NaN。
func safeDiv(a, b float64) float64 {
	if b == 0 || math.IsNaN(b) || math.IsInf(b, 0) {
		return math.NaN()
	}
	return a / b
}
