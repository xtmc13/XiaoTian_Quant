package indicator

import (
	"testing"
)

func TestParseSource(t *testing.T) {
	code := `
my_indicator_name = "Super Trend"
my_indicator_description = "A super trend indicator"

# @param fast_period int 12 Fast MA period range=5:50:1
# @param slow_period int 26 Slow MA period range=10:100:1
# @param mode str "ema" MA type values=ema,sma,wma
# @strategy stopLossPct 2.5
# @strategy takeProfitPct 5.0
# @strategy trailingEnabled true
# @strategy tradeDirection long
`
	result := ParseSource(code)

	if result.Name != "Super Trend" {
		t.Errorf("Name = %q, want %q", result.Name, "Super Trend")
	}
	if result.Description != "A super trend indicator" {
		t.Errorf("Description = %q, want %q", result.Description, "A super trend indicator")
	}
	if len(result.Params) != 3 {
		t.Fatalf("len(Params) = %d, want 3", len(result.Params))
	}

	// Param 1
	p0 := result.Params[0]
	if p0.Name != "fast_period" || p0.Type != "int" {
		t.Errorf("param[0] = %+v", p0)
	}
	if p0.Default != 12 {
		t.Errorf("param[0].Default = %v, want 12", p0.Default)
	}
	if p0.Range == nil || p0.Range.Min != 5 || p0.Range.Max != 50 || p0.Range.Step != 1 {
		t.Errorf("param[0].Range = %+v", p0.Range)
	}

	// Param 2
	p1 := result.Params[1]
	if p1.Name != "slow_period" {
		t.Errorf("param[1].Name = %q", p1.Name)
	}

	// Param 3 (str with values)
	p2 := result.Params[2]
	if p2.Name != "mode" || p2.Type != "str" {
		t.Errorf("param[2] = %+v", p2)
	}
	if len(p2.Values) != 3 {
		t.Errorf("param[2].Values len = %d, want 3", len(p2.Values))
	}

	// Strategy config
	cfg := result.StrategyConfig
	if cfg.StopLossPct != 2.5 {
		t.Errorf("StopLossPct = %v, want 2.5", cfg.StopLossPct)
	}
	if cfg.TakeProfitPct != 5.0 {
		t.Errorf("TakeProfitPct = %v, want 5.0", cfg.TakeProfitPct)
	}
	if !cfg.TrailingEnabled {
		t.Error("TrailingEnabled should be true")
	}
	if cfg.TradeDirection != "long" {
		t.Errorf("TradeDirection = %q, want long", cfg.TradeDirection)
	}
}

func TestParseSource_NoMeta(t *testing.T) {
	code := `
# @param length int 14 RSI length
`
	result := ParseSource(code)
	if result.Name != "" {
		t.Error("expected empty name")
	}
	if len(result.Params) != 1 {
		t.Fatalf("len(Params) = %d, want 1", len(result.Params))
	}
}

func TestNormalizeParamType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"string", "str"},
		{"integer", "int"},
		{"boolean", "bool"},
		{"double", "float"},
		{"number", "float"},
		{"INT", "int"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeParamType(tt.input); got != tt.want {
				t.Errorf("normalizeParamType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDefaultValue(t *testing.T) {
	tests := []struct {
		typ  string
		val  string
		want any
	}{
		{"int", "42", 42},
		{"float", "3.14", 3.14},
		{"bool", "true", true},
		{"bool", "1", true},
		{"bool", "no", false},
		{"str", "ema", "ema"},
		{"str", `"quoted"`, "quoted"},
		{"str", `'quoted'`, "quoted"},
	}
	for _, tt := range tests {
		t.Run(tt.typ+"_"+tt.val, func(t *testing.T) {
			got := parseDefaultValue(tt.typ, tt.val)
			switch want := tt.want.(type) {
			case int:
				if g, ok := got.(int); !ok || g != want {
					t.Errorf("got %v, want %v", got, want)
				}
			case float64:
				if g, ok := got.(float64); !ok || g != want {
					t.Errorf("got %v, want %v", got, want)
				}
			case bool:
				if g, ok := got.(bool); !ok || g != want {
					t.Errorf("got %v, want %v", got, want)
				}
			case string:
				if g, ok := got.(string); !ok || g != want {
					t.Errorf("got %v, want %v", got, want)
				}
			}
		})
	}
}

func TestFindDeclaredParamNames(t *testing.T) {
	code := `
# @param a int 1
# @param b float 2.0
x = 1
# @param c str "hello"
`
	names := FindDeclaredParamNames(code)
	if len(names) != 3 {
		t.Fatalf("len(names) = %d, want 3", len(names))
	}
	if names[0] != "a" || names[1] != "b" || names[2] != "c" {
		t.Errorf("names = %v", names)
	}
}

func TestFindUnreadParams(t *testing.T) {
	code := `
# @param fast int 12
# @param slow int 26
fast = params.get('fast')
`
	unread := FindUnreadParams(code)
	if len(unread) != 1 || unread[0] != "slow" {
		t.Errorf("unread = %v, want [slow]", unread)
	}
}

func TestFindUnreadParams_NoneDeclared(t *testing.T) {
	code := `x = 1`
	unread := FindUnreadParams(code)
	if unread != nil {
		t.Errorf("expected nil, got %v", unread)
	}
}

func TestStrategyConfigToMap(t *testing.T) {
	cfg := StrategyConfig{
		StopLossPct:   2.5,
		TakeProfitPct: 5.0,
		TradeDirection: "long",
	}
	m := StrategyConfigToMap(cfg)
	if m["stopLossPct"] != 2.5 {
		t.Error("stopLossPct mismatch")
	}
	if m["takeProfitPct"] != 5.0 {
		t.Error("takeProfitPct mismatch")
	}
	if m["tradeDirection"] != "long" {
		t.Error("tradeDirection mismatch")
	}
	if _, ok := m["trailingEnabled"]; ok {
		t.Error("trailingEnabled should be omitted when false")
	}
}

func TestExtractIndicatorMeta(t *testing.T) {
	name, desc := ExtractIndicatorMeta(`
my_indicator_name = 'Test Name'
my_indicator_description = "Test Desc"
`)
	if name != "Test Name" || desc != "Test Desc" {
		t.Errorf("name=%q desc=%q", name, desc)
	}
}
