package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// Context keys used by auth middleware.
const (
	UserIDKey   = "user_id"
	UsernameKey = "username"
	RoleKey     = "role"
)

// mustChangeAllowedPaths 列出 must_change_password=true 时仍放行的路径
// （A3.4：只许改密与登出，其余一律 403）。
var mustChangeAllowedPaths = map[string]bool{
	"/api/auth/change-password": true,
	"/api/user/change-password": true,
	"/api/auth/logout":          true,
}

var corsAllowedOrigins = func() map[string]bool {
	m := map[string]bool{
		"http://localhost:5173": true,
		"http://localhost:3000": true,
		"http://localhost:8080": true,
	}
	if env := os.Getenv("CORS_ALLOWED_ORIGINS"); env != "" {
		for _, o := range strings.Split(env, ",") {
			m[strings.TrimSpace(o)] = true
		}
	}
	return m
}()

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		// Allow if origin is in whitelist or if no whitelist restriction (development)
		if corsAllowedOrigins["*"] || corsAllowedOrigins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization")
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// authenticate 是 AuthRequired/AdminRequired 共用的鉴权核心：
// 解析 JWT → 查库校验 token_version（A3.3 吊销）与 is_active →
// must_change_password 守卫（A3.4）→ 写入上下文。任一步失败写出响应并返回 false。
// 数据库不可用（单测环境）时跳过 tv/活动状态/强制改密检查，保持既有行为。
func authenticate(c *gin.Context, requireAdmin bool) bool {
	token := extractToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication required"})
		c.Abort()
		return false
	}
	claims, err := store.VerifyJWT(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "Invalid or expired token"})
		c.Abort()
		return false
	}

	userID := int(claims["user_id"].(float64))
	role, _ := claims["role"].(string)
	username, _ := claims["sub"].(string)

	if requireAdmin && role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"detail": "Admin access required"})
		c.Abort()
		return false
	}

	if db := store.GetDB(); db != nil {
		flags, found := store.GetUserSecurityFlags(userID)
		if !found || !flags.IsActive {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Invalid or expired token"})
			c.Abort()
			return false
		}
		// A3.3: JWT 里的 token_version 必须与数据库一致，否则视为已吊销。
		claimTV, _ := claims["token_version"].(float64)
		if int(claimTV) != flags.TokenVersion {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Token revoked"})
			c.Abort()
			return false
		}
		// A3.4: 强制改密期间仅放行改密/登出路径。
		if flags.MustChangePassword && !mustChangeAllowedPaths[c.Request.URL.Path] {
			c.JSON(http.StatusForbidden, gin.H{
				"detail":               "password change required",
				"must_change_password": true,
			})
			c.Abort()
			return false
		}
	}

	c.Set(UserIDKey, userID)
	c.Set(UsernameKey, username)
	c.Set(RoleKey, role)
	c.Next()
	return true
}

func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		authenticate(c, false)
	}
}

func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		authenticate(c, true)
	}
}

// extractToken extracts the JWT from the Authorization header.
// Query parameter token support has been removed for security —
// tokens in URLs are logged by servers, browsers, and proxies.
func extractToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

// ── RequestID ─────────────────────────────────────────────────────

// RequestID injects a unique request ID into each Gin context.
// It checks the X-Request-ID header first (for trace propagation),
// otherwise generates a random 16-byte hex string.
// The request_id is used by UnifiedResponseWrapper for the Meta.RequestID field.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			b := make([]byte, 16)
			if _, err := rand.Read(b); err != nil {
				// Fallback on crypto failure (extremely unlikely)
				rid = "unknown-" + strings.ReplaceAll(c.Request.RemoteAddr, ":", "-")
			} else {
				rid = hex.EncodeToString(b[:8]) + "-" + hex.EncodeToString(b[8:])
			}
		}
		c.Set("request_id", rid)
		c.Header("X-Request-ID", rid)
		c.Next()
	}
}
