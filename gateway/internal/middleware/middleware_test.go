package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAuthMiddlewareNoToken(t *testing.T) {
	r := gin.New()
	r.Use(AuthRequired())
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddlewareInvalidToken(t *testing.T) {
	r := gin.New()
	r.Use(AuthRequired())
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid_token_here")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should fail auth
	if w.Code == 200 {
		t.Fatal("invalid token should not pass")
	}
}

func TestCORSMiddleware(t *testing.T) {
	r := gin.New()
	r.Use(CORS())
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("CORS header should be set")
	}
}

func TestAdminRequiredMiddleware(t *testing.T) {
	r := gin.New()
	// Set a non-admin user
	r.Use(func(c *gin.Context) {
		c.Set(UserIDKey, 1)
		c.Set("role", "user")
		c.Next()
	})
	r.Use(AdminRequired())
	r.GET("/admin", func(c *gin.Context) { c.String(200, "admin") })

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == 200 {
		t.Fatal("non-admin should not access admin routes")
	}
}

func TestAdminRequiredAdminPass(t *testing.T) {
	r := gin.New()
	// Skip AuthRequired by directly injecting user context
	r.Use(func(c *gin.Context) {
		c.Set(UserIDKey, 1)
		c.Set("role", "admin")
	})
	r.Use(AdminRequired())
	r.GET("/admin", func(c *gin.Context) { c.String(200, "ok") })

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("admin should pass, got %d: %s", w.Code, w.Body.String())
	}
}
