package adapter

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// ── Alpaca assets 资产规则缓存 ──────────────────────────────────────────────
//
// 实盘下单前的精度规则来源：GET /v2/assets/{symbol}（按 symbol 单条拉取）。
// Alpaca 是美股/ETF 券商，精度语义与加密所不同：
//   - fractionable=false 的资产（部分股票/ETF）qty 必须是非负整数，
//     小数数量会被拒单——本地下单前 FLOOR 到整数；
//   - fractionable=true 的资产支持碎股，维持旧行为 %.6f；
//   - 价格按美股 min tick 规整：$1 以上 0.01；低于 $1 的 sub-dollar 档位
//     0.0001 仅对部分合规情形开放，保守统一按 0.01 取整。
//
// 缓存是进程级、按 symbol 的（每次下单新建 adapter，实例级缓存永远冷启动——
// 参考 binance_exchangeinfo.go 的同名说明）。

const alpacaAssetTTL = time.Hour

// alpacaAssetRules 单个资产的下单规则。
type alpacaAssetRules struct {
	fractionable bool
}

type alpacaAssetEntry struct {
	rules alpacaAssetRules
	at    time.Time
}

// alpacaAssetCache 进程级按 symbol 缓存，带 TTL 与互斥保护。
type alpacaAssetCache struct {
	mu      sync.Mutex
	entries map[string]alpacaAssetEntry
	ttl     time.Duration // 可变以便测试；生产恒为 alpacaAssetTTL
}

var sharedAlpacaAssets = &alpacaAssetCache{ttl: alpacaAssetTTL}

// resetAlpacaAssetCache 清空进程级缓存（测试用，避免用例间串数据）。
func resetAlpacaAssetCache() {
	sharedAlpacaAssets.mu.Lock()
	defer sharedAlpacaAssets.mu.Unlock()
	sharedAlpacaAssets.entries = nil
	sharedAlpacaAssets.ttl = alpacaAssetTTL
}

// get 返回 symbol 的资产规则；语义与 binanceFilterCache.get 一致：
// 新鲜命中直接返回；过期刷新；刷新失败有旧数据降级用旧数据；无数据才 error。
func (c *alpacaAssetCache) get(symbol string, fetch func() (alpacaAssetRules, error)) (alpacaAssetRules, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.entries[symbol]; ok && time.Since(e.at) < c.ttl {
		return e.rules, nil
	}

	fresh, err := fetch()
	if err != nil {
		if e, ok := c.entries[symbol]; ok {
			log.Printf("[Alpaca] WARN assets refresh failed (%v), using stale rules for %s", err, symbol)
			return e.rules, nil
		}
		return alpacaAssetRules{}, err
	}
	if c.entries == nil {
		c.entries = make(map[string]alpacaAssetEntry)
	}
	c.entries[symbol] = alpacaAssetEntry{rules: fresh, at: time.Now()}
	return fresh, nil
}

// assetRules 取资产规则（带进程级缓存）。
func (a *AlpacaAdapter) assetRules(symbol string) (alpacaAssetRules, error) {
	symbol = strings.ToUpper(symbol)
	return sharedAlpacaAssets.get(symbol, func() (alpacaAssetRules, error) {
		return a.fetchAsset(symbol)
	})
}

// fetchAsset 拉取并容错解析 /v2/assets/{symbol}。
func (a *AlpacaAdapter) fetchAsset(symbol string) (alpacaAssetRules, error) {
	result, err := a.get("/assets/" + symbol)
	if err != nil {
		return alpacaAssetRules{}, fmt.Errorf("alpaca assets %s: %w", symbol, err)
	}
	frac, ok := result["fractionable"].(bool)
	if !ok {
		return alpacaAssetRules{}, fmt.Errorf("alpaca assets %s: response missing fractionable", symbol)
	}
	return alpacaAssetRules{fractionable: frac}, nil
}

// normalizeAlpacaOrder 把策略给出的浮点 price/quantity 规整为符合 Alpaca 规则的
// 十进制下单串。契约同 normalizeBinanceOrder：
//   - *OrderConstraintError：本地规则校验拒绝（调用方必须中止下单）；
//   - 规则不可用：内部降级为旧格式串并记 WARN，返回 nil error 不阻塞下单。
func (a *AlpacaAdapter) normalizeAlpacaOrder(symbol, orderType string, price, quantity float64) (qtyStr, priceStr string, err error) {
	ctx := "alpaca " + strings.ToUpper(symbol)
	isLimit := strings.EqualFold(orderType, "limit")

	rules, rerr := a.assetRules(symbol)
	if rerr != nil {
		// 降级：规则拉不到时维持旧行为（硬编码 6/2 位小数），记 WARN 不阻塞。
		log.Printf("[Alpaca] WARN assets unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, rerr)
		qtyStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			priceStr = fmt.Sprintf("%.2f", price)
		}
		return qtyStr, priceStr, nil
	}

	qty := quantity
	if rules.fractionable {
		// 碎股资产：维持旧格式（Alpaca 碎股支持到 9 位小数，6 位是保守子集）。
		qtyStr = fmt.Sprintf("%.6f", quantity)
	} else {
		// 非碎股资产：qty 必须是整数，小数部分 FLOOR 掉（避免超仓方向的进位）。
		qtyStr, qty, err = FloorToDecimals(quantity, 0)
		if err != nil {
			return "", "", fmt.Errorf("%s: quantity precision: %w", ctx, err)
		}
	}
	if qty <= 0 {
		limit := "positive quantity"
		if !rules.fractionable {
			limit = "integer qty (non-fractionable asset)"
		}
		return "", "", &OrderConstraintError{
			Field: "quantity", Raw: quantity, Rounded: qty,
			Limit: limit, Exchange: ctx,
		}
	}

	if isLimit {
		// 美股 min tick 保守按 0.01（sub-dollar 0.0001 档位仅特定情形开放）。
		priceStr, _, err = RoundToDecimals(price, 2)
		if err != nil {
			return "", "", fmt.Errorf("%s: price precision: %w", ctx, err)
		}
	}
	return qtyStr, priceStr, nil
}
