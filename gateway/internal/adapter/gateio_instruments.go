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

// ── Gate.io 交易规则缓存 ────────────────────────────────────────────────────
//
// 实盘下单前的精度/限额规则来源（与 Binance exchangeInfo 缓存同模式）：
//   - 现货 GET /api/v4/spot/currency_pairs（amount_precision/price_precision
//     为小数位数，经 decimalsStep 折算成 step=10^-N；min_base_amount/
//     min_quote_amount 为最小币数量/最小计价金额）
//   - 合约 GET /api/v4/futures/usdt/contracts（quanto_multiplier=每张合约
//     对应的币数量、order_price_round=价格步进、order_size_min/max=张数范围）
//
// 单位语义：现货 amount 为币；合约 size 为张（contracts），
// 1 张 = quanto_multiplier 个币（如 BTC_USDT quanto_multiplier=0.0001 BTC/张）。
// 策略侧 quantity 恒为币数量，合约下单前换算成张数（整数）再校验。
//
// 缓存是进程级的：app 层实盘路径每次下单都会 NewGateIOAdapter（见
// internal/app/context.go SubmitToExchange），实例级缓存会永远冷启动。
// currency_pairs/contracts 是公开参考数据，与账户凭证无关，跨实例共享无泄漏风险。

const gateioInstrumentsTTL = time.Hour

// gateioInstrumentCache 现货/合约双缓存，带 TTL 与互斥保护；刷新时持锁串行，
// 防止并发下单触发规则拉取惊群（每小时至多一次刷新）。
type gateioInstrumentCache struct {
	mu       sync.Mutex
	spot     map[string]*LotFilters
	spotAt   time.Time
	contract map[string]*gateContractFilters
	contrAt  time.Time
	ttl      time.Duration // 可变以便测试；生产恒为 gateioInstrumentsTTL
}

// gateContractFilters 合约下单规则。LotFilters 的数量字段
// （StepSize/MinQty/MaxQty）单位是张（size），TickSize 是价格步进。
type gateContractFilters struct {
	LotFilters
	QuantoMultiplier string // 每张合约对应的币数量（quanto_multiplier，base coin/张）
}

var sharedGateIOInstruments = &gateioInstrumentCache{ttl: gateioInstrumentsTTL}

// resetGateIOInstrumentCache 清空进程级缓存（测试用，避免用例间串数据）。
func resetGateIOInstrumentCache() {
	sharedGateIOInstruments.mu.Lock()
	defer sharedGateIOInstruments.mu.Unlock()
	sharedGateIOInstruments.spot, sharedGateIOInstruments.contract = nil, nil
	sharedGateIOInstruments.spotAt, sharedGateIOInstruments.contrAt = time.Time{}, time.Time{}
	sharedGateIOInstruments.ttl = gateioInstrumentsTTL
}

// getSpot 返回现货交易对规则（key 形如 "BTC_USDT"）。
func (c *gateioInstrumentCache) getSpot(pair string, fetch func() (map[string]*LotFilters, error)) (*LotFilters, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.spotAt) < c.ttl && c.spot != nil {
		if f, ok := c.spot[pair]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("pair %s not found in cached currency_pairs", pair)
	}

	fresh, err := fetch()
	if err != nil {
		if f, ok := c.spot[pair]; ok {
			log.Printf("[GateIO] WARN currency_pairs refresh failed (%v), using stale filters for %s", err, pair)
			return f, nil
		}
		return nil, err
	}
	c.spot, c.spotAt = fresh, time.Now()
	if f, ok := fresh[pair]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("pair %s not found in currency_pairs (%d pairs)", pair, len(fresh))
}

// getContract 返回合约规则（key 形如 "BTC_USDT"）。
func (c *gateioInstrumentCache) getContract(name string, fetch func() (map[string]*gateContractFilters, error)) (*gateContractFilters, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.contrAt) < c.ttl && c.contract != nil {
		if f, ok := c.contract[name]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("contract %s not found in cached contracts", name)
	}

	fresh, err := fetch()
	if err != nil {
		if f, ok := c.contract[name]; ok {
			log.Printf("[GateIO] WARN contracts refresh failed (%v), using stale filters for %s", err, name)
			return f, nil
		}
		return nil, err
	}
	c.contract, c.contrAt = fresh, time.Now()
	if f, ok := fresh[name]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("contract %s not found in contracts (%d contracts)", name, len(fresh))
}

// spotRules 取现货交易对规则（带进程级缓存）。
func (g *GateIOAdapter) spotRules(pair string) (*LotFilters, error) {
	return sharedGateIOInstruments.getSpot(pair, func() (map[string]*LotFilters, error) {
		return g.fetchCurrencyPairs()
	})
}

// contractRules 取 USDT 永续合约规则（带进程级缓存）。
func (g *GateIOAdapter) contractRules(name string) (*gateContractFilters, error) {
	return sharedGateIOInstruments.getContract(name, func() (map[string]*gateContractFilters, error) {
		return g.fetchFuturesContracts()
	})
}

// publicGet 拉取公开参考数据端点（无需签名）；独立 10s 超时，
// 避免元数据拉取拖垮下单延迟。
func (g *GateIOAdapter) publicGet(path, label string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", g.restURL()+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gateio %s: %w", label, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("gateio %s read: %w", label, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateio %s: HTTP %d: %s", label, resp.StatusCode, truncateStr(string(body), 200))
	}
	return body, nil
}

func (g *GateIOAdapter) fetchCurrencyPairs() (map[string]*LotFilters, error) {
	body, err := g.publicGet("/spot/currency_pairs", "currency_pairs")
	if err != nil {
		return nil, err
	}
	return parseGateIOCurrencyPairs(body)
}

func (g *GateIOAdapter) fetchFuturesContracts() (map[string]*gateContractFilters, error) {
	body, err := g.publicGet("/futures/usdt/contracts", "futures contracts")
	if err != nil {
		return nil, err
	}
	return parseGateIOFuturesContracts(body)
}

// parseGateIOCurrencyPairs 解析现货 currency_pairs（顶层是裸数组）。
// amount_precision/price_precision 是小数位数，经 decimalsStep 折算成 step；
// 字段缺失容错；trade_status 非 tradable 的条目跳过（字段缺失则保留）。
func parseGateIOCurrencyPairs(body []byte) (map[string]*LotFilters, error) {
	var pairs []struct {
		ID              string          `json:"id"`   // "BTC_USDT"
		Base            string          `json:"base"` // "BTC"
		Quote           string          `json:"quote"`
		TradeStatus     string          `json:"trade_status"`
		AmountPrecision json.RawMessage `json:"amount_precision"`
		PricePrecision  json.RawMessage `json:"price_precision"`
		MinBaseAmount   string          `json:"min_base_amount"`
		MinQuoteAmount  string          `json:"min_quote_amount"`
	}
	if err := json.Unmarshal(body, &pairs); err != nil {
		return nil, fmt.Errorf("parse currency_pairs: %w", err)
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("parse currency_pairs: no pairs in response")
	}

	out := make(map[string]*LotFilters, len(pairs))
	for _, p := range pairs {
		if p.ID == "" {
			continue
		}
		if p.TradeStatus != "" && p.TradeStatus != "tradable" {
			continue
		}
		f := &LotFilters{}
		if n, err := strconv.Atoi(rawNumber(p.AmountPrecision)); err == nil && rawNumber(p.AmountPrecision) != "" {
			f.StepSize = decimalsStep(n)
		}
		if n, err := strconv.Atoi(rawNumber(p.PricePrecision)); err == nil && rawNumber(p.PricePrecision) != "" {
			f.TickSize = decimalsStep(n)
		}
		f.MinQty = p.MinBaseAmount
		f.MinNotional = p.MinQuoteAmount
		out[p.ID] = f
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("parse currency_pairs: no tradable pairs in response")
	}
	return out, nil
}

// parseGateIOFuturesContracts 解析 USDT 永续 contracts（顶层是裸数组）。
// order_size_min/max 是整数张数（JSON 数字，RawMessage 原样取出避免类型假设）；
// order_price_round 是价格步进十进制串。字段缺失容错。
func parseGateIOFuturesContracts(body []byte) (map[string]*gateContractFilters, error) {
	var contracts []struct {
		Name             string          `json:"name"` // "BTC_USDT"
		QuantoMultiplier string          `json:"quanto_multiplier"`
		OrderPriceRound  string          `json:"order_price_round"`
		OrderSizeMin     json.RawMessage `json:"order_size_min"`
		OrderSizeMax     json.RawMessage `json:"order_size_max"`
	}
	if err := json.Unmarshal(body, &contracts); err != nil {
		return nil, fmt.Errorf("parse futures contracts: %w", err)
	}
	if len(contracts) == 0 {
		return nil, fmt.Errorf("parse futures contracts: no contracts in response")
	}

	out := make(map[string]*gateContractFilters, len(contracts))
	for _, c := range contracts {
		if c.Name == "" {
			continue
		}
		f := &gateContractFilters{QuantoMultiplier: c.QuantoMultiplier}
		f.StepSize = "1" // Gate 合约张数为整数
		f.TickSize = c.OrderPriceRound
		f.MinQty = rawNumber(c.OrderSizeMin)
		f.MaxQty = rawNumber(c.OrderSizeMax)
		out[c.Name] = f
	}
	return out, nil
}

// gateCoinToContracts 把币数量换算为合约张数：contracts = coinQty / quantoMultiplier。
// 单位：coinQty 为币（base coin，如 BTC），返回值为张（size），
// 1 张 = quantoMultiplier 个币。big.Rat 精确除法，杜绝浮点尾差
// （残余尾差由 FloorToStep 的 1e-9 步进容差吸收）。
func gateCoinToContracts(coinQty float64, quantoMultiplier string) (float64, error) {
	qm, err := parseDecimal(quantoMultiplier)
	if err != nil || qm.Sign() <= 0 {
		return 0, fmt.Errorf("invalid quanto_multiplier %q", quantoMultiplier)
	}
	q, ok := new(big.Rat).SetString(strconv.FormatFloat(coinQty, 'f', -1, 64))
	if !ok {
		return 0, fmt.Errorf("invalid quantity %v", coinQty)
	}
	contracts, _ := new(big.Rat).Quo(q, qm).Float64()
	return contracts, nil
}

// normalizeGateSpotOrder 规整现货下单（amount 单位=币）。
// 语义同 normalizeBinanceOrder：约束违反返回 *OrderConstraintError（调用方必须
// 中止下单）；规则不可用时降级为旧格式串（%.6f/%.2f）并记 WARN，不阻塞下单。
func (g *GateIOAdapter) normalizeGateSpotOrder(pair, orderType string, price, quantity float64) (amountStr, priceStr string, err error) {
	ctx := fmt.Sprintf("gateio spot %s", pair)
	isLimit := strings.EqualFold(orderType, "limit")

	f, ferr := g.spotRules(pair)
	if ferr != nil {
		log.Printf("[GateIO] WARN currency_pairs unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, ferr)
		amountStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			priceStr = fmt.Sprintf("%.2f", price)
		}
		return amountStr, priceStr, nil
	}

	amountStr, qty, err := NormalizeQuantity(f, quantity, ctx)
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
	return amountStr, priceStr, nil
}

// normalizeGateFuturesOrder 规整 USDT 永续合约下单：币数量 ÷ quanto_multiplier
// 得到张数，张数向下取整为整数后校验 order_size_min/max；取整后不足
// order_size_min（含不足一张 floor 到 0）明确报错。
// 规则不可用时降级旧格式串（%.6f/%.2f）并记 WARN，不阻塞下单。
func (g *GateIOAdapter) normalizeGateFuturesOrder(contract, orderType string, price, quantity float64) (sizeStr, priceStr string, err error) {
	ctx := fmt.Sprintf("gateio futures %s", contract)
	isLimit := strings.EqualFold(orderType, "limit")

	f, ferr := g.contractRules(contract)
	if ferr != nil || f.QuantoMultiplier == "" {
		if ferr == nil {
			ferr = fmt.Errorf("contract %s has no quanto_multiplier", contract)
		}
		log.Printf("[GateIO] WARN contracts unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, ferr)
		sizeStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			priceStr = fmt.Sprintf("%.2f", price)
		}
		return sizeStr, priceStr, nil
	}

	// 币 → 张换算（单位见 gateCoinToContracts）。
	contracts, cerr := gateCoinToContracts(quantity, f.QuantoMultiplier)
	if cerr != nil {
		log.Printf("[GateIO] WARN coin→contract conversion failed for %s (%v); falling back to legacy %%.6f formatting", ctx, cerr)
		sizeStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			priceStr = fmt.Sprintf("%.2f", price)
		}
		return sizeStr, priceStr, nil
	}

	// 张数取整 + order_size_min/max 校验（f.LotFilters 的数量字段在张数语境下）。
	sizeCtx := fmt.Sprintf("%s size(contracts, quanto_multiplier=%s)", ctx, f.QuantoMultiplier)
	sizeStr, _, err = NormalizeQuantity(&f.LotFilters, contracts, sizeCtx)
	if err != nil {
		// 错误信息同时带上币数量（策略侧仓位单位），便于定位换算问题。
		return "", "", fmt.Errorf("%w (raw coin quantity %s)", err, trimFloat(quantity))
	}

	if isLimit {
		priceStr, _, err = NormalizePrice(&f.LotFilters, price, ctx)
		if err != nil {
			return "", "", err
		}
	}
	return sizeStr, priceStr, nil
}
