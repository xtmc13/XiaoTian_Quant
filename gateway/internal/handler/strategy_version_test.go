package handler

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 策略配置版本快照（0014）handler 测试 ─────────────────────────
//
// 覆盖：手动快照/列表/详情/恢复的完整生命周期、恢复前自动快照、
// 以及多用户越权（B 不能看/恢复 A 的策略版本，admin 放行）。
// 复用 ownToken（ownership_test.go）与 gridDo（grid_bot_test.go）。

func setupVersionRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/strategies/configs")
	g.Use(middleware.AuthRequired())
	{
		g.POST("/:id/versions", CreateStrategyVersion)
		g.GET("/:id/versions", ListStrategyVersions)
		g.GET("/:id/versions/:vid", GetStrategyVersion)
		g.POST("/:id/versions/:vid/restore", RestoreStrategyVersion)
	}
	return r
}

func seedStrategyConfig(t *testing.T, id string, uid int64, configJSON string) {
	t.Helper()
	repo := store.NewStrategyConfigRepo()
	rec := &store.StrategyConfigRecord{
		ID:           id,
		UserID:       uid,
		Name:         "版本策略",
		Category:     "futures",
		StrategyType: "cra_contract",
		Symbol:       "BTCUSDT",
		Coin:         "BTC",
		Direction:    "long",
		Leverage:     10,
		MarketType:   "swap",
		Status:       "stopped",
		ConfigJSON:   configJSON,
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("seed strategy %s: %v", id, err)
	}
	t.Cleanup(func() { _ = repo.Delete(id) })
}

// 生命周期：手动快照 → 列表（倒序）→ 详情 → 改库 → 恢复 → 恢复前自动快照。
func TestStrategyVersionLifecycle(t *testing.T) {
	r := setupVersionRouter()
	tokenA := ownToken(t, 9201, "svusera", "user")
	seedStrategyConfig(t, "sv-life", 9201, `{"leverage":10,"tag":"v1"}`)

	// 手动快照（带 note）。
	w, created := gridDo(t, r, http.MethodPost, "/api/strategies/configs/sv-life/versions", map[string]any{"note": "基线"}, tokenA)
	assertEq(t, w.Code, http.StatusOK, "create version status")
	assertNumEq(t, created["version"], 1, "first version number")

	// 手动快照（空 body，note 可选）。
	w, created = gridDo(t, r, http.MethodPost, "/api/strategies/configs/sv-life/versions", nil, tokenA)
	assertEq(t, w.Code, http.StatusOK, "create version without body status")
	assertNumEq(t, created["version"], 2, "second version number")

	// 列表：倒序、数量正确。
	w, listed := gridDo(t, r, http.MethodGet, "/api/strategies/configs/sv-life/versions", nil, tokenA)
	assertEq(t, w.Code, http.StatusOK, "list versions status")
	versions, ok := listed["versions"].([]any)
	if !ok || len(versions) != 2 {
		t.Fatalf("versions list = %v, want 2 items", listed["versions"])
	}
	first := versions[0].(map[string]any)
	assertNumEq(t, first["version"], 2, "list newest first")

	// 详情：payload 是完整配置快照。
	w, detail := gridDo(t, r, http.MethodGet, "/api/strategies/configs/sv-life/versions/1", nil, tokenA)
	assertEq(t, w.Code, http.StatusOK, "get version status")
	payload, ok := detail["payload"].(map[string]any)
	if !ok {
		t.Fatalf("detail payload missing: %v", detail)
	}
	if payload["config_json"] != `{"leverage":10,"tag":"v1"}` {
		t.Fatalf("payload config_json = %v", payload["config_json"])
	}

	// 非法版本号 400；不存在版本 404。
	w, bad := gridDo(t, r, http.MethodGet, "/api/strategies/configs/sv-life/versions/abc", nil, tokenA)
	assertEq(t, w.Code, http.StatusBadRequest, "invalid vid status")
	if bad["error"] == nil {
		t.Fatalf("invalid vid should return error: %v", bad)
	}
	w, missing := gridDo(t, r, http.MethodGet, "/api/strategies/configs/sv-life/versions/99", nil, tokenA)
	assertEq(t, w.Code, http.StatusNotFound, "missing version status")
	if missing["error"] == nil {
		t.Fatalf("missing version should return error: %v", missing)
	}

	// 直接改库（模拟更新后的新状态），再恢复 v1。
	repo := store.NewStrategyConfigRepo()
	rec, err := repo.GetByID("sv-life")
	if err != nil || rec == nil {
		t.Fatalf("get seeded strategy: %v %v", rec, err)
	}
	rec.ConfigJSON = `{"leverage":99,"tag":"v2"}`
	if err := repo.Update(rec); err != nil {
		t.Fatalf("update strategy: %v", err)
	}
	w, restored := gridDo(t, r, http.MethodPost, "/api/strategies/configs/sv-life/versions/1/restore", nil, tokenA)
	assertEq(t, w.Code, http.StatusOK, "restore status")
	assertNumEq(t, restored["restored_version"], 1, "restored version")
	rec, _ = repo.GetByID("sv-life")
	if rec.ConfigJSON != `{"leverage":10,"tag":"v1"}` {
		t.Fatalf("config after restore = %s", rec.ConfigJSON)
	}
	if rec.UserID != 9201 {
		t.Fatalf("owner after restore = %d, want 9201", rec.UserID)
	}

	// 恢复前自动快照：多出版本 3，note 正确，payload 为恢复前状态。
	w, listed = gridDo(t, r, http.MethodGet, "/api/strategies/configs/sv-life/versions", nil, tokenA)
	versions = listed["versions"].([]any)
	if len(versions) != 3 {
		t.Fatalf("versions after restore = %d, want 3", len(versions))
	}
	latest := versions[0].(map[string]any)
	assertNumEq(t, latest["version"], 3, "auto snapshot version")
	if latest["note"] != "恢复前自动快照" {
		t.Fatalf("auto snapshot note = %v", latest["note"])
	}
	w, detail = gridDo(t, r, http.MethodGet, "/api/strategies/configs/sv-life/versions/3", nil, tokenA)
	assertEq(t, w.Code, http.StatusOK, "auto snapshot detail status")
	preRestore := detail["payload"].(map[string]any)
	if preRestore["config_json"] != `{"leverage":99,"tag":"v2"}` {
		t.Fatalf("pre-restore snapshot payload = %v", preRestore["config_json"])
	}
}

// 越权：B 不能对 A 的策略打快照/看列表/看详情/恢复；且不产生任何版本；
// admin 放行。
func TestStrategyVersionOwnership(t *testing.T) {
	r := setupVersionRouter()
	tokenA := ownToken(t, 9202, "svusera2", "user")
	tokenB := ownToken(t, 9203, "svuserb2", "user")
	tokenAdmin := ownToken(t, 9204, "svuseradm", "admin")
	seedStrategyConfig(t, "sv-own", 9202, `{"leverage":5}`)

	// A 打一条基线快照。
	w, _ := gridDo(t, r, http.MethodPost, "/api/strategies/configs/sv-own/versions", map[string]any{"note": "A 基线"}, tokenA)
	assertEq(t, w.Code, http.StatusOK, "A create version status")

	// B 四个端点全部 403，且错误体为 {"error": ...}。
	for _, tc := range []struct {
		method, path string
		body         map[string]any
	}{
		{http.MethodPost, "/api/strategies/configs/sv-own/versions", map[string]any{"note": "B 偷打"}},
		{http.MethodGet, "/api/strategies/configs/sv-own/versions", nil},
		{http.MethodGet, "/api/strategies/configs/sv-own/versions/1", nil},
		{http.MethodPost, "/api/strategies/configs/sv-own/versions/1/restore", nil},
	} {
		w, resp := gridDo(t, r, tc.method, tc.path, tc.body, tokenB)
		assertEq(t, w.Code, http.StatusForbidden, "B "+tc.method+" "+tc.path)
		if resp["error"] == nil {
			t.Fatalf("B %s %s should return error body: %v", tc.method, tc.path, resp)
		}
	}

	// B 的尝试没有产生任何版本。
	recs, err := store.NewStrategyVersionRepo().ListByStrategy("sv-own")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("versions after B attempts = %d, want 1", len(recs))
	}

	// B 对不存在的策略同样 404 而不是 403 泄信息？不存在的策略 → 404。
	w, _ = gridDo(t, r, http.MethodGet, "/api/strategies/configs/sv-no-such/versions", nil, tokenB)
	assertEq(t, w.Code, http.StatusNotFound, "missing strategy status")

	// admin 可看列表与详情。
	w, listed := gridDo(t, r, http.MethodGet, "/api/strategies/configs/sv-own/versions", nil, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin list versions status")
	if n := len(listed["versions"].([]any)); n != 1 {
		t.Fatalf("admin versions = %d, want 1", n)
	}
	w, _ = gridDo(t, r, http.MethodGet, "/api/strategies/configs/sv-own/versions/1", nil, tokenAdmin)
	assertEq(t, w.Code, http.StatusOK, "admin get version status")
}
