package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/alerting"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── fake ingest service ─────────────────────────────────────────

type fakeIngestService struct {
	mu       sync.Mutex
	payloads []*alerting.WebhookPayload
	active   []*store.AlertEventRecord
	history  []*store.AlertEventRecord
}

func (f *fakeIngestService) Ingest(p *alerting.WebhookPayload) alerting.IngestResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payloads = append(f.payloads, p)
	res := alerting.IngestResult{Received: len(p.Alerts)}
	for _, a := range p.Alerts {
		if a.Status == "firing" {
			res.NotifiedFiring++
		}
	}
	return res
}

func (f *fakeIngestService) Active(limit int) ([]*store.AlertEventRecord, error) {
	return f.active, nil
}

func (f *fakeIngestService) History(limit int) ([]*store.AlertEventRecord, error) {
	return f.history, nil
}

func (f *fakeIngestService) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.payloads)
}

// withAlertSvc 注入 fake 服务并在测试结束后还原全局单例。
func withAlertSvc(t *testing.T, svc AlertIngestService) {
	t.Helper()
	old := AlertIngestSvc
	AlertIngestSvc = svc
	t.Cleanup(func() { AlertIngestSvc = old })
}

const testAlertSecret = "test-alert-secret"

func alertWebhookReq(t *testing.T, body, secret string) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouter()
	r.POST("/api/alerts/webhook", AlertsWebhook)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/alerts/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-Alert-Webhook-Secret", secret)
	}
	r.ServeHTTP(w, req)
	return w
}

const sampleV4Payload = `{
  "version": "4",
  "groupKey": "{}:{alertname=\"GatewayDown\"}",
  "status": "firing",
  "receiver": "gateway-webhook",
  "groupLabels": {"alertname": "GatewayDown"},
  "commonLabels": {"alertname": "GatewayDown", "severity": "critical"},
  "commonAnnotations": {"summary": "XiaoTian 网关不可达"},
  "externalURL": "http://alertmanager:9093",
  "alerts": [
    {
      "status": "firing",
      "labels": {"alertname": "GatewayDown", "severity": "critical", "instance": "gateway:8080"},
      "annotations": {"summary": "XiaoTian 网关不可达", "description": "抓取失败 1 分钟"},
      "startsAt": "2026-09-25T08:00:00.000Z",
      "endsAt": "0001-01-01T00:00:00Z",
      "generatorURL": "http://prometheus:9090/graph",
      "fingerprint": "abc123def456"
    }
  ]
}`

// ── tests ───────────────────────────────────────────────────────

func TestAlertsWebhookWrongSecret401(t *testing.T) {
	t.Setenv(AlertWebhookSecretEnv, testAlertSecret)
	svc := &fakeIngestService{}
	withAlertSvc(t, svc)

	w := alertWebhookReq(t, sampleV4Payload, "wrong-secret")
	assertEq(t, w.Code, http.StatusUnauthorized, "错误密钥必须 401")

	w = alertWebhookReq(t, sampleV4Payload, "")
	assertEq(t, w.Code, http.StatusUnauthorized, "缺失密钥头必须 401")

	if svc.callCount() != 0 {
		t.Fatal("未授权请求不得触达 ingest 服务")
	}
}

func TestAlertsWebhookSecretUnset503(t *testing.T) {
	t.Setenv(AlertWebhookSecretEnv, "")
	svc := &fakeIngestService{}
	withAlertSvc(t, svc)

	w := alertWebhookReq(t, sampleV4Payload, "anything")
	assertEq(t, w.Code, http.StatusServiceUnavailable, "未配置密钥必须 fail-closed 503")
	if svc.callCount() != 0 {
		t.Fatal("未配置密钥时不得触达 ingest 服务")
	}
}

func TestAlertsWebhookParsesV4Payload(t *testing.T) {
	t.Setenv(AlertWebhookSecretEnv, testAlertSecret)
	svc := &fakeIngestService{}
	withAlertSvc(t, svc)

	w := alertWebhookReq(t, sampleV4Payload, testAlertSecret)
	assertEq(t, w.Code, http.StatusOK, "合法 v4 载荷必须 200")

	var res alerting.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("响应应为 IngestResult JSON: %v", err)
	}
	if res.Received != 1 || res.NotifiedFiring != 1 {
		t.Fatalf("ingest 统计错误: %+v", res)
	}

	if svc.callCount() != 1 {
		t.Fatalf("ingest 服务应被调用 1 次, got %d", svc.callCount())
	}
	got := svc.payloads[0]
	if got.Version != "4" || got.Receiver != "gateway-webhook" || len(got.Alerts) != 1 {
		t.Fatalf("v4 载荷解析错误: %+v", got)
	}
	a := got.Alerts[0]
	if a.Fingerprint != "abc123def456" || a.Labels["alertname"] != "GatewayDown" || a.Status != "firing" {
		t.Fatalf("alert 字段解析错误: %+v", a)
	}
	if a.StartsAt.IsZero() {
		t.Fatal("startsAt 应解析为时间")
	}
}

func TestAlertsWebhookBadJSON400(t *testing.T) {
	t.Setenv(AlertWebhookSecretEnv, testAlertSecret)
	withAlertSvc(t, &fakeIngestService{})

	w := alertWebhookReq(t, `{not-json`, testAlertSecret)
	assertEq(t, w.Code, http.StatusBadRequest, "非法 JSON 必须 400")
}

func TestAlertsWebhookDedupEndToEnd(t *testing.T) {
	// 端到端（真实 alerting.Service + 内存 store）：同 fingerprint firing 期间
	// 重发只推一次，resolved 推一次。
	t.Setenv(AlertWebhookSecretEnv, testAlertSecret)
	svc := alerting.NewService(store.NewAlertEventRepo(), nil)
	withAlertSvc(t, svc)

	w := alertWebhookReq(t, sampleV4Payload, testAlertSecret)
	assertEq(t, w.Code, http.StatusOK, "首次 firing")
	var res alerting.IngestResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.NotifiedFiring != 1 {
		t.Fatalf("首次 firing 应推送: %+v", res)
	}

	w = alertWebhookReq(t, sampleV4Payload, testAlertSecret)
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.NotifiedFiring != 0 || res.Suppressed != 1 {
		t.Fatalf("重复 firing 应抑制: %+v", res)
	}

	resolved := strings.Replace(sampleV4Payload, `"status": "firing"`, `"status": "resolved"`, -1)
	w = alertWebhookReq(t, resolved, testAlertSecret)
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.NotifiedResolved != 1 {
		t.Fatalf("resolved 应推送一次: %+v", res)
	}

	// active 列表应为空（已 resolved），history 应有 1 条
	recs, err := store.NewAlertEventRepo().ListActive(10)
	assertTrue(t, err == nil && len(recs) == 0, "resolved 后 active 应为空")
	rec, err := store.NewAlertEventRepo().GetByFingerprint("abc123def456")
	assertTrue(t, err == nil && rec != nil && rec.NotifiedFiring && rec.NotifiedResolved, "事件应落库且双标记")
}

func TestAlertsActiveAndHistoryHandlers(t *testing.T) {
	svc := &fakeIngestService{
		active: []*store.AlertEventRecord{{
			Fingerprint: "fp1", AlertName: "GatewayDown", Status: "firing",
			Severity: "critical", Summary: "网关不可达", LabelsJSON: `{"severity":"critical"}`,
			LastSeen: 1000,
		}},
		history: []*store.AlertEventRecord{{
			Fingerprint: "fp2", AlertName: "GatewayHighMemory", Status: "resolved",
			Severity: "warning", LabelsJSON: `{"severity":"warning"}`, LastSeen: 900,
		}},
	}
	withAlertSvc(t, svc)

	r := setupRouter()
	r.GET("/api/alerts/active", AlertsActive)
	r.GET("/api/alerts/history", AlertsHistory)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/alerts/active", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "active 200")
	var body struct {
		Alerts []alertEventDTO `json:"alerts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Alerts) != 1 {
		t.Fatalf("active 响应解析失败: %v body=%s", err, w.Body.String())
	}
	if body.Alerts[0].Labels["severity"] != "critical" {
		t.Fatalf("labels 应由 JSON 列解出: %+v", body.Alerts[0].Labels)
	}

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/alerts/history?limit=10", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "history 200")
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Alerts) != 1 {
		t.Fatalf("history 响应解析失败: %v", err)
	}
}
