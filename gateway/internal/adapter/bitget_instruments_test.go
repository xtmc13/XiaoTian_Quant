package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

/* ── Bitget symbols + 下单的 mock 交易所 ───────────────────── */

// bitgetOrderTestEnv 用 httptest 模拟 Bitget V2 REST：
//   - GET  /spot/public/symbols                           → 现货规则（可配置故障）
//   - GET  /mix/market/symbols?productType=USDT-FUTURES   → 合约规则（可配置故障）
//   - POST /spot/trade/place-order                        → 记录 JSON 请求体并返回成功
type bitgetOrderTestEnv struct {
	srv *httptest.Server

	spotSymbolsHits atomic.Int32
	mixSymbolsHits  atomic.Int32
	orderHits       atomic.Int32
	failSymbols     atomic.Bool

	mu       sync.Mutex
	lastBody map[string]any
}

const bitgetSpotSymbolsFixture = `{
  "code": "00000", "msg": "success",
  "data": [
    {"symbol": "BTCUSDT", "baseCoin": "BTC", "quoteCoin": "USDT", "status": "online",
     "minTradeAmount": "0.00001", "maxTradeAmount": "1000",
     "pricePlace": 1, "quantityPlace": 5, "quotePrecision": 8, "minTradeUSDT": "5"},
    {"symbol": "ETHUSDT", "baseCoin": "ETH", "quoteCoin": "USDT", "status": "online",
     "minTradeAmount": "0.0001", "maxTradeAmount": "100000",
     "pricePlace": 2, "quantityPlace": 4, "quotePrecision": 8, "minTradeUSDT": "5"},
    {"symbol": "OLDUSDT", "status": "offline", "pricePlace": 1, "quantityPlace": 1}
  ]
}`

const bitgetMixSymbolsFixture = `{
  "code": "00000", "msg": "success",
  "data": [
    {"symbol": "BTCUSDT", "baseCoin": "BTC", "quoteCoin": "USDT", "status": "normal",
     "minTradeNum": "0.001", "sizePlace": 3, "pricePlace": 1, "volumePlace": 4,
     "priceEndStep": "1", "minTradeUSDT": "5"},
    {"symbol": "ETHUSDT", "baseCoin": "ETH", "quoteCoin": "USDT", "status": "normal",
     "minTradeNum": "0.01", "sizePlace": 2, "pricePlace": 2,
     "priceEndStep": "5", "minTradeUSDT": "5"}
  ]
}`

func newBitgetOrderTestEnv(t *testing.T) *bitgetOrderTestEnv {
	t.Helper()
	env := &bitgetOrderTestEnv{}
	mux := http.NewServeMux()

	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}

	mux.HandleFunc("/spot/public/symbols", func(w http.ResponseWriter, r *http.Request) {
		env.spotSymbolsHits.Add(1)
		if env.failSymbols.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(w, bitgetSpotSymbolsFixture)
	})
	mux.HandleFunc("/mix/market/symbols", func(w http.ResponseWriter, r *http.Request) {
		env.mixSymbolsHits.Add(1)
		if env.failSymbols.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(w, bitgetMixSymbolsFixture)
	})
	mux.HandleFunc("/spot/trade/place-order", func(w http.ResponseWriter, r *http.Request) {
		env.orderHits.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		env.mu.Lock()
		env.lastBody = body
		env.mu.Unlock()
		writeJSON(w, `{"code":"00000","msg":"success","data":{"orderId":"bg-1","clientOid":"x"}}`)
	})

	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)

	// 指向 mock 交易所 + 清空进程级规则缓存（用例隔离）。
	t.Setenv("BITGET_REST_URL", env.srv.URL)
	resetBitgetInstrumentCache()
	t.Cleanup(resetBitgetInstrumentCache)
	return env
}

func (e *bitgetOrderTestEnv) body() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastBody
}

func newTestBitgetAdapter() *BitgetAdapter {
	return NewBitgetAdapter("test-key", "test-secret", "test-pass")
}

/* ── 现货下单：小数位数模式精度规整 ────────────────────────── */

func TestBitgetSpotOrderPrecisionNormalization(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	b := newTestBitgetAdapter()

	// BTCUSDT quantityPlace=5（step 0.00001）、pricePlace=1（tick 0.1）。
	_, err := b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.001234567)
	btAssert(t, err == nil, "spot order ok")
	body := env.body()
	btAssertEq(t, body["quantity"], "0.00123", "qty floored to 5 decimals")
	btAssertEq(t, body["price"], "50000.1", "price rounded to 1 decimal")
	btAssertEq(t, env.spotSymbolsHits.Load(), int32(1), "symbols fetched once")

	// 第二单命中缓存：不再请求 symbols。
	_, err = b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 49999.97, 0.000999999)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.body()["quantity"], "0.00099", "second qty floored")
	btAssertEq(t, env.spotSymbolsHits.Load(), int32(1), "cache hit, no refetch")
}

func TestBitgetOrderFloatTail(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	b := newTestBitgetAdapter()

	qty := 0.1 + 0.2 // 0.30000000000000004
	_, err := b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 60000, qty)
	btAssert(t, err == nil, "order ok")
	btAssertEq(t, env.body()["quantity"], "0.30000", "float tail cleaned on the wire")
}

func TestBitgetMarketOrderNoPrice(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	b := newTestBitgetAdapter()

	_, err := b.PlaceOrder("BTCUSDT", "BUY", "MARKET", 0, 0.001234567)
	btAssert(t, err == nil, "market order ok")
	body := env.body()
	btAssertEq(t, body["quantity"], "0.00123", "qty floored")
	_, hasPrice := body["price"]
	btAssert(t, !hasPrice, "market order carries no price")
}

/* ── 本地规则拒绝：不发注定失败的单 ───────────────────────── */

func TestBitgetRejectsBelowMinTradeAmount(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	b := newTestBitgetAdapter()

	// BTCUSDT minTradeAmount=0.00001：0.000009 floor→0，本地拒绝。
	_, err := b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.000009)
	btAssert(t, err != nil, "must reject")
	btAssertEq(t, env.orderHits.Load(), int32(0), "no order sent to exchange")
}

func TestBitgetRejectsBelowMinTradeUSDT(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	b := newTestBitgetAdapter()

	// BTCUSDT minTradeUSDT=5：0.00001×1000 = 0.01 USDT，本地拒绝。
	_, err := b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 1000, 0.00001)
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "minNotional"), "mentions minNotional")
	btAssertEq(t, env.orderHits.Load(), int32(0), "no order sent to exchange")
}

/* ── 降级路径：symbols 不可用不阻塞下单 ────────────────────── */

func TestBitgetFallbackWhenSymbolsDown(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	env.failSymbols.Store(true)
	b := newTestBitgetAdapter()

	_, err := b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.1234567)
	btAssert(t, err == nil, "fallback must not block the order")
	body := env.body()
	btAssertEq(t, body["quantity"], "0.123457", "legacy %.6f formatting")
	btAssertEq(t, body["price"], "50000.05", "legacy %.2f formatting")
	btAssertEq(t, env.orderHits.Load(), int32(1), "order still sent")
}

func TestBitgetUnknownSymbolFallsBack(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	b := newTestBitgetAdapter()

	_, err := b.PlaceOrder("FOOUSDT", "BUY", "LIMIT", 100, 1.23456789)
	btAssert(t, err == nil, "unknown symbol degrades, not blocks")
	btAssertEq(t, env.body()["quantity"], "1.234568", "legacy formatting")
}

// 非 online 状态的交易对被过滤 → 视同未知 symbol 降级。
func TestBitgetOfflineSymbolFallsBack(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	b := newTestBitgetAdapter()

	_, err := b.PlaceOrder("OLDUSDT", "BUY", "LIMIT", 100, 1.23456789)
	btAssert(t, err == nil, "offline symbol degrades, not blocks")
	btAssertEq(t, env.body()["quantity"], "1.234568", "legacy formatting")
}

/* ── 缓存 TTL 与降级 ─────────────────────────────────────── */

func TestBitgetSymbolsCacheTTLExpiry(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	b := newTestBitgetAdapter()

	sharedBitgetInstruments.mu.Lock()
	sharedBitgetInstruments.ttl = 50 * time.Millisecond
	sharedBitgetInstruments.mu.Unlock()

	_, err := b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.001)
	btAssert(t, err == nil, "first order ok")
	btAssertEq(t, env.spotSymbolsHits.Load(), int32(1), "first fetch")

	time.Sleep(80 * time.Millisecond) // 超过 TTL

	_, err = b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.001)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.spotSymbolsHits.Load(), int32(2), "TTL expired → refetch")
}

func TestBitgetSymbolsStaleOnRefreshFailure(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	b := newTestBitgetAdapter()

	sharedBitgetInstruments.mu.Lock()
	sharedBitgetInstruments.ttl = 50 * time.Millisecond
	sharedBitgetInstruments.mu.Unlock()

	_, err := b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.001)
	btAssert(t, err == nil, "prime cache")

	env.failSymbols.Store(true)
	time.Sleep(80 * time.Millisecond)

	// 刷新失败但有过期缓存 → 用旧规则（5 位小数）而不是裸 %.6f。
	_, err = b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.001234567)
	btAssert(t, err == nil, "stale cache keeps order precise")
	btAssertEq(t, env.body()["quantity"], "0.00123", "stale filters still applied")
	btAssertEq(t, env.spotSymbolsHits.Load(), int32(2), "refresh attempted once")
}

/* ── 合约（USDT-FUTURES）规整：小数位数 + priceEndStep ────── */
// 注：当前 BitgetAdapter 尚无 PlaceFuturesOrder 下单方法（见报告），
// 合约规则经 normalizeBitgetOrder(mix=true) 提供并在此直接验证。

func TestBitgetMixOrderNormalization(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	b := newTestBitgetAdapter()

	// BTCUSDT mix sizePlace=3（step 0.001）、pricePlace=1 priceEndStep=1。
	qty, price, err := b.normalizeBitgetOrder("BTCUSDT", "limit", 50000.05, 0.1234567, true)
	btAssert(t, err == nil, "mix normalize ok")
	btAssertEq(t, qty, "0.123", "size floored to 3 decimals")
	btAssertEq(t, price, "50000.1", "price rounded to 1 decimal")
	btAssertEq(t, env.mixSymbolsHits.Load(), int32(1), "mix symbols fetched")
	btAssertEq(t, env.spotSymbolsHits.Load(), int32(0), "spot symbols not fetched")

	// ETHUSDT mix pricePlace=2 priceEndStep=5 → tick 0.05。
	_, price, err = b.normalizeBitgetOrder("ETHUSDT", "limit", 3000.07, 0.5, true)
	btAssert(t, err == nil, "mix normalize ok")
	btAssertEq(t, price, "3000.05", "tick = 10^-2 × priceEndStep 5 = 0.05")

	// minTradeNum 拒绝：0.009 → floor 0.00。
	_, _, err = b.normalizeBitgetOrder("ETHUSDT", "limit", 3000, 0.009, true)
	btAssert(t, err != nil, "below minTradeNum must reject")
}

func TestBitgetMixFallsBackWhenDown(t *testing.T) {
	env := newBitgetOrderTestEnv(t)
	env.failSymbols.Store(true)
	b := newTestBitgetAdapter()

	qty, price, err := b.normalizeBitgetOrder("BTCUSDT", "limit", 50000.05, 0.1234567, true)
	btAssert(t, err == nil, "fallback must not block")
	btAssertEq(t, qty, "0.123457", "legacy %.6f formatting")
	btAssertEq(t, price, "50000.05", "legacy %.2f formatting")
}

/* ── symbols 解析 ─────────────────────────────────────────── */

func TestParseBitgetSymbolsVariants(t *testing.T) {
	spot, err := parseBitgetSymbols([]byte(bitgetSpotSymbolsFixture))
	btAssert(t, err == nil, "parse spot")
	btc := spot["BTCUSDT"]
	btAssertEq(t, btc.StepSize, "0.00001", "quantityPlace 5 → step")
	btAssertEq(t, btc.TickSize, "0.1", "pricePlace 1 → tick")
	btAssertEq(t, btc.MinQty, "0.00001", "minTradeAmount")
	btAssertEq(t, btc.MinNotional, "5", "minTradeUSDT")
	if _, ok := spot["OLDUSDT"]; ok {
		t.Fatal("offline symbol must be filtered")
	}

	mix, err := parseBitgetSymbols([]byte(bitgetMixSymbolsFixture))
	btAssert(t, err == nil, "parse mix")
	eth := mix["ETHUSDT"]
	btAssertEq(t, eth.StepSize, "0.01", "sizePlace 2 → step")
	btAssertEq(t, eth.TickSize, "0.05", "pricePlace 2 × priceEndStep 5 → tick 0.05")
	btAssertEq(t, eth.MinQty, "0.01", "minTradeNum")

	// 字段缺失容错：无 sizePlace/pricePlace 也能解析（下单路径跳过对应校验）。
	m, err := parseBitgetSymbols([]byte(`{"code":"00000","data":[{"symbol":"XUSDT","status":"online","minTradeUSDT":"1"}]}`))
	btAssert(t, err == nil, "missing fields tolerated")
	btAssertEq(t, m["XUSDT"].StepSize, "", "missing quantityPlace empty")
	btAssertEq(t, m["XUSDT"].MinNotional, "1", "minTradeUSDT")

	if _, err := parseBitgetSymbols([]byte(`{"code":"400","msg":"error"}`)); err == nil {
		t.Fatal("code!=00000 must error")
	}
	if _, err := parseBitgetSymbols([]byte(`{"code":"00000","data":[]}`)); err == nil {
		t.Fatal("empty data must error")
	}
	if _, err := parseBitgetSymbols([]byte(`{broken`)); err == nil {
		t.Fatal("broken JSON must error")
	}
}
