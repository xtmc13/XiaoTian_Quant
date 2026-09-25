package hyperopt

import (
	"fmt"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/protection"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── Protection 搜索空间（对标 freqtrade hyperopt 的 protection space）─────────
//
// freqtrade 把 protection 参数作为独立优化空间（hyperopt_optimizer.py 的
// "protection" space）：每个启用的 protection 的可调参数各生成一个维度，
// 回测评分时把采样值写回 protection 配置再跑回测。
//
// 维度命名约定：protection__<ProtectionName>__<param>
// 例：protection__StoplossGuard__trade_limit

// ProtectionParamPrefix 是 protection 空间维度名的前缀。
const ProtectionParamPrefix = "protection__"

// ProtectionParamDef 描述一个 protection 可调参数的默认搜索范围。
type ProtectionParamDef struct {
	Protection string
	Param      string
	Type       strategy.ParamType
	Min        float64
	Max        float64
	Step       float64
	Options    []string
}

// DimensionName 返回该参数的维度名。
func (d ProtectionParamDef) DimensionName() string {
	return d.Protection + "__" + d.Param
}

// ProtectionSpaceRegistry 返回各 protection 的可调参数及默认搜索范围。
// 与 internal/protection 的配置参数名一一对应（freqtrade 对齐后的命名）。
func ProtectionSpaceRegistry() map[string][]ProtectionParamDef {
	return map[string][]ProtectionParamDef{
		"CooldownPeriod": {
			{Protection: "CooldownPeriod", Param: "stop_duration_candles", Type: strategy.ParamInt, Min: 1, Max: 50, Step: 1},
		},
		"StoplossGuard": {
			{Protection: "StoplossGuard", Param: "lookback_period_candles", Type: strategy.ParamInt, Min: 1, Max: 48, Step: 1},
			{Protection: "StoplossGuard", Param: "trade_limit", Type: strategy.ParamInt, Min: 1, Max: 10, Step: 1},
			{Protection: "StoplossGuard", Param: "stop_duration_candles", Type: strategy.ParamInt, Min: 1, Max: 48, Step: 1},
			{Protection: "StoplossGuard", Param: "required_profit", Type: strategy.ParamFloat, Min: -0.10, Max: 0.05, Step: 0.01},
		},
		"MaxDrawdown": {
			{Protection: "MaxDrawdown", Param: "lookback_period_candles", Type: strategy.ParamInt, Min: 1, Max: 96, Step: 1},
			{Protection: "MaxDrawdown", Param: "trade_limit", Type: strategy.ParamInt, Min: 1, Max: 20, Step: 1},
			{Protection: "MaxDrawdown", Param: "stop_duration_candles", Type: strategy.ParamInt, Min: 1, Max: 48, Step: 1},
			{Protection: "MaxDrawdown", Param: "max_drawdown_pct", Type: strategy.ParamFloat, Min: 0.05, Max: 0.50, Step: 0.01},
			{Protection: "MaxDrawdown", Param: "calculation_mode", Type: strategy.ParamCategorical, Options: []string{"ratios", "equity"}},
		},
		"LowProfitPairs": {
			{Protection: "LowProfitPairs", Param: "lookback_period_candles", Type: strategy.ParamInt, Min: 1, Max: 96, Step: 1},
			{Protection: "LowProfitPairs", Param: "min_trade_count", Type: strategy.ParamInt, Min: 1, Max: 20, Step: 1},
			{Protection: "LowProfitPairs", Param: "stop_duration_candles", Type: strategy.ParamInt, Min: 1, Max: 48, Step: 1},
			{Protection: "LowProfitPairs", Param: "min_profit_ratio", Type: strategy.ParamFloat, Min: -0.10, Max: 0.10, Step: 0.01},
		},
		"DailyLossLimit": {
			{Protection: "DailyLossLimit", Param: "max_daily_loss_pct", Type: strategy.ParamFloat, Min: 1.0, Max: 20.0, Step: 0.5},
		},
		"ConsecutiveLosses": {
			{Protection: "ConsecutiveLosses", Param: "max_consecutive", Type: strategy.ParamInt, Min: 1, Max: 10, Step: 1},
			{Protection: "ConsecutiveLosses", Param: "stop_duration", Type: strategy.ParamInt, Min: 5, Max: 240, Step: 5},
		},
		"Overtrading": {
			{Protection: "Overtrading", Param: "max_trades_per_hour", Type: strategy.ParamInt, Min: 1, Max: 30, Step: 1},
		},
		"PriceJump": {
			{Protection: "PriceJump", Param: "max_jump_pct", Type: strategy.ParamFloat, Min: 1.0, Max: 20.0, Step: 0.5},
			{Protection: "PriceJump", Param: "stop_duration", Type: strategy.ParamInt, Min: 5, Max: 120, Step: 5},
			{Protection: "PriceJump", Param: "lookback_bars", Type: strategy.ParamInt, Min: 1, Max: 10, Step: 1},
		},
	}
}

// SpaceRangeOverride 允许调用方覆盖某个 protection 维度的搜索范围。
type SpaceRangeOverride struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// BuildProtectionSpaces 为给定的 protection 配置生成搜索维度。
// base 是任务携带的 protection 基础配置（name + params）；
// overrides 可按维度名缩小搜索范围（可选，nil 用注册表默认范围）。
// 未注册的 protection 名称返回错误（避免静默漏优化）。
func BuildProtectionSpaces(base []protection.ProtectionConfig, overrides map[string]SpaceRangeOverride) ([]Space, error) {
	registry := ProtectionSpaceRegistry()
	spaces := make([]Space, 0, len(base)*2)
	seen := make(map[string]bool)
	for _, pc := range base {
		defs, ok := registry[pc.Name]
		if !ok {
			return nil, fmt.Errorf("protection %q 不在可优化注册表中（可用: %s）",
				pc.Name, strings.Join(ProtectionSpaceNames(), ", "))
		}
		for _, d := range defs {
			dimName := ProtectionParamPrefix + d.Protection + "__" + d.Param
			if seen[dimName] {
				continue // 同一 protection 配置两次时维度去重
			}
			seen[dimName] = true
			sp := Space{
				Name:    dimName,
				Type:    d.Type,
				Min:     d.Min,
				Max:     d.Max,
				Step:    d.Step,
				Options: d.Options,
			}
			if ov, ok := overrides[dimName]; ok {
				sp.Min, sp.Max = ov.Min, ov.Max
				if ov.Step > 0 {
					sp.Step = ov.Step
				}
			}
			if sp.Type == strategy.ParamFloat && sp.Step <= 0 {
				sp.Step = 0.01
			}
			if sp.Type == strategy.ParamInt && sp.Step <= 0 {
				sp.Step = 1
			}
			spaces = append(spaces, sp)
		}
	}
	return spaces, nil
}

// ProtectionSpaceNames 返回注册表中的 protection 名称（排序不定）。
func ProtectionSpaceNames() []string {
	names := make([]string, 0, len(ProtectionSpaceRegistry()))
	for n := range ProtectionSpaceRegistry() {
		names = append(names, n)
	}
	return names
}

// ParseProtectionDimension 把维度名拆成 protection 名与参数名。
// 非 protection 维度返回 ok=false。
func ParseProtectionDimension(dim string) (protName, param string, ok bool) {
	if !strings.HasPrefix(dim, ProtectionParamPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(dim, ProtectionParamPrefix)
	idx := strings.Index(rest, "__")
	if idx <= 0 || idx == len(rest)-2 {
		return "", "", false
	}
	return rest[:idx], rest[idx+2:], true
}

// IsProtectionDimension 判断维度名是否属于 protection 空间。
func IsProtectionDimension(dim string) bool {
	_, _, ok := ParseProtectionDimension(dim)
	return ok
}

// SplitProtectionParams 把一次采样的参数集拆成策略参数与 protection 参数。
// protection 参数按 protection 名分组：{"StoplossGuard": {"trade_limit": 3}}。
func SplitProtectionParams(params map[string]any) (strategyParams map[string]any, protectionParams map[string]map[string]any) {
	strategyParams = make(map[string]any, len(params))
	protectionParams = make(map[string]map[string]any)
	for k, v := range params {
		if prot, param, ok := ParseProtectionDimension(k); ok {
			if protectionParams[prot] == nil {
				protectionParams[prot] = make(map[string]any)
			}
			protectionParams[prot][param] = v
			continue
		}
		strategyParams[k] = v
	}
	return strategyParams, protectionParams
}

// ApplyProtectionParams 把采样的 protection 参数覆盖到基础配置上，
// 返回可直接交给 protection.BuildManagerFromConfig 的配置。
// 基础配置中没有的 protection（优化维度里出现的）会被追加。
func ApplyProtectionParams(base []protection.ProtectionConfig, overrides map[string]map[string]any) []protection.ProtectionConfig {
	out := make([]protection.ProtectionConfig, 0, len(base)+len(overrides))
	applied := make(map[string]bool)
	for _, pc := range base {
		merged := make(map[string]any, len(pc.Params)+4)
		for k, v := range pc.Params {
			merged[k] = v
		}
		if ov, ok := overrides[pc.Name]; ok {
			for k, v := range ov {
				merged[k] = v
			}
			applied[pc.Name] = true
		}
		out = append(out, protection.ProtectionConfig{Name: pc.Name, Params: merged})
	}
	for name, ov := range overrides {
		if applied[name] {
			continue
		}
		merged := make(map[string]any, len(ov))
		for k, v := range ov {
			merged[k] = v
		}
		out = append(out, protection.ProtectionConfig{Name: name, Params: merged})
	}
	return out
}
