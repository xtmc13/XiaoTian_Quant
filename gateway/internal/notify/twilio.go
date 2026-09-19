package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// ── Twilio SMS Channel（A5.1）──────────────────────────────────
//
// 配置走与 Telegram bot token 相同的环境变量机制：
//   - TWILIO_ACCOUNT_SID / TWILIO_AUTH_TOKEN：Basic Auth（SID 为用户名）
//   - TWILIO_FROM_NUMBER：Twilio 发信号码（E.164，如 +15551234567）
//   - TWILIO_TO_NUMBER：默认接收号码（路由规则未指定 receiver 时使用）
//
// 凭证如需加密存放，可用现有凭证保险库（store.CredentialVault）保存后
// 以 env 注入进程——与 Telegram bot token 同等待遇。

// twilioAPIBase 可被 TWILIO_API_BASE 覆盖（httptest 注入用）。
var twilioAPIBase = os.Getenv("TWILIO_API_BASE")
var twilioHTTPClient = &http.Client{Timeout: 10 * time.Second}

// smsBodyLimit Twilio 单条短信正文上限（字符）。
const smsBodyLimit = 1600

// TwilioChannel delivers notifications via Twilio Messages API (SMS).
type TwilioChannel struct {
	accountSID string
	authToken  string
	fromNumber string
	defaultTo  string
	enabled    bool
	httpClient *http.Client
}

// NewTwilioChannelFromEnv 从环境变量构建渠道（未配置 account_sid/token/from 时禁用）。
func NewTwilioChannelFromEnv() *TwilioChannel {
	return &TwilioChannel{
		accountSID: os.Getenv("TWILIO_ACCOUNT_SID"),
		authToken:  os.Getenv("TWILIO_AUTH_TOKEN"),
		fromNumber: os.Getenv("TWILIO_FROM_NUMBER"),
		defaultTo:  os.Getenv("TWILIO_TO_NUMBER"),
		enabled:    os.Getenv("TWILIO_ACCOUNT_SID") != "" && os.Getenv("TWILIO_AUTH_TOKEN") != "" && os.Getenv("TWILIO_FROM_NUMBER") != "",
		httpClient: twilioHTTPClient,
	}
}

func (c *TwilioChannel) Name() string    { return "sms" }
func (c *TwilioChannel) IsEnabled() bool { return c.enabled }

func (c *TwilioChannel) Send(msg Message) error {
	to := strings.TrimSpace(msg.Tags["sms_to"])
	if to == "" {
		to = c.defaultTo
	}
	if to == "" {
		return fmt.Errorf("twilio: no recipient (set TWILIO_TO_NUMBER or tag sms_to)")
	}
	if c.accountSID == "" || c.authToken == "" || c.fromNumber == "" {
		return fmt.Errorf("twilio: not configured")
	}

	body := SMSText(msg)

	form := url.Values{}
	form.Set("To", to)
	form.Set("From", c.fromNumber)
	form.Set("Body", body)

	base := twilioAPIBase
	if base == "" {
		base = "https://api.twilio.com"
	}
	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", strings.TrimRight(base, "/"), c.accountSID)

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.accountSID, c.authToken)

	client := c.httpClient
	if client == nil {
		client = twilioHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("twilio: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var apiErr struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		}
		_ = json.Unmarshal(raw, &apiErr)
		if apiErr.Message != "" {
			return fmt.Errorf("twilio: status %d: %s (code %d)", resp.StatusCode, apiErr.Message, apiErr.Code)
		}
		return fmt.Errorf("twilio: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// SMSText 复用现有通知模板渲染短信正文：
// 纯文本化（去掉 Markdown 强调符号），格式 "标题\n\n内容\n\n[级别] 时间"，
// 超长按字符截断到 1600（Twilio 上限），截断点避开 UTF-8 多字节字符中间。
func SMSText(msg Message) string {
	title := strings.TrimSpace(stripMarkdown(msg.Title))
	content := strings.TrimSpace(stripMarkdown(msg.Content))

	var b strings.Builder
	b.WriteString(title)
	if content != "" && content != title {
		b.WriteString("\n\n")
		b.WriteString(content)
	}
	if t := time.UnixMilli(msg.Timestamp); msg.Timestamp > 0 {
		b.WriteString(fmt.Sprintf("\n\n[%s] %s", strings.ToUpper(msg.Level), t.Format("2006-01-02 15:04:05")))
	} else {
		b.WriteString(fmt.Sprintf("\n\n[%s]", strings.ToUpper(msg.Level)))
	}

	text := b.String()
	if utf8.RuneCountInString(text) <= smsBodyLimit {
		return text
	}
	// 按字符截断，预留 "…" 空间
	runes := []rune(text)
	return string(runes[:smsBodyLimit-1]) + "…"
}

// stripMarkdown 把模板里用于 IM 的 Markdown 标记转成纯文本，适配短信。
func stripMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"**", "",
		"__", "",
		"`", "",
		"~~", "",
	)
	return replacer.Replace(s)
}
