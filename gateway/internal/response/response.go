package response

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Envelope is the unified API response envelope.
type Envelope struct {
	Success bool        `json:"success"`
	Data    any         `json:"data,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
	Meta    *MetaInfo   `json:"meta,omitempty"`
}

// ErrorInfo holds structured error details.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// MetaInfo holds request metadata.
type MetaInfo struct {
	Timestamp int64  `json:"timestamp"`
	RequestID string `json:"requestId,omitempty"`
}

// OK sends a successful response with data.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{
		Success: true,
		Data:    data,
		Meta:    newMeta(c),
	})
}

// Created sends a 201 Created response.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Envelope{
		Success: true,
		Data:    data,
		Meta:    newMeta(c),
	})
}

// NoContent sends a 204 No Content response.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// BadRequest sends a 400 error.
func BadRequest(c *gin.Context, message string, details ...any) {
	sendError(c, http.StatusBadRequest, "BAD_REQUEST", message, details)
}

// Unauthorized sends a 401 error.
func Unauthorized(c *gin.Context, message string) {
	sendError(c, http.StatusUnauthorized, "UNAUTHORIZED", message, nil)
}

// Forbidden sends a 403 error.
func Forbidden(c *gin.Context, message string) {
	sendError(c, http.StatusForbidden, "FORBIDDEN", message, nil)
}

// NotFound sends a 404 error.
func NotFound(c *gin.Context, message string) {
	sendError(c, http.StatusNotFound, "NOT_FOUND", message, nil)
}

// Conflict sends a 409 error.
func Conflict(c *gin.Context, message string) {
	sendError(c, http.StatusConflict, "CONFLICT", message, nil)
}

// TooManyRequests sends a 429 error.
func TooManyRequests(c *gin.Context, message string) {
	sendError(c, http.StatusTooManyRequests, "RATE_LIMIT", message, nil)
}

// InternalError sends a 500 error.
func InternalError(c *gin.Context, message string, details ...any) {
	sendError(c, http.StatusInternalServerError, "INTERNAL_ERROR", message, details)
}

// sendError builds and sends an error envelope.
func sendError(c *gin.Context, status int, code, message string, details []any) {
	err := &ErrorInfo{Code: code, Message: message}
	if len(details) > 0 && details[0] != nil {
		err.Details = details[0]
	}
	c.JSON(status, Envelope{
		Success: false,
		Error:   err,
		Meta:    newMeta(c),
	})
}

// newMeta builds metadata for the current request.
func newMeta(c *gin.Context) *MetaInfo {
	reqID := c.GetString("request_id")
	if reqID == "" {
		reqID = c.GetHeader("X-Request-ID")
	}
	return &MetaInfo{
		Timestamp: time.Now().UnixMilli(),
		RequestID: reqID,
	}
}

// Raw sends a raw envelope (used by the wrapper middleware).
func Raw(c *gin.Context, status int, body any) {
	c.JSON(status, body)
}
