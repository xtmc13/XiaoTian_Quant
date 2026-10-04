package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// ── D2：在线单量限制（online_order_limit，币富名词解释 #32）──
//
// 语义口径（引擎模型核查结论，详见 cra_online_limit.go 头注）：CRA 单实例
// 单 symbol 单方向持仓，持仓中不会再开首单（cra_strategy.go 首单分支被
// !st.InPosition 门控），dual 也是每循环单选方向——单实例并发持仓恒为 1，
// 参数在实例内部无调节空间。故实现为币富本来的跨交易对总量控制：同一用户
// 名下 running 状态 CRA 合约实例数上限，在 Start 路径校验。

// seedCRAConfig 落库一条策略配置（经 DB 往返，与生产读取路径一致）。
func seedCRAConfig(t *testing.T, id string, userID int64, stype, status string, limit any) {
	t.Helper()
	cfg := map[string]any{
		"first_order_amount": 10, "tp_mode": "static", "take_profit_ratio": 0.013,
		"market_type": "swap", "leverage": 10, "direction": "long",
	}
	if limit != nil {
		cfg["online_order_limit"] = limit
	}
	cj, _ := json.Marshal(cfg)
	item := map[string]any{
		"id": id, "user_id": userID, "name": id, "strategy_type": stype,
		"symbol": "BTCUSDT", "status": status, "config_json": string(cj),
		"market_type": "swap", "leverage": 10,
	}
	store.SetStrategyConfig(id, item)
	store.PersistStrategyConfigs()
	t.Cleanup(func() {
		store.DeleteStrategyConfig(id)
		_ = store.NewStrategyConfigRepo().Delete(id)
	})
}

func TestCRAOnlineOrderLimitParsing(t *testing.T) {
	// 缺失 → 默认 10（与 cra.ParseCRAParams/前端预设一致）。
	if got := craOnlineOrderLimit(map[string]any{}); got != 10 {
		t.Fatalf("missing key: got %d, want 10", got)
	}
	// config_json 取值。
	if got := craOnlineOrderLimit(map[string]any{"config_json": `{"online_order_limit":3}`}); got != 3 {
		t.Fatalf("config_json: got %d, want 3", got)
	}
	// 顶层平铺键兜底；config_json 优先于顶层。
	if got := craOnlineOrderLimit(map[string]any{"online_order_limit": 4}); got != 4 {
		t.Fatalf("top-level: got %d, want 4", got)
	}
	if got := craOnlineOrderLimit(map[string]any{
		"online_order_limit": 4, "config_json": `{"online_order_limit":2}`,
	}); got != 2 {
		t.Fatalf("config_json must win over top-level: got %d, want 2", got)
	}
	// <1 钳制为 1（限额必须有牙齿，0/负数视为配置错误按最严口径）。
	if got := craOnlineOrderLimit(map[string]any{"config_json": `{"online_order_limit":0}`}); got != 1 {
		t.Fatalf("zero clamp: got %d, want 1", got)
	}
}

func TestCRAOnlineOrderLimitCountScope(t *testing.T) {
	seedCRAConfig(t, "ool-scope-run", 7, "cra_contract", "running", nil)
	// 别名类型（trend_long → cra_contract 引擎）也计入。
	seedCRAConfig(t, "ool-scope-alias", 7, "trend_long", "running", nil)
	// 以下一律不计入：stopped / 现货 / 其他用户。
	seedCRAConfig(t, "ool-scope-stopped", 7, "cra_contract", "stopped", nil)
	seedCRAConfig(t, "ool-scope-spot", 7, "cra_spot", "running", nil)
	seedCRAConfig(t, "ool-scope-other-user", 8, "cra_contract", "running", nil)

	if got := countRunningCRAContracts(7, ""); got != 2 {
		t.Fatalf("user 7 running CRA contracts = %d, want 2 (alias included, stopped/spot/other-user excluded)", got)
	}
	// 自排除：重启自身时自己不占名额。
	if got := countRunningCRAContracts(7, "ool-scope-run"); got != 1 {
		t.Fatalf("self-excluded count = %d, want 1", got)
	}
	if got := countRunningCRAContracts(8, ""); got != 1 {
		t.Fatalf("user 8 count = %d, want 1", got)
	}
}

func TestCRAOnlineOrderLimitEnforcement(t *testing.T) {
	seedCRAConfig(t, "ool-enf-run", 9, "cra_contract", "running", nil)

	overLimit := map[string]any{
		"id": "ool-enf-new", "user_id": int64(9), "strategy_type": "cra_contract",
		"config_json": `{"online_order_limit":1}`,
	}
	if err := enforceOnlineOrderLimit("ool-enf-new", overLimit); err == nil {
		t.Fatal("over-limit start must be rejected")
	} else if !strings.Contains(err.Error(), "在线单量限制") {
		t.Fatalf("error must name the limit, got %v", err)
	}

	// 限额 2、已运行 1 → 放行。
	underLimit := map[string]any{
		"id": "ool-enf-new", "user_id": int64(9), "strategy_type": "cra_contract",
		"config_json": `{"online_order_limit":2}`,
	}
	if err := enforceOnlineOrderLimit("ool-enf-new", underLimit); err != nil {
		t.Fatalf("under-limit start must pass, got %v", err)
	}

	// 现货配置不校验（币富该功能在合约页）。
	spot := map[string]any{"id": "ool-enf-spot", "user_id": int64(9), "strategy_type": "cra_spot"}
	if err := enforceOnlineOrderLimit("ool-enf-spot", spot); err != nil {
		t.Fatalf("spot must be exempt, got %v", err)
	}
}

// TestCRAOnlineOrderLimitStartHTTP 用户 Start 路径端到端：超限 409 且策略
// 没有进引擎；未超限 200 且引擎启动成功（证明校验接在真实 Start 链路上）。
func TestCRAOnlineOrderLimitStartHTTP(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("cra_contract", func() strategy.Strategy {
		return cra.NewCRAContractStrategy("cra_contract", "BTCUSDT")
	})

	r := setupRouter()
	r.POST("/strategies/configs/:id/start", StartStrategyConfig)
	start := func(id string) (int, map[string]any) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/strategies/configs/"+id+"/start", nil)
		r.ServeHTTP(w, req)
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code, resp
	}

	// 已运行 1 个（用户 11），新配置限额 1 → 409，detail 说明口径。
	seedCRAConfig(t, "ool-http-run", 11, "cra_contract", "running", nil)
	seedCRAConfig(t, "ool-http-blocked", 11, "cra_contract", "stopped", 1)
	code, resp := start("ool-http-blocked")
	assertEq(t, code, http.StatusConflict, "over-limit start must be 409")
	assertTrue(t, strings.Contains(getString(resp, "detail", ""), "在线单量限制"),
		"409 detail must explain the limit, got "+getString(resp, "detail", ""))
	assertTrue(t, eng.Get("ool-http-blocked") == nil, "blocked strategy must not enter engine")

	// 限额调大为 2 → 同一 Start 链路放行，引擎实例真实在跑。
	seedCRAConfig(t, "ool-http-ok", 11, "cra_contract", "stopped", 2)
	code, resp = start("ool-http-ok")
	assertEq(t, code, http.StatusOK, "under-limit start must be 200, detail="+getString(resp, "detail", ""))
	t.Cleanup(func() { stopStrategyInEngine("ool-http-ok") })
	if s := eng.Get("ool-http-ok"); s == nil || !s.IsRunning() {
		t.Fatal("under-limit strategy must be registered and running in engine")
	}
}
