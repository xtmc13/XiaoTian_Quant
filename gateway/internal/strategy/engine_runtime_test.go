package strategy

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// runtimeFake implements Strategy + RuntimeStatus for engine-level tests.
type runtimeFake struct {
	BaseStrategy
	name    string
	running bool
	status  map[string]any
}

func (f *runtimeFake) Name() string                  { return f.name }
func (f *runtimeFake) Symbol() string                { return "BTCUSDT" }
func (f *runtimeFake) Params() map[string]any        { return map[string]any{} }
func (f *runtimeFake) Start(map[string]any) error    { f.running = true; return nil }
func (f *runtimeFake) Stop() error                   { f.running = false; return nil }
func (f *runtimeFake) IsRunning() bool               { return f.running }
func (f *runtimeFake) RuntimeStatus() map[string]any { return f.status }
func (f *runtimeFake) OnTick(model.Tick, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (f *runtimeFake) OnOrderBook(model.OrderBookData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (f *runtimeFake) OnBar(model.Bar, *event.EventBus) (*model.Signal, error) { return nil, nil }
func (f *runtimeFake) OnOrderUpdate(model.OrderData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

// TestEngineRuntimeStatus：引擎按名字查运行时状态；支持接口断言才返回，
// 未注册/不支持均返回 false（P0-1 引擎层路径）。
func TestEngineRuntimeStatus(t *testing.T) {
	eng := GetEngine(event.NewEventBus(64, 1))

	rs := &runtimeFake{name: "rt_engine_test", status: map[string]any{"running": true}}
	if err := eng.Register(rs); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer func() { _ = eng.Unregister("rt_engine_test") }()

	st, ok := eng.RuntimeStatus("rt_engine_test")
	if !ok {
		t.Fatal("RuntimeStatus should be supported for registered runtimeFake")
	}
	if st["running"] != true {
		t.Errorf("status[running] = %v, want true", st["running"])
	}

	if _, ok := eng.RuntimeStatus("rt_engine_missing"); ok {
		t.Fatal("RuntimeStatus must return false for unknown strategy")
	}
}
