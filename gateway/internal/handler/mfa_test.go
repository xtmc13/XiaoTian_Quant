package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/mfa"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── A3.1 MFA 登录流程集成测试 ──────────────────────────────────

func postJSON(t *testing.T, r *gin.Engine, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp
}

func mfaTestUser(t *testing.T, username string) int {
	t.Helper()
	uid, err := store.CreateUser(username, "password123", "MFA", username+"@test.local", "user")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return uid
}

func mfaTestRouter() *gin.Engine {
	r := setupRouter()
	r.POST("/auth/login", Login)
	r.POST("/auth/mfa/setup", middleware.AuthRequired(), MFASetup)
	r.POST("/auth/mfa/enable", middleware.AuthRequired(), MFAEnable)
	r.POST("/auth/mfa/disable", middleware.AuthRequired(), MFADisable)
	r.POST("/auth/mfa/verify", MFAVerify)
	return r
}

// TestMFALoginFlow: 无 MFA 直接登录 → setup → enable（错码拒绝/对码通过+备用码）
// → 登录转 MFA 第二步 → verify（错码拒绝/TOTP 通过/备用码通过/备用码一次性）
// → disable（需验证 TOTP）→ 恢复直接登录。
func TestMFALoginFlow(t *testing.T) {
	r := mfaTestRouter()
	uid := mfaTestUser(t, "mfaflow")
	userRow := store.FindUserByUsername("mfaflow")
	tok, _ := store.GenerateJWT(uid, "mfaflow", "user", userRow["token_version"].(int))

	// 1) 未启用 MFA：密码正确直接拿到正式令牌。
	code, resp := postJSON(t, r, "/auth/login", gin.H{"username": "mfaflow", "password": "password123"}, "")
	assertEq(t, code, http.StatusOK, "login without MFA")
	if resp["access_token"] == nil || resp["access_token"] == "" {
		t.Fatalf("expected access_token, got %v", resp)
	}

	// 2) setup：返回 secret + otpauth URI，并暂存到用户。
	code, resp = postJSON(t, r, "/auth/mfa/setup", gin.H{}, tok)
	assertEq(t, code, http.StatusOK, "mfa setup")
	secret, _ := resp["secret"].(string)
	if secret == "" || resp["otpauth_uri"] == nil {
		t.Fatalf("setup should return secret and otpauth_uri, got %v", resp)
	}
	stored, enabled, _ := store.GetUserTOTP(uid)
	if stored != secret || enabled {
		t.Fatalf("setup should store secret un-enabled, stored=%q enabled=%v", stored, enabled)
	}

	// 3) enable：错误 TOTP 码拒绝。
	code, _ = postJSON(t, r, "/auth/mfa/enable", gin.H{"code": "000000"}, tok)
	assertEq(t, code, http.StatusBadRequest, "enable with wrong code must be 400")

	// 4) enable：正确 TOTP 码通过，返回 8 个一次性备用码。
	good, err := mfa.CodeAt(secret, time.Now())
	if err != nil {
		t.Fatalf("CodeAt: %v", err)
	}
	code, resp = postJSON(t, r, "/auth/mfa/enable", gin.H{"code": good}, tok)
	assertEq(t, code, http.StatusOK, "enable with good code")
	backupRaw, ok := resp["backup_codes"].([]any)
	if !ok || len(backupRaw) != 8 {
		t.Fatalf("enable should return 8 backup codes, got %v", resp["backup_codes"])
	}
	backup := make([]string, 0, len(backupRaw))
	for _, b := range backupRaw {
		backup = append(backup, b.(string))
	}
	if _, enabled, _ := store.GetUserTOTP(uid); !enabled {
		t.Fatal("MFA should be enabled after enable")
	}

	// 5) 登录进入第二步：返回 mfa_required + 短时效临时令牌，而非正式令牌。
	code, resp = postJSON(t, r, "/auth/login", gin.H{"username": "mfaflow", "password": "password123"}, "")
	assertEq(t, code, http.StatusOK, "login with MFA step one")
	if resp["mfa_required"] != true || resp["mfa_token"] == nil || resp["access_token"] != nil {
		t.Fatalf("expected mfa_required + mfa_token only, got %v", resp)
	}
	mfaToken, _ := resp["mfa_token"].(string)

	// 6) verify：错误码拒绝。
	code, _ = postJSON(t, r, "/auth/mfa/verify", gin.H{"mfa_token": mfaToken, "code": "123456"}, "")
	assertEq(t, code, http.StatusBadRequest, "verify with wrong code must be 400")

	// 7) verify：正确 TOTP 码换正式 JWT。
	code, resp = postJSON(t, r, "/auth/mfa/verify", gin.H{"mfa_token": mfaToken, "code": good}, "")
	assertEq(t, code, http.StatusOK, "verify with TOTP code")
	if resp["access_token"] == nil || resp["access_token"] == "" {
		t.Fatalf("verify should return access_token, got %v", resp)
	}

	// 8) verify：备用码可用，但只能一次性。
	code, resp = postJSON(t, r, "/auth/login", gin.H{"username": "mfaflow", "password": "password123"}, "")
	mfaToken, _ = resp["mfa_token"].(string)
	code, resp = postJSON(t, r, "/auth/mfa/verify", gin.H{"mfa_token": mfaToken, "code": backup[0]}, "")
	assertEq(t, code, http.StatusOK, "verify with backup code")
	if resp["access_token"] == nil {
		t.Fatalf("backup code verify should return access_token, got %v", resp)
	}
	code, resp = postJSON(t, r, "/auth/login", gin.H{"username": "mfaflow", "password": "password123"}, "")
	mfaToken, _ = resp["mfa_token"].(string)
	code, _ = postJSON(t, r, "/auth/mfa/verify", gin.H{"mfa_token": mfaToken, "code": backup[0]}, "")
	assertEq(t, code, http.StatusBadRequest, "reused backup code must be 400")

	// 9) disable：需验证 TOTP，错误码拒绝、正确码通过。
	code, _ = postJSON(t, r, "/auth/mfa/disable", gin.H{"code": "000000"}, tok)
	assertEq(t, code, http.StatusBadRequest, "disable with wrong code must be 400")
	code, _ = postJSON(t, r, "/auth/mfa/disable", gin.H{"code": good}, tok)
	assertEq(t, code, http.StatusOK, "disable with good code")
	if _, enabled, _ := store.GetUserTOTP(uid); enabled {
		t.Fatal("MFA should be disabled")
	}

	// 10) 关闭后恢复直接登录。
	code, resp = postJSON(t, r, "/auth/login", gin.H{"username": "mfaflow", "password": "password123"}, "")
	assertEq(t, code, http.StatusOK, "login after disable")
	if resp["access_token"] == nil || resp["mfa_required"] != nil {
		t.Fatalf("direct login expected after disable, got %v", resp)
	}
}

// ── A3.3 token_version 吊销测试 ────────────────────────────────

// TestTokenVersionRevokedByMiddleware: 改密使 tv+1 后，旧 tv 的 JWT 必须被
// 鉴权中间件拒绝（401 Token revoked），新 tv 的 JWT 放行。
func TestTokenVersionRevokedByMiddleware(t *testing.T) {
	uid := mfaTestUser(t, "tvrevoke")

	r := setupRouter()
	r.GET("/test", middleware.AuthRequired(), func(c *gin.Context) { c.Status(http.StatusOK) })

	oldTok, err := store.GenerateJWT(uid, "tvrevoke", "user", 1)
	assertTrue(t, err == nil, "sign old token")

	assertAuthCode(t, r, oldTok, http.StatusOK, "tv=1 token should pass")

	// 修改密码 → token_version+1（UpdateUserPassword 内建行为）。
	if err := store.UpdateUserPassword(uid, store.HashPassword("newpassword456")); err != nil {
		t.Fatalf("update password: %v", err)
	}
	tv, err := store.GetUserTokenVersion(uid)
	assertTrue(t, err == nil, "read token version")
	assertTrue(t, tv == 2, "token_version should be bumped to 2")

	assertAuthCode(t, r, oldTok, http.StatusUnauthorized, "stale tv token must be rejected")

	newTok, err := store.GenerateJWT(uid, "tvrevoke", "user", tv)
	assertTrue(t, err == nil, "sign new token")
	assertAuthCode(t, r, newTok, http.StatusOK, "fresh tv token should pass")
}

func assertAuthCode(t *testing.T, r *gin.Engine, token string, want int, msg string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, want, msg)
}

// ── A3.4 must_change_password 中间件测试 ──────────────────────

// TestMustChangePasswordGuard: 标记为真时除改密/登出外一律 403
// {must_change_password:true}；改密成功即清除标记并恢复访问。
func TestMustChangePasswordGuard(t *testing.T) {
	uid := mfaTestUser(t, "mustchange")
	if err := store.SetMustChangePassword(uid, true); err != nil {
		t.Fatalf("set must_change_password: %v", err)
	}

	r := setupRouter()
	r.GET("/test", middleware.AuthRequired(), func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/api/other", middleware.AuthRequired(), func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/api/auth/logout", middleware.AuthRequired(), Logout)
	r.POST("/api/auth/change-password", middleware.AuthRequired(), ChangePassword)

	tok, _ := store.GenerateJWT(uid, "mustchange", "user", 1)

	// 普通业务路径 → 403 且带 must_change_password 标记。
	req := httptest.NewRequest("GET", "/api/other", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusForbidden, "business path must be 403 while must_change_password")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assertTrue(t, body["must_change_password"] == true, "403 body must carry must_change_password")

	// 登出路径放行。
	code, _ := postJSON(t, r, "/api/auth/logout", gin.H{}, tok)
	assertEq(t, code, http.StatusOK, "logout must stay reachable")

	// 改密路径放行：旧密码错误 → 400（证明守卫已放行到 handler）。
	code, _ = postJSON(t, r, "/api/auth/change-password", gin.H{"old_password": "wrong", "new_password": "whatever9"}, tok)
	assertEq(t, code, http.StatusBadRequest, "change-password reachable (handler-level 400)")

	// 改密成功 → 标记清除、tv+1 使旧令牌失效，新令牌恢复访问。
	code, _ = postJSON(t, r, "/api/auth/change-password", gin.H{"old_password": "password123", "new_password": "brandnewpass9"}, tok)
	assertEq(t, code, http.StatusOK, "change password")
	flags, found := store.GetUserSecurityFlags(uid)
	assertTrue(t, found, "user flags found")
	assertTrue(t, !flags.MustChangePassword, "must_change_password must be cleared after change")

	assertAuthCode(t, r, tok, http.StatusUnauthorized, "old token invalid after password change (tv bump)")

	tok2, _ := store.GenerateJWT(uid, "mustchange", "user", flags.TokenVersion)
	assertAuthCode(t, r, tok2, http.StatusOK, "access restored with fresh token")
}
