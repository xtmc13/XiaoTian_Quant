package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

func modelBar(close float64) model.Bar {
	return model.Bar{Symbol: "BTCUSDT", Close: close, High: close, Low: close}
}

func modelOrderFill(symbol string, qty, price float64, oid string) model.OrderData {
	return model.OrderData{Symbol: symbol, Side: model.SideBuy, Status: model.StatusFilled,
		Filled: qty, AvgFillPrice: price, ClientOID: oid}
}

func modelOrderSellFill(symbol string, qty, price float64, oid string) model.OrderData {
	return model.OrderData{Symbol: symbol, Side: model.SideSell, Status: model.StatusFilled,
		Filled: qty, AvgFillPrice: price, ClientOID: oid}
}

// ── G1：CRA 运行时手动操控端点（POST /strategies/configs/:id/manual-action）──
//
// 覆盖端点契约：鉴权/属主（403）、非 CRA 类型（400）、非 running（409）、
// 动作枚举与参数形校验（400）、引擎消费正确性（HTTP → 引擎实例状态演进）。
// 下单链路级 paper 全链路在 app 包（cra_manual_action_paper_test.go）。

// seedManualConfig 落库一条 CRA 配置（与 seedCRAConfig 同模式，可指定 market_type）。
func seedManualConfig(t *testing.T, id string, userID int64, stype, status string, cfg map[string]any) {
	t.Helper()
	if cfg == nil {
		cfg = map[string]any{}
	}
	if _, ok := cfg["first_order_amount"]; !ok {
		cfg["first_order_amount"] = 100
	}
	if _, ok := cfg["tp_mode"]; !ok {
		cfg["tp_mode"] = "static"
	}
	if _, ok := cfg["take_profit_ratio"]; !ok {
		cfg["take_profit_ratio"] = 0.013
	}
	cj, _ := json.Marshal(cfg)
	item := map[string]any{
		"id": id, "user_id": userID, "name": id, "strategy_type": stype,
		"symbol": "BTCUSDT", "status": status, "config_json": string(cj),
	}
	if mt, ok := cfg["market_type"].(string); ok {
		item["market_type"] = mt
	}
	store.SetStrategyConfig(id, item)
	store.PersistStrategyConfigs()
	t.Cleanup(func() {
		store.DeleteStrategyConfig(id)
		_ = store.NewStrategyConfigRepo().Delete(id)
	})
}

// setupManualRouter 注册端点；uid>=0 时注入登录用户（属主校验链路）。
func setupManualRouter(uid int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if uid >= 0 {
		r.Use(func(c *gin.Context) {
			c.Set(middleware.UserIDKey, uid)
			c.Set(middleware.RoleKey, "user")
			c.Next()
		})
	}
	r.POST("/strategies/configs/:id/manual-action", ManualStrategyAction)
	return r
}

func postManual(t *testing.T, r *gin.Engine, id, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/strategies/configs/"+id+"/manual-action",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp
}

func TestCRAManualActionGateMatrix(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("cra_spot", func() strategy.Strategy {
		return cra.NewCRASpotStrategy("cra_spot", "BTCUSDT")
	})
	_ = eng

	r := setupManualRouter(-1) // 未注入用户（单用户模式）：属主放行

	// 非 CRA 类型（macd 指标策略）→ 400。
	seedManualConfig(t, "ma-gate-macd", 0, "macd", "running", nil)
	code, resp := postManual(t, r, "ma-gate-macd", `{"action":"close_all"}`)
	assertEq(t, code, http.StatusBadRequest, "non-CRA must be 400")
	assertTrue(t, strings.Contains(getString(resp, "detail", ""), "CRA"), "detail must name CRA gate")

	// 非 running → 409。
	seedManualConfig(t, "ma-gate-stopped", 0, "cra_spot", "stopped", nil)
	code, resp = postManual(t, r, "ma-gate-stopped", `{"action":"close_all"}`)
	assertEq(t, code, http.StatusConflict, "stopped must be 409")

	// CRA 别名类型（martin_trend → cra_spot 引擎）放行类型闸，但引擎无实例 → 409。
	seedManualConfig(t, "ma-gate-alias", 0, "martin_trend", "running", nil)
	code, _ = postManual(t, r, "ma-gate-alias", `{"action":"toggle_add_position"}`)
	assertEq(t, code, http.StatusConflict, "engine-less running config must be 409")

	// 未知动作 / 参数形校验 → 400。
	seedManualConfig(t, "ma-gate-shape", 0, "cra_spot", "running", nil)
	code, _ = postManual(t, r, "ma-gate-shape", `{"action":"panic_sell"}`)
	assertEq(t, code, http.StatusBadRequest, "unknown action must be 400")
	code, _ = postManual(t, r, "ma-gate-shape", `{"action":"add_position"}`)
	assertEq(t, code, http.StatusBadRequest, "add_position without amount must be 400")
	code, _ = postManual(t, r, "ma-gate-shape", `{"action":"add_position","amount":-5}`)
	assertEq(t, code, http.StatusBadRequest, "negative amount must be 400")
	code, _ = postManual(t, r, "ma-gate-shape", `{"action":"reduce_position","qty":0.1,"ratio":0.5}`)
	assertEq(t, code, http.StatusBadRequest, "qty+ratio together must be 400")
	code, _ = postManual(t, r, "ma-gate-shape", `{"action":"reduce_position","ratio":1.5}`)
	assertEq(t, code, http.StatusBadRequest, "ratio>=1 must be 400")
	code, _ = postManual(t, r, "ma-gate-shape", `{"action":"reduce_position"}`)
	assertEq(t, code, http.StatusBadRequest, "reduce without qty/ratio must be 400")

	// 不存在的配置 → 404。
	code, _ = postManual(t, r, "ma-ghost", `{"action":"close_all"}`)
	assertEq(t, code, http.StatusNotFound, "ghost config must be 404")
}

// TestCRAManualActionOwnership 非属主（注入 uid 999，配置属主 42）→ 403；
// 属主本人 → 通过属主闸（落在引擎态 409/成功，而非 403）。
func TestCRAManualActionOwnership(t *testing.T) {
	seedManualConfig(t, "ma-own", 42, "cra_spot", "running", nil)

	rOther := setupManualRouter(999)
	code, _ := postManual(t, rOther, "ma-own", `{"action":"toggle_add_position"}`)
	assertEq(t, code, http.StatusForbidden, "non-owner must be 403")

	rOwner := setupManualRouter(42)
	code, _ = postManual(t, rOwner, "ma-own", `{"action":"toggle_add_position"}`)
	assertTrue(t, code != http.StatusForbidden, "owner must pass ownership gate")
}

// TestCRAManualActionEngineConsumption HTTP → 引擎端到端：真实注册 CRA 实例
// 进引擎、建仓，四个动作经端点消费——开关翻转如实透出、补仓出信号、
// 减仓/清仓置在途标记、detail 透出后端口径。
func TestCRAManualActionEngineConsumption(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("cra_spot", func() strategy.Strategy {
		return cra.NewCRASpotStrategy("cra_spot", "BTCUSDT")
	})
	seedManualConfig(t, "ma-e2e", 0, "cra_spot", "running", nil)

	// 真实启动链路进引擎。
	item := store.GetStrategyConfig("ma-e2e")
	if err := startStrategyInEngine("ma-e2e", item); err != nil {
		t.Fatalf("startStrategyInEngine: %v", err)
	}
	t.Cleanup(func() { stopStrategyInEngine("ma-e2e") })

	r := setupManualRouter(-1)

	// 空仓时 close_all → 409（引擎态校验透出）。
	code, resp := postManual(t, r, "ma-e2e", `{"action":"close_all"}`)
	assertEq(t, code, http.StatusConflict, "close_all on flat must be 409")
	assertTrue(t, strings.Contains(getString(resp, "detail", ""), "无持仓"), "detail must explain")

	// toggle_add_position → 200，引擎态如实翻转。
	code, resp = postManual(t, r, "ma-e2e", `{"action":"toggle_add_position","enabled":false}`)
	assertEq(t, code, http.StatusOK, "toggle must be 200")
	result, _ := resp["result"].(map[string]any)
	assertTrue(t, result["add_position_enabled"] == false, "result must carry add_position_enabled=false")
	assertTrue(t, getString(resp, "detail", "") != "", "detail message must surface")
	if st, ok := eng.RuntimeStatus("ma-e2e"); !ok || st["add_position_enabled"] != false {
		t.Fatalf("engine state must flip: %v", st)
	}

	// 建仓（引擎内策略直接喂 K 线+成交回报，与 OMS 路由同形态）。
	s := eng.Get("ma-e2e")
	sig, err := s.OnBar(modelBar(100), nil)
	if err != nil || sig == nil {
		t.Fatalf("first order signal missing: %v %v", sig, err)
	}
	if _, err := s.OnOrderUpdate(modelOrderFill("BTCUSDT", sig.Qty, 100, "sig:ma-e2e:1"), nil); err != nil {
		t.Fatalf("fill: %v", err)
	}

	// 一键补仓 → 200 + 引擎出 LONG 信号（qty=金额/价；端点 detail 透出后端口径）。
	code, resp = postManual(t, r, "ma-e2e", `{"action":"add_position","amount":50}`)
	assertEq(t, code, http.StatusOK, "add_position must be 200")
	result, _ = resp["result"].(map[string]any)
	assertTrue(t, result["qty"] == 0.5, "result qty must be 0.5 (50/100)")
	assertTrue(t, strings.Contains(getString(resp, "detail", ""), "补仓"), "detail must describe add")

	// 自定义减仓 → 200 + 在途标记 manual_reduce。
	// 先把手动补仓成交灌回（总量 1.5）。
	if _, err := s.OnOrderUpdate(modelOrderFill("BTCUSDT", 0.5, 100, "sig:ma-e2e:manual:2"), nil); err != nil {
		t.Fatalf("manual fill: %v", err)
	}
	code, _ = postManual(t, r, "ma-e2e", `{"action":"reduce_position","ratio":0.4}`)
	assertEq(t, code, http.StatusOK, "reduce_position must be 200")
	if st, _ := eng.RuntimeStatus("ma-e2e"); st["position_qty"] != 1.5 {
		t.Fatalf("position_qty before reduce fill = %v, want 1.5（信号发出≠成交）", st["position_qty"])
	}

	// 减仓在途 → 清仓被拒（在途平仓单互斥）→ 409。
	code, _ = postManual(t, r, "ma-e2e", `{"action":"close_all"}`)
	assertEq(t, code, http.StatusConflict, "close_all during pending reduce must be 409")

	// 减仓成交 → 核销（1.5×0.4=0.6，余 0.9）；随后清仓 → 200 + EntryPaused。
	if _, err := s.OnOrderUpdate(modelOrderSellFill("BTCUSDT", 0.6, 99, "sig:ma-e2e:manual:3"), nil); err != nil {
		t.Fatalf("reduce fill: %v", err)
	}
	if st, _ := eng.RuntimeStatus("ma-e2e"); st["position_qty"] != 0.9 {
		t.Fatalf("position_qty after reduce fill = %v, want 0.9", st["position_qty"])
	}
	code, _ = postManual(t, r, "ma-e2e", `{"action":"close_all"}`)
	assertEq(t, code, http.StatusOK, "close_all must be 200")
	st, _ := eng.RuntimeStatus("ma-e2e")
	assertTrue(t, st["entry_paused"] == true, "entry_paused must surface after close_all")

	// 清仓成交 → 空仓、策略保持 running。
	if _, err := s.OnOrderUpdate(modelOrderSellFill("BTCUSDT", 0.9, 101, "sig:ma-e2e:manual:4"), nil); err != nil {
		t.Fatalf("close fill: %v", err)
	}
	st, _ = eng.RuntimeStatus("ma-e2e")
	assertTrue(t, st["in_position"] == false && st["running"] == true,
		"must be flat and running after close fill")
}
