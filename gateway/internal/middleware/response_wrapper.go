package middleware

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/response"
)

// responseBuffer captures the response body for inspection.
type responseBuffer struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseBuffer) Write(b []byte) (int, error) {
	w.body.Write(b)
	return len(b), nil
}

func (w *responseBuffer) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return len(s), nil
}

func (w *responseBuffer) WriteHeader(statusCode int) {
	// Delay writing header until middleware decides
	w.ResponseWriter.WriteHeader(statusCode)
}

// Hijack implements http.Hijacker for WebSocket support.
func (w *responseBuffer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijack not supported")
	}
	return hijacker.Hijack()
}

// UnifiedResponseWrapper wraps all JSON responses into the unified envelope format.
// It is designed to be non-breaking: existing handlers continue to work unchanged,
// and the middleware transforms the outgoing JSON at the edge.
//
// Skip rules:
//   - Non-JSON responses (e.g., file downloads, WebSocket upgrades)
//   - Responses that are already wrapped (detected by presence of "success" top-level key)
//   - Specific paths like /metrics, /debug/pprof, /ws, /health (raw data expected)
//   - Status 204 No Content
func UnifiedResponseWrapper() gin.HandlerFunc {
	skipPaths := map[string]bool{
		"/metrics":            true,
		"/health":             true,
		"/health/components":  true,
		"/ws":                 true,
		"/ws/v2":              true,
		"/ws/stats":           true,
	}

	return func(c *gin.Context) {
		// Skip WebSocket upgrades and excluded paths
		if skipPaths[c.Request.URL.Path] {
			c.Next()
			return
		}
		if c.GetHeader("Upgrade") == "websocket" {
			c.Next()
			return
		}

		// Capture response
		buf := &bytes.Buffer{}
		writer := &responseBuffer{ResponseWriter: c.Writer, body: buf}
		c.Writer = writer

		c.Next()

		// Only process JSON responses; pass through non-JSON (HTML, etc.)
		contentType := c.Writer.Header().Get("Content-Type")
		if !isJSONContentType(contentType) {
			c.Writer = writer.ResponseWriter
			if buf.Len() > 0 {
				c.Writer.Write(buf.Bytes())
			}
			return
		}

		status := c.Writer.Status()
		if status == http.StatusNoContent {
			return
		}

		// Parse the original response
		var raw map[string]any
		if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
			// Not a JSON object (e.g., array or primitive) — wrap it as data
			wrapPrimitive(c, writer, status, buf.Bytes())
			return
		}

		// Already wrapped? Skip
		if _, ok := raw["success"]; ok {
			return
		}

		// Build unified envelope
		var envelope response.Envelope
		envelope.Meta = &response.MetaInfo{
			Timestamp: time.Now().UnixMilli(),
			RequestID: c.GetString("request_id"),
		}

		if status >= 400 {
			// Error response — try to extract message from common keys
			envelope.Success = false
			msg := extractErrorMessage(raw)
			code := httpStatusToCode(status)
			envelope.Error = &response.ErrorInfo{
				Code:    code,
				Message: msg,
			}
			// Preserve original as details if it has extra fields
			if len(raw) > 1 {
				envelope.Error.Details = raw
			}
		} else {
			// Success response — wrap the entire raw object as data
			envelope.Success = true
			envelope.Data = raw
		}

		// Replace the response
		c.Writer = writer.ResponseWriter
		c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		c.Writer.WriteHeader(status)
		json.NewEncoder(c.Writer).Encode(envelope)
	}
}

// isJSONContentType checks if the content type is JSON.
func isJSONContentType(ct string) bool {
	return ct == "application/json" || ct == "application/json; charset=utf-8" ||
		ct == "text/json" || (len(ct) > 16 && ct[:16] == "application/json")
}

// extractErrorMessage tries to find a human-readable message in common error keys.
func extractErrorMessage(raw map[string]any) string {
	for _, key := range []string{"detail", "error", "message", "msg", "reason"} {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return "request failed"
}

// httpStatusToCode maps HTTP status to a short error code.
func httpStatusToCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "BAD_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "CONFLICT"
	case http.StatusTooManyRequests:
		return "RATE_LIMIT"
	case http.StatusInternalServerError:
		return "INTERNAL_ERROR"
	case http.StatusBadGateway:
		return "BAD_GATEWAY"
	case http.StatusServiceUnavailable:
		return "SERVICE_UNAVAILABLE"
	default:
		return "HTTP_ERROR"
	}
}

// wrapPrimitive handles non-object JSON responses (arrays, strings, numbers).
func wrapPrimitive(c *gin.Context, writer *responseBuffer, status int, data []byte) {
	var primitive any
	json.Unmarshal(data, &primitive)

	envelope := response.Envelope{
		Success: status < 400,
		Data:    primitive,
		Meta: &response.MetaInfo{
			Timestamp: time.Now().UnixMilli(),
			RequestID: c.GetString("request_id"),
		},
	}
	if status >= 400 {
		envelope.Data = nil
		envelope.Error = &response.ErrorInfo{
			Code:    httpStatusToCode(status),
			Message: "request failed",
		}
	}

	c.Writer = writer.ResponseWriter
	c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.Writer.WriteHeader(status)
	json.NewEncoder(c.Writer).Encode(envelope)
}
