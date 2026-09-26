package adapter

import (
	"fmt"
	"strings"
)

// ── IBKR 下单精度：保守本地规整 ─────────────────────────────────────────────
//
// IBKR Client Portal 没有公开的 symbol 级交易规则 REST（真实 minTick 按
// 合约/价格档位在 TWS 合约详情里，web API 不直接给出）。这里实现保守的本地
// 规整近似：
//   - quantity FLOOR 到 4 位小数（覆盖股票碎股与加密资产的常见粒度；
//     整股合约多出的 0 尾不影响成交）；
//   - price 按 min tick ROUND half-up：≥1 美元 0.01（美股常规 tick），
//     <1 美元 0.0001（sub-dollar/外汇/加密的最细档位）。
//
// 这是保守近似：实盘接入具体合约（尤其期货/外汇/低价股）前，应按 IBKR 合约
// 规则校准 tick；规整只会把价格收敛到更近的合法 tick，不会把数量放大。

// normalizeIBKROrder 返回规整后的 quantity/price 十进制串。
// qty FLOOR 后为 0（碎尘单）或非法精度时返回 *OrderConstraintError/ error，
// 调用方不得再把该单发给 IBKR。
func normalizeIBKROrder(symbol, orderType string, price, quantity float64) (qtyStr, priceStr string, err error) {
	ctx := "ibkr " + strings.ToUpper(symbol)

	qtyStr, qty, err := FloorToDecimals(quantity, 4)
	if err != nil {
		return "", "", fmt.Errorf("%s: quantity precision: %w", ctx, err)
	}
	if qty <= 0 {
		return "", "", &OrderConstraintError{
			Field: "quantity", Raw: quantity, Rounded: qty,
			Limit: "positive quantity (4dp floor)", Exchange: ctx,
		}
	}

	// min tick 近似：$1 为界的两档（见文件头注释）。
	tick := "0.01"
	if price > 0 && price < 1 {
		tick = "0.0001"
	}
	switch ibkrOrderType(orderType) {
	case "LMT", "STP":
		priceStr, _, err = RoundToTick(price, tick)
		if err != nil {
			return "", "", fmt.Errorf("%s: price precision: %w", ctx, err)
		}
	}
	return qtyStr, priceStr, nil
}
