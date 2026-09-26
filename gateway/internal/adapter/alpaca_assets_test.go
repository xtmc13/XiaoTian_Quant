package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

/* ── Alpaca assets + 下单的 mock 券商 ───────────────────────── */

// alpacaOrderTestEnv 用 httptest 模拟 Alpaca Trading API：
//   - GET  /assets/{symbol} → 资产规则（fractionable，可配置故障）
//   - POST /orders          → 记录 JSON 请求体并返回订单 id
type alpacaOrderTestEnv struct {
	srv *httptest.Server

	assetsHits atomic.Int32
	ordersHits atomic.Int32
	failAssets atomic.Bool

	mu       sync.Mutex
	lastBody map[string]any
}

func newAlpacaOrderTestEnv(t *testing.T) *alpacaOrderTestEnv {
	t.Helper()
	env := &alpacaOrderTestEnv{}
	mux := http.NewServeMux()

	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		env.assetsHits.Add(1)
		if env.failAssets.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		symbol := strings.TrimPrefix(r.URL.Path, "/assets/")
		w.Header().Set("Content-Type", "application/json")
		switch strings.ToUpper(symbol) {
		case "AAPL": // 整股资产：不可碎股
			fmt.Fprint(w, `{"symbol":"AAPL","fractionable":false,"tradable":true}`)
		case "XYZ": // 碎股资产
			fmt.Fprint(w, `{"symbol":"XYZ","fractionable":true,"tradable":true}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"code":40410000,"message":"asset not found"}`)
		}
	})
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		env.ordersHits.Add(1)
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		env.mu.Lock()
		env.lastBody = parsed
		env.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"alp-order-1","symbol":"AAPL","status":"accepted"}`)
	})

	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)

	// 指向 mock 券商 + 清空进程级规则缓存（用例隔离）。
	t.Setenv("ALPACA_REST_URL", env.srv.URL)
	resetAlpacaAssetCache()
	t.Cleanup(resetAlpacaAssetCache)
	return env
}

func (e *alpacaOrderTestEnv) body() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastBody
}

/* ── 精度规整（fractionable 语义 + 美股 min tick）────────────── */

// 非碎股资产：qty 必须整数 —— 10.9 FLOOR 到 "10"；价格按 0.01 tick 取整。
func TestAlpacaPlaceOrderNonFractionableIntegerQty(t *testing.T) {
	env := newAlpacaOrderTestEnv(t)
	a := NewAlpacaAdapter("key", "secret", true)

	_, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190.256, 10.9)
	btAssert(t, err == nil, "order should succeed")

	body := env.body()
	btAssertEq(t, body["qty"], "10", "non-fractionable qty floored to integer")
	btAssertEq(t, body["limit_price"], "190.26", "price rounded to 0.01 tick")
	btAssertEq(t, env.assetsHits.Load(), int32(1), "asset fetched once")

	// 第二单命中缓存：不再请求 assets。
	_, err = a.PlaceOrder("AAPL", "BUY", "LIMIT", 190.254, 5.5)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.body()["qty"], "5", "second qty floored")
	btAssertEq(t, env.body()["limit_price"], "190.25", "second price rounded")
	btAssertEq(t, env.assetsHits.Load(), int32(1), "cache hit, no refetch")
}

// 非碎股资产的碎尘数量（0.5 股）floor 后为 0：本地拒绝，不发请求。
func TestAlpacaPlaceOrderRejectsSubShareNonFractionable(t *testing.T) {
	env := newAlpacaOrderTestEnv(t)
	a := NewAlpacaAdapter("key", "secret", true)

	_, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190, 0.5)
	btAssert(t, err != nil, "must reject")
	var ce *OrderConstraintError
	btAssert(t, errors.As(err, &ce), "OrderConstraintError")
	btAssert(t, strings.Contains(err.Error(), "integer qty"), "mentions integer qty rule")
	btAssertEq(t, env.ordersHits.Load(), int32(0), "no order sent to broker")
}

// 碎股资产：维持 6 位小数（Alpaca 碎股支持到 9 位，6 位是保守子集）。
func TestAlpacaPlaceOrderFractionableKeepsDecimals(t *testing.T) {
	env := newAlpacaOrderTestEnv(t)
	a := NewAlpacaAdapter("key", "secret", true)

	_, err := a.PlaceOrder("XYZ", "SELL", "LIMIT", 12.3456, 1.23456789)
	btAssert(t, err == nil, "order ok")
	body := env.body()
	btAssertEq(t, body["qty"], "1.234568", "fractionable qty keeps %.6f")
	btAssertEq(t, body["limit_price"], "12.35", "price rounded to 0.01 tick")
}

// 市价单：只规整数量，不带 limit_price。
func TestAlpacaPlaceMarketOrderNoPrice(t *testing.T) {
	env := newAlpacaOrderTestEnv(t)
	a := NewAlpacaAdapter("key", "secret", true)

	_, err := a.PlaceOrder("AAPL", "BUY", "MARKET", 0, 3.7)
	btAssert(t, err == nil, "market order ok")
	body := env.body()
	btAssertEq(t, body["qty"], "3", "market qty floored to integer")
	if _, hasPrice := body["limit_price"]; hasPrice {
		t.Fatal("market order must not carry limit_price")
	}
}

/* ── 降级路径：assets 不可用不阻塞下单 ──────────────────────── */

func TestAlpacaPlaceOrderFallbackWhenAssetsDown(t *testing.T) {
	env := newAlpacaOrderTestEnv(t)
	env.failAssets.Store(true)
	a := NewAlpacaAdapter("key", "secret", true)

	// 规则拉取失败 → 记 WARN 并退回旧格式（%.6f/%.2f），订单仍然发出。
	_, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190.256, 10.9)
	btAssert(t, err == nil, "fallback must not block the order")
	body := env.body()
	btAssertEq(t, body["qty"], "10.900000", "legacy %.6f formatting")
	btAssertEq(t, body["limit_price"], "190.26", "legacy %.2f formatting")
	btAssertEq(t, env.ordersHits.Load(), int32(1), "order still sent")
}

func TestAlpacaPlaceOrderUnknownAssetFallsBack(t *testing.T) {
	env := newAlpacaOrderTestEnv(t)
	a := NewAlpacaAdapter("key", "secret", true)

	// 404 asset not found → 降级为旧格式而不是阻塞。
	_, err := a.PlaceOrder("NOPE", "BUY", "LIMIT", 10, 1.5)
	btAssert(t, err == nil, "unknown asset degrades, not blocks")
	btAssertEq(t, env.body()["qty"], "1.500000", "legacy formatting")
}

/* ── 缓存 TTL 与陈旧降级 ────────────────────────────────────── */

func TestAlpacaAssetCacheTTLExpiry(t *testing.T) {
	env := newAlpacaOrderTestEnv(t)
	a := NewAlpacaAdapter("key", "secret", true)

	sharedAlpacaAssets.mu.Lock()
	sharedAlpacaAssets.ttl = 50 * time.Millisecond
	sharedAlpacaAssets.mu.Unlock()

	_, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190, 5)
	btAssert(t, err == nil, "first order ok")
	btAssertEq(t, env.assetsHits.Load(), int32(1), "first fetch")

	time.Sleep(80 * time.Millisecond) // 超过 TTL

	_, err = a.PlaceOrder("AAPL", "BUY", "LIMIT", 190, 5)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.assetsHits.Load(), int32(2), "TTL expired → refetch")
}

func TestAlpacaAssetStaleOnRefreshFailure(t *testing.T) {
	env := newAlpacaOrderTestEnv(t)
	a := NewAlpacaAdapter("key", "secret", true)

	sharedAlpacaAssets.mu.Lock()
	sharedAlpacaAssets.ttl = 50 * time.Millisecond
	sharedAlpacaAssets.mu.Unlock()

	_, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190, 5)
	btAssert(t, err == nil, "prime cache")
	btAssertEq(t, env.assetsHits.Load(), int32(1), "primed")

	env.failAssets.Store(true)
	time.Sleep(80 * time.Millisecond)

	// 刷新失败但有过期缓存 → 仍按非碎股规则 floor 到整数，而不是裸 %.6f。
	_, err = a.PlaceOrder("AAPL", "BUY", "LIMIT", 190, 7.9)
	btAssert(t, err == nil, "stale cache keeps order precise")
	btAssertEq(t, env.body()["qty"], "7", "stale rules still applied")
	btAssertEq(t, env.assetsHits.Load(), int32(2), "refresh attempted once")
}

/* ── fetchAsset 解析容错 ────────────────────────────────────── */

func TestAlpacaFetchAssetMissingFractionable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/assets/OLD", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"symbol":"OLD","tradable":true}`) // 无 fractionable 字段
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("ALPACA_REST_URL", srv.URL)

	a := NewAlpacaAdapter("key", "secret", true)
	_, err := a.fetchAsset("OLD")
	btAssert(t, err != nil, "missing fractionable must error (fallback path takes over)")
	btAssert(t, strings.Contains(err.Error(), "fractionable"), "error mentions the field")
}
