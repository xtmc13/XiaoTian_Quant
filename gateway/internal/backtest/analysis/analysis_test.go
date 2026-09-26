package analysis

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 玩具策略 ──

// futureStrategy 含未来函数：构造时对全序列预计算，"未来第5根上涨则入场"，
// 3 根后离场。信号位置依赖入场之后的数据，因果上不可能实现。
// 信号不做仓位门控、完全由数据驱动，保证测试确定性。
type futureStrategy struct {
	symbol  string
	signals map[int]string
}

func newFutureStrategy(symbol string, bars []model.Bar) *futureStrategy {
	signals := make(map[int]string)
	for i := 30; i+5 < len(bars); i++ {
		if bars[i+5].Close > bars[i].Close {
			signals[i] = "LONG"
			if _, dup := signals[i+3]; !dup {
				signals[i+3] = "CLOSE"
			}
		}
	}
	return &futureStrategy{symbol: symbol, signals: signals}
}

func (s *futureStrategy) Name() string   { return "future_toy" }
func (s *futureStrategy) Symbol() string { return s.symbol }
func (s *futureStrategy) OnTick(t model.Tick, st *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *futureStrategy) OnBar(bar model.Bar, st *backtest.StrategyState) (*model.Signal, error) {
	if dir, ok := s.signals[st.BarIndex]; ok {
		return &model.Signal{Direction: dir, Symbol: s.symbol, Strategy: s.Name(), Reason: "future window"}, nil
	}
	return nil, nil
}

// smaCrossStrategy 干净因果策略：只用 state.Bars 中的历史数据。
type smaCrossStrategy struct {
	symbol     string
	fast, slow int
}

func (s *smaCrossStrategy) Name() string   { return "sma_cross_toy" }
func (s *smaCrossStrategy) Symbol() string { return s.symbol }
func (s *smaCrossStrategy) OnTick(t model.Tick, st *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}

func smaTail(bars []model.Bar, n int) float64 {
	if len(bars) < n {
		return 0
	}
	sum := 0.0
	for _, b := range bars[len(bars)-n:] {
		sum += b.Close
	}
	return sum / float64(n)
}

func (s *smaCrossStrategy) OnBar(bar model.Bar, st *backtest.StrategyState) (*model.Signal, error) {
	bars := st.Bars
	if len(bars) < s.slow+1 {
		return nil, nil
	}
	fastNow, slowNow := smaTail(bars, s.fast), smaTail(bars, s.slow)
	prev := bars[:len(bars)-1]
	fastPrev, slowPrev := smaTail(prev, s.fast), smaTail(prev, s.slow)

	golden := fastPrev <= slowPrev && fastNow > slowNow
	death := fastPrev >= slowPrev && fastNow < slowNow

	if golden && (st.Position == nil || st.Position.IsClosed) {
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "golden cross"}, nil
	}
	if death && st.Position != nil && !st.Position.IsClosed {
		return &model.Signal{Direction: "CLOSE", Symbol: s.symbol, Strategy: s.Name(), Reason: "death cross"}, nil
	}
	return nil, nil
}

// ── 数据生成 ──

func synthBars(n int) []model.Bar {
	bars := make([]model.Bar, n)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	prevClose := 100.0
	for i := 0; i < n; i++ {
		close := 100 + 10*math.Sin(float64(i)/4.0) + float64(i)*0.05
		open := prevClose
		high := math.Max(open, close) + 0.5
		low := math.Min(open, close) - 0.5
		bars[i] = model.Bar{
			Symbol:   "TESTUSDT",
			Open:     open,
			High:     high,
			Low:      low,
			Close:    close,
			Volume:   1000,
			Interval: "1h",
			Time:     base + int64(i)*3600_000,
		}
		prevClose = close
	}
	return bars
}

// trendBars 纯上涨趋势数据：futureStrategy 在每根都触发 LONG，
// 使递归测试的不稳定点位置完全确定（最短前缀末尾 5 根）。
func trendBars(n int) []model.Bar {
	bars := synthBars(n)
	for i := range bars {
		c := 100 + float64(i)*0.1
		bars[i].Close = c
		bars[i].Open = c - 0.1
		bars[i].High = c + 0.5
		bars[i].Low = c - 0.6
	}
	return bars
}

func futureFactory(symbol string, bars []model.Bar) (backtest.BacktestStrategy, error) {
	return newFutureStrategy(symbol, bars), nil
}

func cleanFactory(symbol string, bars []model.Bar) (backtest.BacktestStrategy, error) {
	return &smaCrossStrategy{symbol: symbol, fast: 5, slow: 20}, nil
}

func testConfig() Config {
	return Config{Symbol: "TESTUSDT", InitialBalance: 100000, MinSignals: 3}
}

// ── Lookahead ──

func TestLookaheadDetectsFutureStrategy(t *testing.T) {
	bars := synthBars(400)
	res, err := RunLookahead(context.Background(), testConfig(), futureFactory, bars)
	if err != nil {
		t.Fatalf("RunLookahead: %v", err)
	}
	if res.Conclusion != "biased" || !res.Biased {
		t.Fatalf("expected biased, got %s (summary: %s)", res.Conclusion, res.Summary)
	}
	if res.FalseEntryCount == 0 {
		t.Fatal("expected false entries > 0")
	}
	// 逐入场截断是确凿检查：必须有 entry_cut 变体报告消失信号 → confidence high
	entryCutFailed := false
	for _, v := range res.Variants {
		if v.Kind == "entry_cut" && v.MissingCount > 0 {
			entryCutFailed = true
		}
	}
	if !entryCutFailed {
		t.Fatal("expected at least one entry_cut variant with missing signals")
	}
	if res.Confidence != "high" {
		t.Fatalf("expected high confidence, got %s", res.Confidence)
	}
	if res.CheckedEntries == 0 {
		t.Fatal("expected checked entries > 0")
	}
}

func TestLookaheadCleanStrategyUnbiased(t *testing.T) {
	bars := synthBars(400)
	res, err := RunLookahead(context.Background(), testConfig(), cleanFactory, bars)
	if err != nil {
		t.Fatalf("RunLookahead: %v", err)
	}
	if res.TotalEntries < 3 {
		t.Fatalf("clean strategy produced too few entries (%d), test data unusable", res.TotalEntries)
	}
	if res.Conclusion != "unbiased" || res.Biased {
		t.Fatalf("expected unbiased, got %s (summary: %s)", res.Conclusion, res.Summary)
	}
	if res.FalseEntryCount != 0 {
		t.Fatalf("expected 0 false entries, got %d", res.FalseEntryCount)
	}
	// 2 shift + 3 truncate + 最多 10 entry_cut
	if res.VariantCount < 5 {
		t.Fatalf("expected at least 5 variants, got %d", res.VariantCount)
	}
}

func TestLookaheadInconclusiveWhenFewSignals(t *testing.T) {
	bars := synthBars(400)
	factory := func(symbol string, bars []model.Bar) (backtest.BacktestStrategy, error) {
		return &smaCrossStrategy{symbol: symbol, fast: 50, slow: 200}, nil
	}
	cfg := testConfig()
	cfg.MinSignals = 100
	res, err := RunLookahead(context.Background(), cfg, factory, bars)
	if err != nil {
		t.Fatalf("RunLookahead: %v", err)
	}
	if res.Conclusion != "inconclusive" {
		t.Fatalf("expected inconclusive, got %s", res.Conclusion)
	}
}

// ── Recursive ──

func TestRecursiveDetectsFutureStrategy(t *testing.T) {
	bars := trendBars(400)
	res, err := RunRecursive(context.Background(), testConfig(), futureFactory, bars)
	if err != nil {
		t.Fatalf("RunRecursive: %v", err)
	}
	if res.Conclusion != "recursive" || !res.Recursive {
		t.Fatalf("expected recursive, got %s (summary: %s)", res.Conclusion, res.Summary)
	}
	// 最短前缀 60 根：未来函数需要 i+5 < len，故索引 55~59 在最短档无信号、
	// 在更长档有 LONG，必然产生恰好 5 个不稳定点
	if res.UnstableCount != 5 {
		t.Fatalf("expected 5 unstable points, got %d (%v)", res.UnstableCount, res.UnstablePoints)
	}
	for i, p := range res.UnstablePoints {
		if p.Index != 55+i {
			t.Fatalf("unstable point %d at index %d, want %d", i, p.Index, 55+i)
		}
		if p.Time != bars[p.Index].Time {
			t.Fatalf("unstable point time mismatch at index %d", p.Index)
		}
	}
	if len(res.Levels) != 5 {
		t.Fatalf("expected 5 prefix levels, got %d", len(res.Levels))
	}
	// 起点偏移维度并入结果：futureStrategy 的相对前瞻（i+5）对平移不变，
	// 偏移组应稳定（0 个不稳定点），但组必须存在。
	if len(res.Groups) != 2 || res.Groups[0].Kind != "prefix" || res.Groups[1].Kind != "start_offset" {
		t.Fatalf("expected prefix+start_offset groups, got %+v", res.Groups)
	}
	if res.Groups[0].UnstableCount != 5 {
		t.Fatalf("prefix group unstable = %d, want 5", res.Groups[0].UnstableCount)
	}
	if res.Groups[1].UnstableCount != 0 {
		t.Fatalf("futureStrategy 相对前瞻对起点平移不变，偏移组应稳定: %d", res.Groups[1].UnstableCount)
	}
}

// warmupStrategy warmup 不收敛玩具策略：入场条件依赖"自数据起点以来的累计均价"
// （递归均值），起点右移后同一绝对时点的均值不同、信号随之改变。
// 同起点前缀对比对它天然盲（前缀共享起点，共享区间累计值完全一致）。
type warmupStrategy struct {
	symbol string
	mean   []float64 // mean[i] = 构造数据 close[0..i] 的累计均值
}

func newWarmupStrategy(symbol string, bars []model.Bar) *warmupStrategy {
	mean := make([]float64, len(bars))
	sum := 0.0
	for i, b := range bars {
		sum += b.Close
		mean[i] = sum / float64(i+1)
	}
	return &warmupStrategy{symbol: symbol, mean: mean}
}

func (s *warmupStrategy) Name() string   { return "warmup_toy" }
func (s *warmupStrategy) Symbol() string { return s.symbol }
func (s *warmupStrategy) OnTick(t model.Tick, st *backtest.StrategyState) (*model.Signal, error) {
	return nil, nil
}
func (s *warmupStrategy) OnBar(bar model.Bar, st *backtest.StrategyState) (*model.Signal, error) {
	if st.BarIndex < len(s.mean) && bar.Close > s.mean[st.BarIndex]*1.02 {
		return &model.Signal{Direction: "LONG", Symbol: s.symbol, Strategy: s.Name(), Reason: "above cumulative mean"}, nil
	}
	return nil, nil
}

// stepBars 阶梯数据：前 jump 根 close=100，之后跳到 110 并保持。
// 累计均值收敛速度取决于起点位置——起点越晚，均值越快贴近 110，信号越早消失。
func stepBars(n, jump int) []model.Bar {
	bars := synthBars(n)
	for i := range bars {
		c := 100.0
		if i >= jump {
			c = 110.0
		}
		bars[i].Close = c
		bars[i].Open = c - 0.1
		bars[i].High = c + 0.2
		bars[i].Low = c - 0.3
	}
	return bars
}

func warmupFactory(symbol string, bars []model.Bar) (backtest.BacktestStrategy, error) {
	return newWarmupStrategy(symbol, bars), nil
}

// 起点偏移维度检出 warmup 不收敛：前缀组稳定（共享起点）而偏移组在重叠尾部
// 全部时点不稳定（s=0 档 LONG、s≥85 档均值已收敛无信号），结论必须 recursive。
func TestRecursiveStartOffsetDetectsWarmupNonConvergence(t *testing.T) {
	bars := stepBars(400, 100)
	res, err := RunRecursive(context.Background(), testConfig(), warmupFactory, bars)
	if err != nil {
		t.Fatalf("RunRecursive: %v", err)
	}
	if res.Conclusion != "recursive" || !res.Recursive {
		t.Fatalf("expected recursive, got %s (summary: %s)", res.Conclusion, res.Summary)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("expected 2 variant groups, got %+v", res.Groups)
	}
	prefix, offset := res.Groups[0], res.Groups[1]
	if prefix.UnstableCount != 0 {
		t.Fatalf("warmup 策略在同起点前缀下应稳定（旧维度盲区）: %d", prefix.UnstableCount)
	}
	// 偏移档位 [0,85,170,255,340]，重叠尾部从 340+30=370 起共 30 个时点，
	// s=0 档信号 LONG 维持到 t≈462，其余档在重叠区全无信号 → 30 点全不稳定。
	if offset.UnstableCount != 30 {
		t.Fatalf("start_offset unstable = %d, want 30 (%v)", offset.UnstableCount, offset.UnstablePoints)
	}
	if offset.ComparedPoints != 30 {
		t.Fatalf("start_offset compared points = %d, want 30", offset.ComparedPoints)
	}
	if len(offset.Levels) != 5 || offset.Levels[4].Bars != 60 {
		t.Fatalf("offset levels wrong: %+v", offset.Levels)
	}
	if res.UnstableCount != 30 {
		t.Fatalf("total unstable = %d, want 30", res.UnstableCount)
	}
}

// 干净因果策略在起点偏移维度同样稳定（ warmup 豁免区外无差异，不误报）。
func TestRecursiveStartOffsetCleanStrategyStable(t *testing.T) {
	bars := synthBars(400)
	res, err := RunRecursive(context.Background(), testConfig(), cleanFactory, bars)
	if err != nil {
		t.Fatalf("RunRecursive: %v", err)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("expected 2 variant groups, got %+v", res.Groups)
	}
	if res.Groups[1].UnstableCount != 0 {
		t.Fatalf("clean strategy start_offset must be stable: %+v", res.Groups[1].UnstablePoints)
	}
	if res.Conclusion != "stable" {
		t.Fatalf("expected stable, got %s (summary: %s)", res.Conclusion, res.Summary)
	}
}

// ctx 取消应立即中止（任务级取消端点的语义）。
func TestRunRecursiveHonoursContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunRecursive(ctx, testConfig(), cleanFactory, synthBars(400)); err == nil {
		t.Fatal("cancelled ctx must abort recursive analysis")
	}
	if _, err := RunLookahead(ctx, testConfig(), cleanFactory, synthBars(400)); err == nil {
		t.Fatal("cancelled ctx must abort lookahead analysis")
	}
}

func TestStartOffsets(t *testing.T) {
	cases := []struct {
		total, levels, min int
		want               []int
	}{
		{400, 5, 60, []int{0, 85, 170, 255, 340}},
		{100, 5, 60, []int{0, 10, 20, 30, 40}},
		{61, 5, 60, []int{0, 1}},
		{60, 5, 60, nil},
		{50, 5, 60, nil},
	}
	for _, c := range cases {
		got := startOffsets(c.total, c.levels, c.min)
		if len(got) != len(c.want) {
			t.Fatalf("startOffsets(%d,%d,%d) = %v, want %v", c.total, c.levels, c.min, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("startOffsets(%d,%d,%d) = %v, want %v", c.total, c.levels, c.min, got, c.want)
			}
		}
	}
}

func TestRecursiveCleanStrategyStable(t *testing.T) {
	bars := synthBars(400)
	res, err := RunRecursive(context.Background(), testConfig(), cleanFactory, bars)
	if err != nil {
		t.Fatalf("RunRecursive: %v", err)
	}
	if res.Levels[len(res.Levels)-1].Entries < 3 {
		t.Fatalf("clean strategy produced too few entries (%d), test data unusable",
			res.Levels[len(res.Levels)-1].Entries)
	}
	if res.Conclusion != "stable" || res.Recursive {
		t.Fatalf("expected stable, got %s (summary: %s)", res.Conclusion, res.Summary)
	}
	if res.UnstableCount != 0 {
		t.Fatalf("expected 0 unstable points, got %d", res.UnstableCount)
	}
}

// ── helpers ──

func TestPrefixLengths(t *testing.T) {
	cases := []struct {
		total, levels, min int
		want               []int
	}{
		{400, 5, 60, []int{60, 145, 230, 315, 400}},
		{100, 5, 60, []int{60, 70, 80, 90, 100}},
		{61, 5, 60, []int{60, 61}},
		{50, 5, 60, nil},
	}
	for _, c := range cases {
		got := prefixLengths(c.total, c.levels, c.min)
		if len(got) != len(c.want) {
			t.Fatalf("prefixLengths(%d,%d,%d) = %v, want %v", c.total, c.levels, c.min, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("prefixLengths(%d,%d,%d) = %v, want %v", c.total, c.levels, c.min, got, c.want)
			}
		}
	}
}

func TestInferIntervalMs(t *testing.T) {
	bars := synthBars(10)
	if got := inferIntervalMs(bars); got != 3600_000 {
		t.Fatalf("inferIntervalMs = %d, want 3600000", got)
	}
}
