// Package pystrat 实现"用户 Python 策略契约化运行时 v1.1"：
// 用户写一个约定契约的 Python 文件（STRATEGY_MANIFEST + initialize/on_bar
// 回调，可选 on_order），平台在独立 python 子进程沙箱里长驻执行，K 线经
// 事件总线驱动 on_bar，订单回报（client_oid "pystrat:<id>" 前缀）驱动
// on_order；context.buy/sell 动作转成信号走 OMS 统一执行层下单（现货市价/
// v1.1 现货限价/合约杠杆）。
//
// 对标 QuantDinger Strategy API V2 / freqtrade IStrategy；契约全文见
// docs/PYTHON_STRATEGY_API.md。
package pystrat

import (
	"fmt"
	"strings"
)

// DirectionLong/Short/Both 是 manifest.direction 的合法值。
const (
	DirectionLong  = "long"
	DirectionShort = "short"
	DirectionBoth  = "both"
)

// Manifest 是 STRATEGY_MANIFEST 解析后的契约（docs/PYTHON_STRATEGY_API.md）。
type Manifest struct {
	Name      string         `json:"name"`
	Symbol    string         `json:"symbol"`
	Interval  string         `json:"interval"`
	Direction string         `json:"direction"`
	Params    map[string]any `json:"params"`
	Risk      ManifestRisk   `json:"risk"`
}

// ManifestRisk 是 manifest.risk 段：max_position_pct 是仓位名义价值占权益
// 上限（0<pct<=1，如 0.5=50%）；stop_loss_pct / take_profit_pct 是默认
// 止损/止盈百分比（0<pct<1），策略可用 context.set_stop_loss/set_take_profit 覆盖。
// leverage/margin_mode 是 v1.1 合约执行覆盖：market='futures' 时作用于
// context.buy 的合约下单，优先级高于平台保存的 xt_pystrategies 列。
type ManifestRisk struct {
	MaxPositionPct float64 `json:"max_position_pct"`
	StopLossPct    float64 `json:"stop_loss_pct"`
	TakeProfitPct  float64 `json:"take_profit_pct"`
	Leverage       int     `json:"leverage,omitempty"`
	MarginMode     string  `json:"margin_mode,omitempty"` // cross | isolated
}

// MaxLeverage 是合约杠杆上限（与 store.PyStratMaxLeverage 同值，主流
// 交易所 U 本位永续上限 125）。
const MaxLeverage = 125

// Validate 校验契约字段，返回中文错误消息（空串 = 通过）。
func (m *Manifest) Validate() string {
	if strings.TrimSpace(m.Name) == "" {
		return "STRATEGY_MANIFEST.name 不能为空"
	}
	if strings.TrimSpace(m.Symbol) == "" {
		return "STRATEGY_MANIFEST.symbol 不能为空（如 BTC/USDT）"
	}
	switch m.Interval {
	case "1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d":
	default:
		return fmt.Sprintf("STRATEGY_MANIFEST.interval 不支持 %q（支持 1m/3m/5m/15m/30m/1h/2h/4h/6h/8h/12h/1d）", m.Interval)
	}
	switch m.Direction {
	case DirectionLong, DirectionShort, DirectionBoth:
	default:
		return fmt.Sprintf("STRATEGY_MANIFEST.direction 必须是 long|short|both，当前 %q", m.Direction)
	}
	if m.Risk.MaxPositionPct < 0 || m.Risk.MaxPositionPct > 1 {
		return "STRATEGY_MANIFEST.risk.max_position_pct 必须在 (0,1] 区间（如 0.5 表示 50%），0 表示不限制"
	}
	if m.Risk.StopLossPct < 0 || m.Risk.StopLossPct >= 1 {
		return "STRATEGY_MANIFEST.risk.stop_loss_pct 必须在 (0,1) 区间"
	}
	if m.Risk.TakeProfitPct < 0 || m.Risk.TakeProfitPct >= 1 {
		return "STRATEGY_MANIFEST.risk.take_profit_pct 必须在 (0,1) 区间"
	}
	if m.Risk.Leverage < 0 || m.Risk.Leverage > MaxLeverage {
		return fmt.Sprintf("STRATEGY_MANIFEST.risk.leverage 必须在 [1,%d] 区间（0 表示不覆盖平台设置）", MaxLeverage)
	}
	switch m.Risk.MarginMode {
	case "", "cross", "isolated":
	default:
		return fmt.Sprintf("STRATEGY_MANIFEST.risk.margin_mode 必须是 cross|isolated，当前 %q", m.Risk.MarginMode)
	}
	return ""
}

// EffectiveLeverage 解析运行时杠杆：manifest.risk.leverage（>0 时）覆盖
// 平台保存的 record 值；都无效时回退 1。
func (m *Manifest) EffectiveLeverage(recordLeverage int) int {
	if m.Risk.Leverage > 0 {
		return m.Risk.Leverage
	}
	if recordLeverage > 0 {
		return recordLeverage
	}
	return 1
}

// EffectiveMarginMode 解析运行时保证金模式：manifest.risk.margin_mode
// （非空时）覆盖平台保存的 record 值；都无效时回退 cross。
func (m *Manifest) EffectiveMarginMode(recordMode string) string {
	if m.Risk.MarginMode != "" {
		return m.Risk.MarginMode
	}
	switch recordMode {
	case "cross", "isolated":
		return recordMode
	}
	return "cross"
}

// EffectiveParams 把运行时覆盖 merge 到默认参数上（覆盖优先）。
func (m *Manifest) EffectiveParams(override map[string]any) map[string]any {
	out := make(map[string]any, len(m.Params)+len(override))
	for k, v := range m.Params {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

// normalizeSymbol 把 "BTC/USDT" 规范成 "BTCUSDT"（与 dca/grid runner 一致）。
func normalizeSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}
