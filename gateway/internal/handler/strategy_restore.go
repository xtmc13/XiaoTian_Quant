package handler

import (
	"github.com/xiaotian-quant/gateway/internal/paper"
)

// clampRestoredQtyByPaperBacking paper 模式重启恢复注入的余额背书钳制。
//
// 恢复注入的只是策略引擎状态（in_position/entry_price），平仓能否成交取决于
// paper 账户里有没有真实可卖的 base——OMS 余额锁查的是 paper 交易所账户账本
// （paper/paper.go，重启经 RestoreAccount 完整恢复）。背书 = paper 账户
// base free 余额与镜像持仓净量的较小者：
//
//   - 背书 ≤ 0 → ok=false：不注入，策略空仓起步（in_position=false）——
//     杜绝"引擎以为有仓、账户没有"的僵尸态（2026-10-08 生产实锤：账本净额
//     注入 12.52 SOL/0.001198 BTC，平仓单全部被余额锁误拒，策略卡死）；
//   - 0 < 背书 < net → 钳到背书量注入；
//   - 背书 ≥ net → 原样注入。
//
// live 模式不经过此函数（维持账本净额口径不动）。
func clampRestoredQtyByPaperBacking(symbol string, net float64) (qty float64, ok bool) {
	pe := paper.GetPaperExchange()
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
