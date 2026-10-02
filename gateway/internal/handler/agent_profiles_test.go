package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentmemory"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── /agent/profiles + /agent/files + 清单角色过滤的契约测试（冻结契约，前端按此对接）──

func registerProfileTestRoutes(r *gin.Engine, uid int, role string) {
	h := func(c *gin.Context) {
		if uid > 0 {
			c.Set(middleware.UserIDKey, uid)
		}
		c.Set(middleware.RoleKey, role)
	}
	g := r.Group("/agent", h)
	g.GET("/profiles", AgentProfilesList)
	g.POST("/profiles", AgentProfileCreate)
	g.POST("/profiles/:id/activate", AgentProfileActivate)
	g.DELETE("/profiles/:id", AgentProfileDelete)
	g.GET("/files/checkpoints", AgentFileCheckpoints)
	g.POST("/files/rollback", AgentFileRollback)
	g.GET("/memory", AgentMemoryList)
	g.POST("/memory", AgentMemoryCreate)
	g.GET("/plugins", AgentPluginsManifest)
}

func doProfileRequest(t *testing.T, uid int, role, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	registerProfileTestRoutes(r, uid, role)
	var reader *bytes.Reader
	if body != nil {
		bs, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(bs)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func profileBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	return body
}

func profileList(t *testing.T, body map[string]any) []any {
	t.Helper()
	profiles, _ := body["profiles"].([]any)
	if len(profiles) == 0 {
		t.Fatalf("profiles 为空: %v", body)
	}
	return profiles
}

func TestAgentProfilesLifecycle(t *testing.T) {
	uid := 901

	// 惰性默认档案（契约字段逐项校验）
	w := doProfileRequest(t, uid, "user", http.MethodGet, "/agent/profiles", nil)
	assertEq(t, w.Code, http.StatusOK, "list status")
	body := profileBody(t, w)
	if body["success"] != true {
		t.Fatalf("list success = %v", body["success"])
	}
	if _, leaked := body["admin_all"]; leaked {
		t.Error("普通用户不应带 admin_all")
	}
	profiles := profileList(t, body)
	if len(profiles) != 1 {
		t.Fatalf("默认档案数 = %d, want 1", len(profiles))
	}
	p0, _ := profiles[0].(map[string]any)
	if p0["name"] != "默认" || p0["is_active"] != true || p0["is_default"] != true {
		t.Fatalf("默认档案字段: %v", p0)
	}
	if _, ok := p0["id"].(float64); !ok {
		t.Fatalf("档案 id: %v", p0)
	}
	if _, ok := p0["created_at"].(float64); !ok {
		t.Errorf("缺 created_at: %v", p0)
	}
	defaultID := int64(p0["id"].(float64))

	// 创建（契约：{"success":true,"id":N}）
	w = doProfileRequest(t, uid, "user", http.MethodPost, "/agent/profiles", map[string]any{"name": "工作"})
	assertEq(t, w.Code, http.StatusOK, "create status")
	body = profileBody(t, w)
	if body["success"] != true {
		t.Fatalf("create success = %v", body["success"])
	}
	workID := int64(body["id"].(float64))
	if workID == 0 || workID == defaultID {
		t.Fatalf("create id = %d", workID)
	}

	// 激活（契约：{"success":true}）
	w = doProfileRequest(t, uid, "user", http.MethodPost, "/agent/profiles/"+pidStr(workID)+"/activate", nil)
	assertEq(t, w.Code, http.StatusOK, "activate status")
	if profileBody(t, w)["success"] != true {
		t.Fatal("activate success != true")
	}
	body = profileBody(t, doProfileRequest(t, uid, "user", http.MethodGet, "/agent/profiles", nil))
	for _, p := range profileList(t, body) {
		pm, _ := p.(map[string]any)
		if int64(pm["id"].(float64)) == workID && pm["is_active"] != true {
			t.Fatalf("工作档案未激活: %v", pm)
		}
	}

	// 默认档案不可删（400）
	w = doProfileRequest(t, uid, "user", http.MethodDelete, "/agent/profiles/"+pidStr(defaultID), nil)
	assertEq(t, w.Code, http.StatusBadRequest, "删默认档案")

	// 删除激活档案：激活回落默认
	w = doProfileRequest(t, uid, "user", http.MethodDelete, "/agent/profiles/"+pidStr(workID), nil)
	assertEq(t, w.Code, http.StatusOK, "删激活档案")
	body = profileBody(t, doProfileRequest(t, uid, "user", http.MethodGet, "/agent/profiles", nil))
	profiles = profileList(t, body)
	if len(profiles) != 1 {
		t.Fatalf("删除后档案数 = %d, want 1", len(profiles))
	}
	p0, _ = profiles[0].(map[string]any)
	if p0["is_active"] != true || int64(p0["id"].(float64)) != defaultID {
		t.Fatalf("激活未回落默认: %v", p0)
	}

	// 不存在/他人档案 404
	w = doProfileRequest(t, uid, "user", http.MethodPost, "/agent/profiles/999999/activate", nil)
	assertEq(t, w.Code, http.StatusNotFound, "激活不存在档案")
	w = doProfileRequest(t, uid, "user", http.MethodDelete, "/agent/profiles/999999", nil)
	assertEq(t, w.Code, http.StatusNotFound, "删除不存在档案")
}

func TestAgentProfilesAdmin(t *testing.T) {
	// admin_all 仅管理员可见，且管理员可删任意用户的非默认档案
	mkUser := func(username, role string) int {
		res, err := store.GetDB().Exec(`INSERT INTO xt_users (username, password_hash, role) VALUES (?,?,?)`, username, "x", role)
		if err != nil {
			t.Fatalf("insert user: %v", err)
		}
		id, _ := res.LastInsertId()
		return int(id)
	}
	uidUser := mkUser("prof_rest_user", "user")
	uidAdmin := mkUser("prof_rest_admin", "admin")

	w := doProfileRequest(t, uidUser, "user", http.MethodPost, "/agent/profiles", map[string]any{"name": "用户B的档案"})
	userProfileID := int64(profileBody(t, w)["id"].(float64))

	w = doProfileRequest(t, uidAdmin, "admin", http.MethodGet, "/agent/profiles", nil)
	assertEq(t, w.Code, http.StatusOK, "admin list status")
	body := profileBody(t, w)
	adminAll, ok := body["admin_all"].([]any)
	if !ok || len(adminAll) == 0 {
		t.Fatalf("管理员应带 admin_all: %v", body)
	}
	// admin_all 元素契约：{"user_id","username","profiles":[...]}
	found := false
	for _, u := range adminAll {
		um, _ := u.(map[string]any)
		if _, ok := um["username"].(string); !ok {
			t.Errorf("admin_all 缺 username: %v", um)
		}
		if int64(um["user_id"].(float64)) == int64(uidAdmin) {
			found = true
			ps, _ := um["profiles"].([]any)
			if len(ps) == 0 {
				t.Errorf("admin_all 中管理员档案为空")
			}
		}
	}
	if !found {
		t.Error("admin_all 缺少管理员本人")
	}

	// 管理员删他人非默认档案 → 200
	w = doProfileRequest(t, uidAdmin, "admin", http.MethodDelete, "/agent/profiles/"+pidStr(userProfileID), nil)
	assertEq(t, w.Code, http.StatusOK, "管理员删他人档案")
	// 他人视角已消失
	body = profileBody(t, doProfileRequest(t, uidUser, "user", http.MethodGet, "/agent/profiles", nil))
	if len(profileList(t, body)) != 1 {
		t.Fatal("被删档案仍可见")
	}
	// 管理员删他人默认档案 → 400
	body = profileBody(t, doProfileRequest(t, uidUser, "user", http.MethodGet, "/agent/profiles", nil))
	defID := int64(profileList(t, body)[0].(map[string]any)["id"].(float64))
	w = doProfileRequest(t, uidAdmin, "admin", http.MethodDelete, "/agent/profiles/"+pidStr(defID), nil)
	assertEq(t, w.Code, http.StatusBadRequest, "管理员删他人默认档案")
}

// 档案隔离：记忆在档案 A 写入后，切到档案 B 不可见；全局行（profile_id=0）两个档案都可见。
func TestAgentProfilesMemoryScoping(t *testing.T) {
	uid := 904

	// 默认档案 A 激活：REST 创建的记忆落入 A
	w := doProfileRequest(t, uid, "user", http.MethodPost, "/agent/memory", map[string]any{"content": "档案A的记忆"})
	assertEq(t, w.Code, http.StatusOK, "create memory A")
	// 全局行（profile_id=0，历史数据语义）
	if err := agentmemory.NewRepo().Create(&agentmemory.Memory{
		ID: agentmemory.NewID(), UserID: int64(uid), Content: "全局记忆", ProfileID: 0,
	}); err != nil {
		t.Fatal(err)
	}

	names := func() []string {
		body := profileBody(t, doProfileRequest(t, uid, "user", http.MethodGet, "/agent/memory", nil))
		mems, _ := body["memories"].([]any)
		out := []string{}
		for _, m := range mems {
			mm, _ := m.(map[string]any)
			out = append(out, mm["content"].(string))
		}
		return out
	}
	contains := func(list []string, s string) bool {
		for _, v := range list {
			if v == s {
				return true
			}
		}
		return false
	}

	got := names()
	if !contains(got, "档案A的记忆") || !contains(got, "全局记忆") {
		t.Fatalf("档案A应见 A记忆+全局: %v", got)
	}

	// 建档案 B 并激活：A 的记忆不可见，全局仍可见；新记忆落入 B
	w = doProfileRequest(t, uid, "user", http.MethodPost, "/agent/profiles", map[string]any{"name": "B"})
	idB := int64(profileBody(t, w)["id"].(float64))
	doProfileRequest(t, uid, "user", http.MethodPost, "/agent/profiles/"+pidStr(idB)+"/activate", nil)
	w = doProfileRequest(t, uid, "user", http.MethodPost, "/agent/memory", map[string]any{"content": "档案B的记忆"})
	assertEq(t, w.Code, http.StatusOK, "create memory B")

	got = names()
	if contains(got, "档案A的记忆") {
		t.Errorf("档案B不应见 A记忆: %v", got)
	}
	if !contains(got, "档案B的记忆") || !contains(got, "全局记忆") {
		t.Errorf("档案B应见 B记忆+全局: %v", got)
	}
}

func TestAgentFileEndpointsAdminOnly(t *testing.T) {
	// 普通用户 403
	w := doProfileRequest(t, 905, "user", http.MethodGet, "/agent/files/checkpoints", nil)
	assertEq(t, w.Code, http.StatusForbidden, "checkpoints 普通用户")
	w = doProfileRequest(t, 905, "user", http.MethodPost, "/agent/files/rollback", map[string]any{"checkpoint_id": "cp_x"})
	assertEq(t, w.Code, http.StatusForbidden, "rollback 普通用户")

	// 管理员 200，契约 {"success":true,"checkpoints":[...]}
	w = doProfileRequest(t, 906, "admin", http.MethodGet, "/agent/files/checkpoints", nil)
	assertEq(t, w.Code, http.StatusOK, "checkpoints 管理员")
	body := profileBody(t, w)
	if body["success"] != true {
		t.Fatalf("checkpoints success = %v", body["success"])
	}
	if _, ok := body["checkpoints"].([]any); !ok {
		t.Fatalf("checkpoints 字段缺失: %v", body)
	}
	// 不存在的检查点 404
	w = doProfileRequest(t, 906, "admin", http.MethodPost, "/agent/files/rollback", map[string]any{"checkpoint_id": "cp_404"})
	assertEq(t, w.Code, http.StatusNotFound, "rollback 不存在检查点")
}

func TestAgentPluginsManifestRoleFilter(t *testing.T) {
	hasFilesNav := func(body map[string]any) bool {
		plugins, _ := body["plugins"].([]any)
		for _, p := range plugins {
			pm, _ := p.(map[string]any)
			ui, _ := pm["ui"].(map[string]any)
			nav, _ := ui["nav"].(map[string]any)
			if nav != nil && nav["id"] == "files" {
				if nav["label"] != "文件回滚" || nav["icon"] != "rollback" {
					t.Errorf("files 导航字段: %v", nav)
				}
				return true
			}
		}
		return false
	}

	w := doProfileRequest(t, 907, "admin", http.MethodGet, "/agent/plugins", nil)
	assertEq(t, w.Code, http.StatusOK, "manifest admin")
	if !hasFilesNav(profileBody(t, w)) {
		t.Error("管理员清单应含 files 导航")
	}
	w = doProfileRequest(t, 907, "user", http.MethodGet, "/agent/plugins", nil)
	assertEq(t, w.Code, http.StatusOK, "manifest user")
	if hasFilesNav(profileBody(t, w)) {
		t.Error("普通用户清单不应含 files 导航")
	}
}

// token 属主角色过滤：X-Agent-Token 路径按属主查库取角色（RBAC 门控输入）。
func TestAgentRequestRoleTokenOwner(t *testing.T) {
	mkUser := func(username, role string) int64 {
		res, err := store.GetDB().Exec(`INSERT INTO xt_users (username, password_hash, role) VALUES (?,?,?)`, username, "x", role)
		if err != nil {
			t.Fatalf("insert user: %v", err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	adminID := mkUser("rbac_token_admin", "admin")
	userID := mkUser("rbac_token_user", "user")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := agentRequestRole(c, uint64(adminID), true); got != "admin" {
		t.Errorf("token 属主 admin 角色 = %q", got)
	}
	if got := agentRequestRole(c, uint64(userID), true); got != "user" {
		t.Errorf("token 属主 user 角色 = %q", got)
	}
	if got := agentRequestRole(c, 99999999, true); got != "" {
		t.Errorf("不存在用户应按普通用户处理 = %q", got)
	}
	// JWT 路径读鉴权中间件写入的 claims
	c.Set(middleware.RoleKey, "admin")
	if got := agentRequestRole(c, uint64(userID), false); got != "admin" {
		t.Errorf("JWT 路径角色 = %q", got)
	}
}

func pidStr(v int64) string {
	return strconv.FormatInt(v, 10)
}
