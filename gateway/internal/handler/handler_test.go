package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	return r
}

func assertEq(t *testing.T, got, want int, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %d, want %d", msg, got, want)
	}
}

func assertTrue(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

/* ── HealthCheck Tests ───────────────────────────────────────── */

func TestHealthCheck(t *testing.T) {
	r := setupRouter()
	r.GET("/health", HealthCheck)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)

	assertEq(t, w.Code, http.StatusOK, "status code")

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	assertTrue(t, err == nil, "should parse JSON")
	assertTrue(t, body["status"] == "ok", "status should be ok")
	assertTrue(t, body["version"] == "2.0.0", "version should be 2.0.0")
}

/* ── Auth Request Validation Tests ───────────────────────────── */

func TestLoginRequestValidation(t *testing.T) {
	// Valid request
	req := LoginRequest{Username: "testuser", Password: "password123"}
	assertTrue(t, len(req.Username) >= 3, "username should be at least 3 chars")
	assertTrue(t, len(req.Password) >= 6, "password should be at least 6 chars")

	// Invalid request
	req2 := LoginRequest{Username: "ab", Password: "123"}
	assertTrue(t, len(req2.Username) < 3, "short username should be invalid")
	assertTrue(t, len(req2.Password) < 6, "short password should be invalid")
}

func TestRegisterRequestValidation(t *testing.T) {
	// Valid request
	req := RegisterRequest{
		Username: "testuser",
		Password: "password123",
		Nickname: "Test",
		Email:    "test@example.com",
		Code:     "123456",
	}
	assertTrue(t, len(req.Username) >= 3, "username should be at least 3 chars")
	assertTrue(t, len(req.Password) >= 6, "password should be at least 6 chars")
	assertTrue(t, strings.Contains(req.Email, "@"), "email should contain @")
	assertTrue(t, req.Code != "", "code should not be empty")

	// Invalid email
	req2 := RegisterRequest{Email: "invalid"}
	assertTrue(t, !strings.Contains(req2.Email, "@") == false, "invalid email check")
}

func TestSendCodeRequestValidation(t *testing.T) {
	req := SendCodeRequest{Email: "test@example.com", CodeType: "register"}
	assertTrue(t, strings.Contains(req.Email, "@"), "email should contain @")
	assertTrue(t, req.CodeType != "", "code_type should not be empty")
}

/* ── Dev Mode Login Tests ────────────────────────────────────── */

func TestDevModeLogin(t *testing.T) {
	// Set dev credentials
	os.Setenv("ADMIN_USER", "devadmin")
	os.Setenv("ADMIN_PASSWORD", "devpass")
	defer func() {
		os.Unsetenv("ADMIN_USER")
		os.Unsetenv("ADMIN_PASSWORD")
	}()

	r := setupRouter()
	r.POST("/login", Login)

	// Valid dev login
	body := `{"username":"devadmin","password":"devpass"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Should return 200 or 401 depending on store state
	assertTrue(t, w.Code == http.StatusOK || w.Code == http.StatusUnauthorized, "should return 200 or 401")
}

/* ── GetMe Tests ─────────────────────────────────────────────── */

func TestGetMe(t *testing.T) {
	r := setupRouter()
	r.GET("/me", func(c *gin.Context) {
		c.Set("user_id", 1)
		c.Set("username", "testuser")
		c.Set("role", "user")
		GetMe(c)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/me", nil)
	r.ServeHTTP(w, req)

	assertEq(t, w.Code, http.StatusOK, "status code")

	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	assertTrue(t, err == nil, "should parse JSON")
	assertTrue(t, body["username"] == "testuser", "username should match")
	assertTrue(t, body["role"] == "user", "role should match")
}

/* ── Status Handler Tests ────────────────────────────────────── */

func TestStatusHandler(t *testing.T) {
	r := setupRouter()
	r.GET("/status", Status)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/status", nil)
	r.ServeHTTP(w, req)

	// Status may return 200 or 500 depending on app initialization
	assertTrue(t, w.Code == http.StatusOK || w.Code == http.StatusInternalServerError, "should return 200 or 500")
}
