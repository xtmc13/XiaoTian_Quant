package strategy

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/protection"
)

// alwaysBlockProtection 恒定阻断的测试 protection（验证策略级阻断链路）。
type alwaysBlockProtection struct{ reason string }

func (p alwaysBlockProtection) Name() string        { return "AlwaysBlock" }
func (p alwaysBlockProtection) Description() string { return "test: always blocks" }
func (p alwaysBlockProtection) Check(protection.ProtectionContext) protection.ProtectionResult {
	return protection.ProtectionResult{Blocked: true, Reason: p.reason}
}
func (p alwaysBlockProtection) Validate() error { return nil }
func (p alwaysBlockProtection) Reset()          {}

func blockingManager(t *testing.T, reason string) *protection.ProtectionManager {
	t.Helper()
	mgr := protection.NewProtectionManager()
	if err := mgr.AddProtection(alwaysBlockProtection{reason: reason}); err != nil {
		t.Fatalf("add protection: %v", err)
	}
	return mgr
}

// TestStrategyProtectionBlocksOnlyOwnStrategy 策略级 manager 只阻断所属策略的信号，
// 其他策略不受影响（隔离性）；Unregister 后释放。
func TestStrategyProtectionBlocksOnlyOwnStrategy(t *testing.T) {
	eng := newHookTestEngine()
	s1 := &hookFake{name: "cfg_a", running: true}
	s2 := &hookFake{name: "cfg_b", running: true}
	var got []model.Signal
	eng.OnSignal = func(sig model.Signal) { got = append(got, sig) }

	eng.SetStrategyProtectionManager("cfg_a", blockingManager(t, "strategy A cooldown"))
	if n := eng.StrategyProtectionCount("cfg_a"); n != 1 {
		t.Fatalf("cfg_a strategy protections: %d", n)
	}

	sig := func() *model.Signal { return &model.Signal{Symbol: "BTCUSDT", Direction: "LONG", Qty: 0.1} }
	eng.emitSignal(s1, sig(), nil)
	if len(got) != 0 {
		t.Fatalf("cfg_a signal must be blocked by strategy-level protection, got %d", len(got))
	}
	eng.emitSignal(s2, sig(), nil)
	if len(got) != 1 || got[0].Strategy != "cfg_b" {
		t.Fatalf("cfg_b signal must pass (isolation), got %+v", got)
	}

	// 注销释放策略级 manager
	if err := eng.Register(s1); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := eng.Unregister("cfg_a"); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if n := eng.StrategyProtectionCount("cfg_a"); n != 0 {
		t.Fatalf("strategy protections must be released on Unregister, got %d", n)
	}
}

// TestStrategyProtectionBeforeGlobal 策略级先于引擎全局：两层都配时策略级拦截
// （全局层不阻断也拦下）；仅全局层配置时全局兜底。
func TestStrategyProtectionBeforeGlobal(t *testing.T) {
	eng := newHookTestEngine()
	s := &hookFake{name: "cfg_l", running: true}
	var got []model.Signal
	eng.OnSignal = func(sig model.Signal) { got = append(got, sig) }
	sig := func() *model.Signal { return &model.Signal{Symbol: "BTCUSDT", Direction: "LONG", Qty: 0.1} }

	// 仅全局阻断（现状回归：全局 manager 仍生效）
	eng.SetProtectionManager(blockingManager(t, "global block"))
	eng.emitSignal(s, sig(), nil)
	if len(got) != 0 {
		t.Fatalf("global protection must block signal")
	}

	// 全局放行 + 策略级阻断 → 仍拦截（策略级独立生效）
	eng2 := newHookTestEngine()
	eng2.OnSignal = func(sig model.Signal) { got = append(got, sig) }
	eng2.SetProtectionManager(protection.NewProtectionManager()) // 空全局
	eng2.SetStrategyProtectionManager("cfg_l", blockingManager(t, "strategy block"))
	eng2.emitSignal(s, sig(), nil)
	if len(got) != 0 {
		t.Fatalf("strategy-level protection must block even with empty global manager")
	}

	// 清除策略级（重载后 config_json 不再含 protections）→ 信号恢复
	eng2.SetStrategyProtectionManager("cfg_l", nil)
	eng2.emitSignal(s, sig(), nil)
	if len(got) != 1 {
		t.Fatalf("cleared strategy protection should let signal pass, got %d", len(got))
	}
}
