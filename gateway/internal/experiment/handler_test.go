package experiment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

// resetExperimentStates clears the package-level state map between tests.
func resetExperimentStates() {
	experimentStatesMu.Lock()
	defer experimentStatesMu.Unlock()
	experimentStates = make(map[string]*ExperimentResult)
}

func seedExperiment(id string, userID int64, withResults bool) {
	res := &ExperimentResult{
		ExperimentID: id,
		Name:         "实验 " + id,
		Status:       "completed",
		CreatedAt:    time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
		DurationMs:   1234,
		BestScore:    0.87,
		UserID:       userID,
	}
	if withResults {
		res.ISResult = &backtest.RunResult{TotalReturnPct: 12.5, MaxDrawdownPct: 3.2}
		res.OOSResult = &backtest.RunResult{TotalReturnPct: 8.1, MaxDrawdownPct: 4.0}
	}
	SetExperimentState(id, res)
}

func listRequest(t *testing.T, setAuth func(*gin.Context)) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/experiments", nil)
	c.Request = req
	if setAuth != nil {
		setAuth(c)
	}
	ExperimentListHandler(c)

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON response: %v (%s)", err, w.Body.String())
	}
	return w, body
}

func itemIDs(t *testing.T, body map[string]any) []string {
	t.Helper()
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("items missing or wrong type: %#v", body)
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("item wrong type: %#v", it)
		}
		id, _ := m["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestExperimentListHandlerFiltersByOwner(t *testing.T) {
	resetExperimentStates()
	seedExperiment("exp_own", 1, true)
	seedExperiment("exp_other", 2, false)
	seedExperiment("exp_legacy", 0, false) // 无属主：对所有登录用户可见

	w, body := listRequest(t, func(c *gin.Context) {
		c.Set(middleware.UserIDKey, 1)
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	ids := itemIDs(t, body)
	if len(ids) != 2 || !contains(ids, "exp_own") || !contains(ids, "exp_legacy") {
		t.Fatalf("expected [exp_own exp_legacy], got %v", ids)
	}
	if contains(ids, "exp_other") {
		t.Fatalf("other user's experiment leaked: %v", ids)
	}
	if body["total"].(float64) != 2 {
		t.Fatalf("total = %v, want 2", body["total"])
	}
}

func TestExperimentListHandlerAdminSeesAll(t *testing.T) {
	resetExperimentStates()
	seedExperiment("exp_u1", 1, false)
	seedExperiment("exp_u2", 2, false)

	_, body := listRequest(t, func(c *gin.Context) {
		c.Set(middleware.UserIDKey, 9)
		c.Set(middleware.RoleKey, "admin")
	})
	ids := itemIDs(t, body)
	if len(ids) != 2 {
		t.Fatalf("admin should see all experiments, got %v", ids)
	}
}

func TestExperimentListHandlerLegacyNoAuthHeader(t *testing.T) {
	resetExperimentStates()
	seedExperiment("exp_a", 1, false)

	// 未注入用户（内部调用/旧行为）：不过滤
	_, body := listRequest(t, nil)
	if ids := itemIDs(t, body); len(ids) != 1 {
		t.Fatalf("expected 1 experiment without auth injection, got %v", ids)
	}
}

func TestExperimentListHandlerItemShape(t *testing.T) {
	resetExperimentStates()
	seedExperiment("exp_shape", 1, true)

	_, body := listRequest(t, func(c *gin.Context) {
		c.Set(middleware.UserIDKey, 1)
	})
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0].(map[string]any)

	for _, key := range []string{"id", "experiment_id", "name", "status", "best_score", "duration_ms", "created_at", "user_id", "is_return_pct", "oos_return_pct"} {
		if _, ok := item[key]; !ok {
			t.Fatalf("item missing key %q: %#v", key, item)
		}
	}
	if item["id"] != "exp_shape" || item["experiment_id"] != "exp_shape" {
		t.Fatalf("id mismatch: %#v", item)
	}
	if item["name"] != "实验 exp_shape" {
		t.Fatalf("name mismatch: %#v", item["name"])
	}
	if item["status"] != "completed" {
		t.Fatalf("status mismatch: %#v", item["status"])
	}
	if item["is_return_pct"] != 12.5 || item["oos_return_pct"] != 8.1 {
		t.Fatalf("metrics summary mismatch: %#v", item)
	}
	if item["created_at"] != "2026-09-19T12:00:00Z" {
		t.Fatalf("created_at mismatch: %#v", item["created_at"])
	}
}

func TestExperimentListHandlerEmpty(t *testing.T) {
	resetExperimentStates()
	_, body := listRequest(t, func(c *gin.Context) {
		c.Set(middleware.UserIDKey, 1)
	})
	items, ok := body["items"].([]any)
	if !ok || len(items) != 0 {
		t.Fatalf("expected empty items array, got %#v", body)
	}
	if body["total"].(float64) != 0 {
		t.Fatalf("total = %v, want 0", body["total"])
	}
}
