package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

func GetStrategyConfigs(c *gin.Context) {
	category := c.Query("category")
	status := c.Query("status")
	coin := c.Query("coin")
	stype := c.Query("type")
	limit := 200
	offset := 0
	fmtScan(c.Query("limit"), &limit)
	fmtScan(c.Query("offset"), &offset)

	mu := store.GetStrategyConfigMu()
	mu.RLock()
	items := make([]map[string]any, 0, len(store.GetStrategyConfigs()))
	for _, v := range store.GetStrategyConfigs() {
		items = append(items, v)
	}
	mu.RUnlock()

	if category != "" {
		items = filterMap(items, "category", category)
	}
	if status != "" {
		items = filterMap(items, "status", status)
	}
	if coin != "" {
		items = filterMapContains(items, "coin", coin)
	}
	if stype != "" {
		items = filterMap(items, "strategy_type", stype)
	}

	sort.Slice(items, func(i, j int) bool {
		a := getFloat(items[i], "updated_at", 0)
		b := getFloat(items[j], "updated_at", 0)
		return a > b
	})

	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	items = items[offset:end]

	for _, it := range items {
		if configJSON, ok := it["config_json"].(string); ok {
			var config map[string]any
			if json.Unmarshal([]byte(configJSON), &config) == nil {
				it["config"] = config
			}
		}
	}
	if items == nil {
		items = []map[string]any{}
	}
	c.JSON(http.StatusOK, items)
}

func GetStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	mu := store.GetStrategyConfigMu()
	mu.RLock()
	item := store.GetStrategyConfigs()[id]
	mu.RUnlock()
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	result := copyMap(item)
	if configJSON, ok := item["config_json"].(string); ok {
		var config map[string]any
		if json.Unmarshal([]byte(configJSON), &config) == nil {
			result["config"] = config
		}
	}
	c.JSON(http.StatusOK, result)
}

func CreateStrategyConfig(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}

	// ── Validation ──
	name := getString(body, "name", "")
	if strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "策略名称不能为空"})
		return
	}
	strategyType := getString(body, "strategy_type", "")
	if strategyType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "策略类型不能为空"})
		return
	}
	symbol := getString(body, "symbol", "")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "交易对不能为空"})
		return
	}

	sid := shortUUID()
	nowTS := time.Now().UnixMilli()

	configJSON := "{}"
	if config, ok := body["config"].(map[string]any); ok && len(config) > 0 {
		data, _ := json.Marshal(config)
		configJSON = string(data)
	} else if cj, ok := body["config_json"].(string); ok && cj != "" {
		configJSON = cj
	}

	item := map[string]any{
		"id":            sid,
		"name":          strings.TrimSpace(name),
		"category":      getString(body, "category", "spot"),
		"strategy_type": strategyType,
		"coin":          getString(body, "coin", ""),
		"config_json":   configJSON,
		"direction":     getString(body, "direction", "long"),
		"leverage":      getFloat(body, "leverage", 1.0),
		"status":        "stopped",
		"pnl":           0.0,
		"created_at":    float64(nowTS),
		"updated_at":    float64(nowTS),
		// ── Contract fields ──
		"market_type":   getString(body, "market_type", "spot"),
		"margin_mode":   getString(body, "margin_mode", "cross"),
		"symbol":        symbol,
		"timeframe":     getString(body, "timeframe", "15m"),
		"initial_capital": getFloat(body, "initial_capital", 1000),
		"execution_mode": getString(body, "execution_mode", "signal"),
	}

	mu := store.GetStrategyConfigMu()
	mu.Lock()
	store.GetStrategyConfigs()[sid] = item
	mu.Unlock()
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "id": sid})
}

func UpdateStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}
	mu := store.GetStrategyConfigMu()
	mu.Lock()
	item := store.GetStrategyConfigs()[id]
	if item == nil {
		mu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	for _, f := range []string{"name", "coin", "strategy_type", "direction", "leverage", "category", "market_type", "margin_mode", "symbol", "timeframe", "execution_mode", "initial_capital"} {
		if v, ok := body[f]; ok {
			item[f] = v
		}
	}
	if config, ok := body["config"].(map[string]any); ok {
		data, _ := json.Marshal(config)
		item["config_json"] = string(data)
	} else if cj, ok := body["config_json"].(string); ok {
		item["config_json"] = cj
	}
	item["updated_at"] = float64(time.Now().UnixMilli())
	mu.Unlock()
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func DeleteStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	mu := store.GetStrategyConfigMu()
	mu.Lock()
	if _, ok := store.GetStrategyConfigs()[id]; !ok {
		mu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	delete(store.GetStrategyConfigs(), id)
	mu.Unlock()
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func BatchStartConfigs(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	ids := getStringSlice(body, "ids")
	nowTS := float64(time.Now().UnixMilli())
	mu := store.GetStrategyConfigMu()
	mu.Lock()
	for _, sid := range ids {
		if item, ok := store.GetStrategyConfigs()[sid]; ok {
			if err := startStrategyInEngine(sid, item); err == nil {
				item["status"] = "running"
				item["updated_at"] = nowTS
			}
		}
	}
	mu.Unlock()
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func BatchStopConfigs(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	ids := getStringSlice(body, "ids")
	nowTS := float64(time.Now().UnixMilli())
	mu := store.GetStrategyConfigMu()
	mu.Lock()
	for _, sid := range ids {
		if item, ok := store.GetStrategyConfigs()[sid]; ok {
			stopStrategyInEngine(sid)
			item["status"] = "stopped"
			item["updated_at"] = nowTS
		}
	}
	mu.Unlock()
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func StartStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	mu := store.GetStrategyConfigMu()
	mu.Lock()
	item, ok := store.GetStrategyConfigs()[id]
	if !ok {
		mu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if err := startStrategyInEngine(id, item); err != nil {
		mu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	item["status"] = "running"
	item["updated_at"] = float64(time.Now().UnixMilli())
	mu.Unlock()
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func StopStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	mu := store.GetStrategyConfigMu()
	mu.Lock()
	item, ok := store.GetStrategyConfigs()[id]
	if !ok {
		mu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	stopStrategyInEngine(id)
	item["status"] = "stopped"
	item["updated_at"] = float64(time.Now().UnixMilli())
	mu.Unlock()
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// startStrategyInEngine registers and starts a strategy instance in the engine.
func startStrategyInEngine(id string, item map[string]any) error {
	strategyType, _ := item["strategy_type"].(string)
	if strategyType == "" {
		// Fallback: try bot_type for frontend-created bot strategies
		strategyType, _ = item["bot_type"].(string)
	}
	if strategyType == "" {
		return fmt.Errorf("strategy_type not set")
	}

	eng := strategy.GetEngine(nil)
	if eng == nil {
		return fmt.Errorf("strategy engine not initialized")
	}

	// If already registered with this id, stop and unregister first
	if existing := eng.Get(id); existing != nil {
		_ = eng.Stop(id)
		_ = eng.Unregister(id)
	}

	// Create strategy instance from factory
	s := strategy.StrategyFactory(strategyType)
	if s == nil {
		return fmt.Errorf("unknown strategy type: %s", strategyType)
	}

	// Wrap with config id as unique name
	wrapped := strategy.WrapStrategy(id, s)

	// Build params from config
	params := buildStrategyParams(item)

	// Register and start
	if err := eng.Register(wrapped); err != nil {
		return fmt.Errorf("register strategy: %w", err)
	}
	if err := eng.Start(id, params); err != nil {
		_ = eng.Unregister(id)
		return fmt.Errorf("start strategy: %w", err)
	}
	return nil
}

// stopStrategyInEngine stops and unregisters a strategy from the engine.
func stopStrategyInEngine(id string) {
	eng := strategy.GetEngine(nil)
	if eng == nil {
		return
	}
	_ = eng.Stop(id)
	_ = eng.Unregister(id)
}

// buildStrategyParams extracts strategy parameters from a config item.
func buildStrategyParams(item map[string]any) map[string]any {
	params := make(map[string]any)

	// Copy basic fields
	for _, key := range []string{"symbol", "coin", "direction", "leverage", "category", "market_type", "margin_mode", "timeframe", "execution_mode", "initial_capital"} {
		if v, ok := item[key]; ok {
			params[key] = v
		}
	}

	// Parse config_json
	if cj, ok := item["config_json"].(string); ok && cj != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(cj), &parsed) == nil {
			for k, v := range parsed {
				params[k] = v
			}
		}
	}

	// Ensure symbol is set
	if params["symbol"] == nil || params["symbol"] == "" {
		if coin, ok := params["coin"].(string); ok && coin != "" {
			params["symbol"] = coin + "USDT"
		} else {
			params["symbol"] = "BTCUSDT"
		}
	}

	// ── Contract fields from config_json ──
	if params["market_type"] == nil || params["market_type"] == "" {
		params["market_type"] = "spot"
	}
	if params["margin_mode"] == nil || params["margin_mode"] == "" {
		params["margin_mode"] = "cross"
	}
	// Extract position_side from direction
	if direction, ok := params["direction"].(string); ok {
		switch direction {
		case "long":
			params["position_side"] = "LONG"
		case "short":
			params["position_side"] = "SHORT"
		case "dual", "both":
			params["position_side"] = "BOTH"
		}
	}

	return params
}

func GetStrategyLogs(c *gin.Context) {
	sid := c.Query("strategy_id")
	limit := 200
	fmtScan(c.Query("limit"), &limit)
	logs := *store.GetLogsStore()
	if sid != "" {
		logs = filterMap(logs, "strategy_id", sid)
	}
	if len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}
	if logs == nil {
		logs = []map[string]any{}
	}
	c.JSON(http.StatusOK, logs)
}

func ClearStrategyLogs(c *gin.Context) {
	sid := c.Query("strategy_id")
	logs := store.GetLogsStore()
	if sid != "" {
		var filtered []map[string]any
		for _, l := range *logs {
			if getString(l, "strategy_id", "") != sid {
				filtered = append(filtered, l)
			}
		}
		*logs = filtered
	} else {
		*logs = nil
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func GetTemplates(c *gin.Context) {
	category := c.DefaultQuery("category", "spot")
	limit := 200
	fmtScan(c.Query("limit"), &limit)
	templates := *store.GetTemplatesStore()
	var filtered []map[string]any
	for _, t := range templates {
		if getString(t, "category", "spot") == category {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	if filtered == nil {
		filtered = []map[string]any{}
	}
	c.JSON(http.StatusOK, filtered)
}

func CreateTemplate(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	tpl := map[string]any{
		"id":            "tpl-" + strconv.FormatInt(time.Now().UnixMilli(), 10),
		"name":          getString(data, "strategy_name", getString(data, "name", "Untitled")),
		"strategy_code": getString(data, "strategy_code", ""),
		"description":   getString(data, "description", ""),
		"category":      getString(data, "category", "spot"),
		"symbol":        getString(data, "symbol", "BTCUSDT"),
		"risk_level":    getString(data, "risk", "medium"),
		"created_at":    float64(time.Now().Unix()),
	}
	*store.GetTemplatesStore() = append(*store.GetTemplatesStore(), tpl)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "id": tpl["id"]})
}

func DeleteTemplate(c *gin.Context) {
	id := c.Param("id")
	templates := store.GetTemplatesStore()
	for i, t := range *templates {
		if t["id"] == id {
			*templates = append((*templates)[:i], (*templates)[i+1:]...)
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"detail": "Template not found"})
}

// ── Helpers ──

func filterMap(items []map[string]any, key, val string) []map[string]any {
	var result []map[string]any
	for _, item := range items {
		if v, ok := item[key].(string); ok && v == val {
			result = append(result, item)
		}
	}
	return result
}

func filterMapContains(items []map[string]any, key, val string) []map[string]any {
	var result []map[string]any
	for _, item := range items {
		if v, ok := item[key].(string); ok && strings.Contains(strings.ToLower(v), strings.ToLower(val)) {
			result = append(result, item)
		}
	}
	return result
}

func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func getStringSlice(m map[string]any, key string) []string {
	if arr, ok := m[key].([]any); ok {
		var result []string
		for _, v := range arr {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

func shortUUID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// GetStrategyParamDefs returns parameter definitions for a strategy type.
// Used by the frontend to render dynamic configuration forms.
func GetStrategyParamDefs(c *gin.Context) {
	strategyType := c.Query("type")
	if strategyType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "type query param required"})
		return
	}

	var defs []map[string]any

	switch strategyType {
	case "breakout", "trend", "custom":
		s := strategies.NewBreakoutStrategy()
		defs = s.ParamDefs()
	case "ema_cross", "ema_follow", "ema_counter":
		s := strategies.NewEMACrossStrategy()
		defs = s.ParamDefs()
	case "macd", "macd_golden", "macd_death":
		s := strategies.NewMACDStrategy()
		defs = s.ParamDefs()
	case "rsi":
		s := strategies.NewRSIStrategy()
		defs = s.ParamDefs()
	case "bollinger_bands":
		s := strategies.NewBollingerBandsStrategy()
		defs = s.ParamDefs()
	case "atr_trailing_stop":
		s := strategies.NewATRTrailingStopStrategy()
		defs = s.ParamDefs()
	case "dual_thrust":
		s := strategies.NewDualThrustStrategy()
		defs = s.ParamDefs()
	case "renko":
		s := strategies.NewRenkoStrategy()
		defs = s.ParamDefs()
	case "grid_trading", "grid":
		s := strategies.NewGridTradingStrategy()
		defs = s.ParamDefs()
	case "arbitrage":
		s := strategies.NewArbitrageStrategy()
		defs = s.ParamDefs()
	case "market_making":
		s := strategies.NewMarketMakingStrategy()
		defs = s.ParamDefs()
	case "martingale", "dca", "martin_trend", "dual_burn":
		s := strategies.NewMartingaleStrategy()
		defs = s.ParamDefs()
	case "wallstreet":
		s := strategies.NewWallstreetStrategy()
		defs = s.ParamDefs()
	case "ml":
		// ML strategy requires modelID, return empty for now
		defs = []map[string]any{}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"detail": "unknown strategy type: " + strategyType})
		return
	}

	if defs == nil {
		defs = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{"type": strategyType, "parameters": defs})
}
