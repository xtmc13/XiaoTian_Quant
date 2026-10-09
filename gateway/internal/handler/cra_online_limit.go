package handler

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── D2/H3：在线单量限制（online_order_limit，币富名词解释 #32）──
//
// 币富完整语义：限制同一机器人通过趋势开仓后进场持仓的交易对数量，多单数量
// 与空单数量**分别**限制——一个币富机器人可同时跑多个交易对，该参数是它
// 多/空两侧的并发持仓总闸。
//
// 本系统模型核查结论（cra_strategy.go OnBar）：CRA 引擎是单实例单 symbol 单
// 方向持仓模型——首单只在 !st.InPosition 时发出，持仓期间只会评估出场与补
// 仓，dual 双向模式也是每个循环只持一边（resolveContractSide 单选）。因此
// 单实例语境下"并发持仓周期数"恒为 1，online_order_limit 在实例内部没有可
// 调节的空间（恒等价于 1）。
//
// 据此把参数实现为币富本来的跨交易对总量控制口径，并按方向分列（H3 补全）：
// 统计同一用户名下 running 状态 CRA 合约实例，按实例 direction 归入多/空两
// 侧——long 计入多侧、short 计入空侧、dual 两侧各占一席（dual 实例下个循环
// 可能开任一侧，两侧都必须占名额）。待启动实例的多侧 running 数与空侧
// running 数分别不超上限才放行；任一侧超限即拒，409 文案注明是哪一侧。
//
// 校验点在用户主动 Start 路径（StartStrategyConfig/BatchStartConfigs）；
// ResumeRunningStrategiesLoop 的断点续跑不校验——那是恢复重启前已合规运行
// 的存量，限额管的是"新开"，且重启后按限额拒起会 Strand 既有持仓（无人管理
// 的风险敞口）。

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

// craItemDirection 读取配置的有效方向（long/short/dual），与引擎运行时口径
// 同源：cra.ParseCRAParams 从 config_json 取 direction（缺失默认 "long"）。
// 顶层平铺键兜底；非法值按 "long"（引擎 strVal 原样透传，未知值在引擎侧也
// 只走多/空分支，这里归多侧保守计数）。
func craItemDirection(item map[string]any) string {
	dir := ""
	if cj, ok := item["config_json"].(string); ok && cj != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(cj), &parsed) == nil {
			dir = strings.ToLower(strings.TrimSpace(getString(parsed, "direction", "")))
		}
	}
	if dir == "" {
		dir = strings.ToLower(strings.TrimSpace(getString(item, "direction", "")))
	}
	if dir != "short" && dir != "dual" {
		dir = "long"
	}
	return dir
}

// countRunningCRAContractsBySide 统计同一用户（user_id 相同；单用户/无属主
// 部署双方都是 0，自然归入同组）名下 running 状态的 CRA 合约实例数，按方向
// 分列：long 计多侧、short 计空侧、dual 两侧各占一席。excludeID 除外
// （重启自身时自己不计入名额）。
func countRunningCRAContractsBySide(userID int64, excludeID string) (longN, shortN int) {
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
		if !isCRAContractItem(it) {
			continue
		}
		switch craItemDirection(it) {
		case "short":
			shortN++
		case "dual":
			longN++
			shortN++
		default:
			longN++
		}
	}
	return longN, shortN
}

// enforceOnlineOrderLimit 启动前校验：待启动配置是 CRA 合约实例时，该用户
// 名下 running 的 CRA 合约实例按方向分列计数——多侧/空侧 running 数分别
// 达到本配置的 online_order_limit 即拒绝启动（报错注明超限的是哪一侧）。
// 非 CRA 合约配置一律放行（现货无此参数，币富该功能在合约页）。
func enforceOnlineOrderLimit(id string, item map[string]any) error {
	if !isCRAContractItem(item) {
		return nil
	}
	limit := craOnlineOrderLimit(item)
	dir := craItemDirection(item)
	longN, shortN := countRunningCRAContractsBySide(getInt64Of(item, "user_id"), id)
	if dir != "short" && longN >= limit {
		return fmt.Errorf("在线单量限制（多侧）：当前已有 %d 个多单侧 running CRA 合约实例，达到本策略上限 %d（online_order_limit，多/空分别计）；请先停止部分多单实例或调大该值", longN, limit)
	}
	if dir != "long" && shortN >= limit {
		return fmt.Errorf("在线单量限制（空侧）：当前已有 %d 个空单侧 running CRA 合约实例，达到本策略上限 %d（online_order_limit，多/空分别计）；请先停止部分空单实例或调大该值", shortN, limit)
	}
	return nil
}
