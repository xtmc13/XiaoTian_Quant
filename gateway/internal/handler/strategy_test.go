package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// TestCreateStrategyConfigWriteThrough 回归测试（Bug 1）：策略实验室新建的配置
// 必须立即可见于 DB 优先的读取路径（store.GetStrategyConfigs 每次都从 DB 重建），
// 而不是重启后才经 JSON 迁移出现；删除后也应立即消失。
func TestCreateStrategyConfigWriteThrough(t *testing.T) {
	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)
	r.GET("/strategies/configs", GetStrategyConfigs)
	r.DELETE("/strategies/configs/:id", DeleteStrategyConfig)

	// ── Create ──
	body := `{"name":"写穿透回归","strategy_type":"trend","symbol":"BTCUSDT","category":"futures","market_type":"futures"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "create status")

	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "create response should be JSON")
	id, _ := resp["id"].(string)
	assertTrue(t, id != "", "create response should carry id")
	t.Cleanup(func() { store.DeleteStrategyConfig(id) })

	// 关键断言：store 层 DB 优先读取路径立即可见。
	found := false
	for _, item := range store.GetStrategyConfigs() {
		if item["id"] == id {
			found = true
			break
		}
	}
	assertTrue(t, found, "created config must be visible via store.GetStrategyConfigs() immediately")

	// HTTP 列表也应立即包含。
	list := getStrategyConfigList(t, r, "/strategies/configs")
	assertTrue(t, listContainsID(list, id), "created config must appear in GET /strategies/configs immediately")

	// ── Delete 后应立即从两条路径消失 ──
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/strategies/configs/"+id, nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "delete status")

	for _, item := range store.GetStrategyConfigs() {
		assertTrue(t, item["id"] != id, "deleted config must not survive in store.GetStrategyConfigs()")
	}
	list = getStrategyConfigList(t, r, "/strategies/configs")
	assertTrue(t, !listContainsID(list, id), "deleted config must not survive in GET /strategies/configs")
}

// TestGetStrategyConfigsExcludesBotCategories 回归测试（Bug 2）：策略机器人
// （martin/wallstreet）与策略实验室共用 strategy_configs 存储；未指定 category
// 时列表必须默认排除机器人品类，显式指定 category 时仍按该品类过滤。
func TestGetStrategyConfigsExcludesBotCategories(t *testing.T) {
	repo := store.NewStrategyConfigRepo()
	seed := []*store.StrategyConfigRecord{
		{ID: "test-excl-martin", Name: "马丁机器人", Category: "martin", StrategyType: "martin", Symbol: "BTCUSDT"},
		{ID: "test-excl-futures", Name: "合约趋势", Category: "futures", StrategyType: "trend", Symbol: "ETHUSDT"},
	}
	if err := repo.UpsertAll(seed); err != nil {
		t.Fatalf("seed configs: %v", err)
	}
	t.Cleanup(func() {
		for _, rec := range seed {
			_ = store.NewStrategyConfigRepo().Delete(rec.ID)
		}
	})

	r := setupRouter()
	r.GET("/strategies/configs", GetStrategyConfigs)

	// 不带 category：只应看到 futures 策略实验室配置。
	list := getStrategyConfigList(t, r, "/strategies/configs")
	assertTrue(t, listContainsID(list, "test-excl-futures"), "futures config must be listed without category param")
	assertTrue(t, !listContainsID(list, "test-excl-martin"), "martin bot must be excluded without category param")

	// 显式 category=martin：仍按品类过滤，机器人可见。
	list = getStrategyConfigList(t, r, "/strategies/configs?category=martin")
	assertTrue(t, listContainsID(list, "test-excl-martin"), "martin bot must be listed with ?category=martin")
	assertTrue(t, !listContainsID(list, "test-excl-futures"), "futures config must not appear with ?category=martin")
}

func getStrategyConfigList(t *testing.T, r *gin.Engine, path string) []map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "list status for "+path)
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("list response should be a JSON array: %v", err)
	}
	return list
}

func listContainsID(list []map[string]any, id string) bool {
	for _, it := range list {
		if it["id"] == id {
			return true
		}
	}
	return false
}
