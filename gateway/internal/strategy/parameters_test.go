package strategy

import (
	"encoding/json"
	"testing"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func ptAssert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func ptAssertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func ptAssertFloat(t *testing.T, got, want float64, msg string) {
	t.Helper()
	const eps = 0.001
	if got < want-eps || got > want+eps {
		t.Fatalf("%s: got %.6f, want %.6f", msg, got, want)
	}
}

/* ── IntParameter Tests ──────────────────────────────────────── */

func TestIntParameter(t *testing.T) {
	p := IntParameter("fast_period", 12, 5, 50, "buy")
	ptAssertEq(t, p.Name, "fast_period", "name")
	ptAssertEq(t, p.Type, ParamInt, "type")
	ptAssertEq(t, p.GetInt(), 12, "default value")
	ptAssertEq(t, p.Space, "buy", "space")
	ptAssert(t, p.Optimize, "should be optimizable")

	err := p.SetValue(20)
	ptAssert(t, err == nil, "set valid value")
	ptAssertEq(t, p.GetInt(), 20, "updated value")
}

func TestIntParameterOutOfRange(t *testing.T) {
	p := IntParameter("period", 10, 5, 50, "buy")

	err := p.SetValue(3)
	ptAssert(t, err != nil, "below min should error")

	err = p.SetValue(100)
	ptAssert(t, err != nil, "above max should error")
}

func TestIntParameterInvalidType(t *testing.T) {
	p := IntParameter("period", 10, 5, 50, "buy")

	err := p.SetValue("hello")
	ptAssert(t, err != nil, "string should error for int param")
}

func TestIntParameterDefaultValue(t *testing.T) {
	p := IntParameter("period", 10, 1, 20, "")
	ptAssertEq(t, p.CurrentValue().(int), 10, "current = default when not set")
}

/* ── FloatParameter Tests ────────────────────────────────────── */

func TestFloatParameter(t *testing.T) {
	p := FloatParameter("stoploss", 0.02, 0.01, 0.10, 0.01, "stoploss")
	ptAssertEq(t, p.Type, ParamFloat, "type")
	ptAssertFloat(t, p.GetFloat(), 0.02, "default")

	err := p.SetValue(0.05)
	ptAssert(t, err == nil, "set valid float")
	ptAssertFloat(t, p.GetFloat(), 0.05, "updated")
}

func TestFloatParameterOutOfRange(t *testing.T) {
	p := FloatParameter("stoploss", 0.02, 0.01, 0.10, 0.01, "stoploss")

	err := p.SetValue(0.001)
	ptAssert(t, err != nil, "below min")

	err = p.SetValue(0.50)
	ptAssert(t, err != nil, "above max")
}

func TestFloatParameterDefaultStep(t *testing.T) {
	p := FloatParameter("stoploss", 0.02, 0.01, 0.05, 0, "stoploss")
	ptAssert(t, p.Step > 0, "auto step when zero")
}

/* ── BoolParameter Tests ─────────────────────────────────────── */

func TestBoolParameter(t *testing.T) {
	p := BoolParameter("use_trailing", true, "trailing")
	ptAssertEq(t, p.Type, ParamBool, "type")
	ptAssertEq(t, p.GetBool(), true, "default true")

	err := p.SetValue(false)
	ptAssert(t, err == nil, "set valid bool")
	ptAssertEq(t, p.GetBool(), false, "updated")
}

func TestBoolParameterInvalid(t *testing.T) {
	p := BoolParameter("flag", false, "")
	err := p.SetValue("true")
	ptAssert(t, err != nil, "string should error for bool")
}

/* ── CategoricalParameter Tests ──────────────────────────────── */

func TestCategoricalParameter(t *testing.T) {
	p := CategoricalParameter("mode", "normal", []string{"aggressive", "normal", "conservative"}, "buy")
	ptAssertEq(t, p.Type, ParamCategorical, "type")
	ptAssertEq(t, p.GetString(), "normal", "default")

	err := p.SetValue("aggressive")
	ptAssert(t, err == nil, "set valid option")
	ptAssertEq(t, p.GetString(), "aggressive", "updated")
}

func TestCategoricalInvalidOption(t *testing.T) {
	p := CategoricalParameter("mode", "normal", []string{"aggressive", "conservative"}, "buy")
	err := p.SetValue("normal")
	ptAssert(t, err != nil, "normal not in options")
}

/* ── ParamRegistry Tests ─────────────────────────────────────── */

func TestParamRegistryRegister(t *testing.T) {
	r := NewParamRegistry()
	r.Register(IntParameter("fast", 12, 5, 50, "buy"))
	r.Register(FloatParameter("stoploss", 0.02, 0.01, 0.10, 0.005, "stoploss"))
	r.Register(BoolParameter("trailing", false, "trailing"))

	ptAssertEq(t, r.Count(), 3, "3 params registered")
}

func TestParamRegistrySetAll(t *testing.T) {
	r := NewParamRegistry()
	r.Register(IntParameter("fast", 12, 5, 50, "buy"))
	r.Register(FloatParameter("stoploss", 0.02, 0.01, 0.10, 0.005, "stoploss"))

	err := r.SetAll(map[string]any{
		"fast":     20,
		"stoploss": 0.05,
	})
	ptAssert(t, err == nil, "set all should succeed")

	ptAssertEq(t, r.Get("fast").GetInt(), 20, "fast updated")
	ptAssertFloat(t, r.Get("stoploss").GetFloat(), 0.05, "stoploss updated")
}

func TestParamRegistrySetAllInvalid(t *testing.T) {
	r := NewParamRegistry()
	r.Register(IntParameter("fast", 12, 5, 50, "buy"))

	err := r.SetAll(map[string]any{
		"fast": 100, // out of range
	})
	ptAssert(t, err != nil, "invalid value should error")
}

func TestParamRegistryAll(t *testing.T) {
	r := NewParamRegistry()
	r.Register(IntParameter("a", 1, 0, 10, ""))
	r.Register(FloatParameter("b", 0.5, 0, 1, 0.1, ""))

	all := r.All()
	ptAssertEq(t, len(all), 2, "2 params")
	// Verify order
	ptAssertEq(t, all[0].Name, "a", "first registered first")
	ptAssertEq(t, all[1].Name, "b", "second registered second")
}

func TestParamRegistryOptimizable(t *testing.T) {
	r := NewParamRegistry()
	p1 := IntParameter("fast", 12, 5, 50, "buy")
	p1.Optimize = true
	r.Register(p1)

	p2 := IntParameter("slow", 26, 10, 100, "buy")
	p2.Optimize = false // not for hyperopt
	r.Register(p2)

	opts := r.Optimizable()
	ptAssertEq(t, len(opts), 1, "only 1 optimizable")
	ptAssertEq(t, opts[0].Name, "fast", "fast is optimizable")
}

func TestParamRegistryGetMissing(t *testing.T) {
	r := NewParamRegistry()
	p := r.Get("nonexistent")
	ptAssert(t, p == nil, "nil for missing param")
}

func TestParamRegistryToHyperoptSpaces(t *testing.T) {
	r := NewParamRegistry()
	r.Register(IntParameter("fast", 12, 5, 50, "buy"))
	r.Register(FloatParameter("stoploss", 0.02, 0.01, 0.10, 0.01, "stoploss"))

	spaces := r.ToHyperoptSpaces()
	ptAssertEq(t, len(spaces), 2, "2 hyperopt spaces")

	ptAssertEq(t, spaces[0].Name, "fast", "first space name")
	ptAssertEq(t, spaces[1].Name, "stoploss", "second space name")
}

/* ── ParamType String Tests ──────────────────────────────────── */

func TestParamTypeString(t *testing.T) {
	ptAssertEq(t, ParamInt.String(), "INT", "int")
	ptAssertEq(t, ParamFloat.String(), "FLOAT", "float")
	ptAssertEq(t, ParamBool.String(), "BOOL", "bool")
	ptAssertEq(t, ParamCategorical.String(), "CATEGORICAL", "categorical")
}

/* ── convert helpers ─────────────────────────────────────────── */

func TestToInt(t *testing.T) {
	tests := []struct {
		input  any
		expect int
		ok     bool
	}{
		{int(5), 5, true},
		{int64(10), 10, true},
		{float64(3.7), 4, true}, // round
		{json.Number("42"), 42, true},
		{"hello", 0, false},
	}

	for _, tt := range tests {
		got, ok := toInt(tt.input)
		if ok != tt.ok {
			t.Fatalf("toInt(%v): expected ok=%v, got ok=%v", tt.input, tt.ok, ok)
		}
		if ok {
			ptAssertEq(t, got, tt.expect, "value")
		}
	}
}

func TestToFloat(t *testing.T) {
	tests := []struct {
		input  any
		expect float64
		ok     bool
	}{
		{float64(3.14), 3.14, true},
		{int(5), 5.0, true},
		{int64(10), 10.0, true},
		{"hello", 0, false},
	}

	for _, tt := range tests {
		got, ok := toFloat(tt.input)
		ptAssertEq(t, ok, tt.ok, "ok")
		if ok {
			ptAssertFloat(t, got, tt.expect, "value")
		}
	}
}

/* ── BaseStrategy Tests ──────────────────────────────────────── */

func TestBaseStrategyDefaults(t *testing.T) {
	b := &BaseStrategy{}

	ptAssert(t, b.ConfirmTradeEntry(nil), "default confirm entry = true")
	ptAssert(t, b.ConfirmTradeExit(nil), "default confirm exit = true")
	ptAssertFloat(t, b.CustomStoploss(nil, 0), 0, "default custom stoploss = 0")
	ptAssertFloat(t, b.CustomStakeAmount(0, nil), 0, "default custom stake = 0")
	ptAssertFloat(t, b.AdjustEntryPrice(nil, nil), 0, "default adjust entry = 0")
	ptAssert(t, b.GetParameters() == nil, "default params = nil")
	ptAssert(t, b.InformativePairs() == nil, "default informative = nil")
}

/* ── InformativePair Tests ───────────────────────────────────── */

func TestInformativePair(t *testing.T) {
	ip := InformativePair{Symbol: "ETHUSDT", Timeframe: "1d", Asset: "ETH"}
	ptAssertEq(t, ip.Symbol, "ETHUSDT", "symbol")
	ptAssertEq(t, ip.Timeframe, "1d", "timeframe")
	ptAssertEq(t, ip.Asset, "ETH", "asset")
}

/* ── Position Tests ──────────────────────────────────────────── */

func TestPosition(t *testing.T) {
	pos := Position{
		Symbol:        "BTCUSDT",
		Side:          "LONG",
		EntryPrice:    50000,
		Quantity:      0.1,
		UnrealizedPnL: 500,
		StoplossPrice: 48000,
		OpenTime:      1234567890,
	}
	ptAssertEq(t, pos.Symbol, "BTCUSDT", "symbol")
	ptAssertFloat(t, pos.EntryPrice, 50000, "entry price")
	ptAssertFloat(t, pos.UnrealizedPnL, 500, "pnl")
}
