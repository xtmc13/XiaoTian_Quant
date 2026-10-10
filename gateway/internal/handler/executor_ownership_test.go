package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── executor 体系越权隔离测试（0057 用户归属 + 信号源写权限收口）──
//
// 路由不经 AuthRequired，改用注入中间件直接写 UserIDKey/RoleKey
// （与 hyperopt_epochs_test 同一模式，口径等同生产中间件）。

func setupExecutorAuthRouter(uid int, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if uid > 0 {
		r.Use(func(c *gin.Context) {
			c.Set(middleware.UserIDKey, uid)
			c.Set(middleware.RoleKey, role)
		})
	}
	r.POST("/executor/sources", ExecutorCreateSignalSource)
	r.PUT("/executor/signal-sources/:id", ExecutorUpdateSignalSource)
	r.DELETE("/executor/signal-sources/:id", ExecutorDeleteSignalSource)
	r.GET("/executor/stats", ExecutorStats)
	return r
}

func execStatsGet(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	r.ServeHTTP(w, req)
	return w
}

// TestSignalSourceOwnershipGuard 非属主改/删他人信号源 → 403；
// 无属主（OwnerUserID=0）的源仅 admin 可管，普通登录用户 403。
func TestSignalSourceOwnershipGuard(t *testing.T) {
	repo := store.NewSignalSourceRepo()
	owned := &store.SignalSource{Name: "A的源", Type: "webhook", OwnerUserID: 9301, Enabled: true, FeeModel: "free", TPSLJSON: `{}`}
	if err := repo.Create(owned); err != nil {
		t.Fatalf("seed owned source: %v", err)
	}
	ownerless := &store.SignalSource{Name: "无主源", Type: "webhook", OwnerUserID: 0, Enabled: true, FeeModel: "free", TPSLJSON: `{}`}
	if err := repo.Create(ownerless); err != nil {
		t.Fatalf("seed ownerless source: %v", err)
	}

	rB := setupExecutorAuthRouter(9302, "user")
	rA := setupExecutorAuthRouter(9301, "user")
	rAdmin := setupExecutorAuthRouter(9303, "admin")

	// B（非属主）改/删 A 的源 → 403
	w := doJSON(t, rB, "PUT", "/executor/signal-sources/"+owned.ID, `{"name":"劫持"}`)
	assertEq(t, w.Code, http.StatusForbidden, "B update A's source")
	w = doJSON(t, rB, "DELETE", "/executor/signal-sources/"+owned.ID, "")
	assertEq(t, w.Code, http.StatusForbidden, "B delete A's source")
	got, err := repo.GetByID(owned.ID)
	assertTrue(t, err == nil && got != nil && got.Name == "A的源", "A's source must survive B's attempts")

	// A（属主）改自己的源 → 200
	w = doJSON(t, rA, "PUT", "/executor/signal-sources/"+owned.ID, `{"name":"A改名"}`)
	assertEq(t, w.Code, http.StatusOK, "A update own source")

	// 无主源：普通用户改/删 → 403；admin 可管 → 200
	w = doJSON(t, rB, "PUT", "/executor/signal-sources/"+ownerless.ID, `{"name":"劫持无主"}`)
	assertEq(t, w.Code, http.StatusForbidden, "B update ownerless source")
	w = doJSON(t, rB, "DELETE", "/executor/signal-sources/"+ownerless.ID, "")
	assertEq(t, w.Code, http.StatusForbidden, "B delete ownerless source")
	w = doJSON(t, rAdmin, "PUT", "/executor/signal-sources/"+ownerless.ID, `{"name":"admin接管"}`)
	assertEq(t, w.Code, http.StatusOK, "admin update ownerless source")
	w = doJSON(t, rAdmin, "DELETE", "/executor/signal-sources/"+ownerless.ID, "")
	assertEq(t, w.Code, http.StatusOK, "admin delete ownerless source")
	if gone, err := repo.GetByID(ownerless.ID); err == nil && gone != nil {
		t.Fatal("admin delete should remove ownerless source")
	}

	// 未注入用户（单用户兼容）：保持放行，不回归。
	rOpen := setupExecutorAuthRouter(0, "")
	w = doJSON(t, rOpen, "PUT", "/executor/signal-sources/"+owned.ID, `{"name":"单用户改名"}`)
	assertEq(t, w.Code, http.StatusOK, "uninjected context keeps legacy behavior")
}

// TestExecutorCreateSignalSourceRequiresUser 未注入用户身份时拒绝创建（401），
// 不再静默建无主源；登录用户创建落自己的 user_id。
func TestExecutorCreateSignalSourceRequiresUser(t *testing.T) {
	rAnon := setupExecutorAuthRouter(0, "")
	w := doJSON(t, rAnon, "POST", "/executor/sources", `{"name":"匿名源"}`)
	assertEq(t, w.Code, http.StatusUnauthorized, "create without identity must 401")

	rA := setupExecutorAuthRouter(9301, "user")
	w = doJSON(t, rA, "POST", "/executor/sources", `{"name":"A的收费源","fee_model":"fixed_monthly","monthly_fee":10}`)
	assertEq(t, w.Code, http.StatusOK, "logged-in user create")
	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "response should parse")
	sid, _ := resp["source_id"].(string)
	assertTrue(t, sid != "", "source_id returned")
	src, err := store.NewSignalSourceRepo().GetByID(sid)
	assertTrue(t, err == nil && src != nil, "source persisted")
	assertEq(t, src.OwnerUserID, 9301, "source owner = creator")
}

// TestExecutorStatsUserIsolation 普通用户只见自己 user_id 的信号/执行统计；
// admin 见全量；未注入用户保持全量（单用户兼容）。
func TestExecutorStatsUserIsolation(t *testing.T) {
	now := time.Now().UnixMilli()
	sigRepo := store.NewSignalRepo()
	execRepo := store.NewSignalExecutionRepo()
	seed := func(uid int, symbol string, pnl float64) {
		sig := &store.SignalRecord{Symbol: symbol, Direction: "LONG", Strategy: "isolation-test", Status: "CLOSED", UserID: uid, CreatedAt: now}
		if err := sigRepo.Create(sig); err != nil {
			t.Fatalf("seed signal: %v", err)
		}
		exec := &store.SignalExecution{
			SignalID: sig.ID, SourceID: "default", Symbol: symbol, Direction: "LONG",
			Status: "closed", RealizedPnL: pnl, UserID: uid, CreatedAt: now, ClosedAt: now,
		}
		if err := execRepo.Create(exec); err != nil {
			t.Fatalf("seed execution: %v", err)
		}
	}
	seed(9311, "USERAUSDT", 10)
	seed(9311, "USERAUSDT", 5)
	seed(9312, "USERBUSDT", -7)

	statsOf := func(t *testing.T, r *gin.Engine) map[string]any {
		t.Helper()
		w := execStatsGet(r, "/executor/stats")
		assertEq(t, w.Code, http.StatusOK, "stats status")
		var resp map[string]any
		assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "response should parse")
		data, ok := resp["data"].(map[string]any)
		assertTrue(t, ok, "data payload")
		return data
	}

	// 用户 A：只看自己的 2 条信号，盈亏 15
	dataA := statsOf(t, setupExecutorAuthRouter(9311, "user"))
	assertEq(t, int(dataA["total_signals"].(float64)), 2, "A total_signals")
	assertEq(t, int(dataA["closed"].(float64)), 2, "A closed")
	assertTrue(t, dataA["cum_pnl"].(float64) == 15, "A cum_pnl must be 15")

	// 用户 B：只看自己的 1 条，盈亏 -7
	dataB := statsOf(t, setupExecutorAuthRouter(9312, "user"))
	assertEq(t, int(dataB["total_signals"].(float64)), 1, "B total_signals")
	assertTrue(t, dataB["cum_pnl"].(float64) == -7, "B cum_pnl must be -7")

	// admin：全量，至少含 A+B 的 3 条（库里还有其它用例的历史数据）
	dataAdmin := statsOf(t, setupExecutorAuthRouter(9313, "admin"))
	assertTrue(t, int(dataAdmin["total_signals"].(float64)) >= 3, "admin sees all signals")

	// 未注入用户：全量（单用户模式行为不变）
	dataOpen := statsOf(t, setupExecutorAuthRouter(0, ""))
	assertTrue(t, int(dataOpen["total_signals"].(float64)) >= 3, "uninjected sees all signals")
}
