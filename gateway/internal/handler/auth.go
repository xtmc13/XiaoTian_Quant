package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

type LoginRequest struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	TurnstileToken string `json:"turnstile_token,omitempty"`
}

type RegisterRequest struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	Nickname       string `json:"nickname"`
	Email          string `json:"email"`
	Code           string `json:"code"`
	TurnstileToken string `json:"turnstile_token,omitempty"`
	// ReferralCode 可选推荐码（affiliate，0020）：注册成功写入 referred_by。
	ReferralCode string `json:"referral_code,omitempty"`
}

type SendCodeRequest struct {
	Email    string `json:"email"`
	CodeType string `json:"code_type"`
}

type LoginCodeRequest struct {
	Email          string `json:"email"`
	Code           string `json:"code"`
	TurnstileToken string `json:"turnstile_token,omitempty"`
}

type ResetPasswordRequest struct {
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request"})
		return
	}

	// A3.2: 配置 TURNSTILE_SECRET_KEY 时强制人机验证（未配置则跳过）。
	if requireTurnstile(c, req.TurnstileToken) {
		return
	}

	row := store.FindUserByUsername(req.Username)
	if row == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "Invalid username or password"})
		return
	}
	passHash, ok := row["password_hash"].(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal error"})
		return
	}
	if !store.VerifyPassword(req.Password, passHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "Invalid username or password"})
		return
	}

	userID, ok := row["id"].(int)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal error"})
		return
	}
	username, _ := row["username"].(string)
	role, _ := row["role"].(string)
	tokenVersion, _ := row["token_version"].(int)
	mfaEnabled, _ := row["totp_enabled"].(bool)
	mustChange, _ := row["must_change_password"].(bool)

	// A3.1: 已启用 MFA 的账户密码校验通过后进入第二步，下发短时效临时令牌。
	if mfaEnabled {
		mfaToken, err := store.GenerateMFAToken(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate MFA token"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"mfa_required": true,
			"mfa_token":    mfaToken,
		})
		return
	}

	token, err := store.GenerateJWT(userID, username, role, tokenVersion)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate token"})
		return
	}

	recordLoginSecurity(c, userID, false)

	c.JSON(http.StatusOK, gin.H{
		"access_token": token, "token_type": "bearer",
		"must_change_password": mustChange,
		"user": gin.H{
			"id": row["id"], "username": row["username"],
			"role": row["role"], "nickname": row["nickname"],
		},
	})
}

func Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request"})
		return
	}
	if len(req.Username) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Username must be at least 3 characters"})
		return
	}
	if len(req.Password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Password must be at least 6 characters"})
		return
	}
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Valid email is required"})
		return
	}
	if req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Verification code is required"})
		return
	}

	// A3.2: 配置 TURNSTILE_SECRET_KEY 时注册强制人机验证（未配置则跳过）。
	if requireTurnstile(c, req.TurnstileToken) {
		return
	}

	// Verify code
	valid, msg := store.VerifyCode(req.Email, req.Code, "register")
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"detail": msg})
		return
	}

	// Check username uniqueness
	existing := store.FindUserByUsername(req.Username)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"detail": "Username already exists"})
		return
	}

	// Check email uniqueness
	emailUser := store.FindUserByEmail(req.Email)
	if emailUser != nil {
		c.JSON(http.StatusConflict, gin.H{"detail": "Email already registered"})
		return
	}

	// 可选推荐码校验：存在且 active 才允许注册携带；自己推荐自己一并拒绝。
	var referrerID int64
	if req.ReferralCode != "" {
		code := normalizeReferralCode(req.ReferralCode)
		if !referralCodePattern.MatchString(code) {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid referral code"})
			return
		}
		rc, err := referralRepo.GetCodeByCode(code)
		if err != nil || rc == nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid referral code"})
			return
		}
		if !rc.Active {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "Referral code is inactive"})
			return
		}
		referrerID = rc.UserID
	}

	userID, err := store.CreateUser(req.Username, req.Password, req.Nickname, req.Email, "user")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to create user"})
		return
	}

	// Mark email as verified
	store.SetEmailVerified(userID)

	// 写推荐关系（仅校验通过时 referrerID > 0）。用户已创建成功，此处失败
	// 只记日志不阻断注册（store 层另有自己的推荐守卫）。
	if referrerID > 0 {
		if err := store.SetReferredBy(userID, referrerID); err != nil {
			log.Printf("[referral] set referred_by user=%d referrer=%d failed: %v", userID, referrerID, err)
		}
	}

	token, err := store.GenerateJWT(userID, req.Username, "user", 1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"access_token": token, "token_type": "bearer",
		"user": gin.H{"id": userID, "username": req.Username, "role": "user", "nickname": req.Nickname, "email": req.Email},
	})
}

func GetMe(c *gin.Context) {
	userID := c.GetInt("user_id")
	username := c.GetString("username")
	role := c.GetString("role")
	flags, found := store.GetUserSecurityFlags(userID)
	if !found {
		c.JSON(http.StatusOK, gin.H{"id": userID, "username": username, "role": role})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id": userID, "username": username, "role": role,
		"totp_enabled":         flags.TOTPEnabled,
		"must_change_password": flags.MustChangePassword,
	})
}

// RefreshToken 用数据库里的当前 token_version 重新签发（旧 tv 的令牌
// 在 A3.3 中间件校验中已被拒绝，这里天然只给合法请求发新令牌）。
func RefreshToken(c *gin.Context) {
	userID := c.GetInt("user_id")
	username := c.GetString("username")
	role := c.GetString("role")
	tv, err := store.GetUserTokenVersion(userID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "user not found"})
		return
	}
	token, err := store.GenerateJWT(userID, username, role, tv)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"access_token": token, "token_type": "bearer"})
}

// Logout 退出登录：JWT 无状态，服务端记录审计日志，客户端丢弃令牌即可。
func Logout(c *gin.Context) {
	username := c.GetString("username")
	store.AddAuditLog(username, "logout", "")
	c.JSON(http.StatusOK, gin.H{"detail": "logged out"})
}

func ListUsers(c *gin.Context) {
	users := store.ListAllUsers()
	if users == nil {
		users = []map[string]any{}
	}
	c.JSON(http.StatusOK, users)
}

// ── Email Verification ──

var emailSvc = &notify.EmailService{}

// SendVerificationCode sends a verification code to the given email.
func SendVerificationCode(c *gin.Context) {
	var req SendCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request"})
		return
	}

	if req.Email == "" || !strings.Contains(req.Email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "valid email is required"})
		return
	}

	validTypes := map[string]bool{"register": true, "login": true, "reset_password": true, "change_password": true}
	if !validTypes[req.CodeType] {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid code_type"})
		return
	}

	// Business logic checks per code type
	switch req.CodeType {
	case "register":
		if user := store.FindUserByEmail(req.Email); user != nil {
			c.JSON(http.StatusConflict, gin.H{"detail": "email already registered"})
			return
		}
	case "login", "reset_password":
		if user := store.FindUserByEmail(req.Email); user == nil {
			c.JSON(http.StatusNotFound, gin.H{"detail": "email not found"})
			return
		}
	}

	// Rate limit: 60s between sends
	if ok, wait := store.CanSendCode(req.Email, req.CodeType); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{"detail": "please wait before sending another code", "wait_seconds": wait})
		return
	}

	// Check SMTP configured
	if !emailSvc.IsConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"detail": "email service not configured"})
		return
	}

	code := emailSvc.GenerateCode()
	if err := emailSvc.SendVerificationCode(req.Email, code, req.CodeType); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to send email: " + err.Error()})
		return
	}

	// Store code (10 min TTL)
	ip := c.ClientIP()
	if err := store.SaveVerificationCode(req.Email, code, req.CodeType, ip, 600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to save code"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"detail": "verification code sent", "email": maskEmail(req.Email)})
}

// LoginByCode authenticates a user by email + verification code.
func LoginByCode(c *gin.Context) {
	var req LoginCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request"})
		return
	}

	if req.Email == "" || req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "email and code are required"})
		return
	}

	// A3.2: 配置 TURNSTILE_SECRET_KEY 时登录强制人机验证（未配置则跳过）。
	if requireTurnstile(c, req.TurnstileToken) {
		return
	}

	// Verify code
	valid, msg := store.VerifyCode(req.Email, req.Code, "login")
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"detail": msg})
		return
	}

	// Find or create user
	user := store.FindUserByEmail(req.Email)
	if user == nil {
		// Auto-create user with email-based username
		username := req.Email[:strings.Index(req.Email, "@")]
		// Ensure unique username
		base := username
		for i := 1; store.FindUserByUsername(username) != nil; i++ {
			username = fmt.Sprintf("%s%d", base, i)
		}
		userID, err := store.CreateUser(username, "", username, req.Email, "user")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to create user"})
			return
		}
		store.SetEmailVerified(userID)
		user = map[string]any{
			"id": userID, "username": username, "role": "user",
			"nickname": username, "token_version": 1,
		}
	}

	token, err := store.GenerateJWT(
		user["id"].(int), user["username"].(string), user["role"].(string), user["token_version"].(int),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Failed to generate token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"access_token": token, "token_type": "bearer",
		"user": gin.H{
			"id": user["id"], "username": user["username"],
			"role": user["role"], "nickname": user["nickname"],
		},
	})
}

// ResetPassword resets a user's password using email + verification code.
func ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request"})
		return
	}

	if req.Email == "" || req.Code == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "email, code, and password are required"})
		return
	}
	if len(req.Password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Password must be at least 6 characters"})
		return
	}

	// Verify code
	valid, msg := store.VerifyCode(req.Email, req.Code, "reset_password")
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"detail": msg})
		return
	}

	// Find user
	user := store.FindUserByEmail(req.Email)
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}

	// Update password
	newHash := store.HashPassword(req.Password)
	if err := store.UpdateUserPassword(user["id"].(int), newHash); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"detail": "password reset successfully"})
}

// maskEmail masks part of an email for privacy in responses.
func maskEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email
	}
	name := parts[0]
	if len(name) <= 2 {
		return name[:1] + "***@" + parts[1]
	}
	return name[:2] + "***@" + parts[1]
}
