package strategies

import "testing"

// TestClassicStrategiesApplySymbol：非 BTC 交易对实例必须把 params 里的 symbol
// 应用进策略——此前 14 个经典策略的 ApplyParams 只读注册表参数，symbol 永远
// 停在硬编码默认 BTCUSDT，导致 SOLUSDT 实例实际跑的是 BTC 数据、下单也下错
// 交易对（2026-10-04 MACD SOLUSDT 实证，暖机日志打出 BTCUSDT）。
func TestClassicStrategiesApplySymbol(t *testing.T) {
	cases := map[string]func() interface {
		ApplyParams(map[string]any) error
		Symbol() string
		Start(map[string]any) error
	}{
		"macd": func() interface {
			ApplyParams(map[string]any) error
			Symbol() string
			Start(map[string]any) error
		} {
			return NewMACDStrategy()
		},
		"ema_cross": func() interface {
			ApplyParams(map[string]any) error
			Symbol() string
			Start(map[string]any) error
		} {
			return NewEMACrossStrategy()
		},
		"rsi": func() interface {
			ApplyParams(map[string]any) error
			Symbol() string
			Start(map[string]any) error
		} {
			return NewRSIStrategy()
		},
		"bollinger_bands": func() interface {
			ApplyParams(map[string]any) error
			Symbol() string
			Start(map[string]any) error
		} {
			return NewBollingerBandsStrategy()
		},
		"atr_trailing": func() interface {
			ApplyParams(map[string]any) error
			Symbol() string
			Start(map[string]any) error
		} {
			return NewATRTrailingStopStrategy()
		},
		"dual_thrust": func() interface {
			ApplyParams(map[string]any) error
			Symbol() string
			Start(map[string]any) error
		} {
			return NewDualThrustStrategy()
		},
		"breakout": func() interface {
			ApplyParams(map[string]any) error
			Symbol() string
			Start(map[string]any) error
		} {
			return NewBreakoutStrategy()
		},
	}
	for name, mk := range cases {
		s := mk()
		if err := s.Start(map[string]any{"symbol": "solusdt"}); err != nil {
			t.Fatalf("%s start: %v", name, err)
		}
		if got := s.Symbol(); got != "SOLUSDT" {
			t.Fatalf("%s Symbol() = %q, want SOLUSDT（symbol 未从 params 应用）", name, got)
		}
	}
}
