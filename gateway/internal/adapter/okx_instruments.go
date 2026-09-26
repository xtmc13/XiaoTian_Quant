package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── OKX instruments 交易规则缓存 ────────────────────────────────────────────
//
// 实盘下单前的精度/限额规则来源（与 Binance exchangeInfo 缓存同模式）：
//   - 现货 GET /api/v5/public/instruments?instType=SPOT
//   - 合约 GET /api/v5/public/instruments?instType=SWAP
//
// 单位语义（与 Binance/Bybit 不同构，关键差异）：
//   - SPOT：sz 以币（base currency）为单位，lotSz/minSz/maxSz 都是币数量；
//   - SWAP：sz 以张（contracts）为单位，lotSz/minSz/maxSz 都是张数，
//     1 张 = ctVal×ctMult 个币（如 BTC-USDT-SWAP ctVal=0.01 BTC/张）。
//     策略侧 quantity 恒为币数量，下单前换算成张数再按 lotSz 取整。
//
// 缓存是进程级的：app 层实盘路径每次下单都会 NewOKXAdapter（见
// internal/app/context.go SubmitToExchange），实例级缓存会永远冷启动。
// instruments 是公开参考数据，与账户凭证无关，跨实例共享无泄漏风险。

const okxInstrumentsTTL = time.Hour

// okxInstrumentCache 现货/合约双缓存，带 TTL 与互斥保护；刷新时持锁串行，
// 防止并发下单触发 instruments 惊群（每小时至多一次刷新）。
type okxInstrumentCache struct {
	mu     sync.Mutex
	spot   map[string]*okxInstrument
	spotAt time.Time
	swap   map[string]*okxInstrument
	swapAt time.Time
	ttl    time.Duration // 可变以便测试；生产恒为 okxInstrumentsTTL
}

// okxInstrument 是一个交易对/合约的下单规则。LotFilters 的数量字段
// （StepSize=lotSz、MinQty=minSz、MaxQty=maxSz）单位随市场而异：
// SPOT 为币、SWAP 为张；价格字段（TickSize=tickSz）两个市场一致。
type okxInstrument struct {
	LotFilters
	CtVal    string // SWAP：每张合约面值（ctVal×ctMult，CtValCcy 计）；SPOT 为空
	CtValCcy string // 面值计价币（如 BTC）；SPOT 为空
}

var sharedOKXInstruments = &okxInstrumentCache{ttl: okxInstrumentsTTL}

// resetOKXInstrumentCache 清空进程级缓存（测试用，避免用例间串数据）。
func resetOKXInstrumentCache() {
	sharedOKXInstruments.mu.Lock()
	defer sharedOKXInstruments.mu.Unlock()
	sharedOKXInstruments.spot, sharedOKXInstruments.swap = nil, nil
	sharedOKXInstruments.spotAt, sharedOKXInstruments.swapAt = time.Time{}, time.Time{}
	sharedOKXInstruments.ttl = okxInstrumentsTTL
}

// get 返回 instId 的规则；缓存新鲜则直接命中，过期则刷新。
// 刷新失败但有旧数据时降级用旧数据；完全没有数据时才返回 error。
func (c *okxInstrumentCache) get(instID string, swap bool, fetch func() (map[string]*okxInstrument, error)) (*okxInstrument, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cached, at := c.spot, c.spotAt
	if swap {
		cached, at = c.swap, c.swapAt
	}
	if time.Since(at) < c.ttl && cached != nil {
		if f, ok := cached[instID]; ok {
			return f, nil
		}
		return nil, fmt.Errorf("instId %s not found in cached instruments", instID)
	}

	fresh, err := fetch()
	if err != nil {
		if f, ok := cached[instID]; ok {
			log.Printf("[OKX] WARN instruments refresh failed (%v), using stale filters for %s", err, instID)
			return f, nil
		}
		return nil, err
	}
	if swap {
		c.swap, c.swapAt = fresh, time.Now()
	} else {
		c.spot, c.spotAt = fresh, time.Now()
	}
	if f, ok := fresh[instID]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("instId %s not found in instruments (%d instruments)", instID, len(fresh))
}

// instrumentFilters 取现货交易对规则（key 形如 "BTC-USDT"）。
func (o *OKXAdapter) instrumentFilters(instID string) (*okxInstrument, error) {
	return sharedOKXInstruments.get(instID, false, func() (map[string]*okxInstrument, error) {
		return o.fetchInstruments("SPOT")
	})
}

// swapInstrument 取永续合约规则。SWAP 合约的 instId 恒以 "-SWAP" 结尾；
// 调用方可能传 "BTC-USDT-SWAP" 全名，也可能只传 "BTC-USDT"
// （PlaceFuturesOrder 由 toOKXInstID 得到），统一补全后缀后单次查询，
// 避免冷缓存+故障时重复拉取。
func (o *OKXAdapter) swapInstrument(instID string) (*okxInstrument, error) {
	key := instID
	if !strings.HasSuffix(key, "-SWAP") {
		key += "-SWAP"
	}
	return sharedOKXInstruments.get(key, true, func() (map[string]*okxInstrument, error) {
		return o.fetchInstruments("SWAP")
	})
}

// fetchInstruments 拉取并解析某 instType 的全量 instruments。
// 公开端点，无需签名；独立 10s 超时，避免元数据拉取拖垮下单延迟。
func (o *OKXAdapter) fetchInstruments(instType string) (map[string]*okxInstrument, error) {
	u, _ := url.Parse(o.restURL() + "/api/v5/public/instruments")
	q := u.Query()
	q.Set("instType", instType)
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("okx %s instruments: %w", instType, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("okx %s instruments read: %w", instType, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("okx %s instruments: HTTP %d: %s", instType, resp.StatusCode, truncateStr(string(body), 200))
	}
	m, err := parseOKXInstruments(body)
	if err != nil {
		return nil, fmt.Errorf("okx %s instruments: %w", instType, err)
	}
	return m, nil
}

// parseOKXInstruments 解析 public/instruments 响应。字段缺失容错：
// lotSz/minSz/maxSz/tickSz 缺省为空串（下单路径跳过对应校验）；
// state 非 live 的条目跳过（字段缺失则保留）；code 容忍字符串或数字。
func parseOKXInstruments(body []byte) (map[string]*okxInstrument, error) {
	var resp struct {
		Code json.RawMessage `json:"code"`
		Msg  string          `json:"msg"`
		Data []struct {
			InstID   string `json:"instId"`
			InstType string `json:"instType"`
			State    string `json:"state"`
			TickSz   string `json:"tickSz"`
			LotSz    string `json:"lotSz"`
			MinSz    string `json:"minSz"`
			MaxSz    string `json:"maxSz"`
			CtVal    string `json:"ctVal"`
			CtMult   string `json:"ctMult"`
			CtValCcy string `json:"ctValCcy"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse instruments: %w", err)
	}
	// code 可能是 "0"（字符串）或 0（数字），统一去引号后判断。
	code := strings.Trim(string(resp.Code), `"`)
	if code != "" && code != "0" {
		return nil, fmt.Errorf("parse instruments: code=%s %s", code, resp.Msg)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("parse instruments: no data in response")
	}

	out := make(map[string]*okxInstrument, len(resp.Data))
	for _, s := range resp.Data {
		if s.InstID == "" {
			continue
		}
		if s.State != "" && s.State != "live" {
			continue
		}
		f := &okxInstrument{
			CtValCcy: s.CtValCcy,
		}
		f.TickSize = s.TickSz
		f.StepSize = s.LotSz
		f.MinQty = s.MinSz
		f.MaxQty = s.MaxSz
		// 每张合约有效面值 = ctVal × ctMult（ctMult 缺省为 1），big.Rat 精确相乘。
		if ct, err := parseDecimal(s.CtVal); err == nil && ct.Sign() > 0 {
			mult := big.NewRat(1, 1)
			if m, err := parseDecimal(s.CtMult); err == nil && m.Sign() > 0 {
				mult = m
			}
			eff := new(big.Rat).Mul(ct, mult)
			decimals := decimalPlaces(s.CtVal) + decimalPlaces(s.CtMult)
			f.CtVal = eff.FloatString(decimals)
		}
		out[s.InstID] = f
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("parse instruments: no live instruments in response")
	}
	return out, nil
}

// okxCoinToContracts 把币数量换算为合约张数：contracts = coinQty / ctVal。
// 单位：coinQty 为币（base currency，如 BTC），返回值与张（contracts）同纲，
// 1 张 = ctVal 个币。big.Rat 精确除法，杜绝 0.05/0.01=4.9999... 类浮点尾差
// （结果转 float64 后由 FloorToStep 的 1e-9 步进容差吸收残余尾差）。
func okxCoinToContracts(coinQty float64, ctVal string) (float64, error) {
	ct, err := parseDecimal(ctVal)
	if err != nil || ct.Sign() <= 0 {
		return 0, fmt.Errorf("invalid ctVal %q", ctVal)
	}
	q, ok := new(big.Rat).SetString(strconv.FormatFloat(coinQty, 'f', -1, 64))
	if !ok {
		return 0, fmt.Errorf("invalid quantity %v", coinQty)
	}
	contracts, _ := new(big.Rat).Quo(q, ct).Float64()
	return contracts, nil
}

// normalizeOKXSpotOrder 规整现货下单（sz 单位=币）。语义同 normalizeBinanceOrder：
// 约束违反返回 *OrderConstraintError（调用方必须中止下单）；规则不可用时降级
// 为旧格式串（%.6f/%.2f）并记 WARN，不阻塞下单。
func (o *OKXAdapter) normalizeOKXSpotOrder(instID, orderType string, price, quantity float64) (szStr, pxStr string, err error) {
	ctx := fmt.Sprintf("okx spot %s", instID)
	isLimit := strings.EqualFold(orderType, "limit")

	f, ferr := o.instrumentFilters(instID)
	if ferr != nil {
		log.Printf("[OKX] WARN instruments unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, ferr)
		szStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			pxStr = fmt.Sprintf("%.2f", price)
		}
		return szStr, pxStr, nil
	}

	szStr, sz, err := NormalizeQuantity(&f.LotFilters, quantity, ctx)
	if err != nil {
		return "", "", err
	}

	p := price
	if isLimit {
		pxStr, p, err = NormalizePrice(&f.LotFilters, price, ctx)
		if err != nil {
			return "", "", err
		}
	}

	if err := CheckMinNotional(&f.LotFilters, sz, p, ctx); err != nil {
		return "", "", err
	}
	return szStr, pxStr, nil
}

// normalizeOKXSwapOrder 规整永续合约下单（sz 单位=张）：
// 币数量 ÷ ctVal 得到张数，张数按 lotSz 向下取整后再校验 minSz/maxSz；
// 取整后不足 minSz（含不足一张 floor 到 0）明确报错，不发 HTTP。
// 规则不可用时降级旧格式串（%.6f/%.2f）并记 WARN，不阻塞下单。
func (o *OKXAdapter) normalizeOKXSwapOrder(instID, orderType string, price, quantity float64) (szStr, pxStr string, err error) {
	ctx := fmt.Sprintf("okx swap %s", instID)
	isLimit := strings.EqualFold(orderType, "limit")

	f, ferr := o.swapInstrument(instID)
	if ferr != nil || f.CtVal == "" {
		if ferr == nil {
			ferr = fmt.Errorf("instId %s has no ctVal", instID)
		}
		log.Printf("[OKX] WARN instruments unavailable for %s (%v); falling back to legacy %%.6f/%%.2f formatting", ctx, ferr)
		szStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			pxStr = fmt.Sprintf("%.2f", price)
		}
		return szStr, pxStr, nil
	}

	// 币 → 张换算（单位见 okxCoinToContracts）。
	contracts, cerr := okxCoinToContracts(quantity, f.CtVal)
	if cerr != nil {
		log.Printf("[OKX] WARN coin→contract conversion failed for %s (%v); falling back to legacy %%.6f formatting", ctx, cerr)
		szStr = fmt.Sprintf("%.6f", quantity)
		if isLimit {
			pxStr = fmt.Sprintf("%.2f", price)
		}
		return szStr, pxStr, nil
	}

	// 张数取整 + minSz/maxSz 校验（f.LotFilters 的数量字段在张数语境下）。
	szCtx := fmt.Sprintf("%s sz(contracts, ctVal=%s %s)", ctx, f.CtVal, f.CtValCcy)
	szStr, _, err = NormalizeQuantity(&f.LotFilters, contracts, szCtx)
	if err != nil {
		// 错误信息同时带上币数量（策略侧仓位单位），便于定位换算问题。
		return "", "", fmt.Errorf("%w (raw coin quantity %s)", err, trimFloat(quantity))
	}

	if isLimit {
		pxStr, _, err = NormalizePrice(&f.LotFilters, price, ctx)
		if err != nil {
			return "", "", err
		}
	}
	return szStr, pxStr, nil
}
