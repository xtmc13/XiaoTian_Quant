package handler

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── DCA 机器人 REST 测试 ────────────────────────────────────────
//
// fake DCAService 记录 StartBot/StopBot；路由组带 main 同款 AuthRequired
// 中间件，用 store.GenerateJWT 签发的真 token 过鉴权（沿用 grid 测试设施
// gridToken/gridDo，SECRET_KEY 已在 TestMain 的 InitDB 前注入）。

type fakeDCAService struct {
	mu         sync.Mutex
	running    map[string]bool
	startCalls []fakeStartCall
	stopCalls  []fakeStopCall
}

func newFakeDCAService() *fakeDCAService {
	return &fakeDCAService{running: map[string]bool{}}
}

func (f *fakeDCAService) StartBot(rec *store.DCABotRecord, currentPrice float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls = append(f.startCalls, fakeStartCall{id: rec.ID, price: currentPrice})
	f.running[rec.ID] = true
	return nil
}

func (f *fakeDCAService) StopBot(botID string, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls = append(f.stopCalls, fakeStopCall{id: botID, status: status})
	delete(f.running, botID)
	return nil
}

func (f *fakeDCAService) IsRunning(botID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[botID]
}

func (f *fakeDCAService) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.startCalls)
}

// setupDCARouter 注册带 AuthRequired 的 DCA 路由并注入 fake service。
func setupDCARouter(t *testing.T) (*gin.Engine, *fakeDCAService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	fake := newFakeDCAService()
	SetDCAService(fake)
	t.Cleanup(func() { SetDCAService(nil) })

	r := gin.New()
	g := r.Group("/api/dca-bots")
	g.Use(middleware.AuthRequired())
	{
		g.GET("/", DCABotList)
		g.POST("/", DCABotCreate)
		g.GET("/:id", DCABotGet)
		g.PUT("/:id", DCABotUpdate)
		g.DELETE("/:id", DCABotDelete)
		g.POST("/:id/start", DCABotStart)
		g.POST("/:id/stop", DCABotStop)
	}
	return r, fake
}

// setDCAPrice 把实时价换成恒定假价，测试结束恢复原生产价源。
func setDCAPrice(t *testing.T, v float64) {
	t.Helper()
	orig := dcaPriceSource
	SetDCAPriceSource(func(symbol string) float64 { return v })
	t.Cleanup(func() { dcaPriceSource = orig })
}

func validDCABody() map[string]any {
	return map[string]any{
		"name":             "test-dca",
		"symbol":           "btc/usdt",
		"quote_amount":     100.0,
		"interval_minutes": 60,
		"max_orders":       10,
		"period_budget":    1000.0,
		"take_profit_pct":  0.05,
		"stop_loss_pct":    0.10,
		"trailing_enabled": false,
	}
}

// 创建落库：user_id 写入、symbol 归一化、默认 paper、参数回显。
func TestDCABotCreatePersists(t *testing.T) {
	r, _ := setupDCARouter(t)
	token := gridToken(t, 71)

	w, created := gridDo(t, r, http.MethodPost, "/api/dca-bots/", validDCABody(), token)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created bot should carry id: %v", created)
	}
	if created["symbol"] != "BTCUSDT" {
		t.Fatalf("symbol should be normalized to BTCUSDT, got %v", created["symbol"])
	}
	if created["status"] != "stopped" {
		t.Fatalf("status should be stopped, got %v", created["status"])
	}
	if created["exchange"] != "paper" {
		t.Fatalf("exchange should default to paper, got %v", created["exchange"])
	}
	assertNumEq(t, created["quote_amount"], 100, "quote_amount")

	rec, err := dcaBotRepo.GetByID(id)
	if err != nil || rec == nil {
		t.Fatalf("bot %s should persist: %v %v", id, rec, err)
	}
	assertEq(t, int(rec.UserID), 71, "record user_id")
	assertEq(t, rec.IntervalMinutes, 60, "interval_minutes")
	if !rec.TrailingEnabled == false {
		t.Fatalf("trailing_enabled default false")
	}
}

// 创建参数校验：缺 symbol/quote_amount/interval 非法 → 400。
func TestDCABotCreateValidation(t *testing.T) {
	r, _ := setupDCARouter(t)
	token := gridToken(t, 71)

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"empty symbol", func(b map[string]any) { b["symbol"] = "  " }},
		{"zero quote", func(b map[string]any) { b["quote_amount"] = 0 }},
		{"zero interval", func(b map[string]any) { b["interval_minutes"] = 0 }},
		{"negative max_orders", func(b map[string]any) { b["max_orders"] = -1 }},
		{"tp >= 1", func(b map[string]any) { b["take_profit_pct"] = 1.5 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validDCABody()
			tc.mutate(body)
			w, resp := gridDo(t, r, http.MethodPost, "/api/dca-bots/", body, token)
			assertEq(t, w.Code, http.StatusBadRequest, "status code")
			assertErrBody(t, resp)
		})
	}
}

// 完整生命周期：创建 → 详情 → 启动（假价）→ 运行态 → 重复启动 409 →
// 运行中 PUT 409 → 停止 → 重复停止 409 → 停止后 PUT 200。
func TestDCABotLifecycle(t *testing.T) {
	r, fake := setupDCARouter(t)
	setDCAPrice(t, 105)
	token := gridToken(t, 71)

	w, created := gridDo(t, r, http.MethodPost, "/api/dca-bots/", validDCABody(), token)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	// 详情：空 orders 序列化为 []。
	w, detail := gridDo(t, r, http.MethodGet, "/api/dca-bots/"+id, nil, token)
	assertEq(t, w.Code, http.StatusOK, "detail status")
	if orders, ok := detail["orders"].([]any); !ok || len(orders) != 0 {
		t.Fatalf("orders should be empty array, got %v", detail["orders"])
	}

	// 启动：假价 105 → started。
	w, started := gridDo(t, r, http.MethodPost, "/api/dca-bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusOK, "start status")
	if started["started"] != true {
		t.Fatalf("start should return started=true: %v", started)
	}
	if fake.startCount() != 1 {
		t.Fatalf("StartBot should be called once, got %d", fake.startCount())
	}
	if !fake.IsRunning(id) {
		t.Fatal("bot should be running after start")
	}
	_, detail = gridDo(t, r, http.MethodGet, "/api/dca-bots/"+id, nil, token)
	if detail["is_running"] != true {
		t.Fatalf("detail should report is_running=true: %v", detail["is_running"])
	}

	// 重复启动 409。
	w, resp := gridDo(t, r, http.MethodPost, "/api/dca-bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusConflict, "re-start status")
	assertErrBody(t, resp)

	// 运行中修改 409。
	w, resp = gridDo(t, r, http.MethodPut, "/api/dca-bots/"+id, validDCABody(), token)
	assertEq(t, w.Code, http.StatusConflict, "update while running")
	assertErrBody(t, resp)

	// 停止。
	w, stopped := gridDo(t, r, http.MethodPost, "/api/dca-bots/"+id+"/stop", nil, token)
	assertEq(t, w.Code, http.StatusOK, "stop status")
	if stopped["stopped"] != true {
		t.Fatalf("stop should return stopped=true: %v", stopped)
	}

	// 重复停止 409。
	w, resp = gridDo(t, r, http.MethodPost, "/api/dca-bots/"+id+"/stop", nil, token)
	assertEq(t, w.Code, http.StatusConflict, "re-stop status")
	assertErrBody(t, resp)

	// 停止后可改参数。
	update := validDCABody()
	update["quote_amount"] = 200.0
	w, updated := gridDo(t, r, http.MethodPut, "/api/dca-bots/"+id, update, token)
	assertEq(t, w.Code, http.StatusOK, "update after stop")
	assertNumEq(t, updated["quote_amount"], 200, "updated quote_amount")
}

// 启动守卫：行情未就绪 503，不触发 StartBot。
func TestDCABotStartPriceGuard(t *testing.T) {
	r, fake := setupDCARouter(t)
	token := gridToken(t, 71)

	w, created := gridDo(t, r, http.MethodPost, "/api/dca-bots/", validDCABody(), token)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	setDCAPrice(t, 0)
	w, resp := gridDo(t, r, http.MethodPost, "/api/dca-bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusServiceUnavailable, "no-price status")
	assertErrBody(t, resp)
	if fake.startCount() != 0 {
		t.Fatalf("StartBot must not be called on price guard, got %d", fake.startCount())
	}
}

// 越权 403：用户 B 对用户 A 的 bot 详情/改/删/启/停全部 403；
// 列表对 B 不可见；不存在的 id 404。
func TestDCABotOwnership(t *testing.T) {
	r, _ := setupDCARouter(t)
	setDCAPrice(t, 105)
	tokenA := gridToken(t, 72)
	tokenB := gridToken(t, 73)

	w, created := gridDo(t, r, http.MethodPost, "/api/dca-bots/", validDCABody(), tokenA)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/dca-bots/" + id},
		{http.MethodPut, "/api/dca-bots/" + id},
		{http.MethodDelete, "/api/dca-bots/" + id},
		{http.MethodPost, "/api/dca-bots/" + id + "/start"},
		{http.MethodPost, "/api/dca-bots/" + id + "/stop"},
	}
	for _, p := range paths {
		w, resp := gridDo(t, r, p.method, p.path, validDCABody(), tokenB)
		assertEq(t, w.Code, http.StatusForbidden, fmt.Sprintf("%s %s by non-owner", p.method, p.path))
		if msg, _ := resp["detail"].(string); msg == "" {
			t.Fatalf("403 body should carry detail, got %v", resp)
		}
	}

	// 非属主删除未生效。
	if rec, err := dcaBotRepo.GetByID(id); err != nil || rec == nil {
		t.Fatalf("bot %s should still exist after non-owner delete: %v %v", id, rec, err)
	}

	// 列表隔离：B 看不到 A 的 bot。
	_, listed := gridDo(t, r, http.MethodGet, "/api/dca-bots/", nil, tokenB)
	if bots, ok := listed["bots"].([]any); !ok {
		t.Fatalf("list response missing bots array: %v", listed)
	} else {
		for _, item := range bots {
			if m, ok := item.(map[string]any); ok && m["id"] == id {
				t.Fatalf("non-owner list leaked bot %s", id)
			}
		}
	}

	// A 自己可正常访问。
	w, _ = gridDo(t, r, http.MethodGet, "/api/dca-bots/"+id, nil, tokenA)
	assertEq(t, w.Code, http.StatusOK, "owner get")

	// 不存在的 id → 404。
	w, resp := gridDo(t, r, http.MethodGet, "/api/dca-bots/no-such-bot", nil, tokenA)
	assertEq(t, w.Code, http.StatusNotFound, "not found status")
	assertErrBody(t, resp)
}

// 未带 token → 401。
func TestDCABotUnauthorized(t *testing.T) {
	r, _ := setupDCARouter(t)
	w, _ := gridDo(t, r, http.MethodGet, "/api/dca-bots/", nil, "")
	assertEq(t, w.Code, http.StatusUnauthorized, "unauthenticated list")
}

// 删除运行中机器人：先 StopBot 再删库。
func TestDCABotDeleteStopsRunning(t *testing.T) {
	r, fake := setupDCARouter(t)
	setDCAPrice(t, 105)
	token := gridToken(t, 74)

	w, created := gridDo(t, r, http.MethodPost, "/api/dca-bots/", validDCABody(), token)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	w, _ = gridDo(t, r, http.MethodPost, "/api/dca-bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusOK, "start status")

	w, deleted := gridDo(t, r, http.MethodDelete, "/api/dca-bots/"+id, nil, token)
	assertEq(t, w.Code, http.StatusOK, "delete status")
	if deleted["deleted"] != true {
		t.Fatalf("delete should return deleted=true: %v", deleted)
	}
	if len(fake.stopCalls) != 1 {
		t.Fatalf("StopBot should be called before delete, got %d", len(fake.stopCalls))
	}
	if rec, err := dcaBotRepo.GetByID(id); err != nil || rec != nil {
		t.Fatalf("bot %s should be deleted: %v %v", id, rec, err)
	}
}

// 实盘安全闸：live 交易所未解锁时启动 403。
func TestDCABotLiveGate(t *testing.T) {
	r, _ := setupDCARouter(t)
	setDCAPrice(t, 105)
	token := gridToken(t, 75)

	body := validDCABody()
	body["exchange"] = "binance"
	w, created := gridDo(t, r, http.MethodPost, "/api/dca-bots/", body, token)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	// 测试环境默认 LIVE_TRADING_ENABLED 未开 → 启动被安全闸拦下。
	w, resp := gridDo(t, r, http.MethodPost, "/api/dca-bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusForbidden, "live gate status")
	if msg, _ := resp["message"].(string); !strings.Contains(msg, "实盘交易未启用") {
		t.Fatalf("message should mention live gate, got %v", resp["message"])
	}
}
