package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Profile ──

func GetProfile(c *gin.Context) {
	userID := c.GetInt("user_id")
	username := c.GetString("username")
	role := c.GetString("role")

	user := store.FindUserByUsername(username)
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}

	// Build profile response
	profile := gin.H{
		"id":             userID,
		"username":       username,
		"nickname":       user["nickname"],
		"email":          user["email"],
		"role":           role,
		"is_active":      user["is_active"],
		"email_verified": user["email_verified"],
		"created_at":     user["created_at"],
		"credits":        0,
		"is_vip":         false,
		"referred_by":    nil,
		"referral_code":  username,
		"referral_count": 0,
		"last_login_at":  nil,
	}

	c.JSON(http.StatusOK, profile)
}

type UpdateProfileRequest struct {
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
}

func UpdateProfile(c *gin.Context) {
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request"})
		return
	}

	username := c.GetString("username")

	if req.Nickname != "" {
		store.UpdateUserProfile(username, "nickname", req.Nickname)
	}

	if req.Email != "" {
		if !strings.Contains(req.Email, "@") {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid email"})
			return
		}
		// Check email not taken by another user
		existing := store.FindUserByEmail(req.Email)
		if existing != nil && existing["username"].(string) != username {
			c.JSON(http.StatusConflict, gin.H{"detail": "email already in use"})
			return
		}
		store.UpdateUserProfile(username, "email", req.Email)
		store.UpdateUserProfile(username, "email_verified", "0")
	}

	c.JSON(http.StatusOK, gin.H{"detail": "profile updated"})
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request"})
		return
	}

	if len(req.NewPassword) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Password must be at least 6 characters"})
		return
	}

	username := c.GetString("username")
	user := store.FindUserByUsername(username)
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
		return
	}

	// Verify old password
	if !store.VerifyPassword(req.OldPassword, user["password_hash"].(string)) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "incorrect current password"})
		return
	}

	newHash := store.HashPassword(req.NewPassword)
	if err := store.UpdateUserPassword(user["id"].(int), newHash); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"detail": "password changed"})
}

// ── Notification Settings ──

func GetNotificationSettings(c *gin.Context) {
	username := c.GetString("username")
	settings := loadUserNotificationSettings(username)
	c.JSON(http.StatusOK, settings)
}

type UpdateNotificationRequest struct {
	Channels map[string]bool `json:"channels"`
}

func UpdateNotificationSettings(c *gin.Context) {
	var req UpdateNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request"})
		return
	}

	username := c.GetString("username")
	saveUserNotificationSettings(username, req.Channels)

	c.JSON(http.StatusOK, gin.H{"detail": "notification settings saved", "channels": req.Channels})
}

// ── Helpers ──

// loadUserNotificationSettings reads notification prefs from a simple JSON file per user.
func loadUserNotificationSettings(username string) map[string]bool {
	defaults := map[string]bool{
		"browser":  true,
		"email":    true,
		"telegram": false,
		"sms":      false,
		"discord":  false,
		"webhook":  false,
	}

	data, err := os.ReadFile(notifySettingsPath(username))
	if err != nil {
		return defaults
	}

	var saved map[string]bool
	if err := json.Unmarshal(data, &saved); err != nil {
		return defaults
	}

	for k, v := range saved {
		defaults[k] = v
	}
	return defaults
}

func saveUserNotificationSettings(username string, channels map[string]bool) {
	data, _ := json.MarshalIndent(channels, "", "  ")
	os.WriteFile(notifySettingsPath(username), data, 0644)
}

func notifySettingsPath(username string) string {
	return "data/notify_" + username + ".json"
}

// ── Admin: User Management ──

func AdminGetUser(c *gin.Context) {
	userID := c.Param("id")
	// Find user by scanning all users
	users := store.ListAllUsers()
	for _, u := range users {
		if id, ok := u["id"].(int); ok && fmt.Sprintf("%d", id) == userID {
			c.JSON(http.StatusOK, u)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"detail": "user not found"})
}

type AdminUpdateUserRequest struct {
	Nickname *string `json:"nickname"`
	Email    *string `json:"email"`
	Role     *string `json:"role"`
	IsActive *int    `json:"is_active"`
}

func AdminUpdateUser(c *gin.Context) {
	userID := c.Param("id")

	var req AdminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid request"})
		return
	}

	updates := make(map[string]any)
	if req.Nickname != nil {
		updates["nickname"] = *req.Nickname
	}
	if req.Email != nil {
		updates["email"] = *req.Email
	}
	if req.Role != nil {
		if *req.Role != "admin" && *req.Role != "user" && *req.Role != "manager" {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid role"})
			return
		}
		updates["role"] = *req.Role
	}
	if req.IsActive != nil {
		updates["is_active"] = float64(*req.IsActive)
	}

	// Parse userID
	var uid int
	if _, err := fmt.Sscanf(userID, "%d", &uid); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid user id"})
		return
	}

	if err := store.AdminUpdateUser(uid, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"detail": "user updated"})
}

// AdminStats returns admin dashboard statistics.
func AdminStats(c *gin.Context) {
	users := store.ListAllUsers()
	total := len(users)
	activeCount := 0
	adminCount := 0
	for _, u := range users {
		if isActive, ok := u["is_active"].(int); ok && isActive == 1 {
			activeCount++
		}
		if role, ok := u["role"].(string); ok && role == "admin" {
			adminCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_users":  total,
		"active_users": activeCount,
		"admin_count":  adminCount,
		"user_count":   total - adminCount,
	})
}

