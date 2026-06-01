package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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
		"created_at":   time.Now().Unix(),
		"last_used_at": nil,
	}
	*store.GetAgentTokensStore() = append(*store.GetAgentTokensStore(), token)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "id": token["id"]})
}

func DeleteAgentToken(c *gin.Context) {
	id := c.Param("id")
	tokens := store.GetAgentTokensStore()
	for i, t := range *tokens {
		if getString(t, "id", "") == id {
			*tokens = append((*tokens)[:i], (*tokens)[i+1:]...)
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"detail": "Token not found"})
}

func CCSwitchStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "stopped",
		"running": false,
		"port":    8082,
	})
}

func CCSwitchConfigure(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "CC Switch configured"})
}

func CCSwitchStart(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "CC Switch started"})
}

func CCSwitchStop(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "CC Switch stopped"})
}

func GetAgentAIConfig(c *gin.Context) {
	cfg := store.GetConfig()
	agentCfg, _ := cfg["agent"].(map[string]any)
	aiCfg, _ := agentCfg["ai"].(map[string]any)
	if aiCfg == nil {
		aiCfg = make(map[string]any)
	}
	c.JSON(http.StatusOK, gin.H{
		"provider":       getString(aiCfg, "provider", ""),
		"api_key":        getString(aiCfg, "api_key", ""),
		"base_url":       getString(aiCfg, "base_url", ""),
		"model":          getString(aiCfg, "model", ""),
		"proxy_enabled":  aiCfg["proxy_enabled"],
		"http_proxy":     getString(aiCfg, "http_proxy", ""),
		"https_proxy":    getString(aiCfg, "https_proxy", ""),
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
	for _, k := range []string{"provider", "api_key", "base_url", "model", "http_proxy", "https_proxy"} {
		if v, ok := data[k].(string); ok {
			aiCfg[k] = v
		}
	}
	if v, ok := data["proxy_enabled"]; ok {
		aiCfg["proxy_enabled"] = v
	}
	store.SaveConfig(cfg)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func AgentAITest(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Connected (HTTP 200)"})
}

func AgentChat(c *gin.Context) {
	var data map[string]any
	c.ShouldBindJSON(&data)
	message := getString(data, "message", "")
	if message == "" {
		c.JSON(http.StatusOK, gin.H{"reply": "Please send a message."})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"reply": "您好！我是小天量化(XiaoTianQuant)的AI助手。这是一个Go网关的placeholder回复。完整的AI聊天功能需要连接到DeepSeek或其他LLM提供商。",
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
		enabledModels = []string{"deepseek-v3", "claude-opus-4-7"}
	}

	responses := make([]map[string]any, 0)
	for _, model := range enabledModels {
		responses = append(responses, map[string]any{
			"model":    model,
			"reply":    fmt.Sprintf("[%s] 收到您的问题：「%s」。这是AI讨论室的placeholder回复，连接真实LLM API后将有完整的多模型讨论功能。", model, message),
			"signal":   "neutral",
			"language": "zh",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"responses": responses,
		"message":   message,
	})
}
