package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/hyperopt"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

// ownToken 签发测试用 JWT（参考 gridToken 的模式，加 role 参数）。
// A3.3/A3.4 起 AuthRequired 会查库校验 token_version / is_active /
// must_change_password，因此先确保对应 uid 的用户存在且标志位合规。

// ── 资源级越权（ownership）测试 ─────────────────────────────────
//
// 场景：用户 A 创建资源，用户 B（同角色普通用户）尝试
// GET/PUT/DELETE/POST action 必须全部 403；B 的列表不得包含 A 的资源；
// admin 可以访问。覆盖网格机器人、策略配置、订单、AI 机器人、组合、
// 通知路由、protection 全局资源、hyperopt 任务。
//
// 测试用户（uid 9101/9102/9103）独立于 gridToken 的 1/2，避免互相干扰；
// token 用 store.GenerateJWT 真签，走 AuthRequired 全链路（含库校验）。

func ownToken(t *testing.T, uid int, username, role string) string {
	t.Helper()
	db := store.GetDB()
	if db == nil {
		t.Fatal("store db not initialized")
	}
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO xt_users (id, username, password_hash, nickname, email, role, token_version) VALUES (?, ?, ?, ?, ?, ?, 1)`,
		uid, username, store.HashPassword("own-pass-123"), "Ownership Tester", fmt.Sprintf("own%d@test.local", uid), role,
	); err != nil {
		t.Fatalf("ensure own test user: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE xt_users SET must_change_password=0, token_version=1, is_active=1, totp_enabled=0 WHERE id=?`, uid,
	); err != nil {
		t.Fatalf("reset own test user flags: %v", err)
	}
	tok, err := store.GenerateJWT(uid, username, role, 1)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}
	return tok
}

// setupOwnershipRouter 注册带 AuthRequired 的全部被测路由。
// 先用真实事件总线初始化策略引擎单例：后续 batch-delete 等会触达引擎的
// 断言才不会以 nil bus 抢先完成 sync.Once（strategy_test 依赖该单例）。
func setupOwnershipRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	strategy.GetEngine(event.NewEventBus(64, 1))
	r := gin.New()
	private := r.Group("/api")
	private.Use(middleware.AuthRequired())
	{
		// grid bots
		private.GET("/grid/bots", GridBotList)
		private.POST("/grid/bots", GridBotCreate)
		private.GET("/grid/bots/:id", GridBotGet)
		private.PUT("/grid/bots/:id", GridBotUpdate)
		private.DELETE("/grid/bots/:id", GridBotDelete)
		private.POST("/grid/bots/:id/start", GridBotStart)
		private.POST("/grid/bots/:id/stop", GridBotStop)

		// strategies
		private.GET("/strategies/configs", GetStrategyConfigs)
		private.POST("/strategies/configs", CreateStrategyConfig)
		private.GET("/strategies/configs/:id", GetStrategyConfig)
		private.PUT("/strategies/configs/:id", UpdateStrategyConfig)
		private.DELETE("/strategies/configs/:id", DeleteStrategyConfig)
		private.POST("/strategies/configs/:id/start", StartStrategyConfig)
		private.POST("/strategies/configs/:id/stop", StopStrategyConfig)
		private.POST("/strategies/configs/batch-delete", BatchDeleteConfigs)

		// orders
		private.GET("/orders", GetOrders)
		private.POST("/orders", PlaceOrder)
		private.DELETE("/orders/:order_id", CancelOrder)
		private.GET("/orders/history", OrderHistory)

		// ai bots
		private.GET("/ai-bots/instances", AIBotInstanceList)
		private.POST("/ai-bots/instances", AIBotInstanceCreate)
		private.GET("/ai-bots/instances/:id", AIBotInstanceGet)
		private.PUT("/ai-bots/instances/:id", AIBotInstanceUpdate)
		private.DELETE("/ai-bots/instances/:id", AIBotInstanceDelete)
		private.POST("/ai-bots/instances/:id/start", AIBotInstanceStart)
		private.POST("/ai-bots/instances/batch-delete", AIBotBatchDelete)

		// combos
		private.GET("/combos", GetCombos)
		private.POST("/combos", CreateCombo)
		private.GET("/combos/:id", GetCombo)
		private.PUT("/combos/:id", UpdateCombo)
		private.DELETE("/combos/:id", DeleteCombo)
		private.POST("/combos/:id/start", StartCombo)
		private.POST("/combos/:id/stop", StopCombo)

		// notify routes
		private.GET("/notify/routes", GetNotifyRoutes)
		private.POST("/notify/routes", UpdateNotifyRoute)
		private.DELETE("/notify/routes/:id", DeleteNotifyRoute)

		// protection（全局资源，写操作 admin-only）
		private.GET("/protection/config", GetProtectionConfig)
		private.POST("/protection/config", ConfigureProtection)
		private.POST("/protection/reset", ResetProtection)

		// hyperopt jobs
		private.GET("/hyperopt/jobs", ListHyperoptJobs)
		private.GET("/hyperopt/jobs/:id", GetHyperoptJob)
		private.POST("/hyperopt/jobs/:id/cancel", CancelHyperoptJob)
		private.DELETE("/hyperopt/jobs/:id", DeleteHyperoptJob)
	}
	return r
}

// ownDo 发 JSON 请求；token 为空则不带 Authorization。
func ownDo(t *testing.T, r *gin.Engine, method, path string, body any, token string) (*httptest.ResponseRecorder, map[string]any) {
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
			// 数组型响应（orders/combos/instances 列表直接返回 []）：
			// 解析失败不算错误，调用方用 rawBody 自行处理。
			var arr []any
			if arrErr := json.Unmarshal(w.Body.Bytes(), &arr); arrErr != nil {
				t.Fatalf("unmarshal response %q: %v", w.Body.String(), err)
			}
		}
	}
	return w, parsed
}

// listContainsStringID 在 []map[string]any 响应数组里按字符串 id 查找。
func listContainsStringID(t *testing.T, items []any, id string) bool {
	t.Helper()
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if m["id"] == id {
			return true
		}
	}
	return false
}

// ── Grid bot ────────────────────────────────────────────────────

// A 创建网格机器人 → B 的 GET/start/stop/PUT/DELETE 全部 403，
// B 的列表不含该机器人；admin 可访问。
func TestOwnershipGridBot(t *testing.T) {
	r := setupOwnershipRouter(t)
	fake := newFakeGridService()
	SetGridService(fake)
	t.Cleanup(func() { SetGridService(nil) })

	tokenA := ownToken(t, 9101, "ownusera", "user")
	tokenB := ownToken(t, 9102, "ownuserb", "user")
	tokenAdmin := ownToken(t, 9103, "ownadmin", "admin")

	w, created := ownDo(t, r, http.MethodPost, "/api/grid/bots", validGridBody(), tokenA)
	assertEq(t, w.Code, http.StatusOK, "A create grid bot")
	botID, _ := created["id"].(string)
	if botID == "" {
		t.Fatalf("created bot missing id: %v", created)
	}

	// B: GET 403 / start 403 / stop 403 / PUT 403 / DELETE 403。
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/grid/bots/" + botID, nil},
		{http.MethodPost, "/api/grid/bots/" + botID + "/start", nil},
		{http.MethodPost, "/api/grid/bots/" + botID + "/stop", nil},
		{http.MethodPut, "/api/grid/bots/" + botID, validGridBody()},
		{http.MethodDelete, "/api/grid/bots/" + botID, nil},
	} {
		w, _ = ownDo(t, r, tc.method, tc.path, tc.body, tokenB)
		assertEq(t, w.Code, http.StatusForbidden, "B "+tc.method+" "+tc.path)
	}

	// B 的列表不含 A 的机器人；A 自己可见；admin 可见。
	_, listedB := ownDo(t, r, http.MethodGet, "/api/grid/bots", nil, tokenB)
	botsB, _ := listedB["bots"].([]any)
	if listContainsStringID(t, botsB, botID) {
		t.Fatalf("B list must not contain A's bot %s", botID)
	}
	_, listedA := ownDo(t, r, http.MethodGet, "/api/grid/bots", nil, tokenA)
	botsA, _ := listedA["bots"].([]any)
	if !listContainsStringID(t, botsA, botID) {
		t.Fatalf("A list must contain own bot %s", botID)
	}
	_, listedAdmin := ownDo(t, r, http.MethodGet, "/api/grid/bots", nil, tokenAdmin)
	botsAdmin, _ := listedAdmin["bots"].([]any)
	if !listContainsStringID(t, botsAdmin, botID) {
		t.Fatalf("admin list must contain A's bot %s", botID)
	}

	// admin 直访详情 200；B 直访详情 403。
	w, _ = ownDo(t, r, http.MethodGet, "/api/grid/bots/"+botID, nil, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin get A's bot")
	w, _ = ownDo(t, r, http.MethodGet, "/api/grid/bots/"+botID, nil, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B get A's bot")

	// B 再试启动仍为 403，机器人不受影响。
	w, _ = ownDo(t, r, http.MethodPost, "/api/grid/bots/"+botID+"/start", nil, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B start A's bot again")

	rec, err := gridBotRepo.GetByID(botID)
	if err != nil || rec == nil {
		t.Fatalf("bot %s must survive B's attempts: %v %v", botID, rec, err)
	}
	assertEq(t, int(rec.UserID), 9101, "bot owner user_id")
}

// ── Strategy config ─────────────────────────────────────────────

// A 创建策略配置 → B GET/PUT/DELETE/start/stop 全部 403，
// B 列表为空，batch-delete 删不掉；admin 可访问。
func TestOwnershipStrategyConfig(t *testing.T) {
	r := setupOwnershipRouter(t)
	tokenA := ownToken(t, 9101, "ownusera", "user")
	tokenB := ownToken(t, 9102, "ownuserb", "user")
	tokenAdmin := ownToken(t, 9103, "ownadmin", "admin")

	body := map[string]any{"name": "own-strat", "symbol": "BTCUSDT", "strategy_type": "trend"}
	w, created := ownDo(t, r, http.MethodPost, "/api/strategies/configs", body, tokenA)
	assertEq(t, w.Code, http.StatusOK, "A create strategy")
	sid, _ := created["id"].(string)
	if sid == "" {
		t.Fatalf("created strategy missing id: %v", created)
	}
	t.Cleanup(func() { store.DeleteStrategyConfig(sid) })

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/strategies/configs/" + sid},
		{http.MethodPut, "/api/strategies/configs/" + sid},
		{http.MethodDelete, "/api/strategies/configs/" + sid},
		{http.MethodPost, "/api/strategies/configs/" + sid + "/start"},
		{http.MethodPost, "/api/strategies/configs/" + sid + "/stop"},
	} {
		w, _ = ownDo(t, r, tc.method, tc.path, body, tokenB)
		assertEq(t, w.Code, http.StatusForbidden, "B "+tc.method+" "+tc.path)
	}

	// 落库属主 = A。
	rec, err := store.NewStrategyConfigRepo().GetByID(sid)
	if err != nil || rec == nil {
		t.Fatalf("strategy %s must persist: %v %v", sid, rec, err)
	}
	if rec.UserID != 9101 {
		t.Fatalf("strategy owner: got %d, want 9101", rec.UserID)
	}

	// B 的列表不含；admin 的列表含；admin GET 200。
	w, listedB := ownDo(t, r, http.MethodGet, "/api/strategies/configs", nil, tokenB)
	assertEq(t, w.Code, http.StatusOK, "B list strategies")
	itemsB, _ := listedB["items"].([]any)
	if listedB["items"] == nil {
		// GetStrategyConfigs 返回数组本体（非 items 键）。
		var arr []any
		if err := json.Unmarshal(rawBody(t, w), &arr); err == nil {
			itemsB = arr
		}
	}
	if listContainsStringID(t, itemsB, sid) {
		t.Fatalf("B strategy list must not contain %s", sid)
	}
	w, _ = ownDo(t, r, http.MethodGet, "/api/strategies/configs/"+sid, nil, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin get A's strategy")

	// B batch-delete 不能删掉 A 的策略。
	w, _ = ownDo(t, r, http.MethodPost, "/api/strategies/configs/batch-delete", map[string]any{"ids": []string{sid}}, tokenB)
	assertEq(t, w.Code, http.StatusOK, "B batch-delete")
	rec, err = store.NewStrategyConfigRepo().GetByID(sid)
	if err != nil || rec == nil {
		t.Fatalf("A's strategy must survive B's batch-delete: %v %v", rec, err)
	}

	// admin batch-delete 可以删掉。
	w, _ = ownDo(t, r, http.MethodPost, "/api/strategies/configs/batch-delete", map[string]any{"ids": []string{sid}}, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin batch-delete")
	rec, err = store.NewStrategyConfigRepo().GetByID(sid)
	if err == nil && rec != nil {
		t.Fatalf("admin batch-delete should remove %s", sid)
	}
}

// rawBody 取响应原始 bytes（ownDo 已解析过一次，这里仅用于数组型响应）。
func rawBody(t *testing.T, w *httptest.ResponseRecorder) []byte {
	t.Helper()
	return w.Body.Bytes()
}

// ── Orders ──────────────────────────────────────────────────────

// A 下单 → B 列表/历史不含该单；B 取消 A 的订单 403；admin 可见。
func TestOwnershipOrders(t *testing.T) {
	r := setupOwnershipRouter(t)
	tokenA := ownToken(t, 9101, "ownusera", "user")
	tokenB := ownToken(t, 9102, "ownuserb", "user")
	tokenAdmin := ownToken(t, 9103, "ownadmin", "admin")

	orderBody := map[string]any{
		"symbol": "BTCUSDT", "side": "BUY", "order_type": "LIMIT",
		"price": 100.0, "quantity": 0.01, "exchange": "paper",
	}
	w, placed := ownDo(t, r, http.MethodPost, "/api/orders", orderBody, tokenA)
	assertEq(t, w.Code, http.StatusOK, "A place order")
	oid, _ := placed["id"].(string)
	if oid == "" {
		t.Fatalf("placed order missing id: %v", placed)
	}

	// paper 单立即成交 → 进历史。B 的历史不含 A 的单。
	w, histB := ownDo(t, r, http.MethodGet, "/api/orders/history", nil, tokenB)
	assertEq(t, w.Code, http.StatusOK, "B order history")
	arrHB, _ := histB["items"].([]any)
	if histB["items"] == nil {
		var arr []any
		_ = json.Unmarshal(rawBody(t, w), &arr)
		arrHB = arr
	}
	if listContainsStringID(t, arrHB, oid) {
		t.Fatalf("B order history must not contain %s", oid)
	}

	// B 取消 A 的订单 → 403（ownership 先于状态检查）；订单仍存活。
	w, _ = ownDo(t, r, http.MethodDelete, "/api/orders/"+oid, nil, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B cancel A's order")
	if o := store.GetOrderByID(oid); o == nil {
		t.Fatalf("order %s must survive B's cancel", oid)
	}

	// admin 的历史含 A 的单（全量）。
	w, histAdmin := ownDo(t, r, http.MethodGet, "/api/orders/history", nil, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin order history")
	arrAdmin, _ := histAdmin["items"].([]any)
	if histAdmin["items"] == nil {
		var arr []any
		_ = json.Unmarshal(rawBody(t, w), &arr)
		arrAdmin = arr
	}
	if !listContainsStringID(t, arrAdmin, oid) {
		t.Fatalf("admin order history must contain %s", oid)
	}

	// 落库 user_id = A。
	uid, found := store.GetOrderOwnerID(oid)
	if !found || uid != 9101 {
		t.Fatalf("order owner: got %d found=%v, want 9101", uid, found)
	}
}

// ── AI bot instance ─────────────────────────────────────────────

// A 创建 AI 机器人 → B GET/PUT/DELETE/start 全部 403，batch-delete 删不掉；
// admin 可访问；B 的实例列表不含 A 的机器人。
func TestOwnershipAIBotInstance(t *testing.T) {
	r := setupOwnershipRouter(t)
	tokenA := ownToken(t, 9101, "ownusera", "user")
	tokenB := ownToken(t, 9102, "ownuserb", "user")
	tokenAdmin := ownToken(t, 9103, "ownadmin", "admin")

	w, created := ownDo(t, r, http.MethodPost, "/api/ai-bots/instances", map[string]any{
		"name": "own-bot", "symbol": "BTCUSDT", "strategy_type": "ai_alpha",
	}, tokenA)
	assertEq(t, w.Code, http.StatusOK, "A create ai bot")
	botID, _ := created["id"].(string)
	if botID == "" {
		t.Fatalf("created ai bot missing id: %v", created)
	}

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/ai-bots/instances/" + botID},
		{http.MethodPut, "/api/ai-bots/instances/" + botID},
		{http.MethodDelete, "/api/ai-bots/instances/" + botID},
		{http.MethodPost, "/api/ai-bots/instances/" + botID + "/start"},
	} {
		w, _ = ownDo(t, r, tc.method, tc.path, map[string]any{"name": "hacked"}, tokenB)
		assertEq(t, w.Code, http.StatusForbidden, "B "+tc.method+" "+tc.path)
	}

	// B 的列表不含 A 的机器人；admin 可直访。
	w, listedB := ownDo(t, r, http.MethodGet, "/api/ai-bots/instances", nil, tokenB)
	assertEq(t, w.Code, http.StatusOK, "B list ai bots")
	itemsB, _ := listedB["items"].([]any)
	if listedB["items"] == nil {
		var arr []any
		_ = json.Unmarshal(rawBody(t, w), &arr)
		itemsB = arr
	}
	if listContainsStringID(t, itemsB, botID) {
		t.Fatalf("B ai bot list must not contain %s", botID)
	}
	w, _ = ownDo(t, r, http.MethodGet, "/api/ai-bots/instances/"+botID, nil, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin get A's ai bot")

	// B batch-delete 不得删掉。
	w, _ = ownDo(t, r, http.MethodPost, "/api/ai-bots/instances/batch-delete", map[string]any{"ids": []string{botID}}, tokenB)
	assertEq(t, w.Code, http.StatusOK, "B batch-delete ai bots")
	if owner, found := store.GetAIBotInstanceOwner(botID); !found || owner != 9101 {
		t.Fatalf("ai bot must survive B batch-delete: owner=%d found=%v", owner, found)
	}

	// admin 删除成功。
	w, _ = ownDo(t, r, http.MethodDelete, "/api/ai-bots/instances/"+botID, nil, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin delete A's ai bot")
	if owner, found := store.GetAIBotInstanceOwner(botID); found {
		t.Fatalf("admin delete should remove bot, still owned by %d", owner)
	}
}

// ── Combos ──────────────────────────────────────────────────────

// A 创建组合 → B GET/PUT/DELETE/start/stop 全部 403；B 列表不含；admin 可访问。
func TestOwnershipCombo(t *testing.T) {
	r := setupOwnershipRouter(t)
	tokenA := ownToken(t, 9101, "ownusera", "user")
	tokenB := ownToken(t, 9102, "ownuserb", "user")
	tokenAdmin := ownToken(t, 9103, "ownadmin", "admin")

	comboBody := map[string]any{"name": "own-combo", "symbol": "BTCUSDT", "members": []any{}, "aggregation_mode": "vote"}
	w, created := ownDo(t, r, http.MethodPost, "/api/combos", comboBody, tokenA)
	assertEq(t, w.Code, http.StatusOK, "A create combo")
	cid, _ := created["id"].(string)
	if cid == "" {
		t.Fatalf("created combo missing id: %v", created)
	}
	t.Cleanup(func() { strategy.DeleteComboConfig(cid) })

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/combos/" + cid},
		{http.MethodPut, "/api/combos/" + cid},
		{http.MethodDelete, "/api/combos/" + cid},
		{http.MethodPost, "/api/combos/" + cid + "/start"},
		{http.MethodPost, "/api/combos/" + cid + "/stop"},
	} {
		w, _ = ownDo(t, r, tc.method, tc.path, comboBody, tokenB)
		assertEq(t, w.Code, http.StatusForbidden, "B "+tc.method+" "+tc.path)
	}

	// B 列表不含；admin 可访问。
	w, listedB := ownDo(t, r, http.MethodGet, "/api/combos", nil, tokenB)
	assertEq(t, w.Code, http.StatusOK, "B list combos")
	arrB, _ := listedB["items"].([]any)
	if listedB["items"] == nil {
		var arr []any
		_ = json.Unmarshal(rawBody(t, w), &arr)
		arrB = arr
	}
	if listContainsStringID(t, arrB, cid) {
		t.Fatalf("B combo list must not contain %s", cid)
	}
	w, _ = ownDo(t, r, http.MethodGet, "/api/combos/"+cid, nil, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin get A's combo")
}

// ── Notify routes ───────────────────────────────────────────────

// 系统路由（user_id=0）所有人可见、仅 admin 可改；用户路由仅属主可改可删。
func TestOwnershipNotifyRoutes(t *testing.T) {
	r := setupOwnershipRouter(t)
	tokenA := ownToken(t, 9101, "ownusera", "user")
	tokenB := ownToken(t, 9102, "ownuserb", "user")
	tokenAdmin := ownToken(t, 9103, "ownadmin", "admin")

	// 系统规则 + A 的私有规则。
	if err := store.SaveNotificationRoute(&store.NotificationRouteRecord{
		ID: "own-sys-route", UserID: 0, Name: "系统规则",
		Events: []string{}, Levels: []string{"CRITICAL"}, Channels: []string{"log"}, Enabled: true,
	}); err != nil {
		t.Fatalf("seed system route: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteNotificationRoute("own-sys-route") })

	ruleBody := map[string]any{
		"id": "own-a-route", "name": "A 的规则",
		"events": []string{"trade"}, "levels": []string{"INFO"}, "channels": []string{"log"}, "enabled": true,
	}
	w, _ := ownDo(t, r, http.MethodPost, "/api/notify/routes", ruleBody, tokenA)
	assertEq(t, w.Code, http.StatusOK, "A create route")
	t.Cleanup(func() { _ = store.DeleteNotificationRoute("own-a-route") })

	// A 与 B 都能看到系统规则 + 各自的规则：B 能看到系统规则，但看不到/改不了 A 的。
	w, listedB := ownDo(t, r, http.MethodGet, "/api/notify/routes", nil, tokenB)
	assertEq(t, w.Code, http.StatusOK, "B list routes")
	rulesB, _ := listedB["rules"].([]any)
	if !listContainsStringID(t, rulesB, "own-sys-route") {
		t.Fatal("B must see system route")
	}
	if listContainsStringID(t, rulesB, "own-a-route") {
		t.Fatal("B must not see A's route")
	}

	// B 改系统规则 → 403；admin 改系统规则 → 200。
	sysBody := map[string]any{
		"id": "own-sys-route", "name": "系统规则改", "levels": []string{"WARN"},
		"events": []string{}, "channels": []string{"log"}, "enabled": true,
	}
	w, _ = ownDo(t, r, http.MethodPost, "/api/notify/routes", sysBody, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B update system route")
	w, _ = ownDo(t, r, http.MethodPost, "/api/notify/routes", sysBody, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin update system route")

	// B 删 A 的规则 → 403；B 改 A 的规则 → 403；admin 删 A 的规则 → 200。
	w, _ = ownDo(t, r, http.MethodDelete, "/api/notify/routes/own-a-route", nil, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B delete A's route")
	w, _ = ownDo(t, r, http.MethodPost, "/api/notify/routes", ruleBody, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B update A's route")
	if owner, ok := store.GetNotificationRouteOwner("own-a-route"); !ok || owner != 9101 {
		t.Fatalf("A's route owner: %d %v", owner, ok)
	}
	w, _ = ownDo(t, r, http.MethodDelete, "/api/notify/routes/own-a-route", nil, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin delete A's route")
}

// ── Protection（全局资源，写操作 admin-only）────────────────────

func TestOwnershipProtectionAdminOnly(t *testing.T) {
	r := setupOwnershipRouter(t)
	tokenB := ownToken(t, 9102, "ownuserb", "user")
	tokenAdmin := ownToken(t, 9103, "ownadmin", "admin")

	// 非 admin 写 → 403；admin 写 → 200；所有人可读。
	w, _ := ownDo(t, r, http.MethodPost, "/api/protection/config", map[string]any{"protections": []any{}}, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B configure protection")
	w, _ = ownDo(t, r, http.MethodPost, "/api/protection/reset", nil, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B reset protection")
	w, _ = ownDo(t, r, http.MethodGet, "/api/protection/config", nil, tokenB)
	assertEq(t, w.Code, http.StatusOK, "B read protection config")
	w, _ = ownDo(t, r, http.MethodPost, "/api/protection/config", map[string]any{"protections": []any{}}, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin configure protection")
}

// ── Hyperopt jobs（内存任务）────────────────────────────────────

// A 的 hyperopt 任务：B GET/cancel/delete 全部 403，列表不含；admin 可访问。
func TestOwnershipHyperoptJob(t *testing.T) {
	r := setupOwnershipRouter(t)
	tokenB := ownToken(t, 9102, "ownuserb", "user")
	tokenAdmin := ownToken(t, 9103, "ownadmin", "admin")

	hyperoptJobsMu.Lock()
	hyperoptJobs["ho-own-test"] = &hyperoptJob{
		ID:     "ho-own-test",
		UserID: 9101,
		Status: "completed",
		Result: &hyperopt.Result{},
	}
	hyperoptJobsMu.Unlock()
	t.Cleanup(func() {
		hyperoptJobsMu.Lock()
		delete(hyperoptJobs, "ho-own-test")
		hyperoptJobsMu.Unlock()
	})

	w, _ := ownDo(t, r, http.MethodGet, "/api/hyperopt/jobs/ho-own-test", nil, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B get A's hyperopt job")
	w, _ = ownDo(t, r, http.MethodPost, "/api/hyperopt/jobs/ho-own-test/cancel", nil, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B cancel A's hyperopt job")
	w, _ = ownDo(t, r, http.MethodDelete, "/api/hyperopt/jobs/ho-own-test", nil, tokenB)
	assertEq(t, w.Code, http.StatusForbidden, "B delete A's hyperopt job")

	// B 的列表不含该任务；admin 可访问。
	w, listedB := ownDo(t, r, http.MethodGet, "/api/hyperopt/jobs", nil, tokenB)
	assertEq(t, w.Code, http.StatusOK, "B list hyperopt jobs")
	jobsB, _ := listedB["jobs"].([]any)
	if listContainsStringID(t, jobsB, "ho-own-test") {
		t.Fatal("B hyperopt job list must not contain A's job")
	}
	w, _ = ownDo(t, r, http.MethodGet, "/api/hyperopt/jobs/ho-own-test", nil, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin get A's hyperopt job")
}
