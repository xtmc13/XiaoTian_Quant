package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
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
	c.JSON(http.StatusOK, data)
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
	c.JSON(http.StatusOK, gin.H{"success": true, "id": name})
}

func ExchangeTest(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	apiKey := getString(data, "api_key", "")
	secret := getString(data, "secret", "")
	if apiKey == "" || secret == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "API key and secret required"})
		return
	}
	name := getString(data, "name", "")
	passphrase := getString(data, "passphrase", "")

	// Build exchange-specific auth and request
	client := &http.Client{Timeout: 10 * time.Second}
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())

	var req *http.Request
	var path string
	var mac hash.Hash

	switch name {
	case "binance":
		params := url.Values{}
		params.Set("timestamp", ts)
		params.Set("recvWindow", "5000")
		req, _ = http.NewRequest("GET", "https://api.binance.com/api/v3/account?"+params.Encode(), nil)
		mac = hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(params.Encode()))
		req.URL.RawQuery = params.Encode() + "&signature=" + hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-MBX-APIKEY", apiKey)

	case "okx":
		// OKX requires ISO 8601 UTC format with milliseconds
		tsOKX := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		path = "/api/v5/account/balance"
		req, _ = http.NewRequest("GET", "https://www.okx.com"+path, nil)
		mac = hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(tsOKX + "GET" + path + ""))
		req.Header.Set("OK-ACCESS-KEY", apiKey)
		req.Header.Set("OK-ACCESS-SIGN", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		req.Header.Set("OK-ACCESS-TIMESTAMP", tsOKX)
		req.Header.Set("OK-ACCESS-PASSPHRASE", passphrase)

	case "bybit":
		path = "/v5/account/wallet-balance"
		params := url.Values{}
		params.Set("accountType", "UNIFIED")
		req, _ = http.NewRequest("GET", "https://api.bybit.com"+path+"?"+params.Encode(), nil)
		signStr := ts + apiKey + "5000" + params.Encode()
		mac = hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(signStr))
		req.Header.Set("X-BAPI-API-KEY", apiKey)
		req.Header.Set("X-BAPI-TIMESTAMP", ts)
		req.Header.Set("X-BAPI-SIGN", hex.EncodeToString(mac.Sum(nil)))
		req.Header.Set("X-BAPI-RECV-WINDOW", "5000")

	case "gate":
		path = "/api/v4/spot/accounts"
		req, _ = http.NewRequest("GET", "https://api.gateio.ws"+path, nil)
		gateTS := fmt.Sprintf("%d", time.Now().Unix())
		payload := "GET\n" + path + "\n\n\n" + gateTS
		mac = hmac.New(sha512.New, []byte(secret))
		mac.Write([]byte(payload))
		req.Header.Set("KEY", apiKey)
		req.Header.Set("SIGN", hex.EncodeToString(mac.Sum(nil)))
		req.Header.Set("Timestamp", gateTS)

	case "mexc":
		params := url.Values{}
		params.Set("timestamp", ts)
		params.Set("recvWindow", "5000")
		req, _ = http.NewRequest("GET", "https://api.mexc.com/api/v3/account?"+params.Encode(), nil)
		mac = hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(params.Encode()))
		req.URL.RawQuery = params.Encode() + "&signature=" + hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-MEXC-APIKEY", apiKey)

	case "bitget":
		path = "/api/v2/spot/account/assets"
		req, _ = http.NewRequest("GET", "https://api.bitget.com"+path, nil)
		mac = hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(ts + "GET" + path + ""))
		req.Header.Set("ACCESS-KEY", apiKey)
		req.Header.Set("ACCESS-SIGN", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		req.Header.Set("ACCESS-TIMESTAMP", ts)
		req.Header.Set("ACCESS-PASSPHRASE", passphrase)
		req.Header.Set("Content-Type", "application/json")

	case "coinbase":
		path = "/api/v3/brokerage/accounts"
		req, _ = http.NewRequest("GET", "https://api.coinbase.com"+path, nil)
		coinbaseTS := fmt.Sprintf("%d", time.Now().Unix())
		mac = hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(coinbaseTS + "GET" + path + ""))
		req.Header.Set("CB-ACCESS-KEY", apiKey)
		req.Header.Set("CB-ACCESS-SIGN", hex.EncodeToString(mac.Sum(nil)))
		req.Header.Set("CB-ACCESS-TIMESTAMP", coinbaseTS)
		req.Header.Set("Content-Type", "application/json")

	case "kucoin", "htx", "zb", "phemex", "deribit":
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("ℹ️ %s 适配器尚未实现，连接测试暂不可用", name)})
		return

	default:
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("交易所 %s 暂不支持直接测试", name)})
		return
	}

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
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "⚠️ 网络连接失败，请检查网络后重试"})
		return
	}

	// Parse exchange-specific success indicators
	switch name {
	case "okx", "bitget":
		// JSON body: {"code":"0"} or {"code":0}
		var jr map[string]any
		json.Unmarshal([]byte(respBody), &jr)
		if isCodeZero(jr) {
			saveExchangeTest(name, c)
			return
		}
		codeStr := fmt.Sprintf("%v", jr["code"])
		msgStr := fmt.Sprintf("%v", jr["msg"])
		// 50102 on OKX often means IP whitelist restriction
		if name == "okx" && (codeStr == "50102" || strings.Contains(msgStr, "Timestamp")) {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "🔒 OKX: API Key 签名正确，但被拒绝（可能是 IP 白名单限制，请将 43.165.169.27 加入 OKX API 白名单）"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("🔑 %s: %s", name, msgStr)})
		return

	case "bybit":
		var jr map[string]any
		json.Unmarshal([]byte(respBody), &jr)
		if retCode, ok := jr["retCode"].(float64); ok && retCode == 0 {
			saveExchangeTest(name, c)
			return
		}
		// retCode missing or non-zero → failure
		msg := fmt.Sprintf("%v", jr["retMsg"])
		if msg == "<nil>" || msg == "" {
			msg = fmt.Sprintf("HTTP %d (请确认 API 是否有效)", code)
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "🔑 Bybit: " + msg})
		return

	case "gate":
		// Gate returns array on success, {"label":"...","detail":"..."} on error
		if strings.Contains(respBody, `"currency"`) || respBody == "[]" || strings.Contains(respBody, `[{}`) {
			saveExchangeTest(name, c)
			return
		}
		if strings.Contains(respBody, "invalid_key") || strings.Contains(respBody, "INVALID") {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "🔑 Gate.io: API Key 无效"})
		} else {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("⚠️ Gate.io 返回异常: %s", trunc(respBody, 50))})
		}
		return

	case "coinbase":
		if code >= 200 && code < 300 {
			saveExchangeTest(name, c)
		} else if code == 401 {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "🔑 Coinbase: API Key 或 Secret 无效"})
		} else {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("⚠️ Coinbase: HTTP %d", code)})
		}
		return

	default:
		// Binance, MEXC — HTTP status based
		if code >= 200 && code < 300 {
			saveExchangeTest(name, c)
		} else if code == 401 || code == 403 {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "🔑 API Key 或 Secret 无效，请检查后重新填写"})
		} else {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("⚠️ 服务器返回 %d（%s），请稍后重试", code, trunc(respBody, 50))})
		}
	}
}

func saveExchangeTest(name string, c *gin.Context) {
	cfg := store.GetConfig()
	exchanges, _ := cfg["exchanges"].(map[string]any)
	if exchanges == nil {
		exchanges = make(map[string]any)
	}
	ex, _ := exchanges[name].(map[string]any)
	if ex == nil {
		ex = make(map[string]any)
		exchanges[name] = ex
	}
	ex["tested"] = true
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "✅ " + name + " 连接测试通过"})
}

func ExchangeDefault(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	cfg := store.GetConfig()
	cfg["default_exchange"] = getString(data, "exchange", "")
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"success": true})
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

func isCodeZero(m map[string]any) bool {
	if v, ok := m["code"].(string); ok && v == "0" {
		return true
	}
	if v, ok := m["code"].(float64); ok && v == 0 {
		return true
	}
	return false
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
