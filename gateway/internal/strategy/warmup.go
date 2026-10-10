package strategy

import (
	"strconv"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// Warmupper 可选接口（2026-10-10 H4）：实现它的策略会把"历史 K 线"（收盘时刻
// 早于本次启动时刻的回补 bar）路由进 OnWarmupBar 而非 OnBar——暖机只养指标
// 序列，不得产生状态迁移/信号/下单副作用。引擎在 Start 记录启动时刻，dispatch
// 按 isWarmupBar 分类；handler 的直接暖机路径（供给管已在跑、无总线回补时）
// 对已确认的历史闭合 bar 直接调 OnWarmupBar。未实现接口的策略维持现状
// （全部 bar 走 OnBar），零行为变化。
type Warmupper interface {
	OnWarmupBar(bar model.Bar)
}

// isWarmupBar 判定 bar 是否为历史暖机 bar：收盘时刻 < 策略本次启动时刻
// （startedAtMs，引擎 Start 记录）。startedAtMs<=0（未记录/老引擎实例）一律
// 视为实时 bar——缺标记时不改变既有行为。
func isWarmupBar(bar model.Bar, startedAtMs int64) bool {
	if startedAtMs <= 0 || bar.Time <= 0 {
		return false
	}
	return barCloseMs(bar) < startedAtMs
}

// barCloseMs 估算 bar 收盘时刻（Unix ms）：model.Bar.Time 是开盘时刻且无
// closeTime 字段，收盘=开盘+周期。周期缺失/不可解析时回退开盘时刻（退化为
// "开盘早于启动"判定，与 pystrat runner 暖机口径一致）——该回退对"启动后
// 才收盘"的跨窗口 bar 会误判为暖机（保守方向：宁可多暖一根，不用陈旧数据
// 交易）；识别得了周期时用收盘时刻，跨窗口 bar 如实参与交易。
func barCloseMs(bar model.Bar) int64 {
	return bar.Time + intervalMs(bar.Interval)
}

// intervalMs 把币安 K 线周期（1m/15m/1h/4h/1d/1w…）折算为毫秒；无法解析
// 返回 0。
func intervalMs(interval string) int64 {
	s := strings.ToLower(strings.TrimSpace(interval))
	if s == "" {
		return 0
	}
	unit := s[len(s)-1]
	n, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	switch unit {
	case 's':
		return n * 1000
	case 'm':
		return n * 60 * 1000
	case 'h':
		return n * 3600 * 1000
	case 'd':
		return n * 24 * 3600 * 1000
	case 'w':
		return n * 7 * 24 * 3600 * 1000
	}
	return 0
}
