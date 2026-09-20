// Package pystrat 实现"用户 Python 策略契约化运行时 v1"：
// 用户写一个约定契约的 Python 文件（STRATEGY_MANIFEST + initialize/on_bar 回调），
// 平台在独立 python 子进程沙箱里长驻执行，K 线经事件总线驱动 on_bar，
// context.buy/sell 动作转成信号走 OMS 统一执行层下单。
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
type ManifestRisk struct {
	MaxPositionPct float64 `json:"max_position_pct"`
	StopLossPct    float64 `json:"stop_loss_pct"`
	TakeProfitPct  float64 `json:"take_profit_pct"`
}

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
	return ""
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
