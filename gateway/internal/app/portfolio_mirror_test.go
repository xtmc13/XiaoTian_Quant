package app

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/paper"
)

// ── H2（2026-10-09）：PortfolioManager 内存镜像"锁定+成交"双重扣减修复 ──
//
// 存量漂移形态：OMS LockBalance 对非 paper 单锁镜像（Free→Used），成交后
// updatePortfolioFromFill 又按 adjustBalance 实扣一次 Free/Total——同一笔
// 资金扣两次，且 Used 永不释放（Free 钳 0 后继续漂移、MarginUsed 单向膨胀，
// Portfolio 页/风险口径失真）。修复：成交结算先释放锁定再实扣（与 paper
// settleSimulatedFill 同款语义），且同一订单 FILLED 回报幂等只结算一次。
//
// 本包测试共享全局镜像单例，一律用独立 symbol（H2*USDT）+ 增量断言隔离。

func mirrorBal(ctx *Context, currency string) (total, free, used float64) {
	b := ctx.PortfolioManager.GetAccount("default").Balances[currency]
	if b == nil {
		return 0, 0, 0
	}
	return b.Total, b.Free, b.Used
}

func setupMirrorH2(t *testing.T) *Context {
	t.Helper()
	return setupPaperCloseTest(t, paper.AccountSnapshot{
		Enabled: true, Balance: 100000, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 100000, Free: 100000},
		},
	})
}

// 现货买入：锁定 200 → 成交实扣 200。修复前 Free=before-400 且 Used=200 残留；
// 修复后 Free=before-200、Used 回落到锁定前。
func TestMirrorSpotBuyFillNoDoubleDeduct(t *testing.T) {
	ctx := setupMirrorH2(t)
	om := order.GetOrderManager()

	_, freeBefore, usedBefore := mirrorBal(ctx, "USDT")
	_, baseFreeBefore, _ := mirrorBal(ctx, "H2B")

	req := &order.Request{
		Symbol: "H2BUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 2, Exchange: "binance",
	}
	if err := om.LockBalance(req); err != nil {
		t.Fatalf("lock: %v", err)
	}
	_, free, used := mirrorBal(ctx, "USDT")
	if free != freeBefore-200 || used != usedBefore+200 {
		t.Fatalf("锁定后 USDT free/used = %v/%v, want %v/%v", free, used, freeBefore-200, usedBefore+200)
	}

	ctx.updatePortfolioFromFill(&model.OrderData{
		ID: "ord-h2-spot-buy-1", Symbol: "H2BUSDT", Side: model.SideBuy,
		OrderType: model.TypeLimit, Price: 100, Quantity: 2, Filled: 2,
		AvgFillPrice: 100, Status: model.StatusFilled, Exchange: "binance",
	})

	total, free, used := mirrorBal(ctx, "USDT")
	if free != freeBefore-200 {
		t.Fatalf("成交后 USDT free = %v, want %v（锁定已释放只实扣一次；双重扣减会得到 %v）",
			free, freeBefore-200, freeBefore-400)
	}
	if used != usedBefore {
		t.Fatalf("成交后 USDT used = %v, want %v（锁定必须随结算释放）", used, usedBefore)
	}
	if total != freeBefore-200 {
		t.Fatalf("成交后 USDT total = %v, want %v", total, freeBefore-200)
	}
	_, baseFree, baseUsed := mirrorBal(ctx, "H2B")
	if baseFree != baseFreeBefore+2 || baseUsed != 0 {
		t.Fatalf("成交后 H2B free/used = %v/%v, want %v/0", baseFree, baseUsed, baseFreeBefore+2)
	}
}

// 现货卖出：锁定 base 2 → 成交实扣 base 2、quote +200。修复前 base Used=2
// 永久残留（下次卖出可用量被低估）；修复后 Used/Total 归零。
func TestMirrorSpotSellFillNoDoubleDeduct(t *testing.T) {
	ctx := setupMirrorH2(t)
	om := order.GetOrderManager()
	_, usdtFreeBefore, usdtUsedBefore := mirrorBal(ctx, "USDT")

	// 先买入 2 H2S（锁定+成交全套），建立 base 持仓。
	buyReq := &order.Request{
		Symbol: "H2SUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 2, Exchange: "binance",
	}
	if err := om.LockBalance(buyReq); err != nil {
		t.Fatalf("buy lock: %v", err)
	}
	ctx.updatePortfolioFromFill(&model.OrderData{
		ID: "ord-h2-spot-sell-setup", Symbol: "H2SUSDT", Side: model.SideBuy,
		OrderType: model.TypeLimit, Price: 100, Quantity: 2, Filled: 2,
		AvgFillPrice: 100, Status: model.StatusFilled, Exchange: "binance",
	})

	// 卖出：锁 base 2。
	sellReq := &order.Request{
		Symbol: "H2SUSDT", Side: model.SideSell, OrderType: model.TypeLimit,
		Price: 100, Quantity: 2, Exchange: "binance",
	}
	if err := om.LockBalance(sellReq); err != nil {
		t.Fatalf("sell lock: %v", err)
	}
	_, baseFree, baseUsed := mirrorBal(ctx, "H2S")
	if baseFree != 0 || baseUsed != 2 {
		t.Fatalf("卖出锁定后 H2S free/used = %v/%v, want 0/2", baseFree, baseUsed)
	}

	ctx.updatePortfolioFromFill(&model.OrderData{
		ID: "ord-h2-spot-sell-1", Symbol: "H2SUSDT", Side: model.SideSell,
		OrderType: model.TypeLimit, Price: 100, Quantity: 2, Filled: 2,
		AvgFillPrice: 100, Status: model.StatusFilled, Exchange: "binance",
	})

	baseTotal, baseFree, baseUsed := mirrorBal(ctx, "H2S")
	if baseTotal != 0 || baseFree != 0 || baseUsed != 0 {
		t.Fatalf("卖出成交后 H2S total/free/used = %v/%v/%v, want 0/0/0（Used 残留即漂移）",
			baseTotal, baseFree, baseUsed)
	}
	_, usdtFree, usdtUsed := mirrorBal(ctx, "USDT")
	if usdtFree != usdtFreeBefore {
		t.Fatalf("买卖往返后 USDT free = %v, want %v（零价差零费口径应回本）", usdtFree, usdtFreeBefore)
	}
	if usdtUsed != usdtUsedBefore {
		t.Fatalf("买卖往返后 USDT used = %v, want %v", usdtUsed, usdtUsedBefore)
	}
}

// 合约开多→平多全链路：开仓锁保证金 40 成交实扣 40；平仓单自身的锁（44，
// 按平仓价计）成交时释放，结算释放保证金+PnL。修复前两张单的锁（40+44=84）
// 全部残留 Used；修复后每笔成交后 Used 回落。
func TestMirrorContractOpenCloseNoDoubleDeduct(t *testing.T) {
	ctx := setupMirrorH2(t)
	om := order.GetOrderManager()
	_, freeBefore, usedBefore := mirrorBal(ctx, "USDT")

	// 开多：100×2/杠杆5 = 40 保证金。
	openReq := &order.Request{
		Symbol: "H2CUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 2, Exchange: "binance",
		MarketType: model.MarketSwap, PositionSide: model.PositionLong, Leverage: 5,
	}
	if err := om.LockBalance(openReq); err != nil {
		t.Fatalf("open lock: %v", err)
	}
	ctx.updatePortfolioFromFill(&model.OrderData{
		ID: "ord-h2-swap-open-1", Symbol: "H2CUSDT", Side: model.SideBuy,
		OrderType: model.TypeLimit, Price: 100, Quantity: 2, Filled: 2,
		AvgFillPrice: 100, Status: model.StatusFilled, Exchange: "binance",
		MarketType: model.MarketSwap, PositionSide: model.PositionLong, Leverage: 5,
	})
	_, free, used := mirrorBal(ctx, "USDT")
	if free != freeBefore-40 || used != usedBefore {
		t.Fatalf("开仓成交后 USDT free/used = %v/%v, want %v/%v（保证金不得锁+扣两次）",
			free, used, freeBefore-40, usedBefore)
	}
	pos := ctx.PortfolioManager.GetAccount("default").Positions["H2CUSDT-LONG"]
	if pos == nil || pos.Quantity != 2 || pos.Margin != 40 {
		t.Fatalf("开仓后镜像持仓不符: %+v", pos)
	}

	// 平多：110 全平，平仓单锁 110×2/5=44。
	closeReq := &order.Request{
		Symbol: "H2CUSDT", Side: model.SideSell, OrderType: model.TypeLimit,
		Price: 110, Quantity: 2, Exchange: "binance", ClosePosition: true,
		MarketType: model.MarketSwap, PositionSide: model.PositionLong, Leverage: 5,
	}
	if err := om.LockBalance(closeReq); err != nil {
		t.Fatalf("close lock: %v", err)
	}
	_, _, used = mirrorBal(ctx, "USDT")
	if used != usedBefore+44 {
		t.Fatalf("平仓锁定后 USDT used = %v, want %v", used, usedBefore+44)
	}
	ctx.updatePortfolioFromFill(&model.OrderData{
		ID: "ord-h2-swap-close-1", Symbol: "H2CUSDT", Side: model.SideSell,
		OrderType: model.TypeLimit, Price: 110, Quantity: 2, Filled: 2,
		AvgFillPrice: 110, Status: model.StatusFilled, Exchange: "binance", ClosePosition: true,
		MarketType: model.MarketSwap, PositionSide: model.PositionLong, Leverage: 5,
	})
	_, free, used = mirrorBal(ctx, "USDT")
	if used != usedBefore {
		t.Fatalf("平仓成交后 USDT used = %v, want %v（平仓单自身锁定必须释放）", used, usedBefore)
	}
	// 结算口径（既有算术不变）：释放保证金按平仓价 44 + 已实现 PnL (110-100)×2=20。
	if want := freeBefore - 40 + 44 + 20; free != want {
		t.Fatalf("平仓成交后 USDT free = %v, want %v", free, want)
	}
	if pos := ctx.PortfolioManager.GetAccount("default").Positions["H2CUSDT-LONG"]; pos != nil {
		t.Fatalf("全平后镜像持仓应删除: %+v", pos)
	}
}

// 幂等：同一订单的 FILLED 回报重复触达（PlaceOrder 即时成交 + reconcile/
// 恢复回写）只结算一次——重复结算会重复释放锁定（吃掉他单的锁）+重复实扣。
func TestMirrorFillDuplicateReportSettlesOnce(t *testing.T) {
	ctx := setupMirrorH2(t)
	om := order.GetOrderManager()
	_, freeBefore, usedBefore := mirrorBal(ctx, "USDT")

	// 另一币种并发挂单锁定 50：验证重复结算不得吃掉它的锁。
	otherReq := &order.Request{
		Symbol: "H2OTHUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 50, Quantity: 1, Exchange: "binance",
	}
	if err := om.LockBalance(otherReq); err != nil {
		t.Fatalf("other lock: %v", err)
	}

	buyReq := &order.Request{
		Symbol: "H2DUPUSDT", Side: model.SideBuy, OrderType: model.TypeLimit,
		Price: 100, Quantity: 1, Exchange: "binance",
	}
	if err := om.LockBalance(buyReq); err != nil {
		t.Fatalf("lock: %v", err)
	}
	ord := &model.OrderData{
		ID: "ord-h2-dup-1", Symbol: "H2DUPUSDT", Side: model.SideBuy,
		OrderType: model.TypeLimit, Price: 100, Quantity: 1, Filled: 1,
		AvgFillPrice: 100, Status: model.StatusFilled, Exchange: "binance",
	}
	ctx.updatePortfolioFromFill(ord)
	_, freeAfterFirst, usedAfterFirst := mirrorBal(ctx, "USDT")

	// 重复回报：余额必须一动不动（H2OTH 单的 50 锁完好）。
	ctx.updatePortfolioFromFill(ord)
	_, freeAfterDup, usedAfterDup := mirrorBal(ctx, "USDT")
	if freeAfterDup != freeAfterFirst || usedAfterDup != usedAfterFirst {
		t.Fatalf("重复 FILLED 不得二次结算: free %v→%v, used %v→%v",
			freeAfterFirst, freeAfterDup, usedAfterFirst, usedAfterDup)
	}
	if usedAfterDup != usedBefore+50 {
		t.Fatalf("他单锁定被重复释放吃掉: used = %v, want %v", usedAfterDup, usedBefore+50)
	}
	if want := freeBefore - 50 - 100; freeAfterDup != want {
		t.Fatalf("终态 USDT free = %v, want %v", freeAfterDup, want)
	}
	pos := ctx.PortfolioManager.GetAccount("default").Positions["H2DUPUSDT-BUY"]
	if pos == nil || pos.Quantity != 1 {
		t.Fatalf("重复回报不得重复加仓: %+v", pos)
	}
}
