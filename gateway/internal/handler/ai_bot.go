package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ── Helpers ──

func aiBotUserID(c *gin.Context) int {
	uid, exists := c.Get(middleware.UserIDKey)
	if !exists {
		return 0
	}
	if v, ok := uid.(int); ok {
		return v
	}
	return 0
}

func aiBotError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"detail": message})
}

// ── Catalog ──

func AIBotCatalogList(c *gin.Context) {
	items := store.GetAIBotCatalog()
	if items == nil {
		items = []map[string]any{}
	}
	c.JSON(http.StatusOK, items)
}

func AIBotCatalogGet(c *gin.Context) {
	id := c.Param("id")
	item := store.GetAIBotCatalogByID(id)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}
	c.JSON(http.StatusOK, item)
}

// ── Instances ──

func AIBotInstanceList(c *gin.Context) {
	userID := aiBotUserID(c)
	if userID == 0 {
		aiBotError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	items := store.GetAIBotInstances(userID)
	if items == nil {
		items = []map[string]any{}
	}
	c.JSON(http.StatusOK, items)
}

func AIBotInstanceCreate(c *gin.Context) {
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

	now := time.Now().Unix()
	id := "aibot-" + shortUUID()
	catalogID := getString(body, "catalog_id", "")
	name := getString(body, "name", "AI Bot")
	strategyType := getString(body, "strategy_type", "ai_alpha")
	symbol := getString(body, "symbol", "BTCUSDT")
	marketType := getString(body, "market_type", "spot")
	executionMode := getString(body, "execution_mode", "paper")

	// Validate execution mode
	if executionMode != "paper" && executionMode != "live" && executionMode != "signal" {
		executionMode = "paper"
	}

	// If created from catalog, inherit catalog defaults
	if catalogID != "" {
		catalog := store.GetAIBotCatalogByID(catalogID)
		if catalog != nil {
			if name == "AI Bot" || name == "" {
				name = getString(catalog, "name", name)
			}
			if strategyType == "ai_alpha" {
				strategyType = getString(catalog, "strategy_type", strategyType)
			}
			if marketType == "spot" {
				marketType = getString(catalog, "market_type", marketType)
			}
		}
	}

	// Parse config_json to extract initial_balance and merge defaults.
	configJSON := getString(body, "config_json", "{}")
	var configMap map[string]any
	if err := json.Unmarshal([]byte(configJSON), &configMap); err != nil || configMap == nil {
		configMap = map[string]any{}
	}
	initialBalance := getFloat(configMap, "initial_balance", 10000)
	if initialBalance <= 0 {
		initialBalance = 10000
	}

	item := map[string]any{
		"id":              id,
		"user_id":         userID,
		"catalog_id":      catalogID,
		"name":            name,
		"strategy_type":   strategyType,
		"symbol":          symbol,
		"market_type":     marketType,
		"status":          "stopped",
		"execution_mode":  executionMode,
		"config_json":     configJSON,
		"exchange_id":     getString(body, "exchange_id", ""),
		"initial_balance": initialBalance,
		"created_at":      now,
		"updated_at":      now,
		"started_at":      0,
		"stopped_at":      0,
	}
	store.SaveAIBotInstance(item)

	// Auto-create subscription record if fee applies (catalog bot)
	if catalogID != "" {
		catalog := store.GetAIBotCatalogByID(catalogID)
		if catalog != nil {
			feeModel := getString(catalog, "fee_model", "free")
			if feeModel != "free" {
				store.CreateAIBotSubscription(userID, map[string]any{
					"bot_instance_id": id,
					"fee_type":        feeModel,
					"fee_percent":     getFloat(catalog, "fee_percent", 0),
					"monthly_fee":     getFloat(catalog, "monthly_fee", 0),
				})
			}
		}
	}

	c.JSON(http.StatusOK, item)
}

func AIBotInstanceGet(c *gin.Context) {
	userID := aiBotUserID(c)
	id := c.Param("id")
	item := store.GetAIBotInstanceByID(id, userID)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}
	c.JSON(http.StatusOK, item)
}

func AIBotInstanceUpdate(c *gin.Context) {
	userID := aiBotUserID(c)
	id := c.Param("id")
	item := store.GetAIBotInstanceByID(id, userID)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}

	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		aiBotError(c, http.StatusBadRequest, "invalid json")
		return
	}

	// Prevent updating while running
	if getString(item, "status", "stopped") == "running" {
		aiBotError(c, http.StatusConflict, "请先停止机器人再修改配置")
		return
	}

	for _, f := range []string{"name", "symbol", "market_type", "execution_mode", "config_json", "exchange_id"} {
		if v, ok := body[f]; ok {
			item[f] = v
		}
	}
	item["updated_at"] = time.Now().Unix()
	store.SaveAIBotInstance(item)
	c.JSON(http.StatusOK, item)
}

func AIBotInstanceDelete(c *gin.Context) {
	userID := aiBotUserID(c)
	id := c.Param("id")
	item := store.GetAIBotInstanceByID(id, userID)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}
	if getString(item, "status", "stopped") == "running" {
		aiBotError(c, http.StatusConflict, "请先停止机器人再删除")
		return
	}
	store.DeleteAIBotInstance(id, userID)
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func AIBotInstanceStart(c *gin.Context) {
	userID := aiBotUserID(c)
	id := c.Param("id")
	item := store.GetAIBotInstanceByID(id, userID)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}

	// Build strategy config from AI bot instance and register to engine
	strategyItem := aiBotToStrategyConfig(item)
	if err := startAIBotInEngine(id, strategyItem); err != nil {
		aiBotError(c, http.StatusInternalServerError, fmt.Sprintf("启动策略引擎失败: %v", err))
		return
	}

	now := time.Now().Unix()
	item["status"] = "running"
	item["started_at"] = now
	item["updated_at"] = now
	item["error_message"] = ""
	store.SaveAIBotInstance(item)

	// Record initial snapshot
	initialBalance := getFloat(item, "initial_balance", 10000)
	store.SaveAIBotSnapshot(id, initialBalance, 0, 0, 0)

	c.JSON(http.StatusOK, item)
}

func AIBotInstancePause(c *gin.Context) {
	userID := aiBotUserID(c)
	id := c.Param("id")
	item := store.GetAIBotInstanceByID(id, userID)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}
	if getString(item, "status", "stopped") != "running" {
		aiBotError(c, http.StatusConflict, "只能暂停运行中的机器人")
		return
	}
	item["status"] = "paused"
	item["updated_at"] = time.Now().Unix()
	store.SaveAIBotInstance(item)
	c.JSON(http.StatusOK, item)
}

func AIBotInstanceResume(c *gin.Context) {
	userID := aiBotUserID(c)
	id := c.Param("id")
	item := store.GetAIBotInstanceByID(id, userID)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}
	if getString(item, "status", "stopped") != "paused" {
		aiBotError(c, http.StatusConflict, "只能恢复暂停的机器人")
		return
	}
	item["status"] = "running"
	item["updated_at"] = time.Now().Unix()
	store.SaveAIBotInstance(item)
	c.JSON(http.StatusOK, item)
}

func AIBotInstanceStop(c *gin.Context) {
	userID := aiBotUserID(c)
	id := c.Param("id")
	item := store.GetAIBotInstanceByID(id, userID)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}

	stopStrategyInEngine(id)
	resetPaperState(id)

	now := time.Now().Unix()
	item["status"] = "stopped"
	item["stopped_at"] = now
	item["updated_at"] = now
	store.SaveAIBotInstance(item)

	c.JSON(http.StatusOK, item)
}

func AIBotInstanceClone(c *gin.Context) {
	userID := aiBotUserID(c)
	id := c.Param("id")
	item := store.GetAIBotInstanceByID(id, userID)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}

	newItem := copyMap(item)
	newItem["id"] = "aibot-" + shortUUID()
	newItem["name"] = getString(item, "name", "AI Bot") + " (克隆)"
	newItem["status"] = "stopped"
	newItem["created_at"] = time.Now().Unix()
	newItem["updated_at"] = time.Now().Unix()
	newItem["started_at"] = 0
	newItem["stopped_at"] = 0
	newItem["unrealized_pnl"] = 0
	newItem["realized_pnl"] = 0
	newItem["total_return_pct"] = 0
	newItem["max_drawdown_pct"] = 0
	newItem["error_message"] = ""
	store.SaveAIBotInstance(newItem)

	c.JSON(http.StatusOK, newItem)
}

// ── Analytics ──

func AIBotInstanceAnalytics(c *gin.Context) {
	userID := aiBotUserID(c)
	id := c.Param("id")
	item := store.GetAIBotInstanceByID(id, userID)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}
	snapshots := store.GetAIBotSnapshots(id, 90)
	if snapshots == nil {
		snapshots = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{
		"bot":       item,
		"snapshots": snapshots,
	})
}

func AIBotInstanceTrades(c *gin.Context) {
	userID := aiBotUserID(c)
	id := c.Param("id")
	item := store.GetAIBotInstanceByID(id, userID)
	if item == nil {
		aiBotError(c, http.StatusNotFound, "bot not found")
		return
	}
	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	trades := store.GetAIBotTrades(id, limit)
	if trades == nil {
		trades = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{
		"bot":    item,
		"trades": trades,
	})
}

// ── Subscriptions ──

func AIBotSubscriptionList(c *gin.Context) {
	userID := aiBotUserID(c)
	if userID == 0 {
		aiBotError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	items := store.GetAIBotSubscriptions(userID)
	if items == nil {
		items = []map[string]any{}
	}
	c.JSON(http.StatusOK, items)
}

func AIBotSubscriptionCreate(c *gin.Context) {
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
	id := store.CreateAIBotSubscription(userID, body)
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func AIBotSubscriptionCancel(c *gin.Context) {
	userID := aiBotUserID(c)
	if userID == 0 {
		aiBotError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		aiBotError(c, http.StatusBadRequest, "invalid subscription id")
		return
	}
	store.CancelAIBotSubscription(id, userID)
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// ── Internal helpers ──

// startAIBotInEngine registers and starts a strategy instance in the engine,
// filtering parameters to only those registered by the strategy.
func startAIBotInEngine(id string, item map[string]any) error {
	strategyType := getString(item, "strategy_type", "")
	if strategyType == "" {
		strategyType, _ = item["bot_type"].(string)
	}
	if strategyType == "" {
		return fmt.Errorf("strategy_type not set")
	}

	eng := strategy.GetEngine(nil)
	if eng == nil {
		return fmt.Errorf("strategy engine not initialized")
	}

	if existing := eng.Get(id); existing != nil {
		_ = eng.Stop(id)
		_ = eng.Unregister(id)
	}

	s := strategy.StrategyFactory(strategyType)
	if s == nil {
		return fmt.Errorf("unknown strategy type: %s", strategyType)
	}

	// Filter params to registered parameter names to avoid "parameter not found" errors.
	// Note: symbol/timeframe are intentionally excluded because most built-in strategies
	// do not register them in their ParamRegistry; they run on their default symbol.
	allowed := map[string]bool{}
	if defs := s.ParamDefs(); defs != nil {
		for _, def := range defs {
			if name, ok := def["name"].(string); ok {
				allowed[name] = true
			}
		}
	}

	filtered := make(map[string]any)
	for k, v := range item {
		if allowed[k] {
			filtered[k] = v
		}
	}

	wrapped := strategy.WrapStrategy(id, s)
	if err := eng.Register(wrapped); err != nil {
		return fmt.Errorf("register strategy: %w", err)
	}
	if err := eng.Start(id, filtered); err != nil {
		_ = eng.Unregister(id)
		return fmt.Errorf("start strategy: %w", err)
	}
	return nil
}

// aiBotToStrategyConfig converts an AI bot instance map to a strategy config map.
func aiBotToStrategyConfig(item map[string]any) map[string]any {
	cfg := make(map[string]any)
	cfg["strategy_type"] = getString(item, "strategy_type", "ai_alpha")
	cfg["symbol"] = getString(item, "symbol", "BTCUSDT")
	cfg["market_type"] = getString(item, "market_type", "spot")

	// Parse config_json into top-level params
	if cj, ok := item["config_json"].(string); ok && cj != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(cj), &parsed) == nil {
			for k, v := range parsed {
				cfg[k] = v
			}
		}
	}

	// Ensure required defaults
	if cfg["symbol"] == nil || cfg["symbol"] == "" {
		cfg["symbol"] = "BTCUSDT"
	}
	if cfg["timeframe"] == nil || cfg["timeframe"] == "" {
		cfg["timeframe"] = "1h"
	}

	return cfg
}
