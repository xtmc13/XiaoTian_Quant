package social

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupSocialRouter() (*gin.Engine, *Engine) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	eng := NewEngine()
	eng.RegisterProvider(1, 29.99, true)
	eng.RegisterProvider(2, 0, false)
	RegisterRoutes(r.Group("/api/social"), eng)
	return r, eng
}

func TestAPI_ListProviders(t *testing.T) {
	r, _ := setupSocialRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/social/providers", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	providers, ok := resp["providers"].([]any)
	assert.True(t, ok)
	assert.Len(t, providers, 1) // only public provider
}

func TestAPI_FollowProvider(t *testing.T) {
	r, _ := setupSocialRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/social/providers/1/follow?follower_id=100", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["success"])
}

func TestAPI_FollowProvider_Private(t *testing.T) {
	r, _ := setupSocialRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/social/providers/2/follow?follower_id=100", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_UnfollowProvider(t *testing.T) {
	r, eng := setupSocialRouter()
	eng.Follow(DefaultCopyConfig(100, 1))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/social/providers/1/unfollow?follower_id=100", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	configs := eng.GetFollowerConfigs(100)
	assert.Len(t, configs, 0)
}

func TestAPI_PublishSignal(t *testing.T) {
	r, _ := setupSocialRouter()
	body, _ := json.Marshal(map[string]any{
		"provider_id":   1,
		"provider_name": "TestTrader",
		"symbol":        "BTCUSDT",
		"direction":     "buy",
		"price":         50000,
		"confidence":    85,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/social/signals", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	sig, ok := resp["signal"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "BTCUSDT", sig["symbol"])
	assert.Equal(t, "buy", sig["direction"])
}

func TestAPI_ListSignals(t *testing.T) {
	r, eng := setupSocialRouter()
	eng.Follow(DefaultCopyConfig(100, 1))
	eng.PublishSignal(Signal{
		ID: "sig-1", ProviderID: 1, Symbol: "BTCUSDT", Direction: "buy",
		Price: 50000, Confidence: 85,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/social/signals?provider_id=1&limit=10", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	signals, ok := resp["signals"].([]any)
	assert.True(t, ok)
	assert.Len(t, signals, 1)
}

func TestAPI_GetFollowerConfigs(t *testing.T) {
	r, eng := setupSocialRouter()
	eng.Follow(DefaultCopyConfig(100, 1))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/social/followers/configs?follower_id=100", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	configs, ok := resp["configs"].([]any)
	assert.True(t, ok)
	assert.Len(t, configs, 1)
}
