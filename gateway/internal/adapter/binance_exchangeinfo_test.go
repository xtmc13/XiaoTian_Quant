package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

/* ── 合约/现货 exchangeInfo + 下单的 mock 交易所 ───────────── */

// binanceOrderTestEnv 用 httptest 模拟 Binance REST：
//   - GET  /fapi/v1/exchangeInfo / /api/v3/exchangeInfo → 交易对规则（可配置故障）
//   - POST /fapi/v1/order / /api/v3/order               → 记录表单并返回 NEW
//
// 覆盖实战 bug 的核心断言：最终发到线上的 quantity/price 字符串精度。
type binanceOrderTestEnv struct {
	srv *httptest.Server

	futExchangeInfoHits  atomic.Int32
	spotExchangeInfoHits atomic.Int32
	futOrderHits         atomic.Int32
	spotOrderHits        atomic.Int32
	failExchangeInfo     atomic.Bool

	mu       sync.Mutex
	lastForm url.Values
	allForms []url.Values
}

const futuresExchangeInfoFixture = `{
  "symbols": [
    {"symbol": "BTCUSDT", "status": "TRADING", "filters": [
      {"filterType": "PRICE_FILTER", "minPrice": "0.01", "maxPrice": "1000000", "tickSize": "0.10"},
      {"filterType": "LOT_SIZE", "minQty": "0.001", "maxQty": "1000", "stepSize": "0.001"},
      {"filterType": "MARKET_LOT_SIZE", "minQty": "0.001", "maxQty": "120", "stepSize": "0.01"},
      {"filterType": "MIN_NOTIONAL", "notional": "5"}
    ]},
    {"symbol": "ETHUSDT", "status": "TRADING", "filters": [
      {"filterType": "PRICE_FILTER", "minPrice": "0.01", "maxPrice": "100000", "tickSize": "0.01"},
      {"filterType": "LOT_SIZE", "minQty": "0.01", "maxQty": "10000", "stepSize": "0.001"},
      {"filterType": "NOTIONAL", "minNotional": "5"}
    ]}
  ]
}`

const spotExchangeInfoFixture = `{
  "symbols": [
    {"symbol": "BTCUSDT", "status": "TRADING", "filters": [
      {"filterType": "PRICE_FILTER", "minPrice": "0.01", "maxPrice": "1000000.00000000", "tickSize": "0.01"},
      {"filterType": "LOT_SIZE", "minQty": "0.00001000", "maxQty": "9000.00000000", "stepSize": "0.00001000"},
      {"filterType": "MIN_NOTIONAL", "minNotional": "5.00000000"}
    ]}
  ]
}`

func newBinanceOrderTestEnv(t *testing.T) *binanceOrderTestEnv {
	t.Helper()
	env := &binanceOrderTestEnv{}
	mux := http.NewServeMux()

	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
	recordOrder := func(w http.ResponseWriter, r *http.Request, hits *atomic.Int32) {
		hits.Add(1)
		_ = r.ParseForm()
		env.mu.Lock()
		env.lastForm = r.Form
		env.allForms = append(env.allForms, r.Form)
		env.mu.Unlock()
		writeJSON(w, `{"orderId":12345,"status":"NEW"}`)
	}

	mux.HandleFunc("/fapi/v1/exchangeInfo", func(w http.ResponseWriter, r *http.Request) {
		env.futExchangeInfoHits.Add(1)
		if env.failExchangeInfo.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(w, futuresExchangeInfoFixture)
	})
	mux.HandleFunc("/api/v3/exchangeInfo", func(w http.ResponseWriter, r *http.Request) {
		env.spotExchangeInfoHits.Add(1)
		if env.failExchangeInfo.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(w, spotExchangeInfoFixture)
	})
	mux.HandleFunc("/fapi/v1/order", func(w http.ResponseWriter, r *http.Request) {
		recordOrder(w, r, &env.futOrderHits)
	})
	mux.HandleFunc("/api/v3/order", func(w http.ResponseWriter, r *http.Request) {
		recordOrder(w, r, &env.spotOrderHits)
	})

	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)

	// 指向 mock 交易所 + 清空进程级规则缓存（用例隔离）。
	t.Setenv("BINANCE_FAPI_URL", env.srv.URL)
	t.Setenv("BINANCE_REST_URL", env.srv.URL+"/api/v3")
	resetBinanceFilterCache()
	t.Cleanup(resetBinanceFilterCache)
	return env
}

func (e *binanceOrderTestEnv) form() url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastForm
}

func newTestBinanceAdapter() *BinanceAdapter {
	return NewBinanceAdapter("test-key", "test-secret", false)
}

/* ── 合约下单：精度规整（核心 bug 场景）───────────────────── */

// 复现生产 -1111 的场景：策略给出 0.1234567 BTC，stepSize=0.001 ——
// 修复前 %.6f 发 "0.123457"（6 位小数，超出精度被拒）；
// 修复后必须发 "0.123"（floor 到 step 的整数倍）。
func TestPlaceFuturesOrderPrecisionNormalization(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	res, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.1234567, 10, "LONG")
	btAssert(t, err == nil, "order should succeed")
	btAssertEq(t, res["status"], "NEW", "exchange accepted")

	form := env.form()
	btAssertEq(t, form.Get("quantity"), "0.123", "qty floored to stepSize 0.001")
	btAssertEq(t, form.Get("price"), "50000.1", "price rounded to tickSize 0.10")
	btAssertEq(t, form.Get("positionSide"), "LONG", "positionSide preserved")
	btAssertEq(t, env.futExchangeInfoHits.Load(), int32(1), "exchangeInfo fetched once")

	// 第二单命中缓存：不再请求 exchangeInfo。
	_, err = b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 49999.97, 0.9876543, 10, "LONG")
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.form().Get("quantity"), "0.987", "second qty floored")
	btAssertEq(t, env.form().Get("price"), "50000.0", "second price rounded")
	btAssertEq(t, env.futExchangeInfoHits.Load(), int32(1), "cache hit, no refetch")
}

// 浮点尾差：策略仓位计算 0.1+0.2 之类结果不得以 "0.30000000000000004" 上线。
func TestPlaceFuturesOrderFloatTail(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	qty := 0.1 + 0.2 // 0.30000000000000004
	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000, qty, 10, "LONG")
	btAssert(t, err == nil, "order ok")
	btAssertEq(t, env.form().Get("quantity"), "0.300", "float tail cleaned on the wire")
}

// 市价单：只规整数量（用 MARKET_LOT_SIZE 步进 0.01），不得带 price 参数。
func TestPlaceFuturesOrderMarketUsesMarketLotSize(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "MARKET", 0, 0.1234567, 10, "LONG")
	btAssert(t, err == nil, "market order ok")
	form := env.form()
	btAssertEq(t, form.Get("quantity"), "0.12", "MARKET_LOT_SIZE step 0.01 applied")
	btAssertEq(t, form.Get("price"), "", "market order carries no price")
}

/* ── 本地规则拒绝：不发注定失败的单 ───────────────────────── */

func TestPlaceFuturesOrderRejectsBelowMinQty(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	// ETHUSDT minQty=0.01：0.0099 floor→0.009，本地拒绝。
	_, err := b.PlaceFuturesOrder("ETHUSDT", "BUY", "LIMIT", 3000, 0.0099, 10, "LONG")
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "minQty"), "mentions minQty")
	btAssert(t, strings.Contains(err.Error(), "0.0099"), "mentions raw qty")
	btAssertEq(t, env.futOrderHits.Load(), int32(0), "no order sent to exchange")
}

func TestPlaceFuturesOrderRejectsBelowMinNotional(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	// BTCUSDT minNotional=5：0.001×1000 = 1 USDT，本地拒绝。
	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 1000, 0.001, 10, "LONG")
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "minNotional"), "mentions minNotional")
	btAssertEq(t, env.futOrderHits.Load(), int32(0), "no order sent to exchange")
}

/* ── 降级路径：exchangeInfo 不可用不阻塞下单 ─────────────── */

func TestPlaceFuturesOrderFallbackWhenExchangeInfoDown(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	env.failExchangeInfo.Store(true)
	b := newTestBinanceAdapter()

	// 规则拉取失败 → 记 WARN 并退回旧格式（%.6f/%.2f），订单仍然发出。
	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.1234567, 10, "LONG")
	btAssert(t, err == nil, "fallback must not block the order")
	form := env.form()
	btAssertEq(t, form.Get("quantity"), "0.123457", "legacy %.6f formatting")
	btAssertEq(t, form.Get("price"), "50000.05", "legacy %.2f formatting")
	btAssertEq(t, env.futOrderHits.Load(), int32(1), "order still sent")
}

func TestPlaceFuturesOrderUnknownSymbolFallsBack(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	_, err := b.PlaceFuturesOrder("FOOUSDT", "BUY", "LIMIT", 100, 1.23456789, 10, "LONG")
	btAssert(t, err == nil, "unknown symbol degrades, not blocks")
	btAssertEq(t, env.form().Get("quantity"), "1.234568", "legacy formatting")
}

/* ── 缓存 TTL 与并发 ─────────────────────────────────────── */

func TestExchangeInfoCacheTTLExpiry(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	sharedBinanceFilters.mu.Lock()
	sharedBinanceFilters.ttl = 50 * time.Millisecond
	sharedBinanceFilters.mu.Unlock()

	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.5, 10, "LONG")
	btAssert(t, err == nil, "first order ok")
	btAssertEq(t, env.futExchangeInfoHits.Load(), int32(1), "first fetch")

	time.Sleep(80 * time.Millisecond) // 超过 TTL

	_, err = b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.5, 10, "LONG")
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.futExchangeInfoHits.Load(), int32(2), "TTL expired → refetch")
}

func TestExchangeInfoStaleOnRefreshFailure(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	sharedBinanceFilters.mu.Lock()
	sharedBinanceFilters.ttl = 50 * time.Millisecond
	sharedBinanceFilters.mu.Unlock()

	_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.5, 10, "LONG")
	btAssert(t, err == nil, "prime cache")
	btAssertEq(t, env.futExchangeInfoHits.Load(), int32(1), "primed")

	env.failExchangeInfo.Store(true)
	time.Sleep(80 * time.Millisecond)

	// 刷新失败但有过期缓存 → 用旧规则（step 0.001）而不是裸 %.6f。
	_, err = b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.1234567, 10, "LONG")
	btAssert(t, err == nil, "stale cache keeps order precise")
	btAssertEq(t, env.form().Get("quantity"), "0.123", "stale filters still applied")
	btAssertEq(t, env.futExchangeInfoHits.Load(), int32(2), "refresh attempted once")
}

func TestExchangeInfoConcurrentOrders(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.1234567, 10, "LONG")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		btAssert(t, err == nil, "concurrent order ok")
	}
	btAssertEq(t, env.futExchangeInfoHits.Load(), int32(1), "single fetch under concurrency")
	btAssertEq(t, env.futOrderHits.Load(), int32(n), "all orders sent")
	btAssertEq(t, env.form().Get("quantity"), "0.123", "precision under concurrency")
}

/* ── 现货路径同样修复 ────────────────────────────────────── */

func TestPlaceSpotOrderPrecisionNormalization(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	// 现货 BTCUSDT step=0.00001 tick=0.01。
	_, err := b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 49999.999, 0.001234567)
	btAssert(t, err == nil, "spot order ok")
	form := env.form()
	btAssertEq(t, form.Get("quantity"), "0.00123", "spot qty floored to 0.00001")
	btAssertEq(t, form.Get("price"), "50000.00", "spot price rounded to 0.01")
	btAssertEq(t, env.spotExchangeInfoHits.Load(), int32(1), "spot exchangeInfo fetched")

	// 现货 minNotional=5：0.00001×1000=0.01 → 拒绝。
	_, err = b.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 1000, 0.00001)
	btAssert(t, err != nil, "spot minNotional reject")
	btAssertEq(t, env.spotOrderHits.Load(), int32(1), "rejected order not sent")
}

/* ── exchangeInfo 解析 ──────────────────────────────────── */

func TestParseBinanceExchangeInfoVariants(t *testing.T) {
	fut, err := parseBinanceExchangeInfo([]byte(futuresExchangeInfoFixture))
	btAssert(t, err == nil, "parse futures")
	btc := fut["BTCUSDT"]
	btAssertEq(t, btc.StepSize, "0.001", "stepSize")
	btAssertEq(t, btc.TickSize, "0.10", "tickSize")
	btAssertEq(t, btc.MinNotional, "5", "MIN_NOTIONAL.notional variant")
	btAssertEq(t, btc.marketStepSize, "0.01", "MARKET_LOT_SIZE parsed")
	eth := fut["ETHUSDT"]
	btAssertEq(t, eth.MinNotional, "5", "NOTIONAL.minNotional variant")

	spot, err := parseBinanceExchangeInfo([]byte(spotExchangeInfoFixture))
	btAssert(t, err == nil, "parse spot")
	btAssertEq(t, spot["BTCUSDT"].MinNotional, "5.00000000", "spot MIN_NOTIONAL.minNotional")

	if _, err := parseBinanceExchangeInfo([]byte(`{"symbols":[]}`)); err == nil {
		t.Fatal("empty symbols must error")
	}
	if _, err := parseBinanceExchangeInfo([]byte(`{broken`)); err == nil {
		t.Fatal("broken JSON must error")
	}
}

// 序列化到表单的字符串必须是合法 JSON 数字之外的纯十进制——
// 抽查并发场景记录到的所有表单，防止任何科学计数法/浮点尾差漏网。
func TestAllOrderFormsAreCleanDecimals(t *testing.T) {
	env := newBinanceOrderTestEnv(t)
	b := newTestBinanceAdapter()

	for _, qty := range []float64{0.1 + 0.2, 0.1234567, 1.0 / 3.0, 999.9999999} {
		_, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000.33, qty, 10, "LONG")
		btAssert(t, err == nil, "order ok")
	}
	env.mu.Lock()
	forms := env.allForms
	env.mu.Unlock()
	btAssertEq(t, len(forms), 4, "4 orders recorded")
	for _, f := range forms {
		q := f.Get("quantity")
		p := f.Get("price")
		btAssert(t, !strings.ContainsAny(q, "eE"), "no scientific notation in qty: "+q)
		btAssert(t, !strings.ContainsAny(p, "eE"), "no scientific notation in price: "+p)
		var jq, jp float64
		btAssert(t, json.Unmarshal([]byte(q), &jq) == nil, "qty parses as JSON number")
		btAssert(t, json.Unmarshal([]byte(p), &jp) == nil, "price parses as JSON number")
	}
}
