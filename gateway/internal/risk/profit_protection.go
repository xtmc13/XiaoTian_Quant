package risk

import "sync/atomic"

// 盈利保护开关（UI 可调，PUT /api/risk/config 写入，成交落点读取）。
// 默认 false；网关启动时由 app.Init 用 config.yaml risk.profit_protection_enabled 初始化。
var profitProtectionEnabled atomic.Bool

// SetProfitProtectionEnabled 设置盈利保护开关（运行时立即生效）。
func SetProfitProtectionEnabled(v bool) {
	profitProtectionEnabled.Store(v)
}

// ProfitProtectionEnabled 返回当前盈利保护开关状态。
func ProfitProtectionEnabled() bool {
	return profitProtectionEnabled.Load()
}
