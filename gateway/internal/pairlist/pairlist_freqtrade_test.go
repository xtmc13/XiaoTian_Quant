package pairlist

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── MarketCapPairList ──────────────────────────────────────────

func mcSource(entries ...*PairInfo) MarketCapSource {
	return func() ([]*PairInfo, error) { return entries, nil }
}

func TestMarketCapPairListRanking(t *testing.T) {
	src := mcSource(
		&PairInfo{Symbol: "SMALLUSDT", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 1e8},
		&PairInfo{Symbol: "BTCUSDT", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 1e12},
		&PairInfo{Symbol: "ETHUSDT", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 4e11},
		&PairInfo{Symbol: "MIDUSDT", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 5e9},
		&PairInfo{Symbol: "NOMARKET", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 0},   // 无市值数据剔除
		&PairInfo{Symbol: "BROKEN", QuoteAsset: "USDT", Status: "BREAK", MarketCap: 9e11},    // 非交易状态剔除
		&PairInfo{Symbol: "BTCBUSD", QuoteAsset: "BUSD", Status: "TRADING", MarketCap: 9e11}, // quote 不匹配剔除
	)

	p := NewMarketCapPairList(2, 3, time.Hour, src)
	got, err := p.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(got) != 2 || got[0] != "BTCUSDT" || got[1] != "ETHUSDT" {
		t.Fatalf("expected [BTCUSDT ETHUSDT], got %v", got)
	}

	// number_assets > max_rank 时以 max_rank 为上限
	p2 := NewMarketCapPairList(3, 2, time.Hour, src)
	got2, err := p2.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(got2) != 2 {
		t.Fatalf("max_rank=2 should cap result to 2, got %v", got2)
	}
}

func TestMarketCapPairListNoSource(t *testing.T) {
	p := NewMarketCapPairList(10, 10, time.Hour, nil)
	_, err := p.Generate("binance", "USDT")
	if err == nil {
		t.Fatal("expected degradation error when no data source configured")
	}
	if !strings.Contains(err.Error(), "未配置市值数据源") {
		t.Fatalf("error should be an explicit degradation message, got: %v", err)
	}
}

func TestMarketCapPairListSourceError(t *testing.T) {
	p := NewMarketCapPairList(10, 10, time.Hour, func() ([]*PairInfo, error) {
		return nil, fmt.Errorf("coingecko 503")
	})
	p.SourceName = "coingecko"
	_, err := p.Generate("binance", "USDT")
	if err == nil || !strings.Contains(err.Error(), "coingecko") {
		t.Fatalf("expected wrapped source error, got: %v", err)
	}
}

func TestMarketCapPairListEmptyUniverse(t *testing.T) {
	p := NewMarketCapPairList(10, 10, time.Hour, mcSource())
	_, err := p.Generate("binance", "USDT")
	if err == nil {
		t.Fatal("empty universe should return explicit error, not empty list")
	}
}

func TestMarketCapPairListCache(t *testing.T) {
	var calls int32
	src := func() ([]*PairInfo, error) {
		atomic.AddInt32(&calls, 1)
		return []*PairInfo{{Symbol: "BTCUSDT", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 1e12}}, nil
	}
	p := NewMarketCapPairList(10, 10, time.Hour, src)
	if _, err := p.Generate("binance", "USDT"); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := p.Generate("binance", "USDT"); err != nil {
		t.Fatalf("generate cached: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("second generate should hit cache, source called %d times", calls)
	}
}

func TestMarketCapPairListInManagerChain(t *testing.T) {
	m := NewManager(DefaultManagerConfig())
	m.SetInfoProvider(mockInfoProvider)
	m.AddProducer(NewMarketCapPairList(3, 3, time.Hour, mcSource(
		&PairInfo{Symbol: "BTCUSDT", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 1e12},
		&PairInfo{Symbol: "ETHUSDT", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 4e11},
		&PairInfo{Symbol: "SOLUSDT", QuoteAsset: "USDT", Status: "TRADING", MarketCap: 5e10},
	)))
	m.AddFilter(NewMaxPairsFilter(2))

	got, err := m.Refresh("binance", "USDT")
	if err != nil {
		t.Fatalf("chain refresh: %v", err)
	}
	if len(got) != 2 || got[0] != "BTCUSDT" {
		t.Fatalf("chain should produce [BTCUSDT ETHUSDT], got %v", got)
	}
}

// ── PercentChangePairList ──────────────────────────────────────

func pctUniverse() func(string, string) ([]*PairInfo, error) {
	return func(exchange, quoteAsset string) ([]*PairInfo, error) {
		return []*PairInfo{
			{Symbol: "BTCUSDT", QuoteAsset: "USDT", Status: "TRADING", PriceChange: 5.0},
			{Symbol: "ETHUSDT", QuoteAsset: "USDT", Status: "TRADING", PriceChange: -2.0},
			{Symbol: "SOLUSDT", QuoteAsset: "USDT", Status: "TRADING", PriceChange: 12.0},
			{Symbol: "DOGEUSDT", QuoteAsset: "USDT", Status: "TRADING", PriceChange: 0.5},
		}, nil
	}
}

func TestPercentChangePairListTickerMode(t *testing.T) {
	p := NewPercentChangePairList(2, "desc", 0, "1h")
	p.Universe = pctUniverse()

	got, err := p.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(got) != 2 || got[0] != "SOLUSDT" || got[1] != "BTCUSDT" {
		t.Fatalf("desc top2 should be [SOLUSDT BTCUSDT], got %v", got)
	}
}

func TestPercentChangePairListAsc(t *testing.T) {
	p := NewPercentChangePairList(1, "asc", 0, "1h")
	p.Universe = pctUniverse()

	got, err := p.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(got) != 1 || got[0] != "ETHUSDT" {
		t.Fatalf("asc top1 should be [ETHUSDT], got %v", got)
	}
}

func TestPercentChangePairListMinMax(t *testing.T) {
	minV, maxV := 1.0, 10.0
	p := NewPercentChangePairList(10, "desc", 0, "1h")
	p.Universe = pctUniverse()
	p.MinValue = &minV
	p.MaxValue = &maxV

	got, err := p.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// 只留下 1% < pct < 10% 的 BTCUSDT(5.0)
	if len(got) != 1 || got[0] != "BTCUSDT" {
		t.Fatalf("min/max filter should leave [BTCUSDT], got %v", got)
	}
}

func TestPercentChangePairListLookbackMode(t *testing.T) {
	p := NewPercentChangePairList(2, "desc", 3, "1h")
	p.Universe = pctUniverse()
	p.Candles = func(symbol, timeframe string, limit int) ([]Candle, error) {
		if limit != 4 {
			t.Fatalf("should request lookback+1=4 candles, got %d", limit)
		}
		switch symbol {
		case "BTCUSDT":
			return []Candle{{Close: 100}, {Close: 101}, {Close: 102}, {Close: 110}}, nil // +10%
		case "ETHUSDT":
			return []Candle{{Close: 100}, {Close: 99}, {Close: 98}, {Close: 95}}, nil // -5%
		case "SOLUSDT":
			return []Candle{{Close: 100}, {Close: 105}, {Close: 110}, {Close: 120}}, nil // +20%
		default:
			return []Candle{{Close: 100}}, nil // 数据不足 → 0%
		}
	}

	got, err := p.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(got) != 2 || got[0] != "SOLUSDT" || got[1] != "BTCUSDT" {
		t.Fatalf("lookback desc top2 should be [SOLUSDT BTCUSDT], got %v", got)
	}
}

func TestPercentChangePairListDegradation(t *testing.T) {
	// 无候选池
	p := NewPercentChangePairList(10, "desc", 0, "1h")
	if _, err := p.Generate("binance", "USDT"); err == nil || !strings.Contains(err.Error(), "候选池") {
		t.Fatalf("missing universe should be explicit error, got: %v", err)
	}

	// lookback 模式无 K 线源
	p2 := NewPercentChangePairList(10, "desc", 5, "1h")
	p2.Universe = pctUniverse()
	if _, err := p2.Generate("binance", "USDT"); err == nil || !strings.Contains(err.Error(), "K 线数据源") {
		t.Fatalf("missing candle source should be explicit error, got: %v", err)
	}
}

func TestPercentChangePairListEmptyAfterFilter(t *testing.T) {
	minV := 1000.0
	p := NewPercentChangePairList(10, "desc", 0, "1h")
	p.Universe = pctUniverse()
	p.MinValue = &minV
	if _, err := p.Generate("binance", "USDT"); err == nil {
		t.Fatal("all pairs filtered out should be explicit error")
	}
}

// ── DelistFilter（增强语义） ────────────────────────────────────

func TestDelistFilterStatusCompat(t *testing.T) {
	f := NewDelistFilter()
	pairs := []string{"BTCUSDT", "INACTIVE", "UNKNOWN"}
	info := map[string]*PairInfo{
		"BTCUSDT":  {Symbol: "BTCUSDT", Status: "TRADING"},
		"INACTIVE": {Symbol: "INACTIVE", Status: "BREAK"},
		// UNKNOWN 无数据 → 保留
	}
	got, err := f.Filter(pairs, info)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(got) != 2 || got[0] != "BTCUSDT" || got[1] != "UNKNOWN" {
		t.Fatalf("status filter mismatch: %v", got)
	}
}

func TestDelistFilterMaxDaysFromNow(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	f := NewDelistFilter()
	f.MaxDaysFromNow = 7
	f.Now = func() time.Time { return now }

	pairs := []string{"SOON", "LATER", "NONE", "NODATA"}
	info := map[string]*PairInfo{
		"SOON":   {Symbol: "SOON", Status: "TRADING", DelistingDate: now.Add(3 * 24 * time.Hour).UnixMilli()},
		"LATER":  {Symbol: "LATER", Status: "TRADING", DelistingDate: now.Add(30 * 24 * time.Hour).UnixMilli()},
		"NONE":   {Symbol: "NONE", Status: "TRADING", DelistingDate: 0},
		"NODATA": {Symbol: "NODATA", Status: "TRADING"},
	}
	got, err := f.Filter(pairs, info)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	for _, s := range got {
		if s == "SOON" {
			t.Fatal("pair delisting in 3 days should be removed with max_days_from_now=7")
		}
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 pairs kept, got %v", got)
	}

	// max_days_from_now=0：凡有退市计划即剔除
	f0 := NewDelistFilter()
	f0.MaxDaysFromNow = 0
	f0.Now = func() time.Time { return now }
	got0, _ := f0.Filter(pairs, info)
	if len(got0) != 2 {
		t.Fatalf("max_days_from_now=0 should remove SOON and LATER, got %v", got0)
	}
}

func TestDelistFilterInactiveDays(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	f := NewDelistFilter()
	f.MaxInactiveDays = 10
	f.Now = func() time.Time { return now }

	pairs := []string{"STALE", "FRESH", "UNKNOWN"}
	info := map[string]*PairInfo{
		"STALE":   {Symbol: "STALE", Status: "TRADING", LastTradeTime: now.Add(-30 * 24 * time.Hour).UnixMilli()},
		"FRESH":   {Symbol: "FRESH", Status: "TRADING", LastTradeTime: now.Add(-2 * 24 * time.Hour).UnixMilli()},
		"UNKNOWN": {Symbol: "UNKNOWN", Status: "TRADING", LastTradeTime: 0},
	}
	got, err := f.Filter(pairs, info)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(got) != 2 || got[0] != "FRESH" || got[1] != "UNKNOWN" {
		t.Fatalf("inactive filter should keep [FRESH UNKNOWN], got %v", got)
	}
}

func TestDelistFilterEmptyInput(t *testing.T) {
	f := NewDelistFilter()
	got, err := f.Filter(nil, nil)
	if err != nil {
		t.Fatalf("empty input should not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty input should return empty, got %v", got)
	}
}

// ── RemotePairList ─────────────────────────────────────────────

func TestRemotePairListGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"pairs": ["BTCUSDT", "ETHUSDT", "SOLUSDT"]}`)
	}))
	defer srv.Close()

	r, err := NewRemotePairList(srv.URL)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	r.BearerToken = "test-token"

	got, err := r.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(got) != 3 || got[0] != "BTCUSDT" {
		t.Fatalf("unexpected remote pairs: %v", got)
	}

	// number_assets 截断
	r.NumberAssets = 2
	r.RefreshPeriod = time.Millisecond // 强制重新拉取
	time.Sleep(2 * time.Millisecond)
	got2, err := r.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(got2) != 2 {
		t.Fatalf("number_assets=2 should cap to 2, got %v", got2)
	}
}

func TestRemotePairListTimeoutAndFallback(t *testing.T) {
	var failMode atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failMode.Load() {
			time.Sleep(300 * time.Millisecond) // 触发客户端超时
			return
		}
		fmt.Fprint(w, `{"pairs": ["BTCUSDT", "ETHUSDT"]}`)
	}))
	defer srv.Close()

	// 首次成功 → 建立缓存
	r, err := NewRemotePairList(srv.URL)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	r.ReadTimeout = 100 * time.Millisecond
	if _, err := r.Generate("binance", "USDT"); err != nil {
		t.Fatalf("first generate: %v", err)
	}

	// 服务端开始超时 + 缓存过期 → 应回退到本地缓存（KeepOnFailure 默认 true）
	failMode.Store(true)
	r.RefreshPeriod = time.Millisecond
	time.Sleep(2 * time.Millisecond)
	got, err := r.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("should fall back to cached pairlist, got error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("fallback should return last good list, got %v", got)
	}

	// 无缓存且失败 → 明确错误
	r2, _ := NewRemotePairList(srv.URL)
	r2.ReadTimeout = 50 * time.Millisecond
	if _, err := r2.Generate("binance", "USDT"); err == nil || !strings.Contains(err.Error(), "无本地缓存可兜底") {
			t.Fatalf("expected explicit degradation error, got: %v", err)
	}

	// KeepOnFailure=false 时即便有缓存也报错
	r.KeepOnFailure = false
	r.RefreshPeriod = time.Millisecond
	time.Sleep(2 * time.Millisecond)
	if _, err := r.Generate("binance", "USDT"); err == nil {
		t.Fatal("KeepOnFailure=false should propagate fetch error")
	}
}

func TestRemotePairListFileURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pairlist.json")
	if err := os.WriteFile(path, []byte(`{"pairs": ["AAAUSDT", "BBBUSDT"]}`), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	r, err := NewRemotePairList("file://" + path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := r.Generate("binance", "USDT")
	if err != nil {
		t.Fatalf("generate from file: %v", err)
	}
	if len(got) != 2 || got[0] != "AAAUSDT" {
		t.Fatalf("file pairlist mismatch: %v", got)
	}
}

func TestRemotePairListFilterModes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"pairs": ["BTCUSDT", "ETHUSDT", "NEWCOIN"]}`)
	}))
	defer srv.Close()

	local := []string{"BTCUSDT", "SOLUSDT"}

	// whitelist + filter → 交集
	r, _ := NewRemotePairList(srv.URL)
	got, err := r.Filter(local, nil)
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(got) != 1 || got[0] != "BTCUSDT" {
		t.Fatalf("whitelist+filter should intersect, got %v", got)
	}

	// whitelist + append → 并集
	r2, _ := NewRemotePairList(srv.URL)
	r2.ProcessingMode = "append"
	got2, err := r2.Filter(local, nil)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if len(got2) != 4 || got2[0] != "BTCUSDT" || got2[3] != "NEWCOIN" {
		t.Fatalf("append should union preserving order, got %v", got2)
	}

	// blacklist → 剔除远端项
	r3, _ := NewRemotePairList(srv.URL)
	r3.Mode = "blacklist"
	got3, err := r3.Filter(local, nil)
	if err != nil {
		t.Fatalf("blacklist: %v", err)
	}
	if len(got3) != 1 || got3[0] != "SOLUSDT" {
		t.Fatalf("blacklist should remove remote pairs, got %v", got3)
	}
}

func TestRemotePairListBlacklistNotProducer(t *testing.T) {
	r, err := NewRemotePairList("http://example.invalid/pairs.json")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	r.Mode = "blacklist"
	if _, err := r.Generate("binance", "USDT"); err == nil {
		t.Fatal("blacklist mode must not be usable as chain-head producer")
	}
}

func TestRemotePairListRemoteRefreshPeriod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"pairs": ["BTCUSDT"], "refresh_period": 7200}`)
	}))
	defer srv.Close()

	r, _ := NewRemotePairList(srv.URL)
	r.RefreshPeriod = time.Minute
	if _, err := r.Generate("binance", "USDT"); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if r.RefreshPeriod != 2*time.Hour {
		t.Fatalf("remote refresh_period should raise local ttl to 2h, got %v", r.RefreshPeriod)
	}
}

func TestRemotePairListBadURL(t *testing.T) {
	if _, err := NewRemotePairList(""); err == nil {
		t.Fatal("empty url should error")
	}
	if _, err := NewRemotePairList("ftp://x/y"); err == nil {
		t.Fatal("unsupported scheme should error")
	}
}

func TestRemotePairListEmptyRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"pairs": []}`)
	}))
	defer srv.Close()
	r, _ := NewRemotePairList(srv.URL)
	if _, err := r.Generate("binance", "USDT"); err == nil {
		t.Fatal("empty remote list should be explicit error")
	}
}

// ── 工厂 ──

func TestFactoryBuildsAllSpecs(t *testing.T) {
	for _, spec := range ProducerSpecs() {
		params := map[string]any{}
		for _, p := range spec.Params {
			if p.Default != nil {
				params[p.Key] = p.Default
			}
		}
		if spec.Name == "StaticPairList" {
			params["pairs"] = []any{"BTCUSDT"}
		}
		if spec.Name == "RemotePairList" {
			params["pairlist_url"] = "file:///tmp/nonexistent-pairlist.json"
		}
		if _, err := BuildProducerFromConfig(spec.Name, params); err != nil {
			t.Fatalf("producer %s should build with default params: %v", spec.Name, err)
		}
	}
	for _, spec := range FilterSpecs() {
		params := map[string]any{}
		for _, p := range spec.Params {
			if p.Default != nil {
				params[p.Key] = p.Default
			}
		}
		if spec.Name == "RemotePairList" {
			params["pairlist_url"] = "file:///tmp/nonexistent-pairlist.json"
		}
		if _, err := BuildFilterFromConfig(spec.Name, params); err != nil {
			t.Fatalf("filter %s should build with default params: %v", spec.Name, err)
		}
	}

	if _, err := BuildProducerFromConfig("Nope", nil); err == nil {
		t.Fatal("unknown producer should error")
	}
	if _, err := BuildFilterFromConfig("Nope", nil); err == nil {
		t.Fatal("unknown filter should error")
	}
}
