package handler

import (
	"encoding/json"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

// ── M7：CRA 壳策略（support_rebound/liquidity_heat）的参数过滤放行完整 CRA
// 键集——修复前只放行 7 个特征键 + order_count，take_profit_ratio/stop_loss_ratio
// 等在 Start 前被静默丢弃（引擎拿到默认值，止损/止盈比形同虚设）。──

func TestFilterParamsForStrategyCRAPackPassthrough(t *testing.T) {
	s := strategies.NewSupportReboundStrategy()
	params := map[string]any{
		// 引擎级键
		"symbol": "BTCUSDT", "timeframe": "4h",
		// 策略 registry 键
		"lookback_bars": 120,
		// CRA 特征键 + 此前被丢弃的 CRA 壳参数
		"first_order_amount":   100,
		"add_positions":        []any{map[string]any{"order": 1, "multiplier": 1, "spread": 0.03, "callback": 0.003}},
		"take_profit_method":   "full",
		"tp_mode":              "static",
		"take_profit_ratio":    0.03,
		"profit_callback":      0.005,
		"stop_loss_ratio":      0.05,
		"stop_loss_type":       "ratio",
		"trade_count_mode":     "single",
		"loop_count":           5,
		"first_order_price":    42000,
		"waterfall_enabled":    true,
		"waterfall_protection": 0.03,
		"order_count":          3,
		// 非 CRA/非 registry 杂物：仍应被过滤。
		"some_junk_key": "x",
	}
	filtered := filterParamsForStrategy(s, params)

	for _, k := range []string{
		"symbol", "timeframe", "lookback_bars",
		"first_order_amount", "add_positions", "take_profit_method", "tp_mode",
		"take_profit_ratio", "profit_callback",
		"stop_loss_ratio", "stop_loss_type",
		"trade_count_mode", "loop_count", "first_order_price",
		"waterfall_enabled", "waterfall_protection", "order_count",
	} {
		if _, ok := filtered[k]; !ok {
			t.Errorf("filtered params missing key %q", k)
		}
	}
	if _, ok := filtered["some_junk_key"]; ok {
		t.Error("junk key must stay filtered out")
	}
	if v, _ := filtered["stop_loss_ratio"].(float64); v != 0.05 {
		t.Errorf("stop_loss_ratio = %v, want 0.05 (M7: 修复前被过滤丢弃)", v)
	}

	// 链路末端：filtered 参数经 support_rebound.Start 的同一路径
	//（json.Marshal → cra.ParseCRAParams）解析，止损比必须是用户值而非默认 0。
	data, err := json.Marshal(filtered)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cp, err := cra.ParseCRAParams(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cp.StopLossRatio != 0.05 {
		t.Errorf("engine stop_loss_ratio = %v, want 0.05", cp.StopLossRatio)
	}
	if cp.TakeProfitRatio != 0.03 {
		t.Errorf("engine take_profit_ratio = %v, want 0.03", cp.TakeProfitRatio)
	}
	if cp.TradeCountMode != "single" || cp.LoopCount != 5 {
		t.Errorf("engine trade_count_mode/loop_count = %v/%v, want single/5", cp.TradeCountMode, cp.LoopCount)
	}
	if cp.FirstOrderPrice != 42000 {
		t.Errorf("engine first_order_price = %v, want 42000", cp.FirstOrderPrice)
	}
	if cp.WaterfallProtection != 0.03 {
		t.Errorf("engine waterfall_protection = %v, want 0.03", cp.WaterfallProtection)
	}
}

func TestFilterParamsForStrategyNonCRAPayloadStillFiltered(t *testing.T) {
	// 无 CRA 特征键的参数包：保持修复前的严格过滤（stop_loss_ratio 等 CRA 键
	// 不放行）——防止非 CRA 配置的残留键悄悄漏进引擎。
	s := strategies.NewSupportReboundStrategy()
	params := map[string]any{
		"symbol":            "BTCUSDT",
		"lookback_bars":     120,
		"stop_loss_ratio":   0.05,
		"take_profit_ratio": 0.03,
		"order_count":       3, // order_count 是既有无条件放行键，不受本闸门影响
	}
	filtered := filterParamsForStrategy(s, params)
	if _, ok := filtered["stop_loss_ratio"]; ok {
		t.Error("stop_loss_ratio without CRA markers must stay filtered out")
	}
	if _, ok := filtered["take_profit_ratio"]; ok {
		t.Error("take_profit_ratio without CRA markers must stay filtered out")
	}
	if _, ok := filtered["lookback_bars"]; !ok {
		t.Error("registry key lookback_bars must pass")
	}
	if _, ok := filtered["order_count"]; !ok {
		t.Error("order_count unconditional passthrough must be preserved")
	}
}

func TestFilterParamsForStrategyNilRegistryPassthrough(t *testing.T) {
	// CRA 引擎（cra_spot/cra_contract）无参数注册表：不过滤，原样透传。
	s := cra.NewCRASpotStrategy("martin_trend", "BTCUSDT")
	params := map[string]any{"symbol": "BTCUSDT", "stop_loss_ratio": 0.05, "anything": 1}
	filtered := filterParamsForStrategy(s, params)
	if len(filtered) != len(params) {
		t.Errorf("nil-registry strategy must pass params through unfiltered, got %v", filtered)
	}
}
