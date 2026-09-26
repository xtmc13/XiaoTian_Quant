package adapter

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// ── Coinbase Advanced Trade products 交易规则缓存 ──────────────────────────
//
// 实盘下单前的精度/限额规则来源：GET /api/v3/brokerage/products（签名端点）。
// Coinbase 用十进制 increment 串表述精度（与 Binance stepSize 同模式）：
//   - base_increment：数量最小步进（数量 FLOOR 到其整数倍）；
//   - quote_increment：价格最小步进（价格 ROUND half-up 到其整数倍）；
//   - base_min_size / quote_min_size：最小下单量 / 最小名义价值（USD 等计价货币）。
//
// 缓存是进程级的（每次下单新建 adapter，实例级缓存永远冷启动——参考
// binance_exchangeinfo.go 的同名说明）。

const coinbaseProductsTTL = time.Hour

// coinbaseProductCache 进程级缓存，带 TTL 与互斥保护；刷新时持锁串行，
// 防止并发下单触发 products 惊群（每小时至多一次刷新）。
type coinbaseProductCache struct {
	mu    sync.Mutex
	rules map[string]*LotFilters
	at    time.Time
	ttl   time.Duration // 可变以便测试；生产恒为 coinbaseProductsTTL
}

var sharedCoinbaseProducts = &coinbaseProductCache{ttl: coinbaseProductsTTL}

// resetCoinbaseProductCache 清空进程级缓存（测试用，避免用例间串数据）。
func resetCoinbaseProductCache() {
	sharedCoinbaseProducts.mu.Lock()
	defer sharedCoinbaseProducts.mu.Unlock()
	sharedCoinbaseProducts.rules, sharedCoinbaseProducts.at = nil, time.Time{}
	sharedCoinbaseProducts.ttl = coinbaseProductsTTL
}

// get 返回 product 的规则；语义与 binanceFilterCache.get 一致：
// 新鲜命中直接返回；过期刷新；刷新失败有旧数据降级用旧数据；无数据才 error。
func (c *coinbaseProductCache) get(productID string, fetch func() (map[string]*LotFilters, error)) (*LotFilters, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.at) < c.ttl && c.rules != nil {
		if f, ok := c.rules[productID]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("product %s not found in cached products", productID)
	}

	fresh, err := fetch()
	if err != nil {
		if f, ok := c.rules[productID]; ok {
			log.Printf("[Coinbase] WARN products refresh failed (%v), using stale filters for %s", err, productID)
			return f, nil
		}
		return nil, err
	}
	c.rules, c.at = fresh, time.Now()
	if f, ok := fresh[productID]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("product %s not found in products (%d products)", productID, len(fresh))
}

// productFilters 取产品规则（带进程级缓存）。
func (cb *CoinbaseAdapter) productFilters(productID string) (*LotFilters, error) {
	return sharedCoinbaseProducts.get(productID, cb.fetchProducts)
}

// fetchProducts 拉取并解析 products 全量产品规则（签名 GET，复用 cb.request）。
func (cb *CoinbaseAdapter) fetchProducts() (map[string]*LotFilters, error) {
	result, err := cb.request("GET", "/products", nil)
	if err != nil {
		return nil, fmt.Errorf("coinbase products: %w", err)
	}
	return parseCoinbaseProducts(result)
}

// parseCoinbaseProducts 容错解析 products 响应：increment/min_size 均为十进制
// 串，原样保留给 big.Rat 取整；缺失字段留空（对应校验跳过）。
func parseCoinbaseProducts(result map[string]any) (map[string]*LotFilters, error) {
	products, _ := result["products"].([]any)
	if len(products) == 0 {
		return nil, fmt.Errorf("parse products: no products in response")
	}
	out := make(map[string]*LotFilters, len(products))
	for _, item := range products {
		p, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := p["product_id"].(string)
		if id == "" {
			continue
		}
		out[id] = &LotFilters{
			StepSize:    getString(p, "base_increment", ""),
			MinQty:      getString(p, "base_min_size", ""),
			MaxQty:      getString(p, "base_max_size", ""),
			TickSize:    getString(p, "quote_increment", ""),
			MinNotional: getString(p, "quote_min_size", ""),
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("parse products: no usable products in response")
	}
	return out, nil
}

// normalizeCoinbaseOrder 把策略给出的浮点 price/quantity 规整为符合 Coinbase
// 规则的十进制下单串。契约同 normalizeBinanceOrder：
//   - *OrderConstraintError：本地规则校验拒绝（调用方必须中止下单）；
//   - 规则不可用：内部降级为旧格式串并记 WARN，返回 nil error 不阻塞下单。
func (cb *CoinbaseAdapter) normalizeCoinbaseOrder(symbol, orderType string, price, quantity float64) (qtyStr, priceStr string, err error) {
	ctx := "coinbase " + symbol
	isLimit := strings.EqualFold(orderType, "limit")

	f, ferr := cb.productFilters(symbol)
	if ferr != nil {
		// 降级：规则拉不到时维持旧行为（硬编码 6/2 位小数），记 WARN 不阻塞。
		log.Printf("[Coinbase] WARN products unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, ferr)
		qtyStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			priceStr = fmt.Sprintf("%.2f", price)
		}
		return qtyStr, priceStr, nil
	}

	qtyStr, qty, err := NormalizeQuantity(f, quantity, ctx)
	if err != nil {
		return "", "", err
	}

	p := price
	if isLimit {
		priceStr, p, err = NormalizePrice(f, price, ctx)
		if err != nil {
			return "", "", err
		}
	}

	if err := CheckMinNotional(f, qty, p, ctx); err != nil {
		return "", "", err
	}
	return qtyStr, priceStr, nil
}
