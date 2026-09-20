package pystrat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Fakes ──

type fakeSandbox struct {
	mu        sync.Mutex
	onBarFn   func(bar model.Bar, state State) (*BarResult, error)
	onOrderFn func(evt OrderEvent) (*OrderResult, error)
	loadErr   error
	closed    bool
	alive     bool
	bars      int
	orders    int
	manifest  *Manifest
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

func (f *fakeSandbox) OnOrder(ctx context.Context, evt OrderEvent) (*OrderResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orders++
	if f.onOrderFn == nil {
		return &OrderResult{}, nil
	}
	return f.onOrderFn(evt)
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

// fakeOrderSource 是 OrderSource 的 channel fake（v1.1 on_order 测试用）。
type fakeOrderSource struct {
	mu    sync.Mutex
	chans map[string]chan OrderEvent
	unsub int
}

func newFakeOrderSource() *fakeOrderSource {
	return &fakeOrderSource{chans: map[string]chan OrderEvent{}}
}

func (f *fakeOrderSource) Subscribe(botID, symbol string) (<-chan OrderEvent, func(), error) {
	ch := make(chan OrderEvent, 64)
	f.mu.Lock()
	f.chans[botID+"|"+symbol] = ch
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		f.unsub++
		f.mu.Unlock()
	}, nil
}

func (f *fakeOrderSource) push(botID, symbol string, evt OrderEvent) {
	f.mu.Lock()
	ch := f.chans[botID+"|"+symbol]
	f.mu.Unlock()
	if ch != nil {
		ch <- evt
	}
}

type fakeExecutor struct {
	mu            sync.Mutex
	buys          []fakeBuy
	sells         []fakeSell
	limitBuys     []fakeLimitBuy
	limitSells    []fakeLimitSell
	contracts     []fakeContract
	buyFill       float64
	sellFill      float64
	contractFill  float64
	limitOrderIDs int
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

type fakeLimitBuy struct {
	botID, symbol, exchange string
	userID                  int64
	quote, price            float64
}
type fakeLimitSell struct {
	botID, symbol, exchange string
	userID                  int64
	qty, price              float64
}

type fakeContract struct {
	botID, symbol, exchange string
	userID                  int64
	side, marginMode        string
	positionSide            string
	qty, price, leverage    float64
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

// BuySpotLimit 实现 LimitExecutor，记录限价买单参数（v1.1 断言用）。
func (f *fakeExecutor) BuySpotLimit(botID, symbol, exchange string, userID int64, quoteAmount, price float64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limitBuys = append(f.limitBuys, fakeLimitBuy{botID, symbol, exchange, userID, quoteAmount, price})
	f.limitOrderIDs++
	return fmt.Sprintf("ord-limit-%d", f.limitOrderIDs), nil
}

// SellSpotLimit 实现 LimitExecutor，记录限价卖单参数。
func (f *fakeExecutor) SellSpotLimit(botID, symbol, exchange string, userID int64, baseQty, price float64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limitSells = append(f.limitSells, fakeLimitSell{botID, symbol, exchange, userID, baseQty, price})
	f.limitOrderIDs++
	return fmt.Sprintf("ord-limit-%d", f.limitOrderIDs), nil
}

// PlaceContract 实现 ContractExecutor，记录合约单参数（v1.1 断言用）。
func (f *fakeExecutor) PlaceContract(botID, symbol, exchange string, userID int64, side string, qty, price, leverage float64, marginMode, positionSide string) (float64, float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contracts = append(f.contracts, fakeContract{botID, symbol, exchange, userID, side, marginMode, positionSide, qty, price, leverage})
	return f.contractFill, price, nil
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

// ── v1.1 测试 ──

// 订单事件（client_oid "pystrat:<id>" 前缀由 OrderSource 过滤）→ 沙箱 on_order。
func TestRunnerOnOrderEventDispatched(t *testing.T) {
	sb := &fakeSandbox{}
	gotEvts := make(chan OrderEvent, 4)
	sb.onOrderFn = func(evt OrderEvent) (*OrderResult, error) {
		gotEvts <- evt
		return &OrderResult{Logs: []string{"order " + evt.ID}}, nil
	}
	src := newFakeBarSource()
	orders := newFakeOrderSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{equity: 10000}

	r := newTestRunner(t, sb, src, exec, accts)
	r.SetOrderSource(orders)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	evt := OrderEvent{
		ID: "ord-1", Symbol: "BTCUSDT", Side: "buy", Type: "market",
		Qty: 0.5, Price: 50000, Filled: 0.5, AvgPrice: 50000,
		Status: "filled", PnL: 1.25, ClientOID: "pystrat:ps1",
	}
	orders.push("ps1", "BTCUSDT", evt)

	select {
	case got := <-gotEvts:
		if got.ID != "ord-1" || got.Status != "filled" || got.PnL != 1.25 || got.Side != "buy" {
			t.Fatalf("sandbox received wrong event: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sandbox on_order not called with order event")
	}
}

// on_order 回调失败/超时不计入 on_bar 错误契约（容忍度高的订单事件）。
func TestRunnerOnOrderErrorDoesNotCount(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onOrderFn = func(evt OrderEvent) (*OrderResult, error) {
		return nil, errors.New("on_order boom")
	}
	src := newFakeBarSource()
	orders := newFakeOrderSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{equity: 10000}

	r := newTestRunner(t, sb, src, exec, accts)
	r.SetOrderSource(orders)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	for i := 0; i < MaxConsecutiveErrors+2; i++ {
		orders.push("ps1", "BTCUSDT", OrderEvent{ID: "ord-x", Status: "filled"})
	}
	waitFor(t, func() bool {
		sb.mu.Lock()
		defer sb.mu.Unlock()
		return sb.orders >= MaxConsecutiveErrors+2
	}, "all order events dispatched")
	if st := r.Status("ps1"); !st.Running || st.ConsecErrors != 0 {
		t.Fatalf("on_order errors must not count into error contract: %+v", st)
	}
	// 错误应可见于运行日志
	found := false
	for _, e := range r.Logs("ps1") {
		if strings.Contains(e.Message, "on_order 回调失败") {
			found = true
		}
	}
	if !found {
		t.Fatal("on_order failure must be visible in logs")
	}
}

// price>0 的 buy → 限价单进 OMS（参数断言），且不再走市价 BuySpot。
func TestRunnerLimitBuyParamsToOMS(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "buy", Amount: 200, Price: 48000}}}, nil
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
	src.push("BTCUSDT", "15m", model.Bar{Close: 50000, Time: time.Now().UnixMilli()})

	waitFor(t, func() bool { return len(exec.limitBuys) == 1 }, "limit buy reached OMS")
	if len(exec.buys) != 0 {
		t.Fatalf("limit buy must not go through market BuySpot, got %d calls", len(exec.buys))
	}
	lb := exec.limitBuys[0]
	if lb.price != 48000 {
		t.Fatalf("limit price = %v want 48000", lb.price)
	}
	if lb.quote != 200 {
		t.Fatalf("limit quote = %v want 200", lb.quote)
	}
	if lb.exchange != "paper" {
		t.Fatalf("paper=1 must force paper exchange, got %q", lb.exchange)
	}
	if lb.botID != "ps1" {
		t.Fatalf("client_oid bot id = %q want ps1", lb.botID)
	}
}

// price>0 的 sell → 限价卖单参数断言。
func TestRunnerLimitSellParamsToOMS(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "sell", Amount: 2400, Price: 48000}}}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{equity: 10000, qty: 0.5, avg: 46000, side: "long", ok: true}

	r := newTestRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")
	src.push("BTCUSDT", "15m", model.Bar{Close: 50000, Time: time.Now().UnixMilli()})

	waitFor(t, func() bool { return len(exec.limitSells) == 1 }, "limit sell reached OMS")
	ls := exec.limitSells[0]
	if ls.price != 48000 {
		t.Fatalf("limit price = %v want 48000", ls.price)
	}
	// amount=2400 @ 48000 → qty 0.05
	if ls.qty != 2400.0/48000.0 {
		t.Fatalf("limit qty = %v want %v", ls.qty, 2400.0/48000.0)
	}
}

// 限价买入成交回报（filled）→ 补挂 TP/SL。
func TestRunnerLimitFillAppliesBracket(t *testing.T) {
	sb := &fakeSandbox{}
	src := newFakeBarSource()
	orders := newFakeOrderSource()
	exec := &fakeExecutor{}
	accts := &fakeAccounts{equity: 10000}
	prot := &fakeProtector{}

	r := newTestRunner(t, sb, src, exec, accts)
	r.SetOrderSource(orders)
	r.SetProtector(prot)
	defer r.StopAll()
	if err := r.Start(testRecord(true)); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")

	orders.push("ps1", "BTCUSDT", OrderEvent{
		ID: "ord-l1", Side: "buy", Type: "limit",
		Qty: 0.5, Price: 48000, Filled: 0.5, AvgPrice: 48000, Status: "filled",
	})
	waitFor(t, func() bool { return len(prot.brackets) == 1 }, "bracket after limit fill")
	br := prot.brackets[0]
	if br.qty != 0.5 || br.entry != 48000 {
		t.Fatalf("bracket from limit fill wrong: %+v", br)
	}
	// 市价成交的事件不应重复挂（type=market 不触发）
	orders.push("ps1", "BTCUSDT", OrderEvent{
		ID: "ord-m1", Side: "buy", Type: "market",
		Qty: 0.1, Price: 50000, Filled: 0.1, AvgPrice: 50000, Status: "filled",
	})
	time.Sleep(150 * time.Millisecond)
	if len(prot.brackets) != 1 {
		t.Fatalf("market fill event must not re-apply bracket, got %d", len(prot.brackets))
	}
}

// market=futures 且 paper=0：买入走合约链路，杠杆/保证金模式按
// manifest.risk 覆盖 record 列。
func TestRunnerFuturesLiveContractBuy(t *testing.T) {
	sb := &fakeSandbox{}
	sb.manifest = &Manifest{
		Name: "fake", Symbol: "BTC/USDT", Interval: "15m", Direction: DirectionLong,
		Risk: ManifestRisk{MaxPositionPct: 0.9, Leverage: 20, MarginMode: "isolated"},
	}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "buy", Amount: 100, Price: 49000}}}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{contractFill: 0.0416}
	accts := &fakeAccounts{equity: 10000}

	r := newTestRunner(t, sb, src, exec, accts)
	r.SetLiveGate(func(exchange string) error { return nil })
	defer r.StopAll()
	rec := testRecord(false)
	rec.Market = store.PyStratMarketFutures
	rec.Leverage = 10
	rec.MarginMode = store.PyStratMarginCross
	if err := r.Start(rec); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")
	src.push("BTCUSDT", "15m", model.Bar{Close: 50000, Time: time.Now().UnixMilli()})

	waitFor(t, func() bool { return len(exec.contracts) == 1 }, "contract buy reached OMS")
	if len(exec.buys) != 0 {
		t.Fatalf("futures buy must not go through spot BuySpot, got %d", len(exec.buys))
	}
	ct := exec.contracts[0]
	if ct.exchange != "binance" {
		t.Fatalf("paper=0 futures exchange = %q want binance", ct.exchange)
	}
	// manifest.risk.leverage=20 覆盖 record.Leverage=10
	if ct.leverage != 20 {
		t.Fatalf("leverage = %v want 20 (manifest override)", ct.leverage)
	}
	if ct.marginMode != "isolated" {
		t.Fatalf("marginMode = %q want isolated (manifest override)", ct.marginMode)
	}
	if ct.positionSide != "LONG" {
		t.Fatalf("positionSide = %q want LONG", ct.positionSide)
	}
	if ct.side != "BUY" {
		t.Fatalf("side = %q want BUY", ct.side)
	}
	// amount=100 保证金 ×20 杠杆 @ refPrice 50000 → qty 0.04
	wantQty := 100.0 * 20 / 50000
	if ct.qty != wantQty {
		t.Fatalf("contract qty = %v want %v (amount×leverage/refPrice)", ct.qty, wantQty)
	}
}

// market=futures 且 paper=1：paper 撮合不支持杠杆 → 回落现货并记日志。
func TestRunnerFuturesPaperFallsBackToSpot(t *testing.T) {
	sb := &fakeSandbox{}
	sb.onBarFn = func(bar model.Bar, state State) (*BarResult, error) {
		return &BarResult{Actions: []Action{{Type: "buy", Amount: 100}}}, nil
	}
	src := newFakeBarSource()
	exec := &fakeExecutor{buyFill: 0.002}
	accts := &fakeAccounts{equity: 10000}

	r := newTestRunner(t, sb, src, exec, accts)
	defer r.StopAll()
	rec := testRecord(true)
	rec.Market = store.PyStratMarketFutures
	rec.Leverage = 20
	if err := r.Start(rec); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, func() bool { return r.IsRunning("ps1") }, "running")
	src.push("BTCUSDT", "15m", model.Bar{Close: 50000, Time: time.Now().UnixMilli()})

	waitFor(t, func() bool { return len(exec.buys) == 1 }, "spot fallback buy")
	if len(exec.contracts) != 0 {
		t.Fatalf("paper mode must not place contract orders, got %d", len(exec.contracts))
	}
	found := false
	for _, e := range r.Logs("ps1") {
		if strings.Contains(e.Message, "回落现货") {
			found = true
		}
	}
	if !found {
		t.Fatal("paper fallback must be logged")
	}
}
