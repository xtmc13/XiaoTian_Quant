package handler

import (
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

var serverStartTime = time.Now()

// EnhancedAdminStats returns comprehensive system statistics.
func EnhancedAdminStats(c *gin.Context) {
	users := store.ListAllUsers()
	totalUsers := len(users)
	activeUsers := 0
	adminCount := 0
	for _, u := range users {
		if isActive, ok := u["is_active"].(int); ok && isActive == 1 {
			activeUsers++
		}
		if role, ok := u["role"].(string); ok && role == "admin" {
			adminCount++
		}
	}

	// Runtime stats
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	// Order stats
	totalOrders := store.CountOrders()
	pendingOrders := store.CountPendingOrders()

	// Trade stats
	totalTrades := store.CountTrades()

	// Strategy stats
	totalStrategies := store.CountStrategies()
	activeStrategies := store.CountActiveStrategies()

	// Notification stats
	unreadNotifs := notify.GetNotificationStore().UnreadCount()

	// Recent risk events
	riskEvents := store.GetRecentRiskEvents(20)

	c.JSON(http.StatusOK, gin.H{
		"users": gin.H{
			"total":  totalUsers,
			"active": activeUsers,
			"admin":  adminCount,
			"normal": totalUsers - adminCount,
		},
		"system": gin.H{
			"goroutines":    runtime.NumGoroutine(),
			"heap_alloc_mb": float64(mem.HeapAlloc) / 1024 / 1024,
			"heap_sys_mb":   float64(mem.HeapSys) / 1024 / 1024,
			"num_gc":        mem.NumGC,
			"uptime_seconds": int(time.Since(serverStartTime).Seconds()),
			"go_version":    runtime.Version(),
			"num_cpu":       runtime.NumCPU(),
		},
		"trading": gin.H{
			"total_orders":      totalOrders,
			"pending_orders":    pendingOrders,
			"total_trades":      totalTrades,
			"total_strategies":  totalStrategies,
			"active_strategies": activeStrategies,
			"unread_notifications": unreadNotifs,
		},
		"risk_events": riskEvents,
	})
}

// AdminAuditLog returns recent audit log entries.
func AdminAuditLog(c *gin.Context) {
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

	entries := store.GetAuditLog(limit, offset)
	total := store.CountAuditLog()

	if entries == nil {
		entries = []map[string]any{}
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":    entries,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// AdminUserDisable disables a user account.
func AdminUserDisable(c *gin.Context) {
	userID := c.Param("id")
	if err := store.SetUserActive(userID, false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	store.AddAuditLog("admin", "user_disabled", "user_id="+userID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// AdminUserEnable enables a user account.
func AdminUserEnable(c *gin.Context) {
	userID := c.Param("id")
	if err := store.SetUserActive(userID, true); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	store.AddAuditLog("admin", "user_enabled", "user_id="+userID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// AdminDashboardSummary returns a summary for the admin dashboard cards.
func AdminDashboardSummary(c *gin.Context) {
	users := store.ListAllUsers()
	activeUsers := 0
	for _, u := range users {
		if isActive, ok := u["is_active"].(int); ok && isActive == 1 {
			activeUsers++
		}
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	c.JSON(http.StatusOK, gin.H{
		"total_users":       len(users),
		"active_users":      activeUsers,
		"pending_orders":    store.CountPendingOrders(),
		"total_trades":      store.CountTrades(),
		"active_strategies": store.CountActiveStrategies(),
		"unread_alerts":     notify.GetNotificationStore().UnreadCount(),
		"uptime_hours":      int(time.Since(serverStartTime).Hours()),
		"memory_mb":         float64(mem.HeapAlloc) / 1024 / 1024,
	})
}

// AdminRecentActivity returns recent activity across all modules.
func AdminRecentActivity(c *gin.Context) {
	limit := 20
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	activities := make([]map[string]any, 0)

	// Recent trades
	trades := store.GetRecentTrades(5)
	for _, t := range trades {
		activities = append(activities, map[string]any{
			"type":      "trade",
			"message":   t["symbol"].(string) + " " + t["side"].(string),
			"timestamp": t["time"],
		})
	}

	// Recent risk events
	riskEvents := store.GetRecentRiskEvents(5)
	for _, e := range riskEvents {
		activities = append(activities, map[string]any{
			"type":    "risk",
			"message": e["message"],
			"level":   e["level"],
			"timestamp": e["timestamp"],
		})
	}

	// Recent audit logs
	auditLogs := store.GetAuditLog(5, 0)
	for _, a := range auditLogs {
		activities = append(activities, map[string]any{
			"type":    "audit",
			"message": a["action"].(string),
			"timestamp": a["created_at"],
		})
	}

	// Recent notifications
	notifs := notify.GetNotificationStore().List(5, 0, false)
	for _, n := range notifs {
		activities = append(activities, map[string]any{
			"type":      "notification",
			"message":   n.Title,
			"timestamp": n.CreatedAt,
		})
	}

	if len(activities) > limit {
		activities = activities[:limit]
	}

	c.JSON(http.StatusOK, gin.H{"activities": activities})
}
