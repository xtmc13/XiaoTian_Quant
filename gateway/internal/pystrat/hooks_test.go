package pystrat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// fakeHookSandbox 在 fakeSandbox 之上实现 v1.2 HookSandbox 可选接口
// （fn nil 即默认行为：confirm/timeout 放行、stake/adjust 为 0）。
type fakeHookSandbox struct {
	*fakeSandbox
	confirmFn func(kind, side string, price, amount float64) (bool, error)
	stakeFn   func(proposed, price float64, side string) (float64, error)
	adjustFn  func(bar model.Bar, qty, avgPrice float64, side string) (float64, error)
	timeoutFn func(evt OrderEvent) (bool, error)
}

func (f *fakeHookSandbox) ConfirmTrade(ctx context.Context, kind, side string, price, amount float64) (HookResult, error) {
	if f.confirmFn == nil {
		return HookResult{Allow: true}, nil
	}
	allow, err := f.confirmFn(kind, side, price, amount)
	return HookResult{Allow: allow}, err
}

func (f *fakeHookSandbox) CustomStake(ctx context.Context, proposed, price float64, side string) (HookResult, error) {
	if f.stakeFn == nil {
		return HookResult{}, nil
	}
	v, err := f.stakeFn(proposed, price, side)
	return HookResult{Value: v}, err
}

func (f *fakeHookSandbox) AdjustPosition(ctx context.Context, bar model.Bar, qty, avgPrice float64, side string) (HookResult, error) {
	if f.adjustFn == nil {
		return HookResult{}, nil
	}
	v, err := f.adjustFn(bar, qty, avgPrice, side)
	return HookResult{Value: v}, err
}

func (f *fakeHookSandbox) CheckEntryTimeout(ctx context.Context, evt OrderEvent) (HookResult, error) {
	if f.timeoutFn == nil {
		return HookResult{Allow: true}, nil
	}
	cancel, err := f.timeoutFn(evt)
	return HookResult{Allow: cancel}, err
}

// fakeCancelExecutor 在 fakeExecutor 之上实现 v1.2 CancelExecutor。
type fakeCancelExecutor struct {
	*fakeExecutor
	cancels []string
}

func (f *fakeCancelExecutor) CancelSpot(botID, orderID string) error {
	f.cancels = append(f.cancels, orderID)
	return nil
}

func newHookRunner(t *testing.T, hs *fakeHookSandbox, src *fakeBarSource, exec Executor, accts *fakeAccounts) *Runner {
	t.Helper()
	return NewRunner(nil, func() Sandbox {
		hs.fakeSandbox.alive = true
		return hs
	}, src, exec, accts)
}

func pushBar(src *fakeBarSource, close float64) {
	src.push("BTCUSDT", "15m", model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: close, Time: time.Now().UnixMilli()})
}

// TestRunnerConfirmEntryVeto：confirm_entry 否决买入动作；放行后正常下单。
func TestRunnerConfirmEntryVeto(t *testing.T) {
	sb := &fakeHookSandbox{fakeSandbox: &fakeSandbox{}}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "buy", Amount: 100}}}, nil
	}
	allow := false
	sb.confirmFn = func(kind, side string, price, amount float64) (bool, error) {
		if kind != "entry" {
			t.Errorf("confirm kind = %q, want entry", kind)
		}
		return allow, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{buyFill: 0.5}
	accts := &fakeAccounts{equity: 10000}

	r := newHookRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	pushBar(src, 50000)
	waitFor(t, func() bool { return sb.bars >= 1 }, "bar processed")
	if len(exec.buys) != 0 {
		t.Fatalf("confirm_entry veto must block buy, got %d", len(exec.buys))
	}

	allow = true
	pushBar(src, 50000)
	waitFor(t, func() bool { return len(exec.buys) == 1 }, "buy after allow")
	if exec.buys[0].quote != 100 {
		t.Fatalf("buy quote = %v, want 100", exec.buys[0].quote)
	}
}

// TestRunnerCustomStakeAmount：custom_stake_amount 覆盖买入金额；返回 0 保持默认。
func TestRunnerCustomStakeAmount(t *testing.T) {
	sb := &fakeHookSandbox{fakeSandbox: &fakeSandbox{}}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "buy", Amount: 100}}}, nil
	}
	stake := 777.0
	sb.stakeFn = func(proposed, price float64, side string) (float64, error) {
		if proposed != 100 {
			t.Errorf("proposed = %v, want 100", proposed)
		}
		return stake, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{buyFill: 0.5}
	accts := &fakeAccounts{equity: 10000}

	r := newHookRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	pushBar(src, 50000)
	waitFor(t, func() bool { return len(exec.buys) == 1 }, "buy with custom stake")
	if exec.buys[0].quote != 777 {
		t.Fatalf("buy quote = %v, want 777 (custom_stake_amount)", exec.buys[0].quote)
	}

	stake = 0
	pushBar(src, 50000)
	waitFor(t, func() bool { return len(exec.buys) == 2 }, "buy with default stake")
	if exec.buys[1].quote != 100 {
		t.Fatalf("stake=0 must keep action amount, got %v", exec.buys[1].quote)
	}
}

// TestRunnerConfirmExitVeto：confirm_exit 否决卖出/平仓；确认钩子异常时
// fail-safe 否决入场但放行出场。
func TestRunnerConfirmExitVeto(t *testing.T) {
	sb := &fakeHookSandbox{fakeSandbox: &fakeSandbox{}}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "sell", Qty: 0.25}}}, nil
	}
	allow := false
	sb.confirmFn = func(kind, side string, price, amount float64) (bool, error) {
		if kind != "exit" {
			t.Errorf("confirm kind = %q, want exit", kind)
		}
		return allow, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{sellFill: 0.25}
	accts := &fakeAccounts{equity: 10000, qty: 0.5, avg: 48000, side: "long", ok: true}

	r := newHookRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	pushBar(src, 50000)
	waitFor(t, func() bool { return sb.bars >= 1 }, "bar processed")
	if len(exec.sells) != 0 {
		t.Fatalf("confirm_exit veto must block sell, got %d", len(exec.sells))
	}

	allow = true
	pushBar(src, 50000)
	waitFor(t, func() bool { return len(exec.sells) == 1 }, "sell after allow")
}

// TestRunnerAdjustTradePosition：adjust_trade_position 持仓期间每根 K 线询问；
// >0 加仓（走 execAction 买入链路），次数受 manifest.risk.max_position_adjustments
// 限制，持仓归零计数清零。
func TestRunnerAdjustTradePosition(t *testing.T) {
	sb := &fakeHookSandbox{fakeSandbox: &fakeSandbox{
		manifest: &Manifest{
			Name: "hooked", Symbol: "BTC/USDT", Interval: "15m", Direction: DirectionLong,
			Risk: ManifestRisk{MaxPositionAdjustments: 1},
		},
	}}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) { return &BarResult{}, nil }
	adjustAmt := 300.0
	sb.adjustFn = func(bar model.Bar, qty, avgPrice float64, side string) (float64, error) {
		if qty != 1 || side != "long" {
			t.Errorf("adjust position = qty %v side %q, want 1/long", qty, side)
		}
		return adjustAmt, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{buyFill: 0.5, sellFill: 0.5}
	accts := &fakeAccounts{equity: 10000, qty: 1, avg: 50000, side: "long", ok: true}

	r := newHookRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	pushBar(src, 50000)
	waitFor(t, func() bool { return len(exec.buys) == 1 }, "adjustment buy")
	if exec.buys[0].quote != 300 {
		t.Fatalf("adjustment buy quote = %v, want 300", exec.buys[0].quote)
	}

	// max_position_adjustments=1：第二根 K 线不再调整
	pushBar(src, 50000)
	waitFor(t, func() bool { return sb.bars >= 2 }, "second bar")
	time.Sleep(20 * time.Millisecond)
	if len(exec.buys) != 1 {
		t.Fatalf("max_position_adjustments=1 must stop further buys, got %d", len(exec.buys))
	}

	// 负值 → 减仓卖出（金额按参考价折算数量）
	accts.qty = 1 // 仍有持仓
	adjustAmt = -100
	pushBar(src, 50000)
	waitFor(t, func() bool { return len(exec.sells) == 1 }, "adjustment reduce sell")
	if exec.sells[0].qty != 100.0/50000.0 {
		t.Fatalf("reduce sell qty = %v, want 100/50000", exec.sells[0].qty)
	}
}

// TestRunnerCheckEntryTimeout：入场限价单超时默认撤单；策略
// check_entry_timeout 返回 false 保留挂单。
func TestRunnerCheckEntryTimeout(t *testing.T) {
	sb := &fakeHookSandbox{fakeSandbox: &fakeSandbox{
		manifest: &Manifest{
			Name: "hooked", Symbol: "BTC/USDT", Interval: "15m", Direction: DirectionLong,
			Risk: ManifestRisk{EntryTimeoutMinutes: 1},
		},
	}}
	calls := 0
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		calls++
		if calls == 1 || calls == 3 { // 第 1、3 根 K 线各挂一笔限价买单
			return &BarResult{Actions: []Action{{Type: "buy", Amount: 100, Price: 50000}}}, nil
		}
		return &BarResult{}, nil
	}
	cancel := true
	sb.timeoutFn = func(evt OrderEvent) (bool, error) {
		if evt.Type != "limit" || evt.Side != "buy" {
			t.Errorf("timeout order = %+v, want limit buy", evt)
		}
		return cancel, nil
	}
	src := newFakeBarSource()
	exec := &fakeCancelExecutor{fakeExecutor: &fakeExecutor{}}
	accts := &fakeAccounts{equity: 10000}

	r := newHookRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	pushBar(src, 50000)
	waitFor(t, func() bool { return len(exec.limitBuys) == 1 }, "limit buy placed")

	// 把挂单时间回拨到超时之外，再喂 K 线触发超时检查
	r.mu.Lock()
	rt := r.bots["ps1"]
	r.mu.Unlock()
	for id, oe := range rt.openEntryOrders {
		oe.placedMs = time.Now().UnixMilli() - 2*60*1000
		rt.openEntryOrders[id] = oe
	}
	if len(rt.openEntryOrders) != 1 {
		t.Fatalf("entry order must be tracked, got %d", len(rt.openEntryOrders))
	}

	pushBar(src, 50000)
	waitFor(t, func() bool { return len(exec.cancels) == 1 }, "timeout cancel")
	if len(rt.openEntryOrders) != 0 {
		t.Fatal("cancelled order must leave timeout tracking")
	}

	// 策略保留挂单：不撤单且继续跟踪
	cancel = false
	pushBar(src, 50000) // 触发第二笔限价单（onBarFn 只在第 4 次调用下首单？ 见下）
	waitFor(t, func() bool { return len(exec.limitBuys) >= 2 }, "second limit buy")
	// 回拨第二笔挂单时间触发超时；策略返回 false → 保留
	for id, oe := range rt.openEntryOrders {
		oe.placedMs = time.Now().UnixMilli() - 2*60*1000
		rt.openEntryOrders[id] = oe
	}
	pushBar(src, 50000)
	waitFor(t, func() bool { return sb.bars >= 4 }, "keep-decision bar processed")
	time.Sleep(20 * time.Millisecond)
	if len(exec.cancels) != 1 {
		t.Fatalf("strategy keep must prevent cancel, got %d", len(exec.cancels))
	}
	if len(rt.openEntryOrders) == 0 {
		t.Fatal("kept order must stay in timeout tracking")
	}
}

// TestRunnerHookErrorNotCounted：钩子回调异常只记日志，不计入连续 10 次
// 错误契约（与 on_order 同一容忍度语义），且 confirm_entry 异常 fail-safe
// 否决本次入场。
func TestRunnerHookErrorNotCounted(t *testing.T) {
	sb := &fakeHookSandbox{fakeSandbox: &fakeSandbox{}}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "buy", Amount: 100}}}, nil
	}
	sb.confirmFn = func(kind, side string, price, amount float64) (bool, error) {
		return false, errors.New("boom")
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{buyFill: 0.5}
	accts := &fakeAccounts{equity: 10000}

	r := newHookRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	pushBar(src, 50000)
	waitFor(t, func() bool { return sb.bars >= 1 }, "bar processed")
	time.Sleep(20 * time.Millisecond)
	if len(exec.buys) != 0 {
		t.Fatalf("confirm_entry error must veto entry (fail-safe), got %d buys", len(exec.buys))
	}
	st := r.Status("ps1")
	if st.ConsecErrors != 0 {
		t.Fatalf("hook errors must not count into error contract, got %d", st.ConsecErrors)
	}
	if !st.Running {
		t.Fatal("strategy must keep running after hook error")
	}
}

// TestValidateStaticV12HookSignatures：v1.2 可选钩子签名错误被静态校验报出；
// 正确签名与不含钩子的策略不受影响。
func TestValidateStaticV12HookSignatures(t *testing.T) {
	base := `STRATEGY_MANIFEST = {"name": "t", "symbol": "BTC/USDT", "interval": "15m", "direction": "long"}
def initialize(context):
    pass
def on_bar(context, bar):
    pass
`
	bad := base + "def confirm_entry(context):\n    return True\n"
	issues := ValidateStatic(bad)
	found := false
	for _, iss := range issues {
		if iss.Code == "SIGNATURE" && strings.Contains(iss.Message, "confirm_entry") {
			found = true
		}
	}
	if !found {
		t.Fatalf("bad confirm_entry signature must be reported, got %v", issues)
	}

	good := base + `def confirm_entry(context, side, price, amount):
    return True
def confirm_exit(context, side, price, qty):
    return True
def custom_stake_amount(context, proposed_amount, price, side):
    return proposed_amount
def adjust_trade_position(context, bar, position):
    return 0
def check_entry_timeout(context, order):
    return True
`
	if issues := ValidateStatic(good); len(issues) != 0 {
		t.Fatalf("valid v1.2 hooks must pass validation, got %v", issues)
	}
}

// TestManifestV12RiskFields：v1.2 risk 字段解析与校验。
func TestManifestV12RiskFields(t *testing.T) {
	m := &Manifest{
		Name: "t", Symbol: "BTC/USDT", Interval: "15m", Direction: DirectionLong,
		Risk: ManifestRisk{MaxPositionAdjustments: 3, EntryTimeoutMinutes: 10},
	}
	if msg := m.Validate(); msg != "" {
		t.Fatalf("valid v1.2 risk fields must pass, got %q", msg)
	}
	m.Risk.MaxPositionAdjustments = -1
	if msg := m.Validate(); msg == "" {
		t.Fatal("negative max_position_adjustments must be rejected")
	}
	m.Risk.MaxPositionAdjustments = 0
	m.Risk.EntryTimeoutMinutes = -5
	if msg := m.Validate(); msg == "" {
		t.Fatal("negative entry_timeout_minutes must be rejected")
	}
}

// TestSubprocessSandboxV12Hooks：真实子进程端到端——worker 执行 v1.2 可选
// 钩子并返回决定；未定义钩子的策略返回默认值。
func TestSubprocessSandboxV12Hooks(t *testing.T) {
	requirePython3(t)

	code := `
STRATEGY_MANIFEST = {"name": "v12", "symbol": "BTC/USDT", "interval": "15m", "direction": "long",
  "risk": {"max_position_adjustments": 2, "entry_timeout_minutes": 5}}

def initialize(context):
    pass

def on_bar(context, bar):
    pass

def confirm_entry(context, side, price, amount):
    context.log("confirm entry amount=%s" % amount)
    return amount < 500

def confirm_exit(context, side, price, qty):
    return False

def custom_stake_amount(context, proposed_amount, price, side):
    return proposed_amount * 2

def adjust_trade_position(context, bar, position):
    if position.get("qty", 0) > 1:
        return -50
    return 0

def check_entry_timeout(context, order):
    return order.get("price", 0) < 60000
`
	sb := NewSubprocessSandbox()
	defer sb.Close()
	manifest, err := sb.Load(context.Background(), code, nil, "BTCUSDT", "15m")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if manifest.Risk.MaxPositionAdjustments != 2 || manifest.Risk.EntryTimeoutMinutes != 5 {
		t.Fatalf("v1.2 risk fields = %+v", manifest.Risk)
	}

	res, err := sb.ConfirmTrade(context.Background(), "entry", "long", 50000, 100)
	if err != nil || !res.Allow {
		t.Fatalf("confirm_entry(100) = %+v, %v; want allow", res, err)
	}
	res, err = sb.ConfirmTrade(context.Background(), "entry", "long", 50000, 1000)
	if err != nil || res.Allow {
		t.Fatalf("confirm_entry(1000) = %+v, %v; want veto", res, err)
	}
	res, err = sb.ConfirmTrade(context.Background(), "exit", "long", 50000, 1)
	if err != nil || res.Allow {
		t.Fatalf("confirm_exit = %+v, %v; want veto", res, err)
	}
	res, err = sb.CustomStake(context.Background(), 100, 50000, "long")
	if err != nil || res.Value != 200 {
		t.Fatalf("custom_stake = %+v, %v; want 200", res, err)
	}
	res, err = sb.AdjustPosition(context.Background(), model.Bar{Close: 50000}, 2, 49000, "long")
	if err != nil || res.Value != -50 {
		t.Fatalf("adjust(qty=2) = %+v, %v; want -50", res, err)
	}
	res, err = sb.AdjustPosition(context.Background(), model.Bar{Close: 50000}, 1, 49000, "long")
	if err != nil || res.Value != 0 {
		t.Fatalf("adjust(qty=1) = %+v, %v; want 0", res, err)
	}
	res, err = sb.CheckEntryTimeout(context.Background(), OrderEvent{ID: "o1", Price: 50000})
	if err != nil || !res.Allow {
		t.Fatalf("check_entry_timeout(50000) = %+v, %v; want cancel", res, err)
	}
	res, err = sb.CheckEntryTimeout(context.Background(), OrderEvent{ID: "o1", Price: 70000})
	if err != nil || res.Allow {
		t.Fatalf("check_entry_timeout(70000) = %+v, %v; want keep", res, err)
	}

	// 未定义钩子的策略：全部默认值（confirm 放行 / stake 0 / adjust 0 / timeout 撤单）
	plain := `
STRATEGY_MANIFEST = {"name": "plain", "symbol": "BTC/USDT", "interval": "15m", "direction": "long"}
def initialize(context):
    pass
def on_bar(context, bar):
    pass
`
	sb2 := NewSubprocessSandbox()
	defer sb2.Close()
	if _, err := sb2.Load(context.Background(), plain, nil, "BTCUSDT", "15m"); err != nil {
		t.Fatalf("load plain: %v", err)
	}
	res, err = sb2.ConfirmTrade(context.Background(), "entry", "long", 50000, 100)
	if err != nil || !res.Allow {
		t.Fatalf("undefined confirm_entry must default allow, got %+v, %v", res, err)
	}
	res, err = sb2.CustomStake(context.Background(), 100, 50000, "long")
	if err != nil || res.Value != 0 {
		t.Fatalf("undefined custom_stake must default 0, got %+v, %v", res, err)
	}
	res, err = sb2.AdjustPosition(context.Background(), model.Bar{Close: 50000}, 1, 49000, "long")
	if err != nil || res.Value != 0 {
		t.Fatalf("undefined adjust must default 0, got %+v, %v", res, err)
	}
	res, err = sb2.CheckEntryTimeout(context.Background(), OrderEvent{ID: "o2", Price: 50000})
	if err != nil || !res.Allow {
		t.Fatalf("undefined check_entry_timeout must default cancel, got %+v, %v", res, err)
	}
}
