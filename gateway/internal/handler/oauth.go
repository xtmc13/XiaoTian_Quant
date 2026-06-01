package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// OAuth state tokens (in-memory, 10 minute expiry)
var oauthStates = map[string]oauthStateEntry{}

type oauthStateEntry struct {
	RedirectURI string
	CreatedAt   time.Time
}

func init() {
	go func() {
		for range time.Tick(5 * time.Minute) {
			now := time.Now()
			for k, v := range oauthStates {
				if now.Sub(v.CreatedAt) > 10*time.Minute {
					delete(oauthStates, k)
				}
			}
		}
	}()
}

// ── Google OAuth ───────────────────────────────────────────────

func OAuthGoogleLogin(c *gin.Context) {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	if clientID == "" {
		c.JSON(http.StatusOK, gin.H{"error": "Google OAuth not configured"})
		return
	}

	state := generateState()
	redirectURI := c.Query("redirect")
	if redirectURI == "" {
		redirectURI = "/dashboard"
	}

	oauthStates[state] = oauthStateEntry{RedirectURI: redirectURI, CreatedAt: time.Now()}

	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=email%%20profile&state=%s",
		clientID, url.QueryEscape(os.Getenv("GOOGLE_REDIRECT_URI")), state,
	)
	c.JSON(http.StatusOK, gin.H{"url": authURL})
}

func OAuthGoogleCallback(c *gin.Context) {
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	code := c.Query("code")
	state := c.Query("state")

	if _, ok := oauthStates[state]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid state"})
		return
	}
	redirectURI := oauthStates[state].RedirectURI
	delete(oauthStates, state)

	// Exchange code for token
	tokenURL := "https://oauth2.googleapis.com/token"
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {os.Getenv("GOOGLE_REDIRECT_URI")},
	}

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "OAuth token exchange failed"})
		return
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &tokenResp)

	if tokenResp.AccessToken == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to get access token"})
		return
	}

	// Get user info
	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	userResp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to get user info"})
		return
	}
	defer userResp.Body.Close()

	var userInfo struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	userBody, _ := io.ReadAll(userResp.Body)
	json.Unmarshal(userBody, &userInfo)

	// Auto-register/login
	autoAuthUser(c, userInfo.Email, userInfo.Name, "google")
	c.Redirect(http.StatusTemporaryRedirect, redirectURI)
}

// ── GitHub OAuth ───────────────────────────────────────────────

func OAuthGitHubLogin(c *gin.Context) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")
	if clientID == "" {
		c.JSON(http.StatusOK, gin.H{"error": "GitHub OAuth not configured"})
		return
	}

	state := generateState()
	redirectURI := c.Query("redirect")
	if redirectURI == "" {
		redirectURI = "/dashboard"
	}
	oauthStates[state] = oauthStateEntry{RedirectURI: redirectURI, CreatedAt: time.Now()}

	authURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=user:email&state=%s",
		clientID, url.QueryEscape(os.Getenv("GITHUB_REDIRECT_URI")), state,
	)
	c.JSON(http.StatusOK, gin.H{"url": authURL})
}

func OAuthGitHubCallback(c *gin.Context) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
	code := c.Query("code")
	state := c.Query("state")

	if _, ok := oauthStates[state]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid state"})
		return
	}
	redirectURI := oauthStates[state].RedirectURI
	delete(oauthStates, state)

	// Exchange code for token
	tokenURL := "https://github.com/login/oauth/access_token"
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {os.Getenv("GITHUB_REDIRECT_URI")},
	}

	req, _ := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "OAuth token exchange failed"})
		return
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &tokenResp)

	if tokenResp.AccessToken == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to get access token"})
		return
	}

	// Get user info
	userReq, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	userReq.Header.Set("Accept", "application/json")
	userResp, err := http.DefaultClient.Do(userReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to get user info"})
		return
	}
	defer userResp.Body.Close()

	var userInfo struct {
		Login    string `json:"login"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	userBody, _ := io.ReadAll(userResp.Body)
	json.Unmarshal(userBody, &userInfo)

	// Get email if not public
	email := userInfo.Email
	if email == "" {
		emailReq, _ := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
		emailReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
		emailReq.Header.Set("Accept", "application/json")
		emailResp, _ := http.DefaultClient.Do(emailReq)
		if emailResp != nil {
			defer emailResp.Body.Close()
			var emails []struct{ Email string `json:"email"`; Primary bool `json:"primary"` }
			emailBody, _ := io.ReadAll(emailResp.Body)
			json.Unmarshal(emailBody, &emails)
			for _, e := range emails {
				if e.Primary { email = e.Email; break }
			}
		}
	}

	name := userInfo.Name
	if name == "" { name = userInfo.Login }

	autoAuthUser(c, email, name, "github")
	c.Redirect(http.StatusTemporaryRedirect, redirectURI)
}

// ── Helpers ────────────────────────────────────────────────────

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func autoAuthUser(c *gin.Context, email, name, provider string) {
	if email == "" || name == "" {
		return
	}

	// Check if user exists
	users := store.ListAllUsers()
	var existingUser map[string]any
	for _, u := range users {
		if e, ok := u["email"].(string); ok && strings.EqualFold(e, email) {
			existingUser = u
			break
		}
	}

	var userID int
	if existingUser != nil {
		if id, ok := existingUser["id"].(int); ok {
			userID = id
		}
	} else {
		// Auto-register with random password
		userID, _ = store.CreateUser(name, generateState()[:16], name, email, "user")
		store.AddAuditLog("oauth", "user_created", fmt.Sprintf("%s via %s", email, provider))
	}

	// Generate JWT and set cookie
	if token, err := store.GenerateJWT(userID, name, "user", 1); err == nil {
		c.SetCookie("token", token, 86400, "/", "", false, true)
	}
}
