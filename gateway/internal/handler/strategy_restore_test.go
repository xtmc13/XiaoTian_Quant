package handler

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

// 重启恢复注入的余额背书（2026-10-08 生产实锤修复）：
// 恢复注入只写策略引擎状态，平仓成交取决于 paper 账户真实可卖余额——
// 注入量必须按 paper 账户 free 余额/镜像持仓钳制，背书为零不注入。

// withPaperAccount 以指定快照覆盖 paper 账户单例，测试结束还原。
func withPaperAccount(t *testing.T, snap paper.AccountSnapshot) {
	t.Helper()
	pe := paper.GetPaperExchange()
	orig := pe.SnapshotAccount()
	pe.RestoreAccount(snap)
	t.Cleanup(func() { pe.RestoreAccount(orig) })
}

func paperSnapWithBase(asset string, amount float64) paper.AccountSnapshot {
	return paper.AccountSnapshot{
		Enabled: true, Balance: 96119, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 96119, Free: 96119},
			asset:  {Currency: asset, Total: amount, Free: amount},
		},
	}
}

// 三态钳制：足额背书原样注入 / 不足钳到背书 / 零背书不注入。
func TestClampRestoredQtyByPaperBacking(t *testing.T) {
	// 足额：SOL free 16.82 ≥ 账本净额 12.52 → 原样。
	withPaperAccount(t, paperSnapWithBase("SOL", 16.824301207189286))
	qty, ok := clampRestoredQtyByPaperBacking("SOLUSDT", 12.519152)
	if !ok || qty != 12.519152 {
		t.Fatalf("足额背书: qty=%v ok=%v, want 12.519152/true", qty, ok)
	}

	// 不足：SOL free 5.0 < 12.52 → 钳到 5.0。
	withPaperAccount(t, paperSnapWithBase("SOL", 5.0))
	qty, ok = clampRestoredQtyByPaperBacking("SOLUSDT", 12.519152)
	if !ok || qty != 5.0 {
		t.Fatalf("不足背书: qty=%v ok=%v, want 5.0/true", qty, ok)
	}

	// 镜像持仓比 free 更小时按镜像持仓钳（挂单锁走了一部分）。
	snap := paperSnapWithBase("SOL", 16.82)
	snap.Positions = []paper.PositionSnapshot{{Data: model.PositionData{
		ID: "SOLUSDT-spot", Symbol: "SOLUSDT", Quantity: 8.0, AvgEntryPrice: 119,
	}}}
	withPaperAccount(t, snap)
	qty, ok = clampRestoredQtyByPaperBacking("SOLUSDT", 12.519152)
	if !ok || qty != 8.0 {
		t.Fatalf("镜像持仓钳制: qty=%v ok=%v, want 8.0/true", qty, ok)
	}

	// 零背书：账户没有 SOL → 不注入。
	withPaperAccount(t, paper.AccountSnapshot{
		Enabled: true, Balance: 96119, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 96119, Free: 96119},
		},
	})
	if qty, ok = clampRestoredQtyByPaperBacking("SOLUSDT", 12.519152); ok || qty != 0 {
		t.Fatalf("零背书: qty=%v ok=%v, want 0/false", qty, ok)
	}
}

// seedStrategyLedgerFill 向账本写一笔该策略的已成交单（重启重建数据源）。
func seedStrategyLedgerFill(t *testing.T, strategyID, symbol, side string, qty, price float64) {
	t.Helper()
	nowMs := time.Now().UnixMilli()
	rec := &store.OrderRecord{
		ID: "ord-restore-" + strategyID + side, Symbol: symbol, Side: side,
		OrderType: "MARKET", Quantity: qty, Filled: qty, Status: "FILLED",
		Exchange: "paper", ClientOID: "sig:" + strategyID + ":seed",
		AvgFillPrice: price, CreatedAt: nowMs, UpdatedAt: nowMs,
	}
	if err := store.NewOrderRepo().Create(rec); err != nil {
		t.Fatalf("seed ledger fill: %v", err)
	}
}

func startMACDForRestoreTest(t *testing.T, id string) {
	t.Helper()
	strategy.RegisterStrategyFactory("macd", func() strategy.Strategy { return strategies.NewMACDStrategy() })
	item := map[string]any{
		"id": id, "strategy_type": "macd", "symbol": "SOLUSDT",
		"execution_mode": "paper", "config_json": "{}", "timeframe": "15m",
	}
	if err := startStrategyInEngine(id, item); err != nil {
		t.Fatalf("startStrategyInEngine: %v", err)
	}
	t.Cleanup(func() { stopStrategyInEngine(id) })
}

func macdInPosition(t *testing.T, eng *strategy.Engine, id string) (inPos bool, entry float64) {
	t.Helper()
	rs, ok := eng.RuntimeStatus(id)
	if !ok {
		t.Fatalf("RuntimeStatus missing for %s", id)
	}
	inPos, _ = rs["in_position"].(bool)
	entry, _ = rs["entry_price"].(float64)
	return inPos, entry
}

// 集成：paper 模式恢复注入三态——
//
//	足额背书 → in_position=true 且按账本 VWAP 管理（可正常平仓）；
//	背书被钳 → 仍注入（引擎状态与账户可卖量一致）；
//	零背书 → in_position=false，策略空仓起步，绝不出现僵尸仓。
func TestRestoreInjectionPaperBackedThreeStates(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))

	// ── 足额背书：SOL 16.82 ≥ 净额 12.519152 → 完整恢复 ──
	withPaperAccount(t, paperSnapWithBase("SOL", 16.824301207189286))
	seedStrategyLedgerFill(t, "rst-full", "SOLUSDT", "BUY", 12.519152, 119.64)
	startMACDForRestoreTest(t, "rst-full")
	inPos, entry := macdInPosition(t, eng, "rst-full")
	if !inPos || entry != 119.64 {
		t.Fatalf("足额背书: in_position=%v entry=%v, want true/119.64", inPos, entry)
	}

	// ── 背书被钳：SOL 5.0 < 净额 12.52 → 仍注入（量被钳，引擎状态一致）──
	withPaperAccount(t, paperSnapWithBase("SOL", 5.0))
	seedStrategyLedgerFill(t, "rst-clamped", "SOLUSDT", "BUY", 12.519152, 119.64)
	startMACDForRestoreTest(t, "rst-clamped")
	inPos, entry = macdInPosition(t, eng, "rst-clamped")
	if !inPos || entry != 119.64 {
		t.Fatalf("钳制背书: in_position=%v entry=%v, want true/119.64", inPos, entry)
	}

	// ── 零背书：账户无 SOL → 不注入，空仓起步（修复前：in_position=true
	// 僵尸仓，平仓全被拒卡死）──
	withPaperAccount(t, paper.AccountSnapshot{
		Enabled: true, Balance: 96119, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 96119, Free: 96119},
		},
	})
	seedStrategyLedgerFill(t, "rst-skip", "SOLUSDT", "BUY", 12.519152, 119.64)
	startMACDForRestoreTest(t, "rst-skip")
	inPos, _ = macdInPosition(t, eng, "rst-skip")
	if inPos {
		t.Fatal("零背书: in_position=true —— 僵尸仓回归（账户没有资产引擎却以为有仓）")
	}
}
