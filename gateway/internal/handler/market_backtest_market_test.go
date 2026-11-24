package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeBacktestMarketType(t *testing.T) {
	cases := []struct {
		name       string
		marketType string
		strategy   string
		want       string
	}{
		{name: "explicit spot", marketType: "spot", strategy: "trend_short", want: "spot"},
		{name: "explicit swap", marketType: "swap", strategy: "sma_cross", want: "swap"},
		{name: "explicit futures alias", marketType: "FUTURES", strategy: "sma_cross", want: "swap"},
		{name: "margin maps to spot", marketType: "margin", strategy: "dual_burn", want: "spot"},
		{name: "empty falls back to contract strategy", marketType: "", strategy: "trend_short", want: "swap"},
		{name: "empty falls back to spot strategy", marketType: "", strategy: "sma_cross", want: "spot"},
		{name: "category alias read by caller", marketType: "contract", strategy: "sma_cross", want: "swap"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeBacktestMarketType(tc.marketType, tc.strategy); got != tc.want {
				t.Fatalf("normalizeBacktestMarketType(%q, %q) = %q, want %q",
					tc.marketType, tc.strategy, got, tc.want)
			}
		})
	}
}

func TestFetchBinanceKlinesMarketSelectsEndpoint(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[[1795242600000,"1","2","0.5","1.5","10"]]`))
	}))
	defer server.Close()

	origClient, origSpotURL, origFuturesURL := binanceClient, binanceSpotKlinesURL, binanceFuturesKlinesURL
	binanceClient = server.Client()
	binanceSpotKlinesURL = server.URL + "/api/v3/klines"
	binanceFuturesKlinesURL = server.URL + "/fapi/v1/klines"
	t.Cleanup(func() {
		binanceClient, binanceSpotKlinesURL, binanceFuturesKlinesURL = origClient, origSpotURL, origFuturesURL
	})

	for _, tc := range []struct {
		marketType string
		wantPath   string
	}{
		{marketType: "spot", wantPath: "/api/v3/klines"},
		{marketType: "swap", wantPath: "/fapi/v1/klines"},
	} {
		klines, err := fetchBinanceKlinesMarket("BTCUSDT", "1h", 2, 0, 0, tc.marketType)
		if err != nil {
			t.Fatalf("%s fetch err: %v", tc.marketType, err)
		}
		if len(klines) != 1 {
			t.Fatalf("%s returned %d klines, want 1", tc.marketType, len(klines))
		}
		if gotPath != tc.wantPath {
			t.Fatalf("%s upstream path = %q, want %q", tc.marketType, gotPath, tc.wantPath)
		}
	}
}

func TestFetchBinanceKlinesRangePropagatesMarketType(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		// 单页不足 pageLimit，触发一页即停
		_, _ = w.Write([]byte(`[[1795242600000,"1","2","0.5","1.5","10"]]`))
	}))
	defer server.Close()

	origClient, origSpotURL, origFuturesURL := binanceClient, binanceSpotKlinesURL, binanceFuturesKlinesURL
	binanceClient = server.Client()
	binanceSpotKlinesURL = server.URL + "/api/v3/klines"
	binanceFuturesKlinesURL = server.URL + "/fapi/v1/klines"
	t.Cleanup(func() {
		binanceClient, binanceSpotKlinesURL, binanceFuturesKlinesURL = origClient, origSpotURL, origFuturesURL
	})

	klines, err := fetchBinanceKlinesRange("BTCUSDT", "1h", 1795242600000, 1795246200000, "swap")
	if err != nil {
		t.Fatalf("range fetch err: %v", err)
	}
	if len(klines) != 1 {
		t.Fatalf("returned %d klines, want 1", len(klines))
	}
	if len(paths) == 0 || paths[0] != "/fapi/v1/klines" {
		t.Fatalf("range upstream paths = %v, want fapi first", paths)
	}
}
