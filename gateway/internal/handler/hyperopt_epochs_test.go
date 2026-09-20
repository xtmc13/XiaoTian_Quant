package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Hyperopt Epochs handler 测试 ────────────────────────────────
//
// 路由不经 AuthRequired，改用注入中间件直接写 UserIDKey/RoleKey，
// 覆盖 requireOwner 的属主/管理员/无属主三分支（与生产中间件口径一致）。

func setupEpochRouter(uid int, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if uid > 0 {
			c.Set(middleware.UserIDKey, uid)
			c.Set(middleware.RoleKey, role)
		}
		c.Next()
	})
	r.GET("/hyperopt/epochs", ListHyperoptEpochs)
	r.GET("/hyperopt/epochs/:id", GetHyperoptEpoch)
	r.POST("/hyperopt/epochs/:id/apply", ApplyHyperoptEpoch)
	return r
}

func seedEpoch(t *testing.T, id, strategyID string, userID int64, params string) *store.HyperoptEpochRecord {
	t.Helper()
	rec := &store.HyperoptEpochRecord{
		ID:          id,
		UserID:      userID,
		JobID:       "ho-test",
		StrategyID:  strategyID,
		TrialID:     1,
		ParamsJSON:  params,
		MetricsJSON: `{"sharpe_ratio": 2.0, "total_trades": 30, "max_drawdown_pct": 5.0}`,
		Loss:        -1.5,
		LossName:    "sharpe",
		CreatedAt:   1700000000000,
	}
	if err := store.NewHyperoptEpochRepo().Create(rec); err != nil {
		t.Fatalf("seed epoch: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.GetDB().Exec(`DELETE FROM xt_hyperopt_epochs WHERE id = ?`, id)
	})
	return rec
}

func applyRequest(t *testing.T, r *gin.Engine, epochID string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/hyperopt/epochs/"+epochID+"/apply", nil)
	r.ServeHTTP(w, req)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

// assertValEq 比较任意 JSON 解码值（assertEq 只支持 int）。
func assertValEq(t *testing.T, got, want any, msg string) {
	t.Helper()
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("%s: got %v (%T), want %v (%T)", msg, got, got, want, want)
	}
}

// TestApplyHyperoptEpochWriteBack 属主一键回写：diff 留旧值、新值写入
// config_json、原有未涉及字段保留、epoch 标记 applied、重复 apply 409。
func TestApplyHyperoptEpochWriteBack(t *testing.T) {
	cfgID := "cfg-epoch-apply"
	seedStrategyConfig(t, cfgID, 1, `{"lookback":10,"stop_loss":0.05}`)
	seedEpoch(t, "ep-apply", cfgID, 1, `{"lookback":20,"trail_pct":0.02}`)

	r := setupEpochRouter(1, "user")
	w, resp := applyRequest(t, r, "ep-apply")
	assertEq(t, w.Code, http.StatusOK, "apply status")
	assertValEq(t, resp["status"], "applied", "apply status body")
	assertValEq(t, resp["strategy_id"], cfgID, "strategy id echoed")

	// diff：被覆盖键留旧值，新键 old 为 null
	diff, ok := resp["diff"].(map[string]any)
	assertTrue(t, ok, "diff should be an object")
	lookback, ok := diff["lookback"].(map[string]any)
	assertTrue(t, ok, "diff.lookback should be an object")
	assertValEq(t, lookback["old"], float64(10), "diff old value kept")
	assertValEq(t, lookback["new"], float64(20), "diff new value")
	trail, ok := diff["trail_pct"].(map[string]any)
	assertTrue(t, ok, "diff.trail_pct should be an object")
	assertTrue(t, trail["old"] == nil, "new key old = null")
	assertValEq(t, trail["new"], 0.02, "new key new value")

	// 写回正确性：config_json 合并新值且保留未涉及字段
	rec, err := store.NewStrategyConfigRepo().GetByID(cfgID)
	assertTrue(t, err == nil && rec != nil, "config readable after apply")
	var cfg map[string]any
	assertTrue(t, json.Unmarshal([]byte(rec.ConfigJSON), &cfg) == nil, "config json parses")
	assertValEq(t, cfg["lookback"], float64(20), "lookback overwritten")
	assertValEq(t, cfg["trail_pct"], 0.02, "trail_pct added")
	assertValEq(t, cfg["stop_loss"], 0.05, "untouched key preserved")

	// epoch 标记 applied
	ep, _ := store.NewHyperoptEpochRepo().GetByID("ep-apply")
	assertTrue(t, ep.Applied, "epoch marked applied")
	assertTrue(t, ep.AppliedAt > 0, "applied_at set")

	// 重复 apply → 409
	w, resp = applyRequest(t, r, "ep-apply")
	assertEq(t, w.Code, http.StatusConflict, "second apply rejected")
	assertValEq(t, resp["error"], "epoch already applied", "conflict message")
}

// TestApplyHyperoptEpochPermission 越权矩阵：非属主 403、admin 放行、
// 策略配置他人属主 403、无 strategy_id 400。
func TestApplyHyperoptEpochPermission(t *testing.T) {
	cfgA, cfgB := "cfg-perm-a", "cfg-perm-b"
	seedStrategyConfig(t, cfgA, 1, `{"lookback":10}`)
	seedStrategyConfig(t, cfgB, 2, `{"lookback":10}`)
	seedEpoch(t, "ep-perm-a", cfgA, 1, `{"lookback":20}`)
	seedEpoch(t, "ep-perm-b", cfgB, 2, `{"lookback":30}`)
	seedEpoch(t, "ep-nostrategy", "", 1, `{"lookback":40}`)

	// 非属主用户 2 访问用户 1 的 epoch → 403
	w, _ := applyRequest(t, setupEpochRouter(2, "user"), "ep-perm-a")
	assertEq(t, w.Code, http.StatusForbidden, "non-owner apply forbidden")

	// 属主本人 → 200
	w, _ = applyRequest(t, setupEpochRouter(1, "user"), "ep-perm-a")
	assertEq(t, w.Code, http.StatusOK, "owner apply ok")

	// admin 可 apply 他人 epoch → 200
	w, _ = applyRequest(t, setupEpochRouter(9, "admin"), "ep-perm-b")
	assertEq(t, w.Code, http.StatusOK, "admin apply ok")

	// epoch 属主 A，但目标策略配置属主 B → 403（配置属主复核）
	w, _ = applyRequest(t, setupEpochRouter(3, "user"), "ep-perm-b")
	assertEq(t, w.Code, http.StatusForbidden, "stranger apply forbidden")
	cfgRec, _ := store.NewStrategyConfigRepo().GetByID(cfgB)
	assertTrue(t, strings.Contains(cfgRec.ConfigJSON, `"lookback":30`), "config B got admin-applied params")

	// 无 strategy_id → 400
	w, _ = applyRequest(t, setupEpochRouter(1, "user"), "ep-nostrategy")
	assertEq(t, w.Code, http.StatusBadRequest, "no strategy_id rejected")

	// 未注入用户（单用户兼容）→ 放行
	w, _ = applyRequest(t, setupEpochRouter(0, ""), "ep-perm-b")
	assertEq(t, w.Code, http.StatusConflict, "already applied → conflict for ownerless path")
}

// TestGetHyperoptEpochDetail 详情：404 / 403 / 200。
func TestGetHyperoptEpochDetail(t *testing.T) {
	seedEpoch(t, "ep-detail", "", 1, `{"lookback":20}`)

	// 404
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/hyperopt/epochs/nope", nil)
	setupEpochRouter(1, "user").ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusNotFound, "missing epoch 404")

	// 403：非属主
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/hyperopt/epochs/ep-detail", nil)
	setupEpochRouter(2, "user").ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusForbidden, "non-owner get forbidden")

	// 200：属主，JSON 字段解析
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/hyperopt/epochs/ep-detail", nil)
	setupEpochRouter(1, "user").ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "owner get ok")
	var resp map[string]any
	assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "detail parses")
	assertValEq(t, resp["id"], "ep-detail", "id echoed")
	assertValEq(t, resp["loss_name"], "sharpe", "loss_name")
	assertValEq(t, resp["applied"], false, "not applied")
	_, hasParams := resp["params"].(map[string]any)
	assertTrue(t, hasParams, "params decoded")
}

// TestListHyperoptEpochsFilter HTTP 层组合过滤：属主可见性 + 查询参数透传。
func TestListHyperoptEpochsFilter(t *testing.T) {
	for _, e := range []struct {
		id     string
		userID int64
		job    string
		loss   float64
		sharpe float64
	}{
		{"lst-1", 1, "job-lst", -2.0, 2.5},
		{"lst-2", 1, "job-lst", -1.0, 1.0},
		{"lst-3", 2, "job-lst", -0.5, 3.0},
		{"lst-4", 0, "job-other", -3.0, 0.5},
	} {
		rec := &store.HyperoptEpochRecord{
			ID:          e.id,
			UserID:      e.userID,
			JobID:       e.job,
			ParamsJSON:  `{}`,
			MetricsJSON: fmt.Sprintf(`{"sharpe_ratio": %v, "total_trades": 30, "max_drawdown_pct": 5.0}`, e.sharpe),
			Loss:        e.loss,
			LossName:    "sharpe",
			CreatedAt:   1700000000000,
		}
		if err := store.NewHyperoptEpochRepo().Create(rec); err != nil {
			t.Fatalf("seed %s: %v", e.id, err)
		}
		t.Cleanup(func() {
			_, _ = store.GetDB().Exec(`DELETE FROM xt_hyperopt_epochs WHERE id = ?`, e.id)
		})
	}

	list := func(r *gin.Engine, query string) []any {
		t.Helper()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/hyperopt/epochs"+query, nil)
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusOK, "list status "+query)
		var resp map[string]any
		assertTrue(t, json.Unmarshal(w.Body.Bytes(), &resp) == nil, "list parses")
		eps, _ := resp["epochs"].([]any)
		return eps
	}
	ids := func(eps []any) map[string]bool {
		out := map[string]bool{}
		for _, e := range eps {
			m := e.(map[string]any)
			out[m["id"].(string)] = true
		}
		return out
	}

	// 用户 1：本人 2 条 + 无属主 1 条；用户 2 的 lst-3 不可见
	eps := list(setupEpochRouter(1, "user"), "")
	got := ids(eps)
	assertEq(t, len(got), 3, "user1 sees own + ownerless")
	assertTrue(t, got["lst-1"] && got["lst-2"] && got["lst-4"], "expected ids")
	assertTrue(t, !got["lst-3"], "other user's epoch hidden")

	// job_id 过滤
	got = ids(list(setupEpochRouter(1, "user"), "?job_id=job-other"))
	assertEq(t, len(got), 1, "job filter")
	assertTrue(t, got["lst-4"], "job-other only ownerless row")

	// loss_max 过滤（HTTP 参数透传）
	got = ids(list(setupEpochRouter(1, "user"), "?loss_max=-1.5"))
	assertTrue(t, got["lst-1"] && got["lst-4"], "loss_max keeps matching")
	assertEq(t, len(got), 2, "loss_max count")

	// sharpe_min 过滤
	got = ids(list(setupEpochRouter(1, "user"), "?sharpe_min=2.0"))
	assertTrue(t, got["lst-1"] && !got["lst-2"], "sharpe_min")

	// job_id + loss_max + limit 组合
	got = ids(list(setupEpochRouter(1, "user"), "?job_id=job-lst&loss_max=-0.7&limit=10"))
	assertEq(t, len(got), 2, "combo count")
	assertTrue(t, got["lst-1"] && got["lst-2"], "combo ids")
}
