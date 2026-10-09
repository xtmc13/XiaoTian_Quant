package handler

import (
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// clampRestoredQtyByPaperBacking paper 模式重启恢复注入的余额背书钳制。
//
// 恢复注入的只是策略引擎状态（in_position/entry_price），平仓能否成交取决于
// paper 账户里有没有真实可平的对侧——OMS 资金锁查的是 paper 交易所账户账本
// （paper/paper.go，重启经 RestoreAccount 完整恢复）。按方向取背书：
//
//   - 净多（net>0）：背书 = min(base free 余额, 镜像持仓净量>0 部分)——平仓单
//     是卖出，需要账户里有真实可卖的 base；
//   - 净空（net<0，合约空单，2026-10-09 H1 片）：背书 = 镜像持仓空头量
//     （NetPositionQuantity<0 的绝对值）——平仓单是买回，需要账户里真实存在
//     这笔空头负债；开空成交的 USDT 回款已记入账户，买回用 quote 支付
//     （quote 被其他策略抽干的残余风险见 handler 注入处的 WARN，不在此处钳——
//     钳小注入量只会让账户空头残留失控，比注入完整量更糟）。
//
// 两方向共同的兜底语义：
//   - 背书 ≤ 0 → ok=false：不注入，策略空仓起步（in_position=false）——
//     杜绝"引擎以为有仓、账户没有"的僵尸态（2026-10-08 生产实锤：账本净额
//     注入 12.52 SOL/0.001198 BTC，平仓单全部被余额锁误拒，策略卡死）；
//   - 0 < 背书 < |net| → 钳到背书量注入；
//   - 背书 ≥ |net| → 原样注入。
//
// live 模式不经过此函数（维持账本净额口径不动）。
func clampRestoredQtyByPaperBacking(symbol string, net float64) (qty float64, ok bool) {
	pe := paper.GetPaperExchange()
	if net < 0 {
		// 空单背书 = 镜像持仓空头量（账户真实负债）。镜像无空头（≤0）即零背书。
		if shortQty := -pe.NetPositionQuantity(symbol); shortQty > 0 {
			if shortQty < -net {
				return shortQty, true
			}
			return -net, true
		}
		return 0, false
	}
	base, _ := parseSymbolPair(symbol)
	backing := pe.FreeBalance(base)
	if posQty := pe.NetPositionQuantity(symbol); posQty > 0 && posQty < backing {
		backing = posQty
	}
	switch {
	case backing <= 0:
		return 0, false
	case backing < net:
		return backing, true
	default:
		return net, true
	}
}

// strategyContractCapable 报告策略配置的持仓语义是否为合约（可持有空单）。
// 判定口径与下单执行链路（app.applyStrategyExecConfig）同源：CRA 合约工厂
// （cra_contract）、config_json 解析出的 market_type=swap/futures/margin，
// 或顶层 category=contract / market_type=swap|futures|margin——任一命中即
// 视为合约。现货策略账本出现负净额属异常（卖出超持仓/账本污染），注入闸门
// 据本判定拒绝并 WARN（现货无空单语义，绝不把负净额恢复成"多单"）。
func strategyContractCapable(factoryName string, item map[string]any) bool {
	if factoryName == "cra_contract" {
		return true
	}
	if isContractCategory(item) {
		return true
	}
	if cj, _ := item["config_json"].(string); cj != "" {
		if p, err := cra.ParseCRAParams(cj); err == nil && p.IsContract() {
			return true
		}
	}
	return false
}
