// Package analysis 回测可信度质检（对标 freqtrade lookahead-analysis / recursive-analysis）。
//
// Lookahead：在人为变体数据集上重跑回测，凡是在"本不该有信号的位置"出现、
// 或"本应有信号的位置"消失的入场即为前视偏差嫌疑。变体三种：
//   - shift_time_N    整体时间轴平移 N 根（内容不变）：因果策略信号应精确平移，
//     依赖绝对时间戳的策略（如按时间查表的未来函数）会错位暴露；
//   - truncate_right_N 截断末尾 N 根：截断线之前的基线信号应原样复现，
//     消失说明信号依赖了被截掉的未来数据；
//   - entry_cut       逐入场截断（freqtrade 核心做法）：对每个基线入场，
//     数据恰好在入场根结束时重跑，入场不复现 = 确凿未来函数。
//
// Recursive：在等长起步、长度递增的数据前缀序列（5 档）上分别回测，
// 比较同一历史时点的信号是否随数据变长而改变；改变即为递归/前视偏差。
package analysis

import (
	"fmt"
	"sort"

	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// Factory 为单次回测构建全新策略实例。bars 是本次运行的数据集：
// 需要全序列预计算的策略必须在工厂内基于传入 bars 完成预计算，
// 这样预计算中的未来函数才会随数据集变化而暴露。
type Factory func(symbol string, bars []model.Bar) (backtest.BacktestStrategy, error)

// SignalEvent 一次原始策略信号（Runner 过滤前），以 K 线索引为对齐基准。
type SignalEvent struct {
	Time      int64  `json:"time"`
	Index     int    `json:"index"`
	Direction string `json:"direction"` // LONG / SHORT / CLOSE
}

func isEntry(dir string) bool { return dir == "LONG" || dir == "SHORT" }

// ── 信号记录 ──

type signalRecorder struct {
	inner   backtest.BacktestStrategy
	signals []SignalEvent
}

func (r *signalRecorder) Name() string   { return r.inner.Name() }
func (r *signalRecorder) Symbol() string { return r.inner.Symbol() }

func (r *signalRecorder) OnTick(t model.Tick, s *backtest.StrategyState) (*model.Signal, error) {
	return r.inner.OnTick(t, s)
}

func (r *signalRecorder) OnBar(bar model.Bar, s *backtest.StrategyState) (*model.Signal, error) {
	sig, err := r.inner.OnBar(bar, s)
	if sig != nil {
		r.signals = append(r.signals, SignalEvent{Time: bar.Time, Index: s.BarIndex, Direction: sig.Direction})
	}
	return sig, err
}

// runOnce 在给定数据集上跑一次回测并返回全部原始信号。
// 滑点置零保证变体间执行价确定，信号差异只可能来自数据变化。
func runOnce(factory Factory, symbol string, bars []model.Bar, initialBalance float64) ([]SignalEvent, error) {
	strat, err := factory(symbol, bars)
	if err != nil {
		return nil, err
	}
	rec := &signalRecorder{inner: strat}
	cfg := backtest.DefaultRunnerConfig()
	cfg.InitialBalance = initialBalance
	cfg.Slippage = 0
	cfg.SlippageSeed = 1
	runner := backtest.NewRunner(cfg)
	runner.LoadBars(symbol, bars)
	if _, err := runner.Run(rec); err != nil {
		return nil, err
	}
	return rec.signals, nil
}

func entriesOf(signals []SignalEvent) []SignalEvent {
	out := make([]SignalEvent, 0, len(signals))
	for _, s := range signals {
		if isEntry(s.Direction) {
			out = append(out, s)
		}
	}
	return out
}

// signalIndexSet 建立 索引→方向 映射（同索引多信号时保留第一个）。
func signalIndexSet(signals []SignalEvent) map[int]string {
	m := make(map[int]string, len(signals))
	for _, s := range signals {
		if _, ok := m[s.Index]; !ok {
			m[s.Index] = s.Direction
		}
	}
	return m
}

func inferIntervalMs(bars []model.Bar) int64 {
	var best int64
	for i := 1; i < len(bars); i++ {
		d := bars[i].Time - bars[i-1].Time
		if d > 0 && (best == 0 || d < best) {
			best = d
		}
	}
	return best
}

// ── 配置 ──

type Config struct {
	Symbol         string
	InitialBalance float64
	MinSignals     int   // 基线最少入场信号数，不足判 inconclusive（默认 3）
	MaxEntryChecks int   // 逐入场截断检查上限（默认 10）
	ShiftN         []int // 时间平移档位（默认 [5, 10]）
	TruncateN      []int // 截尾档位（默认 [5, 10, 20]）
	PrefixLevels   int   // recursive 前缀档位数（默认 5）
	PrefixMinBars  int   // recursive 最短前缀（默认 60）
}

func (c *Config) defaults() {
	if c.InitialBalance <= 0 {
		c.InitialBalance = 100000
	}
	if c.MinSignals <= 0 {
		c.MinSignals = 3
	}
	if c.MaxEntryChecks <= 0 {
		c.MaxEntryChecks = 10
	}
	if len(c.ShiftN) == 0 {
		c.ShiftN = []int{5, 10}
	}
	if len(c.TruncateN) == 0 {
		c.TruncateN = []int{5, 10, 20}
	}
	if c.PrefixLevels <= 0 {
		c.PrefixLevels = 5
	}
	if c.PrefixMinBars <= 0 {
		c.PrefixMinBars = 60
	}
}

// ── Lookahead ──

// FalseSignal 一处偏差嫌疑信号。
type FalseSignal struct {
	Signal  SignalEvent `json:"signal"`
	Variant string      `json:"variant"`
	Kind    string      `json:"kind"` // missing=基线有而变体消失 | displaced=出现在异常位置
	Detail  string      `json:"detail"`
}

// VariantReport 单个变体数据集的回测对比结果。
type VariantReport struct {
	Name           string        `json:"name"`
	Kind           string        `json:"kind"` // shift_time | truncate_right | entry_cut
	N              int           `json:"n"`
	VariantBars    int           `json:"variant_bars"`
	TotalEntries   int           `json:"total_entries"`
	FalseEntries   []FalseSignal `json:"false_entries"`
	MissingCount   int           `json:"missing_count"`
	DisplacedCount int           `json:"displaced_count"`
}

// LookaheadResult lookahead 分析结论。
type LookaheadResult struct {
	Conclusion      string          `json:"conclusion"` // biased | unbiased | inconclusive
	Biased          bool            `json:"biased"`
	Confidence      string          `json:"confidence"` // high | medium | low
	TotalEntries    int             `json:"total_entries"`
	CheckedEntries  int             `json:"checked_entries"`
	FalseEntryCount int             `json:"false_entry_count"`
	VariantCount    int             `json:"variant_count"`
	BaselineEntries []SignalEvent   `json:"baseline_entries"`
	Variants        []VariantReport `json:"variants"`
	Summary         string          `json:"summary"`
}

const minVariantBars = 30

// RunLookahead 执行 lookahead 偏差分析。bars 为完整历史数据（已按时间升序）。
func RunLookahead(cfg Config, factory Factory, bars []model.Bar) (*LookaheadResult, error) {
	cfg.defaults()
	if len(bars) < cfg.PrefixMinBars {
		return nil, fmt.Errorf("数据不足：%d 根K线，至少需要 %d 根", len(bars), cfg.PrefixMinBars)
	}

	baseline, err := runOnce(factory, cfg.Symbol, bars, cfg.InitialBalance)
	if err != nil {
		return nil, fmt.Errorf("基线回测失败: %w", err)
	}
	baseEntries := entriesOf(baseline)

	res := &LookaheadResult{
		Conclusion:      "unbiased",
		Confidence:      "high",
		TotalEntries:    len(baseEntries),
		BaselineEntries: baseEntries,
		Variants:        []VariantReport{},
	}
	if len(baseEntries) < cfg.MinSignals {
		res.Conclusion = "inconclusive"
		res.Confidence = "low"
		res.Summary = fmt.Sprintf("基线仅 %d 个入场信号（少于 %d），无法判定，请扩大时间范围", len(baseEntries), cfg.MinSignals)
		return res, nil
	}

	intervalMs := inferIntervalMs(bars)

	// ── 变体1：整体时间轴平移 N 根（内容不变） ──
	for _, n := range cfg.ShiftN {
		if n <= 0 || intervalMs <= 0 {
			continue
		}
		shifted := make([]model.Bar, len(bars))
		delta := int64(n) * intervalMs
		for i, b := range bars {
			b.Time += delta
			shifted[i] = b
		}
		name := fmt.Sprintf("shift_time_%d", n)
		variantSignals, err := runOnce(factory, cfg.Symbol, shifted, cfg.InitialBalance)
		if err != nil {
			continue
		}
		vr := compareVariant(name, "shift_time", n, len(shifted), baseline, variantSignals,
			func(s SignalEvent) (int64, bool) { return s.Time + delta, true },
			func(v SignalEvent) (int64, bool) { return v.Time - delta, true })
		res.Variants = append(res.Variants, vr)
	}

	// ── 变体2：截断末尾 N 根 ──
	for _, n := range cfg.TruncateN {
		if n <= 0 || len(bars)-n < minVariantBars {
			continue
		}
		cut := bars[:len(bars)-n]
		name := fmt.Sprintf("truncate_right_%d", n)
		variantSignals, err := runOnce(factory, cfg.Symbol, cut, cfg.InitialBalance)
		if err != nil {
			continue
		}
		vr := compareVariant(name, "truncate_right", n, len(cut), baseline, variantSignals,
			func(s SignalEvent) (int64, bool) {
				if s.Index >= len(cut) {
					return 0, false
				}
				return s.Time, true
			},
			func(v SignalEvent) (int64, bool) { return v.Time, true })
		res.Variants = append(res.Variants, vr)
	}

	// ── 变体3：逐入场截断（freqtrade 核心检查） ──
	checks := baseEntries
	if len(checks) > cfg.MaxEntryChecks {
		sampled := make([]SignalEvent, 0, cfg.MaxEntryChecks)
		step := float64(len(baseEntries)) / float64(cfg.MaxEntryChecks)
		for i := 0; i < cfg.MaxEntryChecks; i++ {
			sampled = append(sampled, baseEntries[int(float64(i)*step)])
		}
		checks = sampled
	}
	for _, entry := range checks {
		k := entry.Index
		if k+1 < minVariantBars || k+1 > len(bars) {
			continue
		}
		cut := bars[:k+1]
		name := fmt.Sprintf("entry_cut@%d", entry.Time)
		variantSignals, err := runOnce(factory, cfg.Symbol, cut, cfg.InitialBalance)
		if err != nil {
			continue
		}
		res.CheckedEntries++
		vr := VariantReport{Name: name, Kind: "entry_cut", N: k + 1, VariantBars: len(cut), FalseEntries: []FalseSignal{}}
		vEntries := signalIndexSet(entriesOf(variantSignals))
		vr.TotalEntries = len(vEntries)
		if _, ok := vEntries[k]; !ok {
			vr.FalseEntries = append(vr.FalseEntries, FalseSignal{
				Signal:  entry,
				Variant: name,
				Kind:    "missing",
				Detail:  "数据截断到入场根即消失，说明该信号依赖了入场之后的未来数据",
			})
			vr.MissingCount++
		}
		res.Variants = append(res.Variants, vr)
	}

	// ── 汇总结论 ──
	entryCutFailed := false
	for _, vr := range res.Variants {
		res.FalseEntryCount += vr.MissingCount + vr.DisplacedCount
		if vr.Kind == "entry_cut" && vr.MissingCount > 0 {
			entryCutFailed = true
		}
	}
	res.VariantCount = len(res.Variants)
	switch {
	case res.FalseEntryCount > 0:
		res.Conclusion = "biased"
		res.Biased = true
		if entryCutFailed {
			res.Confidence = "high"
		} else {
			res.Confidence = "medium"
		}
		res.Summary = fmt.Sprintf("检出前视偏差：%d 个变体回测中共 %d 处异常入场信号", res.VariantCount, res.FalseEntryCount)
	default:
		res.Summary = fmt.Sprintf("%d 个入场信号在 %d 个变体回测中全部复现，未发现前视偏差", res.TotalEntries, res.VariantCount)
	}
	return res, nil
}

// compareVariant 对比基线与变体的信号序列。
// expectedOf：基线信号 → 变体中期望的时间（bool=false 表示该信号落在变体范围外，不参与检查）；
// originOf：  变体信号 → 映射回基线坐标的时间，用于判定"异常位置"。
func compareVariant(name, kind string, n, variantBars int, baseline, variant []SignalEvent,
	expectedOf func(SignalEvent) (int64, bool), originOf func(SignalEvent) (int64, bool)) VariantReport {

	vr := VariantReport{Name: name, Kind: kind, N: n, VariantBars: variantBars, FalseEntries: []FalseSignal{}}

	expected := make(map[int64]SignalEvent) // 期望时间 -> 基线信号
	for _, s := range baseline {
		if !isEntry(s.Direction) {
			continue
		}
		if t, ok := expectedOf(s); ok {
			expected[t] = s
		}
	}

	actual := make(map[int64]SignalEvent)
	for _, s := range variant {
		if isEntry(s.Direction) {
			if _, dup := actual[s.Time]; !dup {
				actual[s.Time] = s
			}
		}
	}
	vr.TotalEntries = len(actual)

	originTimes := make(map[int64]bool) // 基线入场时间全集（判定变体信号是否异常位置）
	for _, s := range baseline {
		if isEntry(s.Direction) {
			originTimes[s.Time] = true
		}
	}

	for t, v := range actual {
		if _, ok := expected[t]; ok {
			continue
		}
		// 出现在期望集合之外的位置
		origin, _ := originOf(v)
		detail := "变体中出现在异常位置的入场"
		if originTimes[origin] {
			detail = "该入场对应基线其他时点的信号，位置发生漂移"
		}
		vr.FalseEntries = append(vr.FalseEntries, FalseSignal{Signal: v, Variant: name, Kind: "displaced", Detail: detail})
		vr.DisplacedCount++
	}
	for t, s := range expected {
		if _, ok := actual[t]; !ok {
			vr.FalseEntries = append(vr.FalseEntries, FalseSignal{
				Signal:  s,
				Variant: name,
				Kind:    "missing",
				Detail:  "基线入场在变体数据上未复现，信号依赖了被改动/截断的数据",
			})
			vr.MissingCount++
		}
	}

	sort.Slice(vr.FalseEntries, func(i, j int) bool {
		return vr.FalseEntries[i].Signal.Time < vr.FalseEntries[j].Signal.Time
	})
	return vr
}

// ── Recursive ──

// PrefixLevel 一档前缀数据集的回测概况。
type PrefixLevel struct {
	Name    string `json:"name"`
	Bars    int    `json:"bars"`
	Entries int    `json:"entries"`
	Exits   int    `json:"exits"`
}

// UnstablePoint 同一历史时点在不同数据长度下信号不一致。
type UnstablePoint struct {
	Index  int               `json:"index"`
	Time   int64             `json:"time"`
	ByLens map[string]string `json:"by_lens"` // 档位名 -> 方向（"" = 无信号）
}

// RecursiveResult recursive 分析结论。
type RecursiveResult struct {
	Conclusion     string          `json:"conclusion"` // recursive | stable | inconclusive
	Recursive      bool            `json:"recursive"`
	Confidence     string          `json:"confidence"` // high | medium | low
	Levels         []PrefixLevel   `json:"levels"`
	UnstablePoints []UnstablePoint `json:"unstable_points"`
	UnstableCount  int             `json:"unstable_count"`
	ComparedPoints int             `json:"compared_points"`
	Summary        string          `json:"summary"`
}

// RunRecursive 执行递归偏差分析：同一起点、长度递增的前缀序列上分别回测，
// 对比共享历史区间的信号稳定性。
func RunRecursive(cfg Config, factory Factory, bars []model.Bar) (*RecursiveResult, error) {
	cfg.defaults()
	if len(bars) < cfg.PrefixMinBars+10 {
		return nil, fmt.Errorf("数据不足：%d 根K线，至少需要 %d 根", len(bars), cfg.PrefixMinBars+10)
	}

	// 前缀档位：最短 PrefixMinBars，最长为全量，均匀取 PrefixLevels 档
	lengths := prefixLengths(len(bars), cfg.PrefixLevels, cfg.PrefixMinBars)
	if len(lengths) < 2 {
		return nil, fmt.Errorf("数据不足：无法构造至少 2 档前缀（%d 根K线）", len(bars))
	}

	res := &RecursiveResult{
		Conclusion:     "stable",
		Confidence:     "high",
		Levels:         []PrefixLevel{},
		UnstablePoints: []UnstablePoint{},
	}

	levelSignals := make([]map[int]string, len(lengths))
	levelNames := make([]string, len(lengths))
	for i, l := range lengths {
		name := fmt.Sprintf("prefix_%d", l)
		levelNames[i] = name
		signals, err := runOnce(factory, cfg.Symbol, bars[:l], cfg.InitialBalance)
		if err != nil {
			return nil, fmt.Errorf("前缀 %d 回测失败: %w", l, err)
		}
		idx := signalIndexSet(signals)
		levelSignals[i] = idx
		entries, exits := 0, 0
		for _, s := range signals {
			if isEntry(s.Direction) {
				entries++
			} else if s.Direction == "CLOSE" {
				exits++
			}
		}
		res.Levels = append(res.Levels, PrefixLevel{Name: name, Bars: l, Entries: entries, Exits: exits})
	}

	// 在共享区间 [0, 最短前缀) 上逐时点对比各档信号
	shared := lengths[0]
	res.ComparedPoints = shared
	for idx := 0; idx < shared; idx++ {
		var first string
		stable := true
		byLens := make(map[string]string, len(lengths))
		for i := range lengths {
			dir := levelSignals[i][idx]
			byLens[levelNames[i]] = dir
			if i == 0 {
				first = dir
			} else if dir != first {
				stable = false
			}
		}
		if !stable {
			res.UnstablePoints = append(res.UnstablePoints, UnstablePoint{
				Index:  idx,
				Time:   bars[idx].Time,
				ByLens: byLens,
			})
		}
	}
	res.UnstableCount = len(res.UnstablePoints)

	totalEntries := res.Levels[len(res.Levels)-1].Entries
	switch {
	case totalEntries < cfg.MinSignals:
		res.Conclusion = "inconclusive"
		res.Confidence = "low"
		res.Summary = fmt.Sprintf("全量数据仅 %d 个入场信号（少于 %d），无法判定，请扩大时间范围", totalEntries, cfg.MinSignals)
	case res.UnstableCount > 0:
		res.Conclusion = "recursive"
		res.Recursive = true
		frac := float64(res.UnstableCount) / float64(shared)
		if res.UnstableCount >= 3 || frac > 0.01 {
			res.Confidence = "high"
		} else {
			res.Confidence = "medium"
		}
		res.Summary = fmt.Sprintf("检出递归/前视偏差：%d 档前缀对比中 %d 个历史时点信号随数据变长而改变",
			len(res.Levels), res.UnstableCount)
	default:
		res.Summary = fmt.Sprintf("%d 档前缀（%d~%d 根）共享区间内信号完全一致，未发现递归偏差",
			len(res.Levels), lengths[0], lengths[len(lengths)-1])
	}
	return res, nil
}

// prefixLengths 生成递增前缀长度序列：首档 minBars，末档为 total，中间均匀分布。
func prefixLengths(total, levels, minBars int) []int {
	if total < minBars {
		return nil
	}
	if levels < 2 {
		levels = 2
	}
	step := (total - minBars) / (levels - 1)
	if step < 1 {
		step = 1
	}
	seen := make(map[int]bool)
	out := make([]int, 0, levels)
	for i := 0; i < levels-1; i++ {
		l := minBars + i*step
		if l >= total {
			break
		}
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	if !seen[total] {
		out = append(out, total)
	}
	return out
}
