package middleware

import (
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

var corsAllowedOrigins = func() map[string]bool {
	m := map[string]bool{
		"http://localhost:5173": true,
		"http://localhost:3000": true,
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

func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication required"})
			c.Abort()
			return
		}
		claims, err := store.VerifyJWT(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Invalid or expired token"})
			c.Abort()
			return
		}
		c.Set(UserIDKey, int(claims["user_id"].(float64)))
		c.Set(UsernameKey, claims["sub"].(string))
		c.Set(RoleKey, claims["role"].(string))
		c.Next()
	}
}

func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Authentication required"})
			c.Abort()
			return
		}
		claims, err := store.VerifyJWT(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "Invalid or expired token"})
			c.Abort()
			return
		}
		role := claims["role"].(string)
		if role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"detail": "Admin access required"})
			c.Abort()
			return
		}
		c.Set("user_id", int(claims["user_id"].(float64)))
		c.Set("username", claims["sub"].(string))
		c.Set("role", role)
		c.Next()
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
