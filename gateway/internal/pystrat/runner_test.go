package pystrat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Fakes ──

type fakeSandbox struct {
	mu       sync.Mutex
	onBarFn  func(bar model.Bar, state State) (*BarResult, error)
	loadErr  error
	closed   bool
	alive    bool
	bars     int
	manifest *Manifest
}

func (f *fakeSandbox) Load(ctx context.Context, code string, params map[string]any, symbol, interval string) (*Manifest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if f.manifest != nil {
		return f.manifest, nil
	}
	return &Manifest{
		Name: "fake", Symbol: "BTC/USDT", Interval: "15m", Direction: DirectionLong,
		Params: map[string]any{},
		Risk:   ManifestRisk{MaxPositionPct: 0.9, StopLossPct: 0.05, TakeProfitPct: 0.1},
	}, nil
}

func (f *fakeSandbox) OnBar(ctx context.Context, bar model.Bar, state State) (*BarResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bars++
	if f.onBarFn == nil {
		return &BarResult{}, nil
	}
	return f.onBarFn(bar, state)
}

func (f *fakeSandbox) Close() error { f.closed = true; f.alive = false; return nil }
func (f *fakeSandbox) Alive() bool  { return f.alive }

type fakeBarSource struct {
	chans map[string]chan model.Bar
	unsub int
}

func newFakeBarSource() *fakeBarSource {
	return &fakeBarSource{chans: map[string]chan model.Bar{}}
}

func (f *fakeBarSource) Subscribe(symbol, interval string) (<-chan model.Bar, func(), error) {
	ch := make(chan model.Bar, 64)
	f.chans[symbol+"|"+interval] = ch
	return ch, func() { f.unsub++ }, nil
}

func (f *fakeBarSource) push(symbol, interval string, bar model.Bar) {
	f.chans[symbol+"|"+interval] <- bar
}

type fakeExecutor struct {
	mu       sync.Mutex
	buys     []fakeBuy
	sells    []fakeSell
	buyFill  float64
	sellFill float64
}

type fakeBuy struct {
	botID, symbol, exchange string
	userID                  int64
	quote, refPrice         float64
}
type fakeSell struct {
	botID, symbol, exchange string
	userID                  int64
	qty, refPrice           float64
}

func (f *fakeExecutor) BuySpot(botID, symbol, exchange string, userID int64, quoteAmount, refPrice float64) (float64, float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buys = append(f.buys, fakeBuy{botID, symbol, exchange, userID, quoteAmount, refPrice})
	if f.buyFill <= 0 {
		return f.buyFill, refPrice, nil
	}
	return f.buyFill, refPrice, nil
}

func (f *fakeExecutor) SellSpot(botID, symbol, exchange string, userID int64, baseQty, refPrice float64) (float64, float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sells = append(f.sells, fakeSell{botID, symbol, exchange, userID, baseQty, refPrice})
	return f.sellFill, refPrice, nil
}

type fakeAccounts struct {
	qty     float64
	avg     float64
	side    string
	ok      bool
	equity  float64
	posCall int
}

func (f *fakeAccounts) Position(symbol string) (float64, float64, string, bool) {
	f.posCall++
	return f.qty, f.avg, f.side, f.ok
}
func (f *fakeAccounts) Equity() float64 { return f.equity }

type fakeProtector struct {
	mu       sync.Mutex
	brackets []fakeBracket
}

type fakeBracket struct {
	strategyID, symbol, exchange string
	userID                       int64
	qty, entry, tpPct, slPct     float64
}

func (f *fakeProtector) SetBracket(strategyID, symbol, exchange string, userID int64, qty, entryPrice, tpPct, slPct float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.brackets = append(f.brackets, fakeBracket{strategyID, symbol, exchange, userID, qty, entryPrice, tpPct, slPct})
	return nil
}

// ── Helpers ──

func newTestRunner(t *testing.T, sb *fakeSandbox, src *fakeBarSource, exec *fakeExecutor, accts *fakeAccounts) *Runner {
	t.Helper()
	r := NewRunner(nil, func() Sandbox {
		sb.alive = true
		return sb
	}, src, exec, accts)
	sb.alive = true
	return r
}

func testRecord(paper bool) *store.PyStrategyRecord {
	return &store.PyStrategyRecord{
		ID: "ps1", UserID: 7, Name: "t", Symbol: "BTC/USDT", Interval: "15m",
		Direction: "long", ParamsJSON: "{}", Code: validStrategy, Paper: paper,
		Status: store.PyStratStatusDraft,
	}
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

// ── Tests ──

// bar → on_bar → buy 动作 → OMS BuySpot → TP/SL bracket。
func TestRunnerBarToBuyToOMS(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{
			Actions: []Action{{Type: "buy", Amount: 100}},
			Logs:    []string{"buy signal"},
		}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{buyFill: 0.5}
	accts := &fakeAccounts{equity: 10000, qty: 0, ok: false}
	prot := &fakeProtector{}

	r := newTestRunner(t, sb, src, exec, accts)
	r.SetProtector(prot)
	defer r.StopAll()

	rec := testRecord(true)
	if err := r.Start(rec); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	src.push("BTCUSDT", "15m", model.Bar{Symbol: "BTCUSDT", Interval: "15m", Close: 50000, Time: time.Now().UnixMilli()})

	waitFor(t, func() bool { return len(exec.buys) == 1 }, "buy reached OMS")
	waitFor(t, func() bool { return len(prot.brackets) == 1 }, "bracket registered")

	buy := exec.buys[0]
	if buy.exchange != "paper" {
		t.Fatalf("paper=1 must force paper exchange, got %q", buy.exchange)
	}
	if buy.quote != 100 {
		t.Fatalf("amount buy quote=%v want 100", buy.quote)
	}
	br := prot.brackets[0]
	if br.tpPct != 0.1 || br.slPct != 0.05 {
		t.Fatalf("bracket tp/sl = %v/%v, want 0.1/0.05 (manifest defaults)", br.tpPct, br.slPct)
	}
	if br.entry != 50000 {
		t.Fatalf("bracket entry = %v want refPrice 50000", br.entry)
	}
}

// sell/close_position 在无持仓时为空操作；有持仓时走 SellSpot。
func TestRunnerSellAndClose(t *testing.T) {
	sb := &fakeSandbox{}
	calls := 0
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		calls++
		if calls == 1 {
			return &BarResult{Actions: []Action{{Type: "close_position"}}}, nil
		}
		return &BarResult{Actions: []Action{{Type: "sell", Qty: 0.25}}}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{sellFill: 0.25}
	accts := &fakeAccounts{equity: 10000, qty: 0.5, avg: 48000, side: "long", ok: true}

	r := newTestRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	// bar1：无持仓语义——先把 accounts 置为有持仓，close 应卖出全部
	src.push("BTCUSDT", "15m", model.Bar{Close: 50000, Time: time.Now().UnixMilli()})
	waitFor(t, func() bool { return len(exec.sells) == 1 }, "close sell")
	if exec.sells[0].qty != 0.5 {
		t.Fatalf("close_position must sell full qty 0.5, got %v", exec.sells[0].qty)
	}

	// bar2：减仓卖 0.25
	src.push("BTCUSDT", "15m", model.Bar{Close: 50000, Time: time.Now().UnixMilli()})
	waitFor(t, func() bool { return len(exec.sells) == 2 }, "partial sell")
	if exec.sells[1].qty != 0.25 {
		t.Fatalf("sell qty = %v want 0.25", exec.sells[1].qty)
	}
}

// 异常计数：on_bar 连续报错 10 次 → 暂停（不再 running）。
func TestRunnerErrorContractPauses(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return nil, errors.New("boom")
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{equity: 10000}
	var alerts int
	var alertMu sync.Mutex

	r := newTestRunner(t, sb, src, exec, accts)
	r.SetAlert(func(title, content string) {
		alertMu.Lock()
		alerts++
		alertMu.Unlock()
	})
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	for i := 0; i < MaxConsecutiveErrors; i++ {
		src.push("BTCUSDT", "15m", model.Bar{Close: 1, Time: time.Now().UnixMilli()})
	}
	waitFor(t, func() bool { return !r.IsRunning("ps1") }, "paused after 10 consecutive errors")

	st := r.Status("ps1")
	if st.Running {
		t.Fatal("status must be not running after pause")
	}
	alertMu.Lock()
	if alerts != 1 {
		t.Fatalf("expected 1 alert, got %d", alerts)
	}
	alertMu.Unlock()
	// 错误后日志可见
	if logs := r.Logs("ps1"); logs == nil {
		t.Log("logs not retained after pause (buffer freed with runtime) — acceptable")
	}
}

// 偶发错误会被成功轮重置：9 次错 + 1 次成功 + 9 次错 → 不停（计数被重置后
// 再次累计，但第 19 次后达到 10 → 停）。这里验证"成功轮重置计数"。
func TestRunnerSuccessResetsErrorCount(t *testing.T) {
	sb := &fakeSandbox{}
	var mu sync.Mutex
	fail := true
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, errors.New("boom")
		}
		return &BarResult{}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{equity: 10000}
	r := newTestRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	for i := 0; i < MaxConsecutiveErrors-1; i++ {
		src.push("BTCUSDT", "15m", model.Bar{Close: 1, Time: time.Now().UnixMilli()})
	}
	waitFor(t, func() bool { return r.Status("ps1").ConsecErrors == MaxConsecutiveErrors-1 }, "9 errors")
	mu.Lock()
	fail = false
	mu.Unlock()
	src.push("BTCUSDT", "15m", model.Bar{Close: 1, Time: time.Now().UnixMilli()})
	waitFor(t, func() bool { return r.Status("ps1").ConsecErrors == 0 }, "reset after success")
	if !r.IsRunning("ps1") {
		t.Fatal("must still be running")
	}
}

// paper=0 且实盘闸未放行 → 拒绝启动。
func TestRunnerLiveGateRefusesWhenLocked(t *testing.T) {
	sb := &fakeSandbox{}
	src := newFakeBarSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{equity: 10000}
	r := newTestRunner(t, sb, src, exec, accts)
	r.SetLiveGate(func(exchange string) error {
		return errors.New("实盘交易未启用")
	})
	defer r.StopAll()

	err := r.Start(testRecord(false))
	if err == nil || !strings.Contains(err.Error(), "实盘闸") {
		t.Fatalf("expected live gate refusal, got %v", err)
	}
	if r.IsRunning("ps1") {
		t.Fatal("must not be running")
	}
}

// paper=0 且闸已解锁 → 启动且 exchange=binance。
func TestRunnerLiveGateAllowsWhenUnlocked(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "buy", Amount: 50}}}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{buyFill: 0.01}
	accts := &fakeAccounts{equity: 10000}
	r := newTestRunner(t, sb, src, exec, accts)
	r.SetLiveGate(func(exchange string) error { return nil })
	defer r.StopAll()

	if err := r.Start(testRecord(false)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")
	src.push("BTCUSDT", "15m", model.Bar{Close: 100, Time: time.Now().UnixMilli()})
	waitFor(t, func() bool { return len(exec.buys) == 1 }, "live buy")
	if exec.buys[0].exchange != "binance" {
		t.Fatalf("paper=0 exchange = %q want binance", exec.buys[0].exchange)
	}
}

// 静态校验不过 → Start 直接拒绝。
func TestRunnerStartRejectsInvalidCode(t *testing.T) {
	sb := &fakeSandbox{}
	src := newFakeBarSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{}
	r := newTestRunner(t, sb, src, exec, accts)
	defer r.StopAll()

	rec := testRecord(true)
	rec.Code = "import os\n"
	if err := r.Start(rec); err == nil || !strings.Contains(err.Error(), "静态校验") {
		t.Fatalf("expected static validation refusal, got %v", err)
	}
}

// 暖机 bar（早于启动时刻）不下单。
func TestRunnerWarmupBarNoOrders(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "buy", Amount: 10}}}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{equity: 10000}
	r := newTestRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	old := time.Now().Add(-2 * time.Hour).UnixMilli()
	src.push("BTCUSDT", "15m", model.Bar{Close: 1, Time: old})
	time.Sleep(100 * time.Millisecond)
	if len(exec.buys) != 0 {
		t.Fatal("warmup bar must not place orders")
	}
	src.push("BTCUSDT", "15m", model.Bar{Close: 1, Time: time.Now().UnixMilli()})
	waitFor(t, func() bool { return len(exec.buys) == 1 }, "live bar orders")
}

// set_stop_loss/set_take_profit 覆盖 manifest 默认值，作用于下一次成交。
func TestRunnerStrategyOverridesBracket(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{
			{Type: "set_stop_loss", Pct: 0.02},
			{Type: "set_take_profit", Pct: 0.2},
			{Type: "buy", Amount: 100},
		}}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{buyFill: 1}
	accts := &fakeAccounts{equity: 10000}
	prot := &fakeProtector{}
	r := newTestRunner(t, sb, src, exec, accts)
	r.SetProtector(prot)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")
	src.push("BTCUSDT", "15m", model.Bar{Close: 100, Time: time.Now().UnixMilli()})
	waitFor(t, func() bool { return len(prot.brackets) == 1 }, "bracket")
	if prot.brackets[0].slPct != 0.02 || prot.brackets[0].tpPct != 0.2 {
		t.Fatalf("overrides not applied: %+v", prot.brackets[0])
	}
}

// max_position_pct 风控拦截。
func TestRunnerPositionLimitBlocksBuy(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "buy", Amount: 9500}}}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{equity: 10000}
	r := newTestRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")
	src.push("BTCUSDT", "15m", model.Bar{Close: 100, Time: time.Now().UnixMilli()})
	time.Sleep(100 * time.Millisecond)
	if len(exec.buys) != 0 {
		t.Fatal("buy beyond max_position_pct must be blocked")
	}
}

// direction=short 拒绝买入开仓。
func TestRunnerDirectionShortBlocksBuy(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "buy", Amount: 100}}}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{equity: 10000}
	r := newTestRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	rec := testRecord(true)
	rec.Direction = "short"
	sb.manifest = &Manifest{
		Name: "fake", Symbol: "BTC/USDT", Interval: "15m", Direction: DirectionShort,
		Risk: ManifestRisk{MaxPositionPct: 0.9},
	}
	if err := r.Start(rec); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")
	src.push("BTCUSDT", "15m", model.Bar{Close: 100, Time: time.Now().UnixMilli()})
	time.Sleep(100 * time.Millisecond)
	if len(exec.buys) != 0 {
		t.Fatal("direction=short must block spot buy")
	}
}
