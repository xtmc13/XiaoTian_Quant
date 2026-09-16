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

// TestCreateStrategyConfigCrippledCRAPayload 回归测试（"222"）：模拟无
// strategy_type/execution_mode/direction、CRA config 齐全的残废 payload 走
// POST /strategies/configs，断言落库记录五个字段齐备（strategy_type 默认
// cra_contract、category futures、execution_mode paper、direction 取 config、
// leverage 原样）且用户填的 market_type/margin_mode/timeframe/initial_capital
// 原样落库。
func TestCreateStrategyConfigCrippledCRAPayload(t *testing.T) {
	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)

	craCfg := map[string]any{
		"first_order_price":      0.0,
		"first_order_amount":     5.0,
		"first_order_multiplier": 1.0,
		"trade_count_mode":       "single",
		"loop_count":             5000.0,
		"enable_add_position":    true,
		"order_count":            9.0,
		"add_positions": []any{
			map[string]any{"order": 1.0, "multiplier": 1.0, "spread": 0.03, "callback": 0.003, "ema_enabled": false},
			map[string]any{"order": 2.0, "multiplier": 2.0, "spread": 0.04, "callback": 0.005, "ema_enabled": false},
		},
		"take_profit_method": "full",
		"tp_mode":            "moving",
		"take_profit_ratio":  0.013,
		"profit_callback":    0.001,
		"moving_take_profit_tiers": []any{
			map[string]any{"ratio": 0.011, "drawback": 0.15},
			map[string]any{"ratio": 0.02, "drawback": 0.10},
			map[string]any{"ratio": 0.03, "drawback": 0.10},
			map[string]any{"ratio": 0.04, "drawback": 0.10},
		},
		"add_macd_enabled":  true,
		"add_macd_period":   "15m",
		"stop_loss_enabled": true,
		"stop_loss_type":    "ratio",
		"stop_loss_ratio":   0.4,
		"leverage":          150.0,
		"direction":         "short",
		"market_type":       "swap",
		"margin_mode":       "cross",
	}
	// "222 式"残废 payload：无 strategy_type / execution_mode / direction / category。
	body := map[string]any{
		"name":            "222",
		"symbol":          "BTCUSDT",
		"leverage":        150.0,
		"market_type":     "swap",
		"margin_mode":     "isolated",
		"timeframe":       "5m",
		"initial_capital": 1000.0,
		"config":          craCfg,
	}
	data, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "crippled CRA payload should be accepted")

	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "create response should be JSON")
	id, _ := resp["id"].(string)
	assertTrue(t, id != "", "create response should carry id")
	t.Cleanup(func() { store.DeleteStrategyConfig(id) })

	// ── DB 落库记录：五个字段齐备 ──
	rec, err := store.NewStrategyConfigRepo().GetByID(id)
	assertTrue(t, err == nil && rec != nil, "record must be persisted to DB")
	assertTrue(t, rec.StrategyType == "cra_contract", "strategy_type should default to cra_contract, got "+rec.StrategyType)
	assertTrue(t, rec.Category == "futures", "category should default to futures, got "+rec.Category)
	assertTrue(t, rec.ExecutionMode == "paper", "execution_mode should default to paper (安全红线), got "+rec.ExecutionMode)
	assertTrue(t, rec.Direction == "short", "direction should come from CRA config, got "+rec.Direction)
	assertTrue(t, rec.Leverage == 150, "leverage 150 must be stored as-is")
	assertTrue(t, rec.MarketType == "swap", "user market_type must be stored as-is")
	assertTrue(t, rec.MarginMode == "isolated", "user margin_mode must be stored as-is")
	assertTrue(t, rec.Timeframe == "5m", "user timeframe must be stored as-is")
	assertTrue(t, rec.InitialCapital == 1000, "user initial_capital must be stored as-is")

	// ── 内存/API 视图同样齐备（含别名字段）──
	item := store.GetStrategyConfig(id)
	assertTrue(t, item != nil, "in-memory item must exist")
	for k, want := range map[string]string{
		"strategy_type": "cra_contract", "type": "cra_contract", "strategy_name": "cra_contract",
		"category": "futures", "execution_mode": "paper", "mode": "paper", "strategy_mode": "paper",
		"direction": "short", "trade_direction": "short",
	} {
		got, _ := item[k].(string)
		assertTrue(t, got == want, k+" should be "+want+", got "+got)
	}
	// CRA config 不得丢失。
	assertTrue(t, strings.Contains(getString(item, "config_json", ""), `"add_positions"`), "config_json must keep the CRA ladder")
	assertTrue(t, strings.Contains(getString(item, "config_json", ""), `"tp_mode":"moving"`), "config_json must keep tp_mode")
}

// TestCreateStrategyConfigCrippledSpotCRAPayload 残废 payload 的现货 CRA
// 变体：strategy_type 默认 cra_spot、category spot。
func TestCreateStrategyConfigCrippledSpotCRAPayload(t *testing.T) {
	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)

	body := `{"name":"现货残废","symbol":"BTCUSDT","config":{"first_order_amount":10,"tp_mode":"static","direction":"long","market_type":"spot"}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "crippled spot CRA payload should be accepted")

	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "create response should be JSON")
	id, _ := resp["id"].(string)
	t.Cleanup(func() { store.DeleteStrategyConfig(id) })

	rec, err := store.NewStrategyConfigRepo().GetByID(id)
	assertTrue(t, err == nil && rec != nil, "record must be persisted to DB")
	assertTrue(t, rec.StrategyType == "cra_spot", "strategy_type should default to cra_spot, got "+rec.StrategyType)
	assertTrue(t, rec.Category == "spot", "category should default to spot, got "+rec.Category)
	assertTrue(t, rec.ExecutionMode == "paper", "execution_mode should default to paper, got "+rec.ExecutionMode)
	assertTrue(t, rec.Direction == "long", "direction should default to long, got "+rec.Direction)
}

// TestCreateStrategyConfigNonCRAStillRejected 非 CRA payload 缺 strategy_type
// 时维持原有 400 拒绝（不破坏现有 API 契约）。
func TestCreateStrategyConfigNonCRAStillRejected(t *testing.T) {
	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)

	body := `{"name":"非CRA","symbol":"BTCUSDT"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "non-CRA payload without strategy_type must still be rejected")
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
