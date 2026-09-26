package adapter

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

/* ── Kraken AssetPairs + 下单的 mock 交易所 ─────────────────── */

// krakenOrderTestEnv 用 httptest 模拟 Kraken REST：
//   - GET  /0/public/AssetPairs  → 交易对规则（可配置故障）
//   - POST /0/private/AddOrder   → 记录表单并返回 txid
type krakenOrderTestEnv struct {
	srv *httptest.Server

	assetPairsHits atomic.Int32
	addOrderHits   atomic.Int32
	failAssetPairs atomic.Bool

	mu       sync.Mutex
	lastForm url.Values
}

const krakenAssetPairsFixture = `{
  "error": [],
  "result": {
    "XXBTZUSD": {"altname": "XBTUSD", "wsname": "XBT/USD", "pair_decimals": 1, "lot_decimals": 5, "ordermin": "0.0001"},
    "XETHZUSD": {"altname": "ETHUSD", "wsname": "ETH/USD", "pair_decimals": 2, "lot_decimals": 4, "ordermin": "0.01"},
    "XXBTZUSD.d": {"altname": "XBTUSD.d", "pair_decimals": 1, "lot_decimals": 5, "ordermin": "0.002"}
  }
}`

func newKrakenOrderTestEnv(t *testing.T) *krakenOrderTestEnv {
	t.Helper()
	env := &krakenOrderTestEnv{}
	mux := http.NewServeMux()

	mux.HandleFunc("/0/public/AssetPairs", func(w http.ResponseWriter, r *http.Request) {
		env.assetPairsHits.Add(1)
		if env.failAssetPairs.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, krakenAssetPairsFixture)
	})
	mux.HandleFunc("/0/private/AddOrder", func(w http.ResponseWriter, r *http.Request) {
		env.addOrderHits.Add(1)
		_ = r.ParseForm()
		env.mu.Lock()
		env.lastForm = r.Form
		env.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":[],"result":{"txid":["OKRAKEN-1"],"descr":{"order":"buy 0.1 XBTUSD @ limit 50000"}}}`)
	})

	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)

	// 指向 mock 交易所 + 清空进程级规则缓存（用例隔离）。
	t.Setenv("KRAKEN_REST_URL", env.srv.URL)
	resetKrakenAssetPairsCache()
	t.Cleanup(resetKrakenAssetPairsCache)
	return env
}

func (e *krakenOrderTestEnv) form() url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastForm
}

/* ── 精度规整（核心断言：发到线上的 volume/price 串）─────────── */

// XXBTZUSD：pair_decimals=1、lot_decimals=5 —— 修复前硬编码 %.6f/%.2f，
// 修复后必须按位数取整：数量 floor 5 位、价格 round 1 位。
func TestKrakenPlaceOrderPrecisionNormalization(t *testing.T) {
	env := newKrakenOrderTestEnv(t)
	k := NewKrakenAdapter("key", "c2VjcmV0") // base64 "secret"

	_, err := k.PlaceOrder("BTC/USD", "BUY", "LIMIT", 50000.05, 0.1234567)
	btAssert(t, err == nil, "order should succeed")

	form := env.form()
	btAssertEq(t, form.Get("pair"), "XXBTZUSD", "kraken pair")
	btAssertEq(t, form.Get("volume"), "0.12345", "qty floored to lot_decimals 5")
	btAssertEq(t, form.Get("price"), "50000.1", "price rounded half-up to pair_decimals 1")
	btAssertEq(t, env.assetPairsHits.Load(), int32(1), "AssetPairs fetched once")

	// 第二单命中缓存：不再请求 AssetPairs。
	_, err = k.PlaceOrder("BTC/USD", "BUY", "LIMIT", 49999.96, 0.9876543)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.form().Get("volume"), "0.98765", "second qty floored")
	btAssertEq(t, env.form().Get("price"), "50000.0", "second price rounded")
	btAssertEq(t, env.assetPairsHits.Load(), int32(1), "cache hit, no refetch")
}

// XETHZUSD：lot_decimals=4、pair_decimals=2。
func TestKrakenPlaceOrderETHPairDecimals(t *testing.T) {
	env := newKrakenOrderTestEnv(t)
	k := NewKrakenAdapter("key", "c2VjcmV0")

	_, err := k.PlaceOrder("ETH/USD", "SELL", "LIMIT", 3000.056, 1.23456789)
	btAssert(t, err == nil, "order ok")
	form := env.form()
	btAssertEq(t, form.Get("pair"), "XETHZUSD", "kraken pair")
	btAssertEq(t, form.Get("volume"), "1.2345", "qty floored to lot_decimals 4")
	btAssertEq(t, form.Get("price"), "3000.06", "price rounded to pair_decimals 2")
}

// 市价单：只规整数量，不带 price。
func TestKrakenPlaceMarketOrderNoPrice(t *testing.T) {
	env := newKrakenOrderTestEnv(t)
	k := NewKrakenAdapter("key", "c2VjcmV0")

	_, err := k.PlaceOrder("BTC/USD", "BUY", "MARKET", 0, 0.1234567)
	btAssert(t, err == nil, "market order ok")
	form := env.form()
	btAssertEq(t, form.Get("volume"), "0.12345", "market qty floored")
	btAssertEq(t, form.Get("price"), "", "market order carries no price")
}

/* ── 本地规则拒绝：不发注定失败的单 ─────────────────────────── */

func TestKrakenPlaceOrderRejectsBelowOrderMin(t *testing.T) {
	env := newKrakenOrderTestEnv(t)
	k := NewKrakenAdapter("key", "c2VjcmV0")

	// XXBTZUSD ordermin=0.0001：0.000099 floor 5 位 → 0.00009 < ordermin，本地拒绝。
	_, err := k.PlaceOrder("BTC/USD", "BUY", "LIMIT", 50000, 0.000099)
	btAssert(t, err != nil, "must reject")
	var ce *OrderConstraintError
	btAssert(t, errors.As(err, &ce), "OrderConstraintError")
	btAssert(t, strings.Contains(err.Error(), "ordermin"), "mentions ordermin")
	btAssertEq(t, env.addOrderHits.Load(), int32(0), "no order sent to exchange")
}

/* ── 降级路径：AssetPairs 不可用不阻塞下单 ──────────────────── */

func TestKrakenPlaceOrderFallbackWhenAssetPairsDown(t *testing.T) {
	env := newKrakenOrderTestEnv(t)
	env.failAssetPairs.Store(true)
	k := NewKrakenAdapter("key", "c2VjcmV0")

	// 规则拉取失败 → 记 WARN 并退回旧格式（%.6f/%.2f），订单仍然发出。
	_, err := k.PlaceOrder("BTC/USD", "BUY", "LIMIT", 50000.05, 0.1234567)
	btAssert(t, err == nil, "fallback must not block the order")
	form := env.form()
	btAssertEq(t, form.Get("volume"), "0.123457", "legacy %.6f formatting")
	btAssertEq(t, form.Get("price"), "50000.05", "legacy %.2f formatting")
	btAssertEq(t, env.addOrderHits.Load(), int32(1), "order still sent")
}

func TestKrakenPlaceOrderUnknownPairFallsBack(t *testing.T) {
	env := newKrakenOrderTestEnv(t)
	k := NewKrakenAdapter("key", "c2VjcmV0")

	// SOLZUSD 不在 fixture 里：降级为旧格式而不是阻塞。
	_, err := k.PlaceOrder("SOL/USD", "BUY", "LIMIT", 100, 1.23456789)
	btAssert(t, err == nil, "unknown pair degrades, not blocks")
	btAssertEq(t, env.form().Get("volume"), "1.234568", "legacy formatting")
}

/* ── 缓存 TTL 与陈旧降级 ────────────────────────────────────── */

func TestKrakenAssetPairsCacheTTLExpiry(t *testing.T) {
	env := newKrakenOrderTestEnv(t)
	k := NewKrakenAdapter("key", "c2VjcmV0")

	sharedKrakenAssetPairs.mu.Lock()
	sharedKrakenAssetPairs.ttl = 50 * time.Millisecond
	sharedKrakenAssetPairs.mu.Unlock()

	_, err := k.PlaceOrder("BTC/USD", "BUY", "LIMIT", 50000, 0.5)
	btAssert(t, err == nil, "first order ok")
	btAssertEq(t, env.assetPairsHits.Load(), int32(1), "first fetch")

	time.Sleep(80 * time.Millisecond) // 超过 TTL

	_, err = k.PlaceOrder("BTC/USD", "BUY", "LIMIT", 50000, 0.5)
	btAssert(t, err == nil, "second order ok")
	btAssertEq(t, env.assetPairsHits.Load(), int32(2), "TTL expired → refetch")
}

func TestKrakenAssetPairsStaleOnRefreshFailure(t *testing.T) {
	env := newKrakenOrderTestEnv(t)
	k := NewKrakenAdapter("key", "c2VjcmV0")

	sharedKrakenAssetPairs.mu.Lock()
	sharedKrakenAssetPairs.ttl = 50 * time.Millisecond
	sharedKrakenAssetPairs.mu.Unlock()

	_, err := k.PlaceOrder("BTC/USD", "BUY", "LIMIT", 50000, 0.5)
	btAssert(t, err == nil, "prime cache")
	btAssertEq(t, env.assetPairsHits.Load(), int32(1), "primed")

	env.failAssetPairs.Store(true)
	time.Sleep(80 * time.Millisecond)

	// 刷新失败但有过期缓存 → 用旧规则（lot_decimals 5）而不是裸 %.6f。
	_, err = k.PlaceOrder("BTC/USD", "BUY", "LIMIT", 50000, 0.1234567)
	btAssert(t, err == nil, "stale cache keeps order precise")
	btAssertEq(t, env.form().Get("volume"), "0.12345", "stale rules still applied")
	btAssertEq(t, env.assetPairsHits.Load(), int32(2), "refresh attempted once")
}

/* ── AssetPairs 解析容错 ────────────────────────────────────── */

func TestParseKrakenAssetPairsVariants(t *testing.T) {
	rules, err := parseKrakenAssetPairs([]byte(krakenAssetPairsFixture))
	btAssert(t, err == nil, "parse fixture")
	btc := rules["XXBTZUSD"]
	btAssertEq(t, btc.priceDecimals, 1, "pair_decimals")
	btAssertEq(t, btc.lotDecimals, 5, "lot_decimals")
	btAssertEq(t, btc.orderMin, "0.0001", "ordermin")
	btAssertEq(t, rules["XBTUSD"].lotDecimals, 5, "altname alias resolves")
	if _, ok := rules["XXBTZUSD.d"]; ok {
		t.Fatal(".d dark-pool entry must be skipped")
	}

	// ordermin 以数字（非串）返回、缺字段、decimals 为数字串都要容错。
	variant := `{"error":[],"result":{"FOOUSD":{"altname":"FOOUSD","pair_decimals":"3","lot_decimals":2,"ordermin":1.5}}}`
	r2, err := parseKrakenAssetPairs([]byte(variant))
	btAssert(t, err == nil, "parse variant")
	btAssertEq(t, r2["FOOUSD"].priceDecimals, 3, "string decimals tolerated")
	btAssertEq(t, r2["FOOUSD"].orderMin, "1.5", "numeric ordermin tolerated")

	if _, err := parseKrakenAssetPairs([]byte(`{"error":["EAPI:Rate limit exceeded"]}`)); err == nil {
		t.Fatal("kraken error array must surface")
	}
	if _, err := parseKrakenAssetPairs([]byte(`{"error":[],"result":{}}`)); err == nil {
		t.Fatal("empty result must error")
	}
	if _, err := parseKrakenAssetPairs([]byte(`{broken`)); err == nil {
		t.Fatal("broken JSON must error")
	}
}
