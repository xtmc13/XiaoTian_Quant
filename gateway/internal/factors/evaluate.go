package factors

import (
	"math"
	"sort"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 因子评价 ──
// IC（信息系数）：因子值与未来收益率的相关性。单标的研究采用滚动窗口
// 时序 IC——每个时点取过去 window 个 (因子值, 未来收益) 样本算 Spearman/
// Pearson 相关，得到 IC 时间序列；ICIR = 均值/标准差。另输出全样本
// 一次性 IC（overall_ic / overall_rank_ic）。

// EvaluationConfig 控制评价过程。
type EvaluationConfig struct {
	ForwardBars int `json:"forward_bars"` // 未来收益 horizons（单位：bar）
	ICWindow    int `json:"ic_window"`    // 滚动 IC 窗口（样本数）
}

// DefaultEvaluationConfig 返回默认评价配置。
func DefaultEvaluationConfig() EvaluationConfig {
	return EvaluationConfig{ForwardBars: 5, ICWindow: 100}
}

// ICSeriesPoint 是滚动 IC 序列上的一个点。
type ICSeriesPoint struct {
	Time   int64   `json:"time"`
	IC     float64 `json:"ic"`
	RankIC float64 `json:"rank_ic"`
}

// Evaluation 是因子评价结果。
type Evaluation struct {
	FactorName  string `json:"factor_name"`
	Version     int    `json:"version"`
	Symbol      string `json:"symbol"`
	TF          string `json:"tf"`
	ForwardBars int    `json:"forward_bars"`

	Samples int `json:"samples"` // 参与 IC 计算的样本数

	OverallIC     float64 `json:"overall_ic"`      // 全样本 Pearson（因子值 vs 未来收益）
	OverallRankIC float64 `json:"overall_rank_ic"` // 全样本 Spearman

	ICMean        float64         `json:"ic_mean"` // 滚动 IC 均值
	ICStd         float64         `json:"ic_std"`  // 滚动 IC 标准差
	ICIR          float64         `json:"icir"`    // ICMean/ICStd（年化前）
	RankICMean    float64         `json:"rank_ic_mean"`
	RankICStd     float64         `json:"rank_ic_std"`
	RankICIR      float64         `json:"rank_icir"`
	ICPositivePct float64         `json:"ic_positive_pct"` // IC>0 的占比 %
	ICSeries      []ICSeriesPoint `json:"ic_series"`       // 滚动 IC 序列
}

// Layer 是分层回测中某一层的结果。
type Layer struct {
	Layer         int     `json:"layer"`           // 1..N，1=因子值最低层
	Count         int     `json:"count"`           // 落入该层的 bar 数
	AvgForwardRet float64 `json:"avg_forward_ret"` // 平均未来收益（每 bar）
	TotalReturn   float64 `json:"total_return"`    // 向量化累计收益
	AnnualizedRet float64 `json:"annualized_ret"`  // 年化收益 %
}

// LayeredResult 是分层回测结果。
type LayeredResult struct {
	FactorName  string `json:"factor_name"`
	Version     int    `json:"version"`
	Symbol      string `json:"symbol"`
	TF          string `json:"tf"`
	ForwardBars int    `json:"forward_bars"`
	LayerCount  int    `json:"layer_count"`
	Lookback    int    `json:"lookback"` // 分位窗口（决定 warmup 后样本数）

	Layers       []Layer         `json:"layers"`
	LongShort    float64         `json:"long_short_total_return"` // 最高层 - 最低层
	Monotonicity float64         `json:"monotonicity"`            // 层间收益的 Spearman（1=完全单调）
	Samples      int             `json:"samples"`
	EquityCurves [][]EquityPoint `json:"equity_curves"` // 每层累计净值曲线（0..N-1）
}

// EquityPoint 是分层的累计净值点。
type EquityPoint struct {
	Time   int64   `json:"time"`
	Equity float64 `json:"equity"`
}

// ForwardReturns 计算 forward_bars 步的未来简单收益序列。
// fwd[i] = close[i+f]/close[i] - 1；末尾 f 个为 NaN。
func ForwardReturns(bars []model.Bar, forwardBars int) []float64 {
	closes := closesOf(bars)
	out := nanSeries(len(closes))
	if forwardBars <= 0 {
		return out
	}
	for i := 0; i+forwardBars < len(closes); i++ {
		if closes[i] != 0 {
			out[i] = closes[i+forwardBars]/closes[i] - 1
		}
	}
	return out
}

// Evaluate 对因子做全量评价：因子序列 + 未来收益序列 → IC/RankIC/ICIR。
func Evaluate(d *Def, bars []model.Bar, params map[string]any, cfg EvaluationConfig) (*Evaluation, error) {
	if cfg.ForwardBars <= 0 {
		cfg.ForwardBars = 1
	}
	if cfg.ICWindow <= 0 {
		cfg.ICWindow = 100
	}

	values := ComputeSeries(d, bars, params)
	fwd := ForwardReturns(bars, cfg.ForwardBars)

	// 对齐样本（剔 NaN）
	type sample struct {
		t      int64
		v, fwd float64
	}
	samples := make([]sample, 0, len(bars))
	for i := range bars {
		if !math.IsNaN(values[i]) && !math.IsNaN(fwd[i]) {
			samples = append(samples, sample{t: bars[i].Time, v: values[i], fwd: fwd[i]})
		}
	}
	if len(samples) < cfg.ICWindow+2 {
		return nil, nil // 样本不足，调用方给 400
	}

	ev := &Evaluation{
		FactorName:  d.Name,
		Version:     d.Version,
		ForwardBars: cfg.ForwardBars,
		Samples:     len(samples),
	}

	// 全样本一次性相关
	vs := make([]float64, len(samples))
	fs := make([]float64, len(samples))
	for i, s := range samples {
		vs[i], fs[i] = s.v, s.fwd
	}
	ev.OverallIC = pearson(vs, fs)
	ev.OverallRankIC = spearman(vs, fs)

	// 滚动 IC 序列
	w := cfg.ICWindow
	icVals := make([]float64, 0)
	rankICVals := make([]float64, 0)
	ev.ICSeries = make([]ICSeriesPoint, 0, len(samples)-w+1)
	for i := w; i <= len(samples); i++ {
		windowV := vs[i-w : i]
		windowF := fs[i-w : i]
		ic := pearson(windowV, windowF)
		ric := spearman(windowV, windowF)
		if math.IsNaN(ic) || math.IsNaN(ric) {
			continue
		}
		icVals = append(icVals, ic)
		rankICVals = append(rankICVals, ric)
		ev.ICSeries = append(ev.ICSeries, ICSeriesPoint{Time: samples[i-1].t, IC: ic, RankIC: ric})
	}

	ev.ICMean = mean(icVals)
	ev.ICStd = std(icVals)
	if ev.ICStd > 0 {
		ev.ICIR = ev.ICMean / ev.ICStd
	}
	ev.RankICMean = mean(rankICVals)
	ev.RankICStd = std(rankICVals)
	if ev.RankICStd > 0 {
		ev.RankICIR = ev.RankICMean / ev.RankICStd
	}
	pos := 0
	for _, v := range icVals {
		if v > 0 {
			pos++
		}
	}
	ev.ICPositivePct = float64(pos) / float64(len(icVals)) * 100
	return ev, nil
}

// LayeredBacktest 向量化分层回测：
// 用过去 lookback 根因子值的分位数作为分层阈值（避免前视），每个 bar 按
// 当期因子值归入 1..N 层；每层收益 = 该层 bar 的未来收益均值；净值曲线按
// 层复利累计。不模拟下单/手续费——纯因子收益归因。
func LayeredBacktest(d *Def, bars []model.Bar, params map[string]any, cfg EvaluationConfig, layerCount, lookback int) (*LayeredResult, error) {
	if layerCount < 2 {
		layerCount = 5
	}
	if lookback < 20 {
		lookback = 120
	}
	values := ComputeSeries(d, bars, params)
	fwd := ForwardReturns(bars, cfg.ForwardBars)

	res := &LayeredResult{
		FactorName:  d.Name,
		Version:     d.Version,
		ForwardBars: cfg.ForwardBars,
		LayerCount:  layerCount,
		Lookback:    lookback,
		Layers:      make([]Layer, layerCount),
	}
	for i := range res.Layers {
		res.Layers[i].Layer = i + 1
	}

	// 单标的分层：每个 bar 按当期因子值落入唯一一层。层收益序列 r_l(t) =
	// 该 bar 的未来收益（若 bar 属于层 l）否则 0；层净值按 r_l 逐 bar 复利。
	// 分层阈值用 [t-lookback, t) 的历史因子值估计（无前视）。
	eq := make([][]EquityPoint, layerCount)
	for l := range eq {
		eq[l] = append(eq[l], EquityPoint{Equity: 1})
	}
	hitSum := make([]float64, layerCount)
	hitCnt := make([]int, layerCount)
	samples := 0

	for i := lookback; i < len(bars); i++ {
		if math.IsNaN(values[i]) || math.IsNaN(fwd[i]) {
			continue
		}
		hist := make([]float64, 0, lookback)
		for j := i - lookback; j < i; j++ {
			if !math.IsNaN(values[j]) {
				hist = append(hist, values[j])
			}
		}
		if len(hist) < lookback/2 {
			continue
		}
		qs := quantiles(hist, layerCount)
		layer := 0
		for ; layer < layerCount-1; layer++ {
			if values[i] <= qs[layer] {
				break
			}
		}
		hitSum[layer] += fwd[i]
		hitCnt[layer]++
		samples++
		for l := 0; l < layerCount; l++ {
			prev := eq[l][len(eq[l])-1].Equity
			ret := 0.0
			if l == layer {
				ret = fwd[i]
			}
			eq[l] = append(eq[l], EquityPoint{Time: bars[i].Time, Equity: prev * (1 + ret)})
		}
	}

	res.Samples = samples
	res.EquityCurves = eq

	if samples == 0 {
		return res, nil
	}

	years := estimateYears(bars)
	for l := 0; l < layerCount; l++ {
		total := eq[l][len(eq[l])-1].Equity - 1
		res.Layers[l].Count = hitCnt[l]
		res.Layers[l].TotalReturn = total
		if hitCnt[l] > 0 {
			res.Layers[l].AvgForwardRet = hitSum[l] / float64(hitCnt[l])
		}
		if years > 0 && total > -1 {
			res.Layers[l].AnnualizedRet = (math.Pow(1+total, 1/years) - 1) * 100
		}
	}
	if layerCount >= 2 {
		res.LongShort = res.Layers[layerCount-1].TotalReturn - res.Layers[0].TotalReturn
	}
	res.Monotonicity = layerMonotonicity(res.Layers)
	return res, nil
}

// quantiles 返回把数据切成 n 层的 n-1 个分位阈值（第 k/n 分位）。
func quantiles(data []float64, n int) []float64 {
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sort.Float64s(sorted)
	out := make([]float64, n-1)
	for k := 1; k < n; k++ {
		idx := float64(k) * float64(len(sorted)-1) / float64(n)
		lo := int(math.Floor(idx))
		hi := int(math.Ceil(idx))
		if hi >= len(sorted) {
			hi = len(sorted) - 1
		}
		frac := idx - float64(lo)
		out[k-1] = sorted[lo]*(1-frac) + sorted[hi]*frac
	}
	return out
}

// layerMonotonicity 层收益与层序号（1..n）的 Spearman 相关，1=完全单调递增。
func layerMonotonicity(layers []Layer) float64 {
	n := len(layers)
	if n < 3 {
		return 0
	}
	x := make([]float64, n)
	y := make([]float64, n)
	for i, l := range layers {
		x[i] = float64(l.Layer)
		y[i] = l.TotalReturn
	}
	return spearman(x, y)
}

// estimateYears 用 K 线首尾时间差估年数（至少按 bar 数/tf 推断的 1 天兜底）。
func estimateYears(bars []model.Bar) float64 {
	if len(bars) < 2 {
		return 0
	}
	ms := bars[len(bars)-1].Time - bars[0].Time
	if ms <= 0 {
		return 0
	}
	return float64(ms) / (365.25 * 24 * 3600 * 1000)
}

// ComputeSeries 计算因子值序列（自动合并默认参数）。
func ComputeSeries(d *Def, bars []model.Bar, params map[string]any) []float64 {
	return d.Calc(bars, MergeParams(d, params))
}

// ToValues 把因子序列转成可序列化的 Value 列表（跳过 NaN）。
func ToValues(bars []model.Bar, series []float64, limit int) []Value {
	out := make([]Value, 0, len(series))
	for i, v := range series {
		if math.IsNaN(v) {
			continue
		}
		out = append(out, Value{Time: bars[i].Time, Value: v})
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// ── 统计原语 ──

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func std(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := mean(v)
	sq := 0.0
	for _, x := range v {
		sq += (x - m) * (x - m)
	}
	return math.Sqrt(sq / float64(len(v)-1))
}

func pearson(x, y []float64) float64 {
	n := len(x)
	if n != len(y) || n < 3 {
		return math.NaN()
	}
	mx, my := mean(x), mean(y)
	num, dx, dy := 0.0, 0.0, 0.0
	for i := 0; i < n; i++ {
		cx, cy := x[i]-mx, y[i]-my
		num += cx * cy
		dx += cx * cx
		dy += cy * cy
	}
	if dx == 0 || dy == 0 {
		return math.NaN()
	}
	return num / math.Sqrt(dx*dy)
}

func spearman(x, y []float64) float64 {
	n := len(x)
	if n != len(y) || n < 3 {
		return math.NaN()
	}
	return pearson(rank(x), rank(y))
}
