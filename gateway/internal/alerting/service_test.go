package alerting

import (
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── fakes ───────────────────────────────────────────────────────

type fakeEventStore struct {
	mu      sync.Mutex
	byFp    map[string]*store.AlertEventRecord
	upserts int
}

func newFakeEventStore() *fakeEventStore {
	return &fakeEventStore{byFp: make(map[string]*store.AlertEventRecord)}
}

func (f *fakeEventStore) GetByFingerprint(fp string) (*store.AlertEventRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.byFp[fp]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeEventStore) Upsert(rec *store.AlertEventRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *rec
	if old, ok := f.byFp[rec.Fingerprint]; ok {
		cp.ID = old.ID
	} else {
		cp.ID = int64(len(f.byFp) + 1)
	}
	f.byFp[rec.Fingerprint] = &cp
	f.upserts++
	return nil
}

func (f *fakeEventStore) ListActive(limit int) ([]*store.AlertEventRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*store.AlertEventRecord
	for _, r := range f.byFp {
		if r.NotifiedFiring && !r.NotifiedResolved {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeEventStore) ListRecent(limit int) ([]*store.AlertEventRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*store.AlertEventRecord, 0, len(f.byFp))
	for _, r := range f.byFp {
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

type mockBroadcaster struct {
	mu       sync.Mutex
	template []notify.Template
}

func (m *mockBroadcaster) Broadcast(tpl notify.Template) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.template = append(m.template, tpl)
}

func (m *mockBroadcaster) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.template)
}

func (m *mockBroadcaster) last() notify.Template {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.template[len(m.template)-1]
}

// ── helpers ─────────────────────────────────────────────────────

func firingAlert(fp, name, severity string) Alert {
	return Alert{
		Status:      "firing",
		Labels:      map[string]string{"alertname": name, "severity": severity, "instance": "gateway:8080"},
		Annotations: map[string]string{"summary": name + " 摘要", "description": name + " 描述", "runbook": "docs/OBSERVABILITY.md"},
		StartsAt:    time.Now(),
		Fingerprint: fp,
	}
}

func resolvedAlert(fp, name, severity string) Alert {
	a := firingAlert(fp, name, severity)
	a.Status = "resolved"
	a.EndsAt = time.Now()
	return a
}

func newTestService() (*Service, *fakeEventStore, *mockBroadcaster) {
	st := newFakeEventStore()
	bc := &mockBroadcaster{}
	return NewService(st, bc), st, bc
}

// ── tests ───────────────────────────────────────────────────────

func TestIngestFiringNotifiesOnce(t *testing.T) {
	svc, st, bc := newTestService()

	res := svc.Ingest(&WebhookPayload{Version: "4", Status: "firing", Alerts: []Alert{firingAlert("fp1", "GatewayDown", "critical")}})
	if res.Received != 1 || res.NotifiedFiring != 1 || res.Suppressed != 0 {
		t.Fatalf("首次 firing 统计错误: %+v", res)
	}
	if bc.count() != 1 {
		t.Fatalf("首次 firing 应推送 1 次, got %d", bc.count())
	}

	// 同 fingerprint firing 期间重复到达（Alertmanager group_interval 重发）→ 抑制
	res = svc.Ingest(&WebhookPayload{Version: "4", Status: "firing", Alerts: []Alert{firingAlert("fp1", "GatewayDown", "critical")}})
	if res.NotifiedFiring != 0 || res.Suppressed != 1 {
		t.Fatalf("重复 firing 应被抑制: %+v", res)
	}
	if bc.count() != 1 {
		t.Fatalf("firing 期间不得重复推送, got %d 次", bc.count())
	}

	rec, _ := st.GetByFingerprint("fp1")
	if rec == nil || !rec.NotifiedFiring || rec.NotifiedResolved {
		t.Fatalf("落库去重标记错误: %+v", rec)
	}
	if rec.AlertName != "GatewayDown" || rec.Severity != "critical" || rec.Status != "firing" {
		t.Fatalf("落库字段错误: %+v", rec)
	}
}

func TestIngestResolvedNotifiesOnce(t *testing.T) {
	svc, _, bc := newTestService()
	svc.Ingest(&WebhookPayload{Alerts: []Alert{firingAlert("fp1", "GatewayDown", "critical")}})

	res := svc.Ingest(&WebhookPayload{Alerts: []Alert{resolvedAlert("fp1", "GatewayDown", "critical")}})
	if res.NotifiedResolved != 1 {
		t.Fatalf("resolved 应推送一次: %+v", res)
	}
	if bc.count() != 2 {
		t.Fatalf("firing+resolved 共应推送 2 次, got %d", bc.count())
	}
	if tpl := bc.last(); tpl.Tags["status"] != "resolved" {
		t.Fatalf("resolved 模板 status 错误: %+v", tpl.Tags)
	}

	// 重复 resolved → 抑制
	res = svc.Ingest(&WebhookPayload{Alerts: []Alert{resolvedAlert("fp1", "GatewayDown", "critical")}})
	if res.NotifiedResolved != 0 || res.Suppressed != 1 {
		t.Fatalf("重复 resolved 应被抑制: %+v", res)
	}
	if bc.count() != 2 {
		t.Fatalf("resolved 不得重复推送, got %d 次", bc.count())
	}
}

func TestIngestResolvedWithoutFiringIsSilent(t *testing.T) {
	svc, st, bc := newTestService()
	res := svc.Ingest(&WebhookPayload{Alerts: []Alert{resolvedAlert("fpX", "GatewayDown", "critical")}})
	if res.NotifiedResolved != 0 || res.Suppressed != 1 {
		t.Fatalf("无头 resolved 不应推送: %+v", res)
	}
	if bc.count() != 0 {
		t.Fatalf("无头 resolved 不应推送, got %d", bc.count())
	}
	// 但仍落库（审计流水）
	if rec, _ := st.GetByFingerprint("fpX"); rec == nil {
		t.Fatal("无头 resolved 也应落库")
	}
}

func TestIngestNewIncidentAfterResolved(t *testing.T) {
	svc, _, bc := newTestService()
	svc.Ingest(&WebhookPayload{Alerts: []Alert{firingAlert("fp1", "GatewayDown", "critical")}})
	svc.Ingest(&WebhookPayload{Alerts: []Alert{resolvedAlert("fp1", "GatewayDown", "critical")}})

	// 同 fingerprint 再次 firing = 新事故 → 重新推送
	res := svc.Ingest(&WebhookPayload{Alerts: []Alert{firingAlert("fp1", "GatewayDown", "critical")}})
	if res.NotifiedFiring != 1 {
		t.Fatalf("resolved 后的新事故应重新推送 firing: %+v", res)
	}
	if bc.count() != 3 {
		t.Fatalf("累计应推送 3 次, got %d", bc.count())
	}
}

func TestIngestSeverityMapping(t *testing.T) {
	svc, _, bc := newTestService()
	svc.Ingest(&WebhookPayload{Alerts: []Alert{
		firingAlert("fp-crit", "GatewayDown", "critical"),
		firingAlert("fp-warn", "GatewayHighLatency", "warning"),
		firingAlert("fp-info", "GatewayPortfolioMetricsMissing", "info"),
	}})
	if bc.count() != 3 {
		t.Fatalf("3 条 firing 应推送 3 次, got %d", bc.count())
	}
	levels := map[string]string{}
	for _, tpl := range bc.template {
		levels[tpl.Tags["severity"]] = tpl.Level
	}
	if levels["critical"] != "CRITICAL" || levels["warning"] != "WARN" || levels["info"] != "INFO" {
		t.Fatalf("severity→level 映射错误: %+v", levels)
	}
	for _, tpl := range bc.template {
		if tpl.EventType != notify.EventAlert {
			t.Fatalf("事件类型应为 alert: %+v", tpl.EventType)
		}
	}
}

func TestFingerprintFallbackFromLabels(t *testing.T) {
	a1 := Alert{Status: "firing", Labels: map[string]string{"alertname": "A", "b": "2", "a": "1"}}
	a2 := Alert{Status: "firing", Labels: map[string]string{"a": "1", "alertname": "A", "b": "2"}}
	if a1.FingerprintKey() != a2.FingerprintKey() {
		t.Fatal("label 顺序不应影响兜底 fingerprint")
	}
	a3 := Alert{Status: "firing", Labels: map[string]string{"alertname": "B"}}
	if a1.FingerprintKey() == a3.FingerprintKey() {
		t.Fatal("不同 labelset 不得产生相同兜底 fingerprint")
	}
}

func TestServiceReloadSuppressesDuplicateFiring(t *testing.T) {
	st := newFakeEventStore()
	bc := &mockBroadcaster{}
	svc1 := NewService(st, bc)
	svc1.Ingest(&WebhookPayload{Alerts: []Alert{firingAlert("fp1", "GatewayDown", "critical")}})

	// 模拟重启：同一 store 新建 Service（回载去重状态），firing 重发不得重复推送
	bc2 := &mockBroadcaster{}
	svc2 := NewService(st, bc2)
	res := svc2.Ingest(&WebhookPayload{Alerts: []Alert{firingAlert("fp1", "GatewayDown", "critical")}})
	if res.NotifiedFiring != 0 || res.Suppressed != 1 {
		t.Fatalf("重启后 firing 重发应被抑制: %+v", res)
	}
	if bc2.count() != 0 {
		t.Fatalf("重启后不得重复推送 firing, got %d", bc2.count())
	}
}

func TestActiveAndHistory(t *testing.T) {
	svc, _, _ := newTestService()
	svc.Ingest(&WebhookPayload{Alerts: []Alert{
		firingAlert("fp1", "GatewayDown", "critical"),
		firingAlert("fp2", "GatewayHighMemory", "warning"),
	}})
	svc.Ingest(&WebhookPayload{Alerts: []Alert{resolvedAlert("fp2", "GatewayHighMemory", "warning")}})

	active, err := svc.Active(10)
	if err != nil || len(active) != 1 || active[0].Fingerprint != "fp1" {
		t.Fatalf("active 应只剩 fp1: %+v err=%v", active, err)
	}
	history, err := svc.History(10)
	if err != nil || len(history) != 2 {
		t.Fatalf("history 应含 2 条流水: %+v err=%v", history, err)
	}
}

func TestIngestEmptyPayload(t *testing.T) {
	svc, _, bc := newTestService()
	res := svc.Ingest(&WebhookPayload{Version: "4", Status: "firing"})
	if res.Received != 0 || bc.count() != 0 {
		t.Fatalf("空载荷应零推送: %+v", res)
	}
	if res := svc.Ingest(nil); res.Received != 0 {
		t.Fatalf("nil 载荷应安全返回零值: %+v", res)
	}
}
