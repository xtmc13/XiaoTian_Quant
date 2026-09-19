package handler

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// GetNotifyChannels returns all notification channels and their status.
func GetNotifyChannels(c *gin.Context) {
	mgr := notify.GetManager()
	// Get channel info through history method
	history := mgr.GetHistory(1)
	_ = history

	channels := []gin.H{
		{"name": "log",    "enabled": true,  "configured": true},
		{"name": "email",  "enabled": isEnvSet("SMTP_HOST"),  "configured": isEnvSet("SMTP_HOST")},
		{"name": "lark",   "enabled": isEnvSet("LARK_WEBHOOK"),  "configured": isEnvSet("LARK_WEBHOOK")},
		{"name": "dingtalk", "enabled": isEnvSet("DINGTALK_WEBHOOK"), "configured": isEnvSet("DINGTALK_WEBHOOK")},
		{"name": "telegram", "enabled": isEnvSet("TELEGRAM_BOT_TOKEN"), "configured": isEnvSet("TELEGRAM_BOT_TOKEN") && isEnvSet("TELEGRAM_CHAT_ID")},
		{"name": "discord",  "enabled": isEnvSet("DISCORD_WEBHOOK_URL"), "configured": isEnvSet("DISCORD_WEBHOOK_URL")},
		{"name": "sms",      "enabled": isEnvSet("TWILIO_ACCOUNT_SID") && isEnvSet("TWILIO_AUTH_TOKEN") && isEnvSet("TWILIO_FROM_NUMBER"),
			"configured": isEnvSet("TWILIO_ACCOUNT_SID") && isEnvSet("TWILIO_AUTH_TOKEN") && isEnvSet("TWILIO_FROM_NUMBER") && isEnvSet("TWILIO_TO_NUMBER")},
	}

	c.JSON(http.StatusOK, gin.H{
		"channels": channels,
	})
}

// GetNotifyRoutes returns notification routing rules visible to the current user:
// 系统规则（user_id=0，所有人可见）+ 本人创建的规则；无持久化规则时回退默认规则。
func GetNotifyRoutes(c *gin.Context) {
	uid, injected := ctxUserID(c)
	var records []store.NotificationRouteRecord
	var err error
	if injected {
		records, err = store.ListNotificationRoutesForUser(int64(uid))
	} else {
		records, err = store.ListNotificationRoutes()
	}
	if err != nil || len(records) == 0 {
		// 与原行为一致：没有持久化规则时返回内建默认规则。
		router := notify.NewRouter()
		c.JSON(http.StatusOK, gin.H{
			"rules": router.GetRules(),
		})
		return
	}
	rules := make([]notify.RouteRule, 0, len(records))
	for _, r := range records {
		rules = append(rules, notify.RouteRule{
			ID:        r.ID,
			Name:      r.Name,
			Events:    r.Events,
			Levels:    r.Levels,
			Channels:  r.Channels,
			Enabled:   r.Enabled,
			MinReturn: r.MinReturn,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"rules": rules,
	})
}

// UpdateNotifyRoute updates a notification routing rule.
// 多用户越权防护：系统规则（无属主）仅 admin 可改；已归属规则仅属主可改；
// 新规则归当前用户（未注入则为系统规则）。
func UpdateNotifyRoute(c *gin.Context) {
	var body notify.RouteRule
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	owner, found := store.GetNotificationRouteOwner(body.ID)
	if found {
		if owner == 0 {
			// 系统规则：所有人可见，仅 admin 可改。
			if !requireAdmin(c) {
				return
			}
		} else {
			if !requireOwner(c, owner) {
				return
			}
		}
	}

	uid, injected := ctxUserID(c)
	record := &store.NotificationRouteRecord{
		ID:        body.ID,
		Name:      body.Name,
		Events:    body.Events,
		Levels:    body.Levels,
		Channels:  body.Channels,
		Enabled:   body.Enabled,
		MinReturn: body.MinReturn,
	}
	if found {
		record.UserID = owner // 保留原属主
	} else if injected {
		record.UserID = int64(uid)
	}
	if err := store.SaveNotificationRoute(record); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save route"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "updated",
		"rule":   body,
	})
}

// DeleteNotifyRoute removes a routing rule.
// 系统规则仅 admin 可删；已归属规则仅属主可删。
func DeleteNotifyRoute(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rule id required"})
		return
	}

	owner, found := store.GetNotificationRouteOwner(id)
	if found {
		if owner == 0 {
			if !requireAdmin(c) {
				return
			}
		} else {
			if !requireOwner(c, owner) {
				return
			}
		}
		if err := store.DeleteNotificationRoute(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete route"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "deleted", "id": id})
		return
	}

	// 未持久化的内建默认规则：仅 admin（或未注入的单用户模式）可删。
	if !requireAdmin(c) {
		return
	}
	router := notify.NewRouter()
	if !router.DeleteRule(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted", "id": id})
}

// TestNotifyChannel sends a test message to a channel.
func TestNotifyChannel(c *gin.Context) {
	var body struct {
		Channel string `json:"channel"`
		Message string `json:"message"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if body.Channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel required"})
		return
	}
	if body.Message == "" {
		body.Message = "🧪 Test notification from XiaoTianQuant"
	}

	msg := notify.Message{
		Title:   "Test Notification",
		Content: body.Message,
		Level:   "INFO",
		Tags:    map[string]string{"test": "true", "channel": body.Channel},
	}

	mgr := notify.GetManager()
	errs := mgr.SendSync(msg)
	if len(errs) > 0 {
		var errStrs []string
		for _, e := range errs {
			errStrs = append(errStrs, e.Error())
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  "partial",
			"channel": body.Channel,
			"errors":  errStrs,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "sent",
		"channel": body.Channel,
	})
}

// SendCustomNotification sends a custom notification.
func SendCustomNotification(c *gin.Context) {
	var body struct {
		Title    string            `json:"title"`
		Content  string            `json:"content"`
		Level    string            `json:"level"`
		Channels []string          `json:"channels"`
		Tags     map[string]string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if body.Title == "" || body.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title and content required"})
		return
	}
	if body.Level == "" {
		body.Level = "INFO"
	}

	msg := notify.Message{
		Title:   body.Title,
		Content: body.Content,
		Level:   body.Level,
		Tags:    body.Tags,
	}

	mgr := notify.GetManager()
	errs := mgr.SendSync(msg)

	// Also persist to notification store
	store := notify.GetNotificationStore()
	var uid int64
	if u, injected := ctxUserID(c); injected {
		uid = int64(u)
	}
	store.AddWithUser(body.Title, body.Content, body.Level, "custom", uid)

	if len(errs) > 0 {
		var errStrs []string
		for _, e := range errs {
			errStrs = append(errStrs, e.Error())
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  "partial",
			"errors":  errStrs,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "sent"})
}

func isEnvSet(key string) bool {
	return os.Getenv(key) != ""
}
