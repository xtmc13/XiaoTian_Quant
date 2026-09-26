package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListModelsOpenAICompatible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-x" {
			t.Errorf("missing bearer")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": "kimi-k2.5"},
			{"id": "moonshot-v1-8k"},
			{"id": "text-embedding-3"},   // 应被过滤
			{"id": "whisper-large-v3"},   // 应被过滤
			{"id": "kimi-k2-0905-preview"},
		}})
	}))
	defer srv.Close()

	models, err := ListModels("kimi", srv.URL, "sk-x")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []string{"kimi-k2-0905-preview", "kimi-k2.5", "moonshot-v1-8k"}
	if len(models) != len(want) {
		t.Fatalf("models = %v", models)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Fatalf("models = %v, want %v", models, want)
		}
	}
}

func TestListModelsVersionedBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compatible-mode/v1/models" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "qwen3-max"}}})
	}))
	defer srv.Close()

	models, err := ListModels("qwen", srv.URL+"/compatible-mode/v1", "sk-x")
	if err != nil || len(models) != 1 || models[0] != "qwen3-max" {
		t.Fatalf("models = %v, err = %v", models, err)
	}
}

func TestListModelsAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-ant" {
			t.Errorf("missing x-api-key")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": "claude-haiku-4-5"},
			{"id": "claude-3-5-sonnet-20241022", "deprecated": true}, // 应被过滤
			{"id": "claude-opus-4-7"},
		}})
	}))
	defer srv.Close()

	models, err := ListModels("claude", srv.URL, "sk-ant")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// 新的在前
	if len(models) != 2 || models[0] != "claude-opus-4-7" || models[1] != "claude-haiku-4-5" {
		t.Fatalf("models = %v", models)
	}
}

func TestListModelsGemini(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{
			{"name": "models/gemini-2.5-pro"},
			{"name": "models/gemini-embedding-001"}, // 应被过滤
			{"name": "models/gemini-2.5-flash"},
		}})
	}))
	defer srv.Close()

	models, err := ListModels("gemini", srv.URL, "sk-g")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(models) != 2 || models[0] != "gemini-2.5-flash" || models[1] != "gemini-2.5-pro" {
		t.Fatalf("models = %v", models)
	}
}

func TestListModelsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := ListModels("deepseek", srv.URL, "bad-key"); err == nil {
		t.Fatal("期望返回错误")
	}
}

func TestListModelsNoKey(t *testing.T) {
	if _, err := ListModels("deepseek", "http://x", ""); err == nil {
		t.Fatal("空 key 应报错")
	}
}
