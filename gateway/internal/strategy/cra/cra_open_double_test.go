package cra

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── D1：开仓加倍（open_double，币富名词解释 #15）──
//
// 币富语义：首单金额×2 倍，但补仓倍数仍按首单原始金额进行倍投/等比——加倍
// 只放大首单成交，不改变 add_positions 阶梯的基数。例：首单 10U 开 5 倍合约
// 买 50U，开仓加倍后首单 100U，补仓第 N 档仍按 10U×multiplier 算。

// firstOrderQty 以给定配置启动合约策略并返回首根 K 线发出的首单数量。
func firstOrderQty(t *testing.T, params map[string]any) float64 {
	t.Helper()
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	base := map[string]any{
		"symbol":                 "BTCUSDT",
		"market_type":            "swap",
		"leverage":               5,
		"direction":              "long",
		"first_order_amount":     10,
		"first_order_multiplier": 1,
		"tp_mode":                "static",
		"take_profit_ratio":      0.5, // 远高于测试价格区间，防止盈干扰
		"enable_add_position":    true,
		"order_count":            3,
		"add_positions": []any{
			map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003},
		},
	}
	for k, v := range params {
		base[k] = v
	}
	if err := s.Start(base); err != nil {
		t.Fatalf("start: %v", err)
	}
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 100, High: 100, Low: 100}, nil)
	if err != nil {
		t.Fatalf("onbar: %v", err)
	}
	if sig == nil {
		t.Fatal("expected first order signal")
	}
	return sig.Qty
}

func TestCRAOpenDoubleFirstOrderDoubled(t *testing.T) {
	plain := firstOrderQty(t, nil)
	doubled := firstOrderQty(t, map[string]any{"open_double": true})
	// 10U×1×5 倍杠杆 @100 → 0.5；加倍后 20U×1×5 @100 → 1.0。
	if plain != 0.5 {
		t.Fatalf("baseline first order qty = %v, want 0.5", plain)
	}
	if doubled != 1.0 {
		t.Fatalf("open_double first order qty = %v, want 1.0 (doubled)", doubled)
	}
	if doubled != plain*2 {
		t.Fatalf("open_double qty %v != 2 × baseline %v", doubled, plain)
	}
}

func TestCRAOpenDoubleDisabledByDefault(t *testing.T) {
	// 默认关闭：不传 open_double 与显式 false 同量。
	plain := firstOrderQty(t, nil)
	explicit := firstOrderQty(t, map[string]any{"open_double": false})
	if explicit != plain {
		t.Fatalf("explicit false qty %v != default %v (default must be zero-impact)", explicit, plain)
	}
}

// addPositionQty 开仓成交后驱动价格下跌+反弹触发第 1 档补仓，返回补仓信号数量。
func addPositionQty(t *testing.T, openDouble bool) float64 {
	t.Helper()
	s := NewCRAContractStrategy("cra_contract", "BTCUSDT")
	if err := s.Start(map[string]any{
		"symbol":                 "BTCUSDT",
		"market_type":            "swap",
		"leverage":               5,
		"direction":              "long",
		"first_order_amount":     10,
		"first_order_multiplier": 1,
		"open_double":            openDouble,
		"tp_mode":                "static",
		"take_profit_ratio":      0.5,
		"enable_add_position":    true,
		"order_count":            3,
		// 引擎首档补仓消费 order=2 的配置（nextOrder=PositionCount+1，首单成交
		// 占 order 1）：显式给出 order 2，其 spread/callback 与测试价格路径对齐。
		"add_positions": []any{
			map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003},
			map[string]any{"order": 2, "multiplier": 1, "spread": 0.03, "callback": 0.003},
		},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	// 首单信号（100 进场）→ 成交确认。
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 100, High: 100, Low: 100}, nil)
	if err != nil || sig == nil {
		t.Fatalf("first order: sig=%v err=%v", sig, err)
	}
	if _, err := s.OnOrderUpdate(model.OrderData{
		Symbol: "BTCUSDT", Side: model.SideBuy,
		Filled: sig.Qty, AvgFillPrice: 100, Status: model.StatusFilled,
	}, nil); err != nil {
		t.Fatalf("order update: %v", err)
	}
	// 跌 4%（> spread 3%）→ 挂起补仓；自低点反弹 0.5%（> callback 0.3%）→ 触发。
	if sig, _ := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 96, High: 100, Low: 96}, nil); sig != nil {
		t.Fatalf("spread reached but callback not yet: expected nil signal, got %+v", sig)
	}
	sig, err = s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 96.5, High: 96.5, Low: 96}, nil)
	if err != nil {
		t.Fatalf("onbar add: %v", err)
	}
	if sig == nil {
		t.Fatal("expected add position signal")
	}
	return sig.Qty
}

func TestCRAOpenDoubleAddLadderBaseUnchanged(t *testing.T) {
	plain := addPositionQty(t, false)
	doubled := addPositionQty(t, true)
	// 补仓第 1 档基数 = 首单原始金额 10U ×multiplier 1 ×杠杆 5 @96.5，
	// 与是否开仓加倍无关。
	want := RoundQty(10 * 1 * 5 / 96.5)
	if plain != want {
		t.Fatalf("baseline add qty = %v, want %v", plain, want)
	}
	if doubled != plain {
		t.Fatalf("open_double add qty %v != baseline %v (ladder base must not scale with doubling)", doubled, plain)
	}
}

// 现货开仓加倍不生效（G2-4）：D 片表单已把 open_double 改为仅合约显示，引擎层
// 同步收紧——现货存量配置即使残留 open_double=true，首单也不×2（与 follow_trend
// 门控同款 isContract 门）。
func TestCRAOpenDoubleSpotIgnored(t *testing.T) {
	spotQty := func(openDouble bool) float64 {
		t.Helper()
		s := NewCRASpotStrategy("martin_trend", "BTCUSDT")
		if err := s.Start(map[string]any{
			"symbol":              "BTCUSDT",
			"first_order_amount":  10,
			"tp_mode":             "static",
			"take_profit_ratio":   0.5,
			"enable_add_position": false,
			"open_double":         openDouble,
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Close: 100, High: 100, Low: 100}, nil)
		if err != nil || sig == nil {
			t.Fatalf("first order: sig=%v err=%v", sig, err)
		}
		return sig.Qty
	}
	plain := spotQty(false)
	doubled := spotQty(true)
	// 现货首单：10U×1 @100 → 0.1；open_double 残留 true 不得放大。
	if plain != 0.1 {
		t.Fatalf("spot baseline qty = %v, want 0.1", plain)
	}
	if doubled != plain {
		t.Fatalf("spot open_double must be ignored: qty %v != baseline %v", doubled, plain)
	}
}
