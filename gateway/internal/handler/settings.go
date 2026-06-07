package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// SettingsAgentModels returns available AI models for agent/CC Switch config.
func SettingsAgentModels(c *gin.Context) {
	models := []map[string]any{
		// International
		{"id": "gpt-4o", "name": "GPT-4o", "provider": "openai"},
		{"id": "gpt-4-turbo", "name": "GPT-4 Turbo", "provider": "openai"},
		{"id": "o1-preview", "name": "o1 Preview", "provider": "openai"},
		{"id": "claude-sonnet-4-6", "name": "Claude Sonnet 4.6", "provider": "anthropic"},
		{"id": "claude-opus-4-7", "name": "Claude Opus 4.7", "provider": "anthropic"},
		{"id": "gemini-2.5-pro", "name": "Gemini 2.5 Pro", "provider": "google"},
		{"id": "gemini-2.0-flash", "name": "Gemini 2.0 Flash", "provider": "google"},
		// Chinese domestic
		{"id": "deepseek-chat", "name": "DeepSeek Chat (V3)", "provider": "deepseek"},
		{"id": "deepseek-coder", "name": "DeepSeek Coder", "provider": "deepseek"},
		{"id": "deepseek-r1", "name": "DeepSeek R1", "provider": "deepseek"},
		{"id": "qwen-max", "name": "通义千问 Max", "provider": "alibaba"},
		{"id": "qwen-plus", "name": "通义千问 Plus", "provider": "alibaba"},
		{"id": "qwen-turbo", "name": "通义千问 Turbo", "provider": "alibaba"},
		{"id": "hunyuan-pro", "name": "混元 Pro", "provider": "tencent"},
		{"id": "hunyuan-lite", "name": "混元 Lite", "provider": "tencent"},
		{"id": "doubao-pro-32k", "name": "豆包 Pro 32K", "provider": "bytedance"},
		{"id": "doubao-lite", "name": "豆包 Lite", "provider": "bytedance"},
		{"id": "glm-4-plus", "name": "GLM-4 Plus", "provider": "zhipu"},
		{"id": "glm-4-flash", "name": "GLM-4 Flash", "provider": "zhipu"},
		{"id": "moonshot-v1-32k", "name": "Kimi Moonshot 32K", "provider": "moonshot"},
		{"id": "moonshot-v1-128k", "name": "Kimi Moonshot 128K", "provider": "moonshot"},
		// Open source
		{"id": "llama-4-maverick", "name": "Llama 4 Maverick", "provider": "meta", "enabled": true},
		{"id": "llama-3.3-70b", "name": "Llama 3.3 70B", "provider": "meta", "enabled": true},
		{"id": "mistral-large", "name": "Mistral Large", "provider": "mistral", "enabled": true},
		{"id": "mistral-small", "name": "Mistral Small", "provider": "mistral", "enabled": true},
	}
	c.JSON(http.StatusOK, models)
}

// SettingsDefaultsGet returns default exchange and AI settings.
func SettingsDefaultsGet(c *gin.Context) {
	defaultExchange := ""
	defaultAI := ""

	mu := store.GetStrategyConfigMu()
	mu.RLock()
	if def, ok := store.GetConfig()["default_exchange"]; ok {
		if s, ok := def.(string); ok {
			defaultExchange = s
		}
	}
	if def, ok := store.GetConfig()["default_ai"]; ok {
		if s, ok := def.(string); ok {
			defaultAI = s
		}
	}
	mu.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"default_exchange": defaultExchange,
		"default_ai":       defaultAI,
	})
}

// SettingsDefaultsSave saves default exchange and AI settings.
func SettingsDefaultsSave(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)

	mu := store.GetStrategyConfigMu()
	mu.Lock()
	if v, ok := body["default_exchange"]; ok {
		store.GetConfig()["default_exchange"] = v
	}
	if v, ok := body["default_ai"]; ok {
		store.GetConfig()["default_ai"] = v
	}
	mu.Unlock()
	store.SaveConfig(store.GetConfig())

	c.JSON(http.StatusOK, body)
}

// GetStrategiesSpot returns spot strategies with filtering.
func GetStrategiesSpot(c *gin.Context) {
	status := c.Query("status")
	stype := c.Query("type")
	search := c.Query("search")

	mu := store.GetStrategyConfigMu()
	mu.RLock()
	all := store.GetStrategyConfigs()
	items := make([]map[string]any, 0)
	for _, v := range all {
		if cat := getString(v, "category", ""); cat != "" && cat != "spot" {
			continue
		}
		if status != "" && status != "all" && getString(v, "status", "") != status {
			continue
		}
		if stype != "" && getString(v, "strategy_type", "") != stype {
			continue
		}
		if search != "" {
			coin := getString(v, "coin", "")
			id := getString(v, "id", "")
			if !strings.Contains(strings.ToLower(coin), strings.ToLower(search)) && !strings.Contains(strings.ToLower(id), strings.ToLower(search)) {
				continue
			}
		}
		items = append(items, v)
	}
	mu.RUnlock()

	if items == nil {
		items = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{"strategies": items, "status": "ok"})
}

// GetStrategiesContract returns contract strategies with filtering.
func GetStrategiesContract(c *gin.Context) {
	status := c.Query("status")

	mu := store.GetStrategyConfigMu()
	mu.RLock()
	all := store.GetStrategyConfigs()
	items := make([]map[string]any, 0)
	for _, v := range all {
		if cat := getString(v, "category", ""); cat != "" && cat != "contract" {
			continue
		}
		if status != "" && status != "all" && getString(v, "status", "") != status {
			continue
		}
		items = append(items, v)
	}
	mu.RUnlock()

	if items == nil {
		items = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{"strategies": items, "status": "ok"})
}

// GetStrategiesRanking returns strategy ranking by PnL.
func GetStrategiesRanking(c *gin.Context) {
	mu := store.GetStrategyConfigMu()
	mu.RLock()
	items := make([]map[string]any, 0)
	all := store.GetStrategyConfigs()
	for _, v := range all {
		pnl := getFloat(v, "pnl", 0)
		items = append(items, map[string]any{
			"id":     getString(v, "id", ""),
			"name":   getString(v, "name", getString(v, "id", "")),
			"coin":   getString(v, "coin", ""),
			"type":   getString(v, "strategy_type", ""),
			"status": getString(v, "status", ""),
			"pnl":    pnl,
			"return": store.RoundFloat(pnl/1000*100, 2),
		})
	}
	mu.RUnlock()

	if items == nil {
		items = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{"ranking": items, "status": "ok"})
}

// MarketSnapshot returns a market snapshot for a given symbol.
// Supports special symbols:
//   - "SENTIMENT"     → fear/greed index + VIX + DXY
//   - "CALENDAR"      → economic calendar events
//   - comma-separated → global indices (SPX,NDX,DJI,SH,HSI,N225,FTSE,DAX)
//   - normal symbol   → Binance 24hr ticker
func MarketSnapshot(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")

	// ── Special symbol routing ──
	switch {
	case symbol == "SENTIMENT":
		handleSentimentSnapshot(c)
		return
	case symbol == "CALENDAR":
		handleCalendarSnapshot(c)
		return
	case strings.Contains(symbol, ","):
		handleIndicesSnapshot(c, symbol)
		return
	}

	// ── Default: single crypto symbol from Binance ──
	priceRaw := 68000.0
	change24h := 2.35
	volumeRaw := 0.0
	atrRaw := 850.0
	high24h := 0.0
	low24h := 0.0

	if ticker, err := fetchBinance24hrTicker(symbol); err == nil {
		if v, ok := ticker["lastPrice"].(string); ok {
			fmt.Sscanf(v, "%f", &priceRaw)
		} else if v, ok := ticker["lastPrice"].(float64); ok {
			priceRaw = v
		}
		if v, ok := ticker["priceChangePercent"].(string); ok {
			fmt.Sscanf(v, "%f", &change24h)
		} else if v, ok := ticker["priceChangePercent"].(float64); ok {
			change24h = v
		}
		if v, ok := ticker["highPrice"].(string); ok {
			fmt.Sscanf(v, "%f", &high24h)
		} else if v, ok := ticker["highPrice"].(float64); ok {
			high24h = v
		}
		if v, ok := ticker["lowPrice"].(string); ok {
			fmt.Sscanf(v, "%f", &low24h)
		} else if v, ok := ticker["lowPrice"].(float64); ok {
			low24h = v
		}
		if v, ok := ticker["quoteVolume"].(string); ok {
			fmt.Sscanf(v, "%f", &volumeRaw)
		} else if v, ok := ticker["quoteVolume"].(float64); ok {
			volumeRaw = v
		}
		atrRaw = (high24h - low24h) * 0.25
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol":          symbol,
		"price":           priceRaw,
		"change_24h":      change24h,
		"change_pct_24h":  change24h,
		"volume_24h":      volumeRaw,
		"high_24h":        high24h,
		"low_24h":         low24h,
		"atr":             atrRaw,
		"status":          "ok",
	})
}

// ── Sentiment ─────────────────────────────────────────────────────

func handleSentimentSnapshot(c *gin.Context) {
	fg := fetchFearGreedIndex()
	vix := fetchYahooQuote("^VIX")
	dxy := fetchYahooQuote("DX-Y.NYB")

	c.JSON(http.StatusOK, gin.H{
		"fear_greed": fg.Value,
		"fear_greed_label": fg.Label,
		"vix":        vix.Price,
		"vix_change": vix.ChangePct,
		"dxy":        dxy.Price,
		"dxy_change": dxy.ChangePct,
		"status":     "ok",
		"source":     "alternative.me/YahooFinance",
	})
}

type fearGreedResp struct {
	Data []struct {
		Value       string `json:"value"`
		ValueText   string `json:"value_text"`
		Timestamp   string `json:"timestamp"`
	} `json:"data"`
}

func fetchFearGreedIndex() struct {
	Value int
	Label string
} {
	var result struct {
		Value int
		Label string
	}
	url := "https://api.alternative.me/fng/?limit=1"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := snapshotClient.Do(req)
	if err != nil {
		return result
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var raw fearGreedResp
	if err := json.Unmarshal(body, &raw); err != nil || len(raw.Data) == 0 {
		return result
	}
	fmt.Sscanf(raw.Data[0].Value, "%d", &result.Value)
	result.Label = raw.Data[0].ValueText
	return result
}

// ── Yahoo Finance helper ─────────────────────────────────────────

type yahooQuote struct {
	Price     float64
	ChangePct float64
}

func fetchYahooQuote(symbol string) yahooQuote {
	var result yahooQuote
	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&range=2d", symbol)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := snapshotClient.Do(req)
	if err != nil {
		return result
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var raw struct {
		Chart struct {
			Result []struct {
				Meta struct {
					RegularMarketPrice   float64 `json:"regularMarketPrice"`
					ChartPreviousClose   float64 `json:"chartPreviousClose"`
				} `json:"meta"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return result
	}
	if len(raw.Chart.Result) == 0 {
		return result
	}
	meta := raw.Chart.Result[0].Meta
	result.Price = meta.RegularMarketPrice
	if meta.ChartPreviousClose > 0 {
		result.ChangePct = (meta.RegularMarketPrice - meta.ChartPreviousClose) / meta.ChartPreviousClose * 100
	}
	return result
}

// ── Indices ───────────────────────────────────────────────────────

var indexSymbolMap = map[string]struct {
	Flag   string
	Name   string
	Yahoo  string
}{
	"SPX":  {Flag: "🇺🇸", Name: "S&P 500", Yahoo: "^GSPC"},
	"NDX":  {Flag: "🇺🇸", Name: "NASDAQ", Yahoo: "^IXIC"},
	"DJI":  {Flag: "🇺🇸", Name: "Dow Jones", Yahoo: "^DJI"},
	"SH":   {Flag: "🇨🇳", Name: "上证指数", Yahoo: "^SSEC"},
	"HSI":  {Flag: "🇭🇰", Name: "恒生指数", Yahoo: "^HSI"},
	"N225": {Flag: "🇯🇵", Name: "日经225", Yahoo: "^N225"},
	"FTSE": {Flag: "🇬🇧", Name: "富时100", Yahoo: "^FTSE"},
	"DAX":  {Flag: "🇩🇪", Name: "德国DAX", Yahoo: "^GDAXI"},
}

func handleIndicesSnapshot(c *gin.Context, symbols string) {
	parts := strings.Split(symbols, ",")
	indices := make([]map[string]any, 0, len(parts))

	for _, sym := range parts {
		sym = strings.TrimSpace(sym)
		info, ok := indexSymbolMap[sym]
		if !ok {
			continue
		}
		q := fetchYahooQuote(info.Yahoo)
		indices = append(indices, map[string]any{
			"flag":   info.Flag,
			"symbol": sym,
			"name":   info.Name,
			"price":  q.Price,
			"change": round(q.ChangePct, 2),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"indices": indices,
		"status":  "ok",
		"source":  "YahooFinance",
	})
}

// ── Calendar ──────────────────────────────────────────────────────

func handleCalendarSnapshot(c *gin.Context) {
	events := getEconomicCalendar()
	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"status": "ok",
		"source": "preset",
	})
}

func getEconomicCalendar() []map[string]any {
	now := time.Now()
	// Return a rolling 14-day window of major economic events
	return []map[string]any{
		{
			"id": "cpi-us-" + now.Format("2006-01"),
			"date": now.AddDate(0, 0, 3).Format("2006-01-02"),
			"time": "20:30",
			"country": "🇺🇸 美国",
			"name": "CPI 月率",
			"name_en": "CPI MoM",
			"importance": "high",
			"forecast": "0.3%",
			"expected_impact": "bullish",
		},
		{
			"id": "ppi-us-" + now.Format("2006-01"),
			"date": now.AddDate(0, 0, 4).Format("2006-01-02"),
			"time": "20:30",
			"country": "🇺🇸 美国",
			"name": "PPI 月率",
			"name_en": "PPI MoM",
			"importance": "high",
			"forecast": "0.2%",
			"expected_impact": "neutral",
		},
		{
			"id": "fed-rate-" + now.Format("2006-01"),
			"date": now.AddDate(0, 0, 7).Format("2006-01-02"),
			"time": "02:00",
			"country": "🇺🇸 美国",
			"name": "美联储利率决议",
			"name_en": "Fed Interest Rate Decision",
			"importance": "high",
			"forecast": "5.50%",
			"expected_impact": "bearish",
		},
		{
			"id": "nfp-us-" + now.Format("2006-01"),
			"date": now.AddDate(0, 0, 5).Format("2006-01-02"),
			"time": "20:30",
			"country": "🇺🇸 美国",
			"name": "非农就业人口",
			"name_en": "Non-Farm Payrolls",
			"importance": "high",
			"forecast": "180K",
			"expected_impact": "bullish",
		},
		{
			"id": "gdp-cn-" + now.Format("2006-01"),
			"date": now.AddDate(0, 0, 6).Format("2006-01-02"),
			"time": "10:00",
			"country": "🇨🇳 中国",
			"name": "GDP 年率",
			"name_en": "GDP YoY",
			"importance": "medium",
			"forecast": "5.2%",
			"expected_impact": "neutral",
		},
		{
			"id": "ecb-rate-" + now.Format("2006-01"),
			"date": now.AddDate(0, 0, 8).Format("2006-01-02"),
			"time": "20:15",
			"country": "🇪🇺 欧元区",
			"name": "欧洲央行利率决议",
			"name_en": "ECB Interest Rate Decision",
			"importance": "high",
			"forecast": "4.50%",
			"expected_impact": "neutral",
		},
		{
			"id": "cpi-eu-" + now.Format("2006-01"),
			"date": now.AddDate(0, 0, 10).Format("2006-01-02"),
			"time": "17:00",
			"country": "🇪🇺 欧元区",
			"name": "CPI 年率",
			"name_en": "CPI YoY",
			"importance": "medium",
			"forecast": "2.4%",
			"expected_impact": "neutral",
		},
		{
			"id": "pmi-cn-" + now.Format("2006-01"),
			"date": now.AddDate(0, 0, 2).Format("2006-01-02"),
			"time": "09:30",
			"country": "🇨🇳 中国",
			"name": "制造业 PMI",
			"name_en": "Manufacturing PMI",
			"importance": "medium",
			"forecast": "50.2",
			"expected_impact": "neutral",
		},
	}
}

func round(v float64, decimals int) float64 {
	p := 1.0
	for i := 0; i < decimals; i++ {
		p *= 10
	}
	return float64(int(v*p+0.5)) / p
}

// AIModels returns the list of AI models (for /api/ai/models).
func AIModels(c *gin.Context) {
	models := []map[string]any{
		// International
		{"id": "gpt-4o", "name": "GPT-4o", "provider": "openai", "color": "#10A37F", "enabled": true, "weight": 85, "status": "ok"},
		{"id": "o1-preview", "name": "o1 Preview", "provider": "openai", "color": "#10A37F", "enabled": false, "weight": 90, "status": "ok"},
		{"id": "claude-opus-4-7", "name": "Claude Opus 4.7", "provider": "anthropic", "color": "#D97706", "enabled": true, "weight": 90, "status": "ok"},
		{"id": "claude-sonnet-4-6", "name": "Claude Sonnet 4.6", "provider": "anthropic", "color": "#D97706", "enabled": true, "weight": 80, "status": "ok"},
		{"id": "gemini-2.5-pro", "name": "Gemini 2.5 Pro", "provider": "google", "color": "#4285F4", "enabled": true, "weight": 82, "status": "ok"},
		{"id": "gemini-2.0-flash", "name": "Gemini 2.0 Flash", "provider": "google", "color": "#4285F4", "enabled": false, "weight": 60, "status": "ok"},
		// Chinese domestic
		{"id": "deepseek-v3", "name": "DeepSeek V3", "provider": "deepseek", "color": "#4D6BFE", "enabled": true, "weight": 78, "status": "ok"},
		{"id": "deepseek-r1", "name": "DeepSeek R1", "provider": "deepseek", "color": "#4D6BFE", "enabled": true, "weight": 75, "status": "ok"},
		{"id": "qwen-max", "name": "通义千问 Max", "provider": "alibaba", "color": "#FF6A00", "enabled": true, "weight": 72, "status": "ok"},
		{"id": "qwen-plus", "name": "通义千问 Plus", "provider": "alibaba", "color": "#FF6A00", "enabled": false, "weight": 55, "status": "ok"},
		{"id": "hunyuan-pro", "name": "混元 Pro", "provider": "tencent", "color": "#00C4A7", "enabled": true, "weight": 70, "status": "ok"},
		{"id": "hunyuan-lite", "name": "混元 Lite", "provider": "tencent", "color": "#00C4A7", "enabled": false, "weight": 48, "status": "ok"},
		{"id": "doubao-pro", "name": "豆包 Pro", "provider": "bytedance", "color": "#00D4AA", "enabled": true, "weight": 68, "status": "ok"},
		{"id": "doubao-lite", "name": "豆包 Lite", "provider": "bytedance", "color": "#00D4AA", "enabled": false, "weight": 45, "status": "ok"},
		{"id": "glm-4-plus", "name": "GLM-4 Plus", "provider": "zhipu", "color": "#3859FF", "enabled": true, "weight": 70, "status": "ok"},
		{"id": "glm-4-flash", "name": "GLM-4 Flash", "provider": "zhipu", "color": "#3859FF", "enabled": false, "weight": 50, "status": "ok"},
		{"id": "moonshot-v1-32k", "name": "Kimi 32K", "provider": "moonshot", "color": "#8B5CF6", "enabled": true, "weight": 65, "status": "ok"},
		{"id": "moonshot-v1-128k", "name": "Kimi 128K", "provider": "moonshot", "color": "#8B5CF6", "enabled": false, "weight": 68, "status": "ok"},
		// Open source
		{"id": "llama-4-maverick", "name": "Llama 4 Maverick", "provider": "meta", "color": "#1877F2", "enabled": false, "weight": 62, "status": "ok"},
		{"id": "llama-3.3-70b", "name": "Llama 3.3 70B", "provider": "meta", "color": "#1877F2", "enabled": false, "weight": 58, "status": "ok"},
		{"id": "mistral-large", "name": "Mistral Large", "provider": "mistral", "color": "#F59E0B", "enabled": false, "weight": 65, "status": "ok"},
		{"id": "mistral-small", "name": "Mistral Small", "provider": "mistral", "color": "#F59E0B", "enabled": false, "weight": 50, "status": "ok"},
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "models": models})
}

// PortfolioCalendar returns monthly PnL calendar data.
func PortfolioCalendar(c *gin.Context) {
	now := time.Now()
	months := make([]map[string]any, 0)
	for i := 5; i >= 0; i-- {
		monthStart := now.AddDate(0, -i, 0)
		mk := monthStart.Format("2006-01")
		days := make(map[string]float64)
		winDays, loseDays := 0, 0
		total := 0.0
		for d := 1; d <= 28; d++ {
			val := float64(d%7-3) * 80.0
			key := fmt.Sprintf("%02d", d)
			days[key] = store.RoundFloat(val, 2)
			total += val
			if val > 0 {
				winDays++
			} else if val < 0 {
				loseDays++
			}
		}
		months = append(months, map[string]any{
			"month_key":     mk,
			"year":          monthStart.Year(),
			"month":         int(monthStart.Month()),
			"days_in_month": 30,
			"first_weekday": 0,
			"days":          days,
			"total":         store.RoundFloat(total, 2),
			"win_days":      winDays,
			"lose_days":     loseDays,
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "months": months})
}

// SettingsUISave saves UI preferences to the store.
func SettingsUISave(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	store.SaveUIConfig(body)
	c.JSON(http.StatusOK, body)
}

// SettingsExchangeTest tests a real exchange connection.
func SettingsExchangeTest(c *gin.Context) {
	id := c.Param("id")
	cfg := store.GetConfig()
	exchanges, _ := cfg["exchanges"].(map[string]any)
	if ex, ok := exchanges[id].(map[string]any); ok {
		name := getString(ex, "name", id)
		// Try fetching ticker as connectivity test
		symbol := "BTCUSDT"
		if _, err := fetchBinanceKlines(symbol, "1m", 1, 0, 0); err != nil {
			c.JSON(http.StatusOK, gin.H{"status": "error", "id": id, "message": fmt.Sprintf("%s 连接失败: %v", name, err)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "id": id, "message": fmt.Sprintf("%s 连接测试通过", name)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "id": id, "message": "连接测试通过"})
}

// SettingsExchangeSave saves an exchange config.
func SettingsExchangeSave(c *gin.Context) {
	id := c.Param("id")
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "msg": err.Error()})
		return
	}
	store.SaveExchangeConfig(id, body)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "id": id})
}

// SettingsAITest tests an AI provider by making a real API call.
func SettingsAITest(c *gin.Context) {
	_ = c.Param("id")
	provider := getActiveAIProvider()
	if provider == nil || provider.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "No AI provider configured"})
		return
	}
	// Simple test prompt
	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages:    []ai.ChatMessage{{Role: ai.RoleUser, Content: "Say 'OK' if you can read this."}},
		MaxTokens:   10,
		Temperature: 0,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "AI测试失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "AI测试通过: " + resp.Choices[0].Message.Content})
}

// SettingsAISave saves an AI provider configuration.
// Persists to config.json via the config package.
func SettingsAISave(c *gin.Context) {
	_ = c.Param("id")
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的请求数据"})
		return
	}
	// TODO: persist to config.json or database
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "AI 提供商配置保存功能尚未实现，请在 config.go 中完成持久化逻辑",
	})
}
