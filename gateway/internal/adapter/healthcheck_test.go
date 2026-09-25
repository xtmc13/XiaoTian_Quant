package adapter

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 体检编排单测：mock adapter 验证分级、skip 语义、overall 汇总 ──

// mockTarget 满足 HealthCheckTarget 的最小 mock（无 Ping/WS/元数据能力）。
type mockTarget struct {
	tickerErr     error
	klinesErr     error
	balanceErr    error
	positionsErr  error
	openOrdersErr error
}

func (m *mockTarget) Name() string { return "mock" }

func (m *mockTarget) GetTicker(symbol string) (map[string]any, error) {
	if m.tickerErr != nil {
		return nil, m.tickerErr
	}
	return map[string]any{"symbol": symbol, "last": 1.0}, nil
}

func (m *mockTarget) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	if m.klinesErr != nil {
		return nil, m.klinesErr
	}
	return [][]any{{int64(1), "1", "1", "1", "1", "1"}}, nil
}

func (m *mockTarget) GetBalance() ([]map[string]any, error) {
	if m.balanceErr != nil {
		return nil, m.balanceErr
	}
	return []map[string]any{{"asset": "USDT", "free": 1.0}}, nil
}

func (m *mockTarget) GetPositions() ([]map[string]any, error) {
	if m.positionsErr != nil {
		return nil, m.positionsErr
	}
	return []map[string]any{}, nil
}

func (m *mockTarget) GetOpenOrders(symbol string) ([]map[string]any, error) {
	if m.openOrdersErr != nil {
		return nil, m.openOrdersErr
	}
	return []map[string]any{}, nil
}

// mockFullTarget 在最小 mock 上追加 Ping / WS / 合约账户能力。
type mockFullTarget struct {
	mockTarget
	pingErr        error
	streamErr      error
	emitTick       bool // StartMarketStream 后立即推一条 ticker
	futuresAcctErr error
	onTicker       func(tick model.Tick)
}

func (m *mockFullTarget) Ping() error { return m.pingErr }

func (m *mockFullTarget) StartMarketStream(symbols []string) error {
	if m.streamErr != nil {
		return m.streamErr
	}
	if m.emitTick && m.onTicker != nil {
		go m.onTicker(model.Tick{Symbol: symbols[0]})
	}
	return nil
}

func (m *mockFullTarget) OnTicker(fn func(tick model.Tick)) { m.onTicker = fn }

func (m *mockFullTarget) GetFuturesAccount() (map[string]any, error) {
	if m.futuresAcctErr != nil {
		return nil, m.futuresAcctErr
	}
	return map[string]any{"canTrade": true}, nil
}

func (m *mockFullTarget) Stop() error { return nil }

func findLevel(t *testing.T, r *ExchangeHealthReport, level int) HealthCheckLevel {
	t.Helper()
	for _, l := range r.Levels {
		if l.Level == level {
			return l
		}
	}
	t.Fatalf("level %d missing in report", level)
	return HealthCheckLevel{}
}

func findItem(t *testing.T, l HealthCheckLevel, name string) HealthCheckItem {
	t.Helper()
	for _, it := range l.Items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("item %s missing in level %d", name, l.Level)
	return HealthCheckItem{}
}

func TestHealthCheckHealthyFullyConfigured(t *testing.T) {
	hc := &HealthChecker{}
	target := &mockFullTarget{emitTick: true}
	report := hc.runAgainst(context.Background(), "mock", target, true, nil)

	btAssertEq(t, report.Overall, OverallHealthy, "overall")
	btAssert(t, report.DurationMs >= 0, "duration should be set")

	l1 := findLevel(t, report, 1)
	btAssertEq(t, l1.Status, CheckPass, "L1 status")
	btAssertEq(t, findItem(t, l1, "ping").Status, CheckPass, "ping")
	btAssertEq(t, findItem(t, l1, "ticker").Status, CheckPass, "ticker")
	btAssertEq(t, findItem(t, l1, "ohlcv").Status, CheckPass, "ohlcv")
	// orderbook 未实现 → skip 而非 fail
	btAssertEq(t, findItem(t, l1, "orderbook").Status, CheckSkip, "orderbook skip")

	l2 := findLevel(t, report, 2)
	btAssertEq(t, l2.Status, CheckPass, "L2 status")

	l3 := findLevel(t, report, 3)
	btAssertEq(t, l3.Status, CheckPass, "L3 status")
	btAssertEq(t, findItem(t, l3, "trade_fee").Status, CheckSkip, "trade_fee skip")
	btAssertEq(t, findItem(t, l3, "futures_permission").Status, CheckPass, "futures_permission")

	l4 := findLevel(t, report, 4)
	btAssertEq(t, l4.Status, CheckPass, "L4 status")
	btAssert(t, strings.Contains(findItem(t, l4, "ws_handshake").Detail, "首条"), "ws detail should mention first message")
}

func TestHealthCheckNotConfigured(t *testing.T) {
	hc := &HealthChecker{}
	target := &mockFullTarget{emitTick: true}
	report := hc.runAgainst(context.Background(), "mock", target, false, nil)

	btAssertEq(t, report.Overall, OverallNotConfigured, "overall")

	l2 := findLevel(t, report, 2)
	btAssertEq(t, l2.Status, CheckSkip, "L2 all skip")
	for _, it := range l2.Items {
		btAssertEq(t, it.Status, CheckSkip, "L2 item skip")
	}

	l3 := findLevel(t, report, 3)
	btAssertEq(t, l3.Status, CheckSkip, "L3 all skip")

	// L1/L4 无需凭证，仍真实执行
	btAssertEq(t, findLevel(t, report, 1).Status, CheckPass, "L1 runs without credentials")
	btAssertEq(t, findLevel(t, report, 4).Status, CheckPass, "L4 runs without credentials")
}

func TestHealthCheckUnhealthyOnL1Failure(t *testing.T) {
	hc := &HealthChecker{}
	target := &mockFullTarget{emitTick: true}
	target.tickerErr = fmt.Errorf("connection refused")
	report := hc.runAgainst(context.Background(), "mock", target, true, nil)

	btAssertEq(t, report.Overall, OverallUnhealthy, "overall")
	btAssertEq(t, findLevel(t, report, 1).Status, CheckFail, "L1 fail")
	btAssertEq(t, findItem(t, findLevel(t, report, 1), "ticker").Status, CheckFail, "ticker fail")
}

func TestHealthCheckUnhealthyOnL2Failure(t *testing.T) {
	hc := &HealthChecker{}
	target := &mockFullTarget{emitTick: true}
	target.balanceErr = fmt.Errorf("invalid api key")
	report := hc.runAgainst(context.Background(), "mock", target, true, nil)

	btAssertEq(t, report.Overall, OverallUnhealthy, "overall")
	btAssertEq(t, findLevel(t, report, 2).Status, CheckFail, "L2 fail")
}

func TestHealthCheckDegradedOnL3Failure(t *testing.T) {
	hc := &HealthChecker{}
	target := &mockFullTarget{emitTick: true}
	target.futuresAcctErr = fmt.Errorf("permission denied")
	report := hc.runAgainst(context.Background(), "mock", target, true, nil)

	btAssertEq(t, report.Overall, OverallDegraded, "overall")
	btAssertEq(t, findLevel(t, report, 3).Status, CheckFail, "L3 fail")
}

func TestHealthCheckDegradedOnL4Failure(t *testing.T) {
	hc := &HealthChecker{}
	target := &mockFullTarget{streamErr: fmt.Errorf("ws dial error")}
	report := hc.runAgainst(context.Background(), "mock", target, true, nil)

	btAssertEq(t, report.Overall, OverallDegraded, "overall")
	btAssertEq(t, findLevel(t, report, 4).Status, CheckFail, "L4 fail")
}

func TestHealthCheckSkipSemanticsMinimalAdapter(t *testing.T) {
	hc := &HealthChecker{}
	// mockTarget 无 Ping / StartMarketStream / 元数据接口
	report := hc.runAgainst(context.Background(), "mock", &mockTarget{}, true, nil)

	l1 := findLevel(t, report, 1)
	btAssertEq(t, findItem(t, l1, "ping").Status, CheckSkip, "ping skip")
	btAssertEq(t, findItem(t, l1, "orderbook").Status, CheckSkip, "orderbook skip")
	btAssertEq(t, l1.Status, CheckPass, "L1 pass via ticker/ohlcv")

	l4 := findLevel(t, report, 4)
	btAssertEq(t, l4.Status, CheckSkip, "L4 skip when StartMarketStream missing")

	// skip 不影响 overall
	btAssertEq(t, report.Overall, OverallHealthy, "overall")
}

func TestHealthCheckSecretSanitized(t *testing.T) {
	hc := &HealthChecker{}
	secret := "super-secret-key-12345"
	target := &mockTarget{balanceErr: fmt.Errorf("auth failed for key %s", secret)}
	report := hc.runAgainst(context.Background(), "mock", target, true, []string{secret})

	item := findItem(t, findLevel(t, report, 2), "balance")
	btAssertEq(t, item.Status, CheckFail, "balance fail")
	btAssert(t, !strings.Contains(item.Error, secret), "error must not leak secret")
	btAssert(t, strings.Contains(item.Error, "***"), "error should be masked")
}

func TestHealthCheckPerCheckTimeout(t *testing.T) {
	hc := &HealthChecker{PerCheckTimeout: 50 * time.Millisecond}
	target := &mockTarget{klinesErr: nil}
	// 用一个永远阻塞的 mock 替换 GetKlines 行为
	blocking := &mockBlockingTarget{mockTarget: *target}
	report := hc.runAgainst(context.Background(), "mock", blocking, true, nil)

	item := findItem(t, findLevel(t, report, 1), "ohlcv")
	btAssertEq(t, item.Status, CheckFail, "ohlcv timeout fail")
	btAssert(t, strings.Contains(item.Error, "超时"), "timeout error message")
	btAssertEq(t, report.Overall, OverallUnhealthy, "overall unhealthy on L1 timeout")
}

// mockBlockingTarget 的 GetKlines 永远阻塞，验证看门狗超时。
type mockBlockingTarget struct {
	mockTarget
}

func (m *mockBlockingTarget) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	select {}
}

func TestHealthCheckWSFirstMessageTimeout(t *testing.T) {
	hc := &HealthChecker{WSMessageTimeout: 50 * time.Millisecond}
	// emitTick=false：握手成功但永远等不到首条消息
	target := &mockFullTarget{}
	report := hc.runAgainst(context.Background(), "mock", target, true, nil)

	item := findItem(t, findLevel(t, report, 4), "ws_handshake")
	btAssertEq(t, item.Status, CheckFail, "ws first message timeout fail")
	btAssert(t, strings.Contains(item.Error, "首条消息"), "timeout error message")
	btAssertEq(t, report.Overall, OverallDegraded, "overall degraded on L4 failure")
}

func TestSummarizeOverallRules(t *testing.T) {
	lvl := func(level int, status string) HealthCheckLevel {
		return HealthCheckLevel{Level: level, Status: status}
	}
	cases := []struct {
		name       string
		configured bool
		levels     []HealthCheckLevel
		want       string
	}{
		{"all pass configured", true, []HealthCheckLevel{lvl(1, CheckPass), lvl(2, CheckPass), lvl(3, CheckPass), lvl(4, CheckPass)}, OverallHealthy},
		{"l3/l4 skip still healthy", true, []HealthCheckLevel{lvl(1, CheckPass), lvl(2, CheckPass), lvl(3, CheckSkip), lvl(4, CheckSkip)}, OverallHealthy},
		{"l1 fail", true, []HealthCheckLevel{lvl(1, CheckFail), lvl(2, CheckPass), lvl(3, CheckPass), lvl(4, CheckPass)}, OverallUnhealthy},
		{"l2 fail", true, []HealthCheckLevel{lvl(1, CheckPass), lvl(2, CheckFail), lvl(3, CheckPass), lvl(4, CheckPass)}, OverallUnhealthy},
		{"l3 fail degraded", true, []HealthCheckLevel{lvl(1, CheckPass), lvl(2, CheckPass), lvl(3, CheckFail), lvl(4, CheckPass)}, OverallDegraded},
		{"l4 fail degraded", true, []HealthCheckLevel{lvl(1, CheckPass), lvl(2, CheckPass), lvl(3, CheckPass), lvl(4, CheckFail)}, OverallDegraded},
		{"not configured", false, []HealthCheckLevel{lvl(1, CheckPass), lvl(2, CheckSkip), lvl(3, CheckSkip), lvl(4, CheckPass)}, OverallNotConfigured},
		{"not configured but l1 broken", false, []HealthCheckLevel{lvl(1, CheckFail), lvl(2, CheckSkip), lvl(3, CheckSkip), lvl(4, CheckSkip)}, OverallUnhealthy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			btAssertEq(t, summarizeOverall(tc.configured, tc.levels), tc.want, "overall")
		})
	}
}

func TestBuildHealthCheckTargetAllExchanges(t *testing.T) {
	for _, name := range SupportedHealthCheckExchanges() {
		target, err := BuildHealthCheckTarget(name)
		btAssert(t, err == nil, fmt.Sprintf("build %s: %v", name, err))
		btAssert(t, target != nil, "target nil for "+name)
	}
	// 别名 gateio → gate
	target, err := BuildHealthCheckTarget("gateio")
	btAssert(t, err == nil && target != nil, "gateio alias should build")
	btAssertEq(t, target.Name(), "gateio", "gateio adapter name")

	if _, err := BuildHealthCheckTarget("unknown-exchange"); err == nil {
		t.Fatal("expected error for unsupported exchange")
	}
	btAssert(t, IsHealthCheckExchange("binance"), "binance supported")
	btAssert(t, IsHealthCheckExchange("gateio"), "gateio alias supported")
	btAssert(t, !IsHealthCheckExchange("kucoin"), "kucoin not supported")
}
