package handler

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/notify"
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
	}

	c.JSON(http.StatusOK, gin.H{
		"channels": channels,
	})
}

// GetNotifyRoutes returns notification routing rules.
func GetNotifyRoutes(c *gin.Context) {
	router := notify.NewRouter()
	c.JSON(http.StatusOK, gin.H{
		"rules": router.GetRules(),
	})
}

// UpdateNotifyRoute updates a notification routing rule.
func UpdateNotifyRoute(c *gin.Context) {
	var body notify.RouteRule
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	router := notify.NewRouter()
	router.UpdateRule(body)

	c.JSON(http.StatusOK, gin.H{
		"status": "updated",
		"rule":   body,
	})
}

// DeleteNotifyRoute removes a routing rule.
func DeleteNotifyRoute(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rule id required"})
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
	store.Add(body.Title, body.Content, body.Level, "custom")

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
