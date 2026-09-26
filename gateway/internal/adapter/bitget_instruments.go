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

// ── Bitget symbols 交易规则缓存 ─────────────────────────────────────────────
//
// 实盘下单前的精度/限额规则来源（与 Binance exchangeInfo 缓存同模式）：
//   - 现货 GET /api/v2/spot/public/symbols
//   - 合约 GET /api/v2/mix/market/symbols?productType=USDT-FUTURES
//
// 与 Binance/Bybit 不同：Bitget 不给 step/tick 十进制串，而给小数位数
// （现货 quantityPlace/pricePlace，合约 sizePlace/pricePlace），解析时经
// decimalsStep 折算成 step=10^-N 复用同一 big.Rat 取整核心；
// 合约价格步进还要乘 priceEndStep。数量限制：minTradeAmount（现货）/
// minTradeNum（合约），名义价值下限 minTradeUSDT。
//
// 缓存是进程级的：app 层实盘路径每次下单都会 NewBitgetAdapter（见
// internal/app/context.go SubmitToExchange），实例级缓存会永远冷启动。
// symbols 是公开参考数据，与账户凭证无关，跨实例共享无泄漏风险。

const bitgetInstrumentsTTL = time.Hour

// bitgetInstrumentCache 现货/合约双缓存，带 TTL 与互斥保护；刷新时持锁串行，
// 防止并发下单触发 symbols 惊群（每小时至多一次刷新）。
type bitgetInstrumentCache struct {
	mu     sync.Mutex
	spot   map[string]*LotFilters
	spotAt time.Time
	mix    map[string]*LotFilters
	mixAt  time.Time
	ttl    time.Duration // 可变以便测试；生产恒为 bitgetInstrumentsTTL
}

var sharedBitgetInstruments = &bitgetInstrumentCache{ttl: bitgetInstrumentsTTL}

// resetBitgetInstrumentCache 清空进程级缓存（测试用，避免用例间串数据）。
func resetBitgetInstrumentCache() {
	sharedBitgetInstruments.mu.Lock()
	defer sharedBitgetInstruments.mu.Unlock()
	sharedBitgetInstruments.spot, sharedBitgetInstruments.mix = nil, nil
	sharedBitgetInstruments.spotAt, sharedBitgetInstruments.mixAt = time.Time{}, time.Time{}
	sharedBitgetInstruments.ttl = bitgetInstrumentsTTL
}

// get 返回 symbol 的规则；缓存新鲜则直接命中，过期则刷新。
// 刷新失败但有旧数据时降级用旧数据；完全没有数据时才返回 error。
func (c *bitgetInstrumentCache) get(symbol string, mix bool, fetch func() (map[string]*LotFilters, error)) (*LotFilters, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cached, at := c.spot, c.spotAt
	if mix {
		cached, at = c.mix, c.mixAt
	}
	if time.Since(at) < c.ttl && cached != nil {
		if f, ok := cached[symbol]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("symbol %s not found in cached symbols", symbol)
	}

	fresh, err := fetch()
	if err != nil {
		if f, ok := cached[symbol]; ok {
			log.Printf("[Bitget] WARN symbols refresh failed (%v), using stale filters for %s", err, symbol)
			return f, nil
		}
		return nil, err
	}
	if mix {
		c.mix, c.mixAt = fresh, time.Now()
	} else {
		c.spot, c.spotAt = fresh, time.Now()
	}
	if f, ok := fresh[symbol]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("symbol %s not found in symbols (%d symbols)", symbol, len(fresh))
}

// spotRules 取现货交易对规则（带进程级缓存）。
func (b *BitgetAdapter) spotRules(symbol string) (*LotFilters, error) {
	return sharedBitgetInstruments.get(symbol, false, func() (map[string]*LotFilters, error) {
		return b.fetchSymbols("/spot/public/symbols", "")
	})
}

// mixRules 取 USDT 本位合约规则（带进程级缓存）。
func (b *BitgetAdapter) mixRules(symbol string) (*LotFilters, error) {
	return sharedBitgetInstruments.get(symbol, true, func() (map[string]*LotFilters, error) {
		return b.fetchSymbols("/mix/market/symbols", "USDT-FUTURES")
	})
}

// fetchSymbols 拉取并解析 spot/mix symbols。公开端点，无需签名；独立 10s 超时。
func (b *BitgetAdapter) fetchSymbols(path, productType string) (map[string]*LotFilters, error) {
	url := b.restURL() + path
	if productType != "" {
		url += "?productType=" + productType
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bitget %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("bitget %s read: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitget %s: HTTP %d: %s", path, resp.StatusCode, truncateStr(string(body), 200))
	}
	m, err := parseBitgetSymbols(body)
	if err != nil {
		return nil, fmt.Errorf("bitget %s: %w", path, err)
	}
	return m, nil
}

// parseBitgetSymbols 解析 spot/mix symbols 响应（两接口同构，字段别名兼容）。
// 小数位数模式：quantityPlace/sizePlace → 数量步进 10^-N；pricePlace →
// 价格步进 10^-N × priceEndStep（合约，缺省 1）。字段缺失容错；
// status 非 online/normal 的条目跳过（字段缺失则保留）。
func parseBitgetSymbols(body []byte) (map[string]*LotFilters, error) {
	var resp struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol         string          `json:"symbol"`
			RawStatus      json.RawMessage `json:"status"`
			QuantityPlace  json.RawMessage `json:"quantityPlace"` // 现货数量小数位
			SizePlace      json.RawMessage `json:"sizePlace"`     // 合约数量小数位
			PricePlace     json.RawMessage `json:"pricePlace"`
			PriceEndStep   json.RawMessage `json:"priceEndStep"` // 合约价格步进倍数（缺省 1）
			MinTradeAmount string          `json:"minTradeAmount"` // 现货最小数量
			MaxTradeAmount string          `json:"maxTradeAmount"`
			MinTradeNum    string          `json:"minTradeNum"` // 合约最小数量
			MinTradeUSDT   string          `json:"minTradeUSDT"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse symbols: %w", err)
	}
	if resp.Code != "" && resp.Code != "00000" {
		return nil, fmt.Errorf("parse symbols: code=%s %s", resp.Code, resp.Msg)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("parse symbols: no data in response")
	}

	out := make(map[string]*LotFilters, len(resp.Data))
	for _, s := range resp.Data {
		if s.Symbol == "" {
			continue
		}
		if st := strings.ToLower(rawNumber(s.RawStatus)); st != "" && st != "online" && st != "normal" {
			continue
		}
		f := &LotFilters{}
		// 数量步进：quantityPlace（现货）或 sizePlace（合约）小数位 → 10^-N。
		sizePlace := rawNumber(s.QuantityPlace)
		if sizePlace == "" {
			sizePlace = rawNumber(s.SizePlace)
		}
		if n, err := strconv.Atoi(sizePlace); err == nil && sizePlace != "" {
			f.StepSize = decimalsStep(n)
		}
		// 价格步进：10^-pricePlace × priceEndStep（big.Rat 精确相乘）。
		if n, err := strconv.Atoi(rawNumber(s.PricePlace)); err == nil && rawNumber(s.PricePlace) != "" {
			tick, _ := new(big.Rat).SetString(decimalsStep(n))
			if es, err := parseDecimal(rawNumber(s.PriceEndStep)); err == nil && es.Sign() > 0 {
				tick = new(big.Rat).Mul(tick, es)
			}
			f.TickSize = tick.FloatString(n)
		}
		f.MinQty = s.MinTradeAmount
		if f.MinQty == "" {
			f.MinQty = s.MinTradeNum
		}
		f.MaxQty = s.MaxTradeAmount
		f.MinNotional = s.MinTradeUSDT
		out[s.Symbol] = f
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("parse symbols: no online symbols in response")
	}
	return out, nil
}

// normalizeBitgetOrder 规整下单（现货与 USDT-FUTURES 合约共用：Bitget 合约
// size 单位也是币而非张，无需张数换算）。语义同 normalizeBinanceOrder：
// 约束违反返回 *OrderConstraintError（调用方必须中止下单）；规则不可用时降级
// 为旧格式串（%.6f/%.2f）并记 WARN，不阻塞下单。
func (b *BitgetAdapter) normalizeBitgetOrder(symbol, orderType string, price, quantity float64, mix bool) (qtyStr, priceStr string, err error) {
	market := "spot"
	if mix {
		market = "mix"
	}
	ctx := fmt.Sprintf("bitget %s %s", market, symbol)
	isLimit := strings.EqualFold(orderType, "limit")

	var f *LotFilters
	var ferr error
	if mix {
		f, ferr = b.mixRules(symbol)
	} else {
		f, ferr = b.spotRules(symbol)
	}
	if ferr != nil {
		log.Printf("[Bitget] WARN symbols unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, ferr)
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
