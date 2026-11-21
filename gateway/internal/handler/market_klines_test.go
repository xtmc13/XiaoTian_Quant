package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestMarketKlinesSelectsSpotOrFuturesEndpoint(t *testing.T) {
	requests := make(chan struct {
		path   string
		symbol string
	}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct {
			path   string
			symbol string
		}{path: r.URL.Path, symbol: r.URL.Query().Get("symbol")}
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

	r := setupRouter()
	r.GET("/market/klines", MarketKlines)

	for _, tc := range []struct {
		marketType string
		wantPath   string
	}{
		{marketType: "spot", wantPath: "/api/v3/klines"},
		{marketType: "swap", wantPath: "/fapi/v1/klines"},
	} {
		q := url.Values{
			"symbol":      {"龙虾USDT"},
			"interval":    {"15m"},
			"limit":       {"2"},
			"market_type": {tc.marketType},
			"from":        {"1795242600000"},
			"to":          {"1795243500000"},
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/market/klines?"+q.Encode(), nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s response status = %d, body = %s", tc.marketType, w.Code, w.Body.String())
		}
		got := <-requests
		if got.path != tc.wantPath {
			t.Fatalf("%s upstream path = %q, want %q", tc.marketType, got.path, tc.wantPath)
		}
		if got.symbol != "龙虾USDT" {
			t.Fatalf("%s upstream symbol = %q, want 龙虾USDT", tc.marketType, got.symbol)
		}
	}
}
