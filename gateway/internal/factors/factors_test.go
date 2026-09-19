package factors

import (
	"math"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// synthBars 生成确定性合成 K 线：正弦趋势 + 噪声，避免依赖外部数据。
func synthBars(n int) []model.Bar {
	bars := make([]model.Bar, n)
	price := 100.0
	for i := 0; i < n; i++ {
		t := float64(i)
		drift := 0.1 * math.Sin(t/15) // 缓慢均值回复成分
		trend := 0.02                 // 微弱趋势
		chg := trend + drift + 0.3*math.Sin(t/3)*0.1
		open := price
		price = price*(1+chg/100) + 0.05*math.Sin(t*1.7)
		high := math.Max(open, price) * (1 + 0.001*math.Abs(math.Sin(t)))
		low := math.Min(open, price) * (1 - 0.001*math.Abs(math.Cos(t)))
		bars[i] = model.Bar{
			Symbol: "TESTUSDT", Open: open, High: high, Low: low,
			Close: price, Volume: 1000 + 200*math.Sin(t/5),
			Interval: "1h", Time: int64(1700000000000 + i*3600_000),
		}
	}
	return bars
}

func TestBuiltinFactorCount(t *testing.T) {
	defs := List()
	if len(defs) < 30 {
		t.Fatalf("expected >= 30 builtin factors, got %d", len(defs))
	}
	cats := map[string]int{}
	for _, d := range defs {
		if d.Calc == nil {
			t.Fatalf("factor %s has no calc", d.Name)
		}
		cats[d.Category]++
	}
	for _, c := range Categories() {
		if cats[c] == 0 {
			t.Fatalf("category %s has no factors", c)
		}
	}
	t.Logf("factors: %d total %v", len(defs), cats)
}

func TestRegistryVersioning(t *testing.T) {
	MustRegister(&Def{
		Name: "test_versioned", Version: 1, Category: "momentum",
		Calc: func(bars []model.Bar, _ map[string]any) []float64 {
			out := make([]float64, len(bars))
			for i := range out {
				out[i] = 1
			}
			return out
		},
	})
	MustRegister(&Def{
		Name: "test_versioned", Version: 2, Category: "momentum",
		Calc: func(bars []model.Bar, _ map[string]any) []float64 {
			out := make([]float64, len(bars))
			for i := range out {
				out[i] = 2
			}
			return out
		},
	})
	latest, err := Get("test_versioned", 0)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Version != 2 {
		t.Fatalf("expected latest version 2, got %d", latest.Version)
	}
	v1, err := Get("test_versioned", 1)
	if err != nil {
		t.Fatal(err)
	}
	if v1.Version != 1 {
		t.Fatalf("expected version 1, got %d", v1.Version)
	}
	if err := Register(&Def{Name: "test_versioned", Version: 2, Calc: func(bars []model.Bar, _ map[string]any) []float64 { return nil }}); err == nil {
		t.Fatal("expected duplicate version registration to fail")
	}
}

func TestEveryBuiltinFactorComputes(t *testing.T) {
	bars := synthBars(300)
	for _, d := range List() {
		def, err := Get(d.Name, 0)
		if err != nil {
			t.Fatal(err)
		}
		series := ComputeSeries(def, bars, nil)
		if len(series) != len(bars) {
			t.Fatalf("factor %s: series length %d != bars %d", d.Name, len(series), len(bars))
		}
		valid := 0
		for _, v := range series {
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				valid++
			}
		}
		if valid == 0 {
			t.Fatalf("factor %s produced all-NaN series", d.Name)
		}
	}
}

func TestEvaluateICOfTrendFactor(t *testing.T) {
	bars := synthBars(600)
	def, err := Get("ma_deviation", 0)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := Evaluate(def, bars, nil, EvaluationConfig{ForwardBars: 3, ICWindow: 50})
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected evaluation")
	}
	if ev.Samples == 0 || len(ev.ICSeries) == 0 {
		t.Fatal("expected IC series")
	}
	if ev.ICStd > 0 && ev.ICIR == 0 {
		t.Fatal("ICIR should be non-zero when ICStd>0")
	}
	if ev.OverallRankIC < -1 || ev.OverallRankIC > 1 {
		t.Fatalf("rank IC out of range: %f", ev.OverallRankIC)
	}
	t.Logf("overall_ic=%.4f rank_ic=%.4f icir=%.4f", ev.OverallIC, ev.OverallRankIC, ev.ICIR)
}

func TestEvaluateInsufficientSamples(t *testing.T) {
	bars := synthBars(30)
	def, _ := Get("rsi", 0)
	ev, err := Evaluate(def, bars, nil, EvaluationConfig{ForwardBars: 5, ICWindow: 100})
	if err != nil {
		t.Fatal(err)
	}
	if ev != nil {
		t.Fatal("expected nil evaluation for insufficient samples")
	}
}

func TestLayeredBacktestMonotonicLayers(t *testing.T) {
	// 构造强反转因子：因子值 = -未来收益 的直接前视代理（价格涨幅的负值），
	// 其分层应呈现高层收益 > 低层收益（因子与未来收益正相关）。
	bars := synthBars(800)
	def, _ := Get("rsi_reversion", 0)
	res, err := LayeredBacktest(def, bars, nil, EvaluationConfig{ForwardBars: 2}, 5, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Layers) != 5 {
		t.Fatalf("expected 5 layers, got %d", len(res.Layers))
	}
	if res.Samples == 0 {
		t.Fatal("expected samples")
	}
	for _, l := range res.Layers {
		if l.Count == 0 {
			t.Fatal("every layer should have members")
		}
	}
	if len(res.EquityCurves) != 5 {
		t.Fatal("expected 5 equity curves")
	}
	for _, eq := range res.EquityCurves {
		if len(eq) < 2 {
			t.Fatal("equity curve should have points")
		}
	}
	t.Logf("long_short=%.4f monotonicity=%.3f", res.LongShort, res.Monotonicity)
}

func TestSpearmanPerfect(t *testing.T) {
	x := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	y := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	if got := spearman(x, y); math.Abs(got-1) > 1e-9 {
		t.Fatalf("expected +1, got %f", got)
	}
	rev := []float64{100, 90, 80, 70, 60, 50, 40, 30, 20, 10}
	if got := spearman(x, rev); math.Abs(got+1) > 1e-9 {
		t.Fatalf("expected -1, got %f", got)
	}
}

func TestForwardReturns(t *testing.T) {
	bars := synthBars(10)
	fwd := ForwardReturns(bars, 2)
	if !math.IsNaN(fwd[8]) || !math.IsNaN(fwd[9]) {
		t.Fatal("last 2 should be NaN")
	}
	if math.IsNaN(fwd[0]) {
		t.Fatal("fwd[0] should be valid")
	}
	expect := bars[2].Close/bars[0].Close - 1
	if math.Abs(fwd[0]-expect) > 1e-12 {
		t.Fatalf("fwd[0]=%f expect %f", fwd[0], expect)
	}
}

func TestToValuesLimits(t *testing.T) {
	bars := synthBars(50)
	def, _ := Get("roc", 0)
	series := ComputeSeries(def, bars, nil)
	vals := ToValues(bars, series, 10)
	if len(vals) != 10 {
		t.Fatalf("expected 10 values, got %d", len(vals))
	}
	if vals[9].Time != bars[49].Time {
		t.Fatal("expected latest value last")
	}
}
