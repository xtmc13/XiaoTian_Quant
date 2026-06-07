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
var defaultProviders = []Provider{
	// International
	{Name: "openai", BaseURL: "https://api.openai.com", Model: "gpt-4o"},
	{Name: "claude", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-4-6"},
	{Name: "gemini", BaseURL: "https://generativelanguage.googleapis.com", Model: "gemini-2.5-pro"},
	// Chinese domestic
	{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat"},
	{Name: "qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus"},
	{Name: "hunyuan", BaseURL: "https://api.hunyuan.cloud.tencent.com/v1", Model: "hunyuan-pro"},
	{Name: "doubao", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", Model: "doubao-pro-32k"},
	{Name: "glm", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4-plus"},
	{Name: "kimi", BaseURL: "https://api.moonshot.cn/v1", Model: "moonshot-v1-32k"},
	// Open source
	{Name: "llama", BaseURL: "https://api.groq.com/openai/v1", Model: "llama-4-maverick"},
	{Name: "mistral", BaseURL: "https://api.mistral.ai/v1", Model: "mistral-large"},
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
)

type ChatMessage struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ── Completion Request/Response ──

type CompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
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

func (p *Provider) openAICompatibleChat(req CompletionRequest) (*CompletionResponse, error) {
	url := p.BaseURL + "/v1/chat/completions"
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

// Anthropic Messages API format.
func (p *Provider) claudeChat(req CompletionRequest) (*CompletionResponse, error) {
	// Build anthropic request
	var systemMsg string
	var anthropicMsgs []map[string]any
	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			systemMsg = msg.Content
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

	// Convert to standard format
	result := &CompletionResponse{
		ID:    fmt.Sprint(claudeResp["id"]),
		Model: p.Model,
	}
	if content, ok := claudeResp["content"].([]any); ok && len(content) > 0 {
		if block, ok := content[0].(map[string]any); ok {
			result.Choices = []Choice{{
				Index:   0,
				Message: ChatMessage{Role: RoleAssistant, Content: fmt.Sprint(block["text"])},
			}}
		}
	}
	return result, nil
}

// Google Gemini API format.
func (p *Provider) geminiChat(req CompletionRequest) (*CompletionResponse, error) {
	var contents []map[string]any
	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": msg.Content}},
		})
	}

	payload := map[string]any{
		"contents": contents,
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

	result := &CompletionResponse{Model: p.Model}
	if candidates, ok := geminiResp["candidates"].([]any); ok && len(candidates) > 0 {
		if cand, ok := candidates[0].(map[string]any); ok {
			if content, ok := cand["content"].(map[string]any); ok {
				if parts, ok := content["parts"].([]any); ok && len(parts) > 0 {
					if part, ok := parts[0].(map[string]any); ok {
						result.Choices = []Choice{{
							Index:   0,
							Message: ChatMessage{Role: RoleAssistant, Content: fmt.Sprint(part["text"])},
						}}
					}
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
	if p.Name == "claude" {
		return p.claudeChatStream(req, callback)
	}
	if p.Name == "gemini" {
		return p.geminiChatStream(req, callback)
	}
	return p.openAICompatibleStream(req, callback)
}

func (p *Provider) openAICompatibleStream(req CompletionRequest, callback func(delta string)) error {
	req.Stream = true
	req.Model = p.Model

	url := p.BaseURL + "/v1/chat/completions"
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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
		content, _ := delta["content"].(string)
		if content != "" {
			callback(content)
		}
	}
	return scanner.Err()
}

// Anthropic streaming via SSE.
func (p *Provider) claudeChatStream(req CompletionRequest, callback func(delta string)) error {
	var systemMsg string
	var anthropicMsgs []map[string]any
	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			systemMsg = msg.Content
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

	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequest("POST", p.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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
		if event["type"] == "content_block_delta" {
			delta, _ := event["delta"].(map[string]any)
			if text, ok := delta["text"].(string); ok && text != "" {
				callback(text)
			}
		}
	}
	return scanner.Err()
}

// Gemini streaming.
func (p *Provider) geminiChatStream(req CompletionRequest, callback func(delta string)) error {
	var contents []map[string]any
	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": msg.Content}},
		})
	}
	payload := map[string]any{"contents": contents}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?key=%s", p.BaseURL, p.Model, p.APIKey)

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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
						if part, ok := parts[0].(map[string]any); ok {
							if text, ok := part["text"].(string); ok && text != "" {
								callback(text)
							}
						}
					}
				}
			}
		}
	}
	return scanner.Err()
}
