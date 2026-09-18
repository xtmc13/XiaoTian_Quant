package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/mfa"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── MFA / TOTP 两步验证（A3.1，对标 QuantDinger mfa_service.py） ──

// MFASetup 生成 TOTP 密钥并暂存到当前用户（尚未启用）。
// POST /api/auth/mfa/setup（需登录）。
func MFASetup(c *gin.Context) {
	userID := c.GetInt("user_id")
	username := c.GetString("username")

	secret, err := mfa.GenerateSecret()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to generate secret"})
		return
	}
	if err := store.SetUserTOTPSecret(userID, secret); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to save secret"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"secret":       secret,
		"otpauth_uri":  mfa.OTPAuthURI("XiaoTianQuant", username, secret),
		"backup_codes": nil,
	})
}

type MFAEnableRequest struct {
	Code string `json:"code"`
}

// MFAEnable 校验一次 TOTP 码后正式启用 MFA，返回一次性备用码。
// POST /api/auth/mfa/enable（需登录）。
func MFAEnable(c *gin.Context) {
	userID := c.GetInt("user_id")

	var req MFAEnableRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "code is required"})
		return
	}

	secret, enabled, found := store.GetUserTOTP(userID)
	if !found || secret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "MFA setup not started, call /mfa/setup first"})
		return
	}
	if enabled {
		c.JSON(http.StatusConflict, gin.H{"detail": "MFA already enabled"})
		return
	}
	// 码错误用 400 而非 401：401 会触发前端拦截器清除登录态。
	if !mfa.Validate(secret, req.Code, time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid TOTP code"})
		return
	}

	backupCodes := mfa.GenerateBackupCodes(8)
	if err := store.EnableUserTOTP(userID, backupCodes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to enable MFA"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"detail":       "MFA enabled",
		"backup_codes": backupCodes,
	})
}

type MFADisableRequest struct {
	Code string `json:"code"`
}

// MFADisable 校验 TOTP 码后关闭 MFA（需登录）。
// POST /api/auth/mfa/disable。
func MFADisable(c *gin.Context) {
	userID := c.GetInt("user_id")

	var req MFADisableRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "code is required"})
		return
	}

	secret, enabled, found := store.GetUserTOTP(userID)
	if !found || !enabled {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "MFA not enabled"})
		return
	}
	// 码错误用 400 而非 401：401 会触发前端拦截器清除登录态。
	if !mfa.Validate(secret, req.Code, time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid TOTP code"})
		return
	}

	if err := store.DisableUserTOTP(userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to disable MFA"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"detail": "MFA disabled"})
}

type MFAVerifyRequest struct {
	MFAToken string `json:"mfa_token"`
	Code     string `json:"code"`
}

// MFAVerify 登录第二步：mfa_token（密码校验后下发的短时效令牌）+ TOTP 码
// 或一次性备用码，换取正式 JWT。POST /api/auth/mfa/verify（公开）。
func MFAVerify(c *gin.Context) {
	var req MFAVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.MFAToken == "" || req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "mfa_token and code are required"})
		return
	}

	userID, err := store.VerifyMFAToken(req.MFAToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "invalid or expired mfa_token"})
		return
	}

	secret, enabled, found := store.GetUserTOTP(userID)
	if !found || !enabled {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "MFA not enabled for this user"})
		return
	}

	// TOTP 码或一次性备用码二选一。码错误用 400 而非 401（401 会触发
	// 前端拦截器清除登录态并跳转）。
	ok := mfa.Validate(secret, req.Code, time.Now())
	if !ok {
		ok = store.ConsumeBackupCode(userID, req.Code)
	}
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid MFA code"})
		return
	}

	issueTokenAfterMFA(c, userID)
}

// issueTokenAfterMFA 在 MFA 第二步校验通过后签发正式 JWT 并处理风险登录通知。
func issueTokenAfterMFA(c *gin.Context, userID int) {
	flags, found := store.GetUserSecurityFlags(userID)
	if !found || !flags.IsActive {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "user unavailable"})
		return
	}

	username, role, nickname := "", "user", ""
	if row := store.FindUserByID(userID); row != nil {
		username, _ = row["username"].(string)
		role, _ = row["role"].(string)
		nickname, _ = row["nickname"].(string)
	}

	token, err := store.GenerateJWT(userID, username, role, flags.TokenVersion)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate token"})
		return
	}

	recordLoginSecurity(c, userID, true)

	c.JSON(http.StatusOK, gin.H{
		"access_token": token, "token_type": "bearer",
		"must_change_password": flags.MustChangePassword,
		"user": gin.H{
			"id": userID, "username": username,
			"role": role, "nickname": nickname,
		},
	})
}
