package config

import (
	"os"
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
	cfg := DefaultConfig()

	assertTrue(t, cfg != nil, "default config should not be nil")
	assertEq(t, cfg.Server.Port, "8080")
	assertEq(t, cfg.Server.Mode, "release")
	assertEq(t, cfg.Server.LogLevel, "info")
	assertEq(t, cfg.Server.LogFormat, "json")

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
	os.Setenv("XT_SERVER_PORT", "9090")
	os.Setenv("XT_SERVER_MODE", "debug")
	os.Setenv("XT_RISK_MAX_POSITIONS", "20")
	defer func() {
		os.Unsetenv("XT_SERVER_PORT")
		os.Unsetenv("XT_SERVER_MODE")
		os.Unsetenv("XT_RISK_MAX_POSITIONS")
	}()

	cfg := DefaultConfig()
	LoadFromEnv(cfg)

	assertEq(t, cfg.Server.Port, "9090")
	assertEq(t, cfg.Server.Mode, "debug")
	assertTrue(t, cfg.Risk.MaxPositions == 20, "max_positions should be 20")
}

func TestConfigValidation(t *testing.T) {
	cfg := DefaultConfig()

	// Valid config should pass
	err := cfg.Validate()
	assertTrue(t, err == nil, "default config should be valid")

	// Invalid port should fail
	cfg.Server.Port = ""
	err = cfg.Validate()
	assertTrue(t, err != nil, "empty port should be invalid")
}

func TestExchangeCredsMasking(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Exchange.Binance.APIKey = "secret_key_123"
	cfg.Exchange.Binance.APISecret = "secret_value_456"

	masked := cfg.MaskedString()
	assertTrue(t, !contains(masked, "secret_key_123"), "API key should be masked")
	assertTrue(t, !contains(masked, "secret_value_456"), "API secret should be masked")
	assertTrue(t, contains(masked, "***"), "mask should contain ***")
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr))
}
