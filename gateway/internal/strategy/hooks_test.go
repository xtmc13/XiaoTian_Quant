package strategy

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/protection"
)

// hookFake 是契约钩子（v1.2）测试用的策略假身：嵌入 BaseStrategy，
// fn 非 nil 的钩子覆写默认行为，nil 即默认（= BaseStrategy 实现）。
type hookFake struct {
	BaseStrategy
	name    string
	running bool

	confirmEntry func(sig *model.Signal) bool
	confirmExit  func(pos *Position) bool
	customStake  func(balance float64, sig *model.Signal) float64
	adjust       func(pos *Position, bar model.Bar) float64
	maxAdjusts   int
	entryTimeout func(od model.OrderData) bool
	exitTimeout  func(od model.OrderData) bool

	onBar func(bar model.Bar) (*model.Signal, error)
}

func (f *hookFake) Name() string               { return f.name }
func (f *hookFake) Symbol() string             { return "BTCUSDT" }
func (f *hookFake) Params() map[string]any     { return nil }
func (f *hookFake) Start(map[string]any) error { f.running = true; return nil }
func (f *hookFake) Stop() error                { f.running = false; return nil }
func (f *hookFake) IsRunning() bool            { return f.running }

func (f *hookFake) OnTick(model.Tick, *event.EventBus) (*model.Signal, error) { return nil, nil }
func (f *hookFake) OnOrderBook(model.OrderBookData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}
func (f *hookFake) OnBar(bar model.Bar, _ *event.EventBus) (*model.Signal, error) {
	if f.onBar != nil {
		return f.onBar(bar)
	}
	return nil, nil
}
func (f *hookFake) OnOrderUpdate(model.OrderData, *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (f *hookFake) ConfirmTradeEntry(sig *model.Signal) bool {
	if f.confirmEntry == nil {
		return true
	}
	return f.confirmEntry(sig)
}

func (f *hookFake) ConfirmTradeExit(pos *Position) bool {
	if f.confirmExit == nil {
		return true
	}
	return f.confirmExit(pos)
}

func (f *hookFake) CustomStakeAmount(balance float64, sig *model.Signal) float64 {
	if f.customStake == nil {
		return 0
	}
	return f.customStake(balance, sig)
}

// ── v1.2 可选接口（类型断言探测路径）──

func (f *hookFake) AdjustTradePosition(pos *Position, bar model.Bar) float64 {
	if f.adjust == nil {
		return 0
	}
	return f.adjust(pos, bar)
}

func (f *hookFake) MaxPositionAdjustments() int { return f.maxAdjusts }

func (f *hookFake) CheckEntryTimeout(od model.OrderData) bool {
	if f.entryTimeout == nil {
		return true
	}
	return f.entryTimeout(od)
}

func (f *hookFake) CheckExitTimeout(od model.OrderData) bool {
	if f.exitTimeout == nil {
		return true
	}
	return f.exitTimeout(od)
}

// newHookTestEngine 构造独立引擎实例（不走 GetEngine 单例，避免测试间污染）。
func newHookTestEngine() *Engine {
	return &Engine{
		strategies:          make(map[string]Strategy),
		symbolMap:           make(map[string][]string),
		subIDs:              make(map[string]event.SubscriptionID),
		bus:                 event.NewEventBus(64, 1),
		feedHolds:           make(map[string][]feedHold),
		universes:           make(map[string]*universeState),
		scheduled:           make(map[string]*scheduledEntry),
		adjCounts:           make(map[string]int),
		strategyProtections: make(map[string]*protection.ProtectionManager),
	}
}

// TestEmitSignalConfirmEntryVeto：confirm_trade_entry 对标——策略否决时信号
// 不到达 OnSignal；放行时正常发出且策略名被包装名覆盖。
func TestEmitSignalConfirmEntryVeto(t *testing.T) {
	eng := newHookTestEngine()
	veto := true
	s := &hookFake{name: "hk_entry", running: true,
		confirmEntry: func(sig *model.Signal) bool { return !veto }}
	var got []model.Signal
	eng.OnSignal = func(sig model.Signal) { got = append(got, sig) }

	eng.emitSignal(s, &model.Signal{Symbol: "BTCUSDT", Direction: "LONG", Qty: 0.1}, nil)
	if len(got) != 0 {
		t.Fatalf("vetoed entry signal must not reach OnSignal, got %d", len(got))
	}

	veto = false
	eng.emitSignal(s, &model.Signal{Symbol: "BTCUSDT", Direction: "LONG", Qty: 0.1}, nil)
	if len(got) != 1 {
		t.Fatalf("allowed entry signal should reach OnSignal, got %d", len(got))
	}
	if got[0].Strategy != "hk_entry" {
		t.Fatalf("signal.Strategy = %q, want wrapped name hk_entry", got[0].Strategy)
	}
}

// TestEmitSignalCustomStakeAmount：custom_stake_amount 对标——信号未指定数量
// 时按策略返回金额 ÷ 最新收盘价折算数量；返回 0 保持默认（Qty 不变）。
func TestEmitSignalCustomStakeAmount(t *testing.T) {
	eng := newHookTestEngine()
	eng.MarketData().onBar(model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 50, Time: 1})
	eng.SetBalanceLookup(func() float64 { return 1000 })

	var seenBalance float64
	stake := 500.0
	s := &hookFake{name: "hk_stake", running: true,
		customStake: func(balance float64, sig *model.Signal) float64 {
			seenBalance = balance
			return stake
		}}
	var got []model.Signal
	eng.OnSignal = func(sig model.Signal) { got = append(got, sig) }

	eng.emitSignal(s, &model.Signal{Symbol: "BTCUSDT", Direction: "LONG"}, nil)
	if len(got) != 1 {
		t.Fatalf("signal should pass, got %d", len(got))
	}
	if seenBalance != 1000 {
		t.Fatalf("CustomStakeAmount balance = %v, want 1000", seenBalance)
	}
	if got[0].Qty != 10 { // 500 / 50
		t.Fatalf("qty = %v, want 10 (stake 500 / price 50)", got[0].Qty)
	}

	// stake=0：不覆盖（Qty 保持 0，走下游默认 sizing）
	stake = 0
	eng.emitSignal(s, &model.Signal{Symbol: "BTCUSDT", Direction: "LONG"}, nil)
	if got[1].Qty != 0 {
		t.Fatalf("stake=0 must keep qty 0, got %v", got[1].Qty)
	}

	// 信号自带数量时不询问 stake 钩子
	called := false
	s.customStake = func(float64, *model.Signal) float64 { called = true; return 500 }
	eng.emitSignal(s, &model.Signal{Symbol: "BTCUSDT", Direction: "LONG", Qty: 3}, nil)
	if called {
		t.Fatal("CustomStakeAmount must not be consulted when signal carries qty")
	}
	if got[2].Qty != 3 {
		t.Fatalf("explicit qty overwritten: %v", got[2].Qty)
	}
}

// TestEmitSignalConfirmExitVeto：confirm_trade_exit 对标——出场信号被否决时
// 不下单；钩子拿到持仓快照。
func TestEmitSignalConfirmExitVeto(t *testing.T) {
	eng := newHookTestEngine()
	eng.SetPositionLookup(func(strategyName, symbol string) *Position {
		return &Position{Symbol: symbol, Side: "LONG", Quantity: 1, EntryPrice: 100}
	})
	veto := true
	s := &hookFake{name: "hk_exit", running: true,
		confirmExit: func(pos *Position) bool {
			if pos == nil || pos.Quantity != 1 || pos.EntryPrice != 100 {
				t.Errorf("ConfirmTradeExit position snapshot = %+v, want qty=1 entry=100", pos)
			}
			return !veto
		}}
	var got []model.Signal
	eng.OnSignal = func(sig model.Signal) { got = append(got, sig) }

	eng.emitSignal(s, &model.Signal{Symbol: "BTCUSDT", Direction: "CLOSE"}, nil)
	if len(got) != 0 {
		t.Fatalf("vetoed exit signal must not reach OnSignal, got %d", len(got))
	}
	veto = false
	eng.emitSignal(s, &model.Signal{Symbol: "BTCUSDT", Direction: "CLOSE"}, nil)
	if len(got) != 1 {
		t.Fatalf("allowed exit signal should reach OnSignal, got %d", len(got))
	}
}

// TestDispatchAdjustTradePosition：adjust_trade_position 对标——持仓期间每根
// K 线询问；>0 生成同向加仓信号（qty=金额/bar 收盘）、<0 生成减仓 CLOSE 信号；
// MaxPositionAdjustments 限次数；持仓归零计数清零。
func TestDispatchAdjustTradePosition(t *testing.T) {
	eng := newHookTestEngine()
	pos := &Position{Symbol: "BTCUSDT", Side: "LONG", Quantity: 1, EntryPrice: 100}
	eng.SetPositionLookup(func(_, _ string) *Position { return pos })

	adjustAmt := 250.0
	s := &hookFake{name: "hk_adj", running: true, maxAdjusts: 1,
		adjust: func(p *Position, bar model.Bar) float64 {
			if p.Quantity != 1 {
				t.Errorf("AdjustTradePosition pos qty = %v, want 1", p.Quantity)
			}
			return adjustAmt
		}}
	var got []model.Signal
	eng.OnSignal = func(sig model.Signal) { got = append(got, sig) }

	bar := model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 125, Time: 2}
	eng.dispatch(s, event.Event{Type: event.TypeBar, Data: bar})
	if len(got) != 1 {
		t.Fatalf("adjustment signal expected, got %d", len(got))
	}
	if got[0].Direction != "LONG" || got[0].Qty != 2 || got[0].Reason != "position_adjust" {
		t.Fatalf("adjustment signal = %+v, want LONG qty=2 (250/125) reason=position_adjust", got[0])
	}

	// maxAdjusts=1：第二根 K 线不再询问（计数已达上限）
	eng.dispatch(s, event.Event{Type: event.TypeBar, Data: model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 125, Time: 3}})
	if len(got) != 1 {
		t.Fatalf("max_position_adjustments=1 must stop further adjustments, got %d signals", len(got))
	}

	// 持仓归零 → 计数清零 → 新持仓重新允许调整
	pos = nil
	eng.SetPositionLookup(func(_, _ string) *Position { return pos })
	eng.dispatch(s, event.Event{Type: event.TypeBar, Data: model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 125, Time: 4}})
	if eng.adjCount("hk_adj") != 0 {
		t.Fatalf("adjust count must reset when flat, got %d", eng.adjCount("hk_adj"))
	}
	pos = &Position{Symbol: "BTCUSDT", Side: "LONG", Quantity: 1, EntryPrice: 100}
	eng.dispatch(s, event.Event{Type: event.TypeBar, Data: model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 125, Time: 5}})
	if len(got) != 2 {
		t.Fatalf("new position should allow adjustment again, got %d signals", len(got))
	}

	// 负值 → 减仓 CLOSE 信号
	adjustAmt = -125
	s.maxAdjusts = 0 // 不限
	eng.dispatch(s, event.Event{Type: event.TypeBar, Data: model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 125, Time: 6}})
	last := got[len(got)-1]
	if last.Direction != "CLOSE" || last.Qty != 1 || last.Reason != "position_reduce" {
		t.Fatalf("reduce signal = %+v, want CLOSE qty=1 reason=position_reduce", last)
	}
}

// TestDispatchShortAdjustDirection：SHORT 持仓的加仓调整生成 SHORT 信号。
func TestDispatchShortAdjustDirection(t *testing.T) {
	eng := newHookTestEngine()
	eng.SetPositionLookup(func(_, _ string) *Position {
		return &Position{Symbol: "BTCUSDT", Side: "SHORT", Quantity: 1, EntryPrice: 100}
	})
	s := &hookFake{name: "hk_adj_short", running: true,
		adjust: func(*Position, model.Bar) float64 { return 100 }}
	var got []model.Signal
	eng.OnSignal = func(sig model.Signal) { got = append(got, sig) }

	eng.dispatch(s, event.Event{Type: event.TypeBar, Data: model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 50, Time: 2}})
	if len(got) != 1 || got[0].Direction != "SHORT" {
		t.Fatalf("SHORT position adjustment must emit SHORT signal, got %+v", got)
	}
}

// TestDispatchWithoutHooksUnaffected：未实现任何钩子的存量策略（runtimeFake
// 只嵌 BaseStrategy）在 dispatch/emitSignal 全链路行为不变。
func TestDispatchWithoutHooksUnaffected(t *testing.T) {
	eng := newHookTestEngine()
	s := &runtimeFake{name: "hk_plain", running: true}
	var got []model.Signal
	eng.OnSignal = func(sig model.Signal) { got = append(got, sig) }

	// 无 PositionAdjuster：dispatch 不 panic、不产生额外信号
	eng.dispatch(s, event.Event{Type: event.TypeBar, Data: model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 100, Time: 2}})
	if len(got) != 0 {
		t.Fatalf("plain strategy dispatch should produce no signal, got %d", len(got))
	}
	// BaseStrategy 默认钩子：confirm=true / stake=0 → 信号原样通过
	eng.emitSignal(s, &model.Signal{Symbol: "BTCUSDT", Direction: "LONG", Qty: 1}, nil)
	eng.emitSignal(s, &model.Signal{Symbol: "BTCUSDT", Direction: "CLOSE"}, nil)
	if len(got) != 2 {
		t.Fatalf("BaseStrategy defaults must pass signals through, got %d", len(got))
	}
	if got[0].Qty != 1 {
		t.Fatalf("default stake hook must not change qty, got %v", got[0].Qty)
	}
}

// TestTimeoutDecider：check_entry_timeout / check_exit_timeout 对标——
// 引擎按 symbol 找运行中策略接管挂单超时决策；未接管时 ok=false 走默认逻辑。
func TestTimeoutDecider(t *testing.T) {
	eng := newHookTestEngine()
	entryCancel := false
	exitCancel := true
	s := &hookFake{name: "hk_to", running: true,
		entryTimeout: func(od model.OrderData) bool { return entryCancel },
		exitTimeout:  func(od model.OrderData) bool { return exitCancel }}
	if err := eng.Register(s); err != nil {
		t.Fatalf("register: %v", err)
	}
	decider := eng.TimeoutDecider()

	// 策略返回 false → keep（保留挂单）
	action, ok := decider(order.OrderState{OrderID: "o1", Symbol: "BTCUSDT", Side: "buy", Type: "entry"})
	if !ok || action != "keep" {
		t.Fatalf("entry decider = (%q,%v), want (keep,true)", action, ok)
	}
	// 策略返回 true → cancel（撤单）
	entryCancel = true
	action, ok = decider(order.OrderState{OrderID: "o1", Symbol: "BTCUSDT", Side: "buy", Type: "entry"})
	if !ok || action != "cancel" {
		t.Fatalf("entry decider = (%q,%v), want (cancel,true)", action, ok)
	}
	// exit 类型走 CheckExitTimeout
	action, ok = decider(order.OrderState{OrderID: "o2", Symbol: "BTCUSDT", Side: "sell", Type: "exit"})
	if !ok || action != "cancel" {
		t.Fatalf("exit decider = (%q,%v), want (cancel,true)", action, ok)
	}
	// 无策略的 symbol → 不接管
	if _, ok = decider(order.OrderState{OrderID: "o3", Symbol: "ETHUSDT", Type: "entry"}); ok {
		t.Fatal("unknown symbol must not be taken over")
	}
	// 策略停止 → 不接管
	s.running = false
	if _, ok = decider(order.OrderState{OrderID: "o4", Symbol: "BTCUSDT", Type: "entry"}); ok {
		t.Fatal("stopped strategy must not be consulted")
	}
}

// TestTimeoutTrackerWithDecider：order.TimeoutTracker 集成——策略 keep 重置
// 计时并保留挂单；cancel 产出撤单动作；未接管时走现有默认（超时撤单）。
func TestTimeoutTrackerWithDecider(t *testing.T) {
	eng := newHookTestEngine()
	entryCancel := false
	s := &hookFake{name: "hk_trk", running: true,
		entryTimeout: func(od model.OrderData) bool { return entryCancel }}
	if err := eng.Register(s); err != nil {
		t.Fatalf("register: %v", err)
	}

	trk := order.NewTimeoutTracker(order.TimeoutConfig{
		EntryTimeout: time.Millisecond, ExitTimeout: time.Millisecond,
		ExitTimeoutCount: 3, CancelUnfilled: true,
	})
	trk.SetDecider(eng.TimeoutDecider())
	trk.Track("o1", "BTCUSDT", "buy", "entry", 100, 1)
	time.Sleep(3 * time.Millisecond)

	// 策略 keep：无动作，且计时重置（紧接着再查仍无动作）
	if actions := trk.CheckTimeout(); len(actions) != 0 {
		t.Fatalf("keep decision must yield no action, got %+v", actions)
	}
	if actions := trk.CheckTimeout(); len(actions) != 0 {
		t.Fatalf("keep must reset the timer, got %+v", actions)
	}

	// 策略改判 cancel：产出撤单动作
	entryCancel = true
	time.Sleep(3 * time.Millisecond)
	actions := trk.CheckTimeout()
	if len(actions) != 1 || actions[0].Action != "cancel" || actions[0].OrderID != "o1" {
		t.Fatalf("cancel decision = %+v, want single cancel for o1", actions)
	}

	// 无策略接管的 symbol：走现有默认（超时撤单）
	trk.Track("o9", "ETHUSDT", "buy", "entry", 100, 1)
	time.Sleep(3 * time.Millisecond)
	actions = trk.CheckTimeout()
	found := false
	for _, a := range actions {
		if a.OrderID == "o9" && a.Action == "cancel" {
			found = true
		}
	}
	if !found {
		t.Fatalf("default timeout logic must cancel untracked-decider order, got %+v", actions)
	}
}

// TestAdjustHookPanicSafe：钩子 panic 不拖垮引擎，回退默认行为。
func TestAdjustHookPanicSafe(t *testing.T) {
	eng := newHookTestEngine()
	eng.SetPositionLookup(func(_, _ string) *Position {
		return &Position{Symbol: "BTCUSDT", Side: "LONG", Quantity: 1, EntryPrice: 100}
	})
	s := &hookFake{name: "hk_panic", running: true,
		adjust:       func(*Position, model.Bar) float64 { panic("boom") },
		confirmEntry: func(*model.Signal) bool { panic("boom") }}
	var got []model.Signal
	eng.OnSignal = func(sig model.Signal) { got = append(got, sig) }

	// adjust panic → 无调整信号，引擎存活
	eng.dispatch(s, event.Event{Type: event.TypeBar, Data: model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 100, Time: 2}})
	if len(got) != 0 {
		t.Fatalf("panicking adjust hook must yield no signal, got %d", len(got))
	}
	// confirm panic → 回退默认放行
	eng.emitSignal(s, &model.Signal{Symbol: "BTCUSDT", Direction: "LONG", Qty: 1}, nil)
	if len(got) != 1 {
		t.Fatalf("panicking confirm hook must fall back to default allow, got %d", len(got))
	}
}
