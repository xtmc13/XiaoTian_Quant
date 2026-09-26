package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ── Bybit instruments-info 交易规则缓存 ─────────────────────────────────────
//
// 实盘下单前的精度/限额规则来源（与 Binance exchangeInfo 缓存同模式）：
//   - 现货 GET /v5/market/instruments-info?category=spot
//   - 合约 GET /v5/market/instruments-info?category=linear
// 解析每个 symbol 的 lotSizeFilter（qtyStep/basePrecision、minOrderQty、
// maxOrderQty、minOrderAmt/minNotionalValue）与 priceFilter（tickSize、
// min/maxPrice）。
//
// 缓存是进程级的：app 层实盘路径每次下单都会 NewBybitAdapter（见
// internal/app/context.go SubmitToExchange），实例级缓存会永远冷启动。
// instruments-info 是公开参考数据，与账户凭证无关，跨实例共享无泄漏风险。

const bybitInstrumentsTTL = time.Hour

// maxBybitInstrumentPages 游标分页防御上限：每页 1000 条，正常 1-2 页拉完。
const maxBybitInstrumentPages = 20

// bybitInstrumentCache 现货/合约双缓存，带 TTL 与互斥保护；刷新时持锁串行，
// 防止并发下单触发 instruments-info 惊群（每小时至多一次刷新）。
type bybitInstrumentCache struct {
	mu       sync.Mutex
	spot     map[string]*bybitSymbolFilters
	spotAt   time.Time
	linear   map[string]*bybitSymbolFilters
	linearAt time.Time
	ttl      time.Duration // 可变以便测试；生产恒为 bybitInstrumentsTTL
}

// bybitSymbolFilters 现货与 linear 合约共用同一过滤器结构（字段语义一致：
// 数量以币为单位，Bybit linear 永续的 qty 也是币数量而非张数）。
type bybitSymbolFilters struct {
	LotFilters
}

var sharedBybitInstruments = &bybitInstrumentCache{ttl: bybitInstrumentsTTL}

// resetBybitInstrumentCache 清空进程级缓存（测试用，避免用例间串数据）。
func resetBybitInstrumentCache() {
	sharedBybitInstruments.mu.Lock()
	defer sharedBybitInstruments.mu.Unlock()
	sharedBybitInstruments.spot, sharedBybitInstruments.linear = nil, nil
	sharedBybitInstruments.spotAt, sharedBybitInstruments.linearAt = time.Time{}, time.Time{}
	sharedBybitInstruments.ttl = bybitInstrumentsTTL
}

// get 返回 symbol 的规则；缓存新鲜则直接命中，过期则刷新。
// 刷新失败但有旧数据时降级用旧数据；完全没有数据时才返回 error，
// 由下单路径决定降级策略。
func (c *bybitInstrumentCache) get(symbol string, linear bool, fetch func() (map[string]*bybitSymbolFilters, error)) (*bybitSymbolFilters, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cached, at := c.spot, c.spotAt
	if linear {
		cached, at = c.linear, c.linearAt
	}
	if time.Since(at) < c.ttl && cached != nil {
		if f, ok := cached[symbol]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("symbol %s not found in cached instruments-info", symbol)
	}

	fresh, err := fetch()
	if err != nil {
		if f, ok := cached[symbol]; ok {
			log.Printf("[Bybit] WARN instruments-info refresh failed (%v), using stale filters for %s", err, symbol)
			return f, nil
		}
		return nil, err
	}
	if linear {
		c.linear, c.linearAt = fresh, time.Now()
	} else {
		c.spot, c.spotAt = fresh, time.Now()
	}
	if f, ok := fresh[symbol]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("symbol %s not found in instruments-info (%d symbols)", symbol, len(fresh))
}

// instrumentFilters 取交易对规则（带进程级缓存）。
func (b *BybitAdapter) instrumentFilters(symbol string, linear bool) (*bybitSymbolFilters, error) {
	category := "spot"
	if linear {
		category = "linear"
	}
	return sharedBybitInstruments.get(symbol, linear, func() (map[string]*bybitSymbolFilters, error) {
		return b.fetchInstrumentsInfo(category)
	})
}

// fetchInstrumentsInfo 拉取并解析某 category 的全量 instruments-info。
// 公开端点，无需签名；独立 10s 超时；按 nextPageCursor 翻页直至拉完。
func (b *BybitAdapter) fetchInstrumentsInfo(category string) (map[string]*bybitSymbolFilters, error) {
	merged := make(map[string]*bybitSymbolFilters)
	cursor := ""
	for page := 0; page < maxBybitInstrumentPages; page++ {
		u, _ := url.Parse(b.baseURL() + "/market/instruments-info")
		q := u.Query()
		q.Set("category", category)
		q.Set("limit", "1000")
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		u.RawQuery = q.Encode()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			cancel()
			return nil, err
		}
		resp, err := b.httpClient.Do(req)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("bybit %s instruments-info: %w", category, err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("bybit %s instruments-info read: %w", category, err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("bybit %s instruments-info: HTTP %d: %s", category, resp.StatusCode, truncateStr(string(body), 200))
		}

		parsed, next, err := parseBybitInstruments(body)
		if err != nil {
			return nil, fmt.Errorf("bybit %s instruments-info: %w", category, err)
		}
		for k, v := range parsed {
			merged[k] = v
		}
		if next == "" {
			if len(merged) == 0 {
				return nil, fmt.Errorf("bybit %s instruments-info: no instruments in response", category)
			}
			return merged, nil
		}
		cursor = next
	}
	return nil, fmt.Errorf("bybit %s instruments-info: pagination exceeded %d pages", category, maxBybitInstrumentPages)
}

// parseBybitInstruments 解析 instruments-info 单页响应，返回 symbol→规则与
// 下一页游标（空串表示最后一页）。字段缺失容错：现货用 lotSizeFilter.basePrecision
// 作数量步进、linear 用 qtyStep；minNotional 取 minNotionalValue（linear）或
// minOrderAmt（现货）。status 非 Trading 的 symbol 跳过（字段缺失则保留）。
func parseBybitInstruments(body []byte) (map[string]*bybitSymbolFilters, string, error) {
	var info struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				Symbol        string `json:"symbol"`
				Status        string `json:"status"`
				LotSizeFilter struct {
					BasePrecision    string `json:"basePrecision"`
					QtyStep          string `json:"qtyStep"`
					MinOrderQty      string `json:"minOrderQty"`
					MaxOrderQty      string `json:"maxOrderQty"`
					MinOrderAmt      string `json:"minOrderAmt"`
					MinNotionalValue string `json:"minNotionalValue"`
				} `json:"lotSizeFilter"`
				PriceFilter struct {
					TickSize string `json:"tickSize"`
					MinPrice string `json:"minPrice"`
					MaxPrice string `json:"maxPrice"`
				} `json:"priceFilter"`
			} `json:"list"`
			NextPageCursor string `json:"nextPageCursor"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, "", fmt.Errorf("parse instruments-info: %w", err)
	}
	if info.RetCode != 0 {
		return nil, "", fmt.Errorf("parse instruments-info: retCode=%d %s", info.RetCode, info.RetMsg)
	}

	out := make(map[string]*bybitSymbolFilters, len(info.Result.List))
	for _, s := range info.Result.List {
		if s.Symbol == "" {
			continue
		}
		if s.Status != "" && s.Status != "Trading" {
			continue
		}
		f := &bybitSymbolFilters{}
		f.StepSize = s.LotSizeFilter.QtyStep
		if f.StepSize == "" {
			f.StepSize = s.LotSizeFilter.BasePrecision
		}
		f.MinQty = s.LotSizeFilter.MinOrderQty
		f.MaxQty = s.LotSizeFilter.MaxOrderQty
		f.MinNotional = s.LotSizeFilter.MinNotionalValue
		if f.MinNotional == "" {
			f.MinNotional = s.LotSizeFilter.MinOrderAmt
		}
		f.TickSize = s.PriceFilter.TickSize
		f.MinPrice = s.PriceFilter.MinPrice
		f.MaxPrice = s.PriceFilter.MaxPrice
		out[s.Symbol] = f
	}
	return out, info.Result.NextPageCursor, nil
}

// normalizeBybitOrder 把策略给出的浮点 price/quantity 规整为符合交易所规则的
// 十进制下单串（与 normalizeBinanceOrder 同模式）。返回 error 时分两种：
//   - *OrderConstraintError：本地规则校验拒绝（调用方必须中止下单，发了也注定被拒）；
//   - 其他 error：规则不可用（网络/未知 symbol），函数内部已降级为旧格式串并记 WARN，
//     此时返回 nil error 与旧格式串，不阻塞下单（与修复前行为一致）。
func (b *BybitAdapter) normalizeBybitOrder(symbol, orderType string, price, quantity float64, linear bool) (qtyStr, priceStr string, err error) {
	market := "spot"
	if linear {
		market = "linear"
	}
	ctx := fmt.Sprintf("bybit %s %s", market, symbol)
	isLimit := strings.EqualFold(orderType, "LIMIT")

	f, ferr := b.instrumentFilters(symbol, linear)
	if ferr != nil {
		// 降级：规则拉不到时维持旧行为（硬编码 6/2 位小数），记 WARN 不阻塞。
		log.Printf("[Bybit] WARN instruments-info unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, ferr)
		qtyStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			priceStr = fmt.Sprintf("%.2f", price)
		}
		return qtyStr, priceStr, nil
	}

	qtyStr, qty, err := NormalizeQuantity(&f.LotFilters, quantity, ctx)
	if err != nil {
		return "", "", err
	}

	p := price
	if isLimit {
		priceStr, p, err = NormalizePrice(&f.LotFilters, price, ctx)
		if err != nil {
			return "", "", err
		}
	}

	if err := CheckMinNotional(&f.LotFilters, qty, p, ctx); err != nil {
		return "", "", err
	}
	return qtyStr, priceStr, nil
}
