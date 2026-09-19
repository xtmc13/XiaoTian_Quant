package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMarketFundingProxy：薄代理正常返回字段；上游失败时 502 + null 字段（P1-4）。
func TestMarketFundingProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"lastFundingRate":"0.0001","markPrice":"50000.5","nextFundingTime":1700000000000}`))
	}))
	defer server.Close()

	origClient, origURL := binanceClient, fapiPremiumURL
	binanceClient = server.Client()
	fapiPremiumURL = server.URL + "/fapi/v1/premiumIndex"
	t.Cleanup(func() { binanceClient, fapiPremiumURL = origClient, origURL })

	r := setupRouter()
	r.GET("/market/funding", MarketFunding)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/market/funding?symbol=BTCUSDT", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "funding status")
	var body map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &body) == nil, "funding JSON")
	assertTrue(t, body["funding_rate"].(float64) == 0.0001, "funding_rate")
	assertTrue(t, body["mark_price"].(float64) == 50000.5, "mark_price")
}

func TestMarketFundingUpstreamFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	origClient, origURL := binanceClient, fapiPremiumURL
	binanceClient = server.Client()
	fapiPremiumURL = server.URL + "/fapi/v1/premiumIndex"
	t.Cleanup(func() { binanceClient, fapiPremiumURL = origClient, origURL })

	r := setupRouter()
	r.GET("/market/funding", MarketFunding)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/market/funding?symbol=BTCUSDT", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadGateway, "upstream fail must be 502")
	var body map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &body) == nil, "funding JSON")
	assertTrue(t, body["funding_rate"] == nil, "funding_rate must be null on failure, not 0")
}
