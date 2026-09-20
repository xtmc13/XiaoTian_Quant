package alertexpr

import (
	"errors"
	"fmt"
	"math"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// ── 指标函数表 ──
//
// 数学口径与项目内既有 Go 原生实现保持一致：
//   - ema/sma/macd 复用 strategy/cra 的 EMA/SMA/MACD（序列实现，取末值）；
//   - rsi 与 service.MarketDataService.ComputeIndicators 的 computeRSI 同口径
//     （最近 period 个收盘涨跌的简单平均）；
//   - atr 与 service.ComputeIndicators 的 computeATR 同口径
//     （TR=max(high-low,|high-prevClose|,|low-prevClose|) 最近 period 个平均）；
//   - 布林线 bb_* 与 service.ComputeIndicators 的布林带同口径
//     （period 收盘均值 ± mult×总体标准差）。

type fnDef struct {
	minArgs  int
	defaults []float64
	check    func(args []float64) error
	eval     func(ctx *evalCtx, args []float64) (float64, error)
}

// checkPeriods 校验 period 类参数（其余参数按 mult 校验）。
func checkPeriods(n int) func([]float64) error {
	return func(args []float64) error {
		for i := 0; i < n && i < len(args); i++ {
			if args[i] < 1 || args[i] > maxPeriod || math.Trunc(args[i]) != args[i] {
				return fmt.Errorf("arg%d: period must be an integer in [1,%d], got %v", i+1, maxPeriod, args[i])
			}
		}
		for i := n; i < len(args); i++ {
			if args[i] <= 0 || args[i] > maxMult {
				return fmt.Errorf("arg%d: multiplier must be in (0,%g], got %v", i+1, maxMult, args[i])
			}
		}
		return nil
	}
}

func needBars(ctx *evalCtx, n int, fn string) error {
	if len(ctx.bars) < n {
		return fmt.Errorf("need at least %d bars, got %d", n, len(ctx.bars))
	}
	return nil
}

var funcRegistry = map[string]fnDef{
	"open":   {minArgs: 0, defaults: nil, check: checkPeriods(0), eval: lastBarField(func(b model.Bar) float64 { return b.Open })},
	"high":   {minArgs: 0, defaults: nil, check: checkPeriods(0), eval: lastBarField(func(b model.Bar) float64 { return b.High })},
	"low":    {minArgs: 0, defaults: nil, check: checkPeriods(0), eval: lastBarField(func(b model.Bar) float64 { return b.Low })},
	"close":  {minArgs: 0, defaults: nil, check: checkPeriods(0), eval: lastBarField(func(b model.Bar) float64 { return b.Close })},
	"volume": {minArgs: 0, defaults: nil, check: checkPeriods(0), eval: lastBarField(func(b model.Bar) float64 { return b.Volume })},

	"rsi": {minArgs: 0, defaults: []float64{14}, check: checkPeriods(1), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		period := int(a[0])
		closes := ctx.getCloses()
		if err := needBars(ctx, period+1, "rsi"); err != nil {
			return 0, err
		}
		return rsiLast(closes, period), nil
	}},
	"ema": {minArgs: 0, defaults: []float64{20}, check: checkPeriods(1), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		period := int(a[0])
		if err := needBars(ctx, 1, "ema"); err != nil {
			return 0, err
		}
		series := cra.EMA(ctx.getCloses(), period)
		if len(series) == 0 {
			return 0, errors.New("ema series empty")
		}
		return series[len(series)-1], nil
	}},
	"sma": {minArgs: 0, defaults: []float64{20}, check: checkPeriods(1), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		period := int(a[0])
		if err := needBars(ctx, period, "sma"); err != nil {
			return 0, err
		}
		series := cra.SMA(ctx.getCloses(), period)
		if len(series) == 0 {
			return 0, errors.New("sma series empty")
		}
		return series[len(series)-1], nil
	}},
	"atr": {minArgs: 0, defaults: []float64{14}, check: checkPeriods(1), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		period := int(a[0])
		if err := needBars(ctx, period+1, "atr"); err != nil {
			return 0, err
		}
		return atrLast(ctx.bars, period), nil
	}},

	"macd": {minArgs: 0, defaults: []float64{12, 26, 9}, check: checkPeriods(3), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		macdLine, _, _, err := macdLast(ctx, a)
		return macdLine, err
	}},
	"macd_signal": {minArgs: 0, defaults: []float64{12, 26, 9}, check: checkPeriods(3), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		_, signal, _, err := macdLast(ctx, a)
		return signal, err
	}},
	"macd_hist": {minArgs: 0, defaults: []float64{12, 26, 9}, check: checkPeriods(3), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		_, _, hist, err := macdLast(ctx, a)
		return hist, err
	}},

	"bb_upper": {minArgs: 0, defaults: []float64{20, 2}, check: checkPeriods(1), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		mid, sd, err := bbMidSD(ctx, a)
		if err != nil {
			return 0, err
		}
		return mid + a[1]*sd, nil
	}},
	"bb_mid": {minArgs: 0, defaults: []float64{20, 2}, check: checkPeriods(1), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		mid, _, err := bbMidSD(ctx, a)
		return mid, err
	}},
	"bb_lower": {minArgs: 0, defaults: []float64{20, 2}, check: checkPeriods(1), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		mid, sd, err := bbMidSD(ctx, a)
		if err != nil {
			return 0, err
		}
		return mid - a[1]*sd, nil
	}},
	// bb_width=(upper-lower)/mid，布林收口常用 < 阈值 判断。
	"bb_width": {minArgs: 0, defaults: []float64{20, 2}, check: checkPeriods(1), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		mid, sd, err := bbMidSD(ctx, a)
		if err != nil {
			return 0, err
		}
		if mid == 0 {
			return 0, errors.New("bb_mid is zero")
		}
		return 2 * a[1] * sd / mid, nil
	}},
	// bb_pctb=(close-lower)/(upper-lower)，价格在带内的相对位置。
	"bb_pctb": {minArgs: 0, defaults: []float64{20, 2}, check: checkPeriods(1), eval: func(ctx *evalCtx, a []float64) (float64, error) {
		mid, sd, err := bbMidSD(ctx, a)
		if err != nil {
			return 0, err
		}
		upper, lower := mid+a[1]*sd, mid-a[1]*sd
		if upper == lower {
			return 0, errors.New("bollinger band width is zero")
		}
		return (ctx.bars[len(ctx.bars)-1].Close - lower) / (upper - lower), nil
	}},
}

// lastBarField 裸 OHLCV：取最新一根的对应字段。
func lastBarField(f func(model.Bar) float64) func(*evalCtx, []float64) (float64, error) {
	return func(ctx *evalCtx, _ []float64) (float64, error) {
		return f(ctx.bars[len(ctx.bars)-1]), nil
	}
}

// rsiLast 最近 period 个涨跌简单平均口径的 RSI（同 service.computeRSI）。
// 数据不足时调用方已拦截；avgLoss=0（单边上涨）返回 100。
func rsiLast(closes []float64, period int) float64 {
	gains, losses := 0.0, 0.0
	for i := len(closes) - period; i < len(closes); i++ {
		diff := closes[i] - closes[i-1]
		if diff > 0 {
			gains += diff
		} else {
			losses -= diff
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// atrLast 最近 period 个 TR 的简单平均（同 service.computeATR）。
func atrLast(bars []model.Bar, period int) float64 {
	trSum := 0.0
	recent := bars[len(bars)-period:]
	prevClose := bars[len(bars)-period-1].Close
	for i, b := range recent {
		pc := prevClose
		if i > 0 {
			pc = recent[i-1].Close
		}
		tr := math.Max(b.High-b.Low, math.Max(math.Abs(b.High-pc), math.Abs(b.Low-pc)))
		trSum += tr
	}
	return trSum / float64(period)
}

// macdLast 复用 cra.MACD，返回末值三元组。
func macdLast(ctx *evalCtx, a []float64) (macdLine, signal, hist float64, err error) {
	macdSeries, signalSeries, histSeries := cra.MACD(ctx.getCloses(), int(a[0]), int(a[1]), int(a[2]))
	if len(macdSeries) == 0 || len(signalSeries) == 0 || len(histSeries) == 0 {
		return 0, 0, 0, fmt.Errorf("macd(%v,%v,%v): insufficient bars %d", a[0], a[1], a[2], len(ctx.bars))
	}
	return macdSeries[len(macdSeries)-1], signalSeries[len(signalSeries)-1], histSeries[len(histSeries)-1], nil
}

// bbMidSD 最近 period 个收盘的均值与总体标准差。
func bbMidSD(ctx *evalCtx, a []float64) (mid, sd float64, err error) {
	period := int(a[0])
	if err := needBars(ctx, period, "bb"); err != nil {
		return 0, 0, err
	}
	closes := ctx.getCloses()
	window := closes[len(closes)-period:]
	sum := 0.0
	for _, v := range window {
		sum += v
	}
	mid = sum / float64(period)
	variance := 0.0
	for _, v := range window {
		d := v - mid
		variance += d * d
	}
	return mid, math.Sqrt(variance / float64(period)), nil
}
