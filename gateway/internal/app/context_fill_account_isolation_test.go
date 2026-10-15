package app

import (
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 2026-10-11：组合镜像持仓键空间的账户级隔离 ──
// 修复前 posID = symbol-side（不含账户），同一 symbol+side 的 paper 单与实盘单
// 成交在镜像里互相覆盖——后写覆盖先写，两个账户各有一份仓位却只剩一行。
// 修复后键为 exchange:posID：写哪个账户的仓就命中哪个账户的键。

func aciSnap() paper.AccountSnapshot {
	return paper.AccountSnapshot{
		Enabled: true, Balance: 100000, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 100000, Free: 100000},
		},
	}
}

// 现货：同 symbol+side 的 paper 与实盘成交各记各的行；平仓只动对应账户。
func TestSpotFillPaperAndLiveCoexist(t *testing.T) {
	ctx := setupPaperCloseTest(t, aciSnap())
	t.Cleanup(ctx.Shutdown)
	acct := ctx.PortfolioManager.GetAccount("default")
	liveKey := portfolio.PositionKey("binance", "ACISOLUSDT-BUY")
	paperKey := portfolio.PositionKey("paper", "ACISOLUSDT-BUY")
	t.Cleanup(func() {
		ctx.PortfolioManager.RemovePosition(liveKey)
		ctx.PortfolioManager.RemovePosition(paperKey)
		ctx.PortfolioManager.RemovePosition(portfolio.PositionKey("paper", "ACISOLUSDT-SELL"))
		ctx.PortfolioManager.RemovePosition(portfolio.PositionKey("binance", "ACISOLUSDT-SELL"))
	})

	fill := func(id string, side model.OrderSide, qty float64, exchange string) {
		ctx.updatePortfolioFromFill(&model.OrderData{
			ID: id, Symbol: "ACISOLUSDT", Side: side,
			OrderType: model.TypeLimit, Price: 100, Quantity: qty, Filled: qty,
			AvgFillPrice: 100, Status: model.StatusFilled, Exchange: exchange,
		})
	}

	fill("aci-spot-live-open", model.SideBuy, 1, "binance")
	fill("aci-spot-paper-open", model.SideBuy, 3, "paper")

	live := acct.Positions[liveKey]
	paperPos := acct.Positions[paperKey]
	if live == nil || live.Quantity != 1 {
		t.Fatalf("实盘行缺失或被 paper 覆盖: %+v", live)
	}
	if paperPos == nil || paperPos.Quantity != 3 {
		t.Fatalf("paper 行缺失或被实盘覆盖: %+v", paperPos)
	}

	// paper 卖出 1：paper 多仓减到 2，实盘行原地不动。
	fill("aci-spot-paper-close", model.SideSell, 1, "paper")
	if got := acct.Positions[paperKey]; got == nil || got.Quantity != 2 {
		t.Fatalf("paper 平仓后数量 = %+v, want 2", got)
	}
	if got := acct.Positions[liveKey]; got == nil || got.Quantity != 1 {
		t.Fatalf("paper 平仓误伤实盘行: %+v", got)
	}
	if got := acct.Positions[portfolio.PositionKey("binance", "ACISOLUSDT-SELL")]; got != nil {
		t.Fatalf("paper 卖出不得在实盘侧记账: %+v", got)
	}

	// 实盘卖出 1：实盘多仓清零删除，paper 行不动。
	fill("aci-spot-live-close", model.SideSell, 1, "binance")
	if got := acct.Positions[liveKey]; got != nil {
		t.Fatalf("实盘全平后行应删除: %+v", got)
	}
	if got := acct.Positions[paperKey]; got == nil || got.Quantity != 2 {
		t.Fatalf("实盘平仓误伤 paper 行: %+v", got)
	}
}

// 合约：同 symbol+LONG 的 paper 与实盘持仓并存；平仓只减对应账户的仓。
func TestContractFillPaperAndLiveCoexist(t *testing.T) {
	ctx := setupPaperCloseTest(t, aciSnap())
	t.Cleanup(ctx.Shutdown)
	acct := ctx.PortfolioManager.GetAccount("default")
	liveKey := portfolio.PositionKey("binance", "ACIETHUSDT-LONG")
	paperKey := portfolio.PositionKey("paper", "ACIETHUSDT-LONG")
	t.Cleanup(func() {
		ctx.PortfolioManager.RemovePosition(liveKey)
		ctx.PortfolioManager.RemovePosition(paperKey)
	})

	fill := func(id string, side model.OrderSide, qty, price float64, exchange string, close bool) {
		ctx.updatePortfolioFromFill(&model.OrderData{
			ID: id, Symbol: "ACIETHUSDT", Side: side,
			OrderType: model.TypeLimit, Price: price, Quantity: qty, Filled: qty,
			AvgFillPrice: price, Status: model.StatusFilled, Exchange: exchange,
			MarketType: model.MarketSwap, PositionSide: model.PositionLong,
			Leverage: 5, ClosePosition: close,
		})
	}

	fill("aci-swap-live-open", model.SideBuy, 2, 100, "binance", false)
	fill("aci-swap-paper-open", model.SideBuy, 3, 100, "paper", false)

	live := acct.Positions[liveKey]
	paperPos := acct.Positions[paperKey]
	if live == nil || live.Quantity != 2 || live.Margin != 40 {
		t.Fatalf("实盘 LONG 缺失或被覆盖: %+v", live)
	}
	if paperPos == nil || paperPos.Quantity != 3 || paperPos.Margin != 60 {
		t.Fatalf("paper LONG 缺失或被覆盖: %+v", paperPos)
	}
	if live.Exchange != "binance" || paperPos.Exchange != "paper" {
		t.Fatalf("账户打标错乱: live=%q paper=%q", live.Exchange, paperPos.Exchange)
	}

	// paper 部分平仓 1 @110：只动 paper 行（qty 3→2，释放保证金 20+PnL）。
	fill("aci-swap-paper-close", model.SideSell, 1, 110, "paper", true)
	if got := acct.Positions[paperKey]; got == nil || got.Quantity != 2 {
		t.Fatalf("paper 平仓后 = %+v, want qty 2", got)
	}
	if got := acct.Positions[liveKey]; got == nil || got.Quantity != 2 || got.Margin != 40 {
		t.Fatalf("paper 平仓误伤实盘行: %+v", got)
	}

	// 实盘全平：实盘行删除，paper 行保留。
	fill("aci-swap-live-close", model.SideSell, 2, 110, "binance", true)
	if got := acct.Positions[liveKey]; got != nil {
		t.Fatalf("实盘全平后行应删除: %+v", got)
	}
	if got := acct.Positions[paperKey]; got == nil || got.Quantity != 2 {
		t.Fatalf("实盘平仓误伤 paper 行: %+v", got)
	}
}

// 风控上下文在共存场景分口径：同 symbol 的 paper 行不抬实盘暴露，反之亦然。
func TestBuildRiskContextSameSymbolCoexistence(t *testing.T) {
	markMarketDataFailed()

	mgr := portfolio.GetManager()
	paperKey := portfolio.PositionKey("paper", "ACIRISKUSDT-BUY")
	liveKey := portfolio.PositionKey("binance", "ACIRISKUSDT-BUY")
	mgr.UpdatePosition(model.PositionData{ID: "ACIRISKUSDT-BUY", Symbol: "ACIRISKUSDT", Quantity: 10, CurrentPrice: 100, Exchange: "paper"})
	mgr.UpdatePosition(model.PositionData{ID: "ACIRISKUSDT-BUY", Symbol: "ACIRISKUSDT", Quantity: 1, CurrentPrice: 100, Exchange: "binance"})
	t.Cleanup(func() {
		mgr.RemovePosition(paperKey)
		mgr.RemovePosition(liveKey)
	})

	ctx := &Context{PortfolioManager: mgr}
	liveCtx, _ := ctx.buildRiskContext(&order.Request{
		Symbol: "ACIRISKUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 0.1, Exchange: "binance",
	})
	if want := mgr.NetExposureAgainst(mgr.TotalEquity()); liveCtx.NetExposure != want {
		t.Fatalf("实盘暴露 = %v, want %v", liveCtx.NetExposure, want)
	}
	if liveCtx.NetExposure >= 100 {
		t.Fatalf("实盘暴露 %v 仍混入 paper 持仓（共存泄漏）", liveCtx.NetExposure)
	}

	pe := mgr.PaperEquity()
	paperCtx, _ := ctx.buildRiskContext(&order.Request{
		Symbol: "ACIRISKUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 0.1, Exchange: "paper",
	})
	if want := mgr.PaperNetExposureAgainst(pe); paperCtx.NetExposure != want {
		t.Fatalf("paper 暴露 = %v, want %v", paperCtx.NetExposure, want)
	}
	if paperCtx.PositionCount != mgr.PaperPositionCount() {
		t.Fatalf("paper PositionCount = %d, want %d", paperCtx.PositionCount, mgr.PaperPositionCount())
	}
}

// 信号平仓的账户归属：有实盘凭证（resolveExchange=binance）时，paper 策略的
// CLOSE 也只平 paper 镜像仓并回 paper 撮合；同 symbol 的实盘镜像仓原样保留。
func TestCloseMirroredPositionsOnlyOwnAccount(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "test-key")
	t.Setenv("BINANCE_API_SECRET", "test-secret")
	ctx := setupPaperCloseTest(t, aciSnap())
	t.Cleanup(ctx.Shutdown)
	syntheticAnchorPrices["ACICLSUSDT"] = 100
	t.Cleanup(func() { delete(syntheticAnchorPrices, "ACICLSUSDT") })
	acct := ctx.PortfolioManager.GetAccount("default")
	liveKey := portfolio.PositionKey("binance", "ACICLSUSDT-LONG")
	paperKey := portfolio.PositionKey("paper", "ACICLSUSDT-LONG")
	t.Cleanup(func() {
		ctx.PortfolioManager.RemovePosition(liveKey)
		ctx.PortfolioManager.RemovePosition(paperKey)
	})
	store.SetStrategyConfig("aci-close", map[string]any{
		"id": "aci-close", "strategy_type": "macd", "symbol": "ACICLSUSDT",
		"execution_mode": "paper", "market_type": "swap", "direction": "dual",
		"config_json": `{"market_type":"swap","leverage":5,"direction":"dual"}`,
	})
	t.Cleanup(func() { store.DeleteStrategyConfig("aci-close") })

	open := func(id string, qty float64, exchange string) {
		ctx.updatePortfolioFromFill(&model.OrderData{
			ID: id, Symbol: "ACICLSUSDT", Side: model.SideBuy,
			OrderType: model.TypeLimit, Price: 100, Quantity: qty, Filled: qty,
			AvgFillPrice: 100, Status: model.StatusFilled, Exchange: exchange,
			MarketType: model.MarketSwap, PositionSide: model.PositionLong, Leverage: 5,
		})
	}
	open("aci-close-live-open", 1, "binance")
	open("aci-close-paper-open", 2, "paper")
	if acct.Positions[liveKey] == nil || acct.Positions[paperKey] == nil {
		t.Fatal("前置：同 symbol+LONG 的 paper/实盘镜像仓必须并存")
	}

	ctx.closePositionFromSignal(model.Signal{Symbol: "ACICLSUSDT", Direction: "CLOSE", Strategy: "aci-close"})

	if got := acct.Positions[paperKey]; got != nil && got.Quantity > 0 {
		t.Fatalf("paper 镜像仓应被 CLOSE 平掉: %+v", got)
	}
	if got := acct.Positions[liveKey]; got == nil || got.Quantity != 1 {
		t.Fatalf("paper 策略 CLOSE 不得动实盘镜像仓: %+v", got)
	}
	var closeOrd *model.OrderData
	for _, o := range order.GetOrderManager().GetOrderHistory("ACICLSUSDT", 50) {
		if strings.HasPrefix(o.ClientOID, "sig:aci-close:") {
			closeOrd = o
		}
	}
	if closeOrd == nil {
		t.Fatal("缺少平仓单")
	}
	if closeOrd.Exchange != "paper" {
		t.Fatalf("paper 策略平仓单 Exchange = %q, want paper（不得直发实盘）", closeOrd.Exchange)
	}
	if closeOrd.Status != model.StatusFilled {
		t.Fatalf("paper 平仓单 status = %s, want FILLED", closeOrd.Status)
	}
}
