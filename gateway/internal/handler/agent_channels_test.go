package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/agentdingtalk"
	"github.com/xiaotian-quant/gateway/internal/agentfeishu"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

// ── 飞书 REST 契约（前端冻结：success/configured/linked/open_id） ──

func feishuTestRouter(uid int) *gin.Engine {
	r := setupRouter()
	h := func(c *gin.Context) {
		if uid > 0 {
			c.Set(middleware.UserIDKey, uid)
		}
	}
	r.GET("/api/agent/feishu/status", h, AgentFeishuStatus)
	r.POST("/api/agent/feishu/pair-code", h, AgentFeishuPairCode)
	r.POST("/api/agent/feishu/unlink", h, AgentFeishuUnlink)
	return r
}

func doReq(t *testing.T, r http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAgentFeishuStatusShape(t *testing.T) {
	t.Setenv("FEISHU_APP_ID", "cli_test")
	t.Setenv("FEISHU_APP_SECRET", "secret_test")
	r := feishuTestRouter(601)

	// 未绑定：success/configured/linked/open_id 全字段，open_id 空串
	w := doReq(t, r, http.MethodGet, "/api/agent/feishu/status")
	assertEq(t, w.Code, http.StatusOK, "status 未绑定")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["success"] != true || body["configured"] != true || body["linked"] != false {
		t.Errorf("status body = %v", body)
	}
	if v, ok := body["open_id"]; !ok || v != "" {
		t.Errorf("未绑定 open_id 应为空串: %v", body)
	}

	// 绑定后：linked=true + open_id
	repo := agentfeishu.NewRepo()
	code, err := repo.CreatePairCode(601)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ConsumePairCode(code, "ou_test_601", "oc_601"); err != nil {
		t.Fatal(err)
	}
	w = doReq(t, r, http.MethodGet, "/api/agent/feishu/status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["linked"] != true || body["open_id"] != "ou_test_601" {
		t.Errorf("绑定后 status body = %v", body)
	}

	// 解绑：{"success":true}，随后 linked=false
	w = doReq(t, r, http.MethodPost, "/api/agent/feishu/unlink")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true {
		t.Errorf("unlink body = %v", body)
	}
	w = doReq(t, r, http.MethodGet, "/api/agent/feishu/status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["linked"] != false || body["open_id"] != "" {
		t.Errorf("解绑后 status body = %v", body)
	}
}

func TestAgentFeishuPairCodeShape(t *testing.T) {
	t.Setenv("FEISHU_APP_ID", "cli_test")
	t.Setenv("FEISHU_APP_SECRET", "secret_test")
	r := feishuTestRouter(602)

	w := doReq(t, r, http.MethodPost, "/api/agent/feishu/pair-code")
	assertEq(t, w.Code, http.StatusOK, "pair-code")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	code, _ := body["code"].(string)
	if body["success"] != true || len(code) != 6 {
		t.Errorf("pair-code body = %v", body)
	}
	if exp, _ := body["expires_in"].(float64); exp != 600 {
		t.Errorf("expires_in = %v, want 600", body["expires_in"])
	}
}

func TestAgentFeishuPairCodeNotConfigured(t *testing.T) {
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")
	r := feishuTestRouter(603)
	w := doReq(t, r, http.MethodPost, "/api/agent/feishu/pair-code")
	assertEq(t, w.Code, http.StatusServiceUnavailable, "未配置 pair-code 应 503")
}

func TestAgentFeishuWebhookChallenge(t *testing.T) {
	t.Setenv("FEISHU_APP_ID", "cli_test")
	t.Setenv("FEISHU_APP_SECRET", "secret_test")
	r := setupRouter()
	r.POST("/api/agent/feishu/webhook", AgentFeishuWebhook)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/feishu/webhook",
		strings.NewReader(`{"type":"url_verification","challenge":"ch_123"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "challenge")
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["challenge"] != "ch_123" {
		t.Errorf("challenge 响应: %s", w.Body.String())
	}
}

func TestAgentFeishuWebhookUnconfigured(t *testing.T) {
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")
	r := setupRouter()
	r.POST("/api/agent/feishu/webhook", AgentFeishuWebhook)
	w := doReq(t, r, http.MethodPost, "/api/agent/feishu/webhook")
	assertEq(t, w.Code, http.StatusServiceUnavailable, "未配置 webhook 应 503")
}

// ── 钉钉 REST 契约（success/configured/linked/staff_id） ──

func dingtalkTestRouter(uid int) *gin.Engine {
	r := setupRouter()
	h := func(c *gin.Context) {
		if uid > 0 {
			c.Set(middleware.UserIDKey, uid)
		}
	}
	r.GET("/api/agent/dingtalk/status", h, AgentDingtalkStatus)
	r.POST("/api/agent/dingtalk/pair-code", h, AgentDingtalkPairCode)
	r.POST("/api/agent/dingtalk/unlink", h, AgentDingtalkUnlink)
	return r
}

func TestAgentDingtalkStatusShape(t *testing.T) {
	t.Setenv("DINGTALK_CLIENT_ID", "ding_test")
	t.Setenv("DINGTALK_CLIENT_SECRET", "ding_secret")
	r := dingtalkTestRouter(611)

	w := doReq(t, r, http.MethodGet, "/api/agent/dingtalk/status")
	assertEq(t, w.Code, http.StatusOK, "status 未绑定")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["success"] != true || body["configured"] != true || body["linked"] != false {
		t.Errorf("status body = %v", body)
	}
	if v, ok := body["staff_id"]; !ok || v != "" {
		t.Errorf("未绑定 staff_id 应为空串: %v", body)
	}

	repo := agentdingtalk.NewRepo()
	code, err := repo.CreatePairCode(611)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ConsumePairCode(code, "staff_611"); err != nil {
		t.Fatal(err)
	}
	w = doReq(t, r, http.MethodGet, "/api/agent/dingtalk/status")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["linked"] != true || body["staff_id"] != "staff_611" {
		t.Errorf("绑定后 status body = %v", body)
	}

	w = doReq(t, r, http.MethodPost, "/api/agent/dingtalk/unlink")
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["success"] != true {
		t.Errorf("unlink body = %v", body)
	}
}

func TestAgentDingtalkPairCodeShape(t *testing.T) {
	t.Setenv("DINGTALK_CLIENT_ID", "ding_test")
	t.Setenv("DINGTALK_CLIENT_SECRET", "ding_secret")
	r := dingtalkTestRouter(612)

	w := doReq(t, r, http.MethodPost, "/api/agent/dingtalk/pair-code")
	assertEq(t, w.Code, http.StatusOK, "pair-code")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	code, _ := body["code"].(string)
	if body["success"] != true || len(code) != 6 {
		t.Errorf("pair-code body = %v", body)
	}
	if exp, _ := body["expires_in"].(float64); exp != 600 {
		t.Errorf("expires_in = %v, want 600", body["expires_in"])
	}
}

func TestAgentDingtalkPairCodeNotConfigured(t *testing.T) {
	t.Setenv("DINGTALK_CLIENT_ID", "")
	t.Setenv("DINGTALK_CLIENT_SECRET", "")
	r := dingtalkTestRouter(613)
	w := doReq(t, r, http.MethodPost, "/api/agent/dingtalk/pair-code")
	assertEq(t, w.Code, http.StatusServiceUnavailable, "未配置 pair-code 应 503")
}

func TestAgentDingtalkWebhookUnconfigured(t *testing.T) {
	t.Setenv("DINGTALK_CLIENT_ID", "")
	t.Setenv("DINGTALK_CLIENT_SECRET", "")
	r := setupRouter()
	r.POST("/api/agent/dingtalk/webhook", AgentDingtalkWebhook)
	w := doReq(t, r, http.MethodPost, "/api/agent/dingtalk/webhook")
	assertEq(t, w.Code, http.StatusServiceUnavailable, "未配置 webhook 应 503")
}
