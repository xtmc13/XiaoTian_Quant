package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agent"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func GetAgentTokens(c *gin.Context) {
	// C3: 弃用内存 JSON store，改走 repo 并按属主过滤；
	// 列表只回 token 前缀（明文只在创建时返回一次）。
	repo := store.NewAgentTokenRepo()
	filter := map[string]any{}
	if uid, restricted := agentTokenRestricted(c); restricted {
		filter["user_id"] = uid
	}
	recs, err := repo.List(filter, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	tokens := make([]map[string]any, 0, len(recs))
	for _, t := range recs {
		tokens = append(tokens, agentTokenToJSON(t, ""))
	}
	c.JSON(http.StatusOK, tokens)
}

// agentTokenRestricted 当前请求是否需按属主过滤（登录非 admin）。
func agentTokenRestricted(c *gin.Context) (int64, bool) {
	uid, injected := ctxUserID(c)
	return int64(uid), injected && !ctxIsAdmin(c)
}

// agentTokenToJSON 把 repo 记录转成前端 AgentToken 结构；
// plaintext 非空时（创建场景）填入明文 token，否则只给前缀。
func agentTokenToJSON(t *store.AgentTokenRecord, plaintext string) map[string]any {
	scopes := []string{}
	for _, s := range strings.Split(t.Scopes, ",") {
		if s = strings.TrimSpace(s); s != "" {
			scopes = append(scopes, s)
		}
	}
	token := t.TokenPrefix
	if plaintext != "" {
		token = plaintext
	}
	m := map[string]any{
		"id":         strconv.Itoa(t.ID),
		"name":       t.Name,
		"token":      token,
		"scopes":     scopes,
		"created_at": time.UnixMilli(t.CreatedAt).UTC().Format(time.RFC3339),
		"expires_at": nil,
		"last_used":  nil,
	}
	if t.ExpiresAt > 0 {
		m["expires_at"] = time.UnixMilli(t.ExpiresAt).UTC().Format(time.RFC3339)
	}
	if t.LastUsedAt > 0 {
		m["last_used"] = time.UnixMilli(t.LastUsedAt).UTC().Format(time.RFC3339)
	}
	return m
}

func CreateAgentToken(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	name := getString(data, "name", "Untitled")
	// scopes 兼容数组与逗号分隔字符串
	scopes := "read"
	switch v := data["scopes"].(type) {
	case []any:
		parts := []string{}
		for _, s := range v {
			if str, ok := s.(string); ok && str != "" {
				parts = append(parts, str)
			}
		}
		if len(parts) > 0 {
			scopes = strings.Join(parts, ",")
		}
	case string:
		if strings.TrimSpace(v) != "" {
			scopes = v
		}
	}
	var userID int64
	if uid, injected := ctxUserID(c); injected {
		userID = int64(uid)
	}
	tokenValue, rec, err := agent.GetTokenManager().CreateTokenForUser(name, scopes, 10, int64(getFloat(data, "expires_in", 0)), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, agentTokenToJSON(rec, tokenValue))
}

func DeleteAgentToken(c *gin.Context) {
	id := c.Param("id")
	repo := store.NewAgentTokenRepo()
	if uid, restricted := agentTokenRestricted(c); restricted {
		// C3: 带属主条件删除，非属主按 not found 处理
		if err := repo.DeleteForUser(id, uid); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "detail": "Token not found"})
			return
		}
	} else if err := repo.Delete(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "detail": "Token not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func CCSwitchStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"enabled":        false,
		"mode":           "manual",
		"current_model":  "",
	})
}

func CCSwitchConfigure(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	c.JSON(http.StatusOK, gin.H{"enabled": true, "mode": getString(data, "mode", "manual"), "current_model": getString(data, "current_model", "")})
}

func CCSwitchStart(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": true, "mode": "auto", "current_model": "default"})
}

func CCSwitchStop(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": false, "mode": "manual", "current_model": ""})
}

func GetAgentAIConfig(c *gin.Context) {
	cfg := store.GetConfig()
	agentCfg, _ := cfg["agent"].(map[string]any)
	aiCfg, _ := agentCfg["ai"].(map[string]any)
	if aiCfg == nil {
		aiCfg = make(map[string]any)
	}
	c.JSON(http.StatusOK, gin.H{
		"model":          getString(aiCfg, "model", "deepseek-chat"),
		"temperature":    0.7,
		"max_tokens":     2048,
		"system_prompt":  getString(aiCfg, "system_prompt", ""),
	})
}

func SaveAgentAIConfig(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	cfg := store.GetConfig()
	agentCfg, _ := cfg["agent"].(map[string]any)
	if agentCfg == nil {
		agentCfg = make(map[string]any)
		cfg["agent"] = agentCfg
	}
	aiCfg, _ := agentCfg["ai"].(map[string]any)
	if aiCfg == nil {
		aiCfg = make(map[string]any)
		agentCfg["ai"] = aiCfg
	}
	for _, k := range []string{"provider", "api_key", "base_url", "model", "http_proxy", "https_proxy", "system_prompt"} {
		if v, ok := data[k].(string); ok {
			aiCfg[k] = v
		}
	}
	if v, ok := data["proxy_enabled"]; ok {
		aiCfg["proxy_enabled"] = v
	}
	if v, ok := data["temperature"].(float64); ok {
		aiCfg["temperature"] = v
	}
	if v, ok := data["max_tokens"].(float64); ok {
		aiCfg["max_tokens"] = int(v)
	}
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{
		"model":         getString(aiCfg, "model", "deepseek-chat"),
		"temperature":   0.7,
		"max_tokens":    2048,
		"system_prompt": getString(aiCfg, "system_prompt", ""),
	})
}

func AgentAITest(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Connected (HTTP 200)"})
}

func AgentChat(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	message := getString(data, "message", "")
	if message == "" {
		c.JSON(http.StatusOK, gin.H{"reply": "Please send a message."})
		return
	}

	// Use configured provider or default to deepseek
	cfg := store.GetConfig()
	agentCfg, _ := cfg["agent"].(map[string]any)
	aiCfg, _ := agentCfg["ai"].(map[string]any)
	providerName := "deepseek"
	if aiCfg != nil {
		if p, ok := aiCfg["provider"].(string); ok && p != "" {
			providerName = p
		}
	}

	provider := ai.GetProvider(providerName)
	if provider == nil || provider.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"reply":  fmt.Sprintf("AI provider '%s' not configured. Please set API key in Settings → AI.", providerName),
		})
		return
	}

	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages:    []ai.ChatMessage{{Role: ai.RoleUser, Content: message}},
		MaxTokens:   2048,
		Temperature: 0.7,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"reply":  "AI service error: " + err.Error(),
		})
		return
	}
	if len(resp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"reply":  "AI returned empty response.",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"reply":  resp.Choices[0].Message.Content,
	})
}

// GetAgentAuditLog returns recent agent API call audit records.
func GetAgentAuditLog(c *gin.Context) {
	limit := 50
	var userID int64
	if uid, restricted := agentTokenRestricted(c); restricted {
		userID = uid // C3: 普通用户只看自己的审计记录
	}
	logs := store.GetAgentAuditLog(limit, userID)
	if logs == nil {
		logs = []map[string]any{}
	}
	c.JSON(http.StatusOK, logs)
}

// ChatSend handles the AI discussion room multi-model chat (/api/chat/send)
func ChatSend(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	message := getString(data, "message", "")

	enabledModels := []string{}
	if models, ok := data["enabled_models"].([]any); ok {
		for _, m := range models {
			if s, ok := m.(string); ok {
				enabledModels = append(enabledModels, s)
			}
		}
	}
	if len(enabledModels) == 0 {
		enabledModels = []string{"deepseek", "claude"}
	}

	type modelResponse struct {
		model    string
		reply    string
		signal   string
		err      string
	}

	var wg sync.WaitGroup
	results := make(chan modelResponse, len(enabledModels))

	for _, model := range enabledModels {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			provider := ai.GetProvider(m)
			if provider == nil || provider.APIKey == "" {
				results <- modelResponse{
					model: m,
					err:   fmt.Sprintf("Provider '%s' not configured", m),
				}
				return
			}
			resp, err := provider.ChatCompletion(ai.CompletionRequest{
				Messages: []ai.ChatMessage{
					{Role: ai.RoleSystem, Content: "You are a trading assistant. Give a concise 1-2 sentence view. End with a signal: bullish, bearish, or neutral."},
					{Role: ai.RoleUser, Content: message},
				},
				MaxTokens:   512,
				Temperature: 0.7,
			})
			if err != nil {
				results <- modelResponse{model: m, err: err.Error()}
				return
			}
			if len(resp.Choices) == 0 {
				results <- modelResponse{model: m, err: "Empty response"}
				return
			}
			content := resp.Choices[0].Message.Content
			signal := "neutral"
			if containsSignal(content, "bullish") {
				signal = "bullish"
			} else if containsSignal(content, "bearish") {
				signal = "bearish"
			}
			results <- modelResponse{model: m, reply: content, signal: signal}
		}(model)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	responses := make([]map[string]any, 0)
	for r := range results {
		if r.err != "" {
			responses = append(responses, map[string]any{
				"model":    r.model,
				"reply":    fmt.Sprintf("[%s] Error: %s", r.model, r.err),
				"signal":   "neutral",
				"language": "zh",
			})
		} else {
			responses = append(responses, map[string]any{
				"model":    r.model,
				"reply":    r.reply,
				"signal":   r.signal,
				"language": "zh",
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"responses": responses,
		"message":   message,
	})
}

func containsSignal(text, signal string) bool {
	return len(text) > 0 && (func() bool {
		for i := 0; i <= len(text)-len(signal); i++ {
			if text[i:i+len(signal)] == signal {
				return true
			}
		}
		return false
	}())
}
