// Package cra implements the CRA (币富量化) spot/contract strategy parameter model,
// defaults, parsing and validation used by the strategy executors and handlers.
package cra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// AddPositionItem defines a single averaging-down order.
type AddPositionItem struct {
	Order      int     `json:"order"`
	Multiplier float64 `json:"multiplier"`
	Spread     float64 `json:"spread"`
	Callback   float64 `json:"callback"`
	EmaEnabled bool    `json:"ema_enabled,omitempty"`
}

// MovingTPTier is one tier of the moving take-profit ladder.
type MovingTPTier struct {
	Ratio    float64 `json:"ratio"`
	Drawback float64 `json:"drawback"`
}

// CRAParams is the complete CRA strategy configuration.
type CRAParams struct {
	// Basic / open position
	FirstOrderPrice      float64 `json:"first_order_price"`
	FirstOrderAmount     float64 `json:"first_order_amount"`
	FirstOrderMultiplier float64 `json:"first_order_multiplier"`
	TradeCountMode       string  `json:"trade_count_mode"` // "single" | "cycle"
	LoopCount            int     `json:"loop_count"`

	// Averaging
	EnableAddPosition bool               `json:"enable_add_position"`
	OrderCount        int                `json:"order_count"`
	AddPositions      []*AddPositionItem `json:"add_positions"`

	// Take profit
	TakeProfitMethod      string          `json:"take_profit_method"` // "full" | "tail" | "head_tail"
	TPMode                string          `json:"tp_mode"`            // "static" | "moving"
	TakeProfitRatio       float64         `json:"take_profit_ratio"`
	ProfitCallback        float64         `json:"profit_callback"`
	MovingTakeProfitTiers []*MovingTPTier `json:"moving_take_profit_tiers"`

	// Contract open indicators
	OpenMacdEnabled       bool   `json:"open_macd_enabled"`
	OpenMacdPeriod        string `json:"open_macd_period"` // "close" | "5m" | "15m"
	OpenCounterEmaEnabled bool   `json:"open_counter_ema_enabled"`
	OpenCounterEmaPeriod  string `json:"open_counter_ema_period"`
	OpenTrendEmaEnabled   bool   `json:"open_trend_ema_enabled"`
	OpenTrendEmaPeriod    string `json:"open_trend_ema_period"`

	// 开仓指标选择器（IndicatorPicker）新键。OpenIndicator 为选择器标记键
	//（macd/ema_cross/rsi/trend/trend_long/trend_short/range/custom）；
	// IndicatorParams 承载各指标的自定义数值参数（fast/slow/signal/period
	// 等）。解析策略：新键优先，旧 open_* 键回退；引擎已认识的门槛仍走旧键，
	// custom 由 custom_indicator.go 经指标沙箱执行开仓门槛。
	OpenIndicator   string         `json:"open_indicator"`
	IndicatorParams map[string]any `json:"indicator_params"`

	// Contract add indicators
	AddMacdEnabled bool   `json:"add_macd_enabled"`
	AddMacdPeriod  string `json:"add_macd_period"`
	AddEmaEnabled  bool   `json:"add_ema_enabled"`
	AddEmaPeriod   string `json:"add_ema_period"`

	// Risk / protection
	WaterfallEnabled    bool    `json:"waterfall_enabled"`
	WaterfallProtection float64 `json:"waterfall_protection"`

	// Contract stop loss
	StopLossEnabled bool    `json:"stop_loss_enabled"`
	StopLossType    string  `json:"stop_loss_type"` // "ratio" | "amount" | "price"
	StopLossRatio   float64 `json:"stop_loss_ratio"`
	StopLossAmount  float64 `json:"stop_loss_amount"`
	StopLossPrice   float64 `json:"stop_loss_price"`

	// Contract reverse TP/SL
	ReverseTakeProfitPeriod string `json:"reverse_take_profit_period"` // "close" | "5m" | "15m"
	ReverseStopLoss         bool   `json:"reverse_stop_loss"`

	// Contract burn
	BurnGlobalEnabled   bool `json:"burn_global_enabled"`
	BurnGlobalThreshold int  `json:"burn_global_threshold"`
	BurnDualEnabled     bool `json:"burn_dual_enabled"`
	BurnDualThreshold   int  `json:"burn_dual_threshold"`

	// Contract extras
	OpenDouble        bool     `json:"open_double"`
	FollowTrend       bool     `json:"follow_trend"`
	OnlineOrderLimit  int      `json:"online_order_limit"`
	Leverage          float64  `json:"leverage"`
	Direction         string   `json:"direction"`     // "long" | "short" | "dual"
	MarketType        string   `json:"market_type"`   // "spot" | "swap"
	PositionSide      string   `json:"position_side"` // "LONG" | "SHORT" | "BOTH"
	MarginMode        string   `json:"margin_mode"`   // "cross" | "isolated"
	SelectedExchanges []string `json:"selected_exchanges"`
}

// IsSpot returns true when the params describe a spot strategy.
func (p *CRAParams) IsSpot() bool {
	return p.MarketType == "spot" || p.MarketType == ""
}

// IsContract returns true when the params describe a contract strategy.
func (p *CRAParams) IsContract() bool {
	return p.MarketType == "swap" || p.MarketType == "futures" || p.MarketType == "margin"
}

// AddPositionForOrder returns the add-position config for the nth order (1-based).
func (p *CRAParams) AddPositionForOrder(order int) *AddPositionItem {
	for _, ap := range p.AddPositions {
		if ap.Order == order {
			return ap
		}
	}
	return nil
}

// ParseCRAParams parses a JSON config string into CRAParams.
// It uses json.Number to avoid floating point truncation.
func ParseCRAParams(configJSON string) (*CRAParams, error) {
	if configJSON == "" {
		configJSON = "{}"
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(configJSON)))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse cra params: %w", err)
	}

	// If a nested config_json string is present (e.g. from the API request body),
	// merge its contents into the top-level map so both forms are supported.
	if cj, ok := raw["config_json"].(string); ok && cj != "" {
		var nested map[string]any
		if err := json.Unmarshal([]byte(cj), &nested); err == nil {
			for k, v := range nested {
				if _, exists := raw[k]; !exists {
					raw[k] = v
				}
			}
		}
	}

	p := &CRAParams{}
	p.FirstOrderPrice = numFloat(raw, "first_order_price", 0)
	p.FirstOrderAmount = numFloat(raw, "first_order_amount", 100)
	p.FirstOrderMultiplier = numFloat(raw, "first_order_multiplier", 1)
	p.TradeCountMode = strVal(raw, "trade_count_mode", "cycle")
	p.LoopCount = numInt(raw, "loop_count", 100)

	p.EnableAddPosition = boolVal(raw, "enable_add_position", true)
	p.OrderCount = numInt(raw, "order_count", 7)
	p.AddPositions = parseAddPositions(raw["add_positions"])

	p.TakeProfitMethod = strVal(raw, "take_profit_method", "full")
	p.TPMode = strVal(raw, "tp_mode", "moving")
	p.TakeProfitRatio = numFloat(raw, "take_profit_ratio", 0.013)
	p.ProfitCallback = numFloat(raw, "profit_callback", 0.003)
	p.MovingTakeProfitTiers = parseMovingTPTiers(raw["moving_take_profit_tiers"])

	p.OpenMacdEnabled = boolVal(raw, "open_macd_enabled", false)
	p.OpenMacdPeriod = strVal(raw, "open_macd_period", "close")
	p.OpenCounterEmaEnabled = boolVal(raw, "open_counter_ema_enabled", false)
	p.OpenCounterEmaPeriod = strVal(raw, "open_counter_ema_period", "close")
	p.OpenTrendEmaEnabled = boolVal(raw, "open_trend_ema_enabled", false)
	p.OpenTrendEmaPeriod = strVal(raw, "open_trend_ema_period", "close")

	// 开仓指标选择器新键：indicator_params 宽松校验（对象/正数范围），
	// custom 需要 code_id+name；非法值直接报错，避免脏配置静默进引擎。
	p.OpenIndicator = strings.TrimSpace(strVal(raw, "open_indicator", ""))
	if v, ok := raw["indicator_params"]; ok && v != nil {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("indicator_params must be a JSON object")
		}
		if err := ValidateIndicatorParams(m); err != nil {
			return nil, err
		}
		p.IndicatorParams = m
	}
	// 新键回退旧键：选择器参数里的 period 覆盖同义旧键，保持引擎行为一致。
	if p.IndicatorParams != nil {
		for _, ik := range []string{"macd", "ema_cross", "trend_long", "trend_short"} {
			sub, _ := p.IndicatorParams[ik].(map[string]any)
			if sub == nil {
				continue
			}
			if period := strVal(sub, "period", ""); period != "" && period != "close" {
				switch ik {
				case "macd":
					if p.OpenMacdPeriod == "close" {
						p.OpenMacdPeriod = period
					}
				default:
					if p.OpenTrendEmaPeriod == "close" {
						p.OpenTrendEmaPeriod = period
					}
				}
			}
		}
	}

	p.AddMacdEnabled = boolVal(raw, "add_macd_enabled", false)
	p.AddMacdPeriod = strVal(raw, "add_macd_period", "close")
	p.AddEmaEnabled = boolVal(raw, "add_ema_enabled", false)
	p.AddEmaPeriod = strVal(raw, "add_ema_period", "close")

	p.WaterfallEnabled = boolVal(raw, "waterfall_enabled", true)
	p.WaterfallProtection = numFloat(raw, "waterfall_protection", 0.02)

	p.StopLossEnabled = boolVal(raw, "stop_loss_enabled", false)
	p.StopLossType = strVal(raw, "stop_loss_type", "ratio")
	p.StopLossRatio = numFloat(raw, "stop_loss_ratio", 0)
	p.StopLossAmount = numFloat(raw, "stop_loss_amount", 0)
	p.StopLossPrice = numFloat(raw, "stop_loss_price", 0)

	p.ReverseTakeProfitPeriod = strVal(raw, "reverse_take_profit_period", "close")
	p.ReverseStopLoss = boolVal(raw, "reverse_stop_loss", false)

	p.BurnGlobalEnabled = boolVal(raw, "burn_global_enabled", false)
	p.BurnGlobalThreshold = numInt(raw, "burn_global_threshold", 5)
	p.BurnDualEnabled = boolVal(raw, "burn_dual_enabled", false)
	p.BurnDualThreshold = numInt(raw, "burn_dual_threshold", 3)

	p.OpenDouble = boolVal(raw, "open_double", false)
	p.FollowTrend = boolVal(raw, "follow_trend", false)
	p.OnlineOrderLimit = numInt(raw, "online_order_limit", 10)
	p.Leverage = numFloat(raw, "leverage", 1)
	p.Direction = strVal(raw, "direction", "long")
	p.MarketType = strVal(raw, "market_type", "spot")
	p.PositionSide = strVal(raw, "position_side", "LONG")
	p.MarginMode = strVal(raw, "margin_mode", "cross")
	p.SelectedExchanges = strSlice(raw, "selected_exchanges")

	p.fillMissingAddPositions()
	return p, nil
}

// ValidateIndicatorParams 对开仓指标选择器的 indicator_params 做宽松合法化：
// 顶层必须是对象；已知指标（macd/ema_cross/rsi/trend/trend_long/trend_short/
// range）的数值字段必须是正数；custom 需要 code_id（数值）+ name（字符串）。
// 未知键忽略（向前兼容）。handler Create/Update 与 ParseCRAParams 共用。
func ValidateIndicatorParams(m map[string]any) error {
	if m == nil {
		return nil
	}
	numeric := map[string][]string{
		"macd":        {"fast", "slow", "signal"},
		"ema_cross":   {"fast", "slow"},
		"trend_long":  {"fast", "slow"},
		"trend_short": {"fast", "slow"},
		"rsi":         {"period", "oversold", "overbought"},
		"trend":       {"period"},
		"range":       {"period", "neutral_band"},
	}
	for key, fields := range numeric {
		sub, ok := m[key]
		if !ok || sub == nil {
			continue
		}
		sm, ok := sub.(map[string]any)
		if !ok {
			return fmt.Errorf("indicator_params.%s must be a JSON object", key)
		}
		for _, f := range fields {
			v, exists := sm[f]
			if !exists || v == nil {
				continue
			}
			n, ok := toFloat(v)
			if !ok {
				return fmt.Errorf("indicator_params.%s.%s must be a number", key, f)
			}
			if n <= 0 {
				return fmt.Errorf("indicator_params.%s.%s must be positive", key, f)
			}
		}
	}
	if sub, ok := m["custom"]; ok && sub != nil {
		sm, ok := sub.(map[string]any)
		if !ok {
			return fmt.Errorf("indicator_params.custom must be a JSON object")
		}
		if _, ok := toFloat(sm["code_id"]); !ok {
			return fmt.Errorf("indicator_params.custom.code_id must be a number")
		}
		if strVal(sm, "name", "") == "" {
			return fmt.Errorf("indicator_params.custom.name must be a string")
		}
	}
	return nil
}

// toFloat 宽松数值转换（float64/int/json.Number/数字字符串）。
func toFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	}
	return 0, false
}

// Validate checks CRA params against frontend/CRA constraints.
func (p *CRAParams) Validate() error {
	if p.FirstOrderAmount < 1 || p.FirstOrderAmount > 10000 {
		return fmt.Errorf("first_order_amount must be between 1 and 10000")
	}
	if p.FirstOrderMultiplier < 1 || p.FirstOrderMultiplier > 10 {
		return fmt.Errorf("first_order_multiplier must be between 1 and 10")
	}
	if p.OrderCount < 0 || p.OrderCount > 20 {
		return fmt.Errorf("order_count must be between 0 and 20")
	}
	if p.IsContract() {
		if p.Leverage < 1 || p.Leverage > 150 {
			return fmt.Errorf("leverage must be between 1 and 150")
		}
	}
	if p.TPMode == "moving" && len(p.MovingTakeProfitTiers) != 4 {
		return fmt.Errorf("moving take profit requires exactly 4 tiers")
	}
	if p.TradeCountMode != "single" && p.TradeCountMode != "cycle" {
		return fmt.Errorf("trade_count_mode must be single or cycle")
	}
	return nil
}

// Clone returns a deep copy of CRAParams.
func (p *CRAParams) Clone() *CRAParams {
	cp := *p
	cp.AddPositions = make([]*AddPositionItem, len(p.AddPositions))
	for i, ap := range p.AddPositions {
		if ap != nil {
			apc := *ap
			cp.AddPositions[i] = &apc
		}
	}
	cp.MovingTakeProfitTiers = make([]*MovingTPTier, len(p.MovingTakeProfitTiers))
	for i, t := range p.MovingTakeProfitTiers {
		if t != nil {
			tc := *t
			cp.MovingTakeProfitTiers[i] = &tc
		}
	}
	cp.SelectedExchanges = make([]string, len(p.SelectedExchanges))
	copy(cp.SelectedExchanges, p.SelectedExchanges)
	return &cp
}

// fillMissingAddPositions adds default add-position entries so that OrderCount is always satisfiable.
func (p *CRAParams) fillMissingAddPositions() {
	if !p.EnableAddPosition || p.OrderCount <= 0 {
		return
	}
	existing := make(map[int]*AddPositionItem)
	for _, ap := range p.AddPositions {
		if ap != nil {
			existing[ap.Order] = ap
		}
	}
	defaults := defaultAddPositions(p.IsContract())
	for i := 1; i <= p.OrderCount; i++ {
		if existing[i] != nil {
			continue
		}
		fallback := defaults[i]
		if fallback == nil {
			fallback = &AddPositionItem{Order: i, Multiplier: 1, Spread: 0.03 * float64(i), Callback: 0.003}
		}
		p.AddPositions = append(p.AddPositions, fallback)
	}
}

// defaultAddPositions returns the CRA documented add-position ladder by market.
// No per-order indicator is enabled by default; users opt-in via the UI.
func defaultAddPositions(isContract bool) map[int]*AddPositionItem {
	if isContract {
		return map[int]*AddPositionItem{
			1: {Order: 1, Multiplier: 1, Spread: 0.03, Callback: 0.003, EmaEnabled: false},
			2: {Order: 2, Multiplier: 2, Spread: 0.04, Callback: 0.005, EmaEnabled: false},
			3: {Order: 3, Multiplier: 4, Spread: 0.05, Callback: 0.005, EmaEnabled: false},
			4: {Order: 4, Multiplier: 4, Spread: 0.07, Callback: 0.005, EmaEnabled: false},
			5: {Order: 5, Multiplier: 8, Spread: 0.09, Callback: 0.005, EmaEnabled: false},
			6: {Order: 6, Multiplier: 8, Spread: 0.10, Callback: 0.005, EmaEnabled: false},
			7: {Order: 7, Multiplier: 16, Spread: 0.11, Callback: 0.005, EmaEnabled: false},
			8: {Order: 8, Multiplier: 16, Spread: 0.12, Callback: 0.005, EmaEnabled: false},
			9: {Order: 9, Multiplier: 32, Spread: 0.15, Callback: 0.005, EmaEnabled: false},
		}
	}
	return map[int]*AddPositionItem{
		1: {Order: 1, Multiplier: 1, Spread: 0.035, Callback: 0.003},
		2: {Order: 2, Multiplier: 2, Spread: 0.05, Callback: 0.005},
		3: {Order: 3, Multiplier: 4, Spread: 0.07, Callback: 0.005},
		4: {Order: 4, Multiplier: 8, Spread: 0.09, Callback: 0.005},
		5: {Order: 5, Multiplier: 16, Spread: 0.11, Callback: 0.005},
		6: {Order: 6, Multiplier: 32, Spread: 0.13, Callback: 0.005},
		7: {Order: 7, Multiplier: 64, Spread: 0.15, Callback: 0.005},
	}
}

// DefaultSpot returns CRA defaults for a spot strategy type.
func DefaultSpot(strategyType string) *CRAParams {
	p := &CRAParams{
		FirstOrderPrice:      0,
		FirstOrderAmount:     10,
		FirstOrderMultiplier: 1,
		TradeCountMode:       "cycle",
		LoopCount:            100,
		EnableAddPosition:    true,
		OrderCount:           7,
		TakeProfitMethod:     "full",
		TPMode:               "moving",
		TakeProfitRatio:      0.02,
		ProfitCallback:       0.003,
		WaterfallEnabled:     true,
		WaterfallProtection:  0.02,
		Direction:            "long",
		MarketType:           "spot",
		PositionSide:         "LONG",
		MarginMode:           "cross",
	}
	switch strategyType {
	case "wallstreet":
		p.OrderCount = 5
		p.FirstOrderAmount = 20
	case "aggressive":
		p.OrderCount = 5
		p.FirstOrderAmount = 5
		p.WaterfallProtection = 0.03
	case "conservative":
		p.OrderCount = 10
		p.FirstOrderAmount = 50
		p.WaterfallProtection = 0.01
	case "high_frequency":
		p.OrderCount = 3
		p.FirstOrderAmount = 5
		p.LoopCount = 1000
	}
	p.AddPositions = make([]*AddPositionItem, 0, p.OrderCount)
	defaults := defaultAddPositions(false)
	for i := 1; i <= p.OrderCount; i++ {
		if d, ok := defaults[i]; ok {
			cp := *d
			p.AddPositions = append(p.AddPositions, &cp)
		}
	}
	p.MovingTakeProfitTiers = []*MovingTPTier{
		{Ratio: 0.02, Drawback: 0.20},
		{Ratio: 0.03, Drawback: 0.20},
		{Ratio: 0.04, Drawback: 0.10},
		{Ratio: 0.05, Drawback: 0.10},
	}
	return p
}

// DefaultContract returns CRA defaults for a contract strategy type.
func DefaultContract(strategyType string) *CRAParams {
	p := &CRAParams{
		FirstOrderPrice:      0,
		FirstOrderAmount:     5,
		FirstOrderMultiplier: 1,
		TradeCountMode:       "cycle",
		LoopCount:            5000,
		EnableAddPosition:    true,
		OrderCount:           9,
		TakeProfitMethod:     "full",
		TPMode:               "moving",
		TakeProfitRatio:      0.011,
		ProfitCallback:       0.003,
		WaterfallEnabled:     true,
		WaterfallProtection:  0.02,
		StopLossEnabled:      true,
		StopLossType:         "ratio",
		StopLossRatio:        0.40,
		Leverage:             10,
		Direction:            "long",
		MarketType:           "swap",
		PositionSide:         "LONG",
		MarginMode:           "cross",
	}
	switch strategyType {
	case "trend_short":
		p.Direction = "short"
		p.PositionSide = "SHORT"
	case "counter_stable":
		p.Direction = "dual"
		p.PositionSide = "BOTH"
		p.Leverage = 5
	case "counter_safe":
		p.Direction = "dual"
		p.PositionSide = "BOTH"
		p.Leverage = 3
		p.StopLossRatio = 0.20
	case "high_frequency":
		p.OrderCount = 5
		p.FirstOrderAmount = 2
		p.LoopCount = 10000
	case "head_tail_arbitrage":
		p.TakeProfitMethod = "head_tail"
		p.OrderCount = 5
	}
	p.AddPositions = make([]*AddPositionItem, 0, p.OrderCount)
	defaults := defaultAddPositions(true)
	for i := 1; i <= p.OrderCount; i++ {
		if d, ok := defaults[i]; ok {
			cp := *d
			p.AddPositions = append(p.AddPositions, &cp)
		}
	}
	p.MovingTakeProfitTiers = []*MovingTPTier{
		{Ratio: 0.011, Drawback: 0.15},
		{Ratio: 0.02, Drawback: 0.10},
		{Ratio: 0.03, Drawback: 0.10},
		{Ratio: 0.04, Drawback: 0.10},
	}
	return p
}

// ── raw JSON helpers ──

func strVal(m map[string]any, key, def string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return def
}

func boolVal(m map[string]any, key string, def bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	if v, ok := m[key].(string); ok {
		return v == "true" || v == "1"
	}
	if v, ok := m[key].(json.Number); ok {
		return v.String() != "0"
	}
	if v, ok := m[key].(float64); ok {
		return v != 0
	}
	return def
}

func numFloat(m map[string]any, key string, def float64) float64 {
	v, ok := m[key]
	if !ok {
		return def
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return def
}

func numInt(m map[string]any, key string, def int) int {
	v, ok := m[key]
	if !ok {
		return def
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case json.Number:
		n, _ := val.Int64()
		return int(n)
	case string:
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return def
}

func strSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		var out []string
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func parseAddPositions(v any) []*AddPositionItem {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []*AddPositionItem
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		emaEnabled := boolVal(m, "ema_enabled", false)
		if !emaEnabled {
			emaEnabled = boolVal(m, "ema", false)
		}
		out = append(out, &AddPositionItem{
			Order:      numInt(m, "order", 0),
			Multiplier: numFloat(m, "multiplier", 1),
			Spread:     numFloat(m, "spread", 0.03),
			Callback:   numFloat(m, "callback", 0.003),
			EmaEnabled: emaEnabled,
		})
	}
	return out
}

func parseMovingTPTiers(v any) []*MovingTPTier {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []*MovingTPTier
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, &MovingTPTier{
			Ratio:    numFloat(m, "ratio", 0.02),
			Drawback: numFloat(m, "drawback", 0.10),
		})
	}
	return out
}
