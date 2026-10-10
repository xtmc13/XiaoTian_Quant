package strategy

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── H4（2026-10-10）：引擎 dispatch 的暖机路由 ──
//
// 实现 Warmupper 的策略：收盘时刻早于本次 Start 时刻的历史回补 bar 路由进
// OnWarmupBar（只养指标），不产信号、不驱动 universe/adjust 钩子；其余 bar
// 照常 OnBar。未实现 Warmupper 的策略全部走 OnBar（兼容，零行为变化）。

// warmFake 实现 Warmupper 的测试替身：分别记录暖机/实时 bar，实时 bar 发信号。
type warmFake struct {
	BaseStrategy
	name       string
	running    bool
	warmupSeen []model.Bar
	liveSeen   []model.Bar
}

func (f *warmFake) Name() string               { return f.name }
func (f *warmFake) Symbol() string             { return "BTCUSDT" }
func (f *warmFake) Params() map[string]any     { return map[string]any{} }
func (f *warmFake) Start(map[string]any) error { f.running = true; return nil }
func (f *warmFake) Stop() error                { f.running = false; return nil }
func (f *warmFake) IsRunning() bool            { return f.running }
func (f *warmFake) OnTick(model.Tick, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (f *warmFake) OnOrderBook(model.OrderBookData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (f *warmFake) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	f.liveSeen = append(f.liveSeen, bar)
	return &model.Signal{Symbol: "BTCUSDT", Direction: "LONG", Strategy: f.name, Timestamp: time.Now().UnixMilli()}, nil
}
func (f *warmFake) OnOrderUpdate(model.OrderData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (f *warmFake) OnWarmupBar(bar model.Bar) { f.warmupSeen = append(f.warmupSeen, bar) }

func TestEngineRoutesHistoricalBarsToWarmup(t *testing.T) {
	e := newTestEngine()
	defer e.bus.Close()
	var signals []model.Signal
	e.OnSignal = func(sig model.Signal) { signals = append(signals, sig) }

	s := &warmFake{name: "warm1"}
	if err := e.Register(WrapStrategy("cfg-warm1", s)); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("cfg-warm1", nil); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Unregister("cfg-warm1") }()

	now := time.Now().UnixMilli()
	pub := func(bar model.Bar) {
		e.bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "BTCUSDT", Data: bar})
	}

	// 历史回补：2 小时前开盘的 15m bar（收盘远早于启动）→ 暖机，无信号。
	pub(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Time: now - 2*3600*1000, Close: 100})
	if len(s.warmupSeen) != 1 || len(s.liveSeen) != 0 {
		t.Fatalf("historical bar must route to warmup: warm=%v live=%v", s.warmupSeen, s.liveSeen)
	}
	if len(signals) != 0 {
		t.Fatalf("warmup bar must not emit signals: %v", signals)
	}

	// 跨窗口 bar：开盘早于启动、收盘晚于启动（策略启动时该 bar 尚未闭合）
	// → 实时 bar，正常交易（判断口径是收盘时刻，不是开盘时刻）。
	pub(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Time: now - 5*60*1000, Close: 101})
	if len(s.liveSeen) != 1 || len(s.warmupSeen) != 1 {
		t.Fatalf("bar closing after start must be live: warm=%d live=%d", len(s.warmupSeen), len(s.liveSeen))
	}
	if len(signals) != 1 {
		t.Fatalf("live bar must emit signal, got %d", len(signals))
	}

	// 启动前 20 分钟开盘的 15m bar：收盘（启动前 5 分钟）早于启动 → 暖机。
	pub(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Time: now - 20*60*1000, Close: 99})
	if len(s.warmupSeen) != 2 || len(s.liveSeen) != 1 {
		t.Fatalf("bar closed before start must be warmup: warm=%d live=%d", len(s.warmupSeen), len(s.liveSeen))
	}

	// 无周期信息的 bar：退化为开盘时刻判定；当前时刻开盘 → 实时。
	pub(model.Bar{Symbol: "BTCUSDT", Interval: "", Time: now, Close: 102})
	if len(s.liveSeen) != 2 {
		t.Fatalf("bar without interval at now must be live: live=%d", len(s.liveSeen))
	}
}

func TestEngineWarmupCompatForPlainStrategies(t *testing.T) {
	e := newTestEngine()
	defer e.bus.Close()

	// 未实现 Warmupper 的策略：历史 bar 照常走 OnBar（零行为变化）。
	s := &mtStrategy{name: "plain1", symbol: "BTCUSDT", tf: "15m"}
	if err := e.Register(WrapStrategy("cfg-plain1", s)); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("cfg-plain1", nil); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Unregister("cfg-plain1") }()

	old := model.Bar{Symbol: "BTCUSDT", Interval: "15m", Time: time.Now().UnixMilli() - 2*3600*1000, Close: 100}
	e.bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "BTCUSDT", Data: old})
	if len(s.barsSeen) != 1 {
		t.Fatalf("strategy without Warmupper must receive all bars via OnBar, got %d", len(s.barsSeen))
	}
}

func TestIsWarmupBarEdgeCases(t *testing.T) {
	startedAt := int64(1_700_000_000_000)
	// 未记录启动时刻（老引擎实例/直调）：一律实时，不改变既有行为。
	if isWarmupBar(model.Bar{Interval: "15m", Time: 1000}, 0) {
		t.Fatal("no startedAt → never warmup")
	}
	// 无时间戳的合成 bar（测试/旧数据源）：无法证明是历史，按实时处理。
	if isWarmupBar(model.Bar{Interval: "15m", Time: 0}, startedAt) {
		t.Fatal("zero-time bar must not be classified warmup")
	}
	// 收盘恰好在启动时刻边界：不早于启动 → 实时。
	if isWarmupBar(model.Bar{Interval: "15m", Time: startedAt - 15*60*1000}, startedAt) {
		t.Fatal("bar closing exactly at start must be live")
	}
	// 不可解析周期：回退开盘时刻判定。
	if !isWarmupBar(model.Bar{Interval: "bogus", Time: startedAt - 1000}, startedAt) {
		t.Fatal("unknown interval falls back to open-time comparison")
	}
}
