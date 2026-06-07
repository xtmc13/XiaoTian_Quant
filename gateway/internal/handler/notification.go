package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/notify"
)

// GetNotifications returns recent notifications.
func GetNotifications(c *gin.Context) {
	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}
	offset := 0
	if o := c.Query("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}
	unreadOnly := c.Query("unread") == "1"

	store := notify.GetNotificationStore()
	items := store.List(limit, offset, unreadOnly)

	if items == nil {
		items = []*notify.Notification{}
	}

	// Normalize to frontend NotificationItem format
	notifications := make([]map[string]any, 0, len(items))
	for _, n := range items {
		notifications = append(notifications, map[string]any{
			"id":         n.ID,
			"title":      n.Title,
			"message":    n.Content,
			"content":    n.Content,
			"level":      n.Level,
			"category":   n.Category,
			"type":       normalizeNotifyLevel(n.Level),
			"read":       n.Read,
			"created_at": n.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"notifications": notifications,
		"unread_count":  store.UnreadCount(),
		"total":         store.Total(),
	})
}

// MarkNotificationRead marks a notification as read.
func MarkNotificationRead(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	store := notify.GetNotificationStore()
	if !store.MarkRead(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// MarkAllNotificationsRead marks all notifications as read.
func MarkAllNotificationsRead(c *gin.Context) {
	store := notify.GetNotificationStore()
	count := store.MarkAllRead()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "marked": count})
}

// ClearNotifications removes all notifications.
func ClearNotifications(c *gin.Context) {
	store := notify.GetNotificationStore()
	store.Clear()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// normalizeNotifyLevel maps backend notification levels to frontend type values.
func normalizeNotifyLevel(level string) string {
	switch strings.ToUpper(level) {
	case "INFO":
		return "info"
	case "WARN", "WARNING":
		return "warning"
	case "CRITICAL", "ERROR":
		return "error"
	case "SUCCESS":
		return "success"
	default:
		return "info"
	}
}

// GetUnreadCount returns the unread notification count.
func GetUnreadCount(c *gin.Context) {
	store := notify.GetNotificationStore()
	c.JSON(http.StatusOK, gin.H{
		"unread_count": store.UnreadCount(),
	})
}
