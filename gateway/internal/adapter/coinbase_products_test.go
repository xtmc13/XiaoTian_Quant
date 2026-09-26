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

/* ── Coinbase products + 下单的 mock 交易所 ─────────────────── */

// coinbaseOrderTestEnv 用 httptest 模拟 Coinbase Advanced Trade REST：
//   - GET  /products → 产品规则（可配置故障）
//   - POST /orders   → 记录 JSON 请求体并返回 order_id
type coinbaseOrderTestEnv struct {
	srv *httptest.Server

	productsHits atomic.Int32
	ordersHits   atomic.Int32
	failProducts atomic.Bool

	mu       sync.Mutex
	lastBody map[string]any
}

const coinbaseProductsFixture = `{
  "products": [
    {"product_id": "BTC-USD", "base_increment": "0.00000001", "quote_increment": "0.01",
     "base_min_size": "0.00001600", "base_max_size": "280", "quote_min_size": "1.00"},
    {"product_id": "ETH-USD", "base_increment": "0.0001", "quote_increment": "0.01",
     "base_min_size": "0.001", "quote_min_size": "1.00"}
  ],
  "num_products": 2
}`

func newCoinbaseOrderTestEnv(t *testing.T) *coinbaseOrderTestEnv {
	t.Helper()
	env := &coinbaseOrderTestEnv{}
	mux := http.NewServeMux()

	mux.HandleFunc("/products", func(w http.ResponseWriter, r *http.Request) {
		env.productsHits.Add(1)
		if env.failProducts.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, coinbaseProductsFixture)
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
		fmt.Fprint(w, `{"order_id":"cb-order-1","success":true}`)
	})

	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)

	// 指向 mock 交易所 + 清空进程级规则缓存（用例隔离）。
	t.Setenv("COINBASE_REST_URL", env.srv.URL)
	resetCoinbaseProductCache()
	t.Cleanup(resetCoinbaseProductCache)
	return env
}

// orderConfig 返回最近一次下单的 order_configuration.limit_gtc 子对象。
func (e *coinbaseOrderTestEnv) orderConfig(key string) map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	cfg, _ := e.lastBody["order_configuration"].(map[string]any)
	sub, _ := cfg[key].(map[string]any)
	return sub
}

/* ── 精度规整（increment 十进制串，step 模式）────────────────── */

// BTC-USD：base_increment=0.00000001、quote_increment=0.01 —— 修复前硬编码
// %.6f/%.2f，修复后数量 floor 到 8 位、价格 round 到 2 位。
func TestCoinbasePlaceOrderPrecisionNormalization(t *testing.T) {
	env := newCoinbaseOrderTestEnv(t)
	cb := NewCoinbaseAdapter("key", "secret")

	_, err := cb.PlaceOrder("BTC-USD", "BUY", "LIMIT", 50000.056, 0.123456789)
	btAssert(t, err == nil, "order should succeed")

	cfg := env.orderConfig("limit_gtc")
	btAssertEq(t, cfg["base_size"], "0.12345678", "qty floored to base_increment")
	btAssertEq(t, cfg["limit_price"], "50000.06", "price rounded to quote_increment")
	btAssertEq(t, env.productsHits.Load(), int32(1), "products fetched once")

	// 第二单命中缓存：不再请求 products。
	_, err = cb.PlaceOrder("BTC-USD", "BUY", "LIMIT", 49999.994, 0.987654321)
	btAssert(t, err == nil, "second order ok")
	cfg = env.orderConfig("limit_gtc")
	btAssertEq(t, cfg["base_size"], "0.98765432", "second qty floored")
	btAssertEq(t, cfg["limit_price"], "49999.99", "second price rounded")
	btAssertEq(t, env.productsHits.Load(), int32(1), "cache hit, no refetch")
}

// 浮点尾差：0.1+0.2 不得以 "0.30000000000000004" 上线。
func TestCoinbasePlaceOrderFloatTail(t *testing.T) {
	env := newCoinbaseOrderTestEnv(t)
	cb := NewCoinbaseAdapter("key", "secret")

	qty := 0.1 + 0.2
	_, err := cb.PlaceOrder("BTC-USD", "BUY", "LIMIT", 60000, qty)
	btAssert(t, err == nil, "order ok")
	btAssertEq(t, env.orderConfig("limit_gtc")["base_size"], "0.30000000", "float tail cleaned on the wire")
}

/* ── 本地规则拒绝：不发注定失败的单 ─────────────────────────── */

func TestCoinbasePlaceOrderRejectsBelowMinSize(t *testing.T) {
	env := newCoinbaseOrderTestEnv(t)
	cb := NewCoinbaseAdapter("key", "secret")

	// BTC-USD base_min_size=0.000016：0.00001 floor 到 8 位仍是 0.00001 < min，本地拒绝。
	_, err := cb.PlaceOrder("BTC-USD", "BUY", "LIMIT", 60000, 0.00001)
	btAssert(t, err != nil, "must reject")
	var ce *OrderConstraintError
	btAssert(t, errors.As(err, &ce), "OrderConstraintError")
	btAssert(t, strings.Contains(err.Error(), "minQty"), "mentions minQty (base_min_size)")
	btAssertEq(t, env.ordersHits.Load(), int32(0), "no order sent to exchange")
}

func TestCoinbasePlaceOrderRejectsBelowMinNotional(t *testing.T) {
	env := newCoinbaseOrderTestEnv(t)
	cb := NewCoinbaseAdapter("key", "secret")

	// quote_min_size=1 USD：0.00002 × 100 = 0.002 USD，本地拒绝。
	_, err := cb.PlaceOrder("BTC-USD", "BUY", "LIMIT", 100, 0.00002)
	btAssert(t, err != nil, "must reject")
	btAssert(t, strings.Contains(err.Error(), "minNotional"), "mentions minNotional (quote_min_size)")
	btAssertEq(t, env.ordersHits.Load(), int32(0), "no order sent to exchange")
}

/* ── 降级路径：products 不可用不阻塞下单 ────────────────────── */

func TestCoinbasePlaceOrderFallbackWhenProductsDown(t *testing.T) {
	env := newCoinbaseOrderTestEnv(t)
	env.failProducts.Store(true)
	cb := NewCoinbaseAdapter("key", "secret")

	// 规则拉取失败 → 记 WARN 并退回旧格式（%.6f/%.2f），订单仍然发出。
	_, err := cb.PlaceOrder("BTC-USD", "BUY", "LIMIT", 50000.056, 0.123456789)
	btAssert(t, err == nil, "fallback must not block the order")
	cfg := env.orderConfig("limit_gtc")
	btAssertEq(t, cfg["base_size"], "0.123457", "legacy %.6f formatting")
	btAssertEq(t, cfg["limit_price"], "50000.06", "legacy %.2f formatting")
	btAssertEq(t, env.ordersHits.Load(), int32(1), "order still sent")
}

func TestCoinbasePlaceOrderUnknownProductFallsBack(t *testing.T) {
	env := newCoinbaseOrderTestEnv(t)
	cb := NewCoinbaseAdapter("key", "secret")

	_, err := cb.PlaceOrder("FOO-USD", "BUY", "LIMIT", 100, 1.23456789)
	btAssert(t, err == nil, "unknown product degrades, not blocks")
	btAssertEq(t, env.orderConfig("limit_gtc")["base_size"], "1.234568", "legacy formatting")
}

/* ── 缓存 TTL 与陈旧降级 ────────────────────────────────────── */

func TestCoinbaseProductsCacheTTLExpiry(t *testing.T) {
	env := newCoinbaseOrderTestEnv(t)
	cb := NewCoinbaseAdapter("key", "secret")

	sharedCoinbaseProducts.mu.Lock()
	sharedCoinbaseProducts.ttl = 50 * time.Millisecond
	sharedCoinbaseProducts.mu.Unlock()

	_, err := cb.PlaceOrder("BTC-USD", "BUY", "LIMIT", 60000, 0.5)
	btAssert(t, err == nil, "first order ok")
	btAssertEq(t, env.productsHits.Load(), int32(1), "first fetch")

	time.Sleep(80 * time.Millisecond) // 超过 TTL

	_, err = cb.PlaceOrder("BTC-USD", "BUY", "LIMIT", 60000, 0.5)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.productsHits.Load(), int32(2), "TTL expired → refetch")
}

func TestCoinbaseProductsStaleOnRefreshFailure(t *testing.T) {
	env := newCoinbaseOrderTestEnv(t)
	cb := NewCoinbaseAdapter("key", "secret")

	sharedCoinbaseProducts.mu.Lock()
	sharedCoinbaseProducts.ttl = 50 * time.Millisecond
	sharedCoinbaseProducts.mu.Unlock()

	_, err := cb.PlaceOrder("BTC-USD", "BUY", "LIMIT", 60000, 0.5)
	btAssert(t, err == nil, "prime cache")
	btAssertEq(t, env.productsHits.Load(), int32(1), "primed")

	env.failProducts.Store(true)
	time.Sleep(80 * time.Millisecond)

	// 刷新失败但有过期缓存 → 用旧规则（8 位 increment）而不是裸 %.6f。
	_, err = cb.PlaceOrder("BTC-USD", "BUY", "LIMIT", 60000, 0.123456789)
	btAssert(t, err == nil, "stale cache keeps order precise")
	btAssertEq(t, env.orderConfig("limit_gtc")["base_size"], "0.12345678", "stale filters still applied")
	btAssertEq(t, env.productsHits.Load(), int32(2), "refresh attempted once")
}

/* ── products 解析容错 ──────────────────────────────────────── */

func TestParseCoinbaseProductsVariants(t *testing.T) {
	rules, err := parseCoinbaseProducts(mustJSON(t, coinbaseProductsFixture))
	btAssert(t, err == nil, "parse fixture")
	btc := rules["BTC-USD"]
	btAssertEq(t, btc.StepSize, "0.00000001", "base_increment → StepSize")
	btAssertEq(t, btc.TickSize, "0.01", "quote_increment → TickSize")
	btAssertEq(t, btc.MinQty, "0.00001600", "base_min_size → MinQty")
	btAssertEq(t, btc.MinNotional, "1.00", "quote_min_size → MinNotional")
	btAssertEq(t, btc.MaxQty, "280", "base_max_size → MaxQty")

	// 缺 product_id 的条目跳过；字段缺失留空（对应校验跳过）。
	variant := map[string]any{"products": []any{
		map[string]any{"quote_increment": "0.01"}, // no product_id
		map[string]any{"product_id": "X-USD"},
	}}
	r2, err := parseCoinbaseProducts(variant)
	btAssert(t, err == nil, "parse variant")
	btAssertEq(t, len(r2), 1, "id-less entry skipped")
	btAssertEq(t, r2["X-USD"].StepSize, "", "missing fields stay empty")

	if _, err := parseCoinbaseProducts(map[string]any{}); err == nil {
		t.Fatal("empty response must error")
	}
	if _, err := parseCoinbaseProducts(map[string]any{"products": []any{}}); err == nil {
		t.Fatal("empty products must error")
	}
}

func mustJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}
