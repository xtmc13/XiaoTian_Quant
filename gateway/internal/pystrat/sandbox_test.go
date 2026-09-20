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
