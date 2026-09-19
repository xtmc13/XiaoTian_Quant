package factors

import (
	"math"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// 内置因子库：30+ 因子，覆盖动量/波动/趋势/成交量/均值回复五类。
// 所有 Calc 输出与输入 bars 等长的序列，窗口不足处为 NaN。
// 计算口径优先复用 strategy/indicators 包的递推方式（EMA/MACD），
// 其余经典指标（RSI/ATR/ADX/OBV 等）在本包 math.go 中实现。

func init() {
	registerMomentum()
	registerVolatility()
	registerTrend()
	registerVolume()
	registerMeanReversion()
}

func intParams(defs ...ParamSchema) []ParamSchema { return defs }

func pInt(name string, def int, min, max int, desc string) ParamSchema {
	return ParamSchema{Name: name, Type: "int", Default: def, Min: float64(min), Max: float64(max), Description: desc}
}

func pFloat(name string, def float64, min, max float64, desc string) ParamSchema {
	return ParamSchema{Name: name, Type: "float", Default: def, Min: min, Max: max, Description: desc}
}

// ── 动量类 ──

func registerMomentum() {
	MustRegister(&Def{
		Name: "roc", Version: 1, Category: "momentum",
		Description: "变动率 ROC：close/close[n]-1，衡量 n 周期动量",
		Params:      intParams(pInt("period", 10, 1, 500, "动量周期")),
		Defaults:    map[string]any{"period": 10},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			return rocSeries(closesOf(bars), IntParam(params, "period", 10))
		},
	})

	MustRegister(&Def{
		Name: "momentum", Version: 1, Category: "momentum",
		Description: "价格动量：close - close[n]（绝对价差）",
		Params:      intParams(pInt("period", 10, 1, 500, "动量周期")),
		Defaults:    map[string]any{"period": 10},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			return diffSeries(closesOf(bars), IntParam(params, "period", 10))
		},
	})

	MustRegister(&Def{
		Name: "rsi", Version: 1, Category: "momentum",
		Description: "相对强弱指标 RSI（Wilder 平滑），0..100",
		Params:      intParams(pInt("period", 14, 2, 200, "RSI 周期")),
		Defaults:    map[string]any{"period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			return rsiSeries(closesOf(bars), IntParam(params, "period", 14))
		},
	})

	MustRegister(&Def{
		Name: "ma_deviation", Version: 1, Category: "momentum",
		Description: "均线偏离：(close - SMA[n])/SMA[n]，价格相对均线的位置",
		Params:      intParams(pInt("period", 20, 2, 500, "均线周期")),
		Defaults:    map[string]any{"period": 20},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes := closesOf(bars)
			ma := smaSeries(closes, IntParam(params, "period", 20))
			out := nanSeries(len(bars))
			for i := range bars {
				out[i] = safeDiv(closes[i]-ma[i], ma[i])
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "ema_deviation", Version: 1, Category: "momentum",
		Description: "指数均线偏离：(close - EMA[n])/EMA[n]",
		Params:      intParams(pInt("period", 20, 2, 500, "EMA 周期")),
		Defaults:    map[string]any{"period": 20},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes := closesOf(bars)
			ma := emaSeries(closes, IntParam(params, "period", 20))
			out := nanSeries(len(bars))
			for i := range bars {
				out[i] = safeDiv(closes[i]-ma[i], ma[i])
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "macd", Version: 1, Category: "momentum",
		Description: "MACD 快线（DIF）：EMA[fast] - EMA[slow]",
		Params: intParams(
			pInt("fast", 12, 2, 200, "快线周期"),
			pInt("slow", 26, 3, 500, "慢线周期"),
		),
		Defaults: map[string]any{"fast": 12, "slow": 26},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			m, _, _ := macdSeries(closesOf(bars), IntParam(params, "fast", 12), IntParam(params, "slow", 26), 9)
			return m
		},
	})

	MustRegister(&Def{
		Name: "macd_hist", Version: 1, Category: "momentum",
		Description: "MACD 柱（DIF - DEA），动能变化",
		Params: intParams(
			pInt("fast", 12, 2, 200, "快线周期"),
			pInt("slow", 26, 3, 500, "慢线周期"),
			pInt("signal", 9, 2, 200, "信号线周期"),
		),
		Defaults: map[string]any{"fast": 12, "slow": 26, "signal": 9},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			_, _, h := macdSeries(closesOf(bars), IntParam(params, "fast", 12), IntParam(params, "slow", 26), IntParam(params, "signal", 9))
			return h
		},
	})

	MustRegister(&Def{
		Name: "cmo", Version: 1, Category: "momentum",
		Description: "钱德动量振荡器 CMO，-100..100，衡量净动量占比",
		Params:      intParams(pInt("period", 14, 2, 200, "CMO 周期")),
		Defaults:    map[string]any{"period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes := closesOf(bars)
			period := IntParam(params, "period", 14)
			out := nanSeries(len(closes))
			if len(closes) < period+1 {
				return out
			}
			for i := period; i < len(closes); i++ {
				gain, loss := 0.0, 0.0
				for j := i - period + 1; j <= i; j++ {
					ch := closes[j] - closes[j-1]
					if ch > 0 {
						gain += ch
					} else {
						loss += -ch
					}
				}
				out[i] = safeDiv(gain-loss, gain+loss) * 100
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "ppo", Version: 1, Category: "momentum",
		Description: "百分比价格振荡器 PPO：(EMA[f]-EMA[s])/EMA[s]*100",
		Params: intParams(
			pInt("fast", 12, 2, 200, "快线周期"),
			pInt("slow", 26, 3, 500, "慢线周期"),
		),
		Defaults: map[string]any{"fast": 12, "slow": 26},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes := closesOf(bars)
			ef := emaSeries(closes, IntParam(params, "fast", 12))
			es := emaSeries(closes, IntParam(params, "slow", 26))
			out := nanSeries(len(closes))
			for i := range closes {
				out[i] = safeDiv(ef[i]-es[i], es[i]) * 100
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "trix", Version: 1, Category: "momentum",
		Description: "三重指数平滑均线变化率 TRIX，1 周期 EMA 三重平滑的 ROC%",
		Params:      intParams(pInt("period", 15, 3, 300, "平滑周期")),
		Defaults:    map[string]any{"period": 15},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes := closesOf(bars)
			p := IntParam(params, "period", 15)
			e1 := emaSeries(closes, p)
			e2 := emaSeries(e1, p)
			e3 := emaSeries(e2, p)
			out := nanSeries(len(closes))
			for i := 1; i < len(closes); i++ {
				if e3[i-1] != 0 && !math.IsNaN(e3[i]) && !math.IsNaN(e3[i-1]) {
					out[i] = (e3[i]/e3[i-1] - 1) * 100
				}
			}
			return out
		},
	})
}

// ── 波动类 ──

func registerVolatility() {
	MustRegister(&Def{
		Name: "atr", Version: 1, Category: "volatility",
		Description: "平均真实波幅 ATR（Wilder 平滑）",
		Params:      intParams(pInt("period", 14, 1, 200, "ATR 周期")),
		Defaults:    map[string]any{"period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			return atrSeries(bars, IntParam(params, "period", 14))
		},
	})

	MustRegister(&Def{
		Name: "atr_pct", Version: 1, Category: "volatility",
		Description: "归一化 ATR：ATR/close，跨品种可比的波动率",
		Params:      intParams(pInt("period", 14, 1, 200, "ATR 周期")),
		Defaults:    map[string]any{"period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			atr := atrSeries(bars, IntParam(params, "period", 14))
			out := nanSeries(len(bars))
			for i, b := range bars {
				out[i] = safeDiv(atr[i], b.Close)
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "volatility", Version: 1, Category: "volatility",
		Description: "收益率滚动标准差（未年化）",
		Params:      intParams(pInt("period", 20, 2, 500, "窗口周期")),
		Defaults:    map[string]any{"period": 20},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			return rollingStdSeries(returnsSeries(closesOf(bars)), IntParam(params, "period", 20))
		},
	})

	MustRegister(&Def{
		Name: "bollinger_width", Version: 1, Category: "volatility",
		Description: "布林带宽：(上轨-下轨)/中轨",
		Params:      intParams(pInt("period", 20, 2, 500, "布林带周期"), pFloat("mult", 2, 0.5, 5, "标准差倍数")),
		Defaults:    map[string]any{"period": 20, "mult": 2.0},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes := closesOf(bars)
			p := IntParam(params, "period", 20)
			mult := FloatParam(params, "mult", 2)
			mid := smaSeries(closes, p)
			sd := rollingStdSeries(closes, p)
			out := nanSeries(len(closes))
			for i := range closes {
				if !math.IsNaN(mid[i]) && !math.IsNaN(sd[i]) {
					out[i] = 2 * mult * sd[i] / mid[i]
				}
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "bollinger_pctb", Version: 1, Category: "volatility",
		Description: "%B 指标：(close-下轨)/(上轨-下轨)，价格 within 布林带位置",
		Params:      intParams(pInt("period", 20, 2, 500, "布林带周期"), pFloat("mult", 2, 0.5, 5, "标准差倍数")),
		Defaults:    map[string]any{"period": 20, "mult": 2.0},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes := closesOf(bars)
			p := IntParam(params, "period", 20)
			mult := FloatParam(params, "mult", 2)
			mid := smaSeries(closes, p)
			sd := rollingStdSeries(closes, p)
			out := nanSeries(len(closes))
			for i := range closes {
				if math.IsNaN(mid[i]) || math.IsNaN(sd[i]) {
					continue
				}
				up, lo := mid[i]+mult*sd[i], mid[i]-mult*sd[i]
				if up != lo {
					out[i] = (closes[i] - lo) / (up - lo)
				}
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "hl_range", Version: 1, Category: "volatility",
		Description: "高低价振幅：(high-low)/close",
		Params:      nil,
		Defaults:    map[string]any{},
		Calc: func(bars []model.Bar, _ map[string]any) []float64 {
			out := nanSeries(len(bars))
			for i, b := range bars {
				out[i] = safeDiv(b.High-b.Low, b.Close)
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "parkinson_vol", Version: 1, Category: "volatility",
		Description: "帕金森波动率：high/low 估算的区间内波动（未年化）",
		Params:      intParams(pInt("period", 20, 2, 500, "窗口周期")),
		Defaults:    map[string]any{"period": 20},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			p := IntParam(params, "period", 20)
			variance := nanSeries(len(bars))
			for i := 1; i < len(bars); i++ {
				if bars[i].High > 0 && bars[i].Low > 0 {
					ln := math.Log(bars[i].High / bars[i].Low)
					variance[i] = ln * ln / (4 * math.Ln2)
				}
			}
			return rollingMeanSeries(variance, p)
		},
	})
}

// ── 趋势类 ──

func registerTrend() {
	MustRegister(&Def{
		Name: "ema_slope", Version: 1, Category: "trend",
		Description: "EMA 斜率：对 EMA[n] 做线性回归的每 bar 斜率，趋势强度",
		Params:      intParams(pInt("period", 20, 3, 500, "EMA 周期"), pInt("slope_window", 10, 3, 200, "回归窗口")),
		Defaults:    map[string]any{"period": 20, "slope_window": 10},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			e := emaSeries(closesOf(bars), IntParam(params, "period", 20))
			sl := slopeSeries(e, IntParam(params, "slope_window", 10))
			// 归一化到当前价格，跨品种可比
			closes := closesOf(bars)
			out := nanSeries(len(bars))
			for i := range bars {
				out[i] = safeDiv(sl[i], closes[i])
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "adx", Version: 1, Category: "trend",
		Description: "平均趋向指标 ADX，趋势强度 0..100（不含方向）",
		Params:      intParams(pInt("period", 14, 2, 200, "ADX 周期")),
		Defaults:    map[string]any{"period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			adx, _, _ := adxSeries(bars, IntParam(params, "period", 14))
			return adx
		},
	})

	MustRegister(&Def{
		Name: "dmi_diff", Version: 1, Category: "trend",
		Description: "DMI 差：+DI - -DI，趋势方向与强度合成",
		Params:      intParams(pInt("period", 14, 2, 200, "DMI 周期")),
		Defaults:    map[string]any{"period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			_, p, m := adxSeries(bars, IntParam(params, "period", 14))
			out := nanSeries(len(bars))
			for i := range bars {
				out[i] = p[i] - m[i]
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "sma_cross_spread", Version: 1, Category: "trend",
		Description: "双均线差：(SMA[fast]-SMA[slow])/SMA[slow]",
		Params: intParams(
			pInt("fast", 10, 1, 300, "快均线周期"),
			pInt("slow", 30, 2, 1000, "慢均线周期"),
		),
		Defaults: map[string]any{"fast": 10, "slow": 30},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes := closesOf(bars)
			f := smaSeries(closes, IntParam(params, "fast", 10))
			s := smaSeries(closes, IntParam(params, "slow", 30))
			out := nanSeries(len(closes))
			for i := range closes {
				out[i] = safeDiv(f[i]-s[i], s[i])
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "trend_consistency", Version: 1, Category: "trend",
		Description: "趋势一致性：过去 n 根中收盘价沿同方向变动的占比 - 反向占比，[-1,1]",
		Params:      intParams(pInt("period", 14, 3, 200, "观察周期")),
		Defaults:    map[string]any{"period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes := closesOf(bars)
			p := IntParam(params, "period", 14)
			out := nanSeries(len(closes))
			for i := p; i < len(closes); i++ {
				up, down := 0, 0
				for j := i - p + 1; j <= i; j++ {
					if closes[j] > closes[j-1] {
						up++
					} else if closes[j] < closes[j-1] {
						down++
					}
				}
				out[i] = float64(up-down) / float64(p)
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "price_position", Version: 1, Category: "trend",
		Description: "价格位置：(close - LLV[n])/(HHV[n] - LLV[n])，当前价格在 n 周期区间中的位置",
		Params:      intParams(pInt("period", 20, 2, 500, "区间周期")),
		Defaults:    map[string]any{"period": 20},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			p := IntParam(params, "period", 20)
			highs, lows, closes := highsOf(bars), lowsOf(bars), closesOf(bars)
			hh := rollingMaxSeries(highs, p)
			ll := rollingMinSeries(lows, p)
			out := nanSeries(len(bars))
			for i := range bars {
				if hh[i] != ll[i] && !math.IsNaN(hh[i]) {
					out[i] = (closes[i] - ll[i]) / (hh[i] - ll[i])
				}
			}
			return out
		},
	})
}

// ── 成交量类 ──

func registerVolume() {
	MustRegister(&Def{
		Name: "obv", Version: 1, Category: "volume",
		Description: "能量潮 OBV（累积成交量，随价格方向加减）",
		Params:      nil,
		Defaults:    map[string]any{},
		Calc: func(bars []model.Bar, _ map[string]any) []float64 {
			return obvSeries(bars)
		},
	})

	MustRegister(&Def{
		Name: "obv_roc", Version: 1, Category: "volume",
		Description: "OBV 变化率：n 周期 OBV 涨跌幅度（归一化到成交量量级）",
		Params:      intParams(pInt("period", 10, 1, 500, "动量周期")),
		Defaults:    map[string]any{"period": 10},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			obv := obvSeries(bars)
			vols := volumesOf(bars)
			meanVol := rollingMeanSeries(vols, IntParam(params, "period", 10))
			diff := diffSeries(obv, IntParam(params, "period", 10))
			out := nanSeries(len(bars))
			for i := range bars {
				out[i] = safeDiv(diff[i], meanVol[i])
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "volume_ratio", Version: 1, Category: "volume",
		Description: "量比：当前成交量 / 过去 n 根均量",
		Params:      intParams(pInt("period", 20, 2, 500, "均量周期")),
		Defaults:    map[string]any{"period": 20},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			vols := volumesOf(bars)
			meanVol := rollingMeanSeries(vols, IntParam(params, "period", 20))
			out := nanSeries(len(bars))
			for i := range bars {
				out[i] = safeDiv(vols[i], meanVol[i])
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "vroc", Version: 1, Category: "volume",
		Description: "量变动率：volume/volume[n]-1",
		Params:      intParams(pInt("period", 10, 1, 500, "周期")),
		Defaults:    map[string]any{"period": 10},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			return rocSeries(volumesOf(bars), IntParam(params, "period", 10))
		},
	})

	MustRegister(&Def{
		Name: "mfi", Version: 1, Category: "volume",
		Description: "资金流量指标 MFI（成交量的 RSI），0..100",
		Params:      intParams(pInt("period", 14, 2, 200, "MFI 周期")),
		Defaults:    map[string]any{"period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			return mfiSeries(bars, IntParam(params, "period", 14))
		},
	})

	MustRegister(&Def{
		Name: "cmf", Version: 1, Category: "volume",
		Description: "蔡金资金流量 CMF：收盘价位置加权的成交量净流，[-1,1]",
		Params:      intParams(pInt("period", 20, 2, 500, "周期")),
		Defaults:    map[string]any{"period": 20},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			return cmfSeries(bars, IntParam(params, "period", 20))
		},
	})

	MustRegister(&Def{
		Name: "vwma_deviation", Version: 1, Category: "volume",
		Description: "成交量加权均线偏离：(close - VWMA[n])/VWMA[n]",
		Params:      intParams(pInt("period", 20, 2, 500, "VWMA 周期")),
		Defaults:    map[string]any{"period": 20},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			p := IntParam(params, "period", 20)
			closes := closesOf(bars)
			out := nanSeries(len(bars))
			for i := p - 1; i < len(bars); i++ {
				num, den := 0.0, 0.0
				for j := i - p + 1; j <= i; j++ {
					num += closes[j] * bars[j].Volume
					den += bars[j].Volume
				}
				if den > 0 {
					vwma := num / den
					out[i] = (closes[i] - vwma) / vwma
				}
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "force_index", Version: 1, Category: "volume",
		Description: "强力指数（EMA 平滑）：(close[i]-close[i-1])*volume 的 EMA",
		Params:      intParams(pInt("period", 13, 2, 200, "EMA 平滑周期")),
		Defaults:    map[string]any{"period": 13},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes, vols := closesOf(bars), volumesOf(bars)
			raw := nanSeries(len(bars))
			for i := 1; i < len(bars); i++ {
				raw[i] = (closes[i] - closes[i-1]) * vols[i]
			}
			return emaSeries(raw, IntParam(params, "period", 13))
		},
	})
}

// ── 均值回复类 ──

func registerMeanReversion() {
	MustRegister(&Def{
		Name: "stoch_k", Version: 1, Category: "mean_reversion",
		Description: "随机指标 %K：当前收盘在 n 周期高低区间中的位置，0..100",
		Params:      intParams(pInt("k_period", 14, 2, 300, "区间周期")),
		Defaults:    map[string]any{"k_period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			k, _ := stochSeries(bars, IntParam(params, "k_period", 14), 3)
			return k
		},
	})

	MustRegister(&Def{
		Name: "stoch_d", Version: 1, Category: "mean_reversion",
		Description: "随机指标 %D：%K 的 m 周期均线",
		Params:      intParams(pInt("k_period", 14, 2, 300, "区间周期"), pInt("d_period", 3, 2, 100, "平滑周期")),
		Defaults:    map[string]any{"k_period": 14, "d_period": 3},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			_, d := stochSeries(bars, IntParam(params, "k_period", 14), IntParam(params, "d_period", 3))
			return d
		},
	})

	MustRegister(&Def{
		Name: "stoch_rsi", Version: 1, Category: "mean_reversion",
		Description: "RSI 的随机指标：RSI 值在其 n 周期区间中的位置，0..1",
		Params:      intParams(pInt("rsi_period", 14, 2, 200, "RSI 周期"), pInt("stoch_period", 14, 2, 300, "随机区间")),
		Defaults:    map[string]any{"rsi_period": 14, "stoch_period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			rsi := rsiSeries(closesOf(bars), IntParam(params, "rsi_period", 14))
			p := IntParam(params, "stoch_period", 14)
			hh := rollingMaxSeries(rsi, p)
			ll := rollingMinSeries(rsi, p)
			out := nanSeries(len(bars))
			for i := range bars {
				if hh[i] != ll[i] && !math.IsNaN(hh[i]) && !math.IsNaN(ll[i]) {
					out[i] = (rsi[i] - ll[i]) / (hh[i] - ll[i])
				}
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "rsi_reversion", Version: 1, Category: "mean_reversion",
		Description: "RSI 反转得分：50 - RSI（正值=超卖反弹倾向）",
		Params:      intParams(pInt("period", 14, 2, 200, "RSI 周期")),
		Defaults:    map[string]any{"period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			rsi := rsiSeries(closesOf(bars), IntParam(params, "period", 14))
			out := nanSeries(len(bars))
			for i, v := range rsi {
				if !math.IsNaN(v) {
					out[i] = 50 - v
				}
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "williams_r", Version: 1, Category: "mean_reversion",
		Description: "威廉指标 %R：-100..0，接近 -100 超卖、接近 0 超买",
		Params:      intParams(pInt("period", 14, 2, 300, "区间周期")),
		Defaults:    map[string]any{"period": 14},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			p := IntParam(params, "period", 14)
			highs, lows, closes := highsOf(bars), lowsOf(bars), closesOf(bars)
			hh := rollingMaxSeries(highs, p)
			ll := rollingMinSeries(lows, p)
			out := nanSeries(len(bars))
			for i := range bars {
				if hh[i] != ll[i] && !math.IsNaN(hh[i]) {
					out[i] = (hh[i] - closes[i]) / (hh[i] - ll[i]) * -100
				}
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "cci", Version: 1, Category: "mean_reversion",
		Description: "顺势指标 CCI：偏离典型价均值的程度，±100 为常态边界",
		Params:      intParams(pInt("period", 20, 2, 300, "CCI 周期")),
		Defaults:    map[string]any{"period": 20},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			return cciSeries(bars, IntParam(params, "period", 20))
		},
	})

	MustRegister(&Def{
		Name: "boll_zscore", Version: 1, Category: "mean_reversion",
		Description: "布林 z-score：(close - SMA[n]) / std[n]，偏离均线多少个标准差",
		Params:      intParams(pInt("period", 20, 2, 500, "周期")),
		Defaults:    map[string]any{"period": 20},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			closes := closesOf(bars)
			p := IntParam(params, "period", 20)
			mid := smaSeries(closes, p)
			sd := rollingStdSeries(closes, p)
			out := nanSeries(len(bars))
			for i := range bars {
				out[i] = safeDiv(closes[i]-mid[i], sd[i])
			}
			return out
		},
	})

	MustRegister(&Def{
		Name: "close_pct_rank", Version: 1, Category: "mean_reversion",
		Description: "收盘价分位：当前 close 在过去 n 根中的百分位，0..1（高分位=回调压力）",
		Params:      intParams(pInt("period", 50, 5, 1000, "观察窗口")),
		Defaults:    map[string]any{"period": 50},
		Calc: func(bars []model.Bar, params map[string]any) []float64 {
			return pctRankSeries(closesOf(bars), IntParam(params, "period", 50))
		},
	})

	MustRegister(&Def{
		Name: "gap_reversion", Version: 1, Category: "mean_reversion",
		Description: "跳空回复：(open/close[n-1]-1) 的反向信号强度",
		Params:      nil,
		Defaults:    map[string]any{},
		Calc: func(bars []model.Bar, _ map[string]any) []float64 {
			out := nanSeries(len(bars))
			for i := 1; i < len(bars); i++ {
				if bars[i-1].Close != 0 {
					out[i] = bars[i-1].Close/bars[i].Open - 1
				}
			}
			return out
		},
	})
}
