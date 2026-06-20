package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
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

var defaultAIModels = map[string]any{
	"providers": []map[string]any{
		{
			"key":     "openai",
			"label":   "OpenAI",
			"models":  []string{"gpt-4o", "gpt-4-turbo", "gpt-4o-mini", "o1-preview", "o1-mini"},
			"baseUrl": "https://api.openai.com/v1",
		},
		{
			"key":     "anthropic",
			"label":   "Anthropic",
			"models":  []string{"claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-5-haiku-20241022"},
			"baseUrl": "https://api.anthropic.com/v1",
		},
		{
			"key":     "deepseek",
			"label":   "DeepSeek",
			"models":  []string{"deepseek-chat", "deepseek-coder", "deepseek-reasoner"},
			"baseUrl": "https://api.deepseek.com/v1",
		},
		{
			"key":     "openrouter",
			"label":   "OpenRouter",
			"models":  []string{"openai/gpt-4o", "anthropic/claude-3.5-sonnet", "deepseek/deepseek-chat"},
			"baseUrl": "https://openrouter.ai/api/v1",
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
	c.JSON(http.StatusOK, defaultExchanges)
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
	c.JSON(http.StatusOK, defaultAIModels)
}

// GetConversionRate returns USD/CNY conversion rate.
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
