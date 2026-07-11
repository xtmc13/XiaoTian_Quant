package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
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

	// GetStrategyConfigs returns a defensive copy, so no extra lock is needed here.
	configs := store.GetStrategyConfigs()
	items := make([]map[string]any, 0, len(configs))
	for _, v := range configs {
		items = append(items, v)
	}

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
	// Normalize field names to match frontend StrategyItem interface
	normalized := make([]map[string]any, 0, len(items))
	for _, it := range items {
		normalized = append(normalized, normalizeStrategyConfig(it))
	}
	c.JSON(http.StatusOK, normalized)
}

func GetStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	item := store.GetStrategyConfig(id)
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
	c.JSON(http.StatusOK, normalizeStrategyConfig(result))
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
		"type":          strategyType,
		"strategy_name": strategyType,
		"coin":          getString(body, "coin", ""),
		"config_json":   configJSON,
		"direction":     getString(body, "direction", "long"),
		"trade_direction": getString(body, "direction", "long"),
		"leverage":      getFloat(body, "leverage", 1.0),
		"status":        "stopped",
		"pnl":           0.0,
		"total_pnl":     0.0,
		"total_pnl_percent": 0.0,
		"current_equity": getFloat(body, "initial_capital", 0),
		"created_at":    float64(nowTS),
		"updated_at":    float64(nowTS),
		// ── Contract fields ──
		"market_type":   getString(body, "market_type", "spot"),
		"margin_mode":   getString(body, "margin_mode", "cross"),
		"symbol":        symbol,
		"timeframe":     getString(body, "timeframe", "15m"),
		"initial_capital": getFloat(body, "initial_capital", 0),
		"execution_mode": getString(body, "execution_mode", "signal"),
		"mode":          getString(body, "execution_mode", "signal"),
		"strategy_mode": getString(body, "execution_mode", "signal"),
		"group_id":      getString(body, "group_id", ""),
		"group_name":    getString(body, "group_name", ""),
		"indicator_name": getString(body, "indicator_name", ""),
	}

	store.SetStrategyConfig(sid, item)
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
	item := store.GetStrategyConfig(id)
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	for _, f := range []string{"name", "coin", "strategy_type", "direction", "leverage", "category", "market_type", "margin_mode", "symbol", "timeframe", "execution_mode", "initial_capital", "group_id", "group_name", "indicator_name"} {
		if v, ok := body[f]; ok {
			item[f] = v
		}
	}
	// Keep alias fields in sync
	if v, ok := body["strategy_type"]; ok {
		item["type"] = v
		item["strategy_name"] = v
	}
	if v, ok := body["direction"]; ok {
		item["trade_direction"] = v
	}
	if v, ok := body["execution_mode"]; ok {
		item["mode"] = v
		item["strategy_mode"] = v
	}
	if config, ok := body["config"].(map[string]any); ok {
		data, _ := json.Marshal(config)
		item["config_json"] = string(data)
	} else if cj, ok := body["config_json"].(string); ok {
		item["config_json"] = cj
	}
	item["updated_at"] = float64(time.Now().UnixMilli())
	store.SetStrategyConfig(id, item)
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func DeleteStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	if !store.DeleteStrategyConfig(id) {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func BatchStartConfigs(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	ids := getStringSlice(body, "ids")
	nowTS := float64(time.Now().UnixMilli())
	for _, sid := range ids {
		item := store.GetStrategyConfig(sid)
		if item == nil {
			continue
		}
		if err := startStrategyInEngine(sid, item); err == nil {
			item["status"] = "running"
			item["updated_at"] = nowTS
			store.SetStrategyConfig(sid, item)
		}
	}
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func BatchStopConfigs(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	ids := getStringSlice(body, "ids")
	nowTS := float64(time.Now().UnixMilli())
	for _, sid := range ids {
		item := store.GetStrategyConfig(sid)
		if item == nil {
			continue
		}
		stopStrategyInEngine(sid)
		item["status"] = "stopped"
		item["updated_at"] = nowTS
		store.SetStrategyConfig(sid, item)
	}
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "stopped": len(ids)})
}

func BatchCloseConfigs(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	ids := getStringSlice(body, "ids")
	nowTS := float64(time.Now().UnixMilli())
	closed := 0
	for _, sid := range ids {
		item := store.GetStrategyConfig(sid)
		if item == nil {
			continue
		}
		stopStrategyInEngine(sid)
		item["status"] = "stopped"
		item["closed_at"] = nowTS
		item["updated_at"] = nowTS
		store.SetStrategyConfig(sid, item)
		closed++
	}
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "closed": closed})
}

func BatchDeleteConfigs(c *gin.Context) {
	var body map[string]any
	c.ShouldBindJSON(&body)
	ids := getStringSlice(body, "ids")
	deleted := 0
	for _, sid := range ids {
		if store.GetStrategyConfig(sid) == nil {
			continue
		}
		stopStrategyInEngine(sid)
		store.DeleteStrategyConfig(sid)
		deleted++
	}
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "deleted": deleted})
}

func StartStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	item := store.GetStrategyConfig(id)
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	if err := startStrategyInEngine(id, item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	item["status"] = "running"
	item["updated_at"] = float64(time.Now().UnixMilli())
	store.SetStrategyConfig(id, item)
	store.PersistStrategyConfigs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func StopStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	item := store.GetStrategyConfig(id)
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
		return
	}
	stopStrategyInEngine(id)
	item["status"] = "stopped"
	item["updated_at"] = float64(time.Now().UnixMilli())
	store.SetStrategyConfig(id, item)
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

	// Map CRA frontend strategy types to unified backend factories.
	factoryName := strategyType
	if mapped, ok := mapCRAFactory(strategyType, item); ok {
		factoryName = mapped
	}

	// Create strategy instance from factory
	s := strategy.StrategyFactory(factoryName)
	if s == nil {
		return fmt.Errorf("unknown strategy type: %s", strategyType)
	}

	// Wrap with config id as unique name
	wrapped := strategy.WrapStrategy(id, s)

	// Build params from config
	params := buildStrategyParams(item)

	// Filter params to only those accepted by the strategy's parameter registry.
	// CRA-style configs carry many frontend fields that indicator strategies do not declare.
	if registry := s.GetParameters(); registry != nil {
		filtered := make(map[string]any)
		for _, p := range registry.All() {
			if v, ok := params[p.Name]; ok {
				filtered[p.Name] = v
			}
		}
		params = filtered
	}

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

// mapCRAFactory maps frontend CRA strategy types to the unified backend factories.
func mapCRAFactory(strategyType string, item map[string]any) (string, bool) {
	spotTypes := map[string]bool{
		"martin_trend":   true,
		"wallstreet":     true,
		"aggressive":     true,
		"conservative":   true,
		"high_frequency": true,
	}
	contractTypes := map[string]bool{
		"trend_long":          true,
		"trend_short":         true,
		"counter_stable":      true,
		"counter_safe":        true,
		"high_frequency":      true,
		"head_tail_arbitrage": true,
	}
	if spotTypes[strategyType] {
		// high_frequency is ambiguous: use category/market_type if available.
		if strategyType == "high_frequency" {
			if isContractCategory(item) {
				return "cra_contract", true
			}
		}
		return "cra_spot", true
	}
	if contractTypes[strategyType] {
		return "cra_contract", true
	}
	return "", false
}

func isContractCategory(item map[string]any) bool {
	if v, ok := item["category"].(string); ok {
		return v == "contract"
	}
	if v, ok := item["market_type"].(string); ok {
		return v == "swap" || v == "futures" || v == "margin"
	}
	return false
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
	store.PersistStrategyLogs()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func GetTemplates(c *gin.Context) {
	category := c.DefaultQuery("category", "spot")
	limit := 200
	fmtScan(c.Query("limit"), &limit)

	userID := getUserID(c)
	items, err := store.GetStrategyTemplateRepo().List(userID, category, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to list templates"})
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, rec := range items {
		result = append(result, rec.ToMap())
	}
	if result == nil {
		result = []map[string]any{}
	}
	c.JSON(http.StatusOK, result)
}

func CreateTemplate(c *gin.Context) {
	var data map[string]any
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}
	userID := getUserID(c)
	name := getString(data, "strategy_name", getString(data, "name", ""))
	if strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "template name required"})
		return
	}
	defaultConfig := "{}"
	if dc, ok := data["default_config"]; ok {
		switch v := dc.(type) {
		case string:
			defaultConfig = v
		default:
			b, _ := json.Marshal(v)
			defaultConfig = string(b)
		}
	}
	rec := &store.StrategyTemplateRecord{
		UserID:            userID,
		Name:              strings.TrimSpace(name),
		Category:          getString(data, "category", "spot"),
		StrategyType:      getString(data, "strategy_type", ""),
		Description:       getString(data, "description", ""),
		DefaultConfigJSON: defaultConfig,
	}
	if err := store.GetStrategyTemplateRepo().Create(rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to create template"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "id": rec.ID})
}

func DeleteTemplate(c *gin.Context) {
	id := c.Param("id")
	userID := getUserID(c)
	deleted, err := store.GetStrategyTemplateRepo().Delete(id, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to delete template"})
		return
	}
	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Template not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// getUserID extracts the authenticated user ID from the Gin context.
func getUserID(c *gin.Context) int64 {
	if v, ok := c.Get("user_id"); ok {
		switch val := v.(type) {
		case int:
			return int64(val)
		case int64:
			return val
		case float64:
			return int64(val)
		}
	}
	return 0
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

	// CRA 策略的前端表单由 CRAParamForm 统一渲染，不需要动态参数定义。
	craTypes := map[string]bool{
		"martin_trend":        true,
		"wallstreet":          true,
		"aggressive":          true,
		"conservative":        true,
		"high_frequency":      true,
		"high_flat":           true,
		"counter_stable":      true,
		"counter_safe":        true,
		"head_tail_arbitrage": true,
		"dual_burn":           true,
		"global_burn":         true,
		"trend_long":          true,
		"trend_short":         true,
	}
	if craTypes[strategyType] {
		c.JSON(http.StatusOK, gin.H{"type": strategyType, "params": []map[string]any{}})
		return
	}

	var defs []map[string]any

	switch strategyType {
	case "breakout", "trend", "custom":
		s := strategies.NewBreakoutStrategy()
		defs = s.ParamDefs()
	case "ema_cross", "ema_follow", "ema_counter", "ema_follow_trend", "ema_counter_trend", "ema_spot":
		s := strategies.NewEMACrossStrategy()
		defs = s.ParamDefs()
	case "macd", "macd_golden", "macd_death", "macd_spot_long":
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
	case "arbitrage", "head_tail_arb":
		s := strategies.NewArbitrageStrategy()
		defs = s.ParamDefs()
	case "market_making":
		s := strategies.NewMarketMakingStrategy()
		defs = s.ParamDefs()
	case "martingale", "dca":
		s := strategies.NewMartingaleStrategy()
		defs = s.ParamDefs()
	// AI Bot marketplace aliases (registered in cmd/server/main.go)
	case "optimus", "mono_optimus", "noah":
		s := strategies.NewGridTradingStrategy()
		defs = s.ParamDefs()
	case "cyberbot", "mono_cyberbot":
		s := strategies.NewRSIStrategy()
		defs = s.ParamDefs()
	case "crypto_future", "ai_alpha_futures", "alt_volatility":
		s := strategies.NewDualThrustStrategy()
		defs = s.ParamDefs()
	case "ai_alpha":
		s := strategies.NewEMACrossStrategy()
		defs = s.ParamDefs()
	case "terminator_volatility":
		s := strategies.NewATRTrailingStopStrategy()
		defs = s.ParamDefs()
	case "trade_holder":
		s := strategies.NewMartingaleStrategy()
		defs = s.ParamDefs()
	case "ml":
		defs = []map[string]any{
			{"name": "model_id", "type": "string", "required": true, "description": "已训练的ML模型ID"},
			{"name": "symbol", "type": "symbol", "required": true, "description": "交易对"},
			{"name": "interval", "type": "interval", "required": true, "description": "K线周期"},
			{"name": "predict_threshold", "type": "float", "default": 0.001, "description": "预测阈值，预测值超过此绝对值时开仓"},
			{"name": "position_size_pct", "type": "float", "default": 0.02, "min": 0.005, "max": 0.5, "description": "单次仓位比例"},
			{"name": "max_holding_bars", "type": "int", "default": 48, "min": 1, "max": 500, "description": "最大持仓K线数"},
			{"name": "stop_loss_pct", "type": "float", "default": 0.05, "min": 0.005, "max": 0.5, "description": "止损百分比"},
			{"name": "take_profit_pct", "type": "float", "default": 0.10, "min": 0.005, "max": 1.0, "description": "止盈百分比"},
			{"name": "direction", "type": "choice", "choices": []string{"long", "short", "both"}, "default": "both", "description": "交易方向"},
			{"name": "use_ensemble", "type": "boolean", "default": false, "description": "是否使用多模型集成预测"},
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"detail": "unknown strategy type: " + strategyType})
		return
	}

	if defs == nil {
		defs = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{"type": strategyType, "params": defs})
}

// normalizeStrategyConfig converts a raw store strategy config map into the
// frontend-expected StrategyItem format. It maps backend field names to frontend
// field names and converts Unix timestamps to ISO-8601 strings.
func normalizeStrategyConfig(it map[string]any) map[string]any {
	result := make(map[string]any)

	// Copy basic fields
	for _, k := range []string{"id", "name", "symbol", "status", "leverage", "timeframe", "initial_capital", "current_equity", "total_pnl", "total_pnl_percent", "group_id", "group_name", "indicator_name", "market_type", "margin_mode", "config", "config_json"} {
		if v, ok := it[k]; ok {
			result[k] = v
		}
	}

	// Field name mappings
	if v, ok := it["strategy_type"].(string); ok && v != "" {
		result["type"] = v
		result["strategy_name"] = v
	} else if v, ok := it["type"].(string); ok {
		result["type"] = v
		result["strategy_name"] = v
	}

	if v, ok := it["execution_mode"].(string); ok && v != "" {
		result["mode"] = v
		result["strategy_mode"] = v
	} else if v, ok := it["mode"].(string); ok {
		result["mode"] = v
		result["strategy_mode"] = v
	}

	if v, ok := it["direction"].(string); ok && v != "" {
		result["trade_direction"] = v
	} else if v, ok := it["trade_direction"].(string); ok {
		result["trade_direction"] = v
	}

	if v, ok := it["category"].(string); ok && v != "" {
		result["market_category"] = v
	}

	if v, ok := it["pnl"].(float64); ok {
		if _, hasTotalPnl := result["total_pnl"]; !hasTotalPnl {
			result["total_pnl"] = v
		}
	}

	// Time conversion: backend stores float64 Unix milliseconds, frontend expects ISO string
	if v, ok := it["created_at"].(float64); ok && v > 0 {
		result["created_at"] = time.UnixMilli(int64(v)).Format(time.RFC3339)
	} else if v, ok := it["created_at"].(string); ok {
		result["created_at"] = v
	}
	if v, ok := it["updated_at"].(float64); ok && v > 0 {
		result["updated_at"] = time.UnixMilli(int64(v)).Format(time.RFC3339)
	} else if v, ok := it["updated_at"].(string); ok {
		result["updated_at"] = v
	}

	return result
}
