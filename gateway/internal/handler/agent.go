package handler

import (
	"context"
	"fmt"
	"net"
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

// ── CC Switch：Anthropic ↔ OpenAI 格式转换代理 ──
// 真实启停：Start 在后台 goroutine 里跑 agent.ServeCCSwitch HTTP 代理，
// 运行状态（端口/baseURL/started_at/running）存包级变量；Status 读该变量。
// 响应 JSON 形状保持 {enabled, mode, current_model} 不变。

type ccSwitchRuntime struct {
	mu        sync.Mutex
	running   bool
	mode      string // manual | auto
	model     string // 当前代理的模型
	targetURL string // 上游 OpenAI 兼容后端
	port      int
	startedAt int64
	srv       *http.Server
}

var (
	ccSwitchState ccSwitchRuntime
	ccSwitchOnce  sync.Once
)

// ccSwitchEnsureLoaded 首次使用时从 store 配置恢复 mode/model/targetURL/port。
func ccSwitchEnsureLoaded() {
	ccSwitchOnce.Do(func() {
		cfg := store.GetConfig()
		agentCfg, _ := cfg["agent"].(map[string]any)
		ccCfg, _ := agentCfg["cc_switch"].(map[string]any)
		ccSwitchState.mode = getString(ccCfg, "mode", "manual")
		ccSwitchState.model = getString(ccCfg, "model", "")
		ccSwitchState.targetURL = strings.TrimRight(getString(ccCfg, "target_url", ""), "/")
		ccSwitchState.port = int(getFloat(ccCfg, "port", 0))
		if ccSwitchState.port <= 0 {
			ccSwitchState.port = 8899
		}
	})
}

// ccSwitchPersistConfig 把当前配置（不含运行态）落盘到 agent.cc_switch。
func ccSwitchPersistConfig() {
	cfg := store.GetConfig()
	agentCfg, _ := cfg["agent"].(map[string]any)
	if agentCfg == nil {
		agentCfg = map[string]any{}
		cfg["agent"] = agentCfg
	}
	ccCfg, _ := agentCfg["cc_switch"].(map[string]any)
	if ccCfg == nil {
		ccCfg = map[string]any{}
		agentCfg["cc_switch"] = ccCfg
	}
	ccSwitchState.mu.Lock()
	ccCfg["mode"] = ccSwitchState.mode
	ccCfg["model"] = ccSwitchState.model
	ccCfg["target_url"] = ccSwitchState.targetURL
	ccCfg["port"] = ccSwitchState.port
	ccSwitchState.mu.Unlock()
	_ = store.SaveConfig(cfg)
}

// ccSwitchSnapshot 读运行状态快照。
func ccSwitchSnapshot() (running bool, mode, model string) {
	ccSwitchState.mu.Lock()
	defer ccSwitchState.mu.Unlock()
	return ccSwitchState.running, ccSwitchState.mode, ccSwitchState.model
}

// ccSwitchResolveTarget 解析上游后端：优先 cc_switch.target_url + agent.ai.api_key，
// 否则按 agent.ai.provider（或 base_url）取已注册 provider 的地址与密钥。
func ccSwitchResolveTarget() (targetURL, apiKey, model string) {
	cfg := store.GetConfig()
	agentCfg, _ := cfg["agent"].(map[string]any)
	aiCfg, _ := agentCfg["ai"].(map[string]any)
	ccCfg, _ := agentCfg["cc_switch"].(map[string]any)

	targetURL = strings.TrimRight(getString(ccCfg, "target_url", ""), "/")
	apiKey = getString(aiCfg, "api_key", "")
	model = getString(ccCfg, "model", "")
	if model == "" {
		model = getString(aiCfg, "model", "")
	}

	if targetURL == "" {
		providerName := getString(aiCfg, "provider", "deepseek")
		if p := ai.GetProvider(providerName); p != nil {
			targetURL = strings.TrimRight(p.BaseURL, "/")
			if apiKey == "" {
				apiKey = p.APIKey
			}
			if model == "" {
				model = p.Model
			}
		}
	}
	_, _, stateModel := ccSwitchSnapshot()
	if model == "" {
		model = stateModel
	}
	return targetURL, apiKey, model
}

func CCSwitchStatus(c *gin.Context) {
	ccSwitchEnsureLoaded()
	running, mode, model := ccSwitchSnapshot()
	c.JSON(http.StatusOK, gin.H{
		"enabled":       running,
		"mode":          mode,
		"current_model": model,
	})
}

func CCSwitchConfigure(c *gin.Context) {
	ccSwitchEnsureLoaded()
	var data map[string]any
	c.ShouldBindJSON(&data)

	ccSwitchState.mu.Lock()
	if m := getString(data, "mode", ""); m != "" {
		ccSwitchState.mode = m
	}
	if m := getString(data, "current_model", ""); m != "" {
		ccSwitchState.model = m
	}
	if u := getString(data, "target_url", ""); u != "" {
		ccSwitchState.targetURL = strings.TrimRight(u, "/")
	}
	if p := int(getFloat(data, "port", 0)); p > 0 {
		ccSwitchState.port = p
	}
	running := ccSwitchState.running
	mode := ccSwitchState.mode
	model := ccSwitchState.model
	ccSwitchState.mu.Unlock()

	ccSwitchPersistConfig()
	c.JSON(http.StatusOK, gin.H{"enabled": running, "mode": mode, "current_model": model})
}

func CCSwitchStart(c *gin.Context) {
	ccSwitchEnsureLoaded()

	ccSwitchState.mu.Lock()
	if ccSwitchState.running {
		mode := ccSwitchState.mode
		model := ccSwitchState.model
		ccSwitchState.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"enabled": true, "mode": mode, "current_model": model})
		return
	}
	port := ccSwitchState.port
	ccSwitchState.mu.Unlock()

	targetURL, apiKey, model := ccSwitchResolveTarget()
	if targetURL == "" || apiKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"enabled": false,
			"error":   "CC Switch target not configured: set agent AI provider API key first",
		})
		return
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"enabled": false, "error": err.Error()})
		return
	}
	srv := &http.Server{Handler: agent.ServeCCSwitch(targetURL, apiKey, model)}
	go func() {
		_ = srv.Serve(ln) // 关闭走 Shutdown，Serve 返回的 error 忽略
	}()

	ccSwitchState.mu.Lock()
	ccSwitchState.running = true
	ccSwitchState.mode = "auto"
	ccSwitchState.model = model
	ccSwitchState.targetURL = targetURL
	ccSwitchState.startedAt = time.Now().UnixMilli()
	ccSwitchState.srv = srv
	ccSwitchState.mu.Unlock()
	ccSwitchPersistConfig()

	c.JSON(http.StatusOK, gin.H{"enabled": true, "mode": "auto", "current_model": model})
}

func CCSwitchStop(c *gin.Context) {
	ccSwitchEnsureLoaded()

	ccSwitchState.mu.Lock()
	srv := ccSwitchState.srv
	ccSwitchState.srv = nil
	ccSwitchState.running = false
	ccSwitchState.mode = "manual"
	ccSwitchState.mu.Unlock()

	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
	ccSwitchPersistConfig()

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

// AgentAITest 真实拨测：对配置 provider 发一个 1-token 的 ChatCompletion，
// 成功回 {success:true,message:"OK",model,latency_ms}，失败回 {success:false,message}。
func AgentAITest(c *gin.Context) {
	provider, providerName := configuredAgentAIProvider()
	if provider == nil || provider.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("AI provider '%s' not configured. Please set API key in Settings → AI.", providerName),
		})
		return
	}

	started := time.Now()
	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages:  []ai.ChatMessage{{Role: ai.RoleUser, Content: "ping"}},
		MaxTokens: 1,
	})
	latency := time.Since(started).Milliseconds()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if resp == nil || len(resp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "AI returned empty response"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "OK",
		"model":      provider.Model,
		"latency_ms": latency,
	})
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
