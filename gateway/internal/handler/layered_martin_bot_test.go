package handler

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 分层马丁格尔机器人 REST 测试 ────────────────────────────────

type fakeLMService struct {
	mu         sync.Mutex
	running    map[string]bool
	startCalls []fakeLMStartCall
	stopCalls  []fakeStopCall
}

type fakeLMStartCall struct {
	id         string
	price      float64
	groupCount int
}

func newFakeLMService() *fakeLMService {
	return &fakeLMService{running: map[string]bool{}}
}

func (f *fakeLMService) StartBot(rec *store.LayeredMartinBotRecord, groups []*store.LayeredMartinGroupRecord, currentPrice float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls = append(f.startCalls, fakeLMStartCall{id: rec.ID, price: currentPrice, groupCount: len(groups)})
	f.running[rec.ID] = true
	return nil
}

func (f *fakeLMService) StopBot(botID string, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls = append(f.stopCalls, fakeStopCall{id: botID, status: status})
	delete(f.running, botID)
	return nil
}

func (f *fakeLMService) IsRunning(botID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[botID]
}

// setupLMRouter 注册带 AuthRequired 的分层马丁路由并注入 fake service。
func setupLMRouter(t *testing.T) (*gin.Engine, *fakeLMService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	fake := newFakeLMService()
	SetLayeredMartinService(fake)
	t.Cleanup(func() { SetLayeredMartinService(nil) })

	r := gin.New()
	g := r.Group("/api/layered-martin-bots")
	g.Use(middleware.AuthRequired())
	{
		g.GET("/", LayeredMartinBotList)
		g.POST("/", LayeredMartinBotCreate)
		g.GET("/:id", LayeredMartinBotGet)
		g.PUT("/:id", LayeredMartinBotUpdate)
		g.DELETE("/:id", LayeredMartinBotDelete)
		g.POST("/:id/start", LayeredMartinBotStart)
		g.POST("/:id/stop", LayeredMartinBotStop)
	}
	return r, fake
}

// setLMPrice 把实时价换成恒定假价，测试结束恢复原生产价源。
func setLMPrice(t *testing.T, v float64) {
	t.Helper()
	orig := lmPriceSource
	SetLMPriceSource(func(symbol string) float64 { return v })
	t.Cleanup(func() { lmPriceSource = orig })
}

func validLMBody() map[string]any {
	return map[string]any{
		"name":                "test-lm",
		"symbol":              "btc/usdt",
		"price_deviation_pct": 0.03,
		"take_profit_pct":     0.05,
		"stop_loss_pct":       0.10,
		"trailing_enabled":    true,
		"groups": []map[string]any{
			{"quote_amount": 100.0, "multiplier": 2.0, "max_layers": 5, "budget_cap": 1000.0},
			{"quote_amount": 50.0, "multiplier": 3.0, "max_layers": 3, "budget_cap": 0.0},
		},
	}
}

// 创建落库：主表 + 每组参数落库，user_id 写入、symbol 归一化。
func TestLMBotCreatePersists(t *testing.T) {
	r, _ := setupLMRouter(t)
	token := gridToken(t, 81)

	w, created := gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/", validLMBody(), token)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created bot should carry id: %v", created)
	}
	if created["symbol"] != "BTCUSDT" {
		t.Fatalf("symbol should be normalized, got %v", created["symbol"])
	}
	if created["exchange"] != "paper" {
		t.Fatalf("exchange should default to paper, got %v", created["exchange"])
	}
	if created["trailing_enabled"] != true {
		t.Fatalf("trailing_enabled should round-trip true")
	}
	groups, ok := created["groups"].([]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("created should carry 2 groups, got %v", created["groups"])
	}
	g0 := groups[0].(map[string]any)
	assertNumEq(t, g0["quote_amount"], 100, "group 0 quote_amount")
	assertNumEq(t, g0["max_layers"], 5, "group 0 max_layers")

	rec, err := lmBotRepo.GetByID(id)
	if err != nil || rec == nil {
		t.Fatalf("bot %s should persist: %v %v", id, rec, err)
	}
	assertEq(t, int(rec.UserID), 81, "record user_id")
	persisted, err := lmBotRepo.GetGroups(id)
	if err != nil || len(persisted) != 2 {
		t.Fatalf("groups should persist: %v %v", persisted, err)
	}
	if persisted[0].QuoteAmount != 100 || persisted[1].Multiplier != 3 {
		t.Fatalf("group params mismatch: %+v", persisted)
	}
}

// 创建参数校验：分组为空/参数非法 → 400。
func TestLMBotCreateValidation(t *testing.T) {
	r, _ := setupLMRouter(t)
	token := gridToken(t, 81)

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"empty symbol", func(b map[string]any) { b["symbol"] = " " }},
		{"bad deviation", func(b map[string]any) { b["price_deviation_pct"] = 1.5 }},
		{"no groups", func(b map[string]any) { b["groups"] = []map[string]any{} }},
		{"bad group quote", func(b map[string]any) {
			b["groups"] = []map[string]any{{"quote_amount": 0, "multiplier": 2, "max_layers": 3}}
		}},
		{"bad group layers", func(b map[string]any) {
			b["groups"] = []map[string]any{{"quote_amount": 100, "multiplier": 2, "max_layers": 0}}
		}},
		{"bad multiplier", func(b map[string]any) {
			b["groups"] = []map[string]any{{"quote_amount": 100, "multiplier": 0.5, "max_layers": 3}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validLMBody()
			tc.mutate(body)
			w, resp := gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/", body, token)
			assertEq(t, w.Code, http.StatusBadRequest, "status code")
			assertErrBody(t, resp)
		})
	}
}

// 完整生命周期：创建 → 详情（groups+orders）→ 启动 → 运行态 → 409 → 停止。
func TestLMBotLifecycle(t *testing.T) {
	r, fake := setupLMRouter(t)
	setLMPrice(t, 105)
	token := gridToken(t, 81)

	w, created := gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/", validLMBody(), token)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	// 详情：groups + 空 orders。
	w, detail := gridDo(t, r, http.MethodGet, "/api/layered-martin-bots/"+id, nil, token)
	assertEq(t, w.Code, http.StatusOK, "detail status")
	if groups, ok := detail["groups"].([]any); !ok || len(groups) != 2 {
		t.Fatalf("detail should carry 2 groups, got %v", detail["groups"])
	}
	if orders, ok := detail["orders"].([]any); !ok || len(orders) != 0 {
		t.Fatalf("orders should be empty array, got %v", detail["orders"])
	}

	// 启动：fake 收到当前价与 2 个组。
	w, started := gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusOK, "start status")
	if started["started"] != true {
		t.Fatalf("start should return started=true: %v", started)
	}
	fake.mu.Lock()
	if len(fake.startCalls) != 1 || fake.startCalls[0].price != 105 || fake.startCalls[0].groupCount != 2 {
		t.Fatalf("StartBot call mismatch: %+v", fake.startCalls)
	}
	fake.mu.Unlock()
	if !fake.IsRunning(id) {
		t.Fatal("bot should be running after start")
	}

	// 重复启动 409。
	w, resp := gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusConflict, "re-start status")
	assertErrBody(t, resp)

	// 运行中修改 409。
	w, resp = gridDo(t, r, http.MethodPut, "/api/layered-martin-bots/"+id, validLMBody(), token)
	assertEq(t, w.Code, http.StatusConflict, "update while running")
	assertErrBody(t, resp)

	// 停止 + 重复停止 409。
	w, stopped := gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/"+id+"/stop", nil, token)
	assertEq(t, w.Code, http.StatusOK, "stop status")
	if stopped["stopped"] != true {
		t.Fatalf("stop should return stopped=true: %v", stopped)
	}
	w, resp = gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/"+id+"/stop", nil, token)
	assertEq(t, w.Code, http.StatusConflict, "re-stop status")
	assertErrBody(t, resp)

	// 停止后可全量替换分组。
	update := validLMBody()
	update["groups"] = []map[string]any{
		{"quote_amount": 200.0, "multiplier": 2.0, "max_layers": 4, "budget_cap": 500.0},
	}
	w, updated := gridDo(t, r, http.MethodPut, "/api/layered-martin-bots/"+id, update, token)
	assertEq(t, w.Code, http.StatusOK, "update after stop")
	groups, ok := updated["groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("updated should carry 1 group, got %v", updated["groups"])
	}
	persisted, err := lmBotRepo.GetGroups(id)
	if err != nil || len(persisted) != 1 || persisted[0].QuoteAmount != 200 {
		t.Fatalf("groups should be replaced: %+v %v", persisted, err)
	}
}

// 越权 403：非属主对详情/改/删/启/停全部 403，列表不可见。
func TestLMBotOwnership(t *testing.T) {
	r, _ := setupLMRouter(t)
	setLMPrice(t, 105)
	tokenA := gridToken(t, 82)
	tokenB := gridToken(t, 83)

	w, created := gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/", validLMBody(), tokenA)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/layered-martin-bots/" + id},
		{http.MethodPut, "/api/layered-martin-bots/" + id},
		{http.MethodDelete, "/api/layered-martin-bots/" + id},
		{http.MethodPost, "/api/layered-martin-bots/" + id + "/start"},
		{http.MethodPost, "/api/layered-martin-bots/" + id + "/stop"},
	}
	for _, p := range paths {
		w, resp := gridDo(t, r, p.method, p.path, validLMBody(), tokenB)
		assertEq(t, w.Code, http.StatusForbidden, fmt.Sprintf("%s %s by non-owner", p.method, p.path))
		if msg, _ := resp["detail"].(string); msg == "" {
			t.Fatalf("403 body should carry detail, got %v", resp)
		}
	}

	if rec, err := lmBotRepo.GetByID(id); err != nil || rec == nil {
		t.Fatalf("bot %s should still exist after non-owner delete: %v %v", id, rec, err)
	}

	// 列表隔离：B 看不到 A 的 bot。
	_, listed := gridDo(t, r, http.MethodGet, "/api/layered-martin-bots/", nil, tokenB)
	if bots, ok := listed["bots"].([]any); ok {
		for _, item := range bots {
			if m, ok := item.(map[string]any); ok && m["id"] == id {
				t.Fatalf("non-owner list leaked bot %s", id)
			}
		}
	}

	// 不存在的 id → 404。
	w, resp := gridDo(t, r, http.MethodGet, "/api/layered-martin-bots/no-such-bot", nil, tokenA)
	assertEq(t, w.Code, http.StatusNotFound, "not found status")
	assertErrBody(t, resp)
}

// 未带 token → 401；行情未就绪启动 503；实盘安全闸 403。
func TestLMBotGuards(t *testing.T) {
	r, _ := setupLMRouter(t)
	token := gridToken(t, 84)

	w, _ := gridDo(t, r, http.MethodGet, "/api/layered-martin-bots/", nil, "")
	assertEq(t, w.Code, http.StatusUnauthorized, "unauthenticated list")

	w, created := gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/", validLMBody(), token)
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	setLMPrice(t, 0)
	w, resp := gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusServiceUnavailable, "no-price status")
	assertErrBody(t, resp)

	// 实盘未解锁 → 启动 403。
	setLMPrice(t, 105)
	body := validLMBody()
	body["exchange"] = "binance"
	w, created2 := gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/", body, token)
	assertEq(t, w.Code, http.StatusOK, "create live status")
	liveID, _ := created2["id"].(string)
	w, resp = gridDo(t, r, http.MethodPost, "/api/layered-martin-bots/"+liveID+"/start", nil, token)
	assertEq(t, w.Code, http.StatusForbidden, "live gate status")
}
