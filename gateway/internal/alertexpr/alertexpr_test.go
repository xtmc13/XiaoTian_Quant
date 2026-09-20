package alertexpr

import (
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// makeBars 构造 n 根合成 K 线：close 从 start 开始每根步进 step。
func makeBars(n int, start, step float64) []model.Bar {
	bars := make([]model.Bar, n)
	for i := 0; i < n; i++ {
		c := start + float64(i)*step
		bars[i] = model.Bar{
			Open:   c - step/2,
			High:   c + 1,
			Low:    c - 1,
			Close:  c,
			Volume: 100 + float64(i),
			Time:   int64(1700000000+i) * 1000,
		}
	}
	return bars
}

func evalBool(t *testing.T, expr string, bars []model.Bar) Result {
	t.Helper()
	e, err := Parse(expr)
	if err != nil {
		t.Fatalf("Parse(%q): %v", expr, err)
	}
	res, err := e.Eval(bars)
	if err != nil {
		t.Fatalf("Eval(%q): %v", expr, err)
	}
	return res
}

// ── Tokenizer / 安全 ──

func TestTokenizeRejections(t *testing.T) {
	cases := map[string]string{
		"":                 "empty",
		"rsi14 < 30; drop": "semicolon",
		"close > 0 && 1=1": "single equals",
		"a & b":            "single ampersand",
		"a | b":            "single pipe",
		`close > "30"`:     "quotes",
		"close > $30":      "dollar",
		"close[0] > 30":    "brackets",
		"close > 30 # x":   "hash comment",
		" alert(1)":        "unknown identifier",
		"foo14 < 30":       "unknown identifier with digits",
		"close > (30":      "unbalanced paren",
		"close 30":         "missing operator",
		"1 < 2 < 3":        "chained comparison",
		"rsi(14 < 30":      "unbalanced call paren",
		"rsi(x) < 30":      "non-numeric arg",
		"rsi(0) < 30":      "period too small",
		"rsi(501) < 30":    "period too large",
		"rsi(14.5) < 30":   "non-integer period",
		"bb_upper(20,0)":   "mult zero",
		"bb_upper(20,11)":  "mult too large",
		"close / 0 > 1":    "division by zero is eval-time, not parse",
	}
	for expr, why := range cases {
		_, err := Parse(expr)
		if why == "division by zero is eval-time, not parse" {
			if err != nil {
				t.Errorf("Parse(%q) [%s]: expected parse success (eval-time error), got %v", expr, why, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("Parse(%q) [%s]: expected error, got nil", expr, why)
		}
	}
}

func TestMaxExprLen(t *testing.T) {
	long := "close > 0 " + strings.Repeat("&& close > 0 ", 60) // > 500 bytes
	if _, err := Parse(long); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected length error, got %v", err)
	}
}

func TestIllegalNonASCII(t *testing.T) {
	if _, err := Parse("收盘价 > 30"); err == nil {
		t.Error("expected rejection of non-ASCII input")
	}
}

// ── 解析优先级 ──

func TestArithmeticPrecedence(t *testing.T) {
	bars := makeBars(1, 100, 0)
	cases := []struct {
		expr string
		want bool
	}{
		{"1 + 2 * 3 == 7", true},
		{"(1 + 2) * 3 == 9", true},
		{"10 - 4 - 3 == 3", true}, // 左结合
		{"16 / 4 / 2 == 2", true}, // 左结合
		{"2 * 3 + 4 * 5 == 26", true},
		{"-1 < 0", true},
		{"-(1 + 2) == -3", true},
		{"1 + 2 < 3 + 4", true},
	}
	for _, tc := range cases {
		if got := evalBool(t, tc.expr, bars).Matched; got != tc.want {
			t.Errorf("%s = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestLogicalPrecedence(t *testing.T) {
	bars := makeBars(1, 100, 0)
	// && 优先级高于 ||：true || (false && false) = true；
	// 若 || 优先则 (true||false)&&false = false。
	if !evalBool(t, "1 == 1 || 1 == 2 && 2 == 3", bars).Matched {
		t.Error("&& should bind tighter than ||")
	}
	// 比较优先级高于 &&
	if !evalBool(t, "1 < 2 && 3 < 4", bars).Matched {
		t.Error("comparison should bind tighter than &&")
	}
}

// TestTruthTable 真值表：&& 与 || 的四种组合。
func TestTruthTable(t *testing.T) {
	bars := makeBars(1, 100, 0)
	type row struct {
		a, b string
		and  bool
		or   bool
	}
	rows := []row{
		{"1 == 1", "2 == 2", true, true},
		{"1 == 1", "2 == 3", false, true},
		{"1 == 2", "2 == 2", false, true},
		{"1 == 2", "2 == 3", false, false},
	}
	for _, r := range rows {
		if got := evalBool(t, r.a+" && "+r.b, bars).Matched; got != r.and {
			t.Errorf("%s && %s = %v, want %v", r.a, r.b, got, r.and)
		}
		if got := evalBool(t, r.a+" || "+r.b, bars).Matched; got != r.or {
			t.Errorf("%s || %s = %v, want %v", r.a, r.b, got, r.or)
		}
	}
}

// ── 比较语义 ──

func TestComparisons(t *testing.T) {
	bars := makeBars(1, 100, 1)
	for _, tc := range []struct {
		expr string
		want bool
	}{
		{"2 > 1", true}, {"2 >= 2", true}, {"1 > 2", false},
		{"1 < 2", true}, {"2 <= 2", true}, {"2 < 1", false},
		{"2 == 2", true}, {"2 == 3", false}, {"2 != 3", true}, {"2 != 2", false},
		{"close == 100", true},
		{"open < close", true}, // open = 99.5，close = 100
		{"high > low", true},
		{"volume >= 100", true},
	} {
		if got := evalBool(t, tc.expr, bars).Matched; got != tc.want {
			t.Errorf("%s = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

// ── 指标函数 ──

func TestOHLCVLastBar(t *testing.T) {
	bars := makeBars(5, 100, 1)
	res := evalBool(t, "close == 104", bars)
	if !res.Matched {
		t.Errorf("close should be last bar value 104, matched=%v", res.Matched)
	}
	if res.Value != 104 {
		t.Errorf("Value = %v, want 104 (left operand capture)", res.Value)
	}
}

func TestSMA(t *testing.T) {
	// closes: 100..119，sma20 = 109.5
	bars := makeBars(20, 100, 1)
	res := evalBool(t, "sma20 == 109.5", bars)
	if !res.Matched {
		t.Errorf("sma20 over 100..119 should be 109.5, got value=%v", res.Value)
	}
}

func TestEMAReusesCRA(t *testing.T) {
	bars := makeBars(60, 100, 1)
	// 与 cra.EMA 直算末值对齐（复用同一实现）。
	ema := craEMALast(t, bars, 20)
	res := evalBool(t, "ema20 > 0", bars)
	if res.Value != ema {
		t.Errorf("ema20 = %v, cra direct = %v", res.Value, ema)
	}
}

func TestRSI(t *testing.T) {
	// 单边上涨：所有 diff > 0 → RSI=100。
	up := makeBars(30, 100, 1)
	if res := evalBool(t, "rsi14 == 100", up); !res.Matched {
		t.Errorf("monotonic uptrend rsi should be 100, got %v", res.Value)
	}
	// 单边下跌：avgGain=0 → RSI=0。
	down := makeBars(30, 200, -1)
	if res := evalBool(t, "rsi14 < 5", down); !res.Matched {
		t.Errorf("monotonic downtrend rsi should be ~0, got %v", res.Value)
	}
	// 数据不足 → 求值错误。
	short := makeBars(10, 100, 1)
	e, _ := Parse("rsi14 < 30")
	if _, err := e.Eval(short); err == nil {
		t.Error("expected insufficient-bars error for rsi14 with 10 bars")
	}
}

func TestATR(t *testing.T) {
	// 手工构造：high=close+1, low=close-1，步进 1 → 每根 TR=2 → atr14=2。
	bars := makeBars(20, 100, 1)
	if res := evalBool(t, "atr14 == 2", bars); !res.Matched {
		t.Errorf("atr14 = %v, want 2", res.Value)
	}
}

func TestMACD(t *testing.T) {
	bars := makeBars(60, 100, 0.5)
	// 单调上涨 → MACD 线在信号线上方，hist > 0。
	if res := evalBool(t, "macd_hist > 0", bars); !res.Matched {
		t.Errorf("expected positive macd_hist in uptrend, got %v", res.Value)
	}
	if res := evalBool(t, "macd > macd_signal", bars); !res.Matched {
		t.Errorf("expected macd > macd_signal in uptrend, got macd=%v", res.Value)
	}
}

func TestBollinger(t *testing.T) {
	// 常数序列 → 带宽 0：bb_width == 0，bb_upper == bb_lower == bb_mid。
	flat := makeBars(25, 100, 0)
	if res := evalBool(t, "bb_width == 0", flat); !res.Matched {
		t.Errorf("flat series bb_width = %v, want 0", res.Value)
	}
	if res := evalBool(t, "bb_upper == bb_lower", flat); !res.Matched {
		t.Errorf("flat series bb_upper should equal bb_lower")
	}
	// 常数序列 pctb 分母为 0 → 求值错误。
	e, _ := Parse("bb_pctb > 0")
	if _, err := e.Eval(flat); err == nil {
		t.Error("expected zero-bandwidth error for bb_pctb on flat series")
	}
	// 末根向上尖刺：close 突破上轨 → pctb > 1。
	spikeUp := make([]model.Bar, 25)
	for i := range spikeUp {
		c := 100.0
		if i == 24 {
			c = 120
		}
		spikeUp[i] = model.Bar{Open: c, High: c + 1, Low: c - 1, Close: c, Volume: 1}
	}
	if res := evalBool(t, "bb_pctb > 1", spikeUp); !res.Matched {
		t.Errorf("spike-up bb_pctb = %v, want > 1", res.Value)
	}
	// 末根向下尖刺：跌破下轨 → pctb < 0。
	spikeDown := make([]model.Bar, 25)
	for i := range spikeDown {
		c := 100.0
		if i == 24 {
			c = 80
		}
		spikeDown[i] = model.Bar{Open: c, High: c + 1, Low: c - 1, Close: c, Volume: 1}
	}
	if res := evalBool(t, "bb_pctb < 0", spikeDown); !res.Matched {
		t.Errorf("spike-down bb_pctb = %v, want < 0", res.Value)
	}
}

// TestExampleExpressions 三个文档示例端到端可求值。
func TestExampleExpressions(t *testing.T) {
	bars := makeBars(200, 100, 0.3)
	for _, expr := range []string{
		"rsi14 < 30 && close > ema20",
		"ema12 > ema50 && close > ema50",
		"(bb_upper(20,2)-bb_lower(20,2))/bb_mid(20,2) < 0.05",
		"bb_width < 0.05",
	} {
		if _, err := Parse(expr); err != nil {
			t.Errorf("example %q failed to parse: %v", expr, err)
			continue
		}
		e, _ := Parse(expr)
		if _, err := e.Eval(bars); err != nil {
			t.Errorf("example %q failed to eval: %v", expr, err)
		}
	}
}

func TestValueCapture(t *testing.T) {
	bars := makeBars(20, 100, 1)
	// 首个比较式的左操作数被捕获（即使该比较式为 false，后续 && 短路也不影响）。
	res := evalBool(t, "rsi14 < 30 && close > 0", bars)
	if res.Value != 100 {
		t.Errorf("Value = %v, want 100 (rsi14 left operand)", res.Value)
	}
	// 无比较式：Value 为整体表达式数值。
	res2 := evalBool(t, "close * 2", bars)
	if res2.Value != 238 { // close=119
		t.Errorf("Value = %v, want 238", res2.Value)
	}
}

func TestEmptyBars(t *testing.T) {
	e, err := Parse("close > 0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Eval(nil); err != ErrNoBars {
		t.Errorf("expected ErrNoBars, got %v", err)
	}
}

func TestDepthLimit(t *testing.T) {
	expr := strings.Repeat("(", maxDepth+5) + "1" + strings.Repeat(")", maxDepth+5)
	if _, err := Parse(expr); err == nil || !strings.Contains(err.Error(), "nested too deep") {
		t.Errorf("expected depth error, got %v", err)
	}
}

func TestCaseInsensitive(t *testing.T) {
	bars := makeBars(20, 100, 1)
	if res := evalBool(t, "RSI14 < 200", bars); !res.Matched {
		t.Error("uppercase identifiers should work")
	}
}

func TestConcurrentEval(t *testing.T) {
	e, err := Parse("rsi14 < 30 && close > ema20")
	if err != nil {
		t.Fatal(err)
	}
	bars := makeBars(100, 100, 0.5)
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() {
			_, err := e.Eval(bars)
			done <- err
		}()
	}
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent eval: %v", err)
		}
	}
}

// craEMALast 测试辅助：直接调 cra.EMA 取末值（验证复用口径）。
func craEMALast(t *testing.T, bars []model.Bar, period int) float64 {
	t.Helper()
	series := cra.EMA(cra.BarsToCloses(bars), period)
	return series[len(series)-1]
}
