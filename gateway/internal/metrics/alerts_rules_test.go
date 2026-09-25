package metrics

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// promtool 风格静态自检：校验 ops/prometheus/alerts.yml 的结构与指标引用。
// 本机/CI 无 promtool 时，本测试即规则文件的静态防线（结构 + for/runbook
// 注解完整性 + 指标名拼写防漂移——表达式中出现的指标必须在本包真实暴露）。

type alertRulesFile struct {
	Groups []struct {
		Name  string `yaml:"name"`
		Rules []struct {
			Alert       string            `yaml:"alert"`
			Expr        string            `yaml:"expr"`
			For         string            `yaml:"for"`
			Labels      map[string]string `yaml:"labels"`
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"rules"`
	} `yaml:"groups"`
}

// knownAlertMetrics 是告警规则允许引用的指标全集（本包注册 + runtime + up 探针）。
var knownAlertMetrics = map[string]bool{
	"up": true,
	// HTTP 中间件
	"http_requests_total":                  true,
	"http_request_duration_seconds_bucket": true,
	// Go runtime（runtimeMetrics）
	"go_goroutines":                    true,
	"process_uptime_seconds":           true,
	"process_start_time_seconds":       true,
	"go_gc_cycles_total":               true,
	"go_memstats_alloc_bytes":          true,
	"go_memstats_sys_bytes":            true,
	"go_memstats_heap_alloc_bytes":     true,
	"go_memstats_heap_sys_bytes":       true,
	"go_memstats_heap_idle_bytes":      true,
	"go_memstats_heap_inuse_bytes":     true,
	"go_memstats_heap_released_bytes":  true,
	"go_memstats_heap_objects":         true,
	"go_memstats_gc_cpu_fraction":      true,
	"go_memstats_last_gc_time_seconds": true,
	// 业务便捷指标
	"orders_total":          true,
	"signals_total":         true,
	"portfolio_equity_usdt": true,
	"portfolio_positions":   true,
	"strategies_active":     true,
	// A9.1 业务指标
	"fills_total":                 true,
	"risk_order_rejections_total": true,
	"notify_sends_total":          true,
	"ws_connections":              true,
	"ws_disconnects_total":        true,
	"bots_running":                true,
	"reconcile_diffs_total":       true,
	// 磁盘采样（diskMetrics）
	"gateway_disk_total_bytes": true,
	"gateway_disk_avail_bytes": true,
}

var metricTokenRe = regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`)

func TestAlertRulesFile(t *testing.T) {
	path := filepath.Join("..", "..", "..", "ops", "prometheus", "alerts.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("alerts.yml 不在仓库预期位置（%v），跳过静态自检", err)
	}

	var f alertRulesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		t.Fatalf("alerts.yml YAML 解析失败: %v", err)
	}
	if len(f.Groups) == 0 {
		t.Fatal("alerts.yml 缺少 groups")
	}

	required := map[string]bool{
		"GatewayDown":                   false, // 进程 down
		"GatewayHighErrorRate":          false, // HTTP 5xx
		"GatewayHighLatencyP95":         false, // p95 延迟
		"GatewayOrderFailureRate":       false, // 下单失败率
		"GatewayWebSocketChurn":         false, // WS 断连
		"GatewayPositionMismatch":       false, // paper/live 持仓不一致
		"GatewayDiskLow":                false, // 磁盘
		"GatewayHighMemory":             false, // 内存
		"GatewayRiskRejectionsBurst":    false,
		"GatewayNotifyDeliveryFailures": false,
	}

	ruleCount := 0
	for _, g := range f.Groups {
		if g.Name == "" {
			t.Error("存在无名 group")
		}
		for _, r := range g.Rules {
			ruleCount++
			if r.Alert == "" {
				t.Errorf("%s: 存在无名 rule", g.Name)
				continue
			}
			if _, ok := required[r.Alert]; ok {
				required[r.Alert] = true
			}
			if strings.TrimSpace(r.Expr) == "" {
				t.Errorf("%s: 缺少 expr", r.Alert)
			}
			if r.For == "" {
				t.Errorf("%s: 缺少 for（每条规则必须带 for）", r.Alert)
			}
			for _, key := range []string{"summary", "description", "runbook"} {
				if r.Annotations[key] == "" {
					t.Errorf("%s: 缺少 annotations.%s", r.Alert, key)
				}
			}
			switch r.Labels["severity"] {
			case "critical", "warning", "info":
			default:
				t.Errorf("%s: severity 非法 %q", r.Alert, r.Labels["severity"])
			}
			// 指标名防漂移：expr 中指标形态的 token 必须在已知集合内
			seen := map[string]bool{}
			for _, tok := range metricTokenRe.FindAllString(r.Expr, -1) {
				if seen[tok] {
					continue
				}
				seen[tok] = true
				if knownAlertMetrics[tok] {
					continue
				}
				// 只对指标形态后缀的 token 强校验（函数/标签名不含这些后缀）
				for _, suffix := range []string{"_total", "_bytes", "_seconds", "_bucket"} {
					if strings.HasSuffix(tok, suffix) {
						t.Errorf("%s: expr 引用了未知指标 %q（若为新指标请先加入 knownAlertMetrics）", r.Alert, tok)
					}
				}
			}
		}
	}
	for name, found := range required {
		if !found {
			t.Errorf("缺少必需告警规则 %s", name)
		}
	}
	t.Logf("alerts.yml 静态自检通过：%d 条规则", ruleCount)
}
