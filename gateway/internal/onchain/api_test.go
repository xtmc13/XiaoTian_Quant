package onchain

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupOnchainRouter() (*gin.Engine, *MockClient) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	mc := NewMockClient()
	RegisterRoutes(r.Group("/api/onchain"), mc)
	return r, mc
}

func TestAPI_GetETHMetrics(t *testing.T) {
	r, _ := setupOnchainRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/onchain/eth/metrics", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp ETHMetrics
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Greater(t, resp.GasPriceGwei, 0.0)
	assert.Greater(t, resp.ActiveAddresses, int64(0))
}

func TestAPI_GetBTCMetrics(t *testing.T) {
	r, _ := setupOnchainRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/onchain/btc/metrics", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp BTCMetrics
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Greater(t, resp.HashRateEH, 0.0)
	assert.Greater(t, resp.ActiveAddresses, int64(0))
}

func TestAPI_GetExchangeFlow(t *testing.T) {
	r, _ := setupOnchainRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/onchain/exchange-flow?exchange=binance", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp ExchangeFlow
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "binance", resp.Exchange)
}

func TestAPI_GetWhaleAlerts(t *testing.T) {
	r, _ := setupOnchainRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/onchain/whale-alerts?min_usd=1000000", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	alerts, ok := resp["alerts"].([]any)
	assert.True(t, ok)
	assert.Len(t, alerts, 1)
}

func TestAPI_GetBTCSignal(t *testing.T) {
	r, _ := setupOnchainRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/onchain/signal/btc", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp OnChainSignal
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "BTC", resp.Symbol)
	assert.NotEmpty(t, resp.Direction)
	assert.Greater(t, resp.Strength, 0.0)
}

func TestAPI_GetETHSignal(t *testing.T) {
	r, _ := setupOnchainRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/onchain/signal/eth", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp OnChainSignal
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ETH", resp.Symbol)
	assert.NotEmpty(t, resp.Direction)
	assert.Greater(t, resp.Strength, 0.0)
}
