package handler

import (
	"strconv"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/store"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/cra"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

// ── H1 片（2026-10-09）：合约空单（short）重启仓位重建 ──
//
// 闸门从"仅净多"扩展为带符号净额：净空（<0，swap 空单先 SELL 开仓）按
// restored_position_side=short 注入；paper 背书按方向取镜像空头量；
// 现货策略拒绝负净额注入（WARN 空仓起步）。

// seedStrategyLedgerFillN 同 seedStrategyLedgerFill，但带序号后缀支持同一策略
// 多笔同向成交（既有helper 的订单 ID 不含序号，同策略同向会撞主键）。
func seedStrategyLedgerFillN(t *testing.T, strategyID, symbol, side string, qty, price float64, seq int) {
	t.Helper()
	nowMs := time.Now().UnixMilli()
	rec := &store.OrderRecord{
		ID: "ord-restore-" + strategyID + side + "-" + strconv.Itoa(seq), Symbol: symbol, Side: side,
		OrderType: "MARKET", Quantity: qty, Filled: qty, Status: "FILLED",
		Exchange: "paper", ClientOID: "sig:" + strategyID + ":seed" + strconv.Itoa(seq),
		AvgFillPrice: price, CreatedAt: nowMs, UpdatedAt: nowMs,
	}
	if err := store.NewOrderRepo().Create(rec); err != nil {
		t.Fatalf("seed ledger fill: %v", err)
	}
}

// paperSnapWithShort 构造带空头镜像的 paper 账户快照（合约空单：base 记负
// 负债 + quote 持有开空回款）。
func paperSnapWithShort(symbol, asset string, shortQty, entry float64) paper.AccountSnapshot {
	usdt := 96119.0
	return paper.AccountSnapshot{
		Enabled: true, Balance: usdt, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: usdt, Free: usdt},
			asset:  {Currency: asset, Total: -shortQty, Free: -shortQty},
		},
		Positions: []paper.PositionSnapshot{{Data: model.PositionData{
			ID: symbol + "-spot", Symbol: symbol, Side: "SHORT",
			Quantity: -shortQty, AvgEntryPrice: entry,
		}}},
	}
}

// 空单背书钳制三态：足额镜像原样注入 / 不足钳到镜像 / 零背书（无空头或只有
// 多头镜像）不注入。
func TestClampRestoredQtyByPaperBackingShort(t *testing.T) {
	// 足额：镜像空头 16.82 ≥ 账本净空 12.52 → 原样注入 12.52。
	withPaperAccount(t, paperSnapWithShort("SOLUSDT", "SOL", 16.824301, 119.5))
	qty, ok := clampRestoredQtyByPaperBacking("SOLUSDT", -12.519152)
	if !ok || qty != 12.519152 {
		t.Fatalf("足额空头背书: qty=%v ok=%v, want 12.519152/true", qty, ok)
	}

	// 不足：镜像空头 5.0 → 钳到 5.0。
	withPaperAccount(t, paperSnapWithShort("SOLUSDT", "SOL", 5.0, 119.5))
	qty, ok = clampRestoredQtyByPaperBacking("SOLUSDT", -12.519152)
	if !ok || qty != 5.0 {
		t.Fatalf("不足空头背书: qty=%v ok=%v, want 5.0/true", qty, ok)
	}

	// 零背书：账户无持仓（空头已平/从未开过）→ 不注入。
	withPaperAccount(t, paperSnapWithBase("SOL", 0))
	if qty, ok = clampRestoredQtyByPaperBacking("SOLUSDT", -12.519152); ok || qty != 0 {
		t.Fatalf("零背书: qty=%v ok=%v, want 0/false", qty, ok)
	}

	// 多头镜像不是空单背书：账户只有多头持仓（16.82 LONG）→ 不注入。
	snap := paperSnapWithBase("SOL", 16.82)
	snap.Positions = []paper.PositionSnapshot{{Data: model.PositionData{
		ID: "SOLUSDT-spot", Symbol: "SOLUSDT", Side: "LONG", Quantity: 16.82, AvgEntryPrice: 119,
	}}}
	withPaperAccount(t, snap)
	if qty, ok = clampRestoredQtyByPaperBacking("SOLUSDT", -12.519152); ok || qty != 0 {
		t.Fatalf("多头镜像不得当空单背书: qty=%v ok=%v, want 0/false", qty, ok)
	}
}

// 端到端：MACD 合约配置（config_json market_type=swap）+ 账本净空 + paper
// 空头镜像 → 恢复为 SHORT 仓继续管理。
func TestRestoreInjectionShortMACDContract(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("macd", func() strategy.Strategy { return strategies.NewMACDStrategy() })

	withPaperAccount(t, paperSnapWithShort("SOLUSDT", "SOL", 12.5, 119.5))
	// 账本：开空 8.5@120 + 补空 4@118.67 → 净空 12.5（先 SELL 开仓）。
	seedStrategyLedgerFill(t, "rst-short-macd", "SOLUSDT", "SELL", 8.5, 120)
	seedStrategyLedgerFillN(t, "rst-short-macd", "SOLUSDT", "SELL", 4.0, 118.67, 2)
	item := map[string]any{
		"id": "rst-short-macd", "strategy_type": "macd", "symbol": "SOLUSDT",
		"execution_mode": "paper", "config_json": `{"market_type":"swap","leverage":5}`, "timeframe": "15m",
	}
	if err := startStrategyInEngine("rst-short-macd", item); err != nil {
		t.Fatalf("startStrategyInEngine: %v", err)
	}
	t.Cleanup(func() { stopStrategyInEngine("rst-short-macd") })

	rs, ok := eng.RuntimeStatus("rst-short-macd")
	if !ok {
		t.Fatal("RuntimeStatus missing")
	}
	if rs["in_position"] != true || rs["direction"] != "SHORT" {
		t.Fatalf("restored = %+v, want in_position/SHORT", rs)
	}
	// VWAP 取卖出（开仓）侧加权：(8.5×120+4×118.67)/12.5 ≈ 119.5744。
	entry, _ := rs["entry_price"].(float64)
	want := (8.5*120 + 4*118.67) / 12.5
	if entry < want-0.001 || entry > want+0.001 {
		t.Fatalf("entry_price = %v, want ≈%v（开空 VWAP）", entry, want)
	}
}

// 现货策略拒绝负净额注入：MACD 现货配置（config_json 无 market_type）账本
// 出现净空（异常/污染）→ 不注入、空仓起步（绝不把空单恢复成多单）。
func TestRestoreInjectionShortSpotRefused(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("macd", func() strategy.Strategy { return strategies.NewMACDStrategy() })

	// 账户甚至有空头镜像也不注入——现货策略无空单语义。
	withPaperAccount(t, paperSnapWithShort("SOLUSDT", "SOL", 12.5, 119.5))
	seedStrategyLedgerFill(t, "rst-short-spot", "SOLUSDT", "SELL", 12.5, 119.5)
	item := map[string]any{
		"id": "rst-short-spot", "strategy_type": "macd", "symbol": "SOLUSDT",
		"execution_mode": "paper", "config_json": "{}", "timeframe": "15m",
	}
	if err := startStrategyInEngine("rst-short-spot", item); err != nil {
		t.Fatalf("startStrategyInEngine: %v", err)
	}
	t.Cleanup(func() { stopStrategyInEngine("rst-short-spot") })

	rs, ok := eng.RuntimeStatus("rst-short-spot")
	if !ok {
		t.Fatal("RuntimeStatus missing")
	}
	if rs["in_position"] == true {
		t.Fatalf("现货策略负净额必须拒绝注入（空仓起步）: %+v", rs)
	}
}

// 零背书空单不注入：合约配置 + 账本净空，但 paper 账户无空头镜像（仓位已被
// 手工平掉/账户重置）→ 空仓起步，杜绝"引擎以为有空仓、账户没有"的僵尸态。
func TestRestoreInjectionShortZeroBacking(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("macd", func() strategy.Strategy { return strategies.NewMACDStrategy() })

	withPaperAccount(t, paperSnapWithBase("SOL", 0))
	seedStrategyLedgerFill(t, "rst-short-zero", "SOLUSDT", "SELL", 12.5, 119.5)
	item := map[string]any{
		"id": "rst-short-zero", "strategy_type": "macd", "symbol": "SOLUSDT",
		"execution_mode": "paper", "config_json": `{"market_type":"swap"}`, "timeframe": "15m",
	}
	if err := startStrategyInEngine("rst-short-zero", item); err != nil {
		t.Fatalf("startStrategyInEngine: %v", err)
	}
	t.Cleanup(func() { stopStrategyInEngine("rst-short-zero") })

	rs, ok := eng.RuntimeStatus("rst-short-zero")
	if !ok {
		t.Fatal("RuntimeStatus missing")
	}
	if rs["in_position"] == true {
		t.Fatalf("零背书空单不得注入: %+v", rs)
	}
}

// dual 分账口径端到端：CRA dual 合约，账本 = 循环1 多仓全平（BUY→SELL 抵消）
// + 循环2 空仓在持（SELL 0.4）→ 净额 -0.4 恢复为空仓，逐笔重放方向=short、
// 成本=循环2 开空价 104（聚合 VWAP 混杂历史循环，分档重放精确）。
func TestRestoreInjectionCRADualShortLoop(t *testing.T) {
	eng := strategy.GetEngine(event.NewEventBus(4096, 1))
	strategy.RegisterStrategyFactory("cra_contract", func() strategy.Strategy {
		return cra.NewCRAContractStrategy("cra_contract", "SOLUSDT")
	})

	withPaperAccount(t, paperSnapWithShort("SOLUSDT", "SOL", 0.4, 104))
	seedStrategyLedgerFill(t, "rst-dual", "SOLUSDT", "BUY", 0.5, 100)
	seedStrategyLedgerFillN(t, "rst-dual", "SOLUSDT", "SELL", 0.5, 103, 2)
	seedStrategyLedgerFillN(t, "rst-dual", "SOLUSDT", "SELL", 0.4, 104, 3)
	item := map[string]any{
		"id": "rst-dual", "strategy_type": "cra_contract", "symbol": "SOLUSDT",
		"execution_mode": "paper", "timeframe": "15m",
		"config_json": `{"direction":"dual","market_type":"swap","leverage":5,"first_order_amount":10,"tp_mode":"static","take_profit_ratio":0.5}`,
	}
	if err := startStrategyInEngine("rst-dual", item); err != nil {
		t.Fatalf("startStrategyInEngine: %v", err)
	}
	t.Cleanup(func() { stopStrategyInEngine("rst-dual") })

	rs, ok := eng.RuntimeStatus("rst-dual")
	if !ok {
		t.Fatal("RuntimeStatus missing")
	}
	if rs["in_position"] != true || rs["direction"] != "short" {
		t.Fatalf("dual 循环2 空仓必须恢复: %+v", rs)
	}
	if entry, _ := rs["entry_price"].(float64); entry != 104 {
		t.Fatalf("entry_price = %v, want 104（当前循环开空价，逐笔重放）", rs["entry_price"])
	}
	if qty, _ := rs["position_qty"].(float64); qty != 0.4 {
		t.Fatalf("position_qty = %v, want 0.4", rs["position_qty"])
	}
}
