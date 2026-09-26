package pairlist

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/dataprovider"
	"github.com/xiaotian-quant/gateway/internal/market"
)

// ── 测试桩 ──

// fakeDPSource 实现 dataprovider.Source（任意名字/载荷）。
type fakeDPSource struct {
	name string
	data any
	err  error
}

func (f *fakeDPSource) Name() string               { return f.name }
func (f *fakeDPSource) Description() string        { return "fake" }
func (f *fakeDPSource) TTL() time.Duration         { return time.Hour }
func (f *fakeDPSource) MinInterval() time.Duration { return 0 }
func (f *fakeDPSource) RequiresKey() bool          { return false }
func (f *fakeDPSource) Configured() bool           { return true }
func (f *fakeDPSource) Fetch(context.Context) (any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.data, nil
}

func fakeCoinGeckoService(data any, err error) func() *dataprovider.Service {
	svc := dataprovider.NewService([]dataprovider.Source{
		&fakeDPSource{name: dataprovider.CoinGeckoSourceName, data: data, err: err},
	}, nil, nil)
	return func() *dataprovider.Service { return svc }
}

// universeFixtureServer 起一个假的 Binance 公开 REST（ticker/24hr + exchangeInfo）。
func universeFixtureServer(t *testing.T, tickerHits *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/ticker/24hr":
			if tickerHits != nil {
				tickerHits.Add(1)
			}
			fmt.Fprint(w, `[
				{"symbol":"BTCUSDT","lastPrice":"67000.5","priceChangePercent":"1.5","quoteVolume":"900000000","highPrice":"68000","lowPrice":"65000","bidPrice":"67000","askPrice":"67001"},
				{"symbol":"ETHUSDT","lastPrice":"3500.1","priceChangePercent":"-2.0","quoteVolume":"400000000","highPrice":"3600","lowPrice":"3400","bidPrice":"3500","askPrice":"3501"},
				{"symbol":"SOLUSDT","lastPrice":"150.2","priceChangePercent":"8.0","quoteVolume":"100000000","highPrice":"155","lowPrice":"140","bidPrice":"150","askPrice":"150.2"},
				{"symbol":"DOGEUSDT","lastPrice":"0.12","priceChangePercent":"0.5","quoteVolume":"50000000","highPrice":"0.13","lowPrice":"0.11","bidPrice":"0.12","askPrice":"0.121"},
				{"symbol":"BTCBUSD","lastPrice":"66900","priceChangePercent":"1.4","quoteVolume":"1000","highPrice":"68000","lowPrice":"65000","bidPrice":"66900","askPrice":"66910"}
			]`)
		case "/exchangeInfo":
			fmt.Fprint(w, `{"symbols":[
				{"symbol":"BTCUSDT","baseAsset":"BTC","quoteAsset":"USDT","status":"TRADING","filters":[{"filterType":"PRICE_FILTER","tickSize":"0.01000000"},{"filterType":"LOT_SIZE","stepSize":"0.00001000"}]},
				{"symbol":"ETHUSDT","baseAsset":"ETH","quoteAsset":"USDT","status":"TRADING","filters":[{"filterType":"PRICE_FILTER","tickSize":"0.10000000"},{"filterType":"LOT_SIZE","stepSize":"0.00010000"}]},
				{"symbol":"SOLUSDT","baseAsset":"SOL","quoteAsset":"USDT","status":"TRADING","filters":[{"filterType":"PRICE_FILTER","tickSize":"0.01000000"},{"filterType":"LOT_SIZE","stepSize":"0.01000000"}]},
				{"symbol":"DOGEUSDT","baseAsset":"DOGE","quoteAsset":"USDT","status":"BREAK","filters":[]},
				{"symbol":"BTCBUSD","baseAsset":"BTC","quoteAsset":"BUSD","status":"TRADING","filters":[]}
			]}`)
		default:
			http.Error(w, "not found", 404)
		}
	}))
}

// ── BinanceUniverse ──

func TestBinanceUniverseMergeAndQuoteFilter(t *testing.T) {
	srv := universeFixtureServer(t, nil)
	defer srv.Close()
	u := NewBinanceUniverse(srv.URL, nil, time.Minute)

	pairs, err := u.Universe("binance", "USDT")
	if err != nil {
		t.Fatalf("universe: %v", err)
	}
	if len(pairs) != 4 { // BTCBUSD 被 quote 过滤
		t.Fatalf("USDT pairs: %d", len(pairs))
	}
	var btc *PairInfo
	for _, p := range pairs {
		if p.Symbol == "BTCUSDT" {
			btc = p
		}
	}
	if btc == nil {
		t.Fatalf("BTCUSDT missing: %+v", pairs)
	}
	if btc.BaseAsset != "BTC" || btc.QuoteAsset != "USDT" || btc.Status != "TRADING" {
		t.Fatalf("bad merge: %+v", btc)
	}
	if btc.Price != 67000.5 || btc.Volume24h != 9e8 || btc.PriceChange != 1.5 {
		t.Fatalf("bad ticker fields: %+v", btc)
	}
	if btc.Spread <= 0 || btc.Volatility <= 0 {
		t.Fatalf("spread/volatility should be computed: %+v", btc)
	}
	if btc.PricePrecision != 2 || btc.QtyPrecision != 5 {
		t.Fatalf("precision from tickSize/stepSize: %+v", btc)
	}
	// DOGEUSDT exchangeInfo 状态 BREAK 应覆盖 ticker 默认 TRADING
	for _, p := range pairs {
		if p.Symbol == "DOGEUSDT" && p.Status != "BREAK" {
			t.Fatalf("exchangeInfo status should override: %+v", p)
		}
	}

	// quote=BUSD
	busd, err := u.Universe("BINANCE", "BUSD") // exchange 大小写不敏感
	if err != nil || len(busd) != 1 || busd[0].Symbol != "BTCBUSD" {
		t.Fatalf("BUSD universe: %v err=%v", busd, err)
	}
}

func TestBinanceUniverseCacheAndStaleFallback(t *testing.T) {
	var tickerHits atomic.Int32
	var failMode atomic.Bool
	// 手工组合：先正常后故障
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failMode.Load() {
			http.Error(w, "boom", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/ticker/24hr":
			tickerHits.Add(1)
			fmt.Fprint(w, `[{"symbol":"BTCUSDT","lastPrice":"67000","priceChangePercent":"1.0","quoteVolume":"900000000","highPrice":"68000","lowPrice":"65000","bidPrice":"67000","askPrice":"67001"}]`)
		case "/exchangeInfo":
			fmt.Fprint(w, `{"symbols":[{"symbol":"BTCUSDT","baseAsset":"BTC","quoteAsset":"USDT","status":"TRADING","filters":[]}]}`)
		}
	}))
	defer srv.Close()

	u := NewBinanceUniverse(srv.URL, nil, time.Minute)
	if _, err := u.pairs(); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if tickerHits.Load() != 1 {
		t.Fatalf("ticker hits: %d", tickerHits.Load())
	}
	// TTL 内第二次：命中缓存不再打上游
	if _, err := u.pairs(); err != nil {
		t.Fatalf("cached fetch: %v", err)
	}
	if tickerHits.Load() != 1 {
		t.Fatalf("TTL cache should avoid refetch, hits=%d", tickerHits.Load())
	}
	// 缓存过期 + 上游故障 → 回旧缓存（降级不报 error）
	u.TTL = time.Millisecond
	time.Sleep(2 * time.Millisecond)
	failMode.Store(true)
	got, err := u.pairs()
	if err != nil || len(got) != 1 {
		t.Fatalf("stale fallback expected, got %v err=%v", got, err)
	}
	// 无缓存 + 上游故障 → 明确错误
	u2 := NewBinanceUniverse(srv.URL, nil, time.Minute)
	if _, err := u2.pairs(); err == nil || !strings.Contains(err.Error(), "ticker/24hr") {
		t.Fatalf("no-cache failure should be explicit error, got: %v", err)
	}
}

func TestBinanceUniverseUnsupportedExchange(t *testing.T) {
	u := NewBinanceUniverse("http://unused", nil, time.Minute)
	_, err := u.Universe("okx", "USDT")
	if err == nil || !strings.Contains(err.Error(), "未接线") {
		t.Fatalf("non-binance exchange should be explicit degradation error, got: %v", err)
	}
}

// ── K 线源 ──

func TestCandleProviderFromFeeder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/klines" {
			http.Error(w, "not found", 404)
			return
		}
		if r.URL.Query().Get("symbol") != "BTCUSDT" {
			t.Errorf("symbol param: %s", r.URL.Query().Get("symbol"))
		}
		w.Header().Set("Content-Type", "application/json")
		// 3 根：前两根已闭合，最后一根未闭合（应被剔除）
		fmt.Fprint(w, `[
			[1758700000000,"100","101","99","100.5","1000",0,0,0,0,0,0],
			[1758703600000,"100.5","103","100","102.5","1100",0,0,0,0,0,0],
			[1758707200000,"102.5","104","102","103.9","900",0,0,0,0,0,0]
		]`)
	}))
	defer srv.Close()

	feeder := market.NewKlineFeeder(nil)
	feeder.BaseURL = srv.URL
	cp := CandleProviderFromFeeder(feeder)
	candles, err := cp("BTCUSDT", "1h", 3)
	if err != nil {
		t.Fatalf("candles: %v", err)
	}
	if len(candles) != 2 { // 未闭合的最后一根被剔除
		t.Fatalf("closed candles: %d", len(candles))
	}
	if candles[0].Close != 100.5 || candles[1].Close != 102.5 || candles[0].Time >= candles[1].Time {
		t.Fatalf("candles should be ascending closed bars: %+v", candles)
	}
}

// ── CoinGecko MarketCapSource 适配 ──

func cgPayload() *dataprovider.CoinGeckoMarketsData {
	return &dataprovider.CoinGeckoMarketsData{Coins: []dataprovider.CoinGeckoCoin{
		{Symbol: "BTC", Name: "Bitcoin", MarketCapRank: 1, MarketCap: 1.3e12, CurrentPrice: 67000, TotalVolume: 25e9, PriceChangePct24h: 1.5},
		{Symbol: "ETH", Name: "Ethereum", MarketCapRank: 2, MarketCap: 4e11, CurrentPrice: 3500, TotalVolume: 12e9, PriceChangePct24h: -2},
		{Symbol: "SOL", Name: "Solana", MarketCapRank: 5, MarketCap: 7e10, CurrentPrice: 150, TotalVolume: 3e9},
		{Symbol: "DOGE", Name: "Dogecoin", MarketCapRank: 8, MarketCap: 2e10, CurrentPrice: 0.12, TotalVolume: 1e9},
		{Symbol: "NOTLISTED", Name: "Ghost", MarketCapRank: 9, MarketCap: 1e10, CurrentPrice: 1, TotalVolume: 1},
	}}
}

func TestCoinGeckoMarketCapSourceAdapter(t *testing.T) {
	srv := universeFixtureServer(t, nil)
	defer srv.Close()
	u := NewBinanceUniverse(srv.URL, nil, time.Minute)

	src := coinGeckoMarketCapSource(fakeCoinGeckoService(cgPayload(), nil), u)
	infos, err := src()
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	// NOTLISTED 不在交易所 universe → 剔除；DOGEUSDT 是 BREAK → 剔除；
	// BTC 有 USDT+BUSD 两个挂牌 → 都产出（Generate 时按 quote 过滤）
	symbols := map[string]bool{}
	for _, p := range infos {
		symbols[p.Symbol] = true
		if p.MarketCap <= 0 || p.Status != "TRADING" {
			t.Fatalf("bad pairinfo: %+v", p)
		}
	}
	for _, want := range []string{"BTCUSDT", "BTCBUSD", "ETHUSDT", "SOLUSDT"} {
		if !symbols[want] {
			t.Fatalf("%s missing in %v", want, symbols)
		}
	}
	if symbols["DOGEUSDT"] || symbols["NOTLISTEDUSDT"] {
		t.Fatalf("BREAK/未挂牌应被求交剔除: %v", symbols)
	}
}

func TestCoinGeckoMarketCapSourceDegradation(t *testing.T) {
	srv := universeFixtureServer(t, nil)
	defer srv.Close()
	u := NewBinanceUniverse(srv.URL, nil, time.Minute)

	// 上游失败且无缓存 → dataprovider status=unavailable → 明确错误
	src := coinGeckoMarketCapSource(fakeCoinGeckoService(nil, fmt.Errorf("coingecko 503")), u)
	if _, err := src(); err == nil || !strings.Contains(err.Error(), "CoinGecko") {
		t.Fatalf("upstream failure should surface explicit error, got: %v", err)
	}

	// 源未注册 → unknown_source
	emptySvc := dataprovider.NewService(nil, nil, nil)
	src2 := coinGeckoMarketCapSource(func() *dataprovider.Service { return emptySvc }, u)
	if _, err := src2(); err == nil {
		t.Fatalf("unregistered source should error")
	}
}

// ── WireProducer / 接线端到端 ──

func TestWireProducerEndToEnd(t *testing.T) {
	srv := universeFixtureServer(t, nil)
	defer srv.Close()
	u := NewBinanceUniverse(srv.URL, nil, time.Minute)

	deps := SourceDeps{
		MarketCap: coinGeckoMarketCapSource(fakeCoinGeckoService(cgPayload(), nil), u),
		Universe:  u.Universe,
		Candles: func(symbol, timeframe string, limit int) ([]Candle, error) {
			return []Candle{{Close: 100}, {Close: 110}}, nil // +10%
		},
		AllPairs: u.AllPairs,
		InfoMap:  u.InfoMap,
	}

	// MarketCapPairList：工厂构建（Source=nil）→ 接线 → 生成
	p, err := BuildProducerFromConfig("MarketCapPairList", map[string]any{
		"number_assets": 2, "max_rank": 10, "refresh_period_sec": 3600,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	WireProducer(p, deps)
	got, err := p.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(got) != 2 || got[0] != "BTCUSDT" || got[1] != "ETHUSDT" {
		t.Fatalf("marketcap top2 by quote=USDT: %v", got)
	}
	// quote=BUSD：只有 BTCBUSD 挂牌（新实例避开上一个 Generate 的 TTL 缓存）
	pBUSD, _ := BuildProducerFromConfig("MarketCapPairList", map[string]any{"number_assets": 10, "max_rank": 250})
	WireProducer(pBUSD, deps)
	gotBUSD, err := pBUSD.Generate("binance", "BUSD")
	if err != nil || len(gotBUSD) != 1 || gotBUSD[0] != "BTCBUSD" {
		t.Fatalf("BUSD quote filter: %v err=%v", gotBUSD, err)
	}

	// PercentChangePairList（ticker 模式）：universe 注入
	pc, err := BuildProducerFromConfig("PercentChangePairList", map[string]any{
		"number_assets": 2, "lookback_period": 0,
	})
	if err != nil {
		t.Fatalf("build pct: %v", err)
	}
	WireProducer(pc, deps)
	gotPct, err := pc.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("pct generate: %v", err)
	}
	// ticker 模式按 PriceChange 降序：SOL(8.0) > BTC(1.5)（DOGE BREAK 剔除，ETH -2.0 垫底）
	if len(gotPct) != 2 || gotPct[0] != "SOLUSDT" || gotPct[1] != "BTCUSDT" {
		t.Fatalf("pct ticker top2: %v", gotPct)
	}

	// PercentChangePairList（lookback 模式）：candles 注入
	pc2, _ := BuildProducerFromConfig("PercentChangePairList", map[string]any{
		"number_assets": 1, "lookback_period": 1, "lookback_timeframe": "1h",
	})
	WireProducer(pc2, deps)
	if _, err := pc2.Generate("binance", "USDT"); err != nil {
		t.Fatalf("pct lookback generate: %v", err)
	}

	// VolumePairList / PerformancePairList：AllPairs 注入
	vp, _ := BuildProducerFromConfig("VolumePairList", map[string]any{"top_n": 1})
	WireProducer(vp, deps)
	gotVol, err := vp.Generate("binance", "USDT")
	if err != nil || len(gotVol) != 1 || gotVol[0] != "BTCUSDT" { // 成交量最大
		t.Fatalf("volume top1: %v err=%v", gotVol, err)
	}

	// Manager 链：InfoMap 接线后过滤器拿到真实 PairInfo
	m := NewManager(DefaultManagerConfig())
	WireManager(m, deps)
	p2, _ := BuildProducerFromConfig("StaticPairList", map[string]any{"pairs": []any{"BTCUSDT", "ETHUSDT"}})
	WireProducer(p2, deps)
	m.AddProducer(p2)
	m.AddFilter(NewPriceFilter(1000, 0)) // min_price=1000：BTC 留，ETH(3500)… 也留；改测 BTC 留 DOGE 无数据也留
	wl, err := m.Refresh("binance", "USDT")
	if err != nil {
		t.Fatalf("manager refresh: %v", err)
	}
	if len(wl) != 2 {
		t.Fatalf("whitelist: %v", wl)
	}
}

// TestWireProducerKeepsInjectedStubs 已注入的桩不被接线覆盖（测试/自定义源兼容）。
func TestWireProducerKeepsInjectedStubs(t *testing.T) {
	custom := mcSource(&PairInfo{Symbol: "CUSTOMUSDT", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 1})
	p := NewMarketCapPairList(1, 1, time.Hour, custom)
	WireProducer(p, SourceDeps{MarketCap: mcSource(&PairInfo{Symbol: "OTHERUSDT", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 2})})
	got, err := p.Generate("binance", "USDT")
	if err != nil || len(got) != 1 || got[0] != "CUSTOMUSDT" {
		t.Fatalf("pre-injected source must win: %v err=%v", got, err)
	}
}

// TestProductionSourceDepsSmoke 生产装配可构建且各注入点非空（不触发真实网络）。
func TestProductionSourceDepsSmoke(t *testing.T) {
	deps := ProductionSourceDeps()
	if deps.MarketCap == nil || deps.Universe == nil || deps.Candles == nil || deps.AllPairs == nil || deps.InfoMap == nil {
		t.Fatalf("production deps must be fully wired: %+v", deps)
	}
	// 非 binance 交易所明确降级（不触网）
	if _, err := deps.Universe("okx", "USDT"); err == nil {
		t.Fatal("non-binance exchange should degrade with explicit error")
	}
}
