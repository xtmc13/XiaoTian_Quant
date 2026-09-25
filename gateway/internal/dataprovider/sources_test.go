package dataprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

// ── 1. Fear & Greed ──

func TestFearGreedFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/fng/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"value":"23","value_classification":"Extreme Fear","timestamp":"1758700000"},
			{"value":"31","value_classification":"Fear","timestamp":"1758613600"}
		],"metadata":{"error":null}}`))
	}))
	defer srv.Close()

	src := newFearGreedSource(Config{BaseURLs: map[string]string{"fear_greed": srv.URL}}, testClient())
	if src.RequiresKey() || !src.Configured() {
		t.Fatal("fear_greed should be keyless and always configured")
	}
	data, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	fg, ok := data.(*FearGreedData)
	if !ok {
		t.Fatalf("unexpected type %T", data)
	}
	if fg.Value != 23 || fg.Classification != "Extreme Fear" {
		t.Fatalf("bad parse: %+v", fg)
	}
	if len(fg.History) != 2 {
		t.Fatalf("expected 2 history points, got %d", len(fg.History))
	}
}

func TestFearGreedFetchUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	src := newFearGreedSource(Config{BaseURLs: map[string]string{"fear_greed": srv.URL}}, testClient())
	if _, err := src.Fetch(context.Background()); err == nil {
		t.Fatal("expected error on 502")
	}
}

// ── 2. Coinglass ──

func TestCoinglassNotConfigured(t *testing.T) {
	src := newCoinglassSource(Config{}, testClient())
	if !src.RequiresKey() {
		t.Fatal("coinglass should require key")
	}
	if src.Configured() {
		t.Fatal("coinglass without key should be unconfigured")
	}
}

func TestCoinglassFetch(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("coinglassSecret")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/funding"):
			_, _ = w.Write([]byte(`{"code":"0","data":[{"exchangeName":"Binance","symbol":"BTCUSDT","fundingRate":"0.0001","time":1758700000000}]}`))
		case strings.Contains(r.URL.Path, "/long_short"):
			_, _ = w.Write([]byte(`{"code":"0","data":[{"longRate":"55.2","shortRate":"44.8","timestamp":1758700000}]}`))
		case strings.Contains(r.URL.Path, "/liquidation"):
			_, _ = w.Write([]byte(`{"code":"0","data":[{"buyVolUsd":"1200000","sellVolUsd":"800000","volUsd":"2000000","timestamp":1758700000}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	src := newCoinglassSource(Config{
		CoinglassAPIKey: "test-key-xyz",
		BaseURLs:        map[string]string{"coinglass": srv.URL},
	}, testClient())
	if !src.Configured() {
		t.Fatal("coinglass with key should be configured")
	}
	data, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	cg := data.(*CoinglassData)
	if len(cg.Funding) != 1 || cg.Funding[0].Exchange != "Binance" {
		t.Fatalf("bad funding: %+v", cg.Funding)
	}
	if cg.Funding[0].RatePct != 0.01 {
		t.Fatalf("rate pct should be 0.01, got %v", cg.Funding[0].RatePct)
	}
	if cg.LongShort == nil || cg.LongShort.LongPct != 55.2 {
		t.Fatalf("bad long/short: %+v", cg.LongShort)
	}
	if cg.Liquidations == nil || cg.Liquidations.TotalUSD != 2000000 {
		t.Fatalf("bad liquidations: %+v", cg.Liquidations)
	}
	if gotAuth != "test-key-xyz" {
		t.Fatalf("api key header missing/wrong: %q", gotAuth)
	}
}

func TestCoinglassAllEndpointsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	src := newCoinglassSource(Config{
		CoinglassAPIKey: "k",
		BaseURLs:        map[string]string{"coinglass": srv.URL},
	}, testClient())
	if _, err := src.Fetch(context.Background()); err == nil {
		t.Fatal("expected aggregate error when all endpoints fail")
	}
}

// ── 3. FRED ──

func TestFredNotConfigured(t *testing.T) {
	src := newFredSource(Config{}, testClient())
	if src.Configured() {
		t.Fatal("fred without key should be unconfigured")
	}
}

func TestFredFetch(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("api_key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"observations":[
			{"date":"2026-09-01","value":"5.33"},
			{"date":"2026-08-01","value":"."},
			{"date":"2026-07-01","value":"5.12"}
		]}`))
	}))
	defer srv.Close()

	src := newFredSource(Config{
		FredAPIKey: "fred-secret",
		BaseURLs:   map[string]string{"fred": srv.URL},
	}, testClient())
	data, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	macro := data.(*MacroData)
	if len(macro.Series) != len(FredSeriesMeta) {
		t.Fatalf("expected %d series, got %d", len(FredSeriesMeta), len(macro.Series))
	}
	s0 := macro.Series[0]
	if s0.ID != "FEDFUNDS" || len(s0.Observations) != 2 {
		t.Fatalf("bad series: %+v", s0)
	}
	// 升序检查 + "." 缺失值跳过
	if s0.Observations[0].Date != "2026-07-01" || s0.Observations[1].Value != 5.33 {
		t.Fatalf("bad observations order/filter: %+v", s0.Observations)
	}
	if gotKey != "fred-secret" {
		t.Fatalf("api_key param wrong: %q", gotKey)
	}
}

// ── 4. CryptoCompare News ──

func TestNewsFetchAndSymbolFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Type":100,"Data":[
			{"id":"1","title":"BTC hits new high","url":"https://ex.com/1","source":"coindesk","published_on":1758700000,"categories":"BTC|Market","body":"bitcoin rally body"},
			{"id":"2","title":"ETH upgrade","url":"https://ex.com/2","source":"cointelegraph","published_on":1758690000,"categories":"ETH|Technology","body":"eth body"},
			{"id":"3","title":"SOL outage","url":"https://ex.com/3","source":"theblock","published_on":1758680000,"categories":"SOL","body":"sol body"}
		]}`))
	}))
	defer srv.Close()

	src := newCryptoCompareNewsSource(Config{BaseURLs: map[string]string{"news": srv.URL}}, testClient())
	if src.RequiresKey() || !src.Configured() {
		t.Fatal("news should be keyless")
	}
	data, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	nd := data.(*NewsData)
	if len(nd.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(nd.Items))
	}

	btc := FilterNewsBySymbol(nd.Items, "BTC")
	if len(btc) != 1 || btc[0].ID != "1" {
		t.Fatalf("BTC filter wrong: %+v", btc)
	}
	// BTCUSDT 形式也应命中
	if got := FilterNewsBySymbol(nd.Items, "btcusdt"); len(got) != 1 {
		t.Fatalf("lowercase pair filter should match: %+v", got)
	}
	if got := FilterNewsBySymbol(nd.Items, ""); len(got) != 3 {
		t.Fatalf("empty filter should return all: %d", len(got))
	}
	if got := FilterNewsBySymbol(nd.Items, "DOGE"); len(got) != 0 {
		t.Fatalf("DOGE should match nothing: %d", len(got))
	}
}

// ── 5. Heatmap ──

func TestHeatmapFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "symbols") {
			t.Error("expected symbols param")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"symbol":"BTCUSDT","lastPrice":"67000","priceChangePercent":"2.5","quoteVolume":"1000000000"},
			{"symbol":"ETHUSDT","lastPrice":"3500","priceChangePercent":"-1.2","quoteVolume":"500000000"},
			{"symbol":"SOLUSDT","lastPrice":"150","priceChangePercent":"5.0","quoteVolume":"0"}
		]`))
	}))
	defer srv.Close()

	src := newHeatmapSource(Config{BaseURLs: map[string]string{"heatmap": srv.URL}}, testClient())
	if src.RequiresKey() || !src.Configured() {
		t.Fatal("heatmap must be keyless (no external key needed)")
	}
	data, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	hm := data.(*HeatmapData)
	// SOL 成交额 0 被剔除
	if len(hm.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(hm.Entries))
	}
	// 按成交额降序
	if hm.Entries[0].Symbol != "BTCUSDT" {
		t.Fatalf("sort by volume wrong: %+v", hm.Entries)
	}
	if hm.Entries[0].Weight != 1.0 {
		t.Fatalf("max volume weight should be 1, got %v", hm.Entries[0].Weight)
	}
	if hm.Entries[1].ChangePct24h != -1.2 || hm.Entries[1].Base != "ETH" {
		t.Fatalf("bad entry: %+v", hm.Entries[1])
	}
}

func TestHeatmapUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	src := newHeatmapSource(Config{BaseURLs: map[string]string{"heatmap": srv.URL}}, testClient())
	if _, err := src.Fetch(context.Background()); err == nil {
		t.Fatal("expected error on 503")
	}
}

// ── 6. 经济日历 ──

func TestCalendarFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<weeklyevents>
	<event>
		<title>CPI m/m</title>
		<country>USD</country>
		<date>09-23-2026</date>
		<time>8:30am</time>
		<impact>High</impact>
		<forecast>0.2%</forecast>
		<previous>0.3%</previous>
	</event>
	<event>
		<title>FOMC Statement</title>
		<country>USD</country>
		<date>09-24-2026</date>
		<time>2:00pm</time>
		<impact>High</impact>
		<forecast></forecast>
		<previous>5.50%</previous>
	</event>
	<event>
		<title>German Holiday</title>
		<country>EUR</country>
		<date>09-25-2026</date>
		<time>All Day</time>
		<impact>Holiday</impact>
		<forecast></forecast>
		<previous></previous>
	</event>
</weeklyevents>`))
	}))
	defer srv.Close()

	src := newCalendarSource(Config{BaseURLs: map[string]string{"calendar": srv.URL}}, testClient())
	if src.RequiresKey() || !src.Configured() {
		t.Fatal("calendar should be keyless")
	}
	data, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	cd := data.(*CalendarData)
	if len(cd.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(cd.Events))
	}
	e0 := cd.Events[0]
	if e0.Name != "CPI m/m" || e0.Date != "2026-09-23" || e0.Impact != "high" {
		t.Fatalf("bad event parse: %+v", e0)
	}
	if e0.Forecast != "0.2%" || e0.Previous != "0.3%" {
		t.Fatalf("forecast/previous wrong: %+v", e0)
	}
	if cd.Events[2].Impact != "holiday" {
		t.Fatalf("holiday impact lowercase: %+v", cd.Events[2])
	}
}

func TestNormalizeFFDate(t *testing.T) {
	if got := normalizeFFDate("09-23-2026"); got != "2026-09-23" {
		t.Fatalf("date normalize wrong: %s", got)
	}
	if got := normalizeFFDate("garbage"); got != "garbage" {
		t.Fatalf("unparseable date should pass through: %s", got)
	}
}
