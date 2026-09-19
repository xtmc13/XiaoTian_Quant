package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/notify"
)

// GetNotifications returns recent notifications.
// H7 越权防护：登录非 admin 用户只看到系统广播(user_id=0) + 本人的通知。
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
	var items []*notify.Notification
	var unreadCount, total int
	if uid, restricted := notifyRestrictedUser(c); restricted {
		items = store.ListForUser(limit, offset, unreadOnly, uid)
		unreadCount = store.UnreadCountForUser(uid)
		total = store.TotalForUser(uid)
	} else {
		items = store.List(limit, offset, unreadOnly)
		unreadCount = store.UnreadCount()
		total = store.Total()
	}

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
		"unread_count":  unreadCount,
		"total":         total,
	})
}

// notifyRestrictedUser 返回 (userID, 是否需按属主过滤)（登录非 admin）。
func notifyRestrictedUser(c *gin.Context) (int64, bool) {
	uid, injected := ctxUserID(c)
	return int64(uid), injected && !ctxIsAdmin(c)
}

// MarkNotificationRead marks a notification as read.
// H7：普通用户只能标记系统广播或本人通知。
func MarkNotificationRead(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	store := notify.GetNotificationStore()
	var ok bool
	if uid, restricted := notifyRestrictedUser(c); restricted {
		ok = store.MarkReadForUser(id, uid)
	} else {
		ok = store.MarkRead(id)
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// MarkAllNotificationsRead marks all notifications as read.
// H7：普通用户只标记系统广播或本人通知。
func MarkAllNotificationsRead(c *gin.Context) {
	store := notify.GetNotificationStore()
	var count int
	if uid, restricted := notifyRestrictedUser(c); restricted {
		count = store.MarkAllReadForUser(uid)
	} else {
		count = store.MarkAllRead()
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "marked": count})
}

// ClearNotifications removes all notifications.
// H7：普通用户只能清除本人通知，系统广播保留。
func ClearNotifications(c *gin.Context) {
	store := notify.GetNotificationStore()
	if uid, restricted := notifyRestrictedUser(c); restricted {
		store.ClearForUser(uid)
	} else {
		store.Clear()
	}
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
