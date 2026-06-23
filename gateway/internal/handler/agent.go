package handler

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func GetAgentTokens(c *gin.Context) {
	tokens := *store.GetAgentTokensStore()
	if tokens == nil {
		tokens = []map[string]any{}
	}
	c.JSON(http.StatusOK, tokens)
}

func CreateAgentToken(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	token := map[string]any{
		"id":           "tok-" + fmt.Sprintf("%d", time.Now().UnixMilli()),
		"name":         getString(data, "name", "Untitled"),
		"token":        getString(data, "token", ""),
		"scopes":       data["scopes"],
		"created_at":   time.Now().Format(time.RFC3339),
		"expires_at":   data["expires_at"],
		"last_used":    nil,
	}
	*store.GetAgentTokensStore() = append(*store.GetAgentTokensStore(), token)
	store.PersistAgentTokens()
	c.JSON(http.StatusOK, token)
}

func DeleteAgentToken(c *gin.Context) {
	id := c.Param("id")
	tokens := store.GetAgentTokensStore()
	for i, t := range *tokens {
		if getString(t, "id", "") == id {
			*tokens = append((*tokens)[:i], (*tokens)[i+1:]...)
			store.PersistAgentTokens()
			c.JSON(http.StatusOK, gin.H{"success": true})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"success": false, "detail": "Token not found"})
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
	logs := store.GetAgentAuditLog(limit)
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
