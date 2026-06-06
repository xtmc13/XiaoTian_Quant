package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/logging"
)

// RequestLogger logs every HTTP request with structured fields.
func RequestLogger(logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		if raw != "" {
			path = path + "?" + raw
		}

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method
		errorMsg := c.Errors.ByType(gin.ErrorTypePrivate).String()

		fields := map[string]any{
			"status":     status,
			"latency_ms":   float64(latency.Nanoseconds()) / 1e6,
			"client_ip":  clientIP,
			"method":     method,
			"path":       path,
			"user_agent": c.Request.UserAgent(),
		}
		if errorMsg != "" {
			fields["error"] = errorMsg
		}

		if status >= 500 {
			logger.Error("request failed", fields)
		} else if status >= 400 {
			logger.Warn("request error", fields)
		} else {
			logger.Info("request", fields)
		}
	}
}
