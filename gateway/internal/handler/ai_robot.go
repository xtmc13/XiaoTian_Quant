package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Types ──

type aiStatusResp struct {
	SignalsToday        int     `json:"signals_today"`
	AvgConfidence       float64 `json:"avg_confidence"`
	FilterRate          float64 `json:"filter_rate"`
	WinRate             float64 `json:"win_rate"`
	Model               string  `json:"model"`
	ScanInterval        int     `json:"scan_interval"`
	MarketFilter        bool    `json:"market_filter"`
	ConfidenceThreshold float64 `json:"confidence_threshold"`
	Enabled             bool    `json:"enabled"`
	UpdatedAt           int64   `json:"updated_at"`
}

type aiSignal struct {
	ID              string   `json:"id"`
	Symbol          string   `json:"symbol"`
	Signal          string   `json:"signal"` // long/short/neutral
	Side            string   `json:"side"`   // buy/sell/neutral（前端兼容字段）
	Confidence      float64  `json:"confidence"`
	Reason          string   `json:"reason"`
	Filters         []string `json:"filters"`
	MarketCondition string   `json:"market_condition"`
	Mode            string   `json:"mode"`
	Provider        string   `json:"provider"`
	Model           string   `json:"model"`
	Timestamp       int64    `json:"timestamp"` // 毫秒（前端 new Date 直接解析）
	CreatedAt       int64    `json:"created_at"`
}

// ── AI 机器人配置（每用户一行，存 xt_ai_robot_configs）────────────────

// defaultAIRobotConfig 新用户默认配置。
func defaultAIRobotConfig(userID int64) map[string]any {
	return map[string]any{
		"provider":              "deepseek",
		"model":                 "deepseek-chat",
		"scan_interval_seconds": 300,
		"confidence_threshold":  60.0,
		"market_filter":         true,
		"market_filters": map[string]any{
			"min_volume_24h":          1_000_000.0,
			"max_volatility":          10.0,
			"trend_timeframe":         "1h",
			"require_trend_alignment": false,
			"filter_whitelist_only":   false,
		},
		"enabled":   false,
		"watchlist": []string{"BTCUSDT", "ETHUSDT"},
		"symbols":   []string{"BTCUSDT", "ETHUSDT"},
		"mode":      "fast",
		"user_id":   userID,
	}
}

// getAIRobotConfig 读用户配置；未保存过返回默认值。
func getAIRobotConfig(userID int64) map[string]any {
	cfg, err := store.DefaultAIRobotConfigRepo().Get(userID)
	if err != nil || cfg == nil {
		return defaultAIRobotConfig(userID)
	}
	return cfg
}

// AIRobotConfigGet godoc
// GET /ai-robot/config
// 响应：配置字段顶层平铺（前端 AIRobotPanel 直读 body.model 等），同时嵌套 "config"。
func AIRobotConfigGet(c *gin.Context) {
	userID := aiBotUserID(c)
	if userID == 0 {
		aiBotError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	cfg := getAIRobotConfig(int64(userID))
	resp := gin.H{"success": true}
	for k, v := range cfg {
		resp[k] = v
	}
	resp["config"] = cfg
	c.JSON(http.StatusOK, resp)
}

// AIRobotConfigSave godoc
// POST /ai-robot/config
func AIRobotConfigSave(c *gin.Context) {
	userID := aiBotUserID(c)
	if userID == 0 {
		aiBotError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		aiBotError(c, http.StatusBadRequest, "invalid json")
		return
	}

	// 与已有配置合并（前端可能只提交部分字段），同时兼容 watchlist/symbols、
	// scan_interval/scan_interval_seconds 两组拼写。
	merged := getAIRobotConfig(int64(userID))
	for k, v := range body {
		if k == "id" || k == "user_id" || k == "created_at" || k == "updated_at" {
			continue
		}
		merged[k] = v
	}
	merged["user_id"] = int64(userID)
	merged["updated_at"] = time.Now().Unix()
	if v, ok := merged["scan_interval_seconds"]; ok {
		merged["scan_interval"] = v
	} else if v, ok := merged["scan_interval"]; ok {
		merged["scan_interval_seconds"] = v
	}
	wl := stringSliceFromAnyLocal(merged["watchlist"])
	if len(wl) == 0 {
		wl = stringSliceFromAnyLocal(merged["symbols"])
	}
	if len(wl) > 0 {
		merged["watchlist"] = wl
		merged["symbols"] = wl
	}

	if err := store.DefaultAIRobotConfigRepo().Save(int64(userID), merged); err != nil {
		aiBotError(c, http.StatusInternalServerError, "save config failed: "+err.Error())
		return
	}
	resp := gin.H{"success": true}
	for k, v := range merged {
		resp[k] = v
	}
	resp["config"] = merged
	c.JSON(http.StatusOK, resp)
}

// stringSliceFromAnyLocal 把 any 规整成 []string（string/[]string/[]any 均可）。
func stringSliceFromAnyLocal(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if val == "" {
			return nil
		}
		return []string{val}
	}
	return nil
}

// AIRobotModels godoc
// GET /ai-robot/models
// 前端 AIRobotPanel 期望 {"models": ["deepseek",...]}（字符串数组）；
// 另附 "details" 承载完整模型元数据（与 GET /config/ai-models 同源）。
func AIRobotModels(c *gin.Context) {
	names := ai.ListProviders()
	// 稳定排序，避免前端 select 选项跳动。
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	details := make([]map[string]any, 0, len(names))
	for _, name := range names {
		p := ai.GetProvider(name)
		enabled := p != nil && p.APIKey != ""
		weight := 65
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
		details = append(details, map[string]any{
			"id":       name,
			"name":     display,
			"provider": name,
			"color":    color,
			"enabled":  enabled,
			"weight":   weight,
			"status":   "ok",
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "models": names, "details": details})
}

// ── 状态与信号（读真实配置 + xt_ai_signals 统计）────────────────────

// AIRobotStatus returns real-time AI robot statistics computed from ai_signals and trades.
func AIRobotStatus(c *gin.Context) {
	userID := aiBotUserID(c)

	cfg := getAIRobotConfig(int64(userID))
	confidenceThreshold := getFloat(cfg, "confidence_threshold", 60)
	model := getString(cfg, "model", "")
	if model == "" {
		model = getString(cfg, "provider", "deepseek")
	}
	scanInterval := getInt(cfg, "scan_interval_seconds", getInt(cfg, "scan_interval", 300))
	marketFilter, _ := cfg["market_filter"].(bool)
	enabled, _ := cfg["enabled"].(bool)

	// 今日信号统计（来自 xt_ai_signals 真实落库信号）。
	signalsToday := 0
	avgConfidence := 0.0
	filterRate := 0.0
	repo := store.DefaultAISignalRepo()
	dayStart := time.Now().Truncate(24 * time.Hour).Unix()
	recs, err := repo.ListByUser(int64(userID), "", dayStart, 500)
	if err == nil && len(recs) > 0 {
		signalsToday = len(recs)
		filtered := 0
		var sum float64
		for _, r := range recs {
			sum += r.Confidence
			if r.Signal == "neutral" || len(r.Filters) > 0 {
				filtered++
			}
		}
		avgConfidence = sum / float64(len(recs))
		filterRate = float64(filtered) / float64(len(recs)) * 100
	}

	// Compute win rate from AI bot trades over the last 30 days.
	winRate := 0.0
	if userID > 0 {
		instances := store.GetAIBotInstances(userID)
		var totalTrades, winTrades int
		since := time.Now().AddDate(0, 0, -30).Unix()
		for _, inst := range instances {
			id := getString(inst, "id", "")
			if id == "" {
				continue
			}
			trades := store.GetAIBotTrades(id, 1000)
			for _, t := range trades {
				closedAtVal := t["closed_at"]
				var closedAt int64
				switch v := closedAtVal.(type) {
				case int64:
					closedAt = v
				case int:
					closedAt = int64(v)
				case float64:
					closedAt = int64(v)
				}
				if closedAt > 0 && closedAt >= since {
					totalTrades++
					if getFloat(t, "pnl", 0) > 0 {
						winTrades++
					}
				}
			}
		}
		if totalTrades > 0 {
			winRate = float64(winTrades) / float64(totalTrades) * 100
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": aiStatusResp{
			SignalsToday:        signalsToday,
			AvgConfidence:       avgConfidence,
			FilterRate:          filterRate,
			WinRate:             winRate,
			Model:               model,
			ScanInterval:        scanInterval,
			MarketFilter:        marketFilter,
			ConfidenceThreshold: confidenceThreshold,
			Enabled:             enabled,
			UpdatedAt:           time.Now().Unix(),
		},
	})
}

// AISignals godoc
// GET /ai/signals?symbol=&limit=
// 读取 xt_ai_signals 表（AI 决策引擎落库的真实信号）。
func AISignals(c *gin.Context) {
	userID := aiBotUserID(c)
	symbol := c.Query("symbol")
	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	recs, err := store.DefaultAISignalRepo().ListByUser(int64(userID), symbol, 0, limit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "signals": []aiSignal{}})
		return
	}
	signals := make([]aiSignal, 0, len(recs))
	for _, r := range recs {
		side := "neutral"
		switch r.Signal {
		case "long":
			side = "buy"
		case "short":
			side = "sell"
		}
		signals = append(signals, aiSignal{
			ID:              r.ID,
			Symbol:          r.Symbol,
			Signal:          r.Signal,
			Side:            side,
			Confidence:      r.Confidence,
			Reason:          r.Reason,
			Filters:         r.Filters,
			MarketCondition: r.MarketCondition,
			Mode:            r.Mode,
			Provider:        r.Provider,
			Model:           r.Provider, // 前端类型字段兼容
			// 前端 new Date() 按毫秒解析，DB 存秒 → 输出转毫秒。
			Timestamp: r.CreatedAt * 1000,
			CreatedAt: r.CreatedAt * 1000,
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "signals": signals})
}
