package strategy_test

import (
	"encoding/json"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

// 注册系统模板引用到的全部策略工厂（生产在 cmd/server/main.go 注册，
// 测试自注册；RegisterStrategyFactory 幂等覆盖）。
func registerTemplateFactories() {
	f := strategy.RegisterStrategyFactory
	f("ema_cross", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	f("macd", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	f("rsi", func() strategy.Strategy { return strategies.NewRSIStrategy() })
	f("bollinger_bands", func() strategy.Strategy { return strategies.NewBollingerBandsStrategy() })
	f("dual_thrust", func() strategy.Strategy { return strategies.NewDualThrustStrategy() })
	f("atr_trailing_stop", func() strategy.Strategy { return strategies.NewATRTrailingStopStrategy() })
	f("ema_follow_trend", func() strategy.Strategy { return strategies.NewEMACrossStrategy() })
	f("breakout", func() strategy.Strategy { return strategies.NewBreakoutStrategy() })
	f("grid_trading", func() strategy.Strategy { return strategies.NewGridTradingStrategy() })
}

// TestSystemCTATemplatesInstantiable: 8 个 CTA 模板的 strategy_type 都能
// 从工厂创建实例，且 default_config 可通过 ApplyParams + ValidateParams
// （即「能直接实例化运行」）。
func TestSystemCTATemplatesInstantiable(t *testing.T) {
	registerTemplateFactories()

	tpls := store.SystemTemplates()
	cta := 0
	for _, tpl := range tpls {
		if tpl.StrategyType == "combo" {
			continue
		}
		cta++
		s := strategy.StrategyFactory(tpl.StrategyType)
		if s == nil {
			t.Errorf("template %s: strategy_type %q not registered", tpl.ID, tpl.StrategyType)
			continue
		}
		var cfg map[string]any
		if err := json.Unmarshal([]byte(tpl.DefaultConfigJSON), &cfg); err != nil {
			t.Errorf("template %s: default_config invalid: %v", tpl.ID, err)
			continue
		}
		if err := s.ApplyParams(cfg); err != nil {
			t.Errorf("template %s: ApplyParams: %v", tpl.ID, err)
		}
		if err := s.ValidateParams(); err != nil {
			t.Errorf("template %s: ValidateParams: %v", tpl.ID, err)
		}
		if s.Symbol() == "" {
			t.Errorf("template %s: symbol empty after apply", tpl.ID)
		}
	}
	if cta != 8 {
		t.Errorf("cta templates = %d, want 8", cta)
	}
}

// TestSystemComboTemplatesInstantiable: 4 个组合模板的 default_config
// 可直接实例化为 StrategyCombo（成员工厂齐全、权重校验通过）。
func TestSystemComboTemplatesInstantiable(t *testing.T) {
	registerTemplateFactories()

	combo := 0
	for _, tpl := range store.SystemTemplates() {
		if tpl.StrategyType != "combo" {
			continue
		}
		combo++
		var payload struct {
			Name            string                 `json:"name"`
			Symbol          string                 `json:"symbol"`
			AggregationMode string                 `json:"aggregation_mode"`
			Members         []strategy.ComboMember `json:"members"`
		}
		if err := json.Unmarshal([]byte(tpl.DefaultConfigJSON), &payload); err != nil {
			t.Errorf("template %s: default_config invalid: %v", tpl.ID, err)
			continue
		}
		cfg := &strategy.ComboConfig{
			ID:              "test-" + tpl.ID,
			Name:            payload.Name,
			Symbol:          payload.Symbol,
			Members:         payload.Members,
			AggregationMode: payload.AggregationMode,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("template %s: combo validate: %v", tpl.ID, err)
			continue
		}
		if _, err := strategy.NewStrategyCombo(cfg); err != nil {
			t.Errorf("template %s: NewStrategyCombo: %v", tpl.ID, err)
		}
	}
	if combo != 4 {
		t.Errorf("combo templates = %d, want 4", combo)
	}
}
