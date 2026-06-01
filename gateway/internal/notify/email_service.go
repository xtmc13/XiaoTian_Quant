package notify

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/smtp"
	"os"
	"strings"
)

// EmailService sends verification and notification emails to users.
// Separate from the EmailChannel which is for system-level alert notifications.
type EmailService struct{}

// GenerateCode creates a 6-digit random verification code.
func (s *EmailService) GenerateCode() string {
	code := make([]byte, 6)
	for i := range code {
		n, _ := rand.Int(rand.Reader, big.NewInt(10))
		code[i] = byte('0') + byte(n.Int64())
	}
	return string(code)
}

// SendVerificationCode sends a verification code email to the specified address.
// codeType is one of: register, login, reset_password, change_password, change_email
func (s *EmailService) SendVerificationCode(to, code, codeType string) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	from := os.Getenv("SMTP_FROM")

	if host == "" || from == "" {
		return fmt.Errorf("SMTP not configured: SMTP_HOST and SMTP_FROM required")
	}
	if port == "" {
		port = "587"
	}
	if from == "" {
		from = "XiaoTianQuant <noreply@xtquant.com>"
	}

	subject, bodyHTML := s.buildEmail(to, codeType, code)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n"+
		"MIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		from, to, subject, bodyHTML)

	auth := smtp.PlainAuth("", user, pass, host)
	return smtp.SendMail(host+":"+port, auth, from, []string{to}, []byte(msg))
}

// buildEmail returns (subject, htmlBody) based on code type.
func (s *EmailService) buildEmail(to, codeType, code string) (string, string) {
	typeLabels := map[string]string{
		"register":        "注册账号",
		"login":           "邮箱登录",
		"reset_password":  "重置密码",
		"change_password": "修改密码",
		"change_email":    "修改邮箱",
	}
	label, ok := typeLabels[codeType]
	if !ok {
		label = codeType
	}

	subject := fmt.Sprintf("[小天量化] %s验证码", label)

	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family: Arial, sans-serif; background: #f5f5f5; padding: 20px;">
<div style="max-width: 480px; margin: 0 auto; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 2px 8px rgba(0,0,0,0.1);">
  <div style="background: #1a73e8; padding: 24px; text-align: center;">
    <h1 style="color: #fff; margin: 0; font-size: 22px;">小天量化 XiaoTianQuant</h1>
  </div>
  <div style="padding: 32px 24px;">
    <h2 style="margin: 0 0 8px; font-size: 18px; color: #333;">%s验证码</h2>
    <p style="color: #666; line-height: 1.6;">您正在使用邮箱 %s，请输入以下验证码完成操作：</p>
    <div style="text-align: center; margin: 24px 0;">
      <span style="display: inline-block; padding: 12px 32px; background: #f0f4ff; border-radius: 6px; font-size: 28px; font-weight: bold; letter-spacing: 6px; color: #1a73e8; border: 2px dashed #1a73e8;">%s</span>
    </div>
    <p style="color: #999; font-size: 13px;">验证码 10 分钟内有效，请勿泄露给他人。</p>
    <p style="color: #999; font-size: 13px;">如果这不是您的操作，请忽略此邮件。</p>
  </div>
  <div style="background: #fafafa; padding: 16px 24px; text-align: center; border-top: 1px solid #eee;">
    <p style="color: #bbb; font-size: 12px; margin: 0;">此邮件由系统自动发送，请勿回复。</p>
  </div>
</div>
</body>
</html>`, label, to, code)

	return subject, body
}

// IsConfigured checks if SMTP is available for sending emails.
func (s *EmailService) IsConfigured() bool {
	return os.Getenv("SMTP_HOST") != "" && os.Getenv("SMTP_FROM") != ""
}

// SmtpFrom returns the configured from address.
func (s *EmailService) SmtpFrom() string {
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		return "XiaoTianQuant <noreply@xtquant.com>"
	}
	return from
}

// extractUsername extracts username part from email address for display.
func extractUsername(email string) string {
	if idx := strings.Index(email, "@"); idx > 0 {
		return email[:idx]
	}
	return email
}
