package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
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
		"name":          getString(body, "name", sid),
		"category":      getString(body, "category", "spot"),
		"strategy_type": getString(body, "strategy_type", ""),
		"coin":          getString(body, "coin", ""),
		"config_json":   configJSON,
		"direction":     getString(body, "direction", "long"),
		"leverage":      getFloat(body, "leverage", 1.0),
		"status":        "stopped",
		"pnl":           0.0,
		"created_at":    float64(nowTS),
		"updated_at":    float64(nowTS),
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
	for _, f := range []string{"name", "coin", "strategy_type", "direction", "leverage", "category"} {
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
			item["status"] = "running"
			item["updated_at"] = nowTS
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
	if item, ok := store.GetStrategyConfigs()[id]; ok {
		item["status"] = "running"
		item["updated_at"] = float64(time.Now().UnixMilli())
		mu.Unlock()
		store.PersistStrategyConfigs()
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	mu.Unlock()
	c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
}

func StopStrategyConfig(c *gin.Context) {
	id := c.Param("id")
	mu := store.GetStrategyConfigMu()
	mu.Lock()
	if item, ok := store.GetStrategyConfigs()[id]; ok {
		item["status"] = "stopped"
		item["updated_at"] = float64(time.Now().UnixMilli())
		mu.Unlock()
		store.PersistStrategyConfigs()
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	mu.Unlock()
	c.JSON(http.StatusNotFound, gin.H{"detail": "not found"})
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
