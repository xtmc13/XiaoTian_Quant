package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ── Cloudflare Turnstile 人机验证（A3.2） ──
// 仅在配置 TURNSTILE_SECRET_KEY 环境变量时启用；未配置则完全跳过（向后兼容）。

const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

var turnstileHTTPClient = &http.Client{Timeout: 10 * time.Second}

// turnstileSecret 返回服务端密钥；空串表示未启用。
func turnstileSecret() string { return strings.TrimSpace(os.Getenv("TURNSTILE_SECRET_KEY")) }

// requireTurnstile 校验前端提交的 cf-turnstile-response。未配置密钥直接放行；
// 已配置时校验失败返回 true 并写出 400 响应。
func requireTurnstile(c *gin.Context, token string) bool {
	secret := turnstileSecret()
	if secret == "" {
		return false
	}
	if strings.TrimSpace(token) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "captcha verification required"})
		return true
	}

	form := url.Values{
		"secret":   {secret},
		"response": {token},
		"remoteip": {c.ClientIP()},
	}
	resp, err := turnstileHTTPClient.PostForm(turnstileVerifyURL, form)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"detail": "captcha verification unavailable"})
		return true
	}
	defer resp.Body.Close()

	var body struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || !body.Success {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "captcha verification failed"})
		return true
	}
	return false
}
