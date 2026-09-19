package paper

import (
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
