package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ai"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 用户级 AI 配置端点测试基建 ──

func setupUserAIRouter(jwtUID int) *gin.Engine {
	r := setupRouter()
	r.Use(func(c *gin.Context) { c.Set(middleware.UserIDKey, jwtUID) })
	r.GET("/agent/user-ai-config", GetAgentUserAIConfig)
	r.PUT("/agent/user-ai-config", PutAgentUserAIConfig)
	r.DELETE("/agent/user-ai-config", DeleteAgentUserAIConfig)
	return r
}

func doUserAI(r *gin.Engine, method, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, "/agent/user-ai-config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, "/agent/user-ai-config", nil)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func cleanupUserAI(t *testing.T, userIDs ...int64) {
	t.Helper()
	repo := store.NewAgentUserAIRepo()
	for _, id := range userIDs {
		_ = repo.Delete(id)
	}
	t.Cleanup(func() {
		for _, id := range userIDs {
			_ = repo.Delete(id)
		}
	})
}

// GET/PUT/DELETE 契约：key 永不回传（仅 has_key）；PUT 覆盖；DELETE 清除；用户隔离。
func TestAgentUserAIConfig_CRUD(t *testing.T) {
	cleanupUserAI(t, 77, 78)
	r7 := setupUserAIRouter(77)

	// 空态
	w := doUserAI(r7, http.MethodGet, "")
	assertEq(t, w.Code, http.StatusOK, "get status")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true || body["has_key"] != false || body["provider"] != "" {
		t.Fatalf("empty get = %v", body)
	}

	// PUT 覆盖
	w = doUserAI(r7, http.MethodPut, `{"provider":"kimi","api_key":"sk-user-secret-key","model":"kimi-k2.5"}`)
	assertEq(t, w.Code, http.StatusOK, "put status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true {
		t.Fatalf("put body = %v", body)
	}

	// GET：has_key=true，provider/model 返回，api_key 绝不出现在响应里
	w = doUserAI(r7, http.MethodGet, "")
	assertEq(t, w.Code, http.StatusOK, "get status")
	raw := w.Body.String()
	if strings.Contains(raw, "sk-user-secret-key") || strings.Contains(raw, "api_key") {
		t.Fatalf("响应泄露 api_key: %s", raw)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["provider"] != "kimi" || body["model"] != "kimi-k2.5" || body["has_key"] != true {
		t.Fatalf("get body = %v", body)
	}

	// 用户隔离：另一个用户看不到
	r78 := setupUserAIRouter(78)
	w = doUserAI(r78, http.MethodGet, "")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["has_key"] != false || body["provider"] != "" {
		t.Fatalf("用户隔离失败: %v", body)
	}

	// DELETE 清除
	w = doUserAI(r7, http.MethodDelete, "")
	assertEq(t, w.Code, http.StatusOK, "delete status")
	w = doUserAI(r7, http.MethodGet, "")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["has_key"] != false || body["provider"] != "" {
		t.Fatalf("delete 后应为空态: %v", body)
	}

	// PUT 校验：provider 空 → 400
	w = doUserAI(r7, http.MethodPut, `{"api_key":"x"}`)
	assertEq(t, w.Code, http.StatusBadRequest, "空 provider 应 400")
}

// 解析链优先级：请求覆盖 > 用户级 > agent.ai.provider > 全局链。
func TestConfiguredAgentAIProvider_UserOverrideChain(t *testing.T) {
	cleanupUserAI(t, 88)
	llm := newMockAgentLLM(t, func(req map[string]any) (string, []ai.ToolCall) { return "ok", nil })
	registerMockAgentProvider(llm)

	cfg := store.GetConfig()
	prevAgent, hadAgent := cfg["agent"]
	t.Cleanup(func() { restoreCfgKey(t, cfg, "agent", prevAgent, hadAgent) })
	cfg["agent"] = map[string]any{"ai": map[string]any{"provider": "kimi"}}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// 无用户配置：走 agent.ai.provider
	_, name := configuredAgentAIProviderForUser(88, "")
	if name != "kimi" {
		t.Fatalf("无用户配置应走 agent.ai.provider，得到 %s", name)
	}

	// 用户配置插入到 agent.ai 之前：provider/key/model 全部生效
	repo := store.NewAgentUserAIRepo()
	if err := repo.Upsert(&store.AgentUserAIRecord{UserID: 88, Provider: "agent-chat-mock", Model: "user-model", APIKey: "sk-user-key"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	p, name := configuredAgentAIProviderForUser(88, "")
	if name != "agent-chat-mock" {
		t.Fatalf("用户配置应优先于 agent.ai，得到 %s", name)
	}
	if p == nil || p.APIKey != "sk-user-key" || p.Model != "user-model" {
		t.Fatalf("用户凭证未生效: %+v", p)
	}

	// 其他用户不受影响（仍走 agent.ai）
	_, name = configuredAgentAIProviderForUser(89, "")
	if name != "kimi" {
		t.Fatalf("用户隔离失败，得到 %s", name)
	}

	// 请求覆盖最高优先级（用户配置被忽略）
	p, name = configuredAgentAIProviderForUser(88, "agent-chat-mock:req-model")
	if name != "agent-chat-mock" || p.Model != "req-model" || p.APIKey == "sk-user-key" {
		t.Fatalf("请求覆盖应优先: name=%s provider=%+v", name, p)
	}

	// 用户 key 为空：沿用注册表/env 凭证，仅 model 覆盖
	if err := repo.Upsert(&store.AgentUserAIRecord{UserID: 88, Provider: "agent-chat-mock", Model: "only-model", APIKey: ""}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	p, _ = configuredAgentAIProviderForUser(88, "")
	if p.APIKey != "mock-key" || p.Model != "only-model" {
		t.Fatalf("空 key 应沿用注册表凭证: %+v", p)
	}
}
