package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func GetConfig(c *gin.Context) {
	cfg := store.GetConfig()
	c.JSON(http.StatusOK, cfg)
}

func SaveConfig(c *gin.Context) {
	var data map[string]any
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := store.SaveConfig(data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func GetGlobalStrategy(c *gin.Context) {
	cfg := store.GetConfig()
	risk, _ := cfg["risk"].(map[string]any)
	ppEnabled := false
	maxOrders := 5
	if risk != nil {
		if v, ok := risk["profit_protection_enabled"].(bool); ok {
			ppEnabled = v
		}
		if v, ok := risk["max_concurrent_orders"].(int); ok {
			maxOrders = v
		} else if v, ok := risk["max_concurrent_orders"].(float64); ok {
			maxOrders = int(v)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"profit_protection_enabled": ppEnabled,
		"max_concurrent_orders":     maxOrders,
	})
}

func SaveGlobalStrategy(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	cfg := store.GetConfig()
	risk, _ := cfg["risk"].(map[string]any)
	if risk == nil {
		risk = make(map[string]any)
		cfg["risk"] = risk
	}
	if v, ok := data["profit_protection_enabled"]; ok {
		risk["profit_protection_enabled"] = v
	}
	if v, ok := data["max_concurrent_orders"]; ok {
		risk["max_concurrent_orders"] = v
	}
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func ExchangeSave(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	name := getString(data, "name", "")
	cfg := store.GetConfig()
	exchanges, _ := cfg["exchanges"].(map[string]any)
	if exchanges == nil {
		exchanges = make(map[string]any)
		cfg["exchanges"] = exchanges
	}
	ex, _ := exchanges[name].(map[string]any)
	if ex == nil {
		ex = make(map[string]any)
		exchanges[name] = ex
	}
	for _, k := range []string{"api_key", "secret", "passphrase", "testnet", "futures"} {
		if v, ok := data[k]; ok {
			ex[k] = v
		}
	}
	if _, ok := ex["enabled"]; !ok {
		ex["enabled"] = true
	}
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func ExchangeTest(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	apiKey := getString(data, "api_key", "")
	secret := getString(data, "secret", "")
	if apiKey == "" || secret == "" {
		c.JSON(http.StatusOK, gin.H{"status": "error", "detail": "API key and secret required"})
		return
	}
	name := getString(data, "name", "")

	// Select the correct test endpoint per exchange
	baseURL := "https://api.binance.com/api/v3"
	if name == "okx" { baseURL = "https://www.okx.com/api/v5" }

	client := &http.Client{Timeout: 10 * time.Second}

	// Use account info endpoint which requires valid auth
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	req, _ := http.NewRequest("GET", baseURL+"/account", nil)
	req.Header.Set("X-MBX-APIKEY", apiKey)

	// Binance HMAC-SHA256 signing
	q := req.URL.Query()
	q.Set("timestamp", timestamp)
	q.Set("recvWindow", "5000")
	queryStr := q.Encode()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(queryStr))
	sign := hex.EncodeToString(mac.Sum(nil))
	q.Set("signature", sign)
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	code := 0
	var respBody string
	if resp != nil {
		code = resp.StatusCode
		body, _ := io.ReadAll(resp.Body)
		respBody = strings.ToLower(string(body))
		resp.Body.Close()
	}

	if err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "error", "detail": "⚠️ 网络连接失败，请检查网络后重试"})
		return
	}
	if code >= 200 && code < 300 {
		cfg := store.GetConfig()
		exchanges, _ := cfg["exchanges"].(map[string]any)
		if exchanges == nil { exchanges = make(map[string]any) }
		ex, _ := exchanges[name].(map[string]any)
		if ex == nil { ex = make(map[string]any); exchanges[name] = ex }
		ex["tested"] = true
		store.SaveConfig(cfg)
		c.JSON(http.StatusOK, gin.H{"status": "ok", "detail": "✅ " + name + " 连接测试通过"})
	} else if code == 401 || code == 403 {
		c.JSON(http.StatusOK, gin.H{"status": "error", "detail": "🔑 API Key 或 Secret 无效，请检查后重新填写"})
	} else {
		trunc := respBody
if len(trunc) > 50 { trunc = trunc[:50] }
c.JSON(http.StatusOK, gin.H{"status": "error", "detail": fmt.Sprintf("⚠️ 服务器返回 %d（%s），请稍后重试", code, trunc)})
	}
}

func ExchangeDefault(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	cfg := store.GetConfig()
	cfg["default_exchange"] = getString(data, "exchange", "")
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func ExchangeStatus(c *gin.Context) {
	cfg := store.GetConfig()
	exchanges, _ := cfg["exchanges"].(map[string]any)
	result := make(map[string]any)
	for name, v := range exchanges {
		ex, _ := v.(map[string]any)
		result[name] = gin.H{
			"connected":       hasCredentials(ex, "api_key", "secret"),
			"testnet":         getBool(ex, "testnet", true),
			"has_credentials": hasCredentials(ex, "api_key", "secret"),
		}
	}
	c.JSON(http.StatusOK, result)
}

func ExchangesConfigured(c *gin.Context) {
	cfg := store.GetConfig()
	exchangesCfg, _ := cfg["exchanges"].(map[string]any)
	allExchanges := []string{
		"binance", "okx", "kucoin", "bybit", "gate", "htx",
		"coinbase", "mexc", "zb", "bitget", "phemex", "deribit",
	}
	result := make(map[string]any)
	for _, name := range allExchanges {
		ex, _ := exchangesCfg[name].(map[string]any)
		result[name] = gin.H{
			"enabled":         getBool(ex, "enabled", false),
			"has_credentials": hasCredentials(ex, "api_key", "secret"),
			"testnet":         getBool(ex, "testnet", true),
			"futures":         getBool(ex, "futures", false),
		}
	}
	c.JSON(http.StatusOK, result)
}

func AISave(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	provider := getString(data, "provider", "")
	cfg := store.GetConfig()
	ai, _ := cfg["ai"].(map[string]any)
	if ai == nil {
		ai = make(map[string]any)
		cfg["ai"] = ai
	}
	prov, _ := ai[provider].(map[string]any)
	if prov == nil {
		prov = make(map[string]any)
		ai[provider] = prov
	}
	for _, k := range []string{"api_key", "base_url", "model"} {
		if v, ok := data[k]; ok {
			prov[k] = v
		}
	}
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func AITest(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	baseURL := getString(data, "base_url", "https://api.openai.com/v1")
	apiKey := getString(data, "api_key", "")
	if apiKey == "" {
		c.JSON(http.StatusOK, gin.H{"status": "error", "detail": "API key required"})
		return
	}
	baseURL = stringsTrimSuffix(baseURL, "/")
	resp, err := http.Get(baseURL + "/models")
	// Attempt with auth header
	if err != nil {
		req, _ := http.NewRequest("GET", baseURL+"/models", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err = client.Do(req)
	}
	if err != nil || resp == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	} else {
		c.JSON(http.StatusOK, gin.H{"status": "error", "detail": fmt.Sprintf("HTTP %d", resp.StatusCode)})
	}
}

func AIDefault(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	cfg := store.GetConfig()
	cfg["default_ai_provider"] = getString(data, "provider", "")
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func Restart(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	cfg := store.GetConfig()
	if exchanges, ok := data["exchanges"].(map[string]any); ok {
		existingEx, _ := cfg["exchanges"].(map[string]any)
		if existingEx == nil {
			existingEx = make(map[string]any)
			cfg["exchanges"] = existingEx
		}
		for name, v := range exchanges {
			if exData, ok := v.(map[string]any); ok {
				ex, _ := existingEx[name].(map[string]any)
				if ex == nil {
					ex = make(map[string]any)
					existingEx[name] = ex
				}
				for k, val := range exData {
					ex[k] = val
				}
			}
		}
	}
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Config saved."})
}

// ── Helpers ──

func hasCredentials(ex map[string]any, keys ...string) bool {
	for _, k := range keys {
		v, _ := ex[k].(string)
		if v == "" {
			return false
		}
	}
	return true
}

func getBool(m map[string]any, key string, def bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return def
}

func stringsTrimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
