package pystrat

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// requirePython3 跳过无 python3 的环境。
func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available, skipping subprocess smoke test")
	}
}

// 端到端冒烟：真实子进程 load + on_bar + 动作回收 + 超时被杀。
func TestSubprocessSandboxEndToEnd(t *testing.T) {
	requirePython3(t)
	sb := NewSubprocessSandbox()
	sb.CallTimeout = 5 * time.Second
	defer sb.Close()

	code := `
import math

STRATEGY_MANIFEST = {"name": "e2e", "symbol": "BTC/USDT", "interval": "15m", "direction": "long",
  "params": {"n": 0}, "risk": {"max_position_pct": 0.5}}

def initialize(context):
    context.seen = 0
    context.log("init ok")

def on_bar(context, bar):
    context.seen += 1
    context.log("bar %d close=%s" % (context.seen, bar["close"]))
    if context.seen == 2:
        context.buy(amount=100)
        context.set_stop_loss(0.03)
`
	manifest, err := sb.Load(context.Background(), code, map[string]any{"n": 1}, "BTCUSDT", "15m")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if manifest.Name != "e2e" || manifest.Interval != "15m" {
		t.Fatalf("manifest mismatch: %+v", manifest)
	}

	state := State{Equity: 10000, HasPosition: false}
	res, err := sb.OnBar(context.Background(), model.Bar{Close: 50000, Time: 1}, state)
	if err != nil {
		t.Fatalf("on_bar 1: %v", err)
	}
	if len(res.Logs) == 0 {
		t.Fatal("expected init/bar logs")
	}

	res, err = sb.OnBar(context.Background(), model.Bar{Close: 51000, Time: 2}, state)
	if err != nil {
		t.Fatalf("on_bar 2: %v", err)
	}
	var types []string
	for _, a := range res.Actions {
		types = append(types, a.Type)
	}
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, "buy") || !strings.Contains(joined, "set_stop_loss") {
		t.Fatalf("expected buy+set_stop_loss actions, got %v", res.Actions)
	}

	// 状态隔离：跨调用变量保持（context.seen 持久）
	res, err = sb.OnBar(context.Background(), model.Bar{Close: 52000, Time: 3}, state)
	if err != nil {
		t.Fatalf("on_bar 3: %v", err)
	}
	if len(res.Actions) != 0 {
		t.Fatalf("bar3 should have no actions, got %v", res.Actions)
	}
}

// import 白名单外模块在沙箱加载期被拒（AST 终审）。
func TestSubprocessSandboxRejectsUnsafeImport(t *testing.T) {
	requirePython3(t)
	sb := NewSubprocessSandbox()
	defer sb.Close()

	code := "import os\n" + validStrategy
	_, err := sb.Load(context.Background(), code, nil, "BTCUSDT", "15m")
	if err == nil || !strings.Contains(err.Error(), "os") {
		t.Fatalf("expected os import rejection, got %v", err)
	}
}

// on_bar 异常以错误返回（runner 错误契约的输入）。
func TestSubprocessSandboxOnBarError(t *testing.T) {
	requirePython3(t)
	sb := NewSubprocessSandbox()
	defer sb.Close()

	code := strings.Replace(validStrategy,
		"def on_bar(context, bar):\n    pass",
		"def on_bar(context, bar):\n    raise ValueError('kaboom')", 1)
	if _, err := sb.Load(context.Background(), code, nil, "BTCUSDT", "15m"); err != nil {
		t.Fatalf("load: %v", err)
	}
	_, err := sb.OnBar(context.Background(), model.Bar{Close: 1}, State{Equity: 1})
	if err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("expected on_bar error surfacing, got %v", err)
	}
	// 异常后子进程仍存活（下一轮可继续）
	if !sb.Alive() {
		t.Fatal("sandbox must survive strategy exceptions")
	}
}

// 死循环 on_bar → 单轮超时 → 子进程被杀 → ErrSandboxDead。
func TestSubprocessSandboxTimeoutKills(t *testing.T) {
	requirePython3(t)
	sb := NewSubprocessSandbox()
	sb.CallTimeout = 500 * time.Millisecond
	defer sb.Close()

	code := strings.Replace(validStrategy,
		"def on_bar(context, bar):\n    pass",
		"def on_bar(context, bar):\n    while True:\n        pass", 1)
	if _, err := sb.Load(context.Background(), code, nil, "BTCUSDT", "15m"); err != nil {
		t.Fatalf("load: %v", err)
	}
	start := time.Now()
	_, err := sb.OnBar(context.Background(), model.Bar{Close: 1}, State{Equity: 1})
	if !errors.Is(err, ErrSandboxDead) {
		t.Fatalf("expected ErrSandboxDead, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout kill too slow: %v", elapsed)
	}
	if sb.Alive() {
		t.Fatal("sandbox must be marked dead after timeout")
	}
}

// on_order 端到端：策略定义 on_order → 收到订单回报并可见 logs。
func TestSubprocessSandboxOnOrder(t *testing.T) {
	requirePython3(t)
	sb := NewSubprocessSandbox()
	defer sb.Close()

	code := validStrategy + `
def on_order(context, order):
    context.log("order %s status=%s pnl=%s" % (order["id"], order["status"], order.get("pnl")))
`
	manifest, err := sb.Load(context.Background(), code, nil, "BTCUSDT", "15m")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if manifest.Name != "t" {
		t.Fatalf("manifest mismatch: %+v", manifest)
	}
	res, err := sb.OnOrder(context.Background(), OrderEvent{
		ID: "ord-9", Symbol: "BTCUSDT", Side: "buy", Type: "limit",
		Qty: 0.5, Price: 48000, Filled: 0.5, AvgPrice: 48000,
		Status: "filled", PnL: 3.5,
	})
	if err != nil {
		t.Fatalf("on_order: %v", err)
	}
	joined := strings.Join(res.Logs, "\n")
	if !strings.Contains(joined, "ord-9") || !strings.Contains(joined, "filled") || !strings.Contains(joined, "3.5") {
		t.Fatalf("on_order logs must carry order payload, got %v", res.Logs)
	}
}

// 策略未定义 on_order → 跳过（ok=true, skipped 语义），子进程不受影响。
func TestSubprocessSandboxOnOrderUndefinedSkips(t *testing.T) {
	requirePython3(t)
	sb := NewSubprocessSandbox()
	defer sb.Close()
	if _, err := sb.Load(context.Background(), validStrategy, nil, "BTCUSDT", "15m"); err != nil {
		t.Fatalf("load: %v", err)
	}
	res, err := sb.OnOrder(context.Background(), OrderEvent{ID: "ord-1", Status: "filled"})
	if err != nil {
		t.Fatalf("on_order without callback must skip, got error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	// 后续 on_bar 照常工作
	if _, err := sb.OnBar(context.Background(), model.Bar{Close: 1}, State{Equity: 1}); err != nil {
		t.Fatalf("on_bar after skipped on_order: %v", err)
	}
}

// on_order 内下单动作被丢弃并记 warning；回调异常以错误返回但子进程存活。
func TestSubprocessSandboxOnOrderDiscardsActions(t *testing.T) {
	requirePython3(t)
	sb := NewSubprocessSandbox()
	defer sb.Close()
	code := validStrategy + `
def on_order(context, order):
    context.buy(amount=100)
    context.log("saw order")
`
	if _, err := sb.Load(context.Background(), code, nil, "BTCUSDT", "15m"); err != nil {
		t.Fatalf("load: %v", err)
	}
	res, err := sb.OnOrder(context.Background(), OrderEvent{ID: "ord-2", Status: "filled"})
	if err != nil {
		t.Fatalf("on_order: %v", err)
	}
	joined := strings.Join(res.Logs, "\n")
	if !strings.Contains(joined, "已忽略") {
		t.Fatalf("actions in on_order must be discarded with warning, got %v", res.Logs)
	}
}

// on_order 超时：不杀子进程（与 on_bar 超时策略不同），返回 ErrOnOrderTimeout，
// 后续 on_bar 照常（迟到响应被孤儿机制消费，协议不串位）。
func TestSubprocessSandboxOnOrderTimeoutDoesNotKill(t *testing.T) {
	requirePython3(t)
	sb := NewSubprocessSandbox()
	sb.CallTimeout = 500 * time.Millisecond
	defer sb.Close()
	// datetime 在白名单内；用墙钟忙等 1.2s（必超 500ms 调用超时，又在测试
	// 轮询窗口内完成，保证孤儿能消费到迟到响应）。
	code := validStrategy + `
import datetime

def on_order(context, order):
    deadline = datetime.datetime.now() + datetime.timedelta(seconds=1.2)
    while datetime.datetime.now() < deadline:
        pass
    context.log("late response arrived")
`
	if _, err := sb.Load(context.Background(), code, nil, "BTCUSDT", "15m"); err != nil {
		t.Fatalf("load: %v", err)
	}
	start := time.Now()
	_, err := sb.OnOrder(context.Background(), OrderEvent{ID: "ord-slow", Status: "filled"})
	if !errors.Is(err, ErrOnOrderTimeout) {
		t.Fatalf("expected ErrOnOrderTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("on_order timeout wait too slow: %v", elapsed)
	}
	if !sb.Alive() {
		t.Fatal("on_order timeout must NOT kill the sandbox")
	}
	// 等孤儿消费迟到响应（worker 忙等 1.2s 必完成），随后 on_bar 必须正常。
	// 注意不能用 on_bar 轮询：waitLate 超时也会杀进程。
	time.Sleep(1500 * time.Millisecond)
	res, err := sb.OnBar(context.Background(), model.Bar{Close: 1}, State{Equity: 1})
	if err != nil {
		t.Fatalf("on_bar after on_order timeout: %v", err)
	}
	_ = res
}
