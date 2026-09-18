package risk

import "sync/atomic"

// indicatorFailOpen 控制"自定义指标开仓检查失败"时的引擎行为：
// true=放行（fail-open，保证交易连续性），false=拦截（fail-close）。
// 默认放行，可通过 config.yaml risk.indicator_fail_open 或 PUT /api/risk/config 调整。
var indicatorFailOpen atomic.Bool

func init() {
	indicatorFailOpen.Store(true)
}

// SetIndicatorFailOpen 设置自定义指标开仓失败放行开关（运行时立即生效）。
func SetIndicatorFailOpen(v bool) {
	indicatorFailOpen.Store(v)
}

// IndicatorFailOpen 返回自定义指标开仓检查失败时的放行开关状态。
func IndicatorFailOpen() bool {
	return indicatorFailOpen.Load()
}
