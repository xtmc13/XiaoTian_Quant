package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
)

// 2026-10-11 事故回归：重启丢失运行时解锁后 live 策略不再被加载侧压回
// paper，其信号单必须在订单时被实盘闸（ctx.LiveGate，handler 注入
// canPlaceLiveOrder 同口径）拒绝，并经 fireSignalOrderRejected 回传拒绝
// 事件——策略回滚，不留幽灵持仓。
func TestSignalLiveGateRejectsWhenClosed(t *testing.T) {
	// binance 凭证使 resolveExchange 解析出真实交易所（execution_mode=live
	// 时 applyStrategyExecConfig 不再强制 paper，单直面实盘闸）。
	t.Setenv("BINANCE_API_KEY", "test-key")
	t.Setenv("BINANCE_API_SECRET", "test-secret")

	ctx := setupPaperCloseTest(t, paper.AccountSnapshot{Enabled: false})
	om := order.GetOrderManager()
	watcher := &rejectWatcher{}
	watcher.wrap(t, om)

	ctx.LiveGate = func(exchange string) error {
		return fmt.Errorf("实盘交易未启用。请在 config.yaml 设置 trading.live_enabled: true（或 .env 中 LIVE_TRADING_ENABLED=true / 管理员运行时解锁）并重启网关")
	}
	t.Cleanup(func() { ctx.LiveGate = nil })

	const cid = "siglive-gate"
	const symbol = "SOLUSDT"
	prefix := "sig:" + cid + ":"
	store.SetStrategyConfig(cid, map[string]any{
		"id": cid, "name": cid, "strategy_type": "cra_contract",
		"symbol": symbol, "status": "running", "execution_mode": "live",
		"config_json": `{"market_type":"swap","direction":"dual","leverage":100,"first_order_amount":100,"tp_mode":"static","take_profit_ratio":0.5}`,
	})
	t.Cleanup(func() {
		store.DeleteStrategyConfig(cid)
		_ = store.NewStrategyConfigRepo().Delete(cid)
	})

	eng := ctx.StrategyEngine
	wrapped := strategy.WrapStrategy(cid, cra.NewCRAContractStrategy("cra_contract", symbol))
	if err := eng.Register(wrapped); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() {
		_ = eng.Stop(cid)
		_ = eng.Unregister(cid)
	})
	if err := eng.Start(cid, map[string]any{
		"symbol": symbol, "direction": "dual", "leverage": 100,
		"first_order_amount": 100, "tp_mode": "static", "take_profit_ratio": 0.5,
	}); err != nil {
		t.Fatalf("engine start: %v", err)
	}

	feedSignalBar(t, ctx, symbol, 110.61, time.Now().UnixMilli())

	// 实盘闸拒绝事件恰好一次，按原样 client_oid 打标回传；订单未创建不落库。
	evs := watcher.forPrefix(prefix)
	if len(evs) != 1 || evs[0].Status != model.StatusRejected {
		t.Fatalf("rejected events = %+v, want exactly one REJECTED", evs)
	}
	if evs[0].Exchange != "binance" {
		t.Fatalf("rejected event exchange = %q, want binance（实盘意图如实呈现）", evs[0].Exchange)
	}
	if om.GetOrder(evs[0].ID) != nil {
		t.Fatalf("gate rejection must not persist an order: %s", evs[0].ID)
	}

	sealRejected(t, ctx, cid, evs[0])
	st, _ := ctx.StrategyEngine.RuntimeStatus(cid)
	if st["in_position"] != false || st["loops_executed"] != 0 {
		t.Fatalf("ghost position after live-gate reject: %v", st)
	}
}
