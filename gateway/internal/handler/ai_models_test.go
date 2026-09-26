package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/ai"
)

// TestDefaultAIModelsCatalog 校验模型目录结构：key 唯一、与运行时注册表一致、
// default 必须落在 models 列表内、baseUrl 非空。
func TestDefaultAIModelsCatalog(t *testing.T) {
	raw, ok := defaultAIModels["providers"].([]map[string]any)
	if !ok || len(raw) == 0 {
		t.Fatal("defaultAIModels.providers 缺失或为空")
	}
	seen := map[string]bool{}
	for _, p := range raw {
		key, _ := p["key"].(string)
		if key == "" {
			t.Fatal("目录条目缺 key")
		}
		if seen[key] {
			t.Fatalf("目录 key 重复: %s", key)
		}
		seen[key] = true

		models, _ := p["models"].([]string)
		if len(models) == 0 {
			t.Fatalf("%s 的 models 为空", key)
		}
		def, _ := p["default"].(string)
		if def == "" {
			t.Fatalf("%s 缺 default 字段", key)
		}
		found := false
		for _, m := range models {
			if m == def {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s 的 default(%s) 不在 models 列表中", key, def)
		}
		if base, _ := p["baseUrl"].(string); base == "" {
			t.Fatalf("%s 缺 baseUrl", key)
		}
		// 已注册进运行时表的 provider，目录 key 必须与注册表同名。
		if reg := ai.GetProvider(key); reg != nil {
			if reg.Name != key {
				t.Fatalf("目录 key %s 与注册表名 %s 不一致", key, reg.Name)
			}
		}
	}
	// 主流厂商必须齐全（openrouter 为聚合网关可选）。
	for _, want := range []string{"openai", "claude", "gemini", "deepseek", "qwen", "glm", "kimi"} {
		if !seen[want] {
			t.Fatalf("目录缺少主流 provider: %s", want)
		}
	}
}

// TestNormalizeProviderName legacy 别名归一。
func TestNormalizeProviderName(t *testing.T) {
	if got := ai.NormalizeProviderName("anthropic"); got != "claude" {
		t.Fatalf("anthropic 应归一到 claude，得到 %s", got)
	}
	if got := ai.NormalizeProviderName("claude"); got != "claude" {
		t.Fatalf("claude 应保持不变，得到 %s", got)
	}
	if got := ai.NormalizeProviderName("deepseek"); got != "deepseek" {
		t.Fatalf("deepseek 应保持不变，得到 %s", got)
	}
}

// TestRunAITestSuccess 用 OpenAI 兼容 mock server 验证连接测试成功路径。
func TestRunAITestSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "OK"}}},
		})
	}))
	defer srv.Close()

	// openrouter 未注册进运行时表，走目录 baseUrl/default 回落，正好测回落链。
	res := runAITest("openrouter", aiTestRequest{APIKey: "sk-test", BaseURL: srv.URL, Model: "openai/gpt-5.5"})
	if res["success"] != true {
		t.Fatalf("期望成功，得到 %v", res)
	}
	if _, ok := res["latency_ms"].(int64); !ok {
		t.Fatalf("成功响应应带 latency_ms: %v", res)
	}
}

// TestRunAITestFailure mock server 返回 500 → 失败路径带错误信息。
func TestRunAITestFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := runAITest("deepseek", aiTestRequest{APIKey: "sk-bad", BaseURL: srv.URL, Model: "deepseek-chat"})
	if res["success"] != false {
		t.Fatalf("期望失败，得到 %v", res)
	}
	msg, _ := res["message"].(string)
	if msg == "" {
		t.Fatalf("失败响应应带 message: %v", res)
	}
}

// TestRunAITestNoKey 缺 API key → 明确提示，不发起请求。
func TestRunAITestNoKey(t *testing.T) {
	res := runAITest("nonexistent-provider", aiTestRequest{})
	if res["success"] != false {
		t.Fatalf("期望失败，得到 %v", res)
	}
	msg, _ := res["message"].(string)
	if !strings.Contains(msg, "API Key") {
		t.Fatalf("缺 key 提示应包含 API Key: %v", res)
	}
}
