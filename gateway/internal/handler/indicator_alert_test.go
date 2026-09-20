package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/alerts"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 指标告警 REST 测试 ────────────────────────────────────────
//
// fake AlertScanService 记录 RunAlert/History；路由组带 main 同款
// AuthRequired 中间件，用 store.GenerateJWT 签发的真 token 过鉴权
// （沿用 grid 测试设施的模式，SECRET_KEY 已在 TestMain 的 InitDB 前注入）。

type fakeAlertScanService struct {
	runCalls     []runCall
	historyCalls []string
	result       *alerts.RunResult
	history      []alerts.TriggerRecord
}

type runCall struct {
	id              string
	respectCooldown bool
}

func (f *fakeAlertScanService) RunAlert(id string, respectCooldown bool) (*alerts.RunResult, error) {
	f.runCalls = append(f.runCalls, runCall{id, respectCooldown})
	if f.result != nil {
		return f.result, nil
	}
	return &alerts.RunResult{Matched: true, Value: 27.5, Triggered: true, Bars: 200}, nil
}

func (f *fakeAlertScanService) History(id string, limit int) []alerts.TriggerRecord {
	f.historyCalls = append(f.historyCalls, id)
	return f.history
}

func setupAlertRouter(t *testing.T) (*gin.Engine, *fakeAlertScanService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	fake := &fakeAlertScanService{}
	SetAlertScanService(fake)
	t.Cleanup(func() { SetAlertScanService(nil) })

	r := gin.New()
	g := r.Group("/api/alerts")
	g.Use(middleware.AuthRequired())
	{
		g.GET("/", IndicatorAlertList)
		g.POST("/", IndicatorAlertCreate)
		g.GET("/:id", IndicatorAlertGet)
		g.PUT("/:id", IndicatorAlertUpdate)
		g.DELETE("/:id", IndicatorAlertDelete)
		g.POST("/:id/enable", IndicatorAlertEnable)
		g.POST("/:id/disable", IndicatorAlertDisable)
		g.POST("/:id/run", IndicatorAlertRun)
		g.GET("/:id/history", IndicatorAlertHistory)
	}
	return r, fake
}

func alertToken(t *testing.T, uid int) string {
	t.Helper()
	db := store.GetDB()
	if db == nil {
		t.Fatal("store db not initialized")
	}
	username := fmt.Sprintf("alerttester%d", uid)
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO xt_users (id, username, password_hash, nickname, email, role, token_version) VALUES (?, ?, ?, ?, ?, ?, 1)`,
		uid, username, store.HashPassword("alert-pass-123"), "Alert Tester", fmt.Sprintf("alert%d@test.local", uid), "user",
	); err != nil {
		t.Fatalf("ensure alert test user: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE xt_users SET must_change_password=0, token_version=1, is_active=1, totp_enabled=0 WHERE id=?`, uid,
	); err != nil {
		t.Fatalf("reset alert test user flags: %v", err)
	}
	tok, err := store.GenerateJWT(uid, username, "user", 1)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}
	return tok
}

func alertDo(t *testing.T, r *gin.Engine, method, path string, body any, token string) (*httptest.ResponseRecorder, map[string]any) {
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
	var resp map[string]any
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return w, resp
}

func validAlertBody() map[string]any {
	return map[string]any{
		"name":             "RSI 超卖",
		"symbol":           "btc/usdt", // 故意带分隔符，normalize 后应为 BTCUSDT
		"interval":         "1h",
		"condition_expr":   "rsi14 < 30 && close > ema20",
		"message":          "关注反弹机会",
		"cooldown_minutes": 30,
	}
}

func TestIndicatorAlertCRUDLifecycle(t *testing.T) {
	r, _ := setupAlertRouter(t)
	tok := alertToken(t, 101)

	// Create
	w, resp := alertDo(t, r, "POST", "/api/alerts/", validAlertBody(), tok)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %v", w.Code, resp)
	}
	id, _ := resp["id"].(string)
	if id == "" {
		t.Fatalf("create: missing id in %v", resp)
	}
	if resp["symbol"] != "BTCUSDT" || resp["condition_expr"] != "rsi14 < 30 && close > ema20" {
		t.Errorf("create: normalization wrong %v", resp)
	}
	if resp["active"] != true {
		t.Errorf("create: default active should be true")
	}
	if resp["cooldown_minutes"] != float64(30) {
		t.Errorf("create: cooldown = %v", resp["cooldown_minutes"])
	}

	// Get
	w, resp = alertDo(t, r, "GET", "/api/alerts/"+id, nil, tok)
	if w.Code != http.StatusOK || resp["id"] != id {
		t.Errorf("get: %d %v", w.Code, resp)
	}

	// List（只含自己的）
	w, resp = alertDo(t, r, "GET", "/api/alerts/", nil, tok)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Errorf("list: items = %d, want 1", len(items))
	}

	// Update
	body := validAlertBody()
	body["name"] = "改名"
	body["cooldown_minutes"] = 0 // 0=不冷却，合法
	w, resp = alertDo(t, r, "PUT", "/api/alerts/"+id, body, tok)
	if w.Code != http.StatusOK || resp["name"] != "改名" {
		t.Errorf("update: %d %v", w.Code, resp)
	}
	if resp["cooldown_minutes"] != float64(0) {
		t.Errorf("update: cooldown = %v", resp["cooldown_minutes"])
	}

	// Disable → Enable
	w, resp = alertDo(t, r, "POST", "/api/alerts/"+id+"/disable", nil, tok)
	if w.Code != http.StatusOK || resp["active"] != false {
		t.Errorf("disable: %d %v", w.Code, resp)
	}
	w, resp = alertDo(t, r, "POST", "/api/alerts/"+id+"/enable", nil, tok)
	if w.Code != http.StatusOK || resp["active"] != true {
		t.Errorf("enable: %d %v", w.Code, resp)
	}

	// Delete
	w, _ = alertDo(t, r, "DELETE", "/api/alerts/"+id, nil, tok)
	if w.Code != http.StatusOK {
		t.Errorf("delete: %d", w.Code)
	}
	w, _ = alertDo(t, r, "GET", "/api/alerts/"+id, nil, tok)
	if w.Code != http.StatusNotFound {
		t.Errorf("get after delete: %d, want 404", w.Code)
	}
}

func TestIndicatorAlertValidation(t *testing.T) {
	r, _ := setupAlertRouter(t)
	tok := alertToken(t, 102)

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"empty name", func(b map[string]any) { b["name"] = "  " }},
		{"bad symbol", func(b map[string]any) { b["symbol"] = "!!" }},
		{"bad interval", func(b map[string]any) { b["interval"] = "7d" }},
		{"bad expr unknown fn", func(b map[string]any) { b["condition_expr"] = "kdi < 30" }},
		{"bad expr syntax", func(b map[string]any) { b["condition_expr"] = "rsi14 <" }},
		{"bad expr illegal char", func(b map[string]any) { b["condition_expr"] = "close > 1; drop table" }},
		{"cooldown negative", func(b map[string]any) { b["cooldown_minutes"] = -1 }},
		{"cooldown too large", func(b map[string]any) { b["cooldown_minutes"] = 20000 }},
		{"message too long", func(b map[string]any) {
			s := make([]byte, 501)
			for i := range s {
				s[i] = 'x'
			}
			b["message"] = string(s)
		}},
	}
	for _, tc := range cases {
		body := validAlertBody()
		tc.mutate(body)
		w, resp := alertDo(t, r, "POST", "/api/alerts/", body, tok)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400 (%v)", tc.name, w.Code, resp)
		}
	}
}

func TestIndicatorAlertRunAndForce(t *testing.T) {
	r, fake := setupAlertRouter(t)
	tok := alertToken(t, 103)
	w, resp := alertDo(t, r, "POST", "/api/alerts/", validAlertBody(), tok)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d", w.Code)
	}
	id, _ := resp["id"].(string)

	// 默认尊重冷却
	w, resp = alertDo(t, r, "POST", "/api/alerts/"+id+"/run", nil, tok)
	if w.Code != http.StatusOK {
		t.Fatalf("run: %d %v", w.Code, resp)
	}
	if resp["matched"] != true || resp["triggered"] != true || resp["value"] != 27.5 {
		t.Errorf("run result wrong: %v", resp)
	}
	if len(fake.runCalls) != 1 || fake.runCalls[0].respectCooldown != true {
		t.Errorf("run should respect cooldown by default: %+v", fake.runCalls)
	}

	// force=1 绕过冷却
	w, _ = alertDo(t, r, "POST", "/api/alerts/"+id+"/run?force=1", nil, tok)
	if w.Code != http.StatusOK {
		t.Fatalf("force run: %d", w.Code)
	}
	if len(fake.runCalls) != 2 || fake.runCalls[1].respectCooldown != false {
		t.Errorf("force run should bypass cooldown: %+v", fake.runCalls)
	}
}

func TestIndicatorAlertHistoryFallback(t *testing.T) {
	r, fake := setupAlertRouter(t)
	tok := alertToken(t, 104)

	// 服务无内存历史时，用库里的 last_triggered_at 兜底。
	fake.history = nil
	w, resp := alertDo(t, r, "POST", "/api/alerts/", validAlertBody(), tok)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d", w.Code)
	}
	id, _ := resp["id"].(string)
	rec, err := store.NewIndicatorAlertRepo().GetByID(id)
	if err != nil || rec == nil {
		t.Fatalf("load rec: %v", err)
	}
	rec.LastTriggeredAt = 1700000000000
	rec.LastValue = 27.5
	if err := store.NewIndicatorAlertRepo().MarkTriggered(id, 27.5, 1700000000000); err != nil {
		t.Fatalf("mark: %v", err)
	}

	w, resp = alertDo(t, r, "GET", "/api/alerts/"+id+"/history", nil, tok)
	if w.Code != http.StatusOK {
		t.Fatalf("history: %d", w.Code)
	}
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("history fallback: items = %v", resp)
	}
	entry, _ := items[0].(map[string]any)
	if entry["at"] != float64(1700000000000) || entry["value"] != 27.5 {
		t.Errorf("history fallback entry wrong: %v", entry)
	}
}

func TestIndicatorAlertOwnership(t *testing.T) {
	r, _ := setupAlertRouter(t)
	tok1 := alertToken(t, 105)
	tok2 := alertToken(t, 106)

	w, resp := alertDo(t, r, "POST", "/api/alerts/", validAlertBody(), tok1)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d", w.Code)
	}
	id, _ := resp["id"].(string)

	// 非属主读取 → 403
	w, _ = alertDo(t, r, "GET", "/api/alerts/"+id, nil, tok2)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-user get: %d, want 403", w.Code)
	}
	// 非属主删除 → 403，记录仍在
	w, _ = alertDo(t, r, "DELETE", "/api/alerts/"+id, nil, tok2)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-user delete: %d, want 403", w.Code)
	}
	w, _ = alertDo(t, r, "GET", "/api/alerts/"+id, nil, tok1)
	if w.Code != http.StatusOK {
		t.Errorf("owner get after cross delete: %d", w.Code)
	}
	// 列表隔离：user2 看不到 user1 的任务
	w, resp = alertDo(t, r, "GET", "/api/alerts/", nil, tok2)
	items, _ := resp["items"].([]any)
	if len(items) != 0 {
		t.Errorf("cross-user list leak: %d items", len(items))
	}
}

func TestIndicatorAlertRunServiceUnavailable(t *testing.T) {
	r, _ := setupAlertRouter(t)
	tok := alertToken(t, 107)
	w, resp := alertDo(t, r, "POST", "/api/alerts/", validAlertBody(), tok)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d", w.Code)
	}
	id, _ := resp["id"].(string)

	// 模拟 main 未注入扫描服务：run 应返回 503。
	SetAlertScanService(nil)
	t.Cleanup(func() { SetAlertScanService(nil) })
	w, _ = alertDo(t, r, "POST", "/api/alerts/"+id+"/run", nil, tok)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("run without service: %d, want 503", w.Code)
	}
}
