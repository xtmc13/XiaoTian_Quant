package ai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ═══════════════════════════════════════════════════════════════════
// Provider Registry Tests
// ═══════════════════════════════════════════════════════════════════

func TestRegisterProvider(t *testing.T) {
	// Clear and register a custom provider
	p := Provider{
		Name:     "test-provider",
		BaseURL:  "https://test.example.com",
		APIKey:   "test-key-123",
		Model:    "test-model",
		TimeoutS: 30,
	}
	RegisterProvider(p)

	got := GetProvider("test-provider")
	if got == nil {
		t.Fatal("expected provider to be registered")
	}
	if got.Name != "test-provider" {
		t.Errorf("name = %q, want %q", got.Name, "test-provider")
	}
	if got.APIKey != "test-key-123" {
		t.Errorf("apiKey = %q, want %q", got.APIKey, "test-key-123")
	}
	if got.Model != "test-model" {
		t.Errorf("model = %q, want %q", got.Model, "test-model")
	}
	if got.client == nil {
		t.Error("expected client to be initialized")
	}
}

func TestGetProvider_NotFound(t *testing.T) {
	got := GetProvider("nonexistent-provider")
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestListProviders(t *testing.T) {
	// Register two test providers
	RegisterProvider(Provider{Name: "alpha", BaseURL: "https://a.com", APIKey: "k1", Model: "m1"})
	RegisterProvider(Provider{Name: "beta", BaseURL: "https://b.com", APIKey: "k2", Model: "m2"})

	names := ListProviders()
	found := make(map[string]bool)
	for _, n := range names {
		found[n] = true
	}

	if !found["alpha"] || !found["beta"] {
		t.Errorf("expected alpha and beta in %v", names)
	}
}

// ═══════════════════════════════════════════════════════════════════
// ChatCompletion — OpenAI-compatible
// ═══════════════════════════════════════════════════════════════════

func TestProvider_ChatCompletion_OpenAICompatible(t *testing.T) {
	// Mock server returning OpenAI-compatible response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("authorization = %q, want Bearer prefix", auth)
		}

		resp := CompletionResponse{
			ID:    "chatcmpl-test",
			Model: "gpt-4o",
			Choices: []Choice{{
				Index:   0,
				Message: ChatMessage{Role: RoleAssistant, Content: "Hello from mock"},
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &Provider{
		Name:     "openai-mock",
		BaseURL:  server.URL,
		APIKey:   "sk-mock",
		Model:    "gpt-4o",
		TimeoutS: 30,
		client:   &http.Client{Timeout: 5 * time.Second},
	}

	req := CompletionRequest{
		Messages:    []ChatMessage{{Role: RoleUser, Content: "Hi"}},
		MaxTokens:   100,
		Temperature: 0.7,
	}

	result, err := p.ChatCompletion(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "chatcmpl-test" {
		t.Errorf("id = %q, want chatcmpl-test", result.ID)
	}
	if len(result.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(result.Choices))
	}
	if result.Choices[0].Message.Content != "Hello from mock" {
		t.Errorf("content = %q, want Hello from mock", result.Choices[0].Message.Content)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens = %d, want 15", result.Usage.TotalTokens)
	}
}

func TestProvider_ChatCompletion_APIKeyMissing(t *testing.T) {
	p := &Provider{
		Name:     "deepseek",
		BaseURL:  "https://api.deepseek.com",
		APIKey:   "",
		Model:    "deepseek-chat",
		TimeoutS: 30,
		client:   &http.Client{Timeout: 5 * time.Second},
	}

	req := CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "test"}},
	}

	_, err := p.ChatCompletion(req)
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !strings.Contains(err.Error(), "API key not configured") {
		t.Errorf("error = %q, want API key not configured", err.Error())
	}
}

func TestProvider_ChatCompletion_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error": "rate limited"}`)
	}))
	defer server.Close()

	p := &Provider{
		Name:     "err-provider",
		BaseURL:  server.URL,
		APIKey:   "sk-test",
		Model:    "m",
		TimeoutS: 30,
		client:   &http.Client{Timeout: 5 * time.Second},
	}

	_, err := p.ChatCompletion(CompletionRequest{Messages: []ChatMessage{{Role: RoleUser, Content: "x"}}})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q, want status 429", err.Error())
	}
}

// ═══════════════════════════════════════════════════════════════════
// ChatCompletion — Claude (Anthropic) adapter
// ═══════════════════════════════════════════════════════════════════

func TestProvider_ChatCompletion_Claude(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key header")
		}

		resp := map[string]any{
			"id":    "msg_01",
			"model": "claude-sonnet",
			"content": []map[string]any{{"type": "text", "text": "Claude says hello"}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &Provider{
		Name:     "claude",
		BaseURL:  server.URL,
		APIKey:   "sk-ant-test",
		Model:    "claude-sonnet-4-6",
		TimeoutS: 30,
		client:   &http.Client{Timeout: 5 * time.Second},
	}

	req := CompletionRequest{
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "You are a trader"},
			{Role: RoleUser, Content: "Analyze BTC"},
		},
	}

	result, err := p.ChatCompletion(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(result.Choices))
	}
	if result.Choices[0].Message.Content != "Claude says hello" {
		t.Errorf("content = %q, want Claude says hello", result.Choices[0].Message.Content)
	}
}

// ═══════════════════════════════════════════════════════════════════
// ChatCompletion — Gemini adapter
// ═══════════════════════════════════════════════════════════════════

func TestProvider_ChatCompletion_Gemini(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("path = %q, want :generateContent", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("key") == "" {
			t.Error("missing key query param")
		}

		resp := map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{
					"parts": []map[string]any{{"text": "Gemini analysis complete"}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &Provider{
		Name:     "gemini",
		BaseURL:  server.URL,
		APIKey:   "gemini-api-key",
		Model:    "gemini-2.5-pro",
		TimeoutS: 30,
		client:   &http.Client{Timeout: 5 * time.Second},
	}

	result, err := p.ChatCompletion(CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "Analyze ETH"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(result.Choices))
	}
	if result.Choices[0].Message.Content != "Gemini analysis complete" {
		t.Errorf("content = %q, want Gemini analysis complete", result.Choices[0].Message.Content)
	}
}

// ═══════════════════════════════════════════════════════════════════
// Streaming Tests
// ═══════════════════════════════════════════════════════════════════

func TestProvider_SupportsStream(t *testing.T) {
	tests := []struct {
		name   string
		p      *Provider
		want   bool
	}{
		{"openai with key", &Provider{Name: "openai", APIKey: "sk"}, true},
		{"deepseek with key", &Provider{Name: "deepseek", APIKey: "sk"}, true},
		{"claude with key", &Provider{Name: "claude", APIKey: "sk"}, false},
		{"gemini with key", &Provider{Name: "gemini", APIKey: "sk"}, false},
		{"nil provider", nil, false},
		{"empty key", &Provider{Name: "openai", APIKey: ""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.p.SupportsStream()
			if got != tt.want {
				t.Errorf("SupportsStream() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProvider_ChatCompletionStream_OpenAICompatible(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher support")
		}

		chunks := []string{
			`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
			`data: {"choices":[{"delta":{"content":" world"}}]}`,
			"data: [DONE]",
		}
		for _, chunk := range chunks {
			fmt.Fprintln(w, chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := &Provider{
		Name:     "stream-test",
		BaseURL:  server.URL,
		APIKey:   "sk-stream",
		Model:    "gpt-4o",
		TimeoutS: 30,
		client:   &http.Client{Timeout: 5 * time.Second},
	}

	var deltas []string
	err := p.ChatCompletionStream(CompletionRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "stream me"}},
	}, func(delta string) {
		deltas = append(deltas, delta)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.Join(deltas, "")
	if got != "Hello world" {
		t.Errorf("streamed = %q, want Hello world", got)
	}
}

// ═══════════════════════════════════════════════════════════════════
// Request/Response Serialization
// ═══════════════════════════════════════════════════════════════════

func TestCompletionRequest_JSON(t *testing.T) {
	req := CompletionRequest{
		Model:       "gpt-4o",
		Messages:    []ChatMessage{{Role: RoleSystem, Content: "sys"}, {Role: RoleUser, Content: "user"}},
		MaxTokens:   256,
		Temperature: 0.5,
		Stream:      false,
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["model"] != "gpt-4o" {
		t.Errorf("model = %v", raw["model"])
	}
	msgs, _ := raw["messages"].([]any)
	if len(msgs) != 2 {
		t.Errorf("messages len = %d, want 2", len(msgs))
	}
}

func TestCompletionResponse_JSON(t *testing.T) {
	resp := CompletionResponse{
		ID:    "id-1",
		Model: "m",
		Choices: []Choice{
			{Index: 0, Message: ChatMessage{Role: RoleAssistant, Content: "c"}},
		},
		Usage: Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var back CompletionResponse
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Usage.TotalTokens != 3 {
		t.Errorf("total_tokens = %d, want 3", back.Usage.TotalTokens)
	}
}

// ═══════════════════════════════════════════════════════════════════
// Role Constants
// ═══════════════════════════════════════════════════════════════════

func TestRole_String(t *testing.T) {
	if RoleSystem != "system" {
		t.Errorf("RoleSystem = %q, want system", RoleSystem)
	}
	if RoleUser != "user" {
		t.Errorf("RoleUser = %q, want user", RoleUser)
	}
	if RoleAssistant != "assistant" {
		t.Errorf("RoleAssistant = %q, want assistant", RoleAssistant)
	}
}
