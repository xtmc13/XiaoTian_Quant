package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
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

/* ── P0-1 运行面板：runtime 接口 ─────────────────────────────── */

// TestGetStrategyRuntimeNotFound 404 路径：未知 id 必须 404。
func TestGetStrategyRuntimeNotFound(t *testing.T) {
	r := setupRouter()
	r.GET("/strategies/configs/:id/runtime", GetStrategyRuntime)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/strategies/configs/no-such-id/runtime", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusNotFound, "runtime for unknown id must be 404")
}

// TestGetStrategyRuntimeRunning 全链路：macd 配置启动后，runtime 接口返回
// 引擎内真实运行状态（running=true）；未启动配置 status 为 null。
func TestGetStrategyRuntimeRunning(t *testing.T) {
	// 引擎单例先用真实事件总线初始化（GetEngine(nil) 依赖该单例）。
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("macd", func() strategy.Strategy { return strategies.NewMACDStrategy() })

	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)
	r.POST("/strategies/configs/batch-start", BatchStartConfigs)
	r.GET("/strategies/configs/:id/runtime", GetStrategyRuntime)

	create := func(name string) string {
		body := `{"name":"` + name + `","strategy_type":"macd","symbol":"BTCUSDT","config":{}}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusOK, "create "+name)
		var resp map[string]any
		assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "create response JSON")
		id, _ := resp["id"].(string)
		assertTrue(t, id != "", "create id")
		t.Cleanup(func() { store.DeleteStrategyConfig(id) })
		return id
	}

	runningID := create("runtime运行中")
	stoppedID := create("runtime已停止")

	// 启动 runningID。
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs/batch-start", strings.NewReader(`{"ids":["`+runningID+`"]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "batch-start")
	t.Cleanup(func() { stopStrategyInEngine(runningID) })

	var payload map[string]any
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/strategies/configs/"+runningID+"/runtime", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "runtime status code")
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &payload) == nil, "runtime response JSON")
	st, ok := payload["status"].(map[string]any)
	assertTrue(t, ok, "status must be an object for running strategy")
	assertTrue(t, st["running"] == true, "engine status.running must be true")
	if _, has := st["bars_collected"]; !has {
		t.Fatal("status must carry bars_collected")
	}

	// 未启动配置：status 为 null，config 仍可解析。
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/strategies/configs/"+stoppedID+"/runtime", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "runtime (stopped) status code")
	payload = map[string]any{}
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &payload) == nil, "runtime (stopped) response JSON")
	assertTrue(t, payload["status"] == nil, "status must be null for stopped strategy")
	if _, has := payload["config"]; !has {
		t.Fatal("config field must be present")
	}
	_ = eng
}

/* ── P0-4 类型-参数防呆 ─────────────────────────────────────── */

// TestCreateStrategyConfigTypeConfigMismatch 显式非 CRA 类型携带 CRA 特征键
// 必须 400；显式 CRA 类型缺 first_order_amount / add_positions 也必须 400。
func TestCreateStrategyConfigTypeConfigMismatch(t *testing.T) {
	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)

	cases := []struct {
		name string
		body string
	}{
		{
			name: "macd + CRA 特征键",
			body: `{"name":"错配macd","strategy_type":"macd","symbol":"BTCUSDT","config":{"add_positions":[{"order":1,"multiplier":1,"spread":0.03,"callback":0.003}]}}`,
		},
		{
			name: "trend + moving_take_profit_tiers",
			body: `{"name":"错配trend","strategy_type":"trend","symbol":"BTCUSDT","config":{"moving_take_profit_tiers":[{"ratio":0.02,"drawback":0.2}]}}`,
		},
		{
			name: "cra_contract 缺首单金额",
			body: `{"name":"缺首单","strategy_type":"cra_contract","symbol":"BTCUSDT","config":{"add_positions":[{"order":1,"multiplier":1,"spread":0.03,"callback":0.003}]}}`,
		},
		{
			name: "cra_spot 缺补仓梯子",
			body: `{"name":"缺梯子","strategy_type":"cra_spot","symbol":"BTCUSDT","config":{"first_order_amount":100}}`,
		},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusBadRequest, tc.name+" must be rejected with 400")
		var resp map[string]any
		assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, tc.name+" error body JSON")
		detail, _ := resp["detail"].(string)
		assertTrue(t, strings.Contains(detail, "参数与策略类型不匹配"), tc.name+" error must explain mismatch, got: "+detail)
	}

	// 对照：显式 cra_contract + 完整 CRA 参数必须 200。
	okBody := `{"name":"正确cra","strategy_type":"cra_contract","symbol":"BTCUSDT","config":{"first_order_amount":100,"add_positions":[{"order":1,"multiplier":1,"spread":0.03,"callback":0.003}]}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(okBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "explicit cra_contract with full params must be accepted")
	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "ok response JSON")
	id, _ := resp["id"].(string)
	assertTrue(t, id != "", "ok response id")
	t.Cleanup(func() { store.DeleteStrategyConfig(id) })
}

// TestCreateStrategyConfigCRATypeAliasesKeepWorking 映射到 CRA 引擎的前端
// 别名类型（trend_long 等）携带 CRA 参数必须照常保存——防呆不得误伤
// 现有创建路径（StrategyCreatePanel 全部走这个组合）。
func TestCreateStrategyConfigCRATypeAliasesKeepWorking(t *testing.T) {
	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)

	body := `{"name":"别名trendlong","strategy_type":"trend_long","symbol":"BTCUSDT","config":{"first_order_amount":100,"add_positions":[{"order":1,"multiplier":1,"spread":0.03,"callback":0.003}]}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "trend_long + CRA params must keep working")
	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "response JSON")
	id, _ := resp["id"].(string)
	t.Cleanup(func() { store.DeleteStrategyConfig(id) })
}

// TestUpdateStrategyConfigTypeConfigMismatch 编辑时同样防呆：把已有配置
// 改成"非 CRA 类型 + CRA 特征键"必须 400。
func TestUpdateStrategyConfigTypeConfigMismatch(t *testing.T) {
	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)
	r.PUT("/strategies/configs/:id", UpdateStrategyConfig)

	body := `{"name":"更新防呆","strategy_type":"cra_contract","symbol":"BTCUSDT","config":{"first_order_amount":100,"add_positions":[{"order":1,"multiplier":1,"spread":0.03,"callback":0.003}]}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "setup create")
	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "setup response JSON")
	id, _ := resp["id"].(string)
	t.Cleanup(func() { store.DeleteStrategyConfig(id) })

	// 改成 macd 但保留 CRA 参数 → 400。
	bad := `{"strategy_type":"macd","config":{"first_order_amount":100,"add_positions":[{"order":1,"multiplier":1,"spread":0.03,"callback":0.003}]}}`
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/strategies/configs/"+id, strings.NewReader(bad))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "update to macd + CRA keys must be 400")

	// 改成 macd 且清掉 CRA 参数（修复残废配置）→ 200。
	good := `{"strategy_type":"macd","config":{}}`
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/strategies/configs/"+id, strings.NewReader(good))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "update to macd with clean config must succeed")
}

/* ── P1-7 模拟盘压回提示 ────────────────────────────────────── */

// TestCreateUpdateForcedPaperFlag 请求 execution_mode 为 live/空被压回 paper
// 时响应带 forced_paper；显式 paper 时不带。
func TestCreateUpdateForcedPaperFlag(t *testing.T) {
	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)
	r.PUT("/strategies/configs/:id", UpdateStrategyConfig)

	post := func(body string) map[string]any {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusOK, "create status")
		var resp map[string]any
		assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "create response JSON")
		return resp
	}

	// live 被压回 → forced_paper。
	resp := post(`{"name":"压回live","strategy_type":"macd","symbol":"BTCUSDT","execution_mode":"live","config":{}}`)
	assertTrue(t, resp["forced_paper"] == true, "live must be flagged forced_paper")
	idLive, _ := resp["id"].(string)
	t.Cleanup(func() { store.DeleteStrategyConfig(idLive) })
	rec, err := store.NewStrategyConfigRepo().GetByID(idLive)
	assertTrue(t, err == nil && rec != nil, "record persisted")
	assertTrue(t, rec.ExecutionMode == "paper", "live must be stored as paper")

	// 空 execution_mode 兜底 paper → forced_paper。
	resp = post(`{"name":"压回空","strategy_type":"macd","symbol":"BTCUSDT","config":{}}`)
	assertTrue(t, resp["forced_paper"] == true, "empty execution_mode must be flagged forced_paper")
	idEmpty, _ := resp["id"].(string)
	t.Cleanup(func() { store.DeleteStrategyConfig(idEmpty) })

	// 显式 paper → 不标记。
	resp = post(`{"name":"正常paper","strategy_type":"macd","symbol":"BTCUSDT","execution_mode":"paper","config":{}}`)
	if _, has := resp["forced_paper"]; has {
		t.Fatal("explicit paper must not be flagged forced_paper")
	}
	idPaper, _ := resp["id"].(string)
	t.Cleanup(func() { store.DeleteStrategyConfig(idPaper) })

	// Update：live → forced_paper；paper → 无标记。
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/strategies/configs/"+idPaper, strings.NewReader(`{"execution_mode":"live"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "update status")
	resp = map[string]any{}
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "update response JSON")
	assertTrue(t, resp["forced_paper"] == true, "update with live must be flagged forced_paper")
}

/* ── P1-8 批量结果汇总 ──────────────────────────────────────── */

// TestBatchStartStopConfigsPerItemResults 批量启停必须返回 per-item 结果与
// started/stopped、failed 计数（前端汇总 toast 数据源）。
func TestBatchStartStopConfigsPerItemResults(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("macd", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	_ = eng

	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)
	r.POST("/strategies/configs/batch-start", BatchStartConfigs)
	r.POST("/strategies/configs/batch-stop", BatchStopConfigs)

	ids := []string{}
	for _, name := range []string{"批量A", "批量B"} {
		body := `{"name":"` + name + `","strategy_type":"macd","symbol":"BTCUSDT","config":{}}`
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusOK, "create "+name)
		var resp map[string]any
		assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "create response JSON")
		id, _ := resp["id"].(string)
		ids = append(ids, id)
		t.Cleanup(func() {
			stopStrategyInEngine(id)
			store.DeleteStrategyConfig(id)
		})
	}

	payload := `{"ids":["` + strings.Join(ids, `","`) + `","ghost-id"]}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs/batch-start", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "batch-start status")
	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "batch-start response JSON")
	assertTrue(t, resp["started"].(float64) == 2, "started must be 2")
	assertTrue(t, resp["failed"].(float64) == 1, "failed must be 1 (ghost id)")
	results, ok := resp["results"].([]any)
	assertTrue(t, ok && len(results) == 3, "results must carry per-item entries")
	ghost := results[2].(map[string]any)
	assertTrue(t, ghost["ok"] == false, "ghost item must be ok=false")
	assertTrue(t, ghost["error"].(string) != "", "ghost item must carry error")

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/strategies/configs/batch-stop", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "batch-stop status")
	resp = map[string]any{}
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "batch-stop response JSON")
	assertTrue(t, resp["stopped"].(float64) == 2, "stopped must be 2")
	assertTrue(t, resp["failed"].(float64) == 1, "failed must be 1 (ghost id)")
}

/* ── 归属判别收归后端（kind 查询参数，2026-09-17 根治互窜） ─────── */

// TestGetStrategyConfigsKindFilter 混合数据集下 kind=bot / kind=strategy 的
// 分流正确性。回归点：market_type='futures' 的无 bot 标记合约策略必须归
// strategy 侧（前端 swap 启发式过滤的漏网之鱼）。
func TestGetStrategyConfigsKindFilter(t *testing.T) {
	repo := store.NewStrategyConfigRepo()
	seed := []*store.StrategyConfigRecord{
		// strategy_mode='bot'（DB 往返后 ToMap 给出 mode/strategy_mode='bot'）
		{ID: "kind-bot-mode", Name: "脚本机器人", Category: "futures", StrategyType: "grid_trading", Symbol: "BTCUSDT", ExecutionMode: "bot"},
		// 历史机器人品类
		{ID: "kind-martin", Name: "马丁机器人", Category: "martin", StrategyType: "martin", Symbol: "BTCUSDT"},
		// 纯策略：cra_contract（swap）
		{ID: "kind-cra", Name: "CRA合约策略", Category: "futures", StrategyType: "cra_contract", Symbol: "BTCUSDT", MarketType: "swap", ExecutionMode: "paper"},
		// 回归点：market_type='futures' 的无 bot 标记合约策略
		{ID: "kind-futures-leak", Name: "futures合约策略", Category: "futures", StrategyType: "trend_long", Symbol: "ETHUSDT", MarketType: "futures", ExecutionMode: "paper"},
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

	botList := getStrategyConfigList(t, r, "/strategies/configs?kind=bot")
	assertTrue(t, listContainsID(botList, "kind-bot-mode"), "kind=bot must include strategy_mode=bot record")
	assertTrue(t, listContainsID(botList, "kind-martin"), "kind=bot must include martin category record")
	assertTrue(t, !listContainsID(botList, "kind-cra"), "kind=bot must exclude cra_contract strategy")
	assertTrue(t, !listContainsID(botList, "kind-futures-leak"), "kind=bot must exclude futures contract strategy (regression)")

	strategyList := getStrategyConfigList(t, r, "/strategies/configs?kind=strategy")
	assertTrue(t, listContainsID(strategyList, "kind-cra"), "kind=strategy must include cra_contract strategy")
	assertTrue(t, listContainsID(strategyList, "kind-futures-leak"), "kind=strategy must include futures contract strategy (regression)")
	assertTrue(t, !listContainsID(strategyList, "kind-bot-mode"), "kind=strategy must exclude strategy_mode=bot record")
	assertTrue(t, !listContainsID(strategyList, "kind-martin"), "kind=strategy must exclude martin category record")

	// 不传 kind：保持旧行为（仅排除 martin/wallstreet 品类）。
	legacyList := getStrategyConfigList(t, r, "/strategies/configs")
	assertTrue(t, !listContainsID(legacyList, "kind-martin"), "legacy default must still exclude martin")
	assertTrue(t, listContainsID(legacyList, "kind-cra"), "legacy default must include cra strategy")
	assertTrue(t, listContainsID(legacyList, "kind-futures-leak"), "legacy default must include futures strategy")
	assertTrue(t, listContainsID(legacyList, "kind-bot-mode"), "legacy default must include bot-mode record (not martin category)")

	// 非法 kind → 400。
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/strategies/configs?kind=bogus", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusBadRequest, "invalid kind must be 400")
}

// TestCreateBotConfigKindPersistence 通过 API 创建的机器人配置，bot 身份
// 必须落进 config_json（DB 重建后仍被判别为 bot）；列表响应水合 bot_type/
// trading_config 顶层字段；带 execution_mode 的 Update 不得洗掉 bot 身份。
func TestCreateBotConfigKindPersistence(t *testing.T) {
	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)
	r.PUT("/strategies/configs/:id", UpdateStrategyConfig)
	r.GET("/strategies/configs", GetStrategyConfigs)

	body := `{
		"name":"网格机器人",
		"strategy_type":"grid_trading",
		"strategy_mode":"bot",
		"bot_type":"grid",
		"symbol":"BTCUSDT",
		"market_type":"swap",
		"trading_config":{"bot_type":"grid","initial_capital":1000,"symbol":"BTCUSDT"}
	}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "bot create status")
	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "create response JSON")
	id, _ := resp["id"].(string)
	assertTrue(t, id != "", "create id")
	t.Cleanup(func() { store.DeleteStrategyConfig(id) })

	// 每个 list 请求都走 DB 重建（GetStrategyConfigs 每次从 DB 重建），
	// 因此这里直接验证持久化后的判别结果。
	botList := getStrategyConfigList(t, r, "/strategies/configs?kind=bot")
	assertTrue(t, listContainsID(botList, id), "created bot must appear in kind=bot after DB rebuild")
	strategyList := getStrategyConfigList(t, r, "/strategies/configs?kind=strategy")
	assertTrue(t, !listContainsID(strategyList, id), "created bot must not appear in kind=strategy")

	// 响应水合：顶层 bot_type 与 trading_config 从 config_json 恢复。
	var item map[string]any
	for _, it := range botList {
		if it["id"] == id {
			item = it
		}
	}
	assertTrue(t, item != nil, "bot item must be in kind=bot list")
	assertTrue(t, getString(item, "bot_type", "") == "grid", "bot_type must be hydrated to top level")
	tc, ok := item["trading_config"].(map[string]any)
	assertTrue(t, ok && getString(tc, "bot_type", "") == "grid", "trading_config.bot_type must be hydrated")

	// Update 携带 execution_mode（别名同步会覆写 strategy_mode）后，
	// bot 身份必须保留。
	upd := `{"execution_mode":"paper","trading_config":{"bot_type":"grid","initial_capital":2000}}`
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/strategies/configs/"+id, strings.NewReader(upd))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "bot update status")

	botList = getStrategyConfigList(t, r, "/strategies/configs?kind=bot")
	assertTrue(t, listContainsID(botList, id), "bot identity must survive update with execution_mode")
	strategyList = getStrategyConfigList(t, r, "/strategies/configs?kind=strategy")
	assertTrue(t, !listContainsID(strategyList, id), "bot must stay out of kind=strategy after update")
}

/* ── 任务 C：开仓指标选择器（indicator_params）后端配套 ──────── */

// TestCreateStrategyConfigIndicatorParams 合法参数透传落库可回读；非法参数
// （非对象/非正数/custom 缺字段）400；custom 合法不报错。
func TestCreateStrategyConfigIndicatorParams(t *testing.T) {
	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)
	r.GET("/strategies/configs/:id", GetStrategyConfig)

	post := func(body string) (int, map[string]any) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code, resp
	}

	craCfg := `"first_order_amount":100,"add_positions":[{"order":1,"multiplier":1,"spread":0.03,"callback":0.003}]`

	// 合法：顺势多 + indicator_params.macd 风格参数 → 200 且可回读。
	okBody := `{"name":"指标参数","strategy_type":"cra_contract","symbol":"BTCUSDT","config":{` + craCfg + `,"open_indicator":"trend_long","indicator_params":{"trend_long":{"fast":10,"slow":30,"period":"5m"}}}}`
	code, resp := post(okBody)
	assertEq(t, code, http.StatusOK, "valid indicator_params create")
	id, _ := resp["id"].(string)
	assertTrue(t, id != "", "create id")
	t.Cleanup(func() { store.DeleteStrategyConfig(id) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/strategies/configs/"+id, nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "get config")
	var got map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &got) == nil, "get config JSON")
	cfg, _ := got["config"].(map[string]any)
	assertTrue(t, cfg != nil, "config must be parsed")
	ip, _ := cfg["indicator_params"].(map[string]any)
	assertTrue(t, ip != nil, "indicator_params must survive round-trip")
	tl, _ := ip["trend_long"].(map[string]any)
	assertTrue(t, tl != nil && tl["fast"].(float64) == 10, "indicator_params.trend_long.fast must be readable")
	assertTrue(t, cfg["open_indicator"].(string) == "trend_long", "open_indicator must survive round-trip")

	// 非法：fast 非正数 → 400。
	bad1 := `{"name":"坏参数1","strategy_type":"cra_contract","symbol":"BTCUSDT","config":{` + craCfg + `,"indicator_params":{"macd":{"fast":-5,"slow":26,"signal":9}}}}`
	code, _ = post(bad1)
	assertEq(t, code, http.StatusBadRequest, "non-positive indicator param must be 400")

	// 非法：indicator_params 不是对象 → 400。
	bad2 := `{"name":"坏参数2","strategy_type":"cra_contract","symbol":"BTCUSDT","config":{` + craCfg + `,"indicator_params":"oops"}}`
	code, _ = post(bad2)
	assertEq(t, code, http.StatusBadRequest, "non-object indicator_params must be 400")

	// 非法：custom 缺 code_id → 400。
	bad3 := `{"name":"坏参数3","strategy_type":"cra_contract","symbol":"BTCUSDT","config":{` + craCfg + `,"open_indicator":"custom","indicator_params":{"custom":{"name":"x"}}}}`
	code, _ = post(bad3)
	assertEq(t, code, http.StatusBadRequest, "custom without code_id must be 400")

	// 合法：custom 完整 → 200（本期解析层接受、不报错）。
	okCustom := `{"name":"自定义指标","strategy_type":"cra_contract","symbol":"BTCUSDT","config":{` + craCfg + `,"open_indicator":"custom","indicator_params":{"custom":{"code_id":42,"name":"我的指标"}}}}`
	code, resp = post(okCustom)
	assertEq(t, code, http.StatusOK, "valid custom indicator create must be accepted")
	customID, _ := resp["id"].(string)
	t.Cleanup(func() { store.DeleteStrategyConfig(customID) })
}

// TestIndicatorParamsFullChain 全链路自测：构造"顺势多"风格 CRA config（含
// indicator_params）→ Create → Start（引擎直驱，假价格）→ 引擎收到配置且
// 信号路径不报错。真实币安下单链路依赖外网与凭证，不可用则以本测试为准。
func TestIndicatorParamsFullChain(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("cra_contract", func() strategy.Strategy {
		return cra.NewCRAContractStrategy("cra_contract", "BTCUSDT")
	})

	r := setupRouter()
	r.POST("/strategies/configs", CreateStrategyConfig)

	body := `{
		"name":"顺势多全链路",
		"strategy_type":"cra_contract",
		"symbol":"BTCUSDT",
		"direction":"long",
		"config":{
			"first_order_amount":100,
			"first_order_multiplier":1,
			"order_count":3,
			"enable_add_position":true,
			"add_positions":[{"order":1,"multiplier":1,"spread":0.03,"callback":0.003}],
			"tp_mode":"static","take_profit_method":"full","take_profit_ratio":0.013,
			"open_indicator":"trend_long",
			"indicator_params":{"trend_long":{"fast":10,"slow":30,"period":"close"}},
			"market_type":"swap","leverage":10,"direction":"long"
		}
	}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "chain create")
	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "chain create JSON")
	id, _ := resp["id"].(string)
	assertTrue(t, id != "", "chain create id")
	t.Cleanup(func() {
		stopStrategyInEngine(id)
		store.DeleteStrategyConfig(id)
	})

	// Start（引擎直驱路径与 StartStrategyConfig 相同）。
	item := store.GetStrategyConfig(id)
	assertTrue(t, item != nil, "chain item must exist")
	assertTrue(t, startStrategyInEngine(id, item) == nil, "startStrategyInEngine must succeed with indicator_params")

	s := eng.Get(id)
	assertTrue(t, s != nil, "strategy must be registered in engine")
	assertTrue(t, s.IsRunning(), "strategy must be running")

	// 假价格直驱 OnBar：信号路径不得报错，顺势多应发出 LONG 首单信号
	//（open_* 门槛全 false → 指标确认放行；custom/trend_long 新键不改变门槛）。
	sig, err := s.OnBar(model.Bar{Symbol: "BTCUSDT", Open: 50000, High: 50000, Low: 50000, Close: 50000, Volume: 1, Interval: "15m", Time: time.Now().UnixMilli()}, nil)
	assertTrue(t, err == nil, "OnBar must not error")
	assertTrue(t, sig != nil, "first bar must emit entry signal")
	assertTrue(t, sig.Direction == "LONG", "顺势多 first signal must be LONG, got "+sig.Direction)

	// RuntimeStatus 路径也不报错（上一迭代的面板数据源）。
	if rs, ok := eng.RuntimeStatus(id); ok {
		assertTrue(t, rs != nil, "runtime status must be non-nil for cra strategy")
	}
}
