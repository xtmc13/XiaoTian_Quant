package dataprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── 7. CoinGecko 市值源 ──

const coinGeckoMarketsFixture = `[
  {"id":"bitcoin","symbol":"btc","name":"Bitcoin","current_price":67000.5,"market_cap":1320000000000,"market_cap_rank":1,"total_volume":25000000000,"price_change_percentage_24h":1.23},
  {"id":"ethereum","symbol":"eth","name":"Ethereum","current_price":3500.1,"market_cap":420000000000,"market_cap_rank":2,"total_volume":12000000000,"price_change_percentage_24h":-0.45},
  {"id":"solana","symbol":"sol","name":"Solana","current_price":150.2,"market_cap":70000000000,"market_cap_rank":5,"total_volume":3000000000,"price_change_percentage_24h":null}
]`

func TestCoinGeckoFetch(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/coins/markets" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, coinGeckoMarketsFixture)
	}))
	defer srv.Close()

	src := newCoinGeckoSource(Config{BaseURLs: map[string]string{CoinGeckoSourceName: srv.URL}}, testClient())
	if src.RequiresKey() || !src.Configured() {
		t.Fatal("coingecko should be keyless and always configured")
	}
	if src.Name() != CoinGeckoSourceName {
		t.Fatalf("name: %s", src.Name())
	}
	data, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	mk, ok := data.(*CoinGeckoMarketsData)
	if !ok {
		t.Fatalf("unexpected type %T", data)
	}
	if len(mk.Coins) != 3 {
		t.Fatalf("coins: %d", len(mk.Coins))
	}
	btc := mk.Coins[0]
	if btc.Symbol != "BTC" || btc.MarketCapRank != 1 || btc.MarketCap != 1.32e12 || btc.CurrentPrice != 67000.5 {
		t.Fatalf("bad btc row: %+v", btc)
	}
	if btc.PriceChangePct24h != 1.23 || mk.Coins[2].PriceChangePct24h != 0 {
		t.Fatalf("bad 24h change parse: %+v / %+v", btc, mk.Coins[2])
	}
	// 查询参数：按市值降序、带 24h 涨跌幅
	for _, want := range []string{"order=market_cap_desc", "price_change_percentage=24h", "vs_currency=usd"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestCoinGeckoRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	src := newCoinGeckoSource(Config{BaseURLs: map[string]string{CoinGeckoSourceName: srv.URL}}, testClient())
	_, err := src.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("429 should produce explicit rate-limit error, got: %v", err)
	}
}

func TestCoinGeckoEmptyPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	src := newCoinGeckoSource(Config{BaseURLs: map[string]string{CoinGeckoSourceName: srv.URL}}, testClient())
	if _, err := src.Fetch(context.Background()); err == nil {
		t.Fatal("empty payload should be explicit error")
	}
}

// TestCoinGeckoServiceResilience 走 Service 全链路验证韧性件：
// TTL 缓存命中（不再打上游）→ 上游故障连续失败熔断开闸（明确降级）→ 限流拦截。
func TestCoinGeckoServiceResilience(t *testing.T) {
	var calls atomic.Int32
	var failMode atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if failMode.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, coinGeckoMarketsFixture)
	}))
	defer srv.Close()

	src := newCoinGeckoSource(Config{BaseURLs: map[string]string{CoinGeckoSourceName: srv.URL}}, testClient())
	src.minInterval = time.Millisecond // 测试加速：限流间隔缩到 1ms
	svc := NewService([]Source{src}, nil, nil)

	// 1) 首次同步拉取成功
	res := svc.Get(context.Background(), CoinGeckoSourceName)
	if res.Status != "ok" {
		t.Fatalf("first get: %s (%s)", res.Status, res.Error)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls: %d", calls.Load())
	}

	// 2) TTL 内第二次命中缓存，不打上游
	res2 := svc.Get(context.Background(), CoinGeckoSourceName)
	if res2.Status != "ok" || calls.Load() != 1 {
		t.Fatalf("TTL cache should serve without refetch: status=%s calls=%d", res2.Status, calls.Load())
	}

	// 3) 上游故障 + 无缓存的新 service：连续失败 → 熔断开闸 → 明确 unavailable
	failMode.Store(true)
	src2 := newCoinGeckoSource(Config{BaseURLs: map[string]string{CoinGeckoSourceName: srv.URL}}, testClient())
	src2.minInterval = time.Millisecond
	svc2 := NewService([]Source{src2}, nil, nil)

	for i := 0; i < 3; i++ {
		r := svc2.Get(context.Background(), CoinGeckoSourceName)
		if r.Status != "unavailable" {
			t.Fatalf("failure round %d should be unavailable, got %s", i, r.Status)
		}
		time.Sleep(2 * time.Millisecond) // 越过限流间隔
	}
	// 熔断开闸后不再打上游，直接明确降级
	before := calls.Load()
	r := svc2.Get(context.Background(), CoinGeckoSourceName)
	if r.Status != "unavailable" || !strings.Contains(r.Error, "circuit open") {
		t.Fatalf("breaker open should short-circuit: %+v", r)
	}
	if calls.Load() != before {
		t.Fatalf("breaker open must not hit upstream: calls %d → %d", before, calls.Load())
	}
	health := svc2.Health()
	if len(health) != 1 || health[0].Circuit != "open" || health[0].Failures < 3 {
		t.Fatalf("health should report open breaker: %+v", health)
	}

	// 4) 限流：全新 service 连续两次 Get（间隔 < MinInterval），第二次被拦截为 refreshing
	src3 := newCoinGeckoSource(Config{BaseURLs: map[string]string{CoinGeckoSourceName: srv.URL}}, testClient())
	src3.minInterval = time.Hour // 放大限流间隔确保第二次必被拦截
	svc3 := NewService([]Source{src3}, nil, nil)
	failMode.Store(false)
	r1 := svc3.Get(context.Background(), CoinGeckoSourceName)
	if r1.Status != "ok" {
		t.Fatalf("rate-limit setup fetch: %s (%s)", r1.Status, r1.Error)
	}
	// 制造"有旧数据但过期"较难（TTL 30min），改验证无数据场景下的限流拦截：
	src4 := newCoinGeckoSource(Config{BaseURLs: map[string]string{CoinGeckoSourceName: srv.URL}}, testClient())
	src4.minInterval = time.Hour
	svc4 := NewService([]Source{src4}, nil, nil)
	failMode.Store(true)
	r4a := svc4.Get(context.Background(), CoinGeckoSourceName) // 失败，记录限流时间戳
	if r4a.Status != "unavailable" {
		t.Fatalf("expected unavailable, got %s", r4a.Status)
	}
	r4b := svc4.Get(context.Background(), CoinGeckoSourceName) // 立即再来：被限流
	if r4b.Status != "refreshing" {
		t.Fatalf("second fetch within MinInterval should be rate-limited (refreshing), got %s (%s)", r4b.Status, r4b.Error)
	}
}
