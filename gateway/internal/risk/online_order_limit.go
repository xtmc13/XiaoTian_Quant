package risk

import "sync/atomic"

// ── 在线单量限制（CRA 合约跨实例总量闸，币富名词解释 #32）──
//
// 2026-10-10 起该参数从策略级 config_json 迁到风控中心风控参数（全局单份，
// 与 max_concurrent_orders 等同通道：config.yaml risk 段持久化 +
// PUT /api/risk/config 运行时生效）。多/空分列语义保留：单个全局上限值，
// 按实例 direction 分侧计数（enforcement 在 handler/cra_online_limit.go）。
//
// 存量策略 config_json 里已落的 online_order_limit 键自此忽略（不再逐策略读）。
//
// 默认 10（与迁移前 cra.ParseCRAParams/前端预设口径一致）：未配置（0）时
// OnlineOrderLimit() 返回默认值——限额参数必须始终有牙齿。
const DefaultOnlineOrderLimit = 10

var onlineOrderLimit atomic.Int64

// SetOnlineOrderLimit 设置全局在线单量限制（运行时立即生效）；<=0 视为
// 未设置，读取侧回退默认值。
func SetOnlineOrderLimit(v int) {
	onlineOrderLimit.Store(int64(v))
}

// OnlineOrderLimit 返回当前生效的全局在线单量限制（未设置时为
// DefaultOnlineOrderLimit）。
func OnlineOrderLimit() int {
	if v := int(onlineOrderLimit.Load()); v > 0 {
		return v
	}
	return DefaultOnlineOrderLimit
}
