package adapter

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

/* ── Gate currency_pairs/contracts + 下单的 mock 交易所 ────── */

// gateOrderTestEnv 用 httptest 模拟 Gate.io V4 REST：
//   - GET  /spot/currency_pairs      → 现货规则（裸数组，可配置故障）
//   - GET  /futures/usdt/contracts   → 合约规则（裸数组，可配置故障）
//   - POST /spot/orders              → 记录 JSON 请求体并返回成功
type gateOrderTestEnv struct {
	srv *httptest.Server

	pairsHits    atomic.Int32
	contractHits atomic.Int32
	orderHits    atomic.Int32
	failRules    atomic.Bool

	mu       sync.Mutex
	lastBody map[string]any
}

const gateCurrencyPairsFixture = `[
  {"id": "BTC_USDT", "base": "BTC", "quote": "USDT", "trade_status": "tradable",
   "amount_precision": 5, "price_precision": 1,
   "min_base_amount": "0.00001", "min_quote_amount": "5"},
  {"id": "ETH_USDT", "base": "ETH", "quote": "USDT", "trade_status": "tradable",
   "amount_precision": 4, "price_precision": 2,
   "min_base_amount": "0.0001", "min_quote_amount": "5"},
  {"id": "OLD_USDT", "base": "OLD", "quote": "USDT", "trade_status": "untradable",
   "amount_precision": 1, "price_precision": 1}
]`

// BTC_USDT：quanto_multiplier=0.0001 BTC/张，整数张，order_size_min=1。
const gateFuturesContractsFixture = `[
  {"name": "BTC_USDT", "quanto_multiplier": "0.0001", "order_price_round": "0.1",
   "order_size_min": 1, "order_size_max": 1000000},
  {"name": "ETH_USDT", "quanto_multiplier": "0.01", "order_price_round": "0.01",
   "order_size_min": 1, "order_size_max": 1000000}
]`

func newGateOrderTestEnv(t *testing.T) *gateOrderTestEnv {
	t.Helper()
	env := &gateOrderTestEnv{}
	mux := http.NewServeMux()

	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}

	mux.HandleFunc("/spot/currency_pairs", func(w http.ResponseWriter, r *http.Request) {
		env.pairsHits.Add(1)
		if env.failRules.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(w, gateCurrencyPairsFixture)
	})
	mux.HandleFunc("/futures/usdt/contracts", func(w http.ResponseWriter, r *http.Request) {
		env.contractHits.Add(1)
		if env.failRules.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(w, gateFuturesContractsFixture)
	})
	mux.HandleFunc("/spot/orders", func(w http.ResponseWriter, r *http.Request) {
		env.orderHits.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		env.mu.Lock()
		env.lastBody = body
		env.mu.Unlock()
		writeJSON(w, `{"id":"gate-1","status":"open"}`)
	})

	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)

	// 指向 mock 交易所 + 清空进程级规则缓存（用例隔离）。
	t.Setenv("GATEIO_REST_URL", env.srv.URL)
	resetGateIOInstrumentCache()
	t.Cleanup(resetGateIOInstrumentCache)
	return env
}

func (e *gateOrderTestEnv) body() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastBody
}

func newTestGateAdapter() *GateIOAdapter {
	return NewGateIOAdapter("test-key", "test-secret")
}

/* ── 现货下单：小数位数模式精度规整 ────────────────────────── */

func TestGateSpotOrderPrecisionNormalization(t *testing.T) {
	env := newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	// BTC_USDT amount_precision=5（step 0.00001）、price_precision=1（tick 0.1）。
	_, err := g.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.001234567)
	btAssert(t, err == nil, "spot order ok")
	body := env.body()
	btAssertEq(t, body["currency_pair"], "BTC_USDT", "pair format")
	btAssertEq(t, body["amount"], "0.00123", "amount floored to 5 decimals")
	btAssertEq(t, body["price"], "50000.1", "price rounded to 1 decimal")
	btAssertEq(t, body["time_in_force"], "gtc", "gtc preserved")
	btAssertEq(t, env.pairsHits.Load(), int32(1), "currency_pairs fetched once")

	// 第二单命中缓存：不再请求 currency_pairs。
	_, err = g.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 49999.97, 0.000999999)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.body()["amount"], "0.00099", "second qty floored")
	btAssertEq(t, env.pairsHits.Load(), int32(1), "cache hit, no refetch")
}

func TestGateOrderFloatTail(t *testing.T) {
	env := newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	qty := 0.1 + 0.2 // 0.30000000000000004
	_, err := g.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 60000, qty)
	btAssert(t, err == nil, "order ok")
	btAssertEq(t, env.body()["amount"], "0.30000", "float tail cleaned on the wire")
}

/* ── 本地规则拒绝：不发注定失败的单 ───────────────────────── */

func TestGateRejectsBelowMinBaseAmount(t *testing.T) {
	env := newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	// BTC_USDT min_base_amount=0.00001：0.000009 floor→0，本地拒绝。
	_, err := g.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.000009)
	btAssert(t, err != nil, "must reject")
	btAssertEq(t, env.orderHits.Load(), int32(0), "no order sent to exchange")
}

func TestGateRejectsBelowMinQuoteAmount(t *testing.T) {
	env := newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	// BTC_USDT min_quote_amount=5：0.00001×1000 = 0.01 USDT，本地拒绝。
	_, err := g.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 1000, 0.00001)
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "minNotional"), "mentions minNotional")
	btAssertEq(t, env.orderHits.Load(), int32(0), "no order sent to exchange")
}

/* ── 降级路径：currency_pairs 不可用不阻塞下单 ─────────────── */

func TestGateFallbackWhenCurrencyPairsDown(t *testing.T) {
	env := newGateOrderTestEnv(t)
	env.failRules.Store(true)
	g := newTestGateAdapter()

	_, err := g.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.1234567)
	btAssert(t, err == nil, "fallback must not block the order")
	body := env.body()
	btAssertEq(t, body["amount"], "0.123457", "legacy %.6f formatting")
	btAssertEq(t, body["price"], "50000.05", "legacy %.2f formatting")
	btAssertEq(t, env.orderHits.Load(), int32(1), "order still sent")
}

func TestGateUnknownPairFallsBack(t *testing.T) {
	env := newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	_, err := g.PlaceOrder("FOOUSDT", "BUY", "LIMIT", 100, 1.23456789)
	btAssert(t, err == nil, "unknown pair degrades, not blocks")
	btAssertEq(t, env.body()["amount"], "1.234568", "legacy formatting")
}

// 非 tradable 状态的交易对被过滤 → 视同未知 pair 降级。
func TestGateUntradablePairFallsBack(t *testing.T) {
	env := newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	_, err := g.PlaceOrder("OLDUSDT", "BUY", "LIMIT", 100, 1.23456789)
	btAssert(t, err == nil, "untradable pair degrades, not blocks")
	btAssertEq(t, env.body()["amount"], "1.234568", "legacy formatting")
}

/* ── 缓存 TTL 与降级 ─────────────────────────────────────── */

func TestGateCurrencyPairsCacheTTLExpiry(t *testing.T) {
	env := newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	sharedGateIOInstruments.mu.Lock()
	sharedGateIOInstruments.ttl = 50 * time.Millisecond
	sharedGateIOInstruments.mu.Unlock()

	_, err := g.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.001)
	btAssert(t, err == nil, "first order ok")
	btAssertEq(t, env.pairsHits.Load(), int32(1), "first fetch")

	time.Sleep(80 * time.Millisecond) // 超过 TTL

	_, err = g.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.001)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.pairsHits.Load(), int32(2), "TTL expired → refetch")
}

func TestGateCurrencyPairsStaleOnRefreshFailure(t *testing.T) {
	env := newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	sharedGateIOInstruments.mu.Lock()
	sharedGateIOInstruments.ttl = 50 * time.Millisecond
	sharedGateIOInstruments.mu.Unlock()

	_, err := g.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.001)
	btAssert(t, err == nil, "prime cache")

	env.failRules.Store(true)
	time.Sleep(80 * time.Millisecond)

	// 刷新失败但有过期缓存 → 用旧规则（5 位小数）而不是裸 %.6f。
	_, err = g.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.001234567)
	btAssert(t, err == nil, "stale cache keeps order precise")
	btAssertEq(t, env.body()["amount"], "0.00123", "stale filters still applied")
	btAssertEq(t, env.pairsHits.Load(), int32(2), "refresh attempted once")
}

/* ── 合约张数换算（quanto_multiplier）────────────────────── */
// 注：当前 GateIOAdapter 尚无合约下单方法（见报告），合约规则经
// normalizeGateFuturesOrder 提供并在此直接验证。

// 基础换算：0.0123 BTC ÷ 0.0001 BTC/张 = 123 张 → size "123"；价格按
// order_price_round 取整。
func TestGateFuturesOrderContractConversion(t *testing.T) {
	env := newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	size, price, err := g.normalizeGateFuturesOrder("BTC_USDT", "limit", 50000.05, 0.0123)
	btAssert(t, err == nil, "futures normalize ok")
	btAssertEq(t, size, "123", "0.0123 BTC = 123 contracts (quanto_multiplier 0.0001)")
	btAssertEq(t, price, "50000.1", "price rounded to order_price_round 0.1")
	btAssertEq(t, env.contractHits.Load(), int32(1), "contracts fetched once")

	// 第二单命中缓存。
	size, _, err = g.normalizeGateFuturesOrder("BTC_USDT", "limit", 50000, 0.01234567)
	btAssert(t, err == nil, "second ok")
	btAssertEq(t, size, "123", "123.4567 contracts floored to 123")
	btAssertEq(t, env.contractHits.Load(), int32(1), "cache hit, no refetch")
}

// 浮点尾差：0.0001+0.0002 BTC = 0.00030000000000000003 → 3 张。
func TestGateFuturesOrderFloatTail(t *testing.T) {
	newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	qty := 0.0001 + 0.0002
	size, _, err := g.normalizeGateFuturesOrder("BTC_USDT", "limit", 60000, qty)
	btAssert(t, err == nil, "ok")
	btAssertEq(t, size, "3", "float tail cleaned: 3 contracts")
}

// 不足一张：0.00005 BTC ÷ 0.0001 = 0.5 张 → floor 0 → 明确报错。
func TestGateFuturesOrderLessThanOneContract(t *testing.T) {
	newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	_, _, err := g.normalizeGateFuturesOrder("BTC_USDT", "limit", 60000, 0.00005)
	btAssert(t, err != nil, "must reject")
	var ce *OrderConstraintError
	btAssert(t, errors.As(err, &ce), "OrderConstraintError")
	btAssert(t, strings.Contains(err.Error(), "contracts"), "mentions contracts unit")
	btAssert(t, strings.Contains(err.Error(), "0.00005"), "mentions raw coin quantity")
}

// 超 order_size_max：200 BTC = 2,000,000 张 > 1,000,000 → 拒绝。
func TestGateFuturesOrderAboveMaxSize(t *testing.T) {
	newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	_, _, err := g.normalizeGateFuturesOrder("BTC_USDT", "limit", 60000, 200)
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "maxQty 1000000"), "mentions order_size_max")
}

// 市价单：只规整张数，不取整价格。
func TestGateFuturesMarketOrder(t *testing.T) {
	newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	size, price, err := g.normalizeGateFuturesOrder("BTC_USDT", "market", 0, 0.01234567)
	btAssert(t, err == nil, "market ok")
	btAssertEq(t, size, "123", "contracts floored")
	btAssertEq(t, price, "", "market order carries no price")
}

// 合约规则不可用 → 降级旧格式，不报错。
func TestGateFuturesFallbackWhenContractsDown(t *testing.T) {
	env := newGateOrderTestEnv(t)
	env.failRules.Store(true)
	g := newTestGateAdapter()

	size, price, err := g.normalizeGateFuturesOrder("BTC_USDT", "limit", 50000.05, 0.0123)
	btAssert(t, err == nil, "fallback must not block")
	btAssertEq(t, size, "0.012300", "legacy %.6f formatting (no conversion)")
	btAssertEq(t, price, "50000.05", "legacy %.2f formatting")
}

func TestGateFuturesUnknownContractFallsBack(t *testing.T) {
	newGateOrderTestEnv(t)
	g := newTestGateAdapter()

	size, _, err := g.normalizeGateFuturesOrder("FOO_USDT", "limit", 100, 1.23456789)
	btAssert(t, err == nil, "unknown contract degrades, not blocks")
	btAssertEq(t, size, "1.234568", "legacy formatting")
}

/* ── 解析与换算单元测试 ───────────────────────────────────── */

func TestParseGateIOCurrencyPairsVariants(t *testing.T) {
	pairs, err := parseGateIOCurrencyPairs([]byte(gateCurrencyPairsFixture))
	btAssert(t, err == nil, "parse pairs")
	btc := pairs["BTC_USDT"]
	btAssertEq(t, btc.StepSize, "0.00001", "amount_precision 5 → step")
	btAssertEq(t, btc.TickSize, "0.1", "price_precision 1 → tick")
	btAssertEq(t, btc.MinQty, "0.00001", "min_base_amount")
	btAssertEq(t, btc.MinNotional, "5", "min_quote_amount")
	if _, ok := pairs["OLD_USDT"]; ok {
		t.Fatal("untradable pair must be filtered")
	}

	// 字段缺失容错：无 precision 也能解析。
	m, err := parseGateIOCurrencyPairs([]byte(`[{"id":"X_USDT","trade_status":"tradable","min_quote_amount":"1"}]`))
	btAssert(t, err == nil, "missing fields tolerated")
	btAssertEq(t, m["X_USDT"].StepSize, "", "missing amount_precision empty")

	if _, err := parseGateIOCurrencyPairs([]byte(`[]`)); err == nil {
		t.Fatal("empty array must error")
	}
	if _, err := parseGateIOCurrencyPairs([]byte(`{broken`)); err == nil {
		t.Fatal("broken JSON must error")
	}
}

func TestParseGateIOFuturesContractsVariants(t *testing.T) {
	m, err := parseGateIOFuturesContracts([]byte(gateFuturesContractsFixture))
	btAssert(t, err == nil, "parse contracts")
	btc := m["BTC_USDT"]
	btAssertEq(t, btc.QuantoMultiplier, "0.0001", "quanto_multiplier")
	btAssertEq(t, btc.StepSize, "1", "integer contracts")
	btAssertEq(t, btc.MinQty, "1", "order_size_min")
	btAssertEq(t, btc.MaxQty, "1000000", "order_size_max")
	btAssertEq(t, btc.TickSize, "0.1", "order_price_round")

	// 字段缺失容错：无 quanto_multiplier 也能解析（下单路径降级处理）。
	m2, err := parseGateIOFuturesContracts([]byte(`[{"name":"Y_USDT","order_price_round":"0.01"}]`))
	btAssert(t, err == nil, "missing fields tolerated")
	btAssertEq(t, m2["Y_USDT"].QuantoMultiplier, "", "missing quanto_multiplier empty")

	if _, err := parseGateIOFuturesContracts([]byte(`[]`)); err == nil {
		t.Fatal("empty array must error")
	}
	if _, err := parseGateIOFuturesContracts([]byte(`{broken`)); err == nil {
		t.Fatal("broken JSON must error")
	}
}

func TestGateCoinToContracts(t *testing.T) {
	c, err := gateCoinToContracts(0.0123, "0.0001")
	btAssert(t, err == nil, "no error")
	btAssertEq(t, c, 123.0, "0.0123 BTC / 0.0001 = 123")

	if _, err := gateCoinToContracts(1, "0"); err == nil {
		t.Fatal("zero quanto_multiplier must error")
	}
	if _, err := gateCoinToContracts(1, "abc"); err == nil {
		t.Fatal("non-numeric quanto_multiplier must error")
	}
}
