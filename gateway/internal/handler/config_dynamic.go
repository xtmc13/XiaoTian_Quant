package handler

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── Default data ──

var defaultMarkets = map[string]any{
	"symbols": []map[string]any{
		{"symbol": "BTCUSDT", "base": "BTC", "quote": "USDT", "precision": map[string]int{"price": 2, "quantity": 5}},
		{"symbol": "ETHUSDT", "base": "ETH", "quote": "USDT", "precision": map[string]int{"price": 2, "quantity": 4}},
		{"symbol": "SOLUSDT", "base": "SOL", "quote": "USDT", "precision": map[string]int{"price": 3, "quantity": 2}},
		{"symbol": "BNBUSDT", "base": "BNB", "quote": "USDT", "precision": map[string]int{"price": 2, "quantity": 3}},
		{"symbol": "XRPUSDT", "base": "XRP", "quote": "USDT", "precision": map[string]int{"price": 4, "quantity": 1}},
		{"symbol": "DOGEUSDT", "base": "DOGE", "quote": "USDT", "precision": map[string]int{"price": 5, "quantity": 0}},
		{"symbol": "ADAUSDT", "base": "ADA", "quote": "USDT", "precision": map[string]int{"price": 4, "quantity": 2}},
		{"symbol": "AVAXUSDT", "base": "AVAX", "quote": "USDT", "precision": map[string]int{"price": 2, "quantity": 3}},
		{"symbol": "DOTUSDT", "base": "DOT", "quote": "USDT", "precision": map[string]int{"price": 2, "quantity": 3}},
		{"symbol": "LINKUSDT", "base": "LINK", "quote": "USDT", "precision": map[string]int{"price": 2, "quantity": 3}},
		{"symbol": "LTCUSDT", "base": "LTC", "quote": "USDT", "precision": map[string]int{"price": 2, "quantity": 4}},
		{"symbol": "MATICUSDT", "base": "MATIC", "quote": "USDT", "precision": map[string]int{"price": 4, "quantity": 2}},
	},
}

var defaultIndices = map[string]any{
	"heatmap": map[string][]string{
		"crypto":      {"BTC", "ETH", "SOL", "BNB", "XRP", "DOGE", "ADA", "AVAX", "DOT", "LINK", "LTC", "MATIC"},
		"us_stocks":   {"AAPL", "MSFT", "NVDA", "GOOGL", "AMZN", "META", "TSLA", "NFLX"},
		"hk_stocks":   {"0700.HK", "9988.HK", "3690.HK", "1810.HK", "2318.HK", "1299.HK", "0883.HK", "0005.HK"},
		"commodities": {"GC=F", "SI=F", "CL=F", "NG=F", "ZW=F", "ZC=F"},
		"sectors":     {"XLK", "XLF", "XLV", "XLE", "XLI", "XLP", "XLU", "XLRE", "XLB", "XLC"},
		"forex":       {"EURUSD=X", "GBPUSD=X", "USDJPY=X", "USDCHF=X", "AUDUSD=X", "USDCAD=X"},
	},
	"global_indices": []map[string]string{
		{"symbol": "SPX", "name": "S&P 500", "region": "US"},
		{"symbol": "NDX", "name": "NASDAQ 100", "region": "US"},
		{"symbol": "DJI", "name": "Dow Jones", "region": "US"},
		{"symbol": "SH", "name": "上证综指", "region": "CN"},
		{"symbol": "HSI", "name": "恒生指数", "region": "HK"},
		{"symbol": "N225", "name": "日经225", "region": "JP"},
		{"symbol": "FTSE", "name": "富时100", "region": "UK"},
		{"symbol": "DAX", "name": "DAX 40", "region": "DE"},
	},
}

var defaultExchanges = map[string]any{
	"exchanges": []map[string]any{
		{"key": "binance", "label": "Binance", "status": "active", "supports": []string{"spot", "futures", "margin"}},
		{"key": "okx", "label": "OKX", "status": "active", "supports": []string{"spot", "futures", "margin"}},
		{"key": "bybit", "label": "Bybit", "status": "active", "supports": []string{"spot", "futures"}},
		{"key": "coinbase", "label": "Coinbase", "status": "stub", "supports": []string{"spot"}},
		{"key": "gate", "label": "Gate.io", "status": "stub", "supports": []string{"spot", "futures"}},
		{"key": "mexc", "label": "MEXC", "status": "stub", "supports": []string{"spot"}},
		{"key": "bitget", "label": "Bitget", "status": "stub", "supports": []string{"spot", "futures"}},
		{"key": "kucoin", "label": "KuCoin", "status": "stub", "supports": []string{"spot", "futures"}},
		{"key": "htx", "label": "HTX", "status": "stub", "supports": []string{"spot"}},
		{"key": "phemex", "label": "Phemex", "status": "stub", "supports": []string{"spot", "futures"}},
		{"key": "deribit", "label": "Deribit", "status": "stub", "supports": []string{"futures"}},
	},
}

// defaultAIModels 是设置页 AI 模型目录（GET /config/ai-models）。
// key 与 internal/ai 运行时 provider 注册表逐一对应（openrouter 为聚合网关，无运行时注册）。
var defaultAIModels = map[string]any{
	"providers": []map[string]any{
		{
			"key":     "openai",
			"label":   "OpenAI",
			"models":  []string{"gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-4.1", "o3", "gpt-4o"},
			"baseUrl": "https://api.openai.com/v1",
			"default": "gpt-5.5",
		},
		{
			"key":     "claude",
			"label":   "Anthropic Claude",
			"models":  []string{"claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-4-6", "claude-sonnet-4-5", "claude-haiku-4-5"},
			"baseUrl": "https://api.anthropic.com",
			"default": "claude-opus-4-7",
		},
		{
			"key":     "gemini",
			"label":   "Google Gemini",
			"models":  []string{"gemini-3.1-pro-preview", "gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite"},
			"baseUrl": "https://generativelanguage.googleapis.com",
			"default": "gemini-3.1-pro-preview",
		},
		{
			"key":     "deepseek",
			"label":   "DeepSeek",
			"models":  []string{"deepseek-chat", "deepseek-reasoner"},
			"baseUrl": "https://api.deepseek.com/v1",
			"default": "deepseek-chat",
		},
		{
			"key":     "qwen",
			"label":   "通义千问",
			"models":  []string{"qwen3-max", "qwen-plus", "qwen-turbo", "qwen3-coder-plus"},
			"baseUrl": "https://dashscope.aliyuncs.com/compatible-mode/v1",
			"default": "qwen3-max",
		},
		{
			"key":     "glm",
			"label":   "智谱 GLM",
			"models":  []string{"glm-4.7", "glm-4.6", "glm-4-plus", "glm-4.5-air"},
			"baseUrl": "https://open.bigmodel.cn/api/paas/v4",
			"default": "glm-4.7",
		},
		{
			"key":     "kimi",
			"label":   "Kimi（月之暗面）",
			"models":  []string{"kimi-k2.5", "kimi-k2-0905-preview", "moonshot-v1-128k", "moonshot-v1-32k", "k3", "kimi-for-coding", "kimi-for-coding-highspeed"},
			"baseUrl": "https://api.moonshot.cn/v1",
			"default": "kimi-k2.5",
		},
		{
			"key":     "doubao",
			"label":   "豆包（火山引擎）",
			"models":  []string{"doubao-seed-1-6", "doubao-pro-32k", "doubao-lite-32k"},
			"baseUrl": "https://ark.cn-beijing.volces.com/api/v3",
			"default": "doubao-seed-1-6",
		},
		{
			"key":     "hunyuan",
			"label":   "腾讯混元",
			"models":  []string{"hunyuan-pro", "hunyuan-standard", "hunyuan-lite"},
			"baseUrl": "https://api.hunyuan.cloud.tencent.com/v1",
			"default": "hunyuan-pro",
		},
		{
			"key":     "llama",
			"label":   "Llama（Groq）",
			"models":  []string{"llama-4-maverick", "llama-4-scout"},
			"baseUrl": "https://api.groq.com/openai/v1",
			"default": "llama-4-maverick",
		},
		{
			"key":     "mistral",
			"label":   "Mistral",
			"models":  []string{"mistral-large-3", "mistral-medium", "codestral"},
			"baseUrl": "https://api.mistral.ai/v1",
			"default": "mistral-large-3",
		},
		{
			"key":     "openrouter",
			"label":   "OpenRouter（聚合）",
			"models":  []string{"openai/gpt-5.5", "anthropic/claude-opus-4.7", "google/gemini-3.1-pro-preview", "deepseek/deepseek-chat", "qwen/qwen3-max"},
			"baseUrl": "https://openrouter.ai/api/v1",
			"default": "openai/gpt-5.5",
		},
	},
}

// ── Helper to read JSON file with fallback ──

func readJSONFile(path string, fallback map[string]any) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return fallback
	}
	return result
}

func getDataDir() string {
	if dir := os.Getenv("GATEWAY_DATA_DIR"); dir != "" {
		return dir
	}
	// Try relative to working directory
	if _, err := os.Stat("data"); err == nil {
		return "data"
	}
	// Fallback to a subdirectory of the executable path
	if ex, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(ex), "data")
	}
	return "data"
}

// ── Handlers ──

// GetMarkets returns the trading pair list with precision info.
func GetMarkets(c *gin.Context) {
	dataDir := getDataDir()
	data := readJSONFile(filepath.Join(dataDir, "markets.json"), defaultMarkets)
	c.JSON(http.StatusOK, data)
}

// GetIndices returns heatmap categories and symbol lists.
func GetIndices(c *gin.Context) {
	dataDir := getDataDir()
	data := readJSONFile(filepath.Join(dataDir, "indices.json"), defaultIndices)
	c.JSON(http.StatusOK, data)
}

// GetExchanges returns supported exchange list with status.
func GetExchanges(c *gin.Context) {
	cfg := store.GetConfig()
	exchanges, _ := cfg["exchanges"].(map[string]any)

	result := make(map[string]any)
	for _, ex := range defaultExchanges["exchanges"].([]map[string]any) {
		key := ex["key"].(string)
		item := map[string]any{
			"enabled":         false,
			"has_credentials": false,
			"testnet":         false,
			"futures":         false,
		}
		if raw, ok := exchanges[key]; ok {
			if m, ok := raw.(map[string]any); ok {
				if v, ok := m["enabled"].(bool); ok {
					item["enabled"] = v
				}
				if v, ok := m["testnet"].(bool); ok {
					item["testnet"] = v
				}
				apiKey, _ := m["api_key"].(string)
				secret, _ := m["secret"].(string)
				item["has_credentials"] = apiKey != "" && secret != ""
				// P0-1：明文已迁移进保险库——以 vault 为准回显"已配置"。
				if !item["has_credentials"].(bool) {
					_, _, _, verr := store.GetVault().GetOrReload(key)
					item["has_credentials"] = verr == nil
					// 区分"未配置"与"已配置但解密失败"（主密钥不匹配/密钥轮换后
					// 旧密文未迁移）——后者必须显式报错引导用户重新录入。
					if verr != nil && errors.Is(verr, store.ErrVaultDecrypt) {
						item["credential_error"] = "保险库解密失败：主密钥不匹配或密文损坏（疑似密钥轮换），请重新录入该交易所 API 凭证"
					}
				}
			}
		}
		result[key] = item
	}

	c.JSON(http.StatusOK, result)
}

// GetAIModels returns available AI model list.
func GetAIModels(c *gin.Context) {
	// Allow overriding via environment
	if envModels := os.Getenv("AI_MODELS_JSON"); envModels != "" {
		var result map[string]any
		if err := json.Unmarshal([]byte(envModels), &result); err == nil {
			c.JSON(http.StatusOK, result)
			return
		}
	}
	c.JSON(http.StatusOK, aiModelsWithRuntimeStatus())
}

// aiModelsWithRuntimeStatus 在目录副本上叠加运行时状态：每个 provider 附
// configured（注册表 APIKey 非空）与 current_model（注册表当前模型），
// 便于设置页展示"已配置"标识与生效模型；openrouter 等未注册 provider 恒为未配置。
func aiModelsWithRuntimeStatus() map[string]any {
	out := map[string]any{}
	for k, v := range defaultAIModels {
		out[k] = v
	}
	providers, _ := defaultAIModels["providers"].([]map[string]any)
	enriched := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		item := make(map[string]any, len(p)+2)
		for k, v := range p {
			item[k] = v
		}
		key, _ := p["key"].(string)
		if rp := ai.GetProvider(ai.NormalizeProviderName(key)); rp != nil {
			item["configured"] = rp.APIKey != ""
			item["current_model"] = rp.Model
		} else {
			item["configured"] = false
			item["current_model"] = p["default"]
		}
		enriched = append(enriched, item)
	}
	out["providers"] = enriched
	return out
}

// GetConversionRate returns USD/CNY conversion rate.
// ── 实时模型列表（设置页"拉取模型"按钮）──

var listModelsCacheMu sync.RWMutex
var listModelsCache = map[string]listModelsCacheEntry{}

type listModelsCacheEntry struct {
	models    []string
	fetchedAt time.Time
}

const listModelsCacheTTL = 5 * time.Minute

type listModelsRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	BaseURL  string `json:"base_url"`
}

// ListAIProviderModels 从厂商 API 实时拉取模型列表。
// POST /config/ai-provider-models，body {provider, api_key?, base_url?}；
// 凭证回落顺序：请求体 → 已存配置 ai.{provider} → 运行时注册表 → 目录默认。
// 结果按 provider+key 哈希缓存 5 分钟，避免重复点击打满厂商限流。
func ListAIProviderModels(c *gin.Context) {
	var body listModelsRequest
	if err := c.ShouldBindJSON(&body); err != nil || body.Provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "provider is required"})
		return
	}
	id := ai.NormalizeProviderName(body.Provider)

	// 与 runAITest 同一套回落链解析凭证。
	cfg := store.GetConfig()
	var saved map[string]any
	if aiCfg, ok := cfg["ai"].(map[string]any); ok {
		saved, _ = aiCfg[id].(map[string]any)
		if saved == nil {
			if legacy := ai.LegacyProviderName(id); legacy != "" {
				saved, _ = aiCfg[legacy].(map[string]any)
			}
		}
	}
	apiKey := firstNonEmpty(body.APIKey, getString(saved, "api_key", ""))
	model := getString(saved, "model", "")
	baseURL := firstNonEmpty(body.BaseURL, getString(saved, "base_url", ""))
	if reg := ai.GetProvider(id); reg != nil {
		apiKey = firstNonEmpty(apiKey, reg.APIKey)
		baseURL = firstNonEmpty(baseURL, reg.BaseURL)
		model = firstNonEmpty(model, reg.Model)
	}
	for _, p := range defaultAIModels["providers"].([]map[string]any) {
		if p["key"] == id {
			baseURL = firstNonEmpty(baseURL, getString(p, "baseUrl", ""))
			break
		}
	}
	if apiKey == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": id + " 未配置 API Key，请先填写并保存"})
		return
	}

	cacheKey := id + "|" + fmt.Sprintf("%x", sha1.Sum([]byte(apiKey)))
	listModelsCacheMu.RLock()
	entry, hit := listModelsCache[cacheKey]
	listModelsCacheMu.RUnlock()
	if hit && time.Since(entry.fetchedAt) < listModelsCacheTTL {
		c.JSON(http.StatusOK, gin.H{"success": true, "models": entry.models, "cached": true})
		return
	}

	models, err := ai.ListModels(id, baseURL, apiKey)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": id + " 拉取失败: " + err.Error()})
		return
	}
	listModelsCacheMu.Lock()
	listModelsCache[cacheKey] = listModelsCacheEntry{models: models, fetchedAt: time.Now()}
	listModelsCacheMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"success": true, "models": models, "current": model})
}

func GetConversionRate(c *gin.Context) {
	// Try to fetch from a simple external API first
	rate := fetchUSDCNYRate()
	c.JSON(http.StatusOK, map[string]any{
		"rate":      rate,
		"from":      "USD",
		"to":        "CNY",
		"timestamp": time.Now().Unix(),
	})
}

func fetchUSDCNYRate() float64 {
	// Check environment override first
	if envRate := os.Getenv("USD_CNY_RATE"); envRate != "" {
		if r, err := strconv.ParseFloat(envRate, 64); err == nil {
			return r
		}
	}

	// Try to fetch from exchangerate-api.com (free tier, no auth needed)
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", "https://api.exchangerate-api.com/v4/latest/USD", nil)
	resp, err := client.Do(req)
	if err != nil || resp == nil {
		return 7.25 // fallback default
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 7.25
	}

	var result struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 7.25
	}
	if r, ok := result.Rates["CNY"]; ok && r > 0 {
		return r
	}
	return 7.25
}

// ── Strategy Configuration Endpoints ────────────────────────────

// defaultStrategyConfig holds default configuration for supported strategies.
var defaultStrategyConfig = map[string]any{
	"strategies": []map[string]any{
		{
			"key":         "martin_trend_v2",
			"label":       "马丁趋势策略V2",
			"description": "倍投补仓趋势策略，支持防瀑布保护和回调确认",
			"category":    "futures",
			"parameters": map[string]any{
				"first_order_amount":     100.0,
				"order_count":            7,
				"add_position_spread":    0.03,
				"add_position_callback":  0.003,
				"take_profit_ratio":      0.013,
				"profit_callback":        0.003,
				"double_first_order":     false,
				"loop_type":              "cycle",
				"loop_count":             100,
				"enable_add_position":    true,
				"flash_crash_protection": 0.02,
			},
		},
		{
			"key":         "wallstreet_v2",
			"label":       "华尔街策略V2",
			"description": "斐波那契数列仓位管理策略，风险控制更优",
			"category":    "futures",
			"parameters": map[string]any{
				"first_order_amount":     100.0,
				"order_count":            7,
				"add_position_spread":    0.03,
				"add_position_callback":  0.003,
				"take_profit_ratio":      0.013,
				"profit_callback":        0.003,
				"double_first_order":     false,
				"loop_type":              "cycle",
				"loop_count":             100,
				"enable_add_position":    true,
				"flash_crash_protection": 0.02,
			},
		},
	},
	"contract_defaults": map[string]any{
		"leverage":                10.0,
		"direction":               "both",
		"margin_mode":             "cross",
		"max_positions":           10,
		"maintenance_margin_rate": 0.004,
	},
	"risk_defaults": map[string]any{
		"warning_threshold":     0.5,
		"danger_threshold":      0.75,
		"critical_threshold":    0.9,
		"flash_crash_window":    "1m",
		"flash_crash_threshold": 0.02,
	},
}

// GetStrategyDefaults returns default strategy configurations.
func GetStrategyDefaults(c *gin.Context) {
	strategyKey := c.Query("key")
	data := defaultStrategyConfig

	if strategyKey != "" {
		if strategies, ok := data["strategies"].([]map[string]any); ok {
			for _, s := range strategies {
				if s["key"] == strategyKey {
					c.JSON(http.StatusOK, s)
					return
				}
			}
			c.JSON(http.StatusNotFound, gin.H{"detail": "strategy config not found"})
			return
		}
	}

	c.JSON(http.StatusOK, data)
}

// GetContractDefaults returns default contract trading parameters.
func GetContractDefaults(c *gin.Context) {
	c.JSON(http.StatusOK, defaultStrategyConfig["contract_defaults"])
}

// CalculatePositionSizes computes position sizes for martingale or fibonacci strategies.
func CalculatePositionSizes(c *gin.Context) {
	var req struct {
		StrategyType     string  `json:"strategy_type" binding:"required"`
		FirstOrderAmount float64 `json:"first_order_amount" binding:"required,min=1"`
		OrderCount       int     `json:"order_count" binding:"required,min=1,max=20"`
		DoubleFirstOrder bool    `json:"double_first_order"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var positions []float64
	var total float64

	switch req.StrategyType {
	case "martin_trend", "martin_trend_v2", "martingale":
		s := strategy.NewMartinStrategy()
		s.FirstOrderAmount = req.FirstOrderAmount
		s.OrderCount = req.OrderCount
		s.DoubleFirstOrder = req.DoubleFirstOrder
		positions = s.CalculatePositions()
	case "wallstreet", "wallstreet_v2":
		s := strategy.NewWallStreetStrategy()
		s.FirstOrderAmount = req.FirstOrderAmount
		s.OrderCount = req.OrderCount
		s.DoubleFirstOrder = req.DoubleFirstOrder
		positions = s.CalculatePositions()
	default:
		c.JSON(http.StatusBadRequest, gin.H{"detail": "unsupported strategy_type: " + req.StrategyType})
		return
	}

	for _, p := range positions {
		total += p
	}

	c.JSON(http.StatusOK, map[string]any{
		"strategy_type":      req.StrategyType,
		"first_order_amount": req.FirstOrderAmount,
		"order_count":        req.OrderCount,
		"double_first_order": req.DoubleFirstOrder,
		"positions":          positions,
		"total":              total,
		"ratio":              calculatePositionRatios(positions),
	})
}

// calculatePositionRatios computes each position as a ratio of total.
func calculatePositionRatios(positions []float64) []float64 {
	total := 0.0
	for _, p := range positions {
		total += p
	}
	if total <= 0 {
		return nil
	}
	ratios := make([]float64, len(positions))
	for i, p := range positions {
		ratios[i] = p / total
	}
	return ratios
}

// FlashCrashCheck checks if a given price history indicates a flash crash.
func FlashCrashCheck(c *gin.Context) {
	var req struct {
		Prices     []float64 `json:"prices" binding:"required"`
		Timestamps []int64   `json:"timestamps" binding:"required"`
		Threshold  float64   `json:"threshold"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	if len(req.Prices) != len(req.Timestamps) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "prices and timestamps must have same length"})
		return
	}
	if len(req.Prices) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "at least 2 price points required"})
		return
	}

	threshold := req.Threshold
	if threshold <= 0 {
		threshold = 0.02
	}

	detector := strategy.NewFlashCrashDetectorWithParams(0, threshold)
	priceHistory := make([]strategy.PricePoint, len(req.Prices))
	for i := range req.Prices {
		priceHistory[i] = strategy.PricePoint{
			Price:     req.Prices[i],
			Timestamp: req.Timestamps[i],
		}
		detector.AddPrice(req.Prices[i], req.Timestamps[i])
	}

	isFlashCrash := detector.IsFlashCrash()
	drop := detector.PriceDropInWindow()

	c.JSON(http.StatusOK, map[string]any{
		"is_flash_crash":     isFlashCrash,
		"price_drop":         drop,
		"threshold":          threshold,
		"data_points":        len(req.Prices),
		"window_first_price": req.Prices[0],
		"window_last_price":  req.Prices[len(req.Prices)-1],
	})
}
