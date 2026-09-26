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

// ── MEXC 交易规则缓存 ───────────────────────────────────────────────────────
//
// 实盘下单前的精度/限额规则来源（与 Binance exchangeInfo 缓存同模式）：
//   - 现货 GET /api/v3/exchangeInfo（schema 类 Binance 但字段名不同：
//     baseSizePrecision=数量步进、quoteAmountPrecision=最小名义价值、
//     quotePrecision=价格小数位；部分版本附 Binance 风格 filters 数组，两种都认）
//   - 合约 GET /api/v1/contract/detail（contractSize=每张合约的币数量、
//     volUnit/volPrecision=张数步进、minVol/maxVol=张数范围、
//     priceUnit/priceScale=价格步进/位数）
//
// 单位语义：现货 quantity 为币；合约 vol 为张（contracts），
// 1 张 = contractSize 个币（如 BTC_USDT contractSize=0.0001 BTC/张）。
// 策略侧 quantity 恒为币数量，下单前换算成张数再取整。
//
// 缓存是进程级的：app 层实盘路径每次下单都会 NewMEXCAdapter（见
// internal/app/context.go SubmitToExchange），实例级缓存会永远冷启动。
// exchangeInfo/contract detail 是公开参考数据，与账户凭证无关，跨实例共享无泄漏风险。

const mexcInstrumentsTTL = time.Hour

// mexcInstrumentCache 现货/合约双缓存，带 TTL 与互斥保护；刷新时持锁串行，
// 防止并发下单触发规则拉取惊群（每小时至多一次刷新）。
type mexcInstrumentCache struct {
	mu         sync.Mutex
	spot       map[string]*LotFilters
	spotAt     time.Time
	contract   map[string]*mexcContractFilters
	contractAt time.Time
	ttl        time.Duration // 可变以便测试；生产恒为 mexcInstrumentsTTL
}

// mexcContractFilters 合约下单规则。LotFilters 的数量字段
// （StepSize/MinQty/MaxQty）单位是张（vol），TickSize 是价格步进。
type mexcContractFilters struct {
	LotFilters
	ContractSize string // 每张合约对应的币数量（contractSize，base coin/张）
}

var sharedMEXCInstruments = &mexcInstrumentCache{ttl: mexcInstrumentsTTL}

// resetMEXCInstrumentCache 清空进程级缓存（测试用，避免用例间串数据）。
func resetMEXCInstrumentCache() {
	sharedMEXCInstruments.mu.Lock()
	defer sharedMEXCInstruments.mu.Unlock()
	sharedMEXCInstruments.spot, sharedMEXCInstruments.contract = nil, nil
	sharedMEXCInstruments.spotAt, sharedMEXCInstruments.contractAt = time.Time{}, time.Time{}
	sharedMEXCInstruments.ttl = mexcInstrumentsTTL
}

// spotRules 取现货交易对规则（带进程级缓存）。
func (mx *MEXCAdapter) spotRules(symbol string) (*LotFilters, error) {
	c := sharedMEXCInstruments
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.spotAt) < c.ttl && c.spot != nil {
		if f, ok := c.spot[symbol]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("symbol %s not found in cached exchangeInfo", symbol)
	}

	fresh, err := mx.fetchSpotExchangeInfo()
	if err != nil {
		if f, ok := c.spot[symbol]; ok {
			log.Printf("[MEXC] WARN exchangeInfo refresh failed (%v), using stale filters for %s", err, symbol)
			return f, nil
		}
		return nil, err
	}
	c.spot, c.spotAt = fresh, time.Now()
	if f, ok := fresh[symbol]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("symbol %s not found in exchangeInfo (%d symbols)", symbol, len(fresh))
}

// contractRules 取合约规则（带进程级缓存；key 形如 "BTC_USDT"）。
func (mx *MEXCAdapter) contractRules(symbol string) (*mexcContractFilters, error) {
	c := sharedMEXCInstruments
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Since(c.contractAt) < c.ttl && c.contract != nil {
		if f, ok := c.contract[symbol]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("contract %s not found in cached contract/detail", symbol)
	}

	fresh, err := mx.fetchContractDetail()
	if err != nil {
		if f, ok := c.contract[symbol]; ok {
			log.Printf("[MEXC] WARN contract/detail refresh failed (%v), using stale filters for %s", err, symbol)
			return f, nil
		}
		return nil, err
	}
	c.contract, c.contractAt = fresh, time.Now()
	if f, ok := fresh[symbol]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("contract %s not found in contract/detail (%d contracts)", symbol, len(fresh))
}

// fetchSpotExchangeInfo 拉取并解析现货 exchangeInfo。
// 公开端点，无需签名；独立 10s 超时，避免元数据拉取拖垮下单延迟。
func (mx *MEXCAdapter) fetchSpotExchangeInfo() (map[string]*LotFilters, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", mx.restURL()+"/exchangeInfo", nil)
	if err != nil {
		return nil, err
	}
	resp, err := mx.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mexc spot exchangeInfo: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("mexc spot exchangeInfo read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mexc spot exchangeInfo: HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	return parseMEXCExchangeInfo(body)
}

// fetchContractDetail 拉取并解析合约 contract/detail（公开端点，无需签名）。
func (mx *MEXCAdapter) fetchContractDetail() (map[string]*mexcContractFilters, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", mx.contractBaseURL()+"/api/v1/contract/detail", nil)
	if err != nil {
		return nil, err
	}
	resp, err := mx.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mexc contract/detail: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("mexc contract/detail read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mexc contract/detail: HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	return parseMEXCContractDetail(body)
}

// parseMEXCExchangeInfo 解析现货 exchangeInfo。schema 类 Binance 但字段名不同；
// 字段缺失容错：平铺字段优先，Binance 风格 filters 数组兜底补空缺。
func parseMEXCExchangeInfo(body []byte) (map[string]*LotFilters, error) {
	var info struct {
		Symbols []struct {
			Symbol string `json:"symbol"`
			// 平铺字段（MEXC 现行 v3 schema）
			BaseSizePrecision    string      `json:"baseSizePrecision"`    // 数量步进（如 "0.0001"）
			QuantityPrecision    json.Number `json:"quantityPrecision"`    // 数量小数位（无前者时用）
			QuoteAmountPrecision string      `json:"quoteAmountPrecision"` // 最小名义价值（如 "5"）
			MinQuoteAmount       string      `json:"minQuoteAmount"`       // 最小名义价值的另一字段名
			TickSize             string      `json:"tickSize"`
			QuotePrecision       json.Number `json:"quotePrecision"` // 价格小数位（无 tickSize 时用）
			// Binance 风格 filters 数组兜底（部分版本/网关返回）
			Filters []struct {
				FilterType  string `json:"filterType"`
				MinQty      string `json:"minQty"`
				MaxQty      string `json:"maxQty"`
				StepSize    string `json:"stepSize"`
				TickSize    string `json:"tickSize"`
				MinNotional string `json:"minNotional"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse exchangeInfo: %w", err)
	}
	if len(info.Symbols) == 0 {
		return nil, fmt.Errorf("parse exchangeInfo: no symbols in response")
	}

	out := make(map[string]*LotFilters, len(info.Symbols))
	for _, s := range info.Symbols {
		if s.Symbol == "" {
			continue
		}
		f := &LotFilters{}
		f.StepSize = s.BaseSizePrecision
		if f.StepSize == "" {
			if n, err := strconv.Atoi(s.QuantityPrecision.String()); err == nil && s.QuantityPrecision.String() != "" {
				f.StepSize = decimalsStep(n)
			}
		}
		f.TickSize = s.TickSize
		if f.TickSize == "" {
			if n, err := strconv.Atoi(s.QuotePrecision.String()); err == nil && s.QuotePrecision.String() != "" {
				f.TickSize = decimalsStep(n)
			}
		}
		f.MinNotional = s.QuoteAmountPrecision
		if f.MinNotional == "" {
			f.MinNotional = s.MinQuoteAmount
		}
		for _, fl := range s.Filters {
			switch fl.FilterType {
			case "LOT_SIZE":
				if f.StepSize == "" {
					f.StepSize = fl.StepSize
				}
				if f.MinQty == "" {
					f.MinQty = fl.MinQty
				}
				if f.MaxQty == "" {
					f.MaxQty = fl.MaxQty
				}
			case "PRICE_FILTER":
				if f.TickSize == "" {
					f.TickSize = fl.TickSize
				}
			case "MIN_NOTIONAL", "NOTIONAL":
				if f.MinNotional == "" {
					f.MinNotional = fl.MinNotional
				}
			}
		}
		out[s.Symbol] = f
	}
	return out, nil
}

// rawNumber 把 JSON token（数字或带引号数字串）还原为十进制串，容错字段类型。
func rawNumber(raw json.RawMessage) string {
	return strings.Trim(strings.TrimSpace(string(raw)), `"`)
}

// parseMEXCContractDetail 解析合约 contract/detail。contractSize/volUnit 等字段
// 是 JSON 数字而非字符串，用 RawMessage 原样取出避免 float64 尾差进入规则。
// state 非 0（启用）的合约跳过（字段缺失则保留）。
func parseMEXCContractDetail(body []byte) (map[string]*mexcContractFilters, error) {
	var resp struct {
		Success bool            `json:"success"`
		Code    json.RawMessage `json:"code"`
		Data    []struct {
			Symbol       string          `json:"symbol"`        // "BTC_USDT"
			ContractSize json.RawMessage `json:"contractSize"`  // 每张=多少币
			VolUnit      json.RawMessage `json:"volUnit"`       // 张数步进
			VolPrecision json.RawMessage `json:"volPrecision"`  // 张数小数位（无 volUnit 时用）
			MinVol       json.RawMessage `json:"minVol"`
			MaxVol       json.RawMessage `json:"maxVol"`
			PriceUnit    json.RawMessage `json:"priceUnit"`   // 价格步进
			PriceScale   json.RawMessage `json:"priceScale"`  // 价格小数位（无 priceUnit 时用）
			State        json.RawMessage `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse contract/detail: %w", err)
	}
	if code := rawNumber(resp.Code); code != "" && code != "0" {
		return nil, fmt.Errorf("parse contract/detail: code=%s", code)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("parse contract/detail: no data in response")
	}

	out := make(map[string]*mexcContractFilters, len(resp.Data))
	for _, c := range resp.Data {
		if c.Symbol == "" {
			continue
		}
		if st := rawNumber(c.State); st != "" && st != "0" {
			continue
		}
		f := &mexcContractFilters{ContractSize: rawNumber(c.ContractSize)}
		if v := rawNumber(c.VolUnit); v != "" {
			f.StepSize = v
		} else if n, err := strconv.Atoi(rawNumber(c.VolPrecision)); err == nil && rawNumber(c.VolPrecision) != "" {
			f.StepSize = decimalsStep(n)
		}
		f.MinQty = rawNumber(c.MinVol)
		f.MaxQty = rawNumber(c.MaxVol)
		if v := rawNumber(c.PriceUnit); v != "" {
			f.TickSize = v
		} else if n, err := strconv.Atoi(rawNumber(c.PriceScale)); err == nil && rawNumber(c.PriceScale) != "" {
			f.TickSize = decimalsStep(n)
		}
		out[c.Symbol] = f
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("parse contract/detail: no enabled contracts in response")
	}
	return out, nil
}

// mexcCoinToContracts 把币数量换算为合约张数：contracts = coinQty / contractSize。
// 单位：coinQty 为币（base coin，如 BTC），返回值为张（vol），
// 1 张 = contractSize 个币。big.Rat 精确除法，杜绝浮点尾差
// （残余尾差由 FloorToStep 的 1e-9 步进容差吸收）。
func mexcCoinToContracts(coinQty float64, contractSize string) (float64, error) {
	cs, err := parseDecimal(contractSize)
	if err != nil || cs.Sign() <= 0 {
		return 0, fmt.Errorf("invalid contractSize %q", contractSize)
	}
	q, ok := new(big.Rat).SetString(strconv.FormatFloat(coinQty, 'f', -1, 64))
	if !ok {
		return 0, fmt.Errorf("invalid quantity %v", coinQty)
	}
	contracts, _ := new(big.Rat).Quo(q, cs).Float64()
	return contracts, nil
}

// normalizeMEXCSpotOrder 规整现货下单（quantity 单位=币）。
// 语义同 normalizeBinanceOrder：约束违反返回 *OrderConstraintError（调用方必须
// 中止下单）；规则不可用时降级为旧格式串（%.6f/%.2f）并记 WARN，不阻塞下单。
func (mx *MEXCAdapter) normalizeMEXCSpotOrder(symbol, orderType string, price, quantity float64) (qtyStr, priceStr string, err error) {
	ctx := fmt.Sprintf("mexc spot %s", symbol)
	isLimit := strings.EqualFold(orderType, "LIMIT")

	f, ferr := mx.spotRules(symbol)
	if ferr != nil {
		log.Printf("[MEXC] WARN exchangeInfo unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, ferr)
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

// normalizeMEXCFuturesOrder 规整合约下单：币数量 ÷ contractSize 得到张数（vol），
// 张数按 volUnit/volPrecision 步进向下取整后校验 minVol/maxVol；取整后不足
// minVol（含不足一张 floor 到 0）明确报错，不发 HTTP。
// 规则不可用时降级旧格式串（vol %.4f / price %.2f）并记 WARN，不阻塞下单。
func (mx *MEXCAdapter) normalizeMEXCFuturesOrder(contractSymbol, orderType string, price, quantity float64) (volStr, priceStr string, err error) {
	ctx := fmt.Sprintf("mexc futures %s", contractSymbol)
	isLimit := strings.EqualFold(orderType, "LIMIT")

	f, ferr := mx.contractRules(contractSymbol)
	if ferr != nil || f.ContractSize == "" {
		if ferr == nil {
			ferr = fmt.Errorf("contract %s has no contractSize", contractSymbol)
		}
		log.Printf("[MEXC] WARN contract/detail unavailable for %s (%v); falling back to legacy %%.4f/%%.2f formatting", ctx, ferr)
		volStr = fmt.Sprintf("%.4f", quantity)
		if isLimit {
			priceStr = fmt.Sprintf("%.2f", price)
		}
		return volStr, priceStr, nil
	}

	// 币 → 张换算（单位见 mexcCoinToContracts）。
	contracts, cerr := mexcCoinToContracts(quantity, f.ContractSize)
	if cerr != nil {
		log.Printf("[MEXC] WARN coin→contract conversion failed for %s (%v); falling back to legacy %%.4f formatting", ctx, cerr)
		volStr = fmt.Sprintf("%.4f", quantity)
		if isLimit {
			priceStr = fmt.Sprintf("%.2f", price)
		}
		return volStr, priceStr, nil
	}

	// 张数取整 + minVol/maxVol 校验（f.LotFilters 的数量字段在张数语境下）。
	volCtx := fmt.Sprintf("%s vol(contracts, contractSize=%s)", ctx, f.ContractSize)
	volStr, _, err = NormalizeQuantity(&f.LotFilters, contracts, volCtx)
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
	return volStr, priceStr, nil
}
