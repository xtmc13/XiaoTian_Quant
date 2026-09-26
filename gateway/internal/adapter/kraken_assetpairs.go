package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Kraken AssetPairs 交易规则缓存 ──────────────────────────────────────────
//
// 实盘下单前的精度/限额规则来源：GET /0/public/AssetPairs（公开端点）。
// Kraken 用"小数位数"而非 stepSize 表述精度：
//   - pair_decimals：价格允许的小数位（价格 ROUND half-up 到该位数）；
//   - lot_decimals：数量允许的小数位（数量 FLOOR 到该位数，避免超仓）；
//   - ordermin：最小下单量（base 货币）。
//
// 缓存是进程级的：app 层实盘路径每次下单都会新建 adapter（参考
// binance_exchangeinfo.go 的同名说明），实例级缓存会永远冷启动。
// AssetPairs 是公开参考数据，与账户凭证无关，跨实例共享无泄漏风险。

const krakenAssetPairsTTL = time.Hour

// krakenPairRules 单个交易对的下单规则（位数模式 + 最小下单量原始串）。
type krakenPairRules struct {
	priceDecimals int
	lotDecimals   int
	orderMin      string
}

// krakenAssetPairsCache 进程级缓存，带 TTL 与互斥保护；刷新时持锁串行，
// 防止并发下单触发 AssetPairs 惊群（每小时至多一次刷新）。
type krakenAssetPairsCache struct {
	mu    sync.Mutex
	rules map[string]krakenPairRules
	at    time.Time
	ttl   time.Duration // 可变以便测试；生产恒为 krakenAssetPairsTTL
}

var sharedKrakenAssetPairs = &krakenAssetPairsCache{ttl: krakenAssetPairsTTL}

// resetKrakenAssetPairsCache 清空进程级缓存（测试用，避免用例间串数据）。
func resetKrakenAssetPairsCache() {
	sharedKrakenAssetPairs.mu.Lock()
	defer sharedKrakenAssetPairs.mu.Unlock()
	sharedKrakenAssetPairs.rules, sharedKrakenAssetPairs.at = nil, time.Time{}
	sharedKrakenAssetPairs.ttl = krakenAssetPairsTTL
}

// get 返回 pair 的规则；缓存新鲜则直接命中，过期则刷新。
// 刷新失败但有旧数据时降级用旧数据；完全没有数据时才返回 error，
// 由下单路径决定降级策略（与 binanceFilterCache.get 同语义）。
func (c *krakenAssetPairsCache) get(pair string, fetch func() (map[string]krakenPairRules, error)) (krakenPairRules, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.at) < c.ttl && c.rules != nil {
		if r, ok := c.rules[pair]; ok {
			return r, nil
		}
		return krakenPairRules{}, fmt.Errorf("pair %s not found in cached AssetPairs", pair)
	}

	fresh, err := fetch()
	if err != nil {
		if r, ok := c.rules[pair]; ok {
			log.Printf("[Kraken] WARN AssetPairs refresh failed (%v), using stale rules for %s", err, pair)
			return r, nil
		}
		return krakenPairRules{}, err
	}
	c.rules, c.at = fresh, time.Now()
	if r, ok := fresh[pair]; ok {
		return r, nil
	}
	return krakenPairRules{}, fmt.Errorf("pair %s not found in AssetPairs (%d pairs)", pair, len(fresh))
}

// pairRules 取交易对规则（带进程级缓存）。
func (k *KrakenAdapter) pairRules(krakenPair string) (krakenPairRules, error) {
	return sharedKrakenAssetPairs.get(krakenPair, k.fetchAssetPairs)
}

// fetchAssetPairs 拉取并解析 AssetPairs 全量交易对规则。
// 公开端点，无需签名；独立 10s 超时，避免元数据拉取拖垮下单延迟。
func (k *KrakenAdapter) fetchAssetPairs() (map[string]krakenPairRules, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", k.restURL()+"/0/public/AssetPairs", nil)
	if err != nil {
		return nil, err
	}
	resp, err := k.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kraken AssetPairs: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("kraken AssetPairs read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kraken AssetPairs: HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	return parseKrakenAssetPairs(body)
}

// parseKrakenAssetPairs 容错解析 AssetPairs 响应。
// 字段类型宽容：decimals 可能是数字或数字串，ordermin 可能是串或数字；
// 同时按结果 key 与 altname 建别名（请求用内部名 XXBTZUSD，部分符号表按
// altname XBTUSD 组织），".d" 暗池条目跳过。
func parseKrakenAssetPairs(body []byte) (map[string]krakenPairRules, error) {
	var raw struct {
		Error  []string                  `json:"error"`
		Result map[string]map[string]any `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse AssetPairs: %w", err)
	}
	if len(raw.Error) > 0 {
		return nil, fmt.Errorf("kraken AssetPairs error: %v", raw.Error)
	}
	if len(raw.Result) == 0 {
		return nil, fmt.Errorf("parse AssetPairs: no pairs in response")
	}

	out := make(map[string]krakenPairRules, len(raw.Result))
	for key, entry := range raw.Result {
		if strings.HasSuffix(key, ".d") {
			continue
		}
		r := krakenPairRules{
			priceDecimals: int(parseFloatSafe(entry["pair_decimals"])),
			lotDecimals:   int(parseFloatSafe(entry["lot_decimals"])),
		}
		switch v := entry["ordermin"].(type) {
		case string:
			r.orderMin = v
		case float64:
			r.orderMin = trimFloat(v)
		}
		out[key] = r
		if alt, _ := entry["altname"].(string); alt != "" {
			if _, exists := out[alt]; !exists {
				out[alt] = r
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("parse AssetPairs: no usable pairs in response")
	}
	return out, nil
}

// normalizeKrakenOrder 把策略给出的浮点 price/quantity 规整为符合 Kraken 规则的
// 十进制下单串。返回 error 时分两种（与 normalizeBinanceOrder 同契约）：
//   - *OrderConstraintError：本地规则校验拒绝（调用方必须中止下单）；
//   - 规则不可用（网络/未知 pair）：内部降级为旧格式串并记 WARN，返回 nil error。
func (k *KrakenAdapter) normalizeKrakenOrder(symbol, krakenPair, orderType string, price, quantity float64) (qtyStr, priceStr string, err error) {
	ctx := fmt.Sprintf("kraken %s(%s)", symbol, krakenPair)
	isLimit := strings.EqualFold(orderType, "limit")

	rules, rerr := k.pairRules(krakenPair)
	if rerr != nil {
		// 降级：规则拉不到时维持旧行为（硬编码 6/2 位小数），记 WARN 不阻塞。
		log.Printf("[Kraken] WARN AssetPairs unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, rerr)
		qtyStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			priceStr = fmt.Sprintf("%.2f", price)
		}
		return qtyStr, priceStr, nil
	}

	qtyStr, qty, err := FloorToDecimals(quantity, rules.lotDecimals)
	if err != nil {
		return "", "", fmt.Errorf("%s: quantity precision: %w", ctx, err)
	}
	if qty <= 0 {
		return "", "", &OrderConstraintError{
			Field: "quantity", Raw: quantity, Rounded: qty,
			Limit: "positive quantity", Exchange: ctx,
		}
	}
	if min, perr := parseDecimal(rules.orderMin); perr == nil && min.Sign() > 0 {
		q, _ := new(big.Rat).SetString(strconv.FormatFloat(qty, 'f', -1, 64))
		if q.Cmp(min) < 0 {
			return "", "", &OrderConstraintError{
				Field: "quantity", Raw: quantity, Rounded: qty,
				Limit: "ordermin " + rules.orderMin, Exchange: ctx,
			}
		}
	}

	if isLimit {
		priceStr, _, err = RoundToDecimals(price, rules.priceDecimals)
		if err != nil {
			return "", "", fmt.Errorf("%s: price precision: %w", ctx, err)
		}
	}
	return qtyStr, priceStr, nil
}
