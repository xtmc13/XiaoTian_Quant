package adapter

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

/* ── MEXC exchangeInfo/contract detail + 下单的 mock 交易所 ── */

// mexcOrderTestEnv 用 httptest 模拟 MEXC REST：
//   - GET  /exchangeInfo                    → 现货交易对规则（可配置故障）
//   - GET  /api/v1/contract/detail          → 合约规则（可配置故障）
//   - POST /order                           → 现货下单，记录表单
//   - POST /api/v1/private/order/submit     → 合约下单，记录表单
type mexcOrderTestEnv struct {
	srv *httptest.Server

	spotInfoHits     atomic.Int32
	contractInfoHits atomic.Int32
	spotOrderHits    atomic.Int32
	futOrderHits     atomic.Int32
	failSpotInfo     atomic.Bool
	failContractInfo atomic.Bool

	mu       sync.Mutex
	lastForm url.Values
}

const mexcSpotExchangeInfoFixture = `{
  "symbols": [
    {"symbol": "BTCUSDT", "status": "1", "baseAsset": "BTC", "quoteAsset": "USDT",
     "baseAssetPrecision": 8, "quotePrecision": 2,
     "baseSizePrecision": "0.00001", "quoteAmountPrecision": "5"},
    {"symbol": "ETHUSDT", "status": "1", "baseAsset": "ETH", "quoteAsset": "USDT",
     "baseAssetPrecision": 8, "quotePrecision": 2,
     "baseSizePrecision": "0.0001", "quoteAmountPrecision": "5"}
  ]
}`

// BTC_USDT：contractSize=0.0001 BTC/张，volUnit=1（整数张），minVol=1，priceUnit=0.1。
// ETH_USDT：contractSize=0.01 ETH/张，volPrecision=0（无 volUnit 时推导步进），priceScale=2。
const mexcContractDetailFixture = `{
  "success": true, "code": 0,
  "data": [
    {"symbol": "BTC_USDT", "state": 0, "contractSize": 0.0001, "volUnit": 1,
     "minVol": 1, "maxVol": 1000000, "priceUnit": 0.1, "priceScale": 1},
    {"symbol": "ETH_USDT", "state": 0, "contractSize": 0.01, "volPrecision": 0,
     "minVol": 1, "maxVol": 1000000, "priceScale": 2},
    {"symbol": "OLD_USDT", "state": 2, "contractSize": 1, "volUnit": 1,
     "minVol": 1, "maxVol": 100, "priceUnit": 0.01}
  ]
}`

func newMEXCOrderTestEnv(t *testing.T) *mexcOrderTestEnv {
	t.Helper()
	env := &mexcOrderTestEnv{}
	mux := http.NewServeMux()

	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
	recordForm := func(w http.ResponseWriter, r *http.Request, hits *atomic.Int32, resp string) {
		hits.Add(1)
		_ = r.ParseForm()
		env.mu.Lock()
		env.lastForm = r.Form
		env.mu.Unlock()
		writeJSON(w, resp)
	}

	mux.HandleFunc("/exchangeInfo", func(w http.ResponseWriter, r *http.Request) {
		env.spotInfoHits.Add(1)
		if env.failSpotInfo.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(w, mexcSpotExchangeInfoFixture)
	})
	mux.HandleFunc("/api/v1/contract/detail", func(w http.ResponseWriter, r *http.Request) {
		env.contractInfoHits.Add(1)
		if env.failContractInfo.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(w, mexcContractDetailFixture)
	})
	mux.HandleFunc("/order", func(w http.ResponseWriter, r *http.Request) {
		recordForm(w, r, &env.spotOrderHits, `{"orderId":"spot-1"}`)
	})
	mux.HandleFunc("/api/v1/private/order/submit", func(w http.ResponseWriter, r *http.Request) {
		// 注：现有代码把 code==200 视为成功（真实 MEXC 合约 API 成功码是 0，见报告）。
		recordForm(w, r, &env.futOrderHits, `{"success":true,"code":200,"data":12345}`)
	})

	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)

	// 指向 mock 交易所 + 清空进程级规则缓存（用例隔离）。
	t.Setenv("MEXC_REST_URL", env.srv.URL)
	t.Setenv("MEXC_CONTRACT_URL", env.srv.URL)
	resetMEXCInstrumentCache()
	t.Cleanup(resetMEXCInstrumentCache)
	return env
}

func (e *mexcOrderTestEnv) form() url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastForm
}

func newTestMEXCAdapter() *MEXCAdapter {
	return NewMEXCAdapter("test-key", "test-secret")
}

/* ── 现货下单：精度规整 ────────────────────────────────────── */

func TestMEXCSpotOrderPrecisionNormalization(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	// 现货 BTCUSDT step=0.00001（baseSizePrecision），tick=0.01（quotePrecision=2 推导）。
	_, err := mx.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000.055, 0.001234567)
	btAssert(t, err == nil, "spot order ok")
	form := env.form()
	btAssertEq(t, form.Get("quantity"), "0.00123", "spot qty floored to baseSizePrecision")
	btAssertEq(t, form.Get("price"), "50000.06", "spot price rounded to 10^-quotePrecision")
	btAssertEq(t, env.spotInfoHits.Load(), int32(1), "exchangeInfo fetched once")

	// 第二单命中缓存：不再请求 exchangeInfo。
	_, err = mx.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 49999.999, 0.000999999)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.form().Get("quantity"), "0.00099", "second qty floored")
	btAssertEq(t, env.spotInfoHits.Load(), int32(1), "cache hit, no refetch")
}

func TestMEXCSpotRejectsBelowMinNotional(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	// BTCUSDT quoteAmountPrecision=5：0.00001×1000 = 0.01 USDT，本地拒绝。
	_, err := mx.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 1000, 0.00001)
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "minNotional"), "mentions minNotional")
	btAssertEq(t, env.spotOrderHits.Load(), int32(0), "no order sent to exchange")
}

/* ── 合约下单：币→张换算 ──────────────────────────────────── */

// 基础换算：0.0123 BTC ÷ 0.0001 BTC/张 = 123 张 → vol "123"；价格按 priceUnit 取整。
func TestMEXCFuturesOrderContractConversion(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	_, err := mx.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.0123, 10, "LONG")
	btAssert(t, err == nil, "futures order ok")
	form := env.form()
	btAssertEq(t, form.Get("symbol"), "BTC_USDT", "contract symbol format")
	btAssertEq(t, form.Get("vol"), "123", "0.0123 BTC = 123 contracts (contractSize 0.0001)")
	btAssertEq(t, form.Get("price"), "50000.1", "price rounded to priceUnit 0.1")
	btAssertEq(t, env.contractInfoHits.Load(), int32(1), "contract/detail fetched once")

	// 第二单命中缓存。
	_, err = mx.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 49999.97, 0.01234567, 10, "LONG")
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.form().Get("vol"), "123", "123.4567 contracts floored to 123")
	btAssertEq(t, env.contractInfoHits.Load(), int32(1), "cache hit, no refetch")
}

// 浮点尾差：0.0001+0.0002 BTC = 0.00030000000000000003 → 3 张，不得以尾差上线。
func TestMEXCFuturesOrderFloatTail(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	qty := 0.0001 + 0.0002
	_, err := mx.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000, qty, 10, "LONG")
	btAssert(t, err == nil, "order ok")
	btAssertEq(t, env.form().Get("vol"), "3", "float tail cleaned: 3 contracts")
}

// 不足一张：0.00005 BTC ÷ 0.0001 = 0.5 张 → floor 0 → 明确报错，不发 HTTP。
func TestMEXCFuturesOrderLessThanOneContract(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	_, err := mx.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000, 0.00005, 10, "LONG")
	btAssert(t, err != nil, "must reject")
	var ce *OrderConstraintError
	btAssert(t, errors.As(err, &ce), "OrderConstraintError")
	btAssert(t, strings.Contains(err.Error(), "contracts"), "mentions contracts unit")
	btAssert(t, strings.Contains(err.Error(), "0.00005"), "mentions raw coin quantity")
	btAssertEq(t, env.futOrderHits.Load(), int32(0), "no order sent to exchange")
}

// 超 maxVol：200 BTC = 2,000,000 张 > maxVol 1,000,000 → 拒绝，不发 HTTP。
func TestMEXCFuturesOrderAboveMaxVol(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	_, err := mx.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000, 200, 10, "LONG")
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "maxQty 1000000"), "mentions maxVol")
	btAssertEq(t, env.futOrderHits.Load(), int32(0), "no order sent")
}

// 市价单：只规整张数，不带 price。
func TestMEXCFuturesMarketOrder(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	_, err := mx.PlaceFuturesOrder("BTCUSDT", "SELL", "MARKET", 0, 0.01234567, 10, "SHORT")
	btAssert(t, err == nil, "market order ok")
	form := env.form()
	btAssertEq(t, form.Get("vol"), "123", "contracts floored")
	btAssertEq(t, form.Get("price"), "", "market order carries no price")
}

// ETH_USDT：无 volUnit，由 volPrecision=0 推导出整数张步进。
func TestMEXCFuturesOrderVolPrecisionDerived(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	// 0.155 ETH ÷ 0.01 = 15.5 张 → floor 15 张。
	_, err := mx.PlaceFuturesOrder("ETHUSDT", "BUY", "LIMIT", 3000.055, 0.155, 10, "LONG")
	btAssert(t, err == nil, "order ok")
	form := env.form()
	btAssertEq(t, form.Get("vol"), "15", "15.5 contracts floored to 15")
	btAssertEq(t, form.Get("price"), "3000.06", "price rounded to 10^-priceScale")
}

/* ── 降级路径：规则不可用不阻塞下单 ────────────────────────── */

func TestMEXCFuturesFallbackWhenContractDetailDown(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	env.failContractInfo.Store(true)
	mx := newTestMEXCAdapter()

	_, err := mx.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.0123, 10, "LONG")
	btAssert(t, err == nil, "fallback must not block the order")
	form := env.form()
	btAssertEq(t, form.Get("vol"), "0.0123", "legacy %.4f formatting (no conversion)")
	btAssertEq(t, form.Get("price"), "50000.05", "legacy %.2f formatting")
	btAssertEq(t, env.futOrderHits.Load(), int32(1), "order still sent")
}

func TestMEXCFuturesUnknownContractFallsBack(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	_, err := mx.PlaceFuturesOrder("FOOUSDT", "BUY", "LIMIT", 100, 1.23456789, 10, "LONG")
	btAssert(t, err == nil, "unknown contract degrades, not blocks")
	btAssertEq(t, env.form().Get("vol"), "1.2346", "legacy formatting")
}

// 非启用状态（state!=0）的合约被过滤 → 视同未知合约降级。
func TestMEXCFuturesDisabledContractFallsBack(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	_, err := mx.PlaceFuturesOrder("OLDUSDT", "BUY", "LIMIT", 100, 5, 10, "LONG")
	btAssert(t, err == nil, "disabled contract degrades, not blocks")
	btAssertEq(t, env.form().Get("vol"), "5.0000", "legacy formatting")
}

func TestMEXCSpotFallbackWhenExchangeInfoDown(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	env.failSpotInfo.Store(true)
	mx := newTestMEXCAdapter()

	_, err := mx.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.1234567)
	btAssert(t, err == nil, "fallback must not block the order")
	form := env.form()
	btAssertEq(t, form.Get("quantity"), "0.123457", "legacy %.6f formatting")
	btAssertEq(t, form.Get("price"), "50000.05", "legacy %.2f formatting")
	btAssertEq(t, env.spotOrderHits.Load(), int32(1), "order still sent")
}

/* ── 缓存 TTL 与降级 ─────────────────────────────────────── */

func TestMEXCInstrumentsCacheTTLExpiry(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	sharedMEXCInstruments.mu.Lock()
	sharedMEXCInstruments.ttl = 50 * time.Millisecond
	sharedMEXCInstruments.mu.Unlock()

	_, err := mx.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.0123, 10, "LONG")
	btAssert(t, err == nil, "first order ok")
	btAssertEq(t, env.contractInfoHits.Load(), int32(1), "first fetch")

	time.Sleep(80 * time.Millisecond) // 超过 TTL

	_, err = mx.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.0123, 10, "LONG")
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.contractInfoHits.Load(), int32(2), "TTL expired → refetch")
}

func TestMEXCInstrumentsStaleOnRefreshFailure(t *testing.T) {
	env := newMEXCOrderTestEnv(t)
	mx := newTestMEXCAdapter()

	sharedMEXCInstruments.mu.Lock()
	sharedMEXCInstruments.ttl = 50 * time.Millisecond
	sharedMEXCInstruments.mu.Unlock()

	_, err := mx.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.0123, 10, "LONG")
	btAssert(t, err == nil, "prime cache")

	env.failContractInfo.Store(true)
	time.Sleep(80 * time.Millisecond)

	// 刷新失败但有过期缓存 → 用旧规则（contractSize 换算）而不是裸 %.4f。
	_, err = mx.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.01234567, 10, "LONG")
	btAssert(t, err == nil, "stale cache keeps conversion precise")
	btAssertEq(t, env.form().Get("vol"), "123", "stale filters still applied")
	btAssertEq(t, env.contractInfoHits.Load(), int32(2), "refresh attempted once")
}

/* ── 解析与换算单元测试 ───────────────────────────────────── */

func TestParseMEXCExchangeInfoVariants(t *testing.T) {
	spot, err := parseMEXCExchangeInfo([]byte(mexcSpotExchangeInfoFixture))
	btAssert(t, err == nil, "parse spot")
	btc := spot["BTCUSDT"]
	btAssertEq(t, btc.StepSize, "0.00001", "baseSizePrecision as step")
	btAssertEq(t, btc.TickSize, "0.01", "tickSize derived from quotePrecision=2")
	btAssertEq(t, btc.MinNotional, "5", "quoteAmountPrecision as minNotional")

	// Binance 风格 filters 兜底：平铺字段缺失时从 filters 补齐。
	filtersVariant := `{"symbols":[{"symbol":"SOLUSDT","filters":[
	  {"filterType":"LOT_SIZE","minQty":"0.01","maxQty":"1000","stepSize":"0.01"},
	  {"filterType":"PRICE_FILTER","tickSize":"0.001"},
	  {"filterType":"MIN_NOTIONAL","minNotional":"1"}]}]}`
	m, err := parseMEXCExchangeInfo([]byte(filtersVariant))
	btAssert(t, err == nil, "parse filters variant")
	sol := m["SOLUSDT"]
	btAssertEq(t, sol.StepSize, "0.01", "LOT_SIZE stepSize fallback")
	btAssertEq(t, sol.MinQty, "0.01", "LOT_SIZE minQty fallback")
	btAssertEq(t, sol.TickSize, "0.001", "PRICE_FILTER tickSize fallback")
	btAssertEq(t, sol.MinNotional, "1", "MIN_NOTIONAL fallback")

	if _, err := parseMEXCExchangeInfo([]byte(`{"symbols":[]}`)); err == nil {
		t.Fatal("empty symbols must error")
	}
	if _, err := parseMEXCExchangeInfo([]byte(`{broken`)); err == nil {
		t.Fatal("broken JSON must error")
	}
}

func TestParseMEXCContractDetailVariants(t *testing.T) {
	m, err := parseMEXCContractDetail([]byte(mexcContractDetailFixture))
	btAssert(t, err == nil, "parse contract detail")
	btc := m["BTC_USDT"]
	btAssertEq(t, btc.ContractSize, "0.0001", "contractSize exact decimal (no float tail)")
	btAssertEq(t, btc.StepSize, "1", "volUnit as step")
	btAssertEq(t, btc.MinQty, "1", "minVol")
	btAssertEq(t, btc.TickSize, "0.1", "priceUnit")
	eth := m["ETH_USDT"]
	btAssertEq(t, eth.StepSize, "1", "volPrecision=0 → integer contracts")
	btAssertEq(t, eth.TickSize, "0.01", "priceScale=2 → 0.01")
	if _, ok := m["OLD_USDT"]; ok {
		t.Fatal("state!=0 contract must be filtered")
	}

	// 字段为字符串形式也容错解析。
	strVariant := `{"success":true,"code":0,"data":[{"symbol":"X_USDT","state":0,"contractSize":"0.001","volUnit":"1","minVol":"1","maxVol":"100","priceUnit":"0.05"}]}`
	m2, err := parseMEXCContractDetail([]byte(strVariant))
	btAssert(t, err == nil, "string fields tolerated")
	btAssertEq(t, m2["X_USDT"].ContractSize, "0.001", "string contractSize")
	btAssertEq(t, m2["X_USDT"].TickSize, "0.05", "string priceUnit")

	if _, err := parseMEXCContractDetail([]byte(`{"success":false,"code":500,"data":[]}`)); err == nil {
		t.Fatal("code!=0 must error")
	}
	if _, err := parseMEXCContractDetail([]byte(`{"success":true,"code":0,"data":[]}`)); err == nil {
		t.Fatal("empty data must error")
	}
	if _, err := parseMEXCContractDetail([]byte(`{broken`)); err == nil {
		t.Fatal("broken JSON must error")
	}
}

func TestMEXCCoinToContracts(t *testing.T) {
	c, err := mexcCoinToContracts(0.0123, "0.0001")
	btAssert(t, err == nil, "no error")
	btAssertEq(t, c, 123.0, "0.0123 BTC / 0.0001 = 123")

	if _, err := mexcCoinToContracts(1, "0"); err == nil {
		t.Fatal("zero contractSize must error")
	}
	if _, err := mexcCoinToContracts(1, ""); err == nil {
		t.Fatal("empty contractSize must error")
	}
}
