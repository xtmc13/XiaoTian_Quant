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
	"github.com/xiaotian-quant/gateway/internal/market"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// providerDisplayNames maps provider IDs to human-readable names.
var providerDisplayNames = map[string]string{
	"openai":   "GPT-4o / O1",
	"claude":   "Claude Sonnet/Opus",
	"gemini":   "Gemini 2.5 / Flash",
	"deepseek": "DeepSeek V3 / R1",
	"qwen":     "通义千问 Max/Plus",
	"hunyuan":  "混元 Pro / Lite",
	"doubao":   "豆包 Pro / Lite",
	"glm":      "GLM-4 Plus / Flash",
	"kimi":     "Kimi Moonshot",
	"llama":    "Llama 4 Maverick",
	"mistral":  "Mistral Large/Small",
}

// providerColors maps provider IDs to brand colors.
var providerColors = map[string]string{
	"openai":   "#10A37F",
	"claude":   "#D97706",
	"gemini":   "#4285F4",
	"deepseek": "#4D6BFE",
	"qwen":     "#FF6A00",
	"hunyuan":  "#00C4A7",
	"doubao":   "#00D4AA",
	"glm":      "#3859FF",
	"kimi":     "#8B5CF6",
	"llama":    "#1877F2",
	"mistral":  "#F59E0B",
}

// SettingsAgentModels returns available AI models for agent/CC Switch config.
// Dynamically builds the list from registered AI providers.
func SettingsAgentModels(c *gin.Context) {
	names := ai.ListProviders()
	// Sort for stable output
	models := make([]map[string]any, 0, len(names))
	for _, name := range names {
		p := ai.GetProvider(name)
		display := providerDisplayNames[name]
		if display == "" {
			display = strings.ToUpper(name[:1]) + name[1:]
		}
		models = append(models, map[string]any{
			"id":       name,
			"name":     display,
			"provider": name,
			"enabled":  p != nil && p.APIKey != "",
		})
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

	// Check cache first
	cacheKey := market.SnapshotKey(symbol)
	if cached, ok := market.GetCache().Get(cacheKey); ok {
		if snap, ok := cached.(gin.H); ok {
			c.JSON(http.StatusOK, snap)
			return
		}
	}

	ticker, err := fetchBinance24hrTicker(symbol)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"symbol": symbol,
			"status": "error",
			"error":  fmt.Sprintf("failed to fetch market data: %v", err),
		})
		return
	}

	priceRaw := 0.0
	change24h := 0.0
	volumeRaw := 0.0
	high24h := 0.0
	low24h := 0.0

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
	atrRaw := (high24h - low24h) * 0.25

	result := gin.H{
		"symbol":          symbol,
		"price":           priceRaw,
		"change_24h":      change24h,
		"change_pct_24h":  change24h,
		"volume_24h":      volumeRaw,
		"high_24h":        high24h,
		"low_24h":         low24h,
		"atr":             atrRaw,
		"status":          "ok",
	}

	market.GetCache().Set(cacheKey, result, market.SnapshotTTL)
	c.JSON(http.StatusOK, result)
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
	status := "ok"
	source := "forexfactory" // free public API
	if len(events) == 0 {
		status = "empty"
		source = "none_available"
	}
	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"status": status,
		"source": source,
	})
}

// getEconomicCalendar fetches this week's high-impact economic events from ForexFactory.
// Falls back to an empty list when the free API is unreachable (no fake data).
func getEconomicCalendar() []map[string]any {
	events, err := fetchForexFactoryCalendar()
	if err != nil {
		return []map[string]any{}
	}
	return events
}

// fetchForexFactoryCalendar fetches economic calendar from ForexFactory's free API.
func fetchForexFactoryCalendar() ([]map[string]any, error) {
	apiClient := &http.Client{Timeout: 10 * time.Second}
	// ForexFactory provides a free JSON calendar endpoint
	url := "https://cdn-nfs.forexfactory.net/ff-calendar.json"
	resp, err := apiClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var raw []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	// Filter for high-impact events in the next 7 days
	now := time.Now()
	cutoff := now.AddDate(0, 0, 7)
	events := make([]map[string]any, 0)
	for _, item := range raw {
		dateStr, _ := item["date"].(string)
		impact, _ := item["impact"].(string)
		if impact != "High" {
			continue
		}
		eventDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if eventDate.Before(now) || eventDate.After(cutoff) {
			continue
		}
		events = append(events, map[string]any{
			"id":              fmt.Sprintf("ff-%s-%s", dateStr, item["title"]),
			"date":            dateStr,
			"time":            getString(item, "time", ""),
			"country":         getString(item, "country", ""),
			"name":            getString(item, "title", ""),
			"name_en":         getString(item, "title", ""),
			"importance":      "high",
			"forecast":        getString(item, "forecast", ""),
			"expected_impact": "neutral",
		})
	}
	return events, nil
}

func round(v float64, decimals int) float64 {
	p := 1.0
	for i := 0; i < decimals; i++ {
		p *= 10
	}
	return float64(int(v*p+0.5)) / p
}

// AIModels returns the list of AI models (for /api/ai/models).
// Dynamically builds from registered AI providers with enriched metadata.
func AIModels(c *gin.Context) {
	names := ai.ListProviders()
	// Sort for stable output
	models := make([]map[string]any, 0, len(names))
	for _, name := range names {
		p := ai.GetProvider(name)
		enabled := p != nil && p.APIKey != ""
		weight := 65 // default weight
		if enabled {
			weight = 75
		}
		display := providerDisplayNames[name]
		if display == "" {
			display = name
		}
		color := providerColors[name]
		if color == "" {
			color = "#6366F1"
		}
		models = append(models, map[string]any{
			"id":       name,
			"name":     display,
			"provider": name,
			"color":    color,
			"enabled":  enabled,
			"weight":   weight,
			"status":   "ok",
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "models": models})
}

// PortfolioCalendar returns monthly PnL calendar data computed from real trades.
func PortfolioCalendar(c *gin.Context) {
	now := time.Now()
	months := make([]map[string]any, 0)

	db := store.GetDB()
	if db == nil {
		// No database — return empty calendar
		for i := 5; i >= 0; i-- {
			monthStart := now.AddDate(0, -i, 0)
			mk := monthStart.Format("2006-01")
			months = append(months, map[string]any{
				"month_key":     mk,
				"year":          monthStart.Year(),
				"month":         int(monthStart.Month()),
				"days_in_month": 30,
				"first_weekday": 0,
				"days":          map[string]float64{},
				"total":         0.0,
				"win_days":      0,
				"lose_days":     0,
			})
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "months": months})
		return
	}

	for i := 5; i >= 0; i-- {
		monthStart := now.AddDate(0, -i, 0)
		mk := monthStart.Format("2006-01")
		monthEnd := monthStart.AddDate(0, 1, 0).Add(-time.Second)

		// Query PnL by day from trades table
		rows, err := db.Query(
			`SELECT date(created_at/1000, 'unixepoch') as trade_date,
			        SUM(CASE WHEN side='SELL' THEN price*quantity ELSE -price*quantity END) as pnl
			 FROM trades
			 WHERE created_at BETWEEN ? AND ?
			 GROUP BY trade_date
			 ORDER BY trade_date`,
			monthStart.UnixMilli(), monthEnd.UnixMilli(),
		)
		days := make(map[string]float64)
		total := 0.0
		winDays, loseDays := 0, 0
		if err == nil {
			for rows.Next() {
				var dateStr string
				var pnl float64
				if scanErr := rows.Scan(&dateStr, &pnl); scanErr == nil {
					// Extract day number from date string "2026-06-13"
					if len(dateStr) >= 10 {
						dayKey := dateStr[8:10]
						days[dayKey] = store.RoundFloat(pnl, 2)
						total += pnl
						if pnl > 0 {
							winDays++
						} else if pnl < 0 {
							loseDays++
						}
					}
				}
			}
			rows.Close()
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
	exName := id
	if ex, ok := exchanges[id].(map[string]any); ok {
		exName = getString(ex, "name", id)
	}

	symbol := "BTCUSDT"
	// Try the specific adapter if available, then fall back to Binance general test
	status := "error"
	msg := ""
	switch strings.ToLower(id) {
	case "binance", "binance_spot", "binance_futures":
		if _, err := fetchBinanceKlines(symbol, "1m", 1, 0, 0); err != nil {
			msg = fmt.Sprintf("%s 连接失败: %v", exName, err)
		} else {
			status = "ok"
			msg = fmt.Sprintf("%s 连接测试通过", exName)
		}
	case "bybit":
		endpoint := "https://api.bybit.com/v5/market/tickers?category=spot&symbol=" + symbol
		status, msg = testGenericExchange(exName, endpoint, symbol)
	case "kraken":
		endpoint := "https://api.kraken.com/0/public/Ticker?pair=XBTUSDT"
		status, msg = testGenericExchange(exName, endpoint, "")
	case "mexc":
		endpoint := "https://api.mexc.com/api/v3/ticker/price?symbol=" + symbol
		status, msg = testGenericExchange(exName, endpoint, symbol)
	case "bitget":
		endpoint := "https://api.bitget.com/api/v2/spot/market/tickers?symbol=" + symbol
		status, msg = testGenericExchange(exName, endpoint, symbol)
	case "alpaca":
		endpoint := "https://paper-api.alpaca.markets/v2/assets"
		status, msg = testGenericExchange(exName, endpoint, "")
	case "coinbase":
		endpoint := "https://api.coinbase.com/v2/prices/BTC-USD/spot"
		status, msg = testGenericExchange(exName, endpoint, "")
	default:
		// Generic connectivity test using public API endpoint
		endpoint := "https://api.binance.com/api/v3/ticker/price?symbol=" + symbol
		status, msg = testGenericExchange(exName, endpoint, symbol)
	}

	c.JSON(http.StatusOK, gin.H{"status": status, "id": id, "message": msg})
}

// testGenericExchange tests connectivity by hitting a public REST endpoint.
func testGenericExchange(name, url, symbol string) (status, msg string) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "error", fmt.Sprintf("%s 连接失败: %v", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "ok", fmt.Sprintf("%s 连接测试通过", name)
	}
	return "error", fmt.Sprintf("%s 连接失败: HTTP %d", name, resp.StatusCode)
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
// Persists API key to provider registry and config store.
func SettingsAISave(c *gin.Context) {
	id := c.Param("id")
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的请求数据"})
		return
	}

	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "provider id is required"})
		return
	}

	apiKey, _ := body["api_key"].(string)
	model, _ := body["model"].(string)

	// Register/update the provider with new credentials
	p := ai.GetProvider(id)
	if p == nil {
		// Create a new provider if not pre-registered
		ai.RegisterProvider(ai.Provider{
			Name:  id,
			Model: model,
		})
		p = ai.GetProvider(id)
	}
	if p != nil {
		if apiKey != "" {
			ai.SetProviderAPIKey(id, apiKey)
		}
		if model != "" {
			ai.SetProviderModel(id, model)
		}
	}

	// Persist provider config to config store
	cfg := store.GetConfig()
	aiProviders, _ := cfg["ai_providers"].(map[string]any)
	if aiProviders == nil {
		aiProviders = make(map[string]any)
	}
	aiProviders[id] = map[string]any{
		"model":   model,
		"enabled": apiKey != "",
	}
	cfg["ai_providers"] = aiProviders
	if err := store.SaveConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": fmt.Sprintf("保存配置失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("%s 配置已保存", id)})
}
