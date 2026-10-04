package paper

import (
	"math"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

func newTestExchange() *PaperExchange {
	cfg := DefaultPaperConfig()
	cfg.MinLatency = 0
	cfg.MaxLatency = time.Millisecond
	return NewPaperExchange(cfg)
}

// credit 测试辅助：直接给 paper 账户充值（绕过交易）。
func credit(pe *PaperExchange, asset string, amount float64) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	if b, ok := pe.balances[asset]; ok {
		b.Free += amount
		b.Total = b.Free + b.Used
	} else {
		pe.balances[asset] = &model.Balance{Currency: asset, Free: amount, Total: amount}
	}
}

func balanceOf(pe *PaperExchange, asset string) (free, used float64) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	if b, ok := pe.balances[asset]; ok {
		return b.Free, b.Used
	}
	return 0, 0
}

// 负余额场景：卖出超过持仓必须被拒绝，任何情况下余额不得为负。
func TestPaperSellWithoutHoldingsRejected(t *testing.T) {
	pe := newTestExchange()

	// 未持有 BTC，直接市价卖出 → 拒绝（纯下跌行情卖空记负余额的修复）
	res, err := pe.PlaceOrder("BTCUSDT", "SELL", "MARKET", 0, 1.0)
	if err == nil {
		t.Fatalf("无持仓卖出必须被拒绝: %+v", res)
	}
	free, _ := balanceOf(pe, "BTC")
	if free != 0 {
		t.Fatalf("BTC 余额不得变化: %v", free)
	}

	// 限价卖出同样拒绝。
	if _, err := pe.PlaceOrder("BTCUSDT", "SELL", "LIMIT", 50000, 0.5); err == nil {
		t.Fatal("无持仓限价卖出必须被拒绝")
	}
}

// 部分成交场景：买方 quote 不足时市价买入按可用截断。
func TestPaperMarketBuyCappedByAvailableQuote(t *testing.T) {
	pe := newTestExchange()
	credit(pe, "BTC", 10)      // 卖方需要持仓
	credit(pe, "USDT", -90000) // 可用 quote 压到 10000
	// 盘口卖单 1 BTC @ 50000。
	if _, err := pe.PlaceOrder("BTCUSDT", "SELL", "LIMIT", 50000, 1.0); err != nil {
		t.Fatalf("挂卖单: %v", err)
	}
	res, err := pe.PlaceOrder("BTCUSDT", "BUY", "MARKET", 0, 1.0)
	if err != nil {
		t.Fatalf("市价买入应部分成交: %v", err)
	}
	if got := res["filled"].(float64); got != 0.2 {
		t.Fatalf("应按可用 quote 截断到 0.2 BTC，实际 %v", got)
	}
	free, _ := balanceOf(pe, "USDT")
	if free < -1e-9 {
		t.Fatalf("USDT 余额不得为负: %v", free)
	}
}

// 限价买入锁仓：余额不足拒绝；锁仓后 cancel 释放。
func TestPaperLimitBuyLockAndCancelRelease(t *testing.T) {
	pe := newTestExchange()

	// 10 万美元只够买 1 BTC @50000 的 2 倍杠杆？不——现货全价：只能买 2 个。
	if _, err := pe.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 3.0); err == nil {
		t.Fatal("3 BTC 需要 15 万 > 10 万，必须拒绝")
	}

	res, err := pe.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 49000, 1.0)
	if err != nil {
		t.Fatalf("1 BTC 应可挂出: %v", err)
	}
	free, used := balanceOf(pe, "USDT")
	if used != 49000 {
		t.Fatalf("锁仓金额应为 49000: free=%v used=%v", free, used)
	}

	orderID := res["order_id"].(string)
	if _, err := pe.CancelOrder("BTCUSDT", orderID); err != nil {
		t.Fatalf("撤单: %v", err)
	}
	free, used = balanceOf(pe, "USDT")
	if used != 0 || free != 100000 {
		t.Fatalf("撤单后锁定应全部释放: free=%v used=%v", free, used)
	}
}

// 完整链路：买入成交后卖出，资金自洽。
func TestPaperBuyThenSellRoundTrip(t *testing.T) {
	// 自成交：挂卖单再买。
	pe2 := newTestExchange()
	credit(pe2, "BTC", 10)
	if _, err := pe2.PlaceOrder("BTCUSDT", "SELL", "LIMIT", 50000, 0.5); err != nil {
		t.Fatalf("挂卖单: %v", err)
	}
	if _, err := pe2.PlaceOrder("BTCUSDT", "BUY", "LIMIT", 50000, 0.5); err != nil {
		t.Fatalf("吃卖单: %v", err)
	}
	free, used := balanceOf(pe2, "USDT")
	// 自成交同一账户：卖出得 25000-25，买入花 25000+25 → 净亏手续费 50。
	if used != 0 {
		t.Fatalf("全部成交后不应有锁定残留: used=%v", used)
	}
	if free != 99950 {
		t.Fatalf("净手续费结算异常: free=%v", free)
	}
	btcFree, btcUsed := balanceOf(pe2, "BTC")
	if btcFree != 10 || btcUsed != 0 {
		t.Fatalf("持仓结算异常（自成交净持仓应不变）: free=%v used=%v", btcFree, btcUsed)
	}
}

// TestPaperAccountSnapshotRestore 快照/恢复往返：持仓、余额、开关随重启存活
// （2026-10-03 修复——此前重启恢复走 SetBalance 清空持仓，策略账本（xt_orders
// sig: 净持仓）失去镜像，每次重启触发一次 "Close from strategy ledger" 失败
// WARN，且观察仓位重启即丢）。
func TestPaperAccountSnapshotRestore(t *testing.T) {
	pe := newTestExchange()
	// 真实成交产生持仓流水（自成交盘口，卖方用测试充值）。
	credit(pe, "BTC", 10)
	if _, err := pe.PlaceOrder("BTCUSDT", "SELL", "LIMIT", 50000, 0.4); err != nil {
		t.Fatalf("挂卖单: %v", err)
	}
	if _, err := pe.PlaceOrder("BTCUSDT", "BUY", "MARKET", 0, 0.4); err != nil {
		t.Fatalf("市价买入: %v", err)
	}
	// 自成交净持仓为 0（position 只记 taker 流水），再直接走内部路径沉淀一笔。
	pe.mu.Lock()
	pe.updatePosition(model.TradeData{Symbol: "BTCUSDT", Side: "BUY", Price: 50000, Quantity: 0.5, Timestamp: time.Now().UnixMilli()})
	pe.mu.Unlock()
	pe.SetEnabled(false)

	snap := pe.SnapshotAccount()
	if len(snap.Positions) == 0 {
		t.Fatal("快照必须含非零持仓")
	}
	usdtBefore, _ := balanceOf(pe, "USDT")

	pe2 := newTestExchange()
	pe2.RestoreAccount(snap)
	if pe2.IsEnabled() {
		t.Fatal("开关状态必须随快照恢复（false）")
	}
	usdtAfter, _ := balanceOf(pe2, "USDT")
	if math.Abs(usdtAfter-usdtBefore) > 1e-6 {
		t.Fatalf("恢复后 USDT 余额不一致: before=%v after=%v", usdtBefore, usdtAfter)
	}
	pe2.mu.RLock()
	pos := pe2.positions["BTCUSDT"]["BTCUSDT-spot"]
	pe2.mu.RUnlock()
	// position 只记 taker 流水：taker 买 0.4 + 手工 0.5 = 0.9 @50000。
	if pos == nil || math.Abs(pos.Quantity-0.9) > 1e-9 {
		t.Fatalf("恢复后持仓数量错误: %+v", pos)
	}
	if math.Abs(pos.AvgEntryPrice-50000) > 1e-9 {
		t.Fatalf("恢复后持仓均价错误: %v", pos.AvgEntryPrice)
	}
	if len(pos.trades) == 0 {
		t.Fatal("恢复后成交历史为空（均价再计算会丢基准）")
	}
}

// TestPaperAccountOldFormatRestore 旧格式快照（只有 enabled+balance，无
// balances/positions 字段）兼容：恢复余额、无持仓——与旧实现行为一致。
func TestPaperAccountOldFormatRestore(t *testing.T) {
	pe := newTestExchange()
	pe.RestoreAccount(AccountSnapshot{Enabled: true, Balance: 50000})
	if got := pe.GetAccount()["balance"].(float64); got != 50000 {
		t.Fatalf("旧格式余额恢复错误: %v", got)
	}
}

// TestPaperOnStateChange 状态变更回调：余额重置/开关切换触发持久化回调
// （回调在锁外触发，内部回取快照不自死锁）。
func TestPaperOnStateChange(t *testing.T) {
	pe := newTestExchange()
	n := 0
	pe.OnStateChange(func() { n++ })
	pe.SetEnabled(false)
	pe.SetBalance(5000)
	if n != 2 {
		t.Fatalf("状态变更回调应触发 2 次，实际 %d", n)
	}
}

// TestPaperApplySimulatedFill OMS 模拟成交入账：持仓+资金结算、累计回报取
// 增量幂等、状态变更回调触发（重启快照随成交落盘的前提）。
func TestPaperApplySimulatedFill(t *testing.T) {
	pe := newTestExchange()
	n := 0
	pe.OnStateChange(func() { n++ })

	trade := func(qty float64) model.TradeData {
		return model.TradeData{Symbol: "BTCUSDT", ID: "ord-s1", Price: 50000, Quantity: qty, Side: "BUY", Timestamp: time.Now().UnixMilli()}
	}
	pe.ApplySimulatedFill("ord-s1", trade(0.4)) // 部分成交 0.4
	pe.ApplySimulatedFill("ord-s1", trade(0.4)) // 重复回报：不得重复入账
	pe.ApplySimulatedFill("ord-s1", trade(1.0)) // 累计到 1.0：只入增量 0.6

	pe.mu.RLock()
	pos := pe.positions["BTCUSDT"]["BTCUSDT-spot"]
	pe.mu.RUnlock()
	if pos == nil || math.Abs(pos.Quantity-1.0) > 1e-9 {
		t.Fatalf("持仓数量 = %+v, want 1.0", pos)
	}
	if math.Abs(pos.AvgEntryPrice-50000) > 1e-9 {
		t.Fatalf("持仓均价 = %v, want 50000", pos.AvgEntryPrice)
	}
	free, _ := balanceOf(pe, "USDT")
	spent := 100000 - free
	// 1.0 BTC @50000，费率 0.1% → 费 50，合计 50050。
	if math.Abs(spent-50050) > 0.01 {
		t.Fatalf("买入结算金额 = %v, want ≈50050", spent)
	}
	btcFree, _ := balanceOf(pe, "BTC")
	if math.Abs(btcFree-1.0) > 1e-9 {
		t.Fatalf("BTC 余额 = %v, want 1.0", btcFree)
	}
	// 第 2 次调用是重复回报，幂等跳过不触发回调。
	if n != 2 {
		t.Fatalf("三次入账（含一次重复回报）应触发 2 次回调，实际 %d", n)
	}

	// 卖出结算：proceeds = qty*price − fee。
	pe.ApplySimulatedFill("ord-s2", model.TradeData{Symbol: "BTCUSDT", ID: "ord-s2", Price: 51000, Quantity: 0.5, Side: "SELL", Timestamp: time.Now().UnixMilli()})
	free2, _ := balanceOf(pe, "USDT")
	got := free2 - free
	want := 0.5*51000 - 0.5*51000*DefaultPaperConfig().FeeRate
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("卖出结算 = %v, want ≈%v", got, want)
	}
	btcFree2, _ := balanceOf(pe, "BTC")
	if math.Abs(btcFree2-0.5) > 1e-9 {
		t.Fatalf("卖出后 BTC 余额 = %v, want 0.5", btcFree2)
	}
	if n != 3 {
		t.Fatalf("卖出后回调应触发 3 次，实际 %d", n)
	}

	// 快照含持仓与真实余额（重启恢复的对账基础）。
	snap := pe.SnapshotAccount()
	if len(snap.Positions) != 1 || math.Abs(snap.Positions[0].Data.Quantity-0.5) > 1e-9 {
		t.Fatalf("快照持仓错误: %+v", snap.Positions)
	}
	if snap.Balance <= 0 || snap.Balance >= 100000 {
		t.Fatalf("快照余额应反映交易后真实值: %v", snap.Balance)
	}
}
