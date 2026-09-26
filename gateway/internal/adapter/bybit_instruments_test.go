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

/* ── Bybit instruments-info + 下单的 mock 交易所 ───────────── */

// bybitOrderTestEnv 用 httptest 模拟 Bybit V5 REST：
//   - GET  /market/instruments-info?category=spot|linear → 交易对规则（可配置故障/分页）
//   - POST /order/create                                 → 记录 JSON 请求体并返回成功
type bybitOrderTestEnv struct {
	srv *httptest.Server

	spotInstrumentsHits   atomic.Int32
	linearInstrumentsHits atomic.Int32
	orderHits             atomic.Int32
	failInstruments       atomic.Bool

	mu        sync.Mutex
	lastBody  map[string]any
	allBodies []map[string]any
}

const bybitSpotInstrumentsFixture = `{
  "retCode": 0, "retMsg": "OK",
  "result": {"category": "spot", "nextPageCursor": "", "list": [
    {"symbol": "BTCUSDT", "baseCoin": "BTC", "quoteCoin": "USDT", "status": "Trading",
     "lotSizeFilter": {"basePrecision": "0.00001", "quotePrecision": "0.00000001",
       "minOrderQty": "0.00001", "maxOrderQty": "1000", "minOrderAmt": "5"},
     "priceFilter": {"tickSize": "0.01"}},
    {"symbol": "OLDUSDT", "status": "Closed",
     "lotSizeFilter": {"basePrecision": "1", "minOrderQty": "1"},
     "priceFilter": {"tickSize": "0.01"}}
  ]}, "time": 1672531200000
}`

const bybitLinearInstrumentsFixture = `{
  "retCode": 0, "retMsg": "OK",
  "result": {"category": "linear", "nextPageCursor": "", "list": [
    {"symbol": "BTCUSDT", "contractType": "LinearPerpetual", "status": "Trading",
     "priceFilter": {"minPrice": "0.05", "maxPrice": "999999.00", "tickSize": "0.05"},
     "lotSizeFilter": {"maxOrderQty": "1000", "minOrderQty": "0.001", "qtyStep": "0.001",
       "minNotionalValue": "5"}},
    {"symbol": "ETHUSDT", "contractType": "LinearPerpetual", "status": "Trading",
     "priceFilter": {"minPrice": "0.01", "maxPrice": "100000", "tickSize": "0.01"},
     "lotSizeFilter": {"maxOrderQty": "10000", "minOrderQty": "0.01", "qtyStep": "0.01",
       "minNotionalValue": "5"}}
  ]}, "time": 1672531200000
}`

func newBybitOrderTestEnv(t *testing.T) *bybitOrderTestEnv {
	t.Helper()
	env := &bybitOrderTestEnv{}
	mux := http.NewServeMux()

	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}

	mux.HandleFunc("/market/instruments-info", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("category") {
		case "spot":
			env.spotInstrumentsHits.Add(1)
		case "linear":
			env.linearInstrumentsHits.Add(1)
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if env.failInstruments.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("category") == "spot" {
			writeJSON(w, bybitSpotInstrumentsFixture)
		} else {
			writeJSON(w, bybitLinearInstrumentsFixture)
		}
	})
	mux.HandleFunc("/order/create", func(w http.ResponseWriter, r *http.Request) {
		env.orderHits.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		env.mu.Lock()
		env.lastBody = body
		env.allBodies = append(env.allBodies, body)
		env.mu.Unlock()
		writeJSON(w, `{"retCode":0,"retMsg":"OK","result":{"orderId":"abc123","orderStatus":"New"}}`)
	})

	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)

	// 指向 mock 交易所 + 清空进程级规则缓存（用例隔离）。
	t.Setenv("BYBIT_REST_URL", env.srv.URL)
	resetBybitInstrumentCache()
	t.Cleanup(resetBybitInstrumentCache)
	return env
}

func (e *bybitOrderTestEnv) body() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastBody
}

func newTestBybitAdapter() *BybitAdapter {
	return NewBybitAdapter("test-key", "test-secret", false)
}

/* ── 现货/合约下单：精度规整 ─────────────────────────────── */

func TestBybitSpotOrderPrecisionNormalization(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	b := newTestBybitAdapter()

	// 现货 BTCUSDT step=0.00001 tick=0.01。
	_, err := b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 49999.999, 0.001234567)
	btAssert(t, err == nil, "spot order ok")
	body := env.body()
	btAssertEq(t, body["qty"], "0.00123", "spot qty floored to basePrecision 0.00001")
	btAssertEq(t, body["price"], "50000.00", "spot price rounded to tickSize 0.01")
	btAssertEq(t, env.spotInstrumentsHits.Load(), int32(1), "instruments-info fetched once")

	// 第二单命中缓存：不再请求 instruments-info。
	_, err = b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.000999999)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.body()["qty"], "0.00099", "second qty floored")
	btAssertEq(t, env.spotInstrumentsHits.Load(), int32(1), "cache hit, no refetch")
}

func TestBybitFuturesOrderPrecisionNormalization(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	b := newTestBybitAdapter()

	// linear BTCUSDT qtyStep=0.001 tickSize=0.05。
	res, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000.07, 0.1234567, 10, "LONG")
	btAssert(t, err == nil, "futures order ok")
	btAssertEq(t, res["orderId"], "abc123", "exchange accepted")
	body := env.body()
	btAssertEq(t, body["qty"], "0.123", "qty floored to qtyStep 0.001")
	btAssertEq(t, body["price"], "50000.05", "price rounded to tickSize 0.05")
	btAssertEq(t, body["category"], "linear", "linear category")
	btAssert(t, body["positionIdx"] == float64(1), "positionIdx LONG preserved")
	btAssertEq(t, env.linearInstrumentsHits.Load(), int32(1), "linear instruments fetched")
	btAssertEq(t, env.spotInstrumentsHits.Load(), int32(0), "spot instruments not fetched")
}

func TestBybitOrderFloatTail(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	b := newTestBybitAdapter()

	qty := 0.1 + 0.2 // 0.30000000000000004
	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000, qty, 10, "LONG")
	btAssert(t, err == nil, "order ok")
	btAssertEq(t, env.body()["qty"], "0.300", "float tail cleaned on the wire")
}

func TestBybitMarketOrderNoPrice(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	b := newTestBybitAdapter()

	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "MARKET", 0, 0.1234567, 10, "")
	btAssert(t, err == nil, "market order ok")
	body := env.body()
	btAssertEq(t, body["qty"], "0.123", "qty floored")
	_, hasPrice := body["price"]
	btAssert(t, !hasPrice, "market order carries no price")
}

/* ── 本地规则拒绝：不发注定失败的单 ───────────────────────── */

func TestBybitRejectsBelowMinOrderQty(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	b := newTestBybitAdapter()

	// ETHUSDT minOrderQty=0.01 qtyStep=0.01：0.0099 floor→0，本地拒绝。
	_, err := b.PlaceFuturesOrder("ETHUSDT", "BUY", "LIMIT", 3000, 0.0099, 10, "LONG")
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "0.0099"), "mentions raw qty")
	btAssertEq(t, env.orderHits.Load(), int32(0), "no order sent to exchange")
}

func TestBybitRejectsBelowMinNotional(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	b := newTestBybitAdapter()

	// BTCUSDT minNotionalValue=5：0.001×1000 = 1 USDT，本地拒绝。
	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 1000, 0.001, 10, "LONG")
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "minNotional"), "mentions minNotional")
	btAssertEq(t, env.orderHits.Load(), int32(0), "no order sent to exchange")
}

/* ── 降级路径：instruments-info 不可用不阻塞下单 ──────────── */

func TestBybitFallbackWhenInstrumentsDown(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	env.failInstruments.Store(true)
	b := newTestBybitAdapter()

	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.1234567, 10, "LONG")
	btAssert(t, err == nil, "fallback must not block the order")
	body := env.body()
	btAssertEq(t, body["qty"], "0.123457", "legacy %.6f formatting")
	btAssertEq(t, body["price"], "50000.05", "legacy %.2f formatting")
	btAssertEq(t, env.orderHits.Load(), int32(1), "order still sent")
}

func TestBybitUnknownSymbolFallsBack(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	b := newTestBybitAdapter()

	_, err := b.PlaceOrder("FOOUSDT", "BUY", "LIMIT", 100, 1.23456789)
	btAssert(t, err == nil, "unknown symbol degrades, not blocks")
	btAssertEq(t, env.body()["qty"], "1.234568", "legacy formatting")
}

// 非 Trading 状态的交易对被过滤 → 视同未知 symbol 降级。
func TestBybitClosedSymbolFallsBack(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	b := newTestBybitAdapter()

	_, err := b.PlaceOrder("OLDUSDT", "BUY", "LIMIT", 100, 1.23456789)
	btAssert(t, err == nil, "closed symbol degrades, not blocks")
	btAssertEq(t, env.body()["qty"], "1.234568", "legacy formatting")
}

/* ── 缓存 TTL 与降级 ─────────────────────────────────────── */

func TestBybitInstrumentsCacheTTLExpiry(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	b := newTestBybitAdapter()

	sharedBybitInstruments.mu.Lock()
	sharedBybitInstruments.ttl = 50 * time.Millisecond
	sharedBybitInstruments.mu.Unlock()

	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.5, 10, "LONG")
	btAssert(t, err == nil, "first order ok")
	btAssertEq(t, env.linearInstrumentsHits.Load(), int32(1), "first fetch")

	time.Sleep(80 * time.Millisecond) // 超过 TTL

	_, err = b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.5, 10, "LONG")
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.linearInstrumentsHits.Load(), int32(2), "TTL expired → refetch")
}

func TestBybitInstrumentsStaleOnRefreshFailure(t *testing.T) {
	env := newBybitOrderTestEnv(t)
	b := newTestBybitAdapter()

	sharedBybitInstruments.mu.Lock()
	sharedBybitInstruments.ttl = 50 * time.Millisecond
	sharedBybitInstruments.mu.Unlock()

	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.5, 10, "LONG")
	btAssert(t, err == nil, "prime cache")

	env.failInstruments.Store(true)
	time.Sleep(80 * time.Millisecond)

	// 刷新失败但有过期缓存 → 用旧规则（qtyStep 0.001）而不是裸 %.6f。
	_, err = b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.1234567, 10, "LONG")
	btAssert(t, err == nil, "stale cache keeps order precise")
	btAssertEq(t, env.body()["qty"], "0.123", "stale filters still applied")
	btAssertEq(t, env.linearInstrumentsHits.Load(), int32(2), "refresh attempted once")
}

/* ── instruments-info 解析 ───────────────────────────────── */

func TestParseBybitInstrumentsVariants(t *testing.T) {
	spot, next, err := parseBybitInstruments([]byte(bybitSpotInstrumentsFixture))
	btAssert(t, err == nil, "parse spot")
	btAssertEq(t, next, "", "no next page")
	btc := spot["BTCUSDT"]
	btAssertEq(t, btc.StepSize, "0.00001", "spot basePrecision as step")
	btAssertEq(t, btc.MinNotional, "5", "spot minOrderAmt as minNotional")
	btAssertEq(t, btc.TickSize, "0.01", "tickSize")
	if _, ok := spot["OLDUSDT"]; ok {
		t.Fatal("non-Trading symbol must be filtered")
	}

	linear, _, err := parseBybitInstruments([]byte(bybitLinearInstrumentsFixture))
	btAssert(t, err == nil, "parse linear")
	btcL := linear["BTCUSDT"]
	btAssertEq(t, btcL.StepSize, "0.001", "linear qtyStep as step")
	btAssertEq(t, btcL.MinNotional, "5", "linear minNotionalValue as minNotional")
	btAssertEq(t, btcL.MinPrice, "0.05", "linear minPrice")

	if _, _, err := parseBybitInstruments([]byte(`{"retCode":10001,"retMsg":"params error"}`)); err == nil {
		t.Fatal("retCode!=0 must error")
	}
	if _, _, err := parseBybitInstruments([]byte(`{broken`)); err == nil {
		t.Fatal("broken JSON must error")
	}
}

// 游标分页：两页合并成一张规则表。
func TestBybitInstrumentsPagination(t *testing.T) {
	env := &bybitOrderTestEnv{}
	mux := http.NewServeMux()
	mux.HandleFunc("/market/instruments-info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"retCode":0,"result":{"category":"spot","nextPageCursor":"page2","list":[
			  {"symbol":"AAAUSDT","status":"Trading","lotSizeFilter":{"basePrecision":"0.1","minOrderQty":"0.1"},"priceFilter":{"tickSize":"0.01"}}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"retCode":0,"result":{"category":"spot","nextPageCursor":"","list":[
		  {"symbol":"BTCUSDT","status":"Trading","lotSizeFilter":{"basePrecision":"0.00001","minOrderQty":"0.00001","minOrderAmt":"1"},"priceFilter":{"tickSize":"0.01"}}]}}`))
	})
	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)
	t.Setenv("BYBIT_REST_URL", env.srv.URL)

	b := newTestBybitAdapter()
	merged, err := b.fetchInstrumentsInfo("spot")
	btAssert(t, err == nil, "paginated fetch ok")
	btAssertEq(t, len(merged), 2, "both pages merged")
	btAssertEq(t, merged["BTCUSDT"].StepSize, "0.00001", "second page symbol present")
}
