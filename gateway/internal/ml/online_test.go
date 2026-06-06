package ml

import (
	"context"
	"testing"
	"time"
)

func TestOnlineLearnerConfig(t *testing.T) {
	cfg := DefaultOnlineConfig()
	if cfg.Symbol != "BTCUSDT" {
		t.Errorf("expected BTCUSDT, got %s", cfg.Symbol)
	}
	if cfg.RetrainEvery != 24*time.Hour {
		t.Errorf("expected 24h retrain, got %v", cfg.RetrainEvery)
	}
	if cfg.WindowDays != 90 {
		t.Errorf("expected 90d window, got %d", cfg.WindowDays)
	}
	if !cfg.Enabled {
		t.Error("expected enabled by default")
	}
}

func TestOnlineLearnerLifecycle(t *testing.T) {
	// Use nil client/downloader — Start() will fail retrain but lifecycle works
	cfg := OnlineConfig{
		ModelID:      "test-model",
		Symbol:       "BTCUSDT",
		Interval:     "1h",
		RetrainEvery: 100 * time.Millisecond,
		WindowDays:   7,
		Enabled:      true,
	}
	ol := NewOnlineLearner(cfg, nil, nil)

	if ol.IsRunning() {
		t.Error("should not be running before Start")
	}

	ctx, cancel := context.WithCancel(context.Background())
	ol.Start(ctx)

	if !ol.IsRunning() {
		t.Error("should be running after Start")
	}

	// Wait a bit for the retrain goroutine to attempt
	time.Sleep(50 * time.Millisecond)

	ol.Stop()
	cancel()

	if ol.IsRunning() {
		t.Error("should not be running after Stop")
	}
}

func TestOnlineLearnerCallbacks(t *testing.T) {
	cfg := OnlineConfig{
		ModelID:      "test-callback",
		Symbol:       "BTCUSDT",
		Interval:     "1h",
		RetrainEvery: 1 * time.Hour,
		WindowDays:   7,
	}
	ol := NewOnlineLearner(cfg, nil, nil)

	errCalled := false
	ol.OnError = func(err error) {
		errCalled = true
	}

	// Trigger retrain with nil pipeline — should call OnError
	ol.TriggerRetrain()
	time.Sleep(100 * time.Millisecond)

	if !errCalled {
		t.Error("expected OnError to be called with nil pipeline")
	}
}

func TestOnlineManager(t *testing.T) {
	om := NewOnlineManager()

	cfg1 := OnlineConfig{ModelID: "model-a", Symbol: "BTCUSDT", Interval: "1h"}
	cfg2 := OnlineConfig{ModelID: "model-b", Symbol: "ETHUSDT", Interval: "1h"}

	ol1 := om.Register(cfg1, nil, nil)
	ol2 := om.Register(cfg2, nil, nil)

	if ol1 == nil || ol2 == nil {
		t.Fatal("register should return learners")
	}

	ids := om.List()
	if len(ids) != 2 {
		t.Errorf("expected 2 models, got %d", len(ids))
	}

	if om.Get("model-a") == nil {
		t.Error("model-a should exist")
	}
	if om.Get("nonexistent") != nil {
		t.Error("nonexistent should be nil")
	}

	// StartAll / StopAll lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	om.StartAll(ctx)
	time.Sleep(50 * time.Millisecond)
	om.StopAll()
	cancel()
}

func TestOnlineManagerAutoModelID(t *testing.T) {
	om := NewOnlineManager()
	cfg := OnlineConfig{Symbol: "BTCUSDT", Interval: "15m"} // no ModelID
	ol := om.Register(cfg, nil, nil)
	if ol == nil {
		t.Fatal("register should return learner")
	}
	if ol.cfg.ModelID == "" {
		t.Error("ModelID should be auto-generated")
	}
}

func TestOnlineLearnerLastResult(t *testing.T) {
	cfg := OnlineConfig{ModelID: "test-last", Symbol: "BTCUSDT", Interval: "1h"}
	ol := NewOnlineLearner(cfg, nil, nil)

	if ol.LastResult() != nil {
		t.Error("last result should be nil before training")
	}
	if ol.LastModelID() != "" {
		t.Error("last model ID should be empty before training")
	}
}
