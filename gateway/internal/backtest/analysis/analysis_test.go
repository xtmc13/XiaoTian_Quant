package analysis

import (
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
	res, err := RunLookahead(testConfig(), futureFactory, bars)
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
	res, err := RunLookahead(testConfig(), cleanFactory, bars)
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
	res, err := RunLookahead(cfg, factory, bars)
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
	res, err := RunRecursive(testConfig(), futureFactory, bars)
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
}

func TestRecursiveCleanStrategyStable(t *testing.T) {
	bars := synthBars(400)
	res, err := RunRecursive(testConfig(), cleanFactory, bars)
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
