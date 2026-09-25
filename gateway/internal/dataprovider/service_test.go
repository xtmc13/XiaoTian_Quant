package dataprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ── 测试桩 ──

// fakeSource 可控源：configured/fetch 行为由测试编排。
type fakeSource struct {
	name       string
	ttl        time.Duration
	configured bool
	calls      atomic.Int32
	fail       atomic.Bool
	payload    any
}

func (f *fakeSource) Name() string               { return f.name }
func (f *fakeSource) Description() string        { return "fake " + f.name }
func (f *fakeSource) TTL() time.Duration         { return f.ttl }
func (f *fakeSource) MinInterval() time.Duration { return 0 } // 测试不限流
func (f *fakeSource) RequiresKey() bool          { return !f.configured }
func (f *fakeSource) Configured() bool           { return f.configured }
func (f *fakeSource) Fetch(ctx context.Context) (any, error) {
	f.calls.Add(1)
	if f.fail.Load() {
		return nil, errors.New("upstream boom with secret FREDKEY999 in url")
	}
	return f.payload, nil
}

// memStore 内存 CacheStore。
type memStore struct {
	mu   sync.Mutex
	rows map[string]string
}

func newMemStore() *memStore { return &memStore{rows: map[string]string{}} }

func (m *memStore) UpsertDataProviderCache(source, cacheKey, payload string, fetchedAt, expiresAt int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[source+"/"+cacheKey] = payload
	return nil
}

func (m *memStore) GetDataProviderCache(source, cacheKey string) (string, int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.rows[source+"/"+cacheKey]
	if !ok {
		return "", 0, 0, errors.New("no rows")
	}
	return p, time.Now().UnixMilli(), time.Now().Add(time.Hour).UnixMilli(), nil
}

func newTestService(srcs []Source, store CacheStore) *Service {
	s := NewService(srcs, store, []string{"FREDKEY999"})
	s.SetLogger(log.New(io.Discard, "", 0))
	return s
}

// ── Get 流程 ──

func TestGetNotConfigured(t *testing.T) {
	svc := newTestService([]Source{&fakeSource{name: "fred", ttl: time.Hour, configured: false}}, nil)
	res := svc.Get(context.Background(), "fred")
	if res.Status != "not_configured" {
		t.Fatalf("expected not_configured, got %s", res.Status)
	}
	if res.Data != nil {
		t.Fatal("not_configured should carry no data")
	}
}

func TestGetUnknownSource(t *testing.T) {
	svc := newTestService(nil, nil)
	if res := svc.Get(context.Background(), "nope"); res.Status != "unknown_source" {
		t.Fatalf("expected unknown_source, got %s", res.Status)
	}
}

func TestGetFetchThenCacheHit(t *testing.T) {
	src := &fakeSource{name: "fear_greed", ttl: time.Hour, configured: true, payload: &FearGreedData{Value: 42, Classification: "Fear"}}
	svc := newTestService([]Source{src}, newMemStore())

	res := svc.Get(context.Background(), "fear_greed")
	if res.Status != "ok" {
		t.Fatalf("expected ok, got %s (%s)", res.Status, res.Error)
	}
	if src.calls.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", src.calls.Load())
	}
	// 第二次命中缓存，不再打上游
	res2 := svc.Get(context.Background(), "fear_greed")
	if res2.Status != "ok" || src.calls.Load() != 1 {
		t.Fatalf("cache should serve second call: status=%s calls=%d", res2.Status, src.calls.Load())
	}
	fg, ok := res2.Data.(*FearGreedData)
	if !ok || fg.Value != 42 {
		t.Fatalf("bad cached data: %+v", res2.Data)
	}
}

func TestGetStaleOnFailureWithOldData(t *testing.T) {
	src := &fakeSource{name: "news", ttl: 50 * time.Millisecond, configured: true, payload: &NewsData{Items: []NewsItem{{ID: "1", Title: "t"}}}}
	svc := newTestService([]Source{src}, nil)

	if res := svc.Get(context.Background(), "news"); res.Status != "ok" {
		t.Fatalf("first get: %s", res.Status)
	}
	// 等缓存过期，源开始失败 → 应回 stale 且仍带旧数据
	time.Sleep(60 * time.Millisecond)
	src.fail.Store(true)
	res := svc.Get(context.Background(), "news")
	if res.Status != "stale" {
		t.Fatalf("expected stale, got %s", res.Status)
	}
	if res.Data == nil {
		t.Fatal("stale response should carry old data")
	}
	// 异步刷新被触发但失败 → 熔断计数增加；稍等让 goroutine 跑完
	time.Sleep(50 * time.Millisecond)
	h := svc.Health()[0]
	if h.State != "stale" && h.State != "no_data" {
		t.Fatalf("health state unexpected: %s", h.State)
	}
}

func TestGetUnavailableWhenNoDataAndFailing(t *testing.T) {
	src := &fakeSource{name: "coinglass", ttl: time.Minute, configured: true}
	src.fail.Store(true)
	svc := newTestService([]Source{src}, nil)

	res := svc.Get(context.Background(), "coinglass")
	if res.Status != "unavailable" {
		t.Fatalf("expected unavailable, got %s", res.Status)
	}
	if res.Error == "" {
		t.Fatal("unavailable should carry sanitized error")
	}
	if strings.Contains(res.Error, "FREDKEY999") {
		t.Fatalf("error leaked secret: %s", res.Error)
	}
	if !strings.Contains(res.Error, "***") {
		t.Fatalf("error should be sanitized: %s", res.Error)
	}
}

func TestCircuitOpensAfterRepeatedFailures(t *testing.T) {
	src := &fakeSource{name: "coinglass", ttl: time.Minute, configured: true}
	src.fail.Store(true)
	svc := newTestService([]Source{src}, nil)

	for i := 0; i < 3; i++ {
		svc.Get(context.Background(), "coinglass")
		// 绕过限流（fakeSource MinInterval=0，但 limiter 首次后 last 已记录，
		// MinInterval=0 意味着总是放行，无需处理）
	}
	h := svc.Health()[0]
	if h.Circuit != "open" {
		t.Fatalf("circuit should be open after 3 failures, got %s", h.Circuit)
	}
	if h.State == "ok" {
		t.Fatal("health should not be ok")
	}
	// 开闸后不再打上游
	callsBefore := src.calls.Load()
	res := svc.Get(context.Background(), "coinglass")
	if res.Status != "unavailable" || !strings.Contains(res.Error, "circuit open") {
		t.Fatalf("open circuit should short-circuit: %+v", res)
	}
	if src.calls.Load() != callsBefore {
		t.Fatal("open circuit must not hit upstream")
	}
}

func TestPersistAndHydrate(t *testing.T) {
	store := newMemStore()
	src := &fakeSource{name: "fear_greed", ttl: time.Hour, configured: true, payload: &FearGreedData{Value: 77, Classification: "Greed"}}
	svc := newTestService([]Source{src}, store)

	if res := svc.Get(context.Background(), "fear_greed"); res.Status != "ok" {
		t.Fatalf("get: %s", res.Status)
	}
	if _, ok := store.rows["fear_greed/default"]; !ok {
		t.Fatal("fetch should persist to store")
	}

	// 新实例（模拟重启）：内存空 → 从 DB 回灌，不打上游
	src2 := &fakeSource{name: "fear_greed", ttl: time.Hour, configured: true, payload: &FearGreedData{Value: 1}}
	svc2 := newTestService([]Source{src2}, store)
	res := svc2.Get(context.Background(), "fear_greed")
	if res.Status != "ok" {
		t.Fatalf("hydrated get: %s", res.Status)
	}
	if src2.calls.Load() != 0 {
		t.Fatal("hydrate should not hit upstream")
	}
	raw, ok := res.Data.(json.RawMessage)
	if !ok {
		t.Fatalf("hydrated data should be raw json, got %T", res.Data)
	}
	var fg FearGreedData
	if err := json.Unmarshal(raw, &fg); err != nil || fg.Value != 77 {
		t.Fatalf("hydrated payload wrong: %v %+v", err, fg)
	}
}

func TestHealthStates(t *testing.T) {
	okSrc := &fakeSource{name: "fear_greed", ttl: time.Hour, configured: true, payload: &FearGreedData{Value: 1}}
	noKey := &fakeSource{name: "fred", ttl: time.Hour, configured: false}
	svc := newTestService([]Source{okSrc, noKey}, nil)

	health := svc.Health()
	if len(health) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(health))
	}
	byName := map[string]SourceHealth{}
	for _, h := range health {
		byName[h.Name] = h
	}
	if byName["fred"].State != "not_configured" {
		t.Fatalf("fred should be not_configured: %+v", byName["fred"])
	}
	if byName["fear_greed"].State != "no_data" {
		t.Fatalf("fear_greed before fetch should be no_data: %+v", byName["fear_greed"])
	}
	svc.Get(context.Background(), "fear_greed")
	health = svc.Health()
	for _, h := range health {
		if h.Name == "fear_greed" && h.State != "ok" {
			t.Fatalf("fear_greed after fetch should be ok: %+v", h)
		}
	}
}

func TestSchedulerStartStop(t *testing.T) {
	src := &fakeSource{name: "fear_greed", ttl: time.Hour, configured: true, payload: &FearGreedData{Value: 5}}
	svc := newTestService([]Source{src}, newMemStore())
	svc.Start()
	if !svc.IsRunning() {
		t.Fatal("service should be running")
	}
	// 启动即跑一轮
	deadline := time.Now().Add(2 * time.Second)
	for src.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if src.calls.Load() == 0 {
		t.Fatal("initial refresh should have fetched")
	}
	svc.Stop()
	if svc.IsRunning() {
		t.Fatal("service should be stopped")
	}
}

// ── HTTP handlers ──

func setupTestRouter(svc *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/dataproviders")
	RegisterRoutes(g, svc)
	return r
}

func TestAPIEndpoints(t *testing.T) {
	fg := &fakeSource{name: "fear_greed", ttl: time.Hour, configured: true, payload: &FearGreedData{Value: 23, Classification: "Extreme Fear"}}
	cg := &fakeSource{name: "coinglass", ttl: time.Hour, configured: false}
	fred := &fakeSource{name: "fred", ttl: time.Hour, configured: false}
	news := &fakeSource{name: "news", ttl: time.Hour, configured: true, payload: &NewsData{Items: []NewsItem{
		{ID: "1", Title: "BTC up", Categories: []string{"BTC"}},
		{ID: "2", Title: "ETH news", Categories: []string{"ETH"}},
	}}}
	hm := &fakeSource{name: "heatmap", ttl: time.Hour, configured: true, payload: &HeatmapData{Entries: []HeatmapEntry{
		{Symbol: "BTCUSDT", Base: "BTC", Price: 67000, ChangePct24h: 2.5, Volume24h: 1e9, Weight: 1},
	}}}
	cal := &fakeSource{name: "calendar", ttl: time.Hour, configured: true, payload: &CalendarData{Events: []CalendarEvent{
		{ID: "e1", Name: "CPI", Date: "2026-09-23", Impact: "high", Forecast: "0.2%", Previous: "0.3%"},
	}}}

	svc := newTestService([]Source{fg, cg, fred, news, hm, cal}, nil)
	r := setupTestRouter(svc)

	get := func(path string) (int, map[string]any) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		r.ServeHTTP(w, req)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}

	// /sentiment：fg ok + coinglass not_configured（降级不报错）
	code, body := get("/dataproviders/sentiment")
	if code != http.StatusOK {
		t.Fatalf("sentiment code=%d", code)
	}
	if body["fear_greed"].(map[string]any)["status"] != "ok" {
		t.Fatalf("fear_greed should be ok: %v", body)
	}
	if body["derivatives"].(map[string]any)["status"] != "not_configured" {
		t.Fatalf("derivatives should degrade to not_configured: %v", body)
	}

	// /macro：未配置降级
	code, body = get("/dataproviders/macro")
	if code != http.StatusOK || body["status"] != "not_configured" {
		t.Fatalf("macro degrade wrong: code=%d body=%v", code, body)
	}

	// /news + 币种过滤
	code, body = get("/dataproviders/news")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("news: code=%d body=%v", code, body)
	}
	items := body["data"].(map[string]any)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("news items: %v", items)
	}
	_, body = get("/dataproviders/news?symbol=BTC")
	items = body["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != "1" {
		t.Fatalf("news BTC filter wrong: %v", items)
	}

	// /heatmap
	code, body = get("/dataproviders/heatmap")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("heatmap: code=%d body=%v", code, body)
	}
	entries := body["data"].(map[string]any)["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("heatmap entries: %v", entries)
	}

	// /calendar
	code, body = get("/dataproviders/calendar")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("calendar: code=%d body=%v", code, body)
	}
	events := body["data"].(map[string]any)["events"].([]any)
	if len(events) != 1 || events[0].(map[string]any)["impact"] != "high" {
		t.Fatalf("calendar events: %v", events)
	}

	// /sources
	code, body = get("/dataproviders/sources")
	if code != http.StatusOK {
		t.Fatalf("sources: code=%d", code)
	}
	sources := body["sources"].([]any)
	if len(sources) != 6 {
		t.Fatalf("sources count: %d", len(sources))
	}
	states := map[string]string{}
	for _, s := range sources {
		m := s.(map[string]any)
		states[m["name"].(string)] = m["state"].(string)
		// 响应绝不泄露密钥
		if le, ok := m["last_error"].(string); ok && strings.Contains(le, "FREDKEY999") {
			t.Fatal("secret leaked in /sources")
		}
	}
	if states["fred"] != "not_configured" || states["fear_greed"] != "ok" {
		t.Fatalf("source states wrong: %v", states)
	}
}
