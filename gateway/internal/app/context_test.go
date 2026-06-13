package app

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/config"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func assertTrue(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

/* ── Context Lifecycle Tests ─────────────────────────────────── */

func TestGetSingleton(t *testing.T) {
	ctx1 := Get()
	ctx2 := Get()

	assertTrue(t, ctx1 != nil, "Get() should return non-nil")
	assertTrue(t, ctx1 == ctx2, "Get() should return same instance")
}

func TestContextInitWithDefaults(t *testing.T) {
	ctx := Get()
	cfg := config.Default()

	err := ctx.Init(cfg)
	assertTrue(t, err == nil, "Init with default config should succeed")
	assertTrue(t, ctx.Config == cfg, "Config should be set")
}

func TestContextDoubleInit(t *testing.T) {
	ctx := Get()
	cfg := config.Default()

	// Reset for test
	ctx.Shutdown()

	err := ctx.Init(cfg)
	assertTrue(t, err == nil, "first Init should succeed")

	// Second init should be no-op (already started)
	err = ctx.Init(cfg)
	assertTrue(t, err == nil, "second Init should be no-op")
}

func TestContextShutdown(t *testing.T) {
	ctx := Get()
	cfg := config.Default()

	ctx.Shutdown()

	err := ctx.Init(cfg)
	assertTrue(t, err == nil, "Init after shutdown should succeed")

	ctx.Shutdown()
	assertTrue(t, !ctx.started, "should not be started after shutdown")
}

func TestContextHealth(t *testing.T) {
	ctx := Get()
	cfg := config.Default()

	ctx.Shutdown()
	err := ctx.Init(cfg)
	assertTrue(t, err == nil, "Init should succeed")

	// Context should be marked as started
	assertTrue(t, ctx.started, "context should be started")
}

func TestContextComponents(t *testing.T) {
	ctx := Get()
	cfg := config.Default()

	ctx.Shutdown()
	err := ctx.Init(cfg)
	assertTrue(t, err == nil, "Init should succeed")

	// Core components should be initialized
	assertTrue(t, ctx.EventBus != nil, "EventBus should be initialized")
	assertTrue(t, ctx.Logger != nil, "Logger should be initialized")
	assertTrue(t, ctx.RiskManager != nil, "RiskManager should be initialized")
	assertTrue(t, ctx.Cache != nil, "Cache should be initialized")
}
