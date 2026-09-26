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

/* ── OKX instruments + 下单的 mock 交易所 ──────────────────── */

// okxOrderTestEnv 用 httptest 模拟 OKX V5 REST：
//   - GET  /api/v5/public/instruments?instType=SPOT|SWAP → 交易对规则（可配置故障）
//   - POST /api/v5/trade/order                           → 记录 JSON 请求体并返回成功
type okxOrderTestEnv struct {
	srv *httptest.Server

	spotInstrumentsHits atomic.Int32
	swapInstrumentsHits atomic.Int32
	orderHits           atomic.Int32
	failInstruments     atomic.Bool

	mu       sync.Mutex
	lastBody map[string]any
}

const okxSpotInstrumentsFixture = `{
  "code": "0", "msg": "",
  "data": [
    {"instType": "SPOT", "instId": "BTC-USDT", "state": "live",
     "tickSz": "0.1", "lotSz": "0.00001", "minSz": "0.00001", "maxSz": "10000"},
    {"instType": "SPOT", "instId": "ETH-USDT", "state": "live",
     "tickSz": "0.01", "lotSz": "0.0001", "minSz": "0.0001", "maxSz": "100000"},
    {"instType": "SPOT", "instId": "OLD-USDT", "state": "suspend",
     "tickSz": "0.01", "lotSz": "1", "minSz": "1", "maxSz": "100"}
  ]
}`

// BTC-USDT-SWAP：ctVal=0.01 BTC/张，lotSz=minSz=1 张（整数张），maxSz=100 张。
// ETH-USDT-SWAP：ctVal=0.1 ETH/张，lotSz=0.1 张（小数张步进），minSz=1 张。
// DOGE-USDT-SWAP：ctVal=10、ctMult=0.5 → 有效面值 5 DOGE/张（验证 ctMult 乘法）。
const okxSwapInstrumentsFixture = `{
  "code": "0", "msg": "",
  "data": [
    {"instType": "SWAP", "instId": "BTC-USDT-SWAP", "state": "live",
     "tickSz": "0.1", "lotSz": "1", "minSz": "1", "maxSz": "100",
     "ctVal": "0.01", "ctMult": "1", "ctValCcy": "BTC"},
    {"instType": "SWAP", "instId": "ETH-USDT-SWAP", "state": "live",
     "tickSz": "0.01", "lotSz": "0.1", "minSz": "1", "maxSz": "1000000",
     "ctVal": "0.1", "ctMult": "1", "ctValCcy": "ETH"},
    {"instType": "SWAP", "instId": "DOGE-USDT-SWAP", "state": "live",
     "tickSz": "0.00001", "lotSz": "1", "minSz": "1", "maxSz": "1000000",
     "ctVal": "10", "ctMult": "0.5", "ctValCcy": "DOGE"}
  ]
}`

func newOKXOrderTestEnv(t *testing.T) *okxOrderTestEnv {
	t.Helper()
	env := &okxOrderTestEnv{}
	mux := http.NewServeMux()

	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}

	mux.HandleFunc("/api/v5/public/instruments", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("instType") {
		case "SPOT":
			env.spotInstrumentsHits.Add(1)
		case "SWAP":
			env.swapInstrumentsHits.Add(1)
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if env.failInstruments.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("instType") == "SPOT" {
			writeJSON(w, okxSpotInstrumentsFixture)
		} else {
			writeJSON(w, okxSwapInstrumentsFixture)
		}
	})
	mux.HandleFunc("/api/v5/trade/order", func(w http.ResponseWriter, r *http.Request) {
		env.orderHits.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		env.mu.Lock()
		env.lastBody = body
		env.mu.Unlock()
		writeJSON(w, `{"code":"0","msg":"","data":[{"ordId":"okx-1","sCode":"0","sMsg":"success"}]}`)
	})

	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)

	// 指向 mock 交易所 + 清空进程级规则缓存（用例隔离）。
	t.Setenv("OKX_REST_URL", env.srv.URL)
	resetOKXInstrumentCache()
	t.Cleanup(resetOKXInstrumentCache)
	return env
}

func (e *okxOrderTestEnv) body() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastBody
}

func newTestOKXAdapter() *OKXAdapter {
	return NewOKXAdapter("test-key", "test-secret", "test-pass", false)
}

/* ── SWAP 张数换算（核心差异路径，用例给足）────────────────── */

// 基础换算：0.05 BTC ÷ 0.01 BTC/张 = 5 张 → sz "5"；价格按 tickSz 取整。
func TestOKXSwapOrderContractConversion(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	_, err := o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.05, 10, "long")
	btAssert(t, err == nil, "swap order ok")
	body := env.body()
	btAssertEq(t, body["sz"], "5", "0.05 BTC = 5 contracts (ctVal 0.01)")
	btAssertEq(t, body["px"], "50000.1", "price rounded to tickSz 0.1")
	btAssertEq(t, body["instId"], "BTC-USDT", "instId passthrough")
	btAssertEq(t, env.swapInstrumentsHits.Load(), int32(1), "swap instruments fetched once")

	// 第二单命中缓存：不再请求 instruments。
	_, err = o.PlaceFuturesOrder("BTC-USDT-SWAP", "BUY", "LIMIT", 49999.97, 0.07, 10, "long")
	btAssert(t, err == nil, "full instId also works")
	btAssertEq(t, env.body()["sz"], "7", "0.07 BTC = 7 contracts")
	btAssertEq(t, env.swapInstrumentsHits.Load(), int32(1), "cache hit, no refetch")
}

// 浮点尾差：0.1+0.2 BTC = 0.30000000000000004 → 30 张，不得以尾差上线；
// 0.03 BTC ÷ 0.01 = 2.9999999999999996 → 恰在边界下一个 ULP，容差吸收为 3 张。
func TestOKXSwapOrderFloatTails(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	qty := 0.1 + 0.2
	_, err := o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000, qty, 10, "long")
	btAssert(t, err == nil, "order ok")
	btAssertEq(t, env.body()["sz"], "30", "float tail cleaned: 30 contracts")

	_, err = o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000, 0.03, 10, "long")
	btAssert(t, err == nil, "boundary qty ok")
	btAssertEq(t, env.body()["sz"], "3", "one ULP below boundary absorbed → 3 contracts")
}

// 不足一张：0.005 BTC ÷ 0.01 = 0.5 张 → floor 0 → 明确报错，不发 HTTP。
func TestOKXSwapOrderLessThanOneContract(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	_, err := o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000, 0.005, 10, "long")
	btAssert(t, err != nil, "must reject")
	var ce *OrderConstraintError
	btAssert(t, errors.As(err, &ce), "OrderConstraintError")
	btAssert(t, strings.Contains(err.Error(), "contracts"), "mentions contracts unit")
	btAssert(t, strings.Contains(err.Error(), "0.005"), "mentions raw coin quantity")
	btAssertEq(t, env.orderHits.Load(), int32(0), "no order sent to exchange")
}

// 取整后不足 minSz：0.05 ETH ÷ 0.1 = 0.5 张 → floor 0.5 张 < minSz 1 → 拒绝。
// 同合约 0.15 ETH → 1.5 张（lotSz=0.1 小数张步进）→ 放行。
func TestOKXSwapOrderMinSzAfterFloor(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	_, err := o.PlaceFuturesOrder("ETHUSDT", "BUY", "LIMIT", 3000, 0.05, 10, "long")
	btAssert(t, err != nil, "0.5 contracts < minSz 1 must reject")
	btAssert(t, strings.Contains(err.Error(), "minQty 1"), "mentions minSz")
	btAssertEq(t, env.orderHits.Load(), int32(0), "no order sent")

	_, err = o.PlaceFuturesOrder("ETHUSDT", "BUY", "LIMIT", 3000, 0.15, 10, "long")
	btAssert(t, err == nil, "1.5 contracts ok")
	btAssertEq(t, env.body()["sz"], "1.5", "fractional lotSz 0.1 honored")
}

// 超 maxSz：2 BTC = 200 张 > maxSz 100 → 拒绝，不发 HTTP。
func TestOKXSwapOrderAboveMaxSz(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	_, err := o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000, 2, 10, "long")
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "maxQty 100"), "mentions maxSz")
	btAssertEq(t, env.orderHits.Load(), int32(0), "no order sent")
}

// ctMult 参与面值：DOGE ctVal=10 × ctMult=0.5 = 5 DOGE/张；25 DOGE → 5 张。
func TestOKXSwapOrderCtMultApplied(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	_, err := o.PlaceFuturesOrder("DOGEUSDT", "BUY", "LIMIT", 0.1, 25, 10, "long")
	btAssert(t, err == nil, "order ok")
	btAssertEq(t, env.body()["sz"], "5", "25 DOGE ÷ (10×0.5) = 5 contracts")
}

// 市价单：只规整张数，不带 px。
func TestOKXSwapMarketOrder(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	_, err := o.PlaceFuturesOrder("BTCUSDT", "SELL", "MARKET", 0, 0.05, 10, "short")
	btAssert(t, err == nil, "market order ok")
	body := env.body()
	btAssertEq(t, body["sz"], "5", "contracts floored")
	btAssertEq(t, body["ordType"], "market", "market type")
	_, hasPx := body["px"]
	btAssert(t, !hasPx, "market order carries no px")
}

/* ── 现货路径（sz 单位=币，不换算）─────────────────────────── */

func TestOKXSpotOrderPrecisionNormalization(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	_, err := o.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.001234567)
	btAssert(t, err == nil, "spot order ok")
	body := env.body()
	btAssertEq(t, body["sz"], "0.00123", "qty floored to lotSz 0.00001 (coin units)")
	btAssertEq(t, body["px"], "50000.1", "price rounded to tickSz 0.1")
	btAssertEq(t, body["tdMode"], "cash", "spot cash mode")
	btAssertEq(t, env.spotInstrumentsHits.Load(), int32(1), "spot instruments fetched")
	btAssertEq(t, env.swapInstrumentsHits.Load(), int32(0), "swap instruments not fetched")
}

func TestOKXSpotRejectsBelowMinSz(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	// BTC-USDT minSz=0.00001：0.000009 floor→0，本地拒绝。
	_, err := o.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.000009)
	btAssert(t, err != nil, "must reject")
	btAssertEq(t, env.orderHits.Load(), int32(0), "no order sent")
}

/* ── 降级路径：instruments 不可用不阻塞下单 ────────────────── */

func TestOKXSwapFallbackWhenInstrumentsDown(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	env.failInstruments.Store(true)
	o := newTestOKXAdapter()

	_, err := o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000.05, 0.05, 10, "long")
	btAssert(t, err == nil, "fallback must not block the order")
	body := env.body()
	btAssertEq(t, body["sz"], "0.050000", "legacy %.6f formatting (no conversion)")
	btAssertEq(t, body["px"], "50000.05", "legacy %.2f formatting")
	btAssertEq(t, env.orderHits.Load(), int32(1), "order still sent")
}

func TestOKXSwapUnknownInstrumentFallsBack(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	_, err := o.PlaceFuturesOrder("FOO-USDT-SWAP", "BUY", "LIMIT", 100, 1.23456789, 10, "long")
	btAssert(t, err == nil, "unknown instrument degrades, not blocks")
	btAssertEq(t, env.body()["sz"], "1.234568", "legacy formatting")
}

func TestOKXSpotUnknownSymbolFallsBack(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	_, err := o.PlaceOrder("FOOUSDT", "BUY", "LIMIT", 100, 1.23456789)
	btAssert(t, err == nil, "unknown symbol degrades, not blocks")
	btAssertEq(t, env.body()["sz"], "1.234568", "legacy formatting")
}

// 非 live 状态的 instId 被过滤 → 视同未知 symbol 降级。
func TestOKXSuspendedInstrumentFallsBack(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	_, err := o.PlaceOrder("OLD-USDT", "BUY", "LIMIT", 100, 5)
	btAssert(t, err == nil, "suspended instrument degrades, not blocks")
	btAssertEq(t, env.body()["sz"], "5.000000", "legacy formatting")
}

/* ── 缓存 TTL、降级与并发 ──────────────────────────────────── */

func TestOKXInstrumentsCacheTTLExpiry(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	sharedOKXInstruments.mu.Lock()
	sharedOKXInstruments.ttl = 50 * time.Millisecond
	sharedOKXInstruments.mu.Unlock()

	_, err := o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.05, 10, "long")
	btAssert(t, err == nil, "first order ok")
	btAssertEq(t, env.swapInstrumentsHits.Load(), int32(1), "first fetch")

	time.Sleep(80 * time.Millisecond) // 超过 TTL

	_, err = o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.05, 10, "long")
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.swapInstrumentsHits.Load(), int32(2), "TTL expired → refetch")
}

func TestOKXInstrumentsStaleOnRefreshFailure(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	sharedOKXInstruments.mu.Lock()
	sharedOKXInstruments.ttl = 50 * time.Millisecond
	sharedOKXInstruments.mu.Unlock()

	_, err := o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.05, 10, "long")
	btAssert(t, err == nil, "prime cache")

	env.failInstruments.Store(true)
	time.Sleep(80 * time.Millisecond)

	// 刷新失败但有过期缓存 → 用旧规则（ctVal 换算）而不是裸 %.6f。
	_, err = o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.07, 10, "long")
	btAssert(t, err == nil, "stale cache keeps conversion precise")
	btAssertEq(t, env.body()["sz"], "7", "stale filters still applied")
	btAssertEq(t, env.swapInstrumentsHits.Load(), int32(2), "refresh attempted once")
}

func TestOKXInstrumentsConcurrentOrders(t *testing.T) {
	env := newOKXOrderTestEnv(t)
	o := newTestOKXAdapter()

	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := o.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.05, 10, "long")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		btAssert(t, err == nil, "concurrent order ok")
	}
	btAssertEq(t, env.swapInstrumentsHits.Load(), int32(1), "single fetch under concurrency")
	btAssertEq(t, env.orderHits.Load(), int32(n), "all orders sent")
	btAssertEq(t, env.body()["sz"], "5", "conversion under concurrency")
}

/* ── instruments 解析与换算单元测试 ────────────────────────── */

func TestParseOKXInstrumentsVariants(t *testing.T) {
	swap, err := parseOKXInstruments([]byte(okxSwapInstrumentsFixture))
	btAssert(t, err == nil, "parse swap")
	btc := swap["BTC-USDT-SWAP"]
	btAssertEq(t, btc.StepSize, "1", "lotSz")
	btAssertEq(t, btc.MinQty, "1", "minSz")
	btAssertEq(t, btc.MaxQty, "100", "maxSz")
	btAssertEq(t, btc.TickSize, "0.1", "tickSz")
	btAssertEq(t, btc.CtVal, "0.01", "ctVal")
	btAssertEq(t, btc.CtValCcy, "BTC", "ctValCcy")
	btAssertEq(t, swap["DOGE-USDT-SWAP"].CtVal, "5.0", "ctVal×ctMult effective face value")

	spot, err := parseOKXInstruments([]byte(okxSpotInstrumentsFixture))
	btAssert(t, err == nil, "parse spot")
	btAssertEq(t, spot["BTC-USDT"].CtVal, "", "spot has no ctVal")
	if _, ok := spot["OLD-USDT"]; ok {
		t.Fatal("non-live instrument must be filtered")
	}

	// code 兼容数字 0。
	if _, err := parseOKXInstruments([]byte(`{"code":0,"data":[{"instId":"X-USDT","tickSz":"0.1"}]}`)); err != nil {
		t.Fatalf("numeric code 0 must be accepted: %v", err)
	}
	// 业务错误码。
	if _, err := parseOKXInstruments([]byte(`{"code":"51000","msg":"Parameter error"}`)); err == nil {
		t.Fatal("non-zero code must error")
	}
	// 字段缺失容错：无 lotSz/minSz 也能解析（下单路径跳过对应校验）。
	m, err := parseOKXInstruments([]byte(`{"code":"0","data":[{"instId":"Y-USDT","tickSz":"0.01"}]}`))
	btAssert(t, err == nil, "missing fields tolerated")
	btAssertEq(t, m["Y-USDT"].StepSize, "", "missing lotSz empty")

	if _, err := parseOKXInstruments([]byte(`{"code":"0","data":[]}`)); err == nil {
		t.Fatal("empty data must error")
	}
	if _, err := parseOKXInstruments([]byte(`{broken`)); err == nil {
		t.Fatal("broken JSON must error")
	}
}

func TestOKXCoinToContracts(t *testing.T) {
	c, err := okxCoinToContracts(0.05, "0.01")
	btAssert(t, err == nil, "no error")
	btAssertEq(t, c, 5.0, "0.05 BTC / 0.01 = 5")

	// 浮点尾差：0.03/0.01 的浮点结果略小于 3，由下游 FloorToStep 容差吸收。
	c, err = okxCoinToContracts(0.03, "0.01")
	btAssert(t, err == nil, "no error")
	btAssert(t, c > 2.999999 && c <= 3.0, "float tail near 3")

	if _, err := okxCoinToContracts(1, "0"); err == nil {
		t.Fatal("zero ctVal must error")
	}
	if _, err := okxCoinToContracts(1, "abc"); err == nil {
		t.Fatal("non-numeric ctVal must error")
	}
}
