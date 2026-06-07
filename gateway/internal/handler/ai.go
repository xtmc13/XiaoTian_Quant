package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/indicator"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

var snapshotClient = &http.Client{Timeout: 10 * time.Second}

func fetchBinance24hrTicker(symbol string) (map[string]any, error) {
	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/24hr?symbol=%s", symbol)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := snapshotClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func formatUSD(v float64) string {
	if v >= 1000000000 {
		return fmt.Sprintf("$%.2fB", v/1000000000)
	}
	if v >= 1000000 {
		return fmt.Sprintf("$%.2fM", v/1000000)
	}
	return fmt.Sprintf("$%.2f", v)
}

func AISnapshot(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	interval := c.DefaultQuery("interval", "1h")

	ticker, err := fetchBinance24hrTicker(symbol)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "error",
			"message": "Market data temporarily unavailable: " + err.Error(),
			"symbol":  symbol,
		})
		return
	}

	priceRaw := 0.0
	change24h := 0.0
	high24h := 0.0
	low24h := 0.0
	volumeRaw := 0.0
	atrRaw := 0.0

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
	if high24h > 0 && low24h > 0 {
		atrRaw = (high24h - low24h) * 0.25
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol":     symbol,
		"interval":   interval,
		"price":      formatUSD(priceRaw),
		"price_raw":  priceRaw,
		"change_24h": change24h,
		"volume_24h": formatUSD(volumeRaw),
		"atr":        formatUSD(atrRaw),
		"atr_raw":    atrRaw,
		"high_24h":   high24h,
		"low_24h":    low24h,
	})
}

func AIKlines(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	interval := c.DefaultQuery("interval", "1h")
	limit := 200
	if l, err := fmt.Sscanf(c.DefaultQuery("limit", "200"), "%d", &limit); l != 1 || err != nil {
		limit = 200
	}
	klines, err := fetchBinanceKlines(symbol, interval, limit, 0, 0)
	lastPrice := "--"
	count := 0
	if err == nil && len(klines) > 0 {
		count = len(klines)
		lastPrice = fmt.Sprintf("$%.1f", klines[len(klines)-1]["close"])
	} else {
		klines = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"symbol":     symbol,
		"interval":   interval,
		"klines":     klines,
		"last_price": lastPrice,
		"count":      count,
	})
}

func AIGenerate(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)

	provider := getActiveAIProvider()
	if provider == nil {
		c.JSON(http.StatusOK, gin.H{"status": "error", "msg": "No AI provider configured. Set an API key in Settings → AI."})
		return
	}

	symbol := getString(body, "symbol", "BTCUSDT")
	desc := getString(body, "description", "trend following strategy")

	prompt := fmt.Sprintf(
		"Generate a complete Python trading strategy for %s. Requirements: %s. "+
			"Return ONLY valid Python code with class Strategy(IStrategy) and methods: "+
			"populate_indicators, populate_entry_trend, populate_exit_trend.",
		symbol, desc,
	)

	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages:    []ai.ChatMessage{{Role: ai.RoleUser, Content: prompt}},
		MaxTokens:   2048,
		Temperature: 0.3,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "error", "msg": "AI error: " + err.Error()})
		return
	}
	if len(resp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{"status": "error", "msg": "Empty AI response"})
		return
	}

	code := resp.Choices[0].Message.Content
	c.JSON(http.StatusOK, gin.H{
		"status":        "ok",
		"strategy_name": "AI_Generated_" + symbol,
		"strategy_code": code,
		"description":   desc,
	})
}

func AIMultiAgent(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)

	provider := getActiveAIProvider()
	if provider == nil {
		c.JSON(http.StatusOK, gin.H{"status": "error", "msg": "No AI provider configured."})
		return
	}

	symbol := getString(body, "symbol", "BTCUSDT")
	roles := []string{"technical analyst", "risk manager", "macro analyst", "sentiment analyst", "bull advocate", "bear advocate"}
	agents := make(gin.H)
	var debateMsgs []ai.ChatMessage
	debateMsgs = append(debateMsgs, ai.ChatMessage{
		Role: ai.RoleSystem, Content: fmt.Sprintf(
			"You are a multi-agent trading council analyzing %s. Each agent gives a concise 1-2 sentence view. Then provide a consensus summary.", symbol),
	})

	for _, role := range roles {
		prompt := fmt.Sprintf("As the %s, what is your view on %s right now?", role, symbol)
		debateMsgs = append(debateMsgs, ai.ChatMessage{Role: ai.RoleUser, Content: prompt})
	}

	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages:    debateMsgs[:min(7, len(debateMsgs))],
		MaxTokens:   1024,
		Temperature: 0.5,
	})
	if err != nil {
		// Fallback to structured mock if AI unavailable
		agents["technical"] = "Analysis unavailable — AI error: " + err.Error()
		agents["debate_summary"] = "AI service unavailable, using fallback analysis"
		c.JSON(http.StatusOK, gin.H{"status": "ok", "strategy_name": "MultiAgent_" + symbol,
			"strategy_code": "# Multi-agent strategy\n", "description": "Multi-agent collaboration",
			"agents": agents, "debate_summary": "Error: " + err.Error()})
		return
	}

	content := resp.Choices[0].Message.Content
	c.JSON(http.StatusOK, gin.H{
		"status":        "ok",
		"strategy_name": "MultiAgent_" + symbol,
		"strategy_code": "# Multi-agent analysis\n# " + content,
		"description":   "Generated by multi-agent collaboration",
		"agents":        gin.H{"analysis": content},
		"debate_summary": content,
	})
}

func min(a, b int) int { if a < b { return a }; return b }

func AIBacktest(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)

	symbol := getString(body, "symbol", "BTCUSDT")
	interval := getString(body, "interval", "1h")
	initialBalance := getFloat(body, "initial_balance", 100000)
	strategyType := getString(body, "strategy_type", "sma_cross")
	numBars := int(getFloat(body, "num_bars", 500))

	// Fetch historical klines from Binance
	klines, err := fetchBinanceKlines(symbol, interval, numBars, 0, 0)
	if err != nil || len(klines) < 50 {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"msg":    "Failed to fetch market data for backtest: " + err.Error(),
		})
		return
	}

	bars := make([]model.Bar, 0, len(klines))
	for _, k := range klines {
		bars = append(bars, model.Bar{
			Symbol:   symbol,
			Open:     getFloat(k, "open", 0),
			High:     getFloat(k, "high", 0),
			Low:      getFloat(k, "low", 0),
			Close:    getFloat(k, "close", 0),
			Volume:   getFloat(k, "volume", 0),
			Interval: interval,
			Time:     int64(getFloat(k, "time", 0)),
		})
	}

	cfg := backtest.DefaultRunnerConfig()
	cfg.InitialBalance = initialBalance
	runner := backtest.NewRunner(cfg)
	runner.LoadBars(symbol, bars)

	var btStrategy backtest.BacktestStrategy
	switch strategyType {
	case "breakout":
		btStrategy = &breakoutBTStrategy{symbol: symbol, lookback: 20, bufferPct: 0.002, stopLossPct: 0.02, takeProfitPct: 0.04}
	case "martin_trend":
		btStrategy = &martinTrendStrategy{symbol: symbol}
	case "macd_golden_long":
		btStrategy = &macdGoldenLongStrategy{symbol: symbol}
	case "macd_death_short":
		btStrategy = &macdDeathShortStrategy{symbol: symbol}
	default:
		btStrategy = &smaCrossStrategy{symbol: symbol, fastPeriod: 12, slowPeriod: 26}
	}

	result, err := runner.Run(btStrategy)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"msg":    "Backtest execution failed: " + err.Error(),
		})
		return
	}

	equityCurve := make([]map[string]any, 0, len(result.EquityCurve))
	for _, pt := range result.EquityCurve {
		equityCurve = append(equityCurve, map[string]any{
			"time":   pt.Timestamp,
			"equity": store.RoundFloat(pt.Equity, 2),
		})
	}

	trades := make([]map[string]any, 0, len(result.Trades))
	for i, t := range result.Trades {
		pnlPct := 0.0
		if t.EntryPrice != 0 {
			pnlPct = (t.ExitPrice - t.EntryPrice) / t.EntryPrice * 100
			if t.Side == "SELL" || t.Side == "sell" {
				pnlPct = -pnlPct
			}
		}
		trades = append(trades, map[string]any{
			"id":           fmt.Sprintf("ai-trade-%d-%d", time.Now().UnixMilli(), i),
			"symbol":       symbol,
			"side":         t.Side,
			"entry_price":  t.EntryPrice,
			"exit_price":   t.ExitPrice,
			"quantity":     t.Quantity,
			"pnl":          store.RoundFloat(t.RealizedPnL, 2),
			"pnl_pct":      store.RoundFloat(pnlPct, 2),
			"entry_time":   t.EntryTime,
			"exit_time":    t.ExitTime,
			"reason":       t.ExitReason,
		})
	}

	finalEquity := initialBalance
	if len(result.EquityCurve) > 0 {
		finalEquity = result.EquityCurve[len(result.EquityCurve)-1].Equity
	}

	c.JSON(http.StatusOK, gin.H{
		"id":               fmt.Sprintf("ai-bt-%d", time.Now().UnixMilli()),
		"strategy_id":      strategyType,
		"symbol":           symbol,
		"start_date":       time.UnixMilli(bars[0].Time).Format("2006-01-02"),
		"end_date":         time.UnixMilli(bars[len(bars)-1].Time).Format("2006-01-02"),
		"initial_balance":  initialBalance,
		"final_equity":     store.RoundFloat(finalEquity, 2),
		"total_return_pct": store.RoundFloat(result.TotalReturnPct, 2),
		"max_drawdown_pct": store.RoundFloat(result.MaxDrawdownPct, 2),
		"sharpe_ratio":     store.RoundFloat(result.SharpeRatio, 2),
		"win_rate":         store.RoundFloat(result.WinRate, 1),
		"profit_factor":    store.RoundFloat(result.ProfitFactor, 2),
		"total_trades":     result.TotalTrades,
		"equity_curve":     equityCurve,
		"trades":           trades,
		"report": gin.H{
			"initial_balance":  initialBalance,
			"final_equity":     store.RoundFloat(finalEquity, 2),
			"total_return_pct": store.RoundFloat(result.TotalReturnPct, 2),
			"max_drawdown_pct": store.RoundFloat(result.MaxDrawdownPct, 2),
			"sharpe_ratio":     store.RoundFloat(result.SharpeRatio, 2),
			"win_rate_pct":     store.RoundFloat(result.WinRate, 1),
			"num_trades":       result.TotalTrades,
			"profit_factor":    store.RoundFloat(result.ProfitFactor, 2),
		},
	})
}

func AIOptimize(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)

	symbol := getString(body, "symbol", "BTCUSDT")
	interval := getString(body, "interval", "1h")
	strategyType := getString(body, "strategy_type", "sma_cross")
	maxEvals := int(getFloat(body, "max_evals", 50))
	if maxEvals <= 0 || maxEvals > 500 {
		maxEvals = 50
	}

	// Fetch historical klines
	klines, err := fetchBinanceKlines(symbol, interval, 500, 0, 0)
	if err != nil || len(klines) < 50 {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"msg":    "Failed to fetch market data for optimization: " + err.Error(),
		})
		return
	}

	bars := make([]model.Bar, 0, len(klines))
	for _, k := range klines {
		bars = append(bars, model.Bar{
			Symbol:   symbol,
			Open:     getFloat(k, "open", 0),
			High:     getFloat(k, "high", 0),
			Low:      getFloat(k, "low", 0),
			Close:    getFloat(k, "close", 0),
			Volume:   getFloat(k, "volume", 0),
			Interval: interval,
			Time:     int64(getFloat(k, "time", 0)),
		})
	}

	// Run hyperopt-style grid search over key parameters
	iterationHistory := make([]map[string]any, 0, maxEvals)
	bestSharpe := -999.0
	bestReturn := -999.0

	for i := 0; i < maxEvals; i++ {
		// Random parameter sampling
		fastPeriod := 5 + i%20
		slowPeriod := fastPeriod + 5 + i%30
		stopLossPct := 0.01 + float64(i%10)*0.005
		takeProfitPct := stopLossPct * 2.0

		cfg := backtest.DefaultRunnerConfig()
		cfg.InitialBalance = 100000
		runner := backtest.NewRunner(cfg)
		runner.LoadBars(symbol, bars)

		var btStrategy backtest.BacktestStrategy
		switch strategyType {
		case "sma_cross":
			btStrategy = &smaCrossStrategy{symbol: symbol, fastPeriod: fastPeriod, slowPeriod: slowPeriod}
		case "breakout":
			btStrategy = &breakoutBTStrategy{symbol: symbol, lookback: 10 + i%30, bufferPct: 0.001 + float64(i%5)*0.001, stopLossPct: stopLossPct, takeProfitPct: takeProfitPct}
		default:
			btStrategy = &smaCrossStrategy{symbol: symbol, fastPeriod: fastPeriod, slowPeriod: slowPeriod}
		}

		result, err := runner.Run(btStrategy)
		if err != nil {
			continue
		}

		iterationHistory = append(iterationHistory, map[string]any{
			"iteration":     i + 1,
			"sharpe":        store.RoundFloat(result.SharpeRatio, 3),
			"return":        store.RoundFloat(result.TotalReturnPct, 2),
			"max_drawdown":  store.RoundFloat(result.MaxDrawdownPct, 2),
			"win_rate":      store.RoundFloat(result.WinRate, 1),
			"total_trades":  result.TotalTrades,
			"params":        map[string]any{"fast_period": fastPeriod, "slow_period": slowPeriod, "stop_loss": stopLossPct, "take_profit": takeProfitPct},
		})

		if result.SharpeRatio > bestSharpe {
			bestSharpe = result.SharpeRatio
			bestReturn = result.TotalReturnPct
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":            "ok",
		"iteration_history": iterationHistory,
		"best_sharpe":       store.RoundFloat(bestSharpe, 3),
		"best_return":       store.RoundFloat(bestReturn, 2),
		"symbol":            symbol,
		"strategy_type":     strategyType,
	})
}

func AIDeploy(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)

	strategyName := getString(body, "strategy_name", "AI_Strategy")
	strategyCode := getString(body, "strategy_code", "")
	strategyType := getString(body, "strategy_type", "script")
	marketCategory := getString(body, "market_category", "crypto")
	executionMode := getString(body, "execution_mode", "signal")

	if strategyCode == "" {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"msg":    "strategy_code is required",
		})
		return
	}

	// Create strategy config via store
	mu := store.GetStrategyConfigMu()
	mu.Lock()
	defer mu.Unlock()

	configs := store.GetStrategyConfigs()
	id := fmt.Sprintf("ai-%d", time.Now().UnixMilli())
	config := map[string]any{
		"id":              id,
		"strategy_name":   strategyName,
		"strategy_type":   strategyType,
		"strategy_code":   strategyCode,
		"market_category": marketCategory,
		"execution_mode":  executionMode,
		"status":          "stopped",
		"created_at":      time.Now().Unix(),
		"updated_at":      time.Now().Unix(),
	}
	configs[id] = config
	store.PersistStrategyConfigs()

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"strategy_id": id,
		"message":     "Strategy deployed successfully. Start it in the Bots page.",
	})
}

func AIValidate(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	code := getString(body, "code", "")

	if code == "" {
		c.JSON(http.StatusOK, gin.H{"status": "error", "msg": "code is required"})
		return
	}

	result := indicator.ValidateCode(code)
	c.JSON(http.StatusOK, gin.H{
		"status": result.Success,
		"valid":  result.Success,
		"error":  result.Msg,
		"hints":  result.Hints,
	})
}

func AIFix(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	code := getString(body, "code", "")
	errorMsg := getString(body, "error", "")

	if code == "" {
		c.JSON(http.StatusOK, gin.H{"status": "error", "msg": "code is required"})
		return
	}

	provider := getActiveAIProvider()
	if provider == nil || provider.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"msg":    "No AI provider configured. Set an API key in Settings → AI.",
		})
		return
	}

	prompt := fmt.Sprintf(
		"Fix the following Python trading strategy code. Error message: %s\n\nCode:\n%s\n\n"+
			"Return ONLY the fixed code, no explanations, no markdown formatting.",
		errorMsg, code,
	)

	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages:    []ai.ChatMessage{{Role: ai.RoleUser, Content: prompt}},
		MaxTokens:   2048,
		Temperature: 0.2,
	})
	if err != nil || len(resp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"msg":    "AI fix failed: " + err.Error(),
		})
		return
	}

	fixedCode := resp.Choices[0].Message.Content
	// Strip markdown code blocks if present
	fixedCode = strings.TrimPrefix(fixedCode, "```python")
	fixedCode = strings.TrimPrefix(fixedCode, "```")
	fixedCode = strings.TrimSuffix(fixedCode, "```")
	fixedCode = strings.TrimSpace(fixedCode)

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"fixed_code": fixedCode,
		"original":   code,
	})
}


// ── Model Registry ──

func AIModelsList(c *gin.Context) {
	names := ai.ListProviders()
	models := make([]map[string]any, 0, len(names))
	for _, name := range names {
		p := ai.GetProvider(name)
		enabled := p != nil && p.APIKey != ""
		models = append(models, map[string]any{
			"id":       name,
			"name":     name,
			"provider": name,
			"category": "llm",
			"enabled":  enabled,
			"weight":   1.0,
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "models": models})
}

// ── Auto-Trade Config ──

func AIAutoTradeGet(c *gin.Context) {
	cfg := store.GetConfig()
	autoTrade, _ := cfg["auto_trade"].(map[string]any)
	if autoTrade == nil {
		autoTrade = map[string]any{
			"enabled":       false,
			"max_positions":   5,
			"risk_per_trade":  0.02,
			"default_strategy": "sma_cross",
			"timeframe":       "1h",
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "config": autoTrade})
}

func AIAutoTradeSave(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)

	cfg := store.GetConfig()
	cfg["auto_trade"] = data
	store.SaveConfig(cfg)

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Auto-trade configuration saved"})
}

// ── Multi-Model Analysis ──

var analysisTasks = make(map[string]map[string]any)

func AIAnalysisStart(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	taskID := "analysis-" + fmt.Sprintf("%d", time.Now().UnixMilli())

	enabledModels := []string{}
	if models, ok := data["enabled_models"].([]any); ok {
		for _, m := range models {
			if s, ok := m.(string); ok {
				enabledModels = append(enabledModels, s)
			}
		}
	}
	if len(enabledModels) == 0 {
		enabledModels = []string{"deepseek"}
	}

	symbol := getString(data, "symbol", "BTCUSDT")
	interval := getString(data, "interval", "1h")

	analysisTasks[taskID] = map[string]any{
		"status":         "processing",
		"symbol":         symbol,
		"interval":       interval,
		"enabled_models": enabledModels,
		"created_at":     time.Now().Unix(),
	}

	// Run real async analysis with configured providers
	go func() {
		results := make([]map[string]any, 0)
		var wg sync.WaitGroup
		resultCh := make(chan map[string]any, len(enabledModels))

		for _, modelName := range enabledModels {
			wg.Add(1)
			go func(m string) {
				defer wg.Done()
				provider := ai.GetProvider(m)
				if provider == nil || provider.APIKey == "" {
					resultCh <- map[string]any{
						"model":      m,
						"signal":     "neutral",
						"confidence": 0.0,
						"reasoning":  fmt.Sprintf("Provider '%s' not configured", m),
						"timestamp":  time.Now().Unix(),
					}
					return
				}
				prompt := fmt.Sprintf(
					"Analyze %s (%s) technically. Respond ONLY in JSON with this exact schema:\n"+
					"{\"signal\":\"bullish|bearish|neutral\",\"confidence\":0-100,\"reasoning\":\"one sentence\"}\n"+
					"No markdown, no extra text, just raw JSON.",
					symbol, interval,
				)
				resp, err := provider.ChatCompletion(ai.CompletionRequest{
					Messages:    []ai.ChatMessage{{Role: ai.RoleUser, Content: prompt}},
					MaxTokens:   256,
					Temperature: 0.3,
				})
				if err != nil || len(resp.Choices) == 0 {
					resultCh <- map[string]any{
						"model":      m,
						"signal":     "neutral",
						"confidence": 0.0,
						"reasoning":  "Analysis failed: " + err.Error(),
						"timestamp":  time.Now().Unix(),
					}
					return
				}
				content := resp.Choices[0].Message.Content
				signal, confidence, reasoning := parseAIJSON(content)
				resultCh <- map[string]any{
					"model":      m,
					"signal":     signal,
					"confidence": confidence,
					"reasoning":  reasoning,
					"raw":        content,
					"timestamp":  time.Now().Unix(),
				}
			}(modelName)
		}

		go func() {
			wg.Wait()
			close(resultCh)
		}()

		for r := range resultCh {
			results = append(results, r)
		}

		// Compute consensus
		bullish, bearish, neutral := 0, 0, 0
		for _, r := range results {
			s := r["signal"].(string)
			switch s {
			case "bullish":
				bullish++
			case "bearish":
				bearish++
			default:
				neutral++
			}
		}
		consensusSignal := "neutral"
		if bullish > bearish && bullish > neutral {
			consensusSignal = "bullish"
		} else if bearish > bullish && bearish > neutral {
			consensusSignal = "bearish"
		}
		agreement := 0.0
		if len(results) > 0 {
			maxVotes := bullish
			if bearish > maxVotes {
				maxVotes = bearish
			}
			if neutral > maxVotes {
				maxVotes = neutral
			}
			agreement = float64(maxVotes) / float64(len(results)) * 100
		}

		analysisTasks[taskID]["status"] = "completed"
		analysisTasks[taskID]["results"] = results
		analysisTasks[taskID]["consensus"] = map[string]any{
			"signal":    consensusSignal,
			"bullish":   bullish,
			"bearish":   bearish,
			"neutral":   neutral,
			"total":     len(results),
			"agreement": agreement,
		}
	}()

	c.JSON(http.StatusOK, gin.H{"status": "ok", "task_id": taskID})
}

func AIAnalysisResult(c *gin.Context) {
	taskID := c.Query("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "task_id required"})
		return
	}
	task, ok := analysisTasks[taskID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "task not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":    task["status"],
		"results":   task["results"],
		"consensus": task["consensus"],
		"symbol":    task["symbol"],
		"interval":  task["interval"],
	})
}

// AIAnalyze runs multi-model analysis for a symbol using configured LLM providers.
func AIAnalyze(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	symbol := getString(body, "symbol", "BTCUSDT")

	provider := getActiveAIProvider()
	if provider == nil || provider.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{
			"status":    "error",
			"symbol":    symbol,
			"message":   "No AI provider configured. Set an API key in Settings → AI.",
		})
		return
	}

	fmt.Printf("[AIAnalyze] provider=%s model=%s symbol=%s\n", provider.Name, provider.Model, symbol)
	start := time.Now()

	prompt := fmt.Sprintf(
		"Analyze %s technically. Respond ONLY in JSON:\n"+
		"{\"sentiment\":\"bullish|bearish|neutral\",\"confidence\":0-100,\"summary\":\"one sentence\"}\n"+
		"No markdown, no extra text.",
		symbol,
	)

	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages: []ai.ChatMessage{
			{Role: ai.RoleSystem, Content: "You are a trading analyst. Respond in strict JSON only."},
			{Role: ai.RoleUser, Content: prompt},
		},
		MaxTokens:   256,
		Temperature: 0.3,
	})
	elapsed := time.Since(start)
	fmt.Printf("[AIAnalyze] ChatCompletion elapsed=%v err=%v\n", elapsed, err)

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":    "error",
			"symbol":    symbol,
			"message":   "AI analysis failed: " + err.Error(),
		})
		return
	}
	if len(resp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"status":    "error",
			"symbol":    symbol,
			"message":   "AI returned empty response.",
		})
		return
	}

	content := resp.Choices[0].Message.Content
	fmt.Printf("[AIAnalyze] content=%s\n", content)
	// Parse structured JSON output from LLM, fallback to keyword matching
	sentiment, confidence, summary := parseAIJSON(content)

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"symbol":     symbol,
		"consensus":  sentiment,
		"confidence": confidence,
		"divergence": 25,
		"analyses": []map[string]any{
			{"model": provider.Name, "name": provider.Name, "sentiment": sentiment, "analysis": summary, "content": content},
		},
		"signals": []map[string]any{
			{"signal": "AI Analysis", "reason": "Generated by " + provider.Name, "sentiment": sentiment, "name": "AI Analysis"},
		},
		"risks": []map[string]any{
			{"desc": "AI analysis is for reference only, not investment advice."},
		},
		"summary": summary,
	})
}

// parseAIJSON extracts signal, confidence, and reasoning from LLM JSON output.
// Falls back to keyword matching if JSON parsing fails.
func parseAIJSON(content string) (signal string, confidence float64, reasoning string) {
	signal = "neutral"
	confidence = 55.0
	reasoning = content

	// Try to extract JSON from markdown code blocks
	jsonStr := content
	if idx := strings.Index(content, "{"); idx != -1 {
		if end := strings.LastIndex(content, "}"); end != -1 && end > idx {
			jsonStr = content[idx : end+1]
		}
	}

	var result struct {
		Signal     string  `json:"signal"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
		Sentiment  string  `json:"sentiment"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err == nil {
		// Use sentiment as fallback for signal
		s := result.Signal
		if s == "" {
			s = result.Sentiment
		}
		switch strings.ToLower(s) {
		case "bullish", "看多", "buy", "long":
			signal = "bullish"
		case "bearish", "看空", "sell", "short":
			signal = "bearish"
		default:
			signal = "neutral"
		}
		if result.Confidence > 0 && result.Confidence <= 100 {
			confidence = result.Confidence
		}
		if result.Reasoning != "" {
			reasoning = result.Reasoning
		}
		return
	}

	// Fallback: keyword matching
	lower := strings.ToLower(content)
	if strings.Contains(lower, "bullish") || strings.Contains(lower, "看多") || strings.Contains(lower, "buy") {
		signal = "bullish"
		confidence = 72.0
	} else if strings.Contains(lower, "bearish") || strings.Contains(lower, "看空") || strings.Contains(lower, "sell") {
		signal = "bearish"
		confidence = 72.0
	}
	return
}

// AIQuickScan runs a fast single-model scan using the configured provider.
func AIQuickScan(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")

	provider := getActiveAIProvider()
	if provider == nil || provider.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{
			"status":    "error",
			"symbol":    symbol,
			"message":   "No AI provider configured. Set an API key in Settings → AI.",
		})
		return
	}

	prompt := fmt.Sprintf(
		"Quick technical scan for %s. Respond ONLY in JSON with this exact schema:\n"+
		"{\"signal\":\"bullish|bearish|neutral\",\"confidence\":0-100,\"reasoning\":\"one key signal\"}\n"+
		"No markdown, no extra text, just raw JSON.",
		symbol,
	)
	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages:    []ai.ChatMessage{{Role: ai.RoleUser, Content: prompt}},
		MaxTokens:   256,
		Temperature: 0.3,
	})
	if err != nil || len(resp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"status":    "error",
			"symbol":    symbol,
			"message":   "AI scan failed: " + err.Error(),
		})
		return
	}

	content := resp.Choices[0].Message.Content
	sentiment, confidence, reason := parseAIJSON(content)

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"symbol":     symbol,
		"consensus":  sentiment,
		"confidence": confidence,
		"signals": []map[string]any{
			{"signal": "AI Quick Scan", "sentiment": sentiment, "name": "AI Quick Scan", "reason": reason},
		},
	})
}

// AIChat handles AI chat messages using the configured provider.
func AIChat(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	message := getString(data, "message", "")
	model := getString(data, "model", "deepseek")

	if message == "" {
		c.JSON(http.StatusOK, gin.H{"status": "error", "message": "Empty message"})
		return
	}

	provider := ai.GetProvider(model)
	if provider == nil || provider.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{
			"status":  "error",
			"message": fmt.Sprintf("Provider '%s' not configured. Set API key in Settings → AI.", model),
		})
		return
	}

	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages:    []ai.ChatMessage{{Role: ai.RoleUser, Content: message}},
		MaxTokens:   2048,
		Temperature: 0.7,
	})
	if err != nil || len(resp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"status":  "error",
			"message": "AI chat failed: " + err.Error(),
		})
		return
	}

	content := resp.Choices[0].Message.Content
	signal, _, _ := parseAIJSON(content)

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"reply":   content,
		"content": content,
		"signal":  signal,
	})
}

// AIModelConfigSave saves AI model configuration.
// Saves to config.json via the config package.
func AIModelConfigSave(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "无效的请求数据"})
		return
	}

	cfg := store.GetConfig()
	aiCfg, _ := cfg["ai"].(map[string]any)
	if aiCfg == nil {
		aiCfg = make(map[string]any)
		cfg["ai"] = aiCfg
	}

	// Persist model config fields
	for _, k := range []string{"provider", "model", "api_key", "base_url", "temperature", "max_tokens"} {
		if v, ok := body[k]; ok {
			aiCfg[k] = v
		}
	}
	store.SaveConfig(cfg)

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "AI model configuration saved"})
}
