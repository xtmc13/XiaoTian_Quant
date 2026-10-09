package app

import (
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── H1 片（2026-10-09）：重启恢复的合约空单，CLOSE 信号的账本兜底平仓 ──
//
// 重启后 PortfolioManager 内存镜像为空（base/持仓不重建——2a5ddcb 实锤），
// 恢复空单的平仓走 closePositionFromSignal 的账本兜底分支：净额<0 必须按
// BUY 买回平仓（此前 qty>0 才处理且固定 SELL——空单 CLOSE 完全空转）。

func shortSolAccountSnap(shortQty, entry float64) paper.AccountSnapshot {
	return paper.AccountSnapshot{
		Enabled: true, Balance: 96119, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 96119, Free: 96119},
			"SOL":  {Currency: "SOL", Total: -shortQty, Free: -shortQty},
		},
		Positions: []paper.PositionSnapshot{{Data: model.PositionData{
			ID: "SOLUSDT-spot", Symbol: "SOLUSDT", Side: "SHORT",
			Quantity: -shortQty, AvgEntryPrice: entry,
		}}},
	}
}

func seedShortTestLedger(t *testing.T, strategyID, symbol string, qty, price float64) {
	t.Helper()
	nowMs := time.Now().UnixMilli()
	if err := store.NewOrderRepo().Create(&store.OrderRecord{
		ID: "ord-short-close-seed-" + strategyID, Symbol: symbol, Side: "SELL",
		OrderType: "MARKET", Quantity: qty, Filled: qty, Status: "FILLED",
		Exchange: "paper", ClientOID: "sig:" + strategyID + ":seed",
		AvgFillPrice: price, CreatedAt: nowMs, UpdatedAt: nowMs,
	}); err != nil {
		t.Fatalf("seed short ledger: %v", err)
	}
}

// 恢复空单 → CLOSE 信号 → BUY 买回成交：镜像空头归零、base 负债归零、
// 订单带 swap/SHORT 键（对冲模式语义），无 REJECTED。
func TestClosePositionFromSignalShortAfterRestore(t *testing.T) {
	ctx := setupPaperCloseTest(t, shortSolAccountSnap(12.5, 119.64))
	seedShortTestLedger(t, "rst-short-close", "SOLUSDT", 12.5, 119.64)
	store.SetStrategyConfig("rst-short-close", map[string]any{
		"id": "rst-short-close", "strategy_type": "macd", "symbol": "SOLUSDT",
		"execution_mode": "paper", "market_type": "swap", "direction": "dual",
		"config_json": `{"market_type":"swap","leverage":5,"direction":"dual"}`,
	})
	t.Cleanup(func() { store.DeleteStrategyConfig("rst-short-close") })

	ctx.closePositionFromSignal(model.Signal{
		Symbol: "SOLUSDT", Direction: "CLOSE", Strategy: "rst-short-close",
	})

	pe := paper.GetPaperExchange()
	if got := paperFree(pe, "SOL"); got < -1e-9 || got > 1e-9 {
		t.Fatalf("平空后 SOL free = %v, want ≈0（空头负债回补归零）", got)
	}
	if got := pe.NetPositionQuantity("SOLUSDT"); got < -1e-9 || got > 1e-9 {
		t.Fatalf("平空后镜像持仓 = %v, want ≈0", got)
	}
	history := order.GetOrderManager().GetOrderHistory("SOLUSDT", 50)
	var closeOrd *model.OrderData
	for _, o := range history {
		if strings.HasPrefix(o.ClientOID, "sig:rst-short-close:") {
			closeOrd = o
		}
	}
	if closeOrd == nil {
		t.Fatal("缺少平空单")
	}
	if closeOrd.Side != model.SideBuy {
		t.Fatalf("平空单方向 = %s, want BUY（买回平仓）", closeOrd.Side)
	}
	if closeOrd.Status != model.StatusFilled {
		t.Fatalf("平空单 status = %s, want FILLED", closeOrd.Status)
	}
	if closeOrd.MarketType != model.MarketSwap {
		t.Fatalf("MarketType = %q, want swap", closeOrd.MarketType)
	}
	if closeOrd.PositionSide != model.PositionShort {
		t.Fatalf("PositionSide = %q, want SHORT（被平仓位方向，BOTH 配置不得默认 LONG）", closeOrd.PositionSide)
	}
	// 幻影镜像仓回归：BUY+SHORT 的平仓成交不得在 PortfolioManager 镜像里
	// 造出新空仓（否则下一次 CLOSE 会把幻影再"平"一次 → 真翻多）。
	acct := ctx.PortfolioManager.GetAccount("default")
	if pos := acct.Positions["SOLUSDT-SHORT"]; pos != nil && pos.Quantity > 0 {
		t.Fatalf("平空成交不得造出 phantom SHORT 镜像仓: %+v", pos)
	}
	if pos := acct.Positions["SOLUSDT-LONG"]; pos != nil && pos.Quantity > 0 {
		t.Fatalf("平空成交不得造出 phantom LONG 镜像仓: %+v", pos)
	}
}

// 零背书空单不平：账本净空但 paper 账户无空头镜像（仓位已被手工平掉）→
// 不下单（拒单空转回归），策略已随信号复位为空仓。
func TestClosePositionFromSignalShortZeroBacking(t *testing.T) {
	ctx := setupPaperCloseTest(t, paper.AccountSnapshot{
		Enabled: true, Balance: 96119, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 96119, Free: 96119},
		},
	})
	seedShortTestLedger(t, "rst-short-skip", "SOLUSDT", 12.5, 119.64)

	ctx.closePositionFromSignal(model.Signal{
		Symbol: "SOLUSDT", Direction: "CLOSE", Strategy: "rst-short-skip",
	})

	history := order.GetOrderManager().GetOrderHistory("SOLUSDT", 50)
	for _, o := range history {
		if strings.HasPrefix(o.ClientOID, "sig:rst-short-skip:") {
			t.Fatalf("零背书空单不得下平仓单（买回没有的空仓=开多事故）: %+v", o)
		}
	}
}

// 幻影仓修正单测：合约成交回报里方向与持仓键不匹配（BUY+SHORT / SELL+LONG）
// 且无该侧持仓 = 平仓语义扑空，如实忽略——不得在镜像里无中生有开仓。
func TestContractFillMismatchedOpenNoPhantom(t *testing.T) {
	ctx := setupPaperCloseTest(t, paper.AccountSnapshot{
		Enabled: true, Balance: 96119, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 96119, Free: 96119},
		},
	})
	acct := ctx.PortfolioManager.GetAccount("default")
	usdtBefore := acct.Balances["USDT"].Free

	// SELL + LONG 键、无 LONG 持仓（账本兜底平多的典型形态）：不得开 LONG 幻影仓。
	ctx.updatePortfolioFromFill(&model.OrderData{
		Symbol: "ADAUSDT", Side: model.SideSell, Status: model.StatusFilled,
		Filled: 5, AvgFillPrice: 120, MarketType: model.MarketSwap,
		PositionSide: model.PositionLong, Leverage: 5,
	})
	if pos := acct.Positions["ADAUSDT-LONG"]; pos != nil && pos.Quantity > 0 {
		t.Fatalf("SELL+LONG 无持仓不得造幻影多仓: %+v", pos)
	}
	// BUY + SHORT 键、无 SHORT 持仓：不得开 SHORT 幻影仓，保证金也不得扣。
	ctx.updatePortfolioFromFill(&model.OrderData{
		Symbol: "ADAUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: 5, AvgFillPrice: 120, MarketType: model.MarketSwap,
		PositionSide: model.PositionShort, Leverage: 5,
	})
	if pos := acct.Positions["ADAUSDT-SHORT"]; pos != nil && pos.Quantity > 0 {
		t.Fatalf("BUY+SHORT 无持仓不得造幻影空仓: %+v", pos)
	}
	if got := acct.Balances["USDT"].Free; got != usdtBefore {
		t.Fatalf("幻影开仓不得扣保证金: USDT free %v → %v", usdtBefore, got)
	}

	// 同向开仓不受影响（回归）：BUY+LONG 正常开多、SELL+SHORT 正常开空。
	ctx.updatePortfolioFromFill(&model.OrderData{
		Symbol: "ADAUSDT", Side: model.SideBuy, Status: model.StatusFilled,
		Filled: 1, AvgFillPrice: 120, MarketType: model.MarketSwap,
		PositionSide: model.PositionLong, Leverage: 5,
	})
	ctx.updatePortfolioFromFill(&model.OrderData{
		Symbol: "ADAUSDT", Side: model.SideSell, Status: model.StatusFilled,
		Filled: 2, AvgFillPrice: 121, MarketType: model.MarketSwap,
		PositionSide: model.PositionShort, Leverage: 5,
	})
	if pos := acct.Positions["ADAUSDT-LONG"]; pos == nil || pos.Quantity != 1 {
		t.Fatalf("BUY+LONG 必须正常开多: %+v", acct.Positions["ADAUSDT-LONG"])
	}
	if pos := acct.Positions["ADAUSDT-SHORT"]; pos == nil || pos.Quantity != 2 {
		t.Fatalf("SELL+SHORT 必须正常开空: %+v", acct.Positions["ADAUSDT-SHORT"])
	}
	// 反向减仓照常：SELL+LONG 平掉多仓。
	ctx.updatePortfolioFromFill(&model.OrderData{
		Symbol: "ADAUSDT", Side: model.SideSell, Status: model.StatusFilled,
		Filled: 1, AvgFillPrice: 122, MarketType: model.MarketSwap,
		PositionSide: model.PositionLong, Leverage: 5,
	})
	if pos := acct.Positions["ADAUSDT-LONG"]; pos != nil && pos.Quantity > 0 {
		t.Fatalf("SELL+LONG 有持仓必须减仓: %+v", pos)
	}
}
