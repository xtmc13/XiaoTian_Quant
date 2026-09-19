package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 便捷指标必须包级单例注册：重复调用不能导致计数清零（回归测试）。
func TestRecordOrderAccumulates(t *testing.T) {
	RecordOrder("BUY", "FILLED")
	RecordOrder("BUY", "FILLED")
	RecordOrder("SELL", "CANCELED")

	out := globalRegistry.String()
	if !strings.Contains(out, `orders_total{side="BUY",status="FILLED"} 2`) {
		t.Fatalf("BUY/FILLED 计数应为 2，输出:\n%s", out)
	}
	if !strings.Contains(out, `orders_total{side="SELL",status="CANCELED"} 1`) {
		t.Fatalf("SELL/CANCELED 计数应为 1，输出:\n%s", out)
	}
}

func TestRecordSignalAccumulates(t *testing.T) {
	RecordSignal("grid", "long")
	RecordSignal("grid", "long")
	RecordSignal("grid", "short")

	out := globalRegistry.String()
	if !strings.Contains(out, `signals_total{strategy="grid",direction="long"} 2`) {
		t.Fatalf("grid/long 信号计数应为 2，输出:\n%s", out)
	}
}

func TestGaugesKeepLatestValue(t *testing.T) {
	SetEquity(12345.6)
	SetPositionCount(3)
	SetActiveStrategies(7)

	out := globalRegistry.String()
	for _, want := range []string{
		"portfolio_equity_usdt 12345.6",
		"portfolio_positions 3",
		"strategies_active 7",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺少 %q，输出:\n%s", want, out)
		}
	}
}

// ── A9.1 业务指标 ──

func TestBusinessMetricsAccumulate(t *testing.T) {
	RecordFill("BUY", 1.5)
	RecordFill("BUY", 0.5)
	RecordFill("SELL", 2)
	RecordFill("BUY", -1) // 非正数量应被忽略

	RecordRiskRejection()
	RecordRiskRejection()

	RecordNotifySend("sms", "success")
	RecordNotifySend("sms", "failure")
	RecordNotifySend("telegram", "success")

	RecordReconcileDiff("position_quantity", "binance")
	RecordReconcileDiff("funding", "okx")

	SetBotsRunning("grid", 2)
	SetBotsRunning("ai", 1)

	out := globalRegistry.String()
	for _, want := range []string{
		`fills_total{side="BUY"} 2.000`,
		`fills_total{side="SELL"} 2.000`,
		"risk_order_rejections_total 2",
		`notify_sends_total{channel="sms",result="success"} 1`,
		`notify_sends_total{channel="sms",result="failure"} 1`,
		`notify_sends_total{channel="telegram",result="success"} 1`,
		`reconcile_diffs_total{type="position_quantity",exchange="binance"} 1`,
		`reconcile_diffs_total{type="funding",exchange="okx"} 1`,
		`bots_running{type="grid"} 2.000`,
		`bots_running{type="ai"} 1.000`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺少 %q，输出:\n%s", want, out)
		}
	}
}

func TestWSConnectionsGauge(t *testing.T) {
	IncWSConnections()
	IncWSConnections()
	DecWSConnections()
	SetWSConnections(10)

	out := globalRegistry.String()
	if !strings.Contains(out, "ws_connections 10.000") {
		t.Fatalf("ws_connections 应为 10（Set 覆盖先前的 +2/-1），输出:\n%s", out)
	}
}

func TestRuntimeMetricsIncludeUptimeAndGC(t *testing.T) {
	out := runtimeMetrics()
	for _, want := range []string{
		"process_uptime_seconds",
		"process_start_time_seconds",
		"go_gc_cycles_total",
		"go_goroutines",
		"go_memstats_heap_alloc_bytes",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("runtime 指标缺少 %q，输出:\n%s", want, out)
		}
	}
}

func TestHTTPMiddlewareRecordsStatusAndDuration(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := HTTPMiddleware(inner)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/test", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("unexpected status %d", rec.Code)
	}

	out := globalRegistry.String()
	if !strings.Contains(out, `http_requests_total{method="GET",path="/api/test",status="418"} 1`) {
		t.Fatalf("请求计数缺少 418 桶，输出:\n%s", out)
	}
	if !strings.Contains(out, `http_request_duration_seconds_count{method="GET",path="/api/test",status="418"} 1`) {
		t.Fatalf("延迟直方图缺少 status 标签，输出:\n%s", out)
	}
}

func TestConfigEnvSwitches(t *testing.T) {
	t.Setenv("PROMETHEUS_ENABLED", "")
	if !Enabled() {
		t.Fatal("默认应启用")
	}
	t.Setenv("PROMETHEUS_ENABLED", "false")
	if Enabled() {
		t.Fatal("PROMETHEUS_ENABLED=false 应禁用")
	}
	t.Setenv("PROMETHEUS_ENABLED", "0")
	if Enabled() {
		t.Fatal("PROMETHEUS_ENABLED=0 应禁用")
	}

	t.Setenv("PROMETHEUS_TOKEN", "tok123")
	if Token() != "tok123" {
		t.Fatal("token 读取失败")
	}
	t.Setenv("PROMETHEUS_LOCALHOST_ONLY", "true")
	if !LocalhostOnly() {
		t.Fatal("localhost-only 读取失败")
	}
}
