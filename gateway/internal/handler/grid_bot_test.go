package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 测试设施 ────────────────────────────────────────────────────
//
// fake GridService 记录 StartBot/StopBot 调用与运行中集合；
// 路由组带 main 同款 AuthRequired 中间件，用 store.GenerateJWT 签发的
// 真 token 过鉴权（SECRET_KEY 已在 TestMain 的 InitDB 前注入）。

type fakeStartCall struct {
	id    string
	price float64
}

type fakeStopCall struct {
	id     string
	status string
}

type fakeGridService struct {
	mu         sync.Mutex
	running    map[string]bool
	startCalls []fakeStartCall
	stopCalls  []fakeStopCall
}

func newFakeGridService() *fakeGridService {
	return &fakeGridService{running: map[string]bool{}}
}

func (f *fakeGridService) StartBot(rec *store.GridBotRecord, currentPrice float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls = append(f.startCalls, fakeStartCall{id: rec.ID, price: currentPrice})
	f.running[rec.ID] = true
	return nil
}

func (f *fakeGridService) StopBot(botID string, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls = append(f.stopCalls, fakeStopCall{id: botID, status: status})
	delete(f.running, botID)
	return nil
}

func (f *fakeGridService) IsRunning(botID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[botID]
}

func (f *fakeGridService) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.startCalls)
}

func (f *fakeGridService) lastStart() fakeStartCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCalls[len(f.startCalls)-1]
}

func (f *fakeGridService) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.stopCalls)
}

func (f *fakeGridService) lastStop() fakeStopCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopCalls[len(f.stopCalls)-1]
}

// setupGridRouter 注册带 AuthRequired 的网格路由并注入 fake service。
func setupGridRouter(t *testing.T) (*gin.Engine, *fakeGridService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	fake := newFakeGridService()
	SetGridService(fake)
	t.Cleanup(func() { SetGridService(nil) })

	r := gin.New()
	g := r.Group("/api/grid")
	g.Use(middleware.AuthRequired())
	{
		g.GET("/bots", GridBotList)
		g.POST("/bots", GridBotCreate)
		g.GET("/bots/:id", GridBotGet)
		g.PUT("/bots/:id", GridBotUpdate)
		g.DELETE("/bots/:id", GridBotDelete)
		g.POST("/bots/:id/start", GridBotStart)
		g.POST("/bots/:id/stop", GridBotStop)
	}
	return r, fake
}

// setGridPrice 把实时价换成恒定假价，测试结束恢复原生产价源。
func setGridPrice(t *testing.T, v float64) {
	t.Helper()
	orig := gridPriceSource
	SetGridPriceSource(func(symbol string) float64 { return v })
	t.Cleanup(func() { gridPriceSource = orig })
}

func gridToken(t *testing.T, uid int) string {
	t.Helper()
	tok, err := store.GenerateJWT(uid, "gridtester", "user", 1)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}
	return tok
}

// gridDo 发 JSON 请求并解析响应体；token 为空则不带 Authorization。
func gridDo(t *testing.T, r *gin.Engine, method, path string, body any, token string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	parsed := map[string]any{}
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("unmarshal response %q: %v", w.Body.String(), err)
		}
	}
	return w, parsed
}

func validGridBody() map[string]any {
	return map[string]any{
		"name":        "test-grid",
		"symbol":      "btc/usdt",
		"lower_price": 100.0,
		"upper_price": 110.0,
		"grid_count":  10,
		"investment":  1000.0,
	}
}

func gridCreateBot(t *testing.T, r *gin.Engine, token string, body map[string]any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	return gridDo(t, r, http.MethodPost, "/api/grid/bots", body, token)
}

// gridFindInList 在列表响应中按 id 查找条目，返回 (找到, is_running)。
func gridFindInList(t *testing.T, listed map[string]any, id string) (bool, bool) {
	t.Helper()
	bots, ok := listed["bots"].([]any)
	if !ok {
		t.Fatalf("list response missing bots array: %v", listed)
	}
	for _, item := range bots {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["id"] == id {
			return true, m["is_running"] == true
		}
	}
	return false, false
}

func assertNumEq(t *testing.T, v any, want float64, msg string) {
	t.Helper()
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("%s: value %v is not a number", msg, v)
	}
	if f != want {
		t.Fatalf("%s: got %v, want %v", msg, f, want)
	}
}

func assertErrBody(t *testing.T, body map[string]any) {
	t.Helper()
	if body["success"] != false {
		t.Fatalf("error body should have success=false, got %v", body["success"])
	}
	if msg, _ := body["message"].(string); msg == "" {
		t.Fatalf("error body should carry non-empty message: %v", body)
	}
}

// ── Tests ───────────────────────────────────────────────────────

// 创建参数校验：lower>=upper、grid_count 越界、investment<=0、symbol 空 → 400。
func TestGridBotCreateValidation(t *testing.T) {
	r, fake := setupGridRouter(t)
	token := gridToken(t, 1)

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"lower>=upper", func(b map[string]any) { b["lower_price"] = 110.0; b["upper_price"] = 100.0 }},
		{"grid_count too small", func(b map[string]any) { b["grid_count"] = 1 }},
		{"grid_count too large", func(b map[string]any) { b["grid_count"] = 201 }},
		{"investment not positive", func(b map[string]any) { b["investment"] = 0.0 }},
		{"empty symbol", func(b map[string]any) { b["symbol"] = "  " }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validGridBody()
			tc.mutate(body)
			w, resp := gridCreateBot(t, r, token, body)
			assertEq(t, w.Code, http.StatusBadRequest, "status code")
			assertErrBody(t, resp)
		})
	}
	if fake.startCount() != 0 {
		t.Fatalf("no start expected during validation, got %d", fake.startCount())
	}
}

// 完整生命周期：创建 → 详情 → 启动（假价 105）→ 运行态列表 →
// 重复启动 409 / 运行中 PUT 409 → 停止 → 重复停止 409。
func TestGridBotLifecycle(t *testing.T) {
	r, fake := setupGridRouter(t)
	setGridPrice(t, 105)
	token := gridToken(t, 1)

	// 创建：symbol 归一化、fee_rate 默认 0.001、status=stopped。
	w, created := gridCreateBot(t, r, token, validGridBody())
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
	if created["is_running"] != false {
		t.Fatalf("fresh bot should not be running: %v", created["is_running"])
	}
	assertNumEq(t, created["fee_rate"], 0.001, "default fee_rate")
	assertNumEq(t, created["lower_price"], 100, "lower_price")
	assertNumEq(t, created["upper_price"], 110, "upper_price")
	assertNumEq(t, created["grid_count"], 10, "grid_count")
	assertNumEq(t, created["investment"], 1000, "investment")

	rec, err := gridBotRepo.GetByID(id)
	if err != nil || rec == nil {
		t.Fatalf("bot %s should persist: %v %v", id, rec, err)
	}
	assertEq(t, int(rec.UserID), 1, "record user_id")

	// 详情：空 trades/snapshots 序列化为 []，未运行 open_orders=0。
	w, detail := gridDo(t, r, http.MethodGet, "/api/grid/bots/"+id, nil, token)
	assertEq(t, w.Code, http.StatusOK, "detail status")
	if detail["id"] != id {
		t.Fatalf("detail id mismatch: %v", detail["id"])
	}
	assertNumEq(t, detail["open_orders"], 0, "open_orders when stopped")
	if trades, ok := detail["trades"].([]any); !ok || len(trades) != 0 {
		t.Fatalf("trades should be empty array, got %v", detail["trades"])
	}
	if snaps, ok := detail["snapshots"].([]any); !ok || len(snaps) != 0 {
		t.Fatalf("snapshots should be empty array, got %v", detail["snapshots"])
	}

	// 列表：能找到本条，未运行（库为全包共享，不断言总条数）。
	w, listed := gridDo(t, r, http.MethodGet, "/api/grid/bots", nil, token)
	assertEq(t, w.Code, http.StatusOK, "list status")
	found, running := gridFindInList(t, listed, id)
	if !found {
		t.Fatalf("list should contain bot %s: %v", id, listed["bots"])
	}
	if running {
		t.Fatalf("listed bot should not be running: %v", listed["bots"])
	}

	// 启动：假价 105 在 [100,110] 内 → started，StartBot 收到 currentPrice=105。
	w, started := gridDo(t, r, http.MethodPost, "/api/grid/bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusOK, "start status")
	if started["started"] != true {
		t.Fatalf("start should return started=true: %v", started)
	}
	if fake.startCount() != 1 {
		t.Fatalf("StartBot should be called once, got %d", fake.startCount())
	}
	call := fake.lastStart()
	if call.id != id {
		t.Fatalf("StartBot bot id: got %s, want %s", call.id, id)
	}
	assertNumEq(t, call.price, 105, "StartBot currentPrice")
	if !fake.IsRunning(id) {
		t.Fatal("bot should be running after start")
	}

	// initial_equity=investment 已落库。
	rec, err = gridBotRepo.GetByID(id)
	if err != nil || rec == nil {
		t.Fatalf("bot %s should persist: %v %v", id, rec, err)
	}
	assertNumEq(t, rec.InitialEquity, 1000, "persisted initial_equity")

	// 运行中：详情 is_running=true，列表 is_running=true。
	_, detail = gridDo(t, r, http.MethodGet, "/api/grid/bots/"+id, nil, token)
	if detail["is_running"] != true {
		t.Fatalf("detail should report is_running=true: %v", detail["is_running"])
	}
	_, listed = gridDo(t, r, http.MethodGet, "/api/grid/bots", nil, token)
	found, running = gridFindInList(t, listed, id)
	if !found || !running {
		t.Fatalf("listed bot %s should be running (found=%v): %v", id, found, listed["bots"])
	}

	// 运行中重复启动 → 409。
	w, resp := gridDo(t, r, http.MethodPost, "/api/grid/bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusConflict, "re-start status")
	assertErrBody(t, resp)

	// 运行中修改参数 → 409。
	w, resp = gridDo(t, r, http.MethodPut, "/api/grid/bots/"+id, validGridBody(), token)
	assertEq(t, w.Code, http.StatusConflict, "update while running")
	assertErrBody(t, resp)

	// 停止 → stopped:true，StopBot(id,"stopped") 被调用。
	w, stopped := gridDo(t, r, http.MethodPost, "/api/grid/bots/"+id+"/stop", nil, token)
	assertEq(t, w.Code, http.StatusOK, "stop status")
	if stopped["stopped"] != true {
		t.Fatalf("stop should return stopped=true: %v", stopped)
	}
	if fake.stopCount() != 1 {
		t.Fatalf("StopBot should be called once, got %d", fake.stopCount())
	}
	scall := fake.lastStop()
	if scall.id != id || scall.status != "stopped" {
		t.Fatalf("StopBot call: got %+v, want {%s stopped}", scall, id)
	}
	if fake.IsRunning(id) {
		t.Fatal("bot should not be running after stop")
	}

	// 重复停止 → 409。
	w, resp = gridDo(t, r, http.MethodPost, "/api/grid/bots/"+id+"/stop", nil, token)
	assertEq(t, w.Code, http.StatusConflict, "re-stop status")
	assertErrBody(t, resp)

	// 停止后可改参数 → 200 且新值生效。
	update := validGridBody()
	update["upper_price"] = 120.0
	update["grid_count"] = 20
	w, updated := gridDo(t, r, http.MethodPut, "/api/grid/bots/"+id, update, token)
	assertEq(t, w.Code, http.StatusOK, "update after stop")
	assertNumEq(t, updated["upper_price"], 120, "updated upper_price")
	assertNumEq(t, updated["grid_count"], 20, "updated grid_count")
}

// 启动价守卫：行情未就绪 503；当前价在区间外 400；均不触发 StartBot。
func TestGridBotStartPriceGuards(t *testing.T) {
	r, fake := setupGridRouter(t)
	token := gridToken(t, 1)

	w, created := gridCreateBot(t, r, token, validGridBody())
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	setGridPrice(t, 0)
	w, resp := gridDo(t, r, http.MethodPost, "/api/grid/bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusServiceUnavailable, "no-price status")
	assertErrBody(t, resp)
	if msg, _ := resp["message"].(string); !strings.Contains(msg, "行情未就绪") {
		t.Fatalf("message should mention 行情未就绪, got %v", resp["message"])
	}

	// 区间外低价：重建价源（t.Cleanup 会按 LIFO 恢复）。
	setGridPrice(t, 95)
	w, resp = gridDo(t, r, http.MethodPost, "/api/grid/bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusBadRequest, "below-range status")
	assertErrBody(t, resp)

	setGridPrice(t, 120)
	w, resp = gridDo(t, r, http.MethodPost, "/api/grid/bots/"+id+"/start", nil, token)
	assertEq(t, w.Code, http.StatusBadRequest, "above-range status")
	assertErrBody(t, resp)

	if fake.startCount() != 0 {
		t.Fatalf("StartBot must not be called on price guards, got %d", fake.startCount())
	}
}

// 删除运行中机器人：先 StopBot 再删库，删后 GetByID 为空。
func TestGridBotDeleteStopsRunningBot(t *testing.T) {
	r, fake := setupGridRouter(t)
	token := gridToken(t, 1)

	w, created := gridCreateBot(t, r, token, validGridBody())
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)
	fake.running[id] = true

	w, deleted := gridDo(t, r, http.MethodDelete, "/api/grid/bots/"+id, nil, token)
	assertEq(t, w.Code, http.StatusOK, "delete status")
	if deleted["deleted"] != true {
		t.Fatalf("delete should return deleted=true: %v", deleted)
	}
	if fake.stopCount() != 1 {
		t.Fatalf("StopBot should be called before delete, got %d", fake.stopCount())
	}
	scall := fake.lastStop()
	if scall.id != id || scall.status != "stopped" {
		t.Fatalf("StopBot call: got %+v, want {%s stopped}", scall, id)
	}
	rec, err := gridBotRepo.GetByID(id)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if rec != nil {
		t.Fatalf("bot %s should be deleted", id)
	}
}

// 详情聚合：trades 最多 100 条、snapshots 最多 200 条一并返回；
// 运行中从 state_json 解析 open_orders。
func TestGridBotDetailAggregatesTradesAndSnapshots(t *testing.T) {
	r, fake := setupGridRouter(t)
	token := gridToken(t, 1)

	w, created := gridCreateBot(t, r, token, validGridBody())
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	for i := 0; i < 3; i++ {
		if err := gridBotRepo.InsertTrade(id, i, "buy", 100, 0.5, 50, 0.05, 0.1, int64(1700000000000+i)); err != nil {
			t.Fatalf("insert trade: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := gridBotRepo.InsertSnapshot(id, 1000, 105, 0.1, 2, int64(1700000001000+i)); err != nil {
			t.Fatalf("insert snapshot: %v", err)
		}
	}

	rec, err := gridBotRepo.GetByID(id)
	if err != nil || rec == nil {
		t.Fatalf("get bot: %v %v", rec, err)
	}
	rec.StateJSON = `{"orders":{"0":{"side":"buy"},"1":{"side":"sell"}}}`
	if err := gridBotRepo.Update(rec); err != nil {
		t.Fatalf("update state: %v", err)
	}

	// 未运行：open_orders=0，但 trades/snapshots 正常返回。
	w, detail := gridDo(t, r, http.MethodGet, "/api/grid/bots/"+id, nil, token)
	assertEq(t, w.Code, http.StatusOK, "detail status")
	assertNumEq(t, detail["open_orders"], 0, "open_orders when stopped")
	if trades, ok := detail["trades"].([]any); !ok || len(trades) != 3 {
		t.Fatalf("should return 3 trades, got %v", detail["trades"])
	}
	if snaps, ok := detail["snapshots"].([]any); !ok || len(snaps) != 2 {
		t.Fatalf("should return 2 snapshots, got %v", detail["snapshots"])
	}

	// 运行中：open_orders 从 state_json 解析为 2。
	fake.running[id] = true
	_, detail = gridDo(t, r, http.MethodGet, "/api/grid/bots/"+id, nil, token)
	assertNumEq(t, detail["open_orders"], 2, "open_orders when running")
}

// 列表按 user_id 隔离：用户 2 看不到用户 1 的机器人。
func TestGridBotListUserIsolation(t *testing.T) {
	r, _ := setupGridRouter(t)
	token1 := gridToken(t, 1)

	w, created := gridCreateBot(t, r, token1, validGridBody())
	assertEq(t, w.Code, http.StatusOK, "create status")
	id, _ := created["id"].(string)

	w, listed := gridDo(t, r, http.MethodGet, "/api/grid/bots", nil, token1)
	assertEq(t, w.Code, http.StatusOK, "owner list status")
	found, _ := gridFindInList(t, listed, id)
	if !found {
		t.Fatalf("owner should see own bot %s: %v", id, listed["bots"])
	}

	token2 := gridToken(t, 2)
	w, listed = gridDo(t, r, http.MethodGet, "/api/grid/bots", nil, token2)
	assertEq(t, w.Code, http.StatusOK, "other user list status")
	if bots, ok := listed["bots"].([]any); !ok || len(bots) != 0 {
		t.Fatalf("other user should see 0 bots, got %v", listed["bots"])
	}
}

// 未带 token 的请求被 AuthRequired 拦下 → 401（同 middleware 测试惯例）。
func TestGridBotUnauthorized(t *testing.T) {
	r, _ := setupGridRouter(t)

	w, _ := gridDo(t, r, http.MethodGet, "/api/grid/bots", nil, "")
	assertEq(t, w.Code, http.StatusUnauthorized, "unauthenticated list")

	w, _ = gridCreateBot(t, r, "", validGridBody())
	assertEq(t, w.Code, http.StatusUnauthorized, "unauthenticated create")
}

// 不存在的 id → 404，错误体带 success=false + message。
func TestGridBotNotFound(t *testing.T) {
	r, _ := setupGridRouter(t)
	token := gridToken(t, 1)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/grid/bots/no-such-bot"},
		{http.MethodPut, "/api/grid/bots/no-such-bot"},
		{http.MethodDelete, "/api/grid/bots/no-such-bot"},
		{http.MethodPost, "/api/grid/bots/no-such-bot/start"},
		{http.MethodPost, "/api/grid/bots/no-such-bot/stop"},
	} {
		w, resp := gridDo(t, r, tc.method, tc.path, validGridBody(), token)
		assertEq(t, w.Code, http.StatusNotFound, fmt.Sprintf("%s %s status", tc.method, tc.path))
		assertErrBody(t, resp)
	}
}
