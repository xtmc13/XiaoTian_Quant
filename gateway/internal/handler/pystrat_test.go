package handler

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/pystrat"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Python 策略 REST 测试 ──────────────────────────────────────

// pyStratAdminToken 签发 role=admin 的测试 JWT（库内角色同步为 admin）。
func pyStratAdminToken(t *testing.T) string {
	t.Helper()
	db := store.GetDB()
	if db == nil {
		t.Fatal("store db not initialized")
	}
	const uid = 31337
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO xt_users (id, username, password_hash, nickname, email, role, token_version) VALUES (?, ?, ?, ?, ?, 'admin', 1)`,
		uid, "pystratadmin", store.HashPassword("pystrat-pass-123"), "PyStrat Admin", "pystrat-admin@test.local",
	); err != nil {
		t.Fatalf("ensure admin user: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE xt_users SET must_change_password=0, token_version=1, is_active=1, totp_enabled=0, role='admin' WHERE id=?`, uid,
	); err != nil {
		t.Fatalf("reset admin flags: %v", err)
	}
	tok, err := store.GenerateJWT(uid, "pystratadmin", "admin", 1)
	if err != nil {
		t.Fatalf("generate admin jwt: %v", err)
	}
	return tok
}

type fakePyStratService struct {
	mu       sync.Mutex
	running  map[string]bool
	started  []string
	stopped  []string
	startErr error
}

func newFakePyStratService() *fakePyStratService {
	return &fakePyStratService{running: map[string]bool{}}
}

func (f *fakePyStratService) Start(rec *store.PyStrategyRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.started = append(f.started, rec.ID)
	f.running[rec.ID] = true
	return nil
}

func (f *fakePyStratService) Stop(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, id)
	delete(f.running, id)
	return nil
}

func (f *fakePyStratService) IsRunning(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[id]
}

func (f *fakePyStratService) Status(id string) pystrat.StrategyStatus {
	return pystrat.StrategyStatus{Running: f.IsRunning(id)}
}

func (f *fakePyStratService) Logs(id string) []pystrat.LogEntry {
	return []pystrat.LogEntry{}
}

// setupPyStratRouter 注册带 AuthRequired 的 /api/pystrates 路由并注入 fake。
func setupPyStratRouter(t *testing.T) (*gin.Engine, *fakePyStratService) {
	t.Helper()
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	fake := newFakePyStratService()
	SetPyStratService(fake)
	t.Cleanup(func() { SetPyStratService(nil) })

	r := gin.New()
	g := r.Group("/api/pystrategies")
	g.Use(middleware.AuthRequired())
	{
		g.GET("/", PyStratList)
		g.POST("/", PyStratCreate)
		g.GET("/:id", PyStratGet)
		g.PUT("/:id", PyStratUpdate)
		g.DELETE("/:id", PyStratDelete)
		g.POST("/:id/validate", PyStratValidate)
		g.POST("/:id/start", PyStratStart)
		g.POST("/:id/stop", PyStratStop)
		g.GET("/:id/logs", PyStratLogs)
		g.GET("/:id/status", PyStratStatus)
	}
	return r, fake
}

func createPyStrat(t *testing.T, r *gin.Engine, token string, body map[string]any) map[string]any {
	t.Helper()
	w, resp := gridDo(t, r, "POST", "/api/pystrategies/", body, token)
	if w.Code != 200 {
		t.Fatalf("create: status %d resp %v", w.Code, resp)
	}
	return resp
}

const pyStratTestCode = `import math

STRATEGY_MANIFEST = {"name": "t", "symbol": "BTC/USDT", "interval": "15m", "direction": "long", "params": {}}

def initialize(context):
    pass

def on_bar(context, bar):
    pass
`

// 所有权：他人策略 get/start/validate → 403；列表不泄漏他人策略。
func TestPyStratOwnership(t *testing.T) {
	r, _ := setupPyStratRouter(t)
	ownerTok := gridToken(t, 101)
	otherTok := gridToken(t, 202)

	created := createPyStrat(t, r, ownerTok, map[string]any{
		"name": "s1", "symbol": "BTC/USDT", "interval": "15m", "direction": "long",
		"code": pyStratTestCode, "params_json": "{}",
	})
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("no id: %v", created)
	}

	// 他人 403
	for _, path := range []string{"/api/pystrategies/" + id, "/api/pystrategies/" + id + "/status"} {
		w, _ := gridDo(t, r, "GET", path, nil, otherTok)
		if w.Code != 403 {
			t.Fatalf("GET %s by other: status %d", path, w.Code)
		}
	}
	w, _ := gridDo(t, r, "POST", "/api/pystrategies/"+id+"/start", nil, otherTok)
	if w.Code != 403 {
		t.Fatalf("start by other: status %d", w.Code)
	}
	w, _ = gridDo(t, r, "POST", "/api/pystrategies/"+id+"/validate", nil, otherTok)
	if w.Code != 403 {
		t.Fatalf("validate by other: status %d", w.Code)
	}
	w, _ = gridDo(t, r, "DELETE", "/api/pystrategies/"+id, nil, otherTok)
	if w.Code != 403 {
		t.Fatalf("delete by other: status %d", w.Code)
	}

	// 他人列表看不到
	w, resp := gridDo(t, r, "GET", "/api/pystrategies/", nil, otherTok)
	if w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
	items, _ := resp["strategies"].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["id"] == id {
			t.Fatal("other user's list leaked the strategy")
		}
	}

	// admin（role=admin 的 JWT + 库内 role=admin）可见
	adminTok := pyStratAdminToken(t)
	w, _ = gridDo(t, r, "GET", "/api/pystrategies/"+id, nil, adminTok)
	if w.Code != 200 {
		t.Fatalf("admin get: %d", w.Code)
	}
	// 属主可见
	w, _ = gridDo(t, r, "GET", "/api/pystrategies/"+id, nil, ownerTok)
	if w.Code != 200 {
		t.Fatalf("owner get: %d", w.Code)
	}
}

// validate：静态校验报行号 + 沙箱错误通道。
func TestPyStratValidateEndpoint(t *testing.T) {
	r, _ := setupPyStratRouter(t)
	tok := gridToken(t, 303)

	badCode := "import os\n" + pyStratTestCode
	created := createPyStrat(t, r, tok, map[string]any{
		"name": "bad", "symbol": "BTC/USDT", "interval": "15m", "direction": "long",
		"code": badCode,
	})
	id, _ := created["id"].(string)

	w, resp := gridDo(t, r, "POST", "/api/pystrategies/"+id+"/validate", nil, tok)
	if w.Code != 200 {
		t.Fatalf("validate: %d %v", w.Code, resp)
	}
	if resp["valid"] != false {
		t.Fatalf("expected valid=false: %v", resp)
	}
	issues, _ := resp["issues"].([]any)
	if len(issues) == 0 {
		t.Fatal("expected issues with line numbers")
	}
	first, _ := issues[0].(map[string]any)
	if first["line"].(float64) != 1 {
		t.Fatalf("import os is line 1, got %v", first["line"])
	}
	if _, hasSandbox := resp["sandbox_error"]; !hasSandbox {
		t.Fatal("sandbox layer must also reject import os")
	}

	// 合法代码 → valid=true（沙箱真实加载）
	created2 := createPyStrat(t, r, tok, map[string]any{
		"name": "good", "symbol": "BTC/USDT", "interval": "15m", "direction": "long",
		"code": pyStratTestCode,
	})
	id2, _ := created2["id"].(string)
	w, resp = gridDo(t, r, "POST", "/api/pystrategies/"+id2+"/validate", nil, tok)
	if w.Code != 200 || resp["valid"] != true {
		t.Fatalf("expected valid=true: %d %v", w.Code, resp)
	}
}

// start/stop 生命周期 + 运行中 409 + 启动失败回写 error。
func TestPyStratStartStopLifecycle(t *testing.T) {
	r, fake := setupPyStratRouter(t)
	tok := gridToken(t, 404)
	created := createPyStrat(t, r, tok, map[string]any{
		"name": "s", "symbol": "BTC/USDT", "interval": "15m", "direction": "long",
		"code": pyStratTestCode,
	})
	id, _ := created["id"].(string)

	w, resp := gridDo(t, r, "POST", "/api/pystrategies/"+id+"/start", nil, tok)
	if w.Code != 200 || resp["started"] != true {
		t.Fatalf("start: %d %v", w.Code, resp)
	}
	if len(fake.started) != 1 {
		t.Fatal("fake service must receive Start")
	}

	// 运行中再 start → 409
	w, _ = gridDo(t, r, "POST", "/api/pystrategies/"+id+"/start", nil, tok)
	if w.Code != 409 {
		t.Fatalf("double start: %d", w.Code)
	}
	// 未运行 stop → 409
	fake.Stop(id)
	w, _ = gridDo(t, r, "POST", "/api/pystrategies/"+id+"/stop", nil, tok)
	if w.Code != 409 {
		t.Fatalf("stop when not running: %d", w.Code)
	}

	// 启动失败 → 400 + error 落库
	fake.startErr = errors.New("实盘交易未启用")
	created2 := createPyStrat(t, r, tok, map[string]any{
		"name": "s2", "symbol": "ETH/USDT", "interval": "15m", "direction": "long",
		"code": pyStratTestCode, "paper": false,
	})
	id2, _ := created2["id"].(string)
	w, resp = gridDo(t, r, "POST", "/api/pystrategies/"+id2+"/start", nil, tok)
	if w.Code != 400 {
		t.Fatalf("failed start: %d", w.Code)
	}
	if !strings.Contains(resp["message"].(string), "实盘交易未启用") {
		t.Fatalf("error message: %v", resp)
	}
	rec, err := pyStratRepo.GetByID(id2)
	if err != nil || rec == nil || !strings.Contains(rec.Error, "实盘交易未启用") {
		t.Fatalf("error not persisted: %v %+v", err, rec)
	}
}

// 创建/更新自动打版本快照（xt_strategy_versions）。
func TestPyStratVersionSnapshotOnSave(t *testing.T) {
	r, _ := setupPyStratRouter(t)
	tok := gridToken(t, 505)
	created := createPyStrat(t, r, tok, map[string]any{
		"name": "v", "symbol": "BTC/USDT", "interval": "15m", "direction": "long",
		"code": pyStratTestCode,
	})
	if created["version"].(float64) != 1 {
		t.Fatalf("create snapshot version = %v want 1", created["version"])
	}
	id, _ := created["id"].(string)

	w, resp := gridDo(t, r, "PUT", "/api/pystrategies/"+id, map[string]any{
		"name": "v2", "symbol": "BTC/USDT", "interval": "15m", "direction": "long",
		"code": pyStratTestCode + "\n# touched\n",
	}, tok)
	if w.Code != 200 {
		t.Fatalf("update: %d %v", w.Code, resp)
	}
	if resp["version"].(float64) != 2 {
		t.Fatalf("update snapshot version = %v want 2", resp["version"])
	}
	vers, err := pyStratVersionRepo.ListByStrategy(id)
	if err != nil || len(vers) != 2 {
		t.Fatalf("versions: %v len=%d", err, len(vers))
	}
}

// 参数校验：缺 name / 非法 params_json / 超长 direction。
func TestPyStratCreateValidation(t *testing.T) {
	r, _ := setupPyStratRouter(t)
	tok := gridToken(t, 606)

	w, _ := gridDo(t, r, "POST", "/api/pystrategies/", map[string]any{
		"symbol": "BTC/USDT", "code": pyStratTestCode,
	}, tok)
	if w.Code != 400 {
		t.Fatalf("missing name: %d", w.Code)
	}

	w, _ = gridDo(t, r, "POST", "/api/pystrategies/", map[string]any{
		"name": "x", "symbol": "BTC/USDT", "code": pyStratTestCode, "params_json": "{bad",
	}, tok)
	if w.Code != 400 {
		t.Fatalf("bad params_json: %d", w.Code)
	}

	w, _ = gridDo(t, r, "POST", "/api/pystrategies/", map[string]any{
		"name": "x", "symbol": "BTC/USDT", "code": pyStratTestCode, "direction": "sideways",
	}, tok)
	if w.Code != 400 {
		t.Fatalf("bad direction: %d", w.Code)
	}
}
