package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── D2：在线单量限制（online_order_limit，币富名词解释 #32）──
//
// 币富语义：限制同一机器人通过趋势开仓后进场持仓的交易对数量（多/空分别计）
// ——一个币富机器人可同时跑多个交易对，该参数是它的并发持仓总闸。
//
// 本系统模型核查结论（cra_strategy.go OnBar）：CRA 引擎是单实例单 symbol 单
// 方向持仓模型——首单只在 !st.InPosition 时发出，持仓期间只会评估出场与补
// 仓，dual 双向模式也是每个循环只持一边（resolveContractSide 单选）。因此
// 单实例语境下"并发持仓周期数"恒为 1，online_order_limit 在实例内部没有可
// 调节的空间（恒等价于 1）。
//
// 据此把参数实现为币富本来的跨交易对总量控制口径：限制同一用户名下 running
// 状态的 CRA 合约实例数上限。校验点在用户主动 Start 路径
// （StartStrategyConfig/BatchStartConfigs）；ResumeRunningStrategiesLoop
// 的断点续跑不校验——那是恢复重启前已合规运行的存量，限额管的是"新开"，
// 且重启后按限额拒起会 Strand 既有持仓（无人管理的风险敞口）。

// isCRAContractItem 判定配置是否映射到 CRA 合约引擎（cra_contract 及其前端
// 别名 trend_long/trend_short/counter_*/head_tail_arbitrage 等，与
// startStrategyInEngine 的 mapCRAFactory 口径同源）。
func isCRAContractItem(item map[string]any) bool {
	st := strings.ToLower(strings.TrimSpace(getString(item, "strategy_type", "")))
	if st == "" {
		st = strings.ToLower(strings.TrimSpace(getString(item, "bot_type", "")))
	}
	if st == "cra_contract" {
		return true
	}
	if st == "cra_spot" || st == "" {
		return false
	}
	mapped, ok := mapCRAFactory(st, item)
	return ok && mapped == "cra_contract"
}

// craOnlineOrderLimit 读取配置自身的 online_order_limit（config_json，兼容
// 顶层平铺键）：缺失默认 10（与 cra.ParseCRAParams/前端预设一致），<1 钳制
// 为 1——限额参数必须始终有牙齿，0/负数视为配置错误按最严口径处理。
func craOnlineOrderLimit(item map[string]any) int {
	limit := 10
	if v := getFloat(item, "online_order_limit", -1); v >= 0 {
		limit = int(v)
	}
	if cj, ok := item["config_json"].(string); ok && cj != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(cj), &parsed) == nil {
			if v := getFloat(parsed, "online_order_limit", -1); v >= 0 {
				limit = int(v)
			}
		}
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}

// countRunningCRAContracts 统计同一用户（user_id 相同；单用户/无属主部署
// 双方都是 0，自然归入同组）名下 running 状态的 CRA 合约实例数，excludeID
// 除外（重启自身时自己不计入名额）。
func countRunningCRAContracts(userID int64, excludeID string) int {
	n := 0
	for _, it := range store.GetStrategyConfigs() {
		if getString(it, "id", "") == excludeID {
			continue
		}
		if getString(it, "status", "") != "running" {
			continue
		}
		if getInt64Of(it, "user_id") != userID {
			continue
		}
		if isCRAContractItem(it) {
			n++
		}
	}
	return n
}

// enforceOnlineOrderLimit 启动前校验：待启动配置是 CRA 合约实例时，若该用户
// 名下 running 的 CRA 合约实例数已达本配置的 online_order_limit，拒绝启动。
// 非 CRA 合约配置一律放行（现货无此参数，币富该功能在合约页）。
func enforceOnlineOrderLimit(id string, item map[string]any) error {
	if !isCRAContractItem(item) {
		return nil
	}
	limit := craOnlineOrderLimit(item)
	running := countRunningCRAContracts(getInt64Of(item, "user_id"), id)
	if running >= limit {
		return fmt.Errorf("在线单量限制：当前已有 %d 个 CRA 合约实例在运行，达到本策略上限 %d（online_order_limit）；请先停止部分实例或调大该值", running, limit)
	}
	return nil
}
