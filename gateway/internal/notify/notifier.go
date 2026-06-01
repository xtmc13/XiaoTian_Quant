package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"time"
)

// ── Notify Message ──

// Message is the standard notification payload.
type Message struct {
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Level     string            `json:"level"` // INFO, WARN, CRITICAL
	Tags      map[string]string `json:"tags"`
	Timestamp int64             `json:"timestamp"`
}

// ── Channel Interface ──

// Channel is a notification delivery channel.
type Channel interface {
	Name() string
	Send(msg Message) error
	IsEnabled() bool
}

// ── Notification Manager ──

// Manager routes notifications to all enabled channels.
type Manager struct {
	channels map[string]Channel
	history  []Message
	mu       sync.RWMutex
	queue    chan Message
	wg       sync.WaitGroup
}

var (
	instance     *Manager
	instanceOnce sync.Once
)

// GetManager returns the global notification manager.
func GetManager() *Manager {
	instanceOnce.Do(func() {
		instance = &Manager{
			channels: make(map[string]Channel),
			queue:    make(chan Message, 500),
		}

		// Register built-in channels
		instance.Register(&LogChannel{enabled: true})
		instance.Register(&EmailChannel{enabled: os.Getenv("SMTP_HOST") != ""})
		instance.Register(&LarkChannel{
			webhook:     os.Getenv("LARK_WEBHOOK"),
			signingKey:  os.Getenv("LARK_SIGNING_KEY"),
			enabled:     os.Getenv("LARK_WEBHOOK") != "",
		})
		instance.Register(&DingTalkChannel{
			webhook: os.Getenv("DINGTALK_WEBHOOK"),
			secret:  os.Getenv("DINGTALK_SECRET"),
			enabled: os.Getenv("DINGTALK_WEBHOOK") != "",
		})
		instance.Register(&TelegramChannel{
			botToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
			chatID:   os.Getenv("TELEGRAM_CHAT_ID"),
			enabled:  os.Getenv("TELEGRAM_BOT_TOKEN") != "",
		})

		instance.wg.Add(1)
		go instance.worker()
	})
	return instance
}

func (m *Manager) Register(ch Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[ch.Name()] = ch
}

// Send dispatches a notification to all enabled channels asynchronously.
func (m *Manager) Send(msg Message) {
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}

	select {
	case m.queue <- msg:
	default:
		// Queue full — log and drop
		log.Printf("[Notify] Queue full, dropping: %s", msg.Title)
	}
}

// SendSync sends synchronously across all channels.
func (m *Manager) SendSync(msg Message) []error {
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}

	m.recordHistory(msg)
	var errs []error
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.channels {
		if !ch.IsEnabled() {
			continue
		}
		if err := ch.Send(msg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ch.Name(), err))
		}
	}
	return errs
}

// GetHistory returns recent notification history.
func (m *Manager) GetHistory(limit int) []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > len(m.history) {
		limit = len(m.history)
	}
	start := len(m.history) - limit
	result := make([]Message, limit)
	copy(result, m.history[start:])
	return result
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for msg := range m.queue {
		m.recordHistory(msg)
		m.mu.RLock()
		for _, ch := range m.channels {
			if !ch.IsEnabled() {
				continue
			}
			go func(ch Channel, msg Message) {
				if err := ch.Send(msg); err != nil {
					log.Printf("[Notify] %s error: %v", ch.Name(), err)
				}
			}(ch, msg)
		}
		m.mu.RUnlock()
	}
}

func (m *Manager) recordHistory(msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = append(m.history, msg)
	if len(m.history) > 1000 {
		m.history = m.history[len(m.history)-1000:]
	}
}

// ── Log Channel ──

type LogChannel struct{ enabled bool }

func (c *LogChannel) Name() string    { return "log" }
func (c *LogChannel) IsEnabled() bool { return c.enabled }

func (c *LogChannel) Send(msg Message) error {
	level := strings.ToUpper(msg.Level)
	log.Printf("[Notify:%s] %s: %s", level, msg.Title, msg.Content)
	return nil
}

// ── Email Channel ──

type EmailChannel struct {
	enabled bool
}

func (c *EmailChannel) Name() string    { return "email" }
func (c *EmailChannel) IsEnabled() bool { return c.enabled }

func (c *EmailChannel) Send(msg Message) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	from := os.Getenv("SMTP_FROM")
	to := os.Getenv("SMTP_TO")

	if host == "" || to == "" {
		return fmt.Errorf("SMTP not configured")
	}

	subject := fmt.Sprintf("[%s] %s", strings.ToUpper(msg.Level), msg.Title)
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n<html><body><h2>%s</h2><p>%s</p><p><small>%s</small></p></body></html>",
		from, to, subject, msg.Title, msg.Content, time.UnixMilli(msg.Timestamp).Format(time.RFC3339))

	auth := smtp.PlainAuth("", user, pass, host)
	return smtp.SendMail(host+":"+port, auth, from, []string{to}, []byte(body))
}

// ── Lark (Feishu) Channel ──

type LarkChannel struct {
	webhook    string
	signingKey string
	enabled    bool
}

func (c *LarkChannel) Name() string    { return "lark" }
func (c *LarkChannel) IsEnabled() bool { return c.enabled }

func (c *LarkChannel) Send(msg Message) error {
	if c.webhook == "" {
		return fmt.Errorf("lark webhook not configured")
	}

	timestamp := time.Now().Unix()
	sign := c.larkSign(timestamp)

	payload := map[string]any{
		"timestamp": fmt.Sprintf("%d", timestamp),
		"sign":      sign,
		"msg_type":  "interactive",
		"card": map[string]any{
			"header": map[string]any{
				"title":    map[string]string{"content": msg.Title},
				"template": larkColor(msg.Level),
			},
			"elements": []map[string]any{
				{"tag": "div", "text": map[string]string{"content": msg.Content}},
				{"tag": "hr"},
				{"tag": "note", "elements": []map[string]string{
					{"tag": "plain_text", "content": time.UnixMilli(msg.Timestamp).Format("2006-01-02 15:04:05")}},
				},
			},
		},
	}

	return c.postJSON(c.webhook, payload)
}

func (c *LarkChannel) larkSign(timestamp int64) string {
	str := fmt.Sprintf("%d\n%s", timestamp, c.signingKey)
	if c.signingKey == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(str))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (c *LarkChannel) postJSON(url string, payload map[string]any) error {
	data, _ := json.Marshal(payload)
	resp, err := http.DefaultClient.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func larkColor(level string) string {
	switch strings.ToUpper(level) {
	case "CRITICAL":
		return "red"
	case "WARN":
		return "yellow"
	default:
		return "blue"
	}
}

// ── DingTalk Channel ──

type DingTalkChannel struct {
	webhook string
	secret  string
	enabled bool
}

func (c *DingTalkChannel) Name() string    { return "dingtalk" }
func (c *DingTalkChannel) IsEnabled() bool { return c.enabled }

func (c *DingTalkChannel) Send(msg Message) error {
	if c.webhook == "" {
		return fmt.Errorf("dingtalk webhook not configured")
	}

	timestamp := time.Now().UnixMilli()
	signURL := fmt.Sprintf("%s&timestamp=%d&sign=%s", c.webhook, timestamp, c.dingSign(timestamp))

	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": msg.Title,
			"text": fmt.Sprintf("## %s\n\n%s\n\n> %s\n> Level: %s\n> Time: %s",
				msg.Title, msg.Content,
				strings.ToUpper(msg.Level),
				strings.ToUpper(msg.Level),
				time.UnixMilli(msg.Timestamp).Format("2006-01-02 15:04:05"),
			),
		},
	}

	return c.postJSON(signURL, payload)
}

func (c *DingTalkChannel) dingSign(timestamp int64) string {
	if c.secret == "" {
		return ""
	}
	str := fmt.Sprintf("%d\n%s", timestamp, c.secret)
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write([]byte(str))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return sign
}

func (c *DingTalkChannel) postJSON(url string, payload map[string]any) error {
	data, _ := json.Marshal(payload)
	resp, err := http.DefaultClient.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ── Telegram Channel ──

type TelegramChannel struct {
	botToken string
	chatID   string
	enabled  bool
}

func (c *TelegramChannel) Name() string    { return "telegram" }
func (c *TelegramChannel) IsEnabled() bool { return c.enabled }

func (c *TelegramChannel) Send(msg Message) error {
	if c.botToken == "" || c.chatID == "" {
		return fmt.Errorf("telegram not configured")
	}

	levelEmoji := "ℹ️"
	switch strings.ToUpper(msg.Level) {
	case "CRITICAL":
		levelEmoji = "🔴"
	case "WARN":
		levelEmoji = "⚠️"
	}

	text := fmt.Sprintf("%s *[%s] %s*\n\n%s\n\n_%s_",
		levelEmoji,
		strings.ToUpper(msg.Level),
		msg.Title,
		msg.Content,
		time.UnixMilli(msg.Timestamp).Format("2006-01-02 15:04:05"),
	)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)
	payload := map[string]any{
		"chat_id":    c.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	data, _ := json.Marshal(payload)
	resp, err := http.DefaultClient.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
