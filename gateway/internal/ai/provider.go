package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ── Provider ──

// Provider wraps an AI API endpoint.
type Provider struct {
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"-"`
	Model    string `json:"model"`
	TimeoutS int    `json:"timeout_s"`

	client *http.Client
}

var (
	providersMu sync.RWMutex
	providers   = map[string]*Provider{}
)

// Supported pre-configured providers.
// 默认模型取各家旗舰（与 handler.defaultAIModels 目录保持一致）。
var defaultProviders = []Provider{
	// International
	{Name: "openai", BaseURL: "https://api.openai.com", Model: "gpt-5.5"},
	{Name: "claude", BaseURL: "https://api.anthropic.com", Model: "claude-opus-4-7"},
	{Name: "gemini", BaseURL: "https://generativelanguage.googleapis.com", Model: "gemini-3.1-pro-preview"},
	// Chinese domestic
	{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat"},
	{Name: "qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3-max"},
	{Name: "hunyuan", BaseURL: "https://api.hunyuan.cloud.tencent.com/v1", Model: "hunyuan-pro"},
	{Name: "doubao", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", Model: "doubao-seed-1-6"},
	{Name: "glm", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4.7"},
	{Name: "kimi", BaseURL: "https://api.moonshot.cn/v1", Model: "kimi-k2.5"},
	// Open source
	{Name: "llama", BaseURL: "https://api.groq.com/openai/v1", Model: "llama-4-maverick"},
	{Name: "mistral", BaseURL: "https://api.mistral.ai/v1", Model: "mistral-large-3"},
}

// legacyProviderNames 历史遗留 provider 命名映射：旧版前端/配置里写的 key
// 与运行时注册表不一致，统一在配置读写与运行时解析入口归一到注册表名称。
var legacyProviderNames = map[string]string{
	"anthropic": "claude", // 旧目录/前端用 anthropic，注册表为 claude
}

// NormalizeProviderName 把 legacy/别名 provider 名归一到注册表名称。
func NormalizeProviderName(name string) string {
	if v, ok := legacyProviderNames[strings.ToLower(strings.TrimSpace(name))]; ok {
		return v
	}
	return name
}

// LegacyProviderName 返回注册表名称对应的 legacy 别名（无则空串），
// 用于按注册表名读配置时回退查找 legacy key（如 ai.anthropic → claude）。
func LegacyProviderName(name string) string {
	for legacy, cur := range legacyProviderNames {
		if cur == name {
			return legacy
		}
	}
	return ""
}

// NewEphemeralProvider 构造一个不注册进全局表的临时 Provider（设置页连接测试用），
// 自带超时 client，调用 ChatCompletion 等方法与注册 provider 完全同路径。
func NewEphemeralProvider(name, baseURL, model, apiKey string) *Provider {
	return &Provider{
		Name:     name,
		BaseURL:  baseURL,
		Model:    model,
		APIKey:   apiKey,
		TimeoutS: 60,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func init() {
	for _, dp := range defaultProviders {
		p := dp
		p.client = &http.Client{Timeout: 60 * time.Second}
		key := os.Getenv(strings.ToUpper(p.Name) + "_API_KEY")
		if key == "" {
			key = os.Getenv(strings.ToUpper(p.Name) + "_KEY")
		}
		p.APIKey = key
		p.TimeoutS = 60
		providers[p.Name] = &p
	}
}

// RegisterProvider adds or overrides a provider.
func RegisterProvider(p Provider) {
	providersMu.Lock()
	defer providersMu.Unlock()
	if p.client == nil {
		p.client = &http.Client{Timeout: 60 * time.Second}
	}
	providers[p.Name] = &p
}

// GetProvider returns a registered provider by name.
func GetProvider(name string) *Provider {
	providersMu.RLock()
	defer providersMu.RUnlock()
	return providers[name]
}

// SetProviderAPIKey updates the API key for a registered provider.
func SetProviderAPIKey(name, key string) {
	providersMu.Lock()
	defer providersMu.Unlock()
	if p, ok := providers[name]; ok {
		p.APIKey = key
	}
}

// SetProviderModel updates the model for a registered provider.
func SetProviderModel(name, model string) {
	providersMu.Lock()
	defer providersMu.Unlock()
	if p, ok := providers[name]; ok {
		p.Model = model
	}
}

// SetProviderBaseURL updates the base URL for a registered provider.
// 不做尾缀归一：chatCompletionsURL 已能处理 base 带/不带版本段两种形态。
func SetProviderBaseURL(name, baseURL string) {
	providersMu.Lock()
	defer providersMu.Unlock()
	if p, ok := providers[name]; ok && baseURL != "" {
		p.BaseURL = strings.TrimRight(baseURL, "/")
	}
}

// ListProviders returns all registered provider names.
func ListProviders() []string {
	providersMu.RLock()
	defer providersMu.RUnlock()
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	return names
}

// ── Chat Messages ──

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool" // OpenAI 协议中回传工具结果的角色
)

type ChatMessage struct {
	Role       Role        `json:"role"`
	Content    string      `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`   // assistant 发起的工具调用
	ToolCallID string      `json:"tool_call_id,omitempty"` // tool 角色回传结果时对应的原调用 id（OpenAI）
	ToolResult *ToolResult `json:"-"`                      // 结构化工具结果（Anthropic tool_result / Gemini functionResponse 序列化时优先使用）
}

// ── Completion Request/Response ──

type CompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	Tools       []Tool        `json:"tools,omitempty"`
	ToolChoice  string        `json:"tool_choice,omitempty"`
}

type CompletionResponse struct {
	ID      string        `json:"id"`
	Model   string        `json:"model"`
	Choices []Choice      `json:"choices"`
	Usage   Usage         `json:"usage"`
}

type Choice struct {
	Index   int         `json:"index"`
	Message ChatMessage `json:"message"`
	Delta   ChatMessage `json:"delta"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ── API Call ──

// ChatCompletion sends a chat completion request (OpenAI-compatible format).
func (p *Provider) ChatCompletion(req CompletionRequest) (*CompletionResponse, error) {
	if p.APIKey == "" {
		return nil, fmt.Errorf("%s: API key not configured", p.Name)
	}

	req.Model = p.Model

	// Claude uses Anthropic Messages API
	if p.Name == "claude" {
		return p.claudeChat(req)
	}
	// Gemini uses its own API format
	if p.Name == "gemini" {
		return p.geminiChat(req)
	}

	// OpenAI-compatible (DeepSeek, Qwen, OpenAI)
	return p.openAICompatibleChat(req)
}

// chatCompletionsURL 由 baseURL 推导 OpenAI 兼容 chat completions 端点。
// base 末段已含版本号（/v1、/v3、/v4…）时直接拼 /chat/completions，
// 否则补 /v1——修复 qwen/glm/kimi/doubao 等 base 自带版本段时重复拼 /v1 的问题。
func chatCompletionsURL(baseURL string) string {
	b := strings.TrimRight(baseURL, "/")
	seg := b[strings.LastIndex(b, "/")+1:]
	if len(seg) >= 2 && seg[0] == 'v' && seg[1] >= '0' && seg[1] <= '9' {
		return b + "/chat/completions"
	}
	return b + "/v1/chat/completions"
}

func (p *Provider) openAICompatibleChat(req CompletionRequest) (*CompletionResponse, error) {
	url := chatCompletionsURL(p.BaseURL)
	return p.doChatRequest(url, req)
}

func (p *Provider) doChatRequest(url string, req CompletionRequest) (*CompletionResponse, error) {
	body, _ := json.Marshal(req)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: HTTP %d — %s", p.Name, resp.StatusCode, string(respBody))
	}

	var result CompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// anthropicTools 把通用 Tool 列表转为 Anthropic messages API 的 tools 格式
func anthropicTools(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":         t.Function.Name,
			"description":  t.Function.Description,
			"input_schema": t.Function.Parameters,
		})
	}
	return out
}

// buildAnthropicMessages 把通用消息列表转为 Anthropic messages 格式（支持 tool_use / tool_result content blocks）
func buildAnthropicMessages(messages []ChatMessage) (string, []map[string]any) {
	var systemMsg string
	var anthropicMsgs []map[string]any
	for _, msg := range messages {
		if msg.Role == RoleSystem {
			systemMsg = msg.Content
			continue
		}
		// 工具结果回传：tool_result block（Role 为 user）
		if msg.ToolResult != nil {
			anthropicMsgs = append(anthropicMsgs, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": msg.ToolResult.ToolCallID,
					"content":     msg.ToolResult.Content,
				}},
			})
			continue
		}
		// assistant 回传工具调用历史：text 与 tool_use blocks 混合
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			blocks := []map[string]any{}
			if msg.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				var input map[string]any
				if json.Unmarshal([]byte(tc.Arguments), &input) != nil || input == nil {
					input = map[string]any{}
				}
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": input,
				})
			}
			anthropicMsgs = append(anthropicMsgs, map[string]any{"role": "assistant", "content": blocks})
			continue
		}
		role := "user"
		if msg.Role == RoleAssistant {
			role = "assistant"
		}
		anthropicMsgs = append(anthropicMsgs, map[string]any{
			"role":    role,
			"content": msg.Content,
		})
	}
	return systemMsg, anthropicMsgs
}

// Anthropic Messages API format.
func (p *Provider) claudeChat(req CompletionRequest) (*CompletionResponse, error) {
	systemMsg, anthropicMsgs := buildAnthropicMessages(req.Messages)

	payload := map[string]any{
		"model":      p.Model,
		"max_tokens": 4096,
		"messages":   anthropicMsgs,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if systemMsg != "" {
		payload["system"] = systemMsg
	}
	if len(req.Tools) > 0 {
		payload["tools"] = anthropicTools(req.Tools)
	}

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequest("POST", p.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("claude: HTTP %d — %s", resp.StatusCode, string(respBody))
	}

	var claudeResp map[string]any
	json.Unmarshal(respBody, &claudeResp)

	// Convert to standard format: 解析 text 与 tool_use content blocks
	result := &CompletionResponse{
		ID:    fmt.Sprint(claudeResp["id"]),
		Model: p.Model,
	}
	if content, ok := claudeResp["content"].([]any); ok && len(content) > 0 {
		var text string
		var toolCalls []ToolCall
		for _, cb := range content {
			block, ok := cb.(map[string]any)
			if !ok {
				continue
			}
			if block["type"] == "tool_use" {
				input, _ := json.Marshal(block["input"])
				toolCalls = append(toolCalls, ToolCall{
					ID:        fmt.Sprint(block["id"]),
					Name:      fmt.Sprint(block["name"]),
					Arguments: string(input),
				})
				continue
			}
			if t, ok := block["text"].(string); ok {
				text += t
			}
		}
		result.Choices = []Choice{{
			Index:   0,
			Message: ChatMessage{Role: RoleAssistant, Content: text, ToolCalls: toolCalls},
		}}
	}
	return result, nil
}

// geminiTools 把通用 Tool 列表转为 Gemini functionDeclarations 格式
func geminiTools(tools []Tool) []map[string]any {
	decls := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		decls = append(decls, map[string]any{
			"name":        t.Function.Name,
			"description": t.Function.Description,
			"parameters":  t.Function.Parameters,
		})
	}
	return []map[string]any{{"functionDeclarations": decls}}
}

// buildGeminiContents 把通用消息列表转为 Gemini contents 格式（支持 functionCall / functionResponse parts）
func buildGeminiContents(messages []ChatMessage) []map[string]any {
	var contents []map[string]any
	for _, msg := range messages {
		// 工具结果回传：functionResponse part
		if msg.ToolResult != nil {
			name := msg.ToolResult.Name
			if name == "" {
				name = msg.ToolResult.ToolCallID
			}
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []map[string]any{{
					"functionResponse": map[string]any{
						"name":     name,
						"response": map[string]any{"name": name, "content": msg.ToolResult.Content},
					},
				}},
			})
			continue
		}
		// assistant 回传工具调用历史：functionCall parts
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			parts := []map[string]any{}
			if msg.Content != "" {
				parts = append(parts, map[string]any{"text": msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				var args map[string]any
				if json.Unmarshal([]byte(tc.Arguments), &args) != nil || args == nil {
					args = map[string]any{}
				}
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{"name": tc.Name, "args": args},
				})
			}
			contents = append(contents, map[string]any{"role": "model", "parts": parts})
			continue
		}
		role := "user"
		if msg.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": msg.Content}},
		})
	}
	return contents
}

// Google Gemini API format.
func (p *Provider) geminiChat(req CompletionRequest) (*CompletionResponse, error) {
	contents := buildGeminiContents(req.Messages)

	payload := map[string]any{
		"contents": contents,
	}
	if len(req.Tools) > 0 {
		payload["tools"] = geminiTools(req.Tools)
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", p.BaseURL, p.Model, p.APIKey)

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gemini: HTTP %d — %s", resp.StatusCode, string(respBody))
	}

	var geminiResp map[string]any
	json.Unmarshal(respBody, &geminiResp)

	// Convert to standard format: 解析 text 与 functionCall parts
	result := &CompletionResponse{Model: p.Model}
	if candidates, ok := geminiResp["candidates"].([]any); ok && len(candidates) > 0 {
		if cand, ok := candidates[0].(map[string]any); ok {
			if content, ok := cand["content"].(map[string]any); ok {
				if parts, ok := content["parts"].([]any); ok && len(parts) > 0 {
					var text string
					var toolCalls []ToolCall
					for _, item := range parts {
						part, ok := item.(map[string]any)
						if !ok {
							continue
						}
						if t, ok := part["text"].(string); ok {
							text += t
						}
						if fc, ok := part["functionCall"].(map[string]any); ok {
							args, _ := json.Marshal(fc["args"])
							toolCalls = append(toolCalls, ToolCall{
								Name:      fmt.Sprint(fc["name"]),
								Arguments: string(args),
							})
						}
					}
					result.Choices = []Choice{{
						Index:   0,
						Message: ChatMessage{Role: RoleAssistant, Content: text, ToolCalls: toolCalls},
					}}
				}
			}
		}
	}
	return result, nil
}

// SupportsStream checks if this provider supports streaming.
func (p *Provider) SupportsStream() bool {
	if p == nil || p.APIKey == "" {
		return false
	}
	// Claude and Gemini streaming need dedicated adapters
	return p.Name != "claude" && p.Name != "gemini"
}

// ChatCompletionStream streams the response token by token.
func (p *Provider) ChatCompletionStream(req CompletionRequest, callback func(delta string)) error {
	_, _, err := p.chatCompletionStream(req, callback)
	return err
}

// ChatCompletionStreamEx 流式聊天：文本增量通过回调返回，
// 同时聚合 tool calls（含分片拼接），返回完整文本与工具调用列表。
func (p *Provider) ChatCompletionStreamEx(req CompletionRequest, callback func(delta string)) (string, []ToolCall, error) {
	return p.chatCompletionStream(req, callback)
}

// chatCompletionStream 流式实现：按协议分发，回调文本增量并聚合工具调用
func (p *Provider) chatCompletionStream(req CompletionRequest, callback func(delta string)) (string, []ToolCall, error) {
	if p.Name == "claude" {
		return p.claudeChatStream(req, callback)
	}
	if p.Name == "gemini" {
		return p.geminiChatStream(req, callback)
	}
	return p.openAICompatibleStream(req, callback)
}

func (p *Provider) openAICompatibleStream(req CompletionRequest, callback func(delta string)) (string, []ToolCall, error) {
	req.Stream = true
	req.Model = p.Model

	url := chatCompletionsURL(p.BaseURL)
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	var fullText string
	agg := newToolCallAggregator()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk map[string]any
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if content, ok := delta["content"].(string); ok && content != "" {
			fullText += content
			if callback != nil {
				callback(content)
			}
		}
		// delta.tool_calls 分片按 index 聚合（arguments 为分片字符串，需拼接）
		if items, ok := delta["tool_calls"].([]any); ok {
			for _, item := range items {
				tc, ok := item.(map[string]any)
				if !ok {
					continue
				}
				idx, _ := tc["index"].(float64)
				id, _ := tc["id"].(string)
				var name, args string
				if fn, ok := tc["function"].(map[string]any); ok {
					name, _ = fn["name"].(string)
					args, _ = fn["arguments"].(string)
				}
				agg.add(int(idx), id, name, args)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fullText, nil, err
	}
	return fullText, agg.result(), nil
}

// Anthropic streaming via SSE.
func (p *Provider) claudeChatStream(req CompletionRequest, callback func(delta string)) (string, []ToolCall, error) {
	systemMsg, anthropicMsgs := buildAnthropicMessages(req.Messages)
	payload := map[string]any{
		"model":      p.Model,
		"max_tokens": 4096,
		"messages":   anthropicMsgs,
		"stream":     true,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if systemMsg != "" {
		payload["system"] = systemMsg
	}
	if len(req.Tools) > 0 {
		payload["tools"] = anthropicTools(req.Tools)
	}

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequest("POST", p.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	var fullText string
	agg := newToolCallAggregator()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var event map[string]any
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		switch event["type"] {
		case "content_block_start":
			// tool_use block 起始：记录 id 与 name
			if cb, ok := event["content_block"].(map[string]any); ok && cb["type"] == "tool_use" {
				idx, _ := event["index"].(float64)
				id, _ := cb["id"].(string)
				name, _ := cb["name"].(string)
				agg.add(int(idx), id, name, "")
			}
		case "content_block_delta":
			delta, _ := event["delta"].(map[string]any)
			if text, ok := delta["text"].(string); ok && text != "" {
				fullText += text
				if callback != nil {
					callback(text)
				}
			}
			// input_json_delta：arguments 分片拼接
			if delta["type"] == "input_json_delta" {
				idx, _ := event["index"].(float64)
				partial, _ := delta["partial_json"].(string)
				agg.add(int(idx), "", "", partial)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fullText, nil, err
	}
	return fullText, agg.result(), nil
}

// Gemini streaming.
func (p *Provider) geminiChatStream(req CompletionRequest, callback func(delta string)) (string, []ToolCall, error) {
	contents := buildGeminiContents(req.Messages)
	payload := map[string]any{"contents": contents}
	if len(req.Tools) > 0 {
		payload["tools"] = geminiTools(req.Tools)
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?key=%s", p.BaseURL, p.Model, p.APIKey)

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	var fullText string
	// Gemini 流式 functionCall：按函数名聚合 args map，结束时序列化为 JSON
	fnArgs := map[string]map[string]any{}
	var fnOrder []string

	// Gemini streams JSON objects separated by newlines or in an array
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line == "[" || line == "," || line == "]" {
			continue
		}
		line = strings.TrimPrefix(line, ",")
		var chunk map[string]any
		if json.Unmarshal([]byte(line), &chunk) != nil {
			continue
		}
		if candidates, ok := chunk["candidates"].([]any); ok && len(candidates) > 0 {
			if cand, ok := candidates[0].(map[string]any); ok {
				if content, ok := cand["content"].(map[string]any); ok {
					if parts, ok := content["parts"].([]any); ok && len(parts) > 0 {
						for _, item := range parts {
							part, ok := item.(map[string]any)
							if !ok {
								continue
							}
							if text, ok := part["text"].(string); ok && text != "" {
								fullText += text
								if callback != nil {
									callback(text)
								}
							}
							if fc, ok := part["functionCall"].(map[string]any); ok {
								name, _ := fc["name"].(string)
								if _, seen := fnArgs[name]; !seen {
									fnArgs[name] = map[string]any{}
									fnOrder = append(fnOrder, name)
								}
								if args, ok := fc["args"].(map[string]any); ok {
									for k, v := range args {
										fnArgs[name][k] = v
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fullText, nil, err
	}
	var toolCalls []ToolCall
	for _, name := range fnOrder {
		args, _ := json.Marshal(fnArgs[name])
		toolCalls = append(toolCalls, ToolCall{Name: name, Arguments: string(args)})
	}
	return fullText, toolCalls, nil
}
