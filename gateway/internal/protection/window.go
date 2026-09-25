package protection

import (
	"fmt"
	"time"
)

// windowParams 是 freqtrade 风格的保护时间窗参数（见 iprotection.py）：
//   - 回溯窗：lookback_period_candles × timeframe，或 lookback_period（分钟）
//   - 锁定时长：stop_duration_candles × timeframe，或 stop_duration（分钟）
//   - unlock_at（"HH:MM"）：固定时刻解锁，优先级高于 stop_duration
//
// K线参数与分钟参数二选一（freqtrade 同此约定：配置了 candles 就用 candles）。
type windowParams struct {
	LookbackPeriodCandles int    `json:"lookback_period_candles,omitempty"`
	LookbackPeriodMinutes int    `json:"lookback_period,omitempty"`
	StopDurationCandles   int    `json:"stop_duration_candles,omitempty"`
	StopDurationMinutes   int    `json:"stop_duration,omitempty"`
	UnlockAt              string `json:"unlock_at,omitempty"`
	Timeframe             string `json:"timeframe,omitempty"`
}

// parseWindowParams 从配置解析时间窗参数。
// 未提供任何回溯参数时回退到 defaultLookbackCandles × defaultTimeframe；
// 未提供任何锁定时长参数时回退到 defaultStopCandles × defaultTimeframe。
func parseWindowParams(params map[string]any, defaultLookbackCandles, defaultStopCandles int, defaultTimeframe string) windowParams {
	w := windowParams{Timeframe: defaultTimeframe}
	if v, ok := toInt(params["lookback_period_candles"]); ok {
		w.LookbackPeriodCandles = v
	}
	if v, ok := toInt(params["lookback_period"]); ok {
		w.LookbackPeriodMinutes = v
	}
	if v, ok := toInt(params["stop_duration_candles"]); ok {
		w.StopDurationCandles = v
	}
	if v, ok := toInt(params["stop_duration"]); ok {
		w.StopDurationMinutes = v
	}
	if v, ok := params["timeframe"].(string); ok && v != "" {
		w.Timeframe = v
	}
	if v, ok := params["unlock_at"].(string); ok && v != "" {
		w.UnlockAt = v
	}
	if w.LookbackPeriodCandles <= 0 && w.LookbackPeriodMinutes <= 0 {
		w.LookbackPeriodCandles = defaultLookbackCandles
	}
	if w.StopDurationCandles <= 0 && w.StopDurationMinutes <= 0 && w.UnlockAt == "" {
		w.StopDurationCandles = defaultStopCandles
	}
	return w
}

// lookback 返回回溯窗口时长。
func (w windowParams) lookback() time.Duration {
	if w.LookbackPeriodCandles > 0 {
		return time.Duration(w.LookbackPeriodCandles) * candleDurationOf(w.Timeframe)
	}
	return time.Duration(w.LookbackPeriodMinutes) * time.Minute
}

// stopDuration 返回锁定时长（unlock_at 模式下无固定时长，返回 0）。
func (w windowParams) stopDuration() time.Duration {
	if w.StopDurationCandles > 0 {
		return time.Duration(w.StopDurationCandles) * candleDurationOf(w.Timeframe)
	}
	return time.Duration(w.StopDurationMinutes) * time.Minute
}

// lockEnd 计算解锁时刻（对标 freqtrade IProtection.calculate_lock_end）：
// 以 anchor（窗口内最近一次触发事件的时间）为基准 + stop_duration；
// 配置了 unlock_at 时解锁到 anchor 当天的下一个 HH:MM（已过则顺延一天）。
func (w windowParams) lockEnd(anchor time.Time) time.Time {
	if w.UnlockAt != "" {
		var hour, minute int
		if _, err := fmt.Sscanf(w.UnlockAt, "%d:%d", &hour, &minute); err == nil && hour >= 0 && hour <= 23 && minute >= 0 && minute <= 59 {
			unlock := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), hour, minute, 0, 0, anchor.Location())
			if !unlock.After(anchor) {
				unlock = unlock.Add(24 * time.Hour)
			}
			return unlock
		}
	}
	return anchor.Add(w.stopDuration())
}

// candleDurationOf 把 timeframe 字符串换算为时长（未知值回退 1h）。
func candleDurationOf(timeframe string) time.Duration {
	switch timeframe {
	case "1m":
		return time.Minute
	case "3m":
		return 3 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "8h":
		return 8 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "1d":
		return 24 * time.Hour
	case "3d":
		return 72 * time.Hour
	case "1w":
		return 7 * 24 * time.Hour
	default:
		return time.Hour
	}
}
