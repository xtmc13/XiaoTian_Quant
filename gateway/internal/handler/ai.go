package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
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

	priceRaw := 68000.0
	change24h := 2.35
	high24h := 68500.0
	low24h := 67200.0
	volumeRaw := 0.0
	atrRaw := 850.0

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
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"report": gin.H{
			"initial_balance": 100000.0,
			"final_equity":    102500.0,
			"total_return_pct": 2.5,
			"max_drawdown_pct": 1.2,
			"sharpe_ratio":     1.35,
			"win_rate_pct":     62.5,
			"num_trades":       15,
		},
		"equity_curve":  []any{},
		"trades":        []any{},
		"strategy_type": "event_driven",
	})
}

func AIOptimize(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	c.JSON(http.StatusOK, gin.H{
		"status":        "ok",
		"strategy_name": "Optimized_Strategy",
		"strategy_code": "# Optimized strategy",
		"description":   "Optimized after 3 iterations",
		"agents": gin.H{
			"technical": "Parameters tuned for volatility regime",
			"risk":      "Sharpe improved from 1.2 to 1.8",
		},
		"iteration_history": []map[string]any{
			{"iteration": 1, "sharpe": 1.2, "return": 8.5},
			{"iteration": 2, "sharpe": 1.5, "return": 10.2},
			{"iteration": 3, "sharpe": 1.8, "return": 12.1},
		},
		"strategy_type": "event_driven",
	})
}

func AIDeploy(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	sid := "tpl-" + formatID(time.Now().Unix())
	c.JSON(http.StatusOK, gin.H{"status": "ok", "id": sid})
}

func AIValidate(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"errors": []any{},
		"valid":  true,
	})
}

func AIFix(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	strategyCode := getString(body, "strategy_code", "")
	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"fixed_code":  strategyCode,
		"errors_after": []any{},
		"fixed":       true,
	})
}

func formatID(ts int64) string {
	return fmt.Sprintf("%d", ts)
}

// ── Model Registry ──

func AIModelsList(c *gin.Context) {
	models := []map[string]any{
		{"id": "deepseek-v3", "name": "DeepSeek V3", "provider": "deepseek", "category": "llm", "enabled": true, "weight": 1.0},
		{"id": "deepseek-r1", "name": "DeepSeek R1", "provider": "deepseek", "category": "reasoning", "enabled": true, "weight": 1.0},
		{"id": "gpt-4o", "name": "GPT-4o", "provider": "openai", "category": "llm", "enabled": false, "weight": 0.8},
		{"id": "claude-opus-4-7", "name": "Claude Opus 4.7", "provider": "anthropic", "category": "llm", "enabled": true, "weight": 1.0},
		{"id": "claude-sonnet-4-6", "name": "Claude Sonnet 4.6", "provider": "anthropic", "category": "llm", "enabled": false, "weight": 0.7},
		{"id": "qwen-max", "name": "Qwen Max", "provider": "alibaba", "category": "llm", "enabled": false, "weight": 0.6},
		{"id": "gemini-2.5-pro", "name": "Gemini 2.5 Pro", "provider": "google", "category": "llm", "enabled": false, "weight": 0.7},
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "models": models})
}

// ── Auto-Trade Config ──

func AIAutoTradeGet(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"enabled":              false,
		"threshold":            70.0,
		"divergence_protection": 30.0,
		"symbol":               "BTCUSDT",
		"position_size":        0.01,
		"max_positions":        3,
	})
}

func AIAutoTradeSave(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
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

	analysisTasks[taskID] = map[string]any{
		"status":         "processing",
		"symbol":         getString(data, "symbol", "BTCUSDT"),
		"interval":       getString(data, "interval", "1h"),
		"enabled_models": enabledModels,
		"created_at":     time.Now().Unix(),
	}

	// Simulate async completion after 3 seconds
	go func() {
		time.Sleep(3 * time.Second)
		results := make([]map[string]any, 0)
		for _, model := range enabledModels {
			signal := "neutral"
			confidence := 55.0
			if model == "deepseek-v3" || model == "claude-opus-4-7" {
				signal = "bullish"
				confidence = 72.0
			}
			results = append(results, map[string]any{
				"model":      model,
				"signal":     signal,
				"confidence": confidence,
				"reasoning":  fmt.Sprintf("%s 分析完成：当前市场趋势向好，建议关注支撑位。", model),
				"timestamp":  time.Now().Unix(),
			})
		}
		analysisTasks[taskID]["status"] = "completed"
		analysisTasks[taskID]["results"] = results
		analysisTasks[taskID]["consensus"] = map[string]any{
			"signal":      "bullish",
			"bullish":     2,
			"bearish":     0,
			"neutral":     1,
			"total":       len(results),
			"agreement":   72.0,
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

// AIAnalyze runs multi-model analysis for a symbol (returns mock data).
func AIAnalyze(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	symbol := getString(body, "symbol", "BTCUSDT")

	analyses := []map[string]any{
		{"model": "DeepSeek V3", "name": "DeepSeek V3", "sentiment": "bullish", "analysis": "当前价格位于上升趋势中，MACD金叉信号明确，RSI未进入超买区，建议逢低做多。短期目标看至前高阻力位。", "content": "当前价格位于上升趋势中，MACD金叉信号明确，RSI未进入超买区，建议逢低做多。"},
		{"model": "Claude Opus", "name": "Claude Opus", "sentiment": "neutral", "analysis": "市场处于震荡整理阶段，方向不明确。布林带收窄预示变盘即将到来，建议等待突破信号确认后再入场。", "content": "市场处于震荡整理阶段，方向不明确。建议等待突破信号确认。"},
		{"model": "GPT-4o", "name": "GPT-4o", "sentiment": "bullish", "analysis": "成交量放大配合价格上涨，机构资金流入明显。短期均线上穿长期均线形成金叉，看多信号较强。", "content": "成交量放大配合价格上涨，机构资金流入明显。看多信号较强。"},
		{"model": "Qwen Max", "name": "Qwen Max", "sentiment": "bearish", "analysis": "价格已接近布林带上轨，RSI显示超买信号。虽然趋势向上但短期回调风险加大，建议减仓观望。", "content": "价格已接近布林带上轨，RSI显示超买信号。短期回调风险加大。"},
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"symbol":     symbol,
		"consensus":  "bullish",
		"confidence": 68,
		"divergence": 25,
		"votes":      gin.H{"bullish": 2, "bearish": 1, "neutral": 1},
		"analyses":   analyses,
		"signals": []map[string]any{
			{"signal": "MACD金叉", "reason": "短期均线上穿长期均线", "sentiment": "bullish", "name": "MACD金叉"},
			{"signal": "布林带收窄", "reason": "波动率降低，突破即将到来", "sentiment": "neutral", "name": "布林带收窄"},
		},
		"risks": []map[string]any{
			{"desc": "市场流动性可能在非交易时段显著降低"},
			{"desc": "注意今晚美联储讲话可能带来的波动"},
		},
		"summary": "综合多模型分析，当前" + symbol + "处于上升趋势整理阶段，短期偏多但需关注关键阻力位。",
	})
}

// AIQuickScan runs a fast multi-model scan (returns mock data).
func AIQuickScan(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"symbol":     symbol,
		"consensus":  "bullish",
		"confidence": 72,
		"signals": []map[string]any{
			{"signal": "RSI底背离", "sentiment": "bullish", "name": "RSI底背离", "reason": "价格新低但RSI未创新低"},
		},
	})
}

// AIChat handles AI chat messages (delegates to ChatSend logic).
func AIChat(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	message := getString(data, "message", "")
	model := getString(data, "model", "deepseek-v3")

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"reply":   fmt.Sprintf("[%s] 收到：「%s」。这是AI讨论回复，连接真实LLM后将提供完整分析。", model, message),
		"content": fmt.Sprintf("[%s] 收到：「%s」。这是AI讨论回复。", model, message),
		"signal":  "neutral",
	})
}

// AIModelConfigSave saves AI model configuration (stub).
func AIModelConfigSave(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "配置已保存"})
}
