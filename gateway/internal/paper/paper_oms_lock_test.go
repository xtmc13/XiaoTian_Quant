package paper

import (
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// OMS 下单链路资金锁（LockOrderFunds/UnlockOrderFunds）与 ApplySimulatedFill
// 结算的一致性：2026-10-08 生产实锤——重启恢复持仓后平仓单被旧余额锁
// （PortfolioManager 内存镜像，重启归零）误拒"insufficient SOL balance:
// 0.00 < 12.52"，paper 账户实际持有 16.82 SOL。

// 现货卖出无 base 必须拒单，且错误口径与生产日志一致。
func TestLockOrderFundsSpotSellRequiresBase(t *testing.T) {
	pe := newTestExchange()

	err := pe.LockOrderFunds("SOLUSDT", model.SideSell, model.MarketType("spot"), 120, 12.52)
	if err == nil || !strings.Contains(err.Error(), "insufficient SOL balance") {
		t.Fatalf("无 base 现货卖出必须拒单: %v", err)
	}

	credit(pe, "SOL", 16.82)
	if err := pe.LockOrderFunds("SOLUSDT", model.SideSell, model.MarketType("spot"), 120, 12.52); err != nil {
		t.Fatalf("持仓充足不得拒单: %v", err)
	}
	free, used := balanceOf(pe, "SOL")
	if diff := free - 4.3; diff > 1e-9 || diff < -1e-9 || used != 12.52 {
		t.Fatalf("锁定后 SOL free/used = %v/%v", free, used)
	}

	// 锁定的部分不可再卖（第二张平仓单只剩 4.30 可锁）。
	if err := pe.LockOrderFunds("SOLUSDT", model.SideSell, model.MarketType("spot"), 120, 12.52); err == nil {
		t.Fatal("超额卖出必须拒单")
	}
}

// 买入锁定 + 成交结算不得双重扣减：USDT 只扣一次（成本+手续费），Used 归零。
func TestLockOrderFundsBuySettleNoDoubleCount(t *testing.T) {
	pe := newTestExchange() // USDT 100000
	if err := pe.LockOrderFunds("BTCUSDT", model.SideBuy, model.MarketType("spot"), 50000, 0.1); err != nil {
		t.Fatal(err)
	}
	free, used := balanceOf(pe, "USDT")
	if free != 95000 || used != 5000 {
		t.Fatalf("锁定后 USDT free/used = %v/%v", free, used)
	}

	pe.ApplySimulatedFill("ord-lb-1", model.TradeData{
		Symbol: "BTCUSDT", ID: "ord-lb-1", Price: 50000, Quantity: 0.1, Side: "BUY",
	})
	free, used = balanceOf(pe, "USDT")
	fee := 50000 * 0.1 * pe.FeeRate()
	want := 100000 - 5000 - fee
	if free != want || used != 0 {
		t.Fatalf("结算后 USDT free/used = %v/%v, want %v/0（双重扣减回归）", free, used, want)
	}
	btcFree, _ := balanceOf(pe, "BTC")
	if btcFree != 0.1 {
		t.Fatalf("BTC free = %v, want 0.1", btcFree)
	}
}

// 卖出锁定 + 成交结算消费锁定：base 只扣一次，USDT 增加 成交额−手续费。
// 即生产事故场景：账户持 16.82 SOL，平仓 12.52 必须成交且账平。
func TestLockOrderFundsSellSettleConsumesLock(t *testing.T) {
	pe := newTestExchange()
	credit(pe, "SOL", 16.824301207189286)

	if err := pe.LockOrderFunds("SOLUSDT", model.SideSell, model.MarketType("spot"), 116.14, 12.519152); err != nil {
		t.Fatalf("恢复持仓的平仓锁定必须成功: %v", err)
	}
	pe.ApplySimulatedFill("ord-ls-1", model.TradeData{
		Symbol: "SOLUSDT", ID: "ord-ls-1", Price: 116.14, Quantity: 12.519152, Side: "SELL",
	})

	solFree, solUsed := balanceOf(pe, "SOL")
	if solUsed != 0 {
		t.Fatalf("SOL used = %v, want 0", solUsed)
	}
	if diff := solFree - (16.824301207189286 - 12.519152); diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("SOL free = %v, want %v", solFree, 16.824301207189286-12.519152)
	}
	usdtFree, _ := balanceOf(pe, "USDT")
	proceeds := 116.14*12.519152 - 116.14*12.519152*pe.FeeRate()
	if diff := usdtFree - (100000 + proceeds); diff > 1e-6 || diff < -1e-6 {
		t.Fatalf("USDT free = %v, want %v", usdtFree, 100000+proceeds)
	}
}

// 合约开空：无 base 也放行（结算允许 base 记负=空头负债），但不锁资金。
func TestLockOrderFundsSwapShortAllowed(t *testing.T) {
	pe := newTestExchange()

	if err := pe.LockOrderFunds("SOLUSDT", model.SideSell, model.MarketSwap, 120, 5); err != nil {
		t.Fatalf("合约开空不得拒单: %v", err)
	}
	free, used := balanceOf(pe, "SOL")
	if free != 0 || used != 0 {
		t.Fatalf("开空无锁: SOL free/used = %v/%v", free, used)
	}

	pe.ApplySimulatedFill("ord-ss-1", model.TradeData{
		Symbol: "SOLUSDT", ID: "ord-ss-1", Price: 120, Quantity: 5, Side: "SELL",
	})
	free, _ = balanceOf(pe, "SOL")
	if free != -5 {
		t.Fatalf("开空后 SOL free = %v, want -5", free)
	}

	// 合约平多：base 足额时照常锁定。
	credit(pe, "SOL", 10) // -5 + 10 = 5
	if err := pe.LockOrderFunds("SOLUSDT", model.SideSell, model.MarketSwap, 120, 8); err != nil {
		t.Fatal(err)
	}
	free, used = balanceOf(pe, "SOL")
	if free != 0 || used != 5 {
		t.Fatalf("平多锁定已有部分: SOL free/used = %v/%v, want 0/5", free, used)
	}
}

// 拒单/撤单回滚：锁定后解锁，余额原样恢复。
func TestUnlockOrderFundsRestores(t *testing.T) {
	pe := newTestExchange()
	credit(pe, "SOL", 16.82)

	if err := pe.LockOrderFunds("SOLUSDT", model.SideSell, model.MarketType("spot"), 120, 12.52); err != nil {
		t.Fatal(err)
	}
	pe.UnlockOrderFunds("SOLUSDT", model.SideSell, 120, 12.52)
	free, used := balanceOf(pe, "SOL")
	if free != 16.82 || used != 0 {
		t.Fatalf("解锁后 SOL free/used = %v/%v", free, used)
	}

	if err := pe.LockOrderFunds("BTCUSDT", model.SideBuy, model.MarketType("spot"), 50000, 0.1); err != nil {
		t.Fatal(err)
	}
	pe.UnlockOrderFunds("BTCUSDT", model.SideBuy, 50000, 0.1)
	usdtFree, usdtUsed := balanceOf(pe, "USDT")
	if usdtFree != 100000 || usdtUsed != 0 {
		t.Fatalf("解锁后 USDT free/used = %v/%v", usdtFree, usdtUsed)
	}

	// 合约开空无锁定，解锁为 no-op。
	pe.UnlockOrderFunds("ETHUSDT", model.SideSell, 3000, 1)
	ethFree, ethUsed := balanceOf(pe, "ETH")
	if ethFree != 0 || ethUsed != 0 {
		t.Fatalf("无锁解锁必须 no-op: ETH free/used = %v/%v", ethFree, ethUsed)
	}
}

// 背书读取 accessor：FreeBalance / NetPositionQuantity。
func TestPaperBackingAccessors(t *testing.T) {
	pe := newTestExchange()
	credit(pe, "SOL", 16.82)
	if got := pe.FreeBalance("SOL"); got != 16.82 {
		t.Fatalf("FreeBalance SOL = %v", got)
	}
	if got := pe.FreeBalance("NOPE"); got != 0 {
		t.Fatalf("FreeBalance NOPE = %v", got)
	}
	if got := pe.NetPositionQuantity("SOLUSDT"); got != 0 {
		t.Fatalf("无持仓 NetPositionQuantity = %v", got)
	}

	pe.ApplySimulatedFill("ord-ba-1", model.TradeData{
		Symbol: "SOLUSDT", ID: "ord-ba-1", Price: 100, Quantity: 2, Side: "BUY",
	})
	if got := pe.NetPositionQuantity("SOLUSDT"); got != 2 {
		t.Fatalf("NetPositionQuantity = %v, want 2", got)
	}
}
