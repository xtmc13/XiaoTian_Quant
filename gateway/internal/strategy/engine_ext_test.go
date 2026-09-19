package strategy

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
)

func TestParseScheduleEvery(t *testing.T) {
	spec, err := ParseSchedule("@every 4h")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != "every" || spec.Every != 4*time.Hour {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	next := spec.NextAfter(time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC))
	if next.Hour() != 14 || next.Minute() != 0 {
		t.Fatalf("unexpected next: %s", next)
	}
}

func TestParseScheduleDaily(t *testing.T) {
	spec, err := ParseSchedule("daily@14:30")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != "daily" || spec.Hour != 14 || spec.Minute != 30 {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	base := time.Date(2026, 9, 19, 14, 30, 0, 0, time.UTC)
	next := spec.NextAfter(base)
	if next.Day() != 20 || next.Hour() != 14 || next.Minute() != 30 {
		t.Fatalf("next should be next day 14:30, got %s", next)
	}
	// 14:29 → 当天 14:30
	early := time.Date(2026, 9, 19, 14, 29, 0, 0, time.UTC)
	next = spec.NextAfter(early)
	if next.Day() != 19 {
		t.Fatalf("next should be same day, got %s", next)
	}
}

func TestParseScheduleWeekly(t *testing.T) {
	spec, err := ParseSchedule("weekly@mon@09:00")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != "weekly" || spec.Weekday != 1 || spec.Hour != 9 {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	// 2026-09-19 是周六 → 下周一 09-21 09:00
	next := spec.NextAfter(time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC))
	if next.Weekday() != time.Monday || next.Day() != 21 || next.Hour() != 9 {
		t.Fatalf("unexpected next: %s", next)
	}
	// 周一当天 08:00 → 当天 09:00
	next = spec.NextAfter(time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC))
	if next.Day() != 21 || next.Hour() != 9 {
		t.Fatalf("unexpected next: %s", next)
	}
}

func TestParseScheduleCompatAliases(t *testing.T) {
	if _, err := ParseSchedule("@daily"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSchedule("@weekly"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "daily", "daily@25:00", "weekly@foo", "@every", "@every 10s"} {
		if _, err := ParseSchedule(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

// fakeFeeder 记录 Ensure/Release 调用（线程安全接口的测试替身）。
type fakeFeeder struct{ ensured, released []string }

func (f *fakeFeeder) EnsureSymbol(symbol, interval string) bool {
	key := symbol + "|" + interval
	for _, k := range f.ensured {
		if k == key {
			return false
		}
	}
	f.ensured = append(f.ensured, key)
	return true
}
func (f *fakeFeeder) ReleaseSymbol(symbol, interval string) {
	f.released = append(f.released, symbol+"|"+interval)
}

// mtStrategy 实现多周期 + 主周期过滤的策略测试替身。
type mtStrategy struct {
	BaseStrategy
	name     string
	symbol   string
	tf       string
	extraTF  []string
	running  bool
	provider BarProvider
	barsSeen []model.Bar
}

func (s *mtStrategy) Name() string               { return s.name }
func (s *mtStrategy) Symbol() string             { return s.symbol }
func (s *mtStrategy) Params() map[string]any     { return map[string]any{} }
func (s *mtStrategy) Start(map[string]any) error { s.running = true; return nil }
func (s *mtStrategy) Stop() error                { s.running = false; return nil }
func (s *mtStrategy) IsRunning() bool            { return s.running }
func (s *mtStrategy) OnTick(model.Tick, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *mtStrategy) OnOrderBook(model.OrderBookData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *mtStrategy) OnOrderUpdate(model.OrderData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *mtStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.barsSeen = append(s.barsSeen, bar)
	return nil, nil
}
func (s *mtStrategy) Timeframes() []string         { return s.extraTF }
func (s *mtStrategy) PrimaryTimeframe() string     { return s.tf }
func (s *mtStrategy) SetBarProvider(p BarProvider) { s.provider = p }

func newTestEngine() *Engine {
	return &Engine{
		strategies: make(map[string]Strategy),
		symbolMap:  make(map[string][]string),
		subIDs:     make(map[string]event.SubscriptionID),
		bus:        event.NewEventBus(64, 4),
		feedHolds:  make(map[string][]feedHold),
		universes:  make(map[string]*universeState),
		scheduled:  make(map[string]*scheduledEntry),
	}
}

func TestEngineMultiTimeframeSetup(t *testing.T) {
	e := newTestEngine()
	defer e.bus.Close()
	f := &fakeFeeder{}
	e.SetKlineFeeder(f)

	// 生产路径注册的是 NamedStrategy 包装：可选接口必须能穿透包装层
	s := &mtStrategy{name: "mt1", symbol: "BTCUSDT", tf: "1h", extraTF: []string{"4h", "1d"}}
	if err := e.Register(WrapStrategy("cfg-mt1", s)); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("cfg-mt1", nil); err != nil {
		t.Fatal(err)
	}
	// 多周期 feeder 供给已建立（主周期 1h 不在其列）
	found4h, found1d := false, false
	for _, k := range f.ensured {
		if k == "BTCUSDT|4h" {
			found4h = true
		}
		if k == "BTCUSDT|1d" {
			found1d = true
		}
	}
	if !found4h || !found1d {
		t.Fatalf("expected 4h/1d feeds ensured, got %v", f.ensured)
	}
	if s.provider == nil {
		t.Fatal("expected BarProvider injected")
	}

	// 主周期过滤：4h bar 不应进入 OnBar；1h bar 应该进入
	bar4h := model.Bar{Symbol: "BTCUSDT", Interval: "4h", Time: 1}
	bar1h := model.Bar{Symbol: "BTCUSDT", Interval: "1h", Time: 2}
	e.bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "BTCUSDT", Data: bar4h})
	e.bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "BTCUSDT", Data: bar1h})
	if len(s.barsSeen) != 1 || s.barsSeen[0].Interval != "1h" {
		t.Fatalf("expected only 1h bar dispatched, got %v", s.barsSeen)
	}

	// MarketData 应同时存有 1h 与 4h
	if _, ok := e.MarketData().GetBar("BTCUSDT", "1h"); !ok {
		t.Fatal("expected 1h bar in MarketData")
	}
	if _, ok := e.MarketData().GetBar("BTCUSDT", "4h"); !ok {
		t.Fatal("expected 4h bar in MarketData")
	}

	// Unregister 释放 feeder 引用
	if err := e.Unregister("cfg-mt1"); err != nil {
		t.Fatal(err)
	}
	if len(f.released) != 2 {
		t.Fatalf("expected 2 feed releases, got %v", f.released)
	}
}

// uniStrategy 实现动态 universe 的策略测试替身。
type uniStrategy struct {
	BaseStrategy
	name       string
	symbol     string
	tf         string
	running    bool
	onUniCalls int
	want       []string
	barsSeen   map[string]int
}

func (s *uniStrategy) Name() string           { return s.name }
func (s *uniStrategy) Symbol() string         { return s.symbol }
func (s *uniStrategy) Params() map[string]any { return map[string]any{} }
func (s *uniStrategy) Start(map[string]any) error {
	s.running = true
	return nil
}
func (s *uniStrategy) Stop() error     { s.running = false; return nil }
func (s *uniStrategy) IsRunning() bool { return s.running }
func (s *uniStrategy) OnTick(model.Tick, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *uniStrategy) OnOrderBook(model.OrderBookData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *uniStrategy) OnOrderUpdate(model.OrderData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *uniStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.barsSeen[bar.Symbol]++
	return nil, nil
}
func (s *uniStrategy) PrimaryTimeframe() string { return s.tf }
func (s *uniStrategy) UniverseRefreshBars() int { return 2 }
func (s *uniStrategy) Watchlist() []string      { return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"} }
func (s *uniStrategy) OnUniverse(ctx UniverseContext) []string {
	s.onUniCalls++
	return s.want
}

func TestEngineDynamicUniverse(t *testing.T) {
	e := newTestEngine()
	defer e.bus.Close()
	f := &fakeFeeder{}
	e.SetKlineFeeder(f)

	s := &uniStrategy{
		name: "uni1", symbol: "BTCUSDT", tf: "1h",
		want:     []string{"BTCUSDT", "ETHUSDT"},
		barsSeen: map[string]int{},
	}
	if err := e.Register(WrapStrategy("cfg-uni1", s)); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("cfg-uni1", nil); err != nil {
		t.Fatal(err)
	}

	// watchlist 供给已建立（除主 symbol 外）
	if len(f.ensured) != 2 {
		t.Fatalf("expected watchlist feeds for ETH/SOL, got %v", f.ensured)
	}

	// 发 2 根主周期 K 线（refresh=2）→ 触发一次 OnUniverse
	for i := 0; i < 2; i++ {
		e.bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "BTCUSDT", Data: model.Bar{
			Symbol: "BTCUSDT", Interval: "1h", Time: int64(100 + i),
		}})
	}
	if s.onUniCalls != 1 {
		t.Fatalf("expected 1 OnUniverse call, got %d", s.onUniCalls)
	}

	// ETH 已加入 universe：ETH 的 K 线应派发到策略
	e.bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "ETHUSDT", Data: model.Bar{
		Symbol: "ETHUSDT", Interval: "1h", Time: 200,
	}})
	if s.barsSeen["ETHUSDT"] != 1 {
		t.Fatalf("expected ETH bar dispatched, got %v", s.barsSeen)
	}
	// SOL 不在 universe：K 线不派发
	e.bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "SOLUSDT", Data: model.Bar{
		Symbol: "SOLUSDT", Interval: "1h", Time: 201,
	}})
	if s.barsSeen["SOLUSDT"] != 0 {
		t.Fatal("SOL should not be dispatched")
	}

	// 换仓：universe 改为 BTC + SOL，再来 2 根 K 线
	s.want = []string{"BTCUSDT", "SOLUSDT"}
	for i := 0; i < 2; i++ {
		e.bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "BTCUSDT", Data: model.Bar{
			Symbol: "BTCUSDT", Interval: "1h", Time: int64(300 + i),
		}})
	}
	// ETH 已移除订阅、SOL 已加入
	e.bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "ETHUSDT", Data: model.Bar{
		Symbol: "ETHUSDT", Interval: "1h", Time: 400,
	}})
	e.bus.PublishSync(event.Event{Type: event.TypeBar, Symbol: "SOLUSDT", Data: model.Bar{
		Symbol: "SOLUSDT", Interval: "1h", Time: 401,
	}})
	if s.barsSeen["ETHUSDT"] != 1 {
		t.Fatalf("ETH should stop receiving after removal, got %d", s.barsSeen["ETHUSDT"])
	}
	if s.barsSeen["SOLUSDT"] != 1 {
		t.Fatalf("SOL should receive after add, got %d", s.barsSeen["SOLUSDT"])
	}

	// Unregister 清理 universe 订阅 + feeder 引用
	if err := e.Unregister("cfg-uni1"); err != nil {
		t.Fatal(err)
	}
	if len(f.released) == 0 {
		t.Fatal("expected feed releases on unregister")
	}
}

// schedStrategy 实现计划调度的策略测试替身。
type schedStrategy struct {
	BaseStrategy
	name        string
	symbol      string
	running     bool
	schedule    string
	onSched     int
	onSchedBars int
}

func (s *schedStrategy) Name() string           { return s.name }
func (s *schedStrategy) Symbol() string         { return s.symbol }
func (s *schedStrategy) Params() map[string]any { return map[string]any{} }
func (s *schedStrategy) Start(map[string]any) error {
	s.running = true
	return nil
}
func (s *schedStrategy) Stop() error     { s.running = false; return nil }
func (s *schedStrategy) IsRunning() bool { return s.running }
func (s *schedStrategy) OnTick(model.Tick, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *schedStrategy) OnOrderBook(model.OrderBookData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *schedStrategy) OnOrderUpdate(model.OrderData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (s *schedStrategy) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	s.onSchedBars++
	return nil, nil
}
func (s *schedStrategy) Schedule() string { return s.schedule }

// schedCallbackStrategy 带 OnSchedule 回调的变体。
type schedCallbackStrategy struct {
	schedStrategy
}

func (s *schedCallbackStrategy) OnSchedule(time.Time, *event.EventBus) (*model.Signal, error) {
	s.onSched++
	return nil, nil
}

func TestEngineScheduleRegistration(t *testing.T) {
	e := newTestEngine()
	defer e.bus.Close()

	s := &schedCallbackStrategy{schedStrategy: schedStrategy{name: "sch1", symbol: "BTCUSDT", schedule: "@every 1m"}}
	if err := e.Register(WrapStrategy("cfg-sch1", s)); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("cfg-sch1", nil); err != nil {
		t.Fatal(err)
	}
	e.schedMu.Lock()
	en, ok := e.scheduled["cfg-sch1"]
	e.schedMu.Unlock()
	if !ok {
		t.Fatal("expected schedule registration")
	}
	if en.spec.Kind != "every" || en.next.IsZero() {
		t.Fatalf("unexpected schedule entry: %+v", en)
	}

	// 到点触发：把 next 拨到过去再跑一轮调度
	en.next = time.Now().Add(-time.Second)
	e.runDueSchedules()
	if s.onSched != 1 {
		t.Fatalf("expected OnSchedule fired once, got %d", s.onSched)
	}

	// 无效 schedule 不注册但也不阻塞启动
	s2 := &schedStrategy{name: "sch2", symbol: "ETHUSDT", schedule: "not-a-schedule"}
	if err := e.Register(s2); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("sch2", nil); err != nil {
		t.Fatal(err)
	}
	e.schedMu.Lock()
	_, ok = e.scheduled["sch2"]
	e.schedMu.Unlock()
	if ok {
		t.Fatal("invalid schedule should not register")
	}

	// Unregister 清理调度注册
	if err := e.Unregister("cfg-sch1"); err != nil {
		t.Fatal(err)
	}
	e.schedMu.Lock()
	_, ok = e.scheduled["cfg-sch1"]
	e.schedMu.Unlock()
	if ok {
		t.Fatal("expected schedule removed on unregister")
	}
}

// defaultSchedStrategy 无 OnSchedule 回调 → 默认用最近一根 K 线调 OnBar。
type defaultSchedStrategy struct {
	schedStrategy
}

func TestEngineScheduleDefaultCallback(t *testing.T) {
	e := newTestEngine()
	defer e.bus.Close()

	s := &defaultSchedStrategy{schedStrategy: schedStrategy{
		name: "schd1", symbol: "BTCUSDT", schedule: "@every 1m",
	}}
	if err := e.Register(WrapStrategy("cfg-schd1", s)); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("cfg-schd1", nil); err != nil {
		t.Fatal(err)
	}
	// 喂一根 K 线进 MarketData
	e.MarketData().onBar(model.Bar{Symbol: "BTCUSDT", Interval: "1h", Time: 123})
	e.fireSchedule("cfg-schd1", time.Now())
	if s.onSchedBars != 1 {
		t.Fatalf("expected default OnBar callback fired, got %d", s.onSchedBars)
	}
}
