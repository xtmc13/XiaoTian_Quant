package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// ── strategyProtectionManagerFromConfig ──

func TestStrategyProtectionManagerFromConfig(t *testing.T) {
	// 无 config_json / 无 protections → (nil, nil)
	if mgr, err := strategyProtectionManagerFromConfig(map[string]any{}); mgr != nil || err != nil {
		t.Fatalf("empty item: mgr=%v err=%v", mgr, err)
	}
	if mgr, err := strategyProtectionManagerFromConfig(map[string]any{
		"config_json": `{"symbol":"BTCUSDT"}`,
	}); mgr != nil || err != nil {
		t.Fatalf("no protections: mgr=%v err=%v", mgr, err)
	}

	// 合法 protections → manager 建成
	mgr, err := strategyProtectionManagerFromConfig(map[string]any{
		"config_json": `{"protections":[{"name":"MaxDrawdown","params":{"max_drawdown_pct":0.05}}]}`,
	})
	if err != nil || mgr == nil || len(mgr.Protections()) != 1 {
		t.Fatalf("valid protections: mgr=%v err=%v", mgr, err)
	}

	// 未知 protection 名 → 明确错误
	if _, err := strategyProtectionManagerFromConfig(map[string]any{
		"config_json": `{"protections":[{"name":"Nope"}]}`,
	}); err == nil || !strings.Contains(err.Error(), "protections") {
		t.Fatalf("unknown protection should error, got: %v", err)
	}

	// 结构非法（protections 不是 [{name,params}] 数组）→ 明确错误
	if _, err := strategyProtectionManagerFromConfig(map[string]any{
		"config_json": `{"protections":"CooldownPeriod"}`,
	}); err == nil {
		t.Fatalf("malformed protections should error")
	}
	// config_json 本身非法 JSON → 明确错误
	if _, err := strategyProtectionManagerFromConfig(map[string]any{
		"config_json": `{bad`,
	}); err == nil {
		t.Fatalf("bad config_json should error")
	}
}

// ── validateStrategyProtections（Create/Update 写库前校验）──

func TestValidateStrategyProtections(t *testing.T) {
	if err := validateStrategyProtections(map[string]any{}); err != nil {
		t.Fatalf("no protections should pass: %v", err)
	}
	if err := validateStrategyProtections(map[string]any{
		"protections": []any{map[string]any{"name": "CooldownPeriod", "params": map[string]any{"stop_duration_candles": 2}}},
	}); err != nil {
		t.Fatalf("valid protections should pass: %v", err)
	}
	if err := validateStrategyProtections(map[string]any{
		"protections": []any{map[string]any{"name": "NotAProtection"}},
	}); err == nil {
		t.Fatalf("unknown protection name must be rejected")
	}
}

// protSpotItem 构造带 protections 的完整 cra_spot 配置（复用现货配置模板）。
func protSpotItem(t *testing.T, id string, protections []any) map[string]any {
	t.Helper()
	cfg := buildSpotConfig(40000, 50000, 5)
	cfg["symbol"] = "BTCUSDT"
	if protections != nil {
		cfg["protections"] = protections
	}
	cj, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return map[string]any{
		"id": id, "strategy_type": "cra_spot", "symbol": "BTCUSDT",
		"timeframe": "15m", "config_json": string(cj),
	}
}

// ── 启动/停止生命周期：策略级 manager 装配与释放 ──

func TestStrategyProtectionStartStopLifecycle(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("cra_spot", func() strategy.Strategy {
		return cra.NewCRASpotStrategy("cra_spot", "BTCUSDT")
	})

	id := "prot-lifecycle-test"
	item := protSpotItem(t, id, []any{
		map[string]any{"name": "MaxDrawdown", "params": map[string]any{"max_drawdown_pct": 0.05}},
		map[string]any{"name": "CooldownPeriod", "params": map[string]any{"stop_duration_candles": 3}},
	})
	store.SetStrategyConfig(id, item)
	t.Cleanup(func() {
		stopStrategyInEngine(id)
		store.DeleteStrategyConfig(id)
	})

	if err := startStrategyInEngine(id, item); err != nil {
		t.Fatalf("start with protections: %v", err)
	}
	if n := eng.StrategyProtectionCount(id); n != 2 {
		t.Fatalf("strategy protections after start: %d, want 2", n)
	}

	// 停止释放
	stopStrategyInEngine(id)
	if n := eng.StrategyProtectionCount(id); n != 0 {
		t.Fatalf("strategy protections after stop: %d, want 0", n)
	}

	// 配置非法：拒绝启动
	badID := "prot-invalid-test"
	badItem := protSpotItem(t, badID, []any{map[string]any{"name": "Nope"}})
	store.SetStrategyConfig(badID, badItem)
	t.Cleanup(func() { store.DeleteStrategyConfig(badID) })
	if err := startStrategyInEngine(badID, badItem); err == nil || !strings.Contains(err.Error(), "protections") {
		t.Fatalf("invalid protections must refuse to start, got: %v", err)
	}
	if eng.Get(badID) != nil {
		t.Fatalf("strategy with invalid protections must not be registered")
	}
}

// ── 热重载：配置更新后运行中策略的 manager 重建/清除 ──

func TestRefreshStrategyProtection(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("cra_spot", func() strategy.Strategy {
		return cra.NewCRASpotStrategy("cra_spot", "BTCUSDT")
	})

	id := "prot-reload-test"
	item := protSpotItem(t, id, []any{
		map[string]any{"name": "MaxDrawdown", "params": map[string]any{"max_drawdown_pct": 0.05}},
	})
	store.SetStrategyConfig(id, item)
	t.Cleanup(func() {
		stopStrategyInEngine(id)
		store.DeleteStrategyConfig(id)
	})
	if err := startStrategyInEngine(id, item); err != nil {
		t.Fatalf("start: %v", err)
	}
	if n := eng.StrategyProtectionCount(id); n != 1 {
		t.Fatalf("initial protections: %d", n)
	}

	// 更新为两条 → 热重载生效
	two := protSpotItem(t, id, []any{
		map[string]any{"name": "MaxDrawdown", "params": map[string]any{"max_drawdown_pct": 0.05}},
		map[string]any{"name": "CooldownPeriod", "params": map[string]any{}},
	})
	refreshStrategyProtection(id, two)
	if n := eng.StrategyProtectionCount(id); n != 2 {
		t.Fatalf("reloaded protections: %d, want 2", n)
	}

	// 移除 protections → 清除
	none := protSpotItem(t, id, nil)
	refreshStrategyProtection(id, none)
	if n := eng.StrategyProtectionCount(id); n != 0 {
		t.Fatalf("cleared protections: %d, want 0", n)
	}

	// 未运行的策略：refresh 不动作也不 panic
	refreshStrategyProtection("never-started-id", none)
}

// ── 列表/详情 API 透出 protections ──

func TestStrategyProtectionsInAPIView(t *testing.T) {
	item := map[string]any{
		"id":            "prot-view-test",
		"strategy_type": "cra_spot",
		"config_json":   `{"symbol":"BTCUSDT","protections":[{"name":"CooldownPeriod","params":{"stop_duration_candles":5}}]}`,
	}
	// 模拟 GetStrategyConfigs 的预处理：config_json 解析进 config
	var cfg map[string]any
	if err := json.Unmarshal([]byte(item["config_json"].(string)), &cfg); err != nil {
		t.Fatalf("setup: %v", err)
	}
	item["config"] = cfg

	out := normalizeStrategyConfig(item)
	prot, ok := out["protections"].([]any)
	if !ok || len(prot) != 1 {
		t.Fatalf("protections must surface in normalized config: %+v", out)
	}
	first, _ := prot[0].(map[string]any)
	if first["name"] != "CooldownPeriod" {
		t.Fatalf("protection entry: %+v", first)
	}

	// 无 protections 的策略不出现该字段
	out2 := normalizeStrategyConfig(map[string]any{"id": "x", "config_json": `{"symbol":"BTCUSDT"}`})
	if _, exists := out2["protections"]; exists {
		t.Fatalf("protections key must be absent when not configured: %+v", out2)
	}
}
