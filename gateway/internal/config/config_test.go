package config

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func assertEq(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func assertTrue(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

/* ── Default Config Tests ────────────────────────────────────── */

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	assertTrue(t, cfg != nil, "default config should not be nil")
	assertEq(t, cfg.Server.Port, "8080")
	assertEq(t, cfg.Server.Mode, "release")
	assertEq(t, strings.ToLower(cfg.Server.LogLevel), "info")
	assertEq(t, cfg.Server.LogFormat, "text")

	// Risk defaults
	assertTrue(t, cfg.Risk.MaxOrderSize > 0, "max_order_size should be positive")
	assertTrue(t, cfg.Risk.MaxPositions > 0, "max_positions should be positive")
	assertTrue(t, cfg.Risk.MaxDrawdown > 0, "max_drawdown should be positive")

	// Portfolio defaults
	assertTrue(t, cfg.Portfolio.InitialBalance >= 0, "initial_balance should be non-negative")

	// Strategy defaults
	assertTrue(t, cfg.Strategy.MaxStrategies > 0, "max_strategies should be positive")

	// Backtest defaults
	assertTrue(t, cfg.Backtest.DefaultCommission >= 0, "commission should be non-negative")
	assertTrue(t, cfg.Backtest.DefaultSlippage >= 0, "slippage should be non-negative")
}

func TestLoadConfigFromEnv(t *testing.T) {
	// Set env vars
	os.Setenv("PORT", "9090")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("MAX_POSITIONS", "20")
	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("MAX_POSITIONS")
	}()

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	assertEq(t, cfg.Server.Port, "9090")
	assertEq(t, cfg.Server.LogLevel, "debug")
	assertTrue(t, cfg.Risk.MaxPositions == 20, "max_positions should be 20")
}

func TestConfigValidation(t *testing.T) {
	cfg := Default()

	// Valid config should pass
	err := cfg.Validate()
	assertTrue(t, err == nil, "default config should be valid")

	// Invalid port should fail
	cfg.Server.Port = ""
	err = cfg.Validate()
	assertTrue(t, err != nil, "empty port should be invalid")
}

func TestExchangeCredsMasking(t *testing.T) {
	cfg := Default()
	cfg.Exchange.Binance.APIKey = "secret_key_123"
	cfg.Exchange.Binance.APISecret = "secret_value_456"

	masked := cfg.MaskedString()
	assertTrue(t, !contains(masked, "secret_key_123"), "API key should be masked")
	assertTrue(t, !contains(masked, "secret_value_456"), "API secret should be masked")
	assertTrue(t, contains(masked, "***"), "mask should contain ***")
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

/* ── Risk 启动校验测试（C2.1 硬校验）────────────────────────── */

func TestRiskWarnings(t *testing.T) {
	cfg := Default()
	assertTrue(t, len(cfg.RiskWarnings()) == 0, "default position_limit_pct=50 应无告警")

	cfg.Risk.PositionLimit = 100
	assertTrue(t, len(cfg.RiskWarnings()) == 0, "position_limit_pct=100 是安全边界，应无告警")

	cfg.Risk.PositionLimit = 2500 // HANDOFF 红线事故值
	warns := cfg.RiskWarnings()
	assertTrue(t, len(warns) == 1, "position_limit_pct=2500 必须告警")
	assertTrue(t, contains(warns[0], "非实盘安全值"), "告警须提示非实盘安全值")
	assertTrue(t, contains(warns[0], "XIAOTIAN_ALLOW_RELAXED_RISK"), "告警须提示逃逸开关")
}

// TestLoadWarnsOnRelaxedPositionLimit：Load 加载含放宽值的 config 时打 WARN
// 但不失败、不改写原始值（实盘解锁硬闸依赖原始值识别"未回调"）。
func TestLoadWarnsOnRelaxedPositionLimit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.yaml")
	err := os.WriteFile(p, []byte("risk:\n  position_limit_pct: 2500\n"), 0o644)
	assertTrue(t, err == nil, "write temp config")

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	cfg, err := Load(p)
	assertTrue(t, err == nil, "Load must not fail on relaxed value")
	assertTrue(t, cfg.Risk.PositionLimit == 2500, "Load 必须保留原始值（不做钳制）")
	assertTrue(t, contains(buf.String(), "非实盘安全值"), "启动日志必须出现非实盘安全值告警")

	t.Cleanup(func() { _, _ = Load("") })
}
