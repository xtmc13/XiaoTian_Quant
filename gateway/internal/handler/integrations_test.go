package handler

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ── 集成预检单测 ────────────────────────────────────────────────
// 全部基于 httptest / 本地 listener mock，不触达真实第三方服务。
// 真实密钥联调需在部署环境用 POST /api/integrations/:name/check 实测。

// testSpec 构造一个指向 mock endpoint 的注册表条目。
func testSpec(name string, configured bool, endpoint string) integrationSpec {
	return integrationSpec{
		Name:        name,
		DisplayName: name,
		Category:    "test",
		RequiredEnv: []string{"TEST_ENV"},
		Configured:  func() (bool, string) { return configured, "" },
		Endpoint:    func() string { return endpoint },
	}
}

func clearIntegrationCache(t *testing.T) {
	t.Helper()
	integrationProbeCacheMu.Lock()
	integrationProbeCache = make(map[string]IntegrationStatus)
	integrationProbeCacheMu.Unlock()
	t.Cleanup(func() {
		integrationProbeCacheMu.Lock()
		integrationProbeCache = make(map[string]IntegrationStatus)
		integrationProbeCacheMu.Unlock()
	})
}

// TestIntegrationStatusAggregation 状态聚合：探测结果写缓存后，GET 聚合态能读到。
func TestIntegrationStatusAggregation(t *testing.T) {
	clearIntegrationCache(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed) // 任何 HTTP 响应都视为可达
	}))
	defer mock.Close()

	spec := testSpec("agg_ok", true, mock.URL)

	// 探测前：configured=true 但 reachable 为 nil（未探测）
	st := currentIntegrationStatus(spec)
	if !st.Configured {
		t.Fatalf("expected configured=true, got %+v", st)
	}
	if st.Reachable != nil {
		t.Fatalf("expected reachable=nil before probe, got %v", *st.Reachable)
	}
	if st.LastVerifiedAt != 0 {
		t.Fatalf("expected last_verified_at=0 before probe, got %d", st.LastVerifiedAt)
	}

	// 主动探测：写缓存
	checked := runIntegrationCheck(spec)
	if checked.Reachable == nil || !*checked.Reachable {
		t.Fatalf("expected reachable=true after check, got %+v (notes=%s)", checked, checked.Notes)
	}
	if checked.LastVerifiedAt <= 0 {
		t.Fatalf("expected last_verified_at>0 after check, got %d", checked.LastVerifiedAt)
	}

	// 聚合态读到缓存
	agg := currentIntegrationStatus(spec)
	if agg.Reachable == nil || !*agg.Reachable {
		t.Fatalf("aggregation lost probe result: %+v", agg)
	}
	if agg.LastVerifiedAt != checked.LastVerifiedAt {
		t.Fatalf("aggregation last_verified_at mismatch: %d vs %d", agg.LastVerifiedAt, checked.LastVerifiedAt)
	}
}

// TestIntegrationUnconfiguredDegrade 未配置降级：configured=false、reachable 恒 nil、
// notes 含所需 env 名；且已配置的缓存探测结果在配置被移除后被清除。
func TestIntegrationUnconfiguredDegrade(t *testing.T) {
	clearIntegrationCache(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer mock.Close()

	configured := true
	spec := integrationSpec{
		Name:        "degrade",
		DisplayName: "degrade",
		Category:    "test",
		RequiredEnv: []string{"SOME_API_KEY"},
		Configured:  func() (bool, string) { return configured, "" },
		Endpoint:    func() string { return mock.URL },
	}

	// 先探测一次写入缓存
	if st := runIntegrationCheck(spec); st.Reachable == nil || !*st.Reachable {
		t.Fatalf("precondition probe failed: %+v", st)
	}

	// 模拟密钥被移除 → 降级
	configured = false
	st := currentIntegrationStatus(spec)
	if st.Configured {
		t.Fatalf("expected configured=false, got %+v", st)
	}
	if st.Reachable != nil {
		t.Fatalf("unconfigured must not expose reachable, got %v", *st.Reachable)
	}
	if st.LastVerifiedAt != 0 {
		t.Fatalf("unconfigured must not expose stale last_verified_at, got %d", st.LastVerifiedAt)
	}
	if !strings.Contains(st.Notes, "SOME_API_KEY") {
		t.Fatalf("notes should name required env vars, got %q", st.Notes)
	}

	// 降级后主动探测：不发请求、清缓存
	st = runIntegrationCheck(spec)
	if st.Reachable != nil {
		t.Fatalf("check on unconfigured integration must not probe, got %+v", st)
	}
	integrationProbeCacheMu.Lock()
	_, cached := integrationProbeCache[spec.Name]
	integrationProbeCacheMu.Unlock()
	if cached {
		t.Fatalf("unconfigured check must clear probe cache")
	}
}

// TestIntegrationProbeTimeout 探测超时：慢端点在超时预算内必须返回不可达 + 超时标注。
func TestIntegrationProbeTimeout(t *testing.T) {
	clearIntegrationCache(t)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer slow.Close()

	orig := integrationProbeTimeout
	integrationProbeTimeout = 100 * time.Millisecond
	defer func() { integrationProbeTimeout = orig }()

	spec := testSpec("slow", true, slow.URL)
	start := time.Now()
	st := runIntegrationCheck(spec)
	elapsed := time.Since(start)

	if st.Reachable == nil || *st.Reachable {
		t.Fatalf("expected reachable=false on timeout, got %+v", st)
	}
	if !strings.Contains(st.Notes, "超时") {
		t.Fatalf("timeout note expected, got %q", st.Notes)
	}
	if elapsed > time.Second {
		t.Fatalf("probe exceeded timeout budget: %v", elapsed)
	}
}

// TestIntegrationNotesNotDuplicated 配置侧 note（如格式告警）在 check 响应与
// status 聚合中只出现一次（缓存只存探测侧 note）。
func TestIntegrationNotesNotDuplicated(t *testing.T) {
	clearIntegrationCache(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer mock.Close()

	spec := integrationSpec{
		Name:        "notes_dedup",
		DisplayName: "notes_dedup",
		Category:    "test",
		RequiredEnv: []string{"TEST_ENV"},
		Configured:  func() (bool, string) { return true, "格式告警X" },
		Endpoint:    func() string { return mock.URL },
	}

	checked := runIntegrationCheck(spec)
	if n := strings.Count(checked.Notes, "格式告警X"); n != 1 {
		t.Fatalf("check response duplicated config note (%d): %q", n, checked.Notes)
	}
	agg := currentIntegrationStatus(spec)
	if n := strings.Count(agg.Notes, "格式告警X"); n != 1 {
		t.Fatalf("aggregated status duplicated config note (%d): %q", n, agg.Notes)
	}
	if !strings.Contains(agg.Notes, "HTTP") {
		t.Fatalf("aggregated status lost probe note: %q", agg.Notes)
	}
}

// TestIntegrationProbeTCP TCP 探测路径（IBKR 等无 HTTP 端点的集成）。
func TestIntegrationProbeTCP(t *testing.T) {
	clearIntegrationCache(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	defer ln.Close()

	spec := testSpec("tcp_ok", true, ln.Addr().String())
	if st := runIntegrationCheck(spec); st.Reachable == nil || !*st.Reachable {
		t.Fatalf("expected tcp reachable=true, got %+v (notes=%s)", st, st.Notes)
	}

	ln.Close() // 关端口 → 不可达
	spec2 := testSpec("tcp_fail", true, ln.Addr().String())
	if st := runIntegrationCheck(spec2); st.Reachable == nil || *st.Reachable {
		t.Fatalf("expected tcp reachable=false on closed port, got %+v", st)
	}
}

// TestIntegrationRegistryEnvWiring 注册表条目与真实 env 的接线：
// 未设置 env 时降级；设置后 configured=true；凭证格式异常进 notes。
func TestIntegrationRegistryEnvWiring(t *testing.T) {
	clearIntegrationCache(t)
	byName := map[string]integrationSpec{}
	for _, s := range integrationSpecs() {
		byName[s.Name] = s
	}

	// Twilio：缺 env → 未配置
	tw := byName["twilio"]
	if ok, _ := tw.Configured(); ok {
		t.Fatalf("twilio should be unconfigured without env")
	}

	// 配置齐但 SID 前缀错误 → configured=true + 格式告警
	t.Setenv("TWILIO_ACCOUNT_SID", "XX123")
	t.Setenv("TWILIO_AUTH_TOKEN", "token")
	t.Setenv("TWILIO_FROM_NUMBER", "+10000000000")
	if ok, note := tw.Configured(); !ok || !strings.Contains(note, "AC") {
		t.Fatalf("twilio bad-format SID should be configured with format note, ok=%v note=%q", ok, note)
	}

	// 格式正确 → 无格式告警
	t.Setenv("TWILIO_ACCOUNT_SID", "AC1234567890abcdef")
	if ok, note := tw.Configured(); !ok || note != "" {
		t.Fatalf("twilio well-formed should be configured without note, ok=%v note=%q", ok, note)
	}

	// BEP20：地址+核验通道缺一不可
	bsc := byName["usdt_bep20"]
	t.Setenv("USDT_BEP20_ADDRESS", "0xdac17f958d2ee523a2206206994597c13d831ec7")
	if ok, _ := bsc.Configured(); ok {
		t.Fatalf("bep20 without verifier channel should be unconfigured")
	}
	t.Setenv("BSCSCAN_API_KEY", "dummy")
	if ok, note := bsc.Configured(); !ok || note != "" {
		t.Fatalf("bep20 with address+key should be configured, ok=%v note=%q", ok, note)
	}
	t.Setenv("USDT_BEP20_ADDRESS", "not-an-address")
	if ok, note := bsc.Configured(); !ok || !strings.Contains(note, "格式异常") {
		t.Fatalf("bep20 bad address should warn, ok=%v note=%q", ok, note)
	}
}

// TestIntegrationCheckUnknownName 未知集成名 → 404（handler 级）。
func TestIntegrationCheckUnknownName(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "name", Value: "nonexistent"}}
	IntegrationCheck(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestIntegrationStatusNoSecretLeak 密钥绝不回显：状态 JSON 不得包含任何密钥取值。
func TestIntegrationStatusNoSecretLeak(t *testing.T) {
	clearIntegrationCache(t)
	secret := "sk_live_SECRET_VALUE_NEVER_ECHO"
	t.Setenv("STRIPE_SECRET_KEY", secret)
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_dummy")

	byName := map[string]integrationSpec{}
	for _, s := range integrationSpecs() {
		byName[s.Name] = s
	}
	st := currentIntegrationStatus(byName["stripe"])
	if !st.Configured {
		t.Fatalf("stripe should be configured with env set")
	}
	body, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), secret) || strings.Contains(string(body), "whsec_dummy") {
		t.Fatalf("status JSON leaks secret: %s", body)
	}
}

// ── 测试辅助 ────────────────────────────────────────────────────

func TestAddressFormatHelpers(t *testing.T) {
	valid := "0xdac17f958d2ee523a2206206994597c13d831ec7"
	if !looksLikeEVMAddress(valid) {
		t.Fatalf("valid evm address rejected")
	}
	for _, bad := range []string{"", "0x123", "0X" + valid[2:], valid + "ff"} {
		if looksLikeEVMAddress(bad) {
			t.Fatalf("invalid evm address accepted: %q", bad)
		}
	}
	if !looksLikeBase58("TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t") {
		t.Fatalf("trc20 contract should be base58")
	}
	if looksLikeBase58("0xnotbase58") || looksLikeBase58("short") {
		t.Fatalf("base58 helper too permissive")
	}
}
