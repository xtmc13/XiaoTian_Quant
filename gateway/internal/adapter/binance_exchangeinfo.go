package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ── Binance exchangeInfo 交易规则缓存 ──────────────────────────────────────
//
// 实盘下单前的精度/限额规则来源：
//   - 现货 GET /api/v3/exchangeInfo
//   - 合约 GET /fapi/v1/exchangeInfo
// 解析每个 symbol 的 LOT_SIZE（min/maxQty/stepSize）、PRICE_FILTER（tickSize/
// min/maxPrice）、MIN_NOTIONAL/NOTIONAL（minNotional）与 MARKET_LOT_SIZE。
//
// 缓存是进程级的：app 层实盘路径每次下单都会 NewBinanceAdapter（见
// internal/app/context.go SubmitToExchange），实例级缓存会永远冷启动。
// exchangeInfo 是公开参考数据，与账户凭证无关，跨实例共享无泄漏风险。

const binanceExchangeInfoTTL = time.Hour

// binanceFilterCache 现货/合约双缓存，带 TTL 与互斥保护；刷新时持锁串行，
// 防止并发下单触发 exchangeInfo 惊群（每小时至多一次刷新）。
type binanceFilterCache struct {
	mu      sync.Mutex
	spot    map[string]*binanceSymbolFilters
	spotAt  time.Time
	futures map[string]*binanceSymbolFilters
	futAt   time.Time
	ttl     time.Duration // 可变以便测试；生产恒为 binanceExchangeInfoTTL
}

// binanceSymbolFilters 在通用 LotFilters 之外附带 MARKET_LOT_SIZE
// （市价单数量步进，Binance 部分交易对市价单步进与限价单不同）。
type binanceSymbolFilters struct {
	LotFilters
	marketStepSize string
	marketMinQty   string
}

var sharedBinanceFilters = &binanceFilterCache{ttl: binanceExchangeInfoTTL}

// resetBinanceFilterCache 清空进程级缓存（测试用，避免用例间串数据）。
func resetBinanceFilterCache() {
	sharedBinanceFilters.mu.Lock()
	defer sharedBinanceFilters.mu.Unlock()
	sharedBinanceFilters.spot, sharedBinanceFilters.futures = nil, nil
	sharedBinanceFilters.spotAt, sharedBinanceFilters.futAt = time.Time{}, time.Time{}
	sharedBinanceFilters.ttl = binanceExchangeInfoTTL
}

// get 返回 symbol 的规则；缓存新鲜则直接命中，过期则刷新。
// 刷新失败但有旧数据时降级用旧数据（交易所规则变化以小时/天计，旧规则
// 大概率仍有效）；完全没有数据时才返回 error，由下单路径决定降级策略。
func (c *binanceFilterCache) get(symbol string, futures bool, fetch func() (map[string]*binanceSymbolFilters, error)) (*binanceSymbolFilters, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cached, at := c.spot, c.spotAt
	if futures {
		cached, at = c.futures, c.futAt
	}
	if time.Since(at) < c.ttl && cached != nil {
		if f, ok := cached[symbol]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("symbol %s not found in cached exchangeInfo", symbol)
	}

	fresh, err := fetch()
	if err != nil {
		if f, ok := cached[symbol]; ok {
			log.Printf("[Binance] WARN exchangeInfo refresh failed (%v), using stale filters for %s", err, symbol)
			return f, nil
		}
		return nil, err
	}
	if futures {
		c.futures, c.futAt = fresh, time.Now()
	} else {
		c.spot, c.spotAt = fresh, time.Now()
	}
	if f, ok := fresh[symbol]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("symbol %s not found in exchangeInfo (%d symbols)", symbol, len(fresh))
}

// symbolFilters 取交易对规则（带进程级缓存）。
func (b *BinanceAdapter) symbolFilters(symbol string, futures bool) (*binanceSymbolFilters, error) {
	return sharedBinanceFilters.get(symbol, futures, func() (map[string]*binanceSymbolFilters, error) {
		return b.fetchExchangeInfo(futures)
	})
}

// fetchExchangeInfo 拉取并解析 exchangeInfo 全量交易对规则。
// 公开端点，无需签名；独立 10s 超时，避免元数据拉取拖垮下单延迟。
func (b *BinanceAdapter) fetchExchangeInfo(futures bool) (map[string]*binanceSymbolFilters, error) {
	url := b.baseURL() + "/exchangeInfo"
	market := "spot"
	if futures {
		url = b.fapiBaseURL() + "/fapi/v1/exchangeInfo"
		market = "futures"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("binance %s exchangeInfo: %w", market, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("binance %s exchangeInfo read: %w", market, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance %s exchangeInfo: HTTP %d: %s", market, resp.StatusCode, truncateStr(string(body), 200))
	}
	return parseBinanceExchangeInfo(body)
}

// parseBinanceExchangeInfo 解析现货/合约 exchangeInfo 响应。
// 两种响应的 filters 结构一致；minNotional 现货在 MIN_NOTIONAL.minNotional，
// 合约旧版在 MIN_NOTIONAL.notional，新版在 NOTIONAL.minNotional，三种都认。
func parseBinanceExchangeInfo(body []byte) (map[string]*binanceSymbolFilters, error) {
	var info struct {
		Symbols []struct {
			Symbol  string `json:"symbol"`
			Filters []struct {
				FilterType  string `json:"filterType"`
				MinQty      string `json:"minQty"`
				MaxQty      string `json:"maxQty"`
				StepSize    string `json:"stepSize"`
				MinPrice    string `json:"minPrice"`
				MaxPrice    string `json:"maxPrice"`
				TickSize    string `json:"tickSize"`
				MinNotional string `json:"minNotional"`
				Notional    string `json:"notional"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse exchangeInfo: %w", err)
	}
	if len(info.Symbols) == 0 {
		return nil, fmt.Errorf("parse exchangeInfo: no symbols in response")
	}

	out := make(map[string]*binanceSymbolFilters, len(info.Symbols))
	for _, s := range info.Symbols {
		if s.Symbol == "" {
			continue
		}
		f := &binanceSymbolFilters{}
		for _, fl := range s.Filters {
			switch fl.FilterType {
			case "LOT_SIZE":
				f.StepSize, f.MinQty, f.MaxQty = fl.StepSize, fl.MinQty, fl.MaxQty
			case "MARKET_LOT_SIZE":
				f.marketStepSize, f.marketMinQty = fl.StepSize, fl.MinQty
			case "PRICE_FILTER":
				f.TickSize, f.MinPrice, f.MaxPrice = fl.TickSize, fl.MinPrice, fl.MaxPrice
			case "MIN_NOTIONAL":
				// 现货叫 minNotional；合约旧版该过滤器字段名是 notional
				if fl.MinNotional != "" {
					f.MinNotional = fl.MinNotional
				} else {
					f.MinNotional = fl.Notional
				}
			case "NOTIONAL":
				if fl.MinNotional != "" {
					f.MinNotional = fl.MinNotional
				}
			}
		}
		out[s.Symbol] = f
	}
	return out, nil
}

// normalizeBinanceOrder 把策略给出的浮点 price/quantity 规整为符合交易所规则的
// 十进制下单串。返回 error 时分两种：
//   - *OrderConstraintError：本地规则校验拒绝（调用方必须中止下单，发了也注定被拒）；
//   - 其他 error：规则不可用（网络/未知 symbol），函数内部已降级为旧格式串并记 WARN，
//     此时返回 nil error 与旧格式串，不阻塞下单（与修复前行为一致）。
func (b *BinanceAdapter) normalizeBinanceOrder(symbol, orderType string, price, quantity float64, futures bool) (qtyStr, priceStr string, err error) {
	market := "spot"
	if futures {
		market = "futures"
	}
	ctx := fmt.Sprintf("binance %s %s", market, symbol)
	isLimit := strings.EqualFold(orderType, "LIMIT")

	f, ferr := b.symbolFilters(symbol, futures)
	if ferr != nil {
		// 降级：规则拉不到时维持旧行为（硬编码 6/2 位小数），记 WARN 不阻塞。
		log.Printf("[Binance] WARN exchangeInfo unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, ferr)
		qtyStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			priceStr = fmt.Sprintf("%.2f", price)
		}
		return qtyStr, priceStr, nil
	}

	// 市价单优先用 MARKET_LOT_SIZE 的步进（若交易所单独给出）。
	qtyFilters := f.LotFilters
	if !isLimit && f.marketStepSize != "" {
		qtyFilters.StepSize = f.marketStepSize
		if f.marketMinQty != "" {
			qtyFilters.MinQty = f.marketMinQty
		}
	}

	qtyStr, qty, err := NormalizeQuantity(&qtyFilters, quantity, ctx)
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
