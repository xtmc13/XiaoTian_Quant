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

// reconcileAuthed 模拟 AuthRequired 注入登录态。
func reconcileAuthed(uid int, role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", uid)
		c.Set("role", role)
		c.Next()
	}
}

func reconcilePost(t *testing.T, r *gin.Engine, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var parsed map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return w.Code, parsed
}

func reconcilePut(t *testing.T, r *gin.Engine, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var parsed map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return w.Code, parsed
}

func reconcileGet(t *testing.T, r *gin.Engine, path string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	var parsed map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return w.Code, parsed
}

func TestReconcileDiffsFlow(t *testing.T) {
	repo := store.NewReconcileRepo()
	// 属主 100 的 open 差异 + 属主 200 的 open 差异。
	for _, d := range []*store.ReconcileDiff{
		{UserID: 100, Exchange: "binance", Symbol: "BTCUSDT", DiffType: "position_quantity", LocalQty: 1, ExchangeQty: 1.5, Detail: "test"},
		{UserID: 200, Exchange: "binance", Symbol: "ETHUSDT", DiffType: "position_missing_local", ExchangeQty: -2, Detail: "test"},
	} {
		if _, _, err := repo.CreateDiff(d); err != nil {
			t.Fatalf("create diff: %v", err)
		}
	}

	r := setupRouter()
	r.GET("/reconcile/diffs", reconcileAuthed(100, "user"), ReconcileDiffsList)
	code, resp := reconcileGet(t, r, "/reconcile/diffs")
	assertEq(t, code, http.StatusOK, "list 200")
	diffs, _ := resp["diffs"].([]any)
	assertTrue(t, len(diffs) == 1, "普通用户只见本人差异")
	first := diffs[0].(map[string]any)
	if first["user_id"].(float64) != 100 {
		t.Fatalf("差异属主异常: %+v", first)
	}

	// admin 全量。
	rAdmin := setupRouter()
	rAdmin.GET("/reconcile/diffs", reconcileAuthed(100, "admin"), ReconcileDiffsList)
	_, respAdmin := reconcileGet(t, rAdmin, "/reconcile/diffs")
	assertTrue(t, len(respAdmin["diffs"].([]any)) == 2, "admin 见全部差异")

	diffID := int64(first["id"].(float64))
	_ = diffID
	// 解决：他人 → 403。
	rOther := setupRouter()
	rOther.POST("/reconcile/diffs/:id/resolve", reconcileAuthed(200, "user"), ReconcileDiffResolve)
	code, _ = reconcilePost(t, rOther, "/reconcile/diffs/1/resolve", map[string]any{"action": "ignore"})
	assertEq(t, code, http.StatusForbidden, "他人 resolve 必须 403")

	// 本人 accept_local → resolved。
	rOwn := setupRouter()
	rOwn.POST("/reconcile/diffs/:id/resolve", reconcileAuthed(100, "user"), ReconcileDiffResolve)
	code, resolved := reconcilePost(t, rOwn, "/reconcile/diffs/1/resolve", map[string]any{"action": "accept_local"})
	assertEq(t, code, http.StatusOK, "本人 resolve 200")
	if resolved["status"].(string) != "resolved" || resolved["resolution"].(string) != "accept_local" {
		t.Fatalf("resolve 结果异常: %+v", resolved)
	}

	// 重复 resolve → 409。
	code, _ = reconcilePost(t, rOwn, "/reconcile/diffs/1/resolve", map[string]any{"action": "ignore"})
	assertEq(t, code, http.StatusConflict, "重复 resolve 必须 409")

	// 非法 action → 400。
	code, _ = reconcilePost(t, rOwn, "/reconcile/diffs/2/resolve", map[string]any{"action": "hack"})
	assertEq(t, code, http.StatusBadRequest, "非法 action 必须 400")
}

func TestReconcileStatusAndConfig(t *testing.T) {
	r := setupRouter()
	r.GET("/reconcile/status", reconcileAuthed(1, "user"), ReconcileStatus)
	code, resp := reconcileGet(t, r, "/reconcile/status")
	assertEq(t, code, http.StatusOK, "status 200")
	if _, ok := resp["config"].(map[string]any); !ok {
		t.Fatalf("status 应含 config: %+v", resp)
	}

	// 配置：GET 登录可见。
	rCfg := setupRouter()
	rCfg.GET("/reconcile/config", reconcileAuthed(1, "user"), ReconcileConfigGet)
	code, cfg := reconcileGet(t, rCfg, "/reconcile/config")
	assertEq(t, code, http.StatusOK, "config get 200")
	if cfg["slippage_pct"].(float64) != 0.5 {
		t.Fatalf("默认滑点阈值应为 0.5: %+v", cfg)
	}

	// PUT：非 admin → 403。
	rPutUser := setupRouter()
	rPutUser.PUT("/reconcile/config", reconcileAuthed(1, "user"), ReconcileConfigPut)
	code, _ = reconcilePut(t, rPutUser, "/reconcile/config", map[string]any{"slippage_pct": 1.0})
	assertEq(t, code, http.StatusForbidden, "非 admin 配置必须 403")

	// PUT：admin → 200 并持久化。
	rPut := setupRouter()
	rPut.PUT("/reconcile/config", reconcileAuthed(1, "admin"), ReconcileConfigPut)
	code, updated := reconcilePut(t, rPut, "/reconcile/config", map[string]any{"slippage_pct": 1.25})
	assertEq(t, code, http.StatusOK, "admin 配置 200")
	if updated["slippage_pct"].(float64) != 1.25 {
		t.Fatalf("配置更新未生效: %+v", updated)
	}
	_, after := reconcileGet(t, rCfg, "/reconcile/config")
	if after["slippage_pct"].(float64) != 1.25 {
		t.Fatalf("配置未持久化: %+v", after)
	}
}

func TestReconcileDeviationsEndpoints(t *testing.T) {
	repo := store.NewReconcileRepo()
	if _, _, err := repo.CreateDeviation(&store.ReconcileDeviation{
		OrderID: "ord-dev-1", UserID: 100, Symbol: "BTCUSDT", Exchange: "binance",
		Kind: "slippage", ExpectedPrice: 50000, AvgPrice: 51000, SlippagePct: 2, Detail: "test",
	}); err != nil {
		t.Fatalf("create deviation: %v", err)
	}

	r := setupRouter()
	r.GET("/reconcile/deviations", reconcileAuthed(100, "user"), ReconcileDeviationsList)
	code, resp := reconcileGet(t, r, "/reconcile/deviations?kind=slippage")
	assertEq(t, code, http.StatusOK, "deviations 200")
	devs, _ := resp["deviations"].([]any)
	assertTrue(t, len(devs) == 1, "应有一条滑点偏差")
	devID := int64(devs[0].(map[string]any)["id"].(float64))

	// 他人 resolve → 403；本人 → 200。
	rOther := setupRouter()
	rOther.POST("/reconcile/deviations/:id/resolve", reconcileAuthed(200, "user"), ReconcileDeviationResolve)
	code, _ = reconcilePost(t, rOther, "/reconcile/deviations/1/resolve", nil)
	assertEq(t, code, http.StatusForbidden, "他人确认偏差必须 403")

	rOwn := setupRouter()
	rOwn.POST("/reconcile/deviations/:id/resolve", reconcileAuthed(100, "user"), ReconcileDeviationResolve)
	code, resolved := reconcilePost(t, rOwn, "/reconcile/deviations/1/resolve", nil)
	assertEq(t, code, http.StatusOK, "本人确认偏差 200")
	if resolved["status"].(string) != "resolved" {
		t.Fatalf("偏差应 resolved: %+v", resolved)
	}
	_ = devID
}
