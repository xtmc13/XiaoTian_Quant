package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/config"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/paper"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// 2026-10-08 生产实锤回归：重启恢复仓位后平仓单被拒"余额不足"。
// 事故形态：PortfolioManager "default" 账户每次重启只按初始余额重建 USDT
// （base 资产归零），而 OMS 余额锁查的就是它——paper 账户（paper/paper.go，
// 经 RestoreAccount 完整恢复）实际持有 16.82 SOL / 0.0218 BTC，平仓单却被
// "insufficient SOL balance: 0.00 < 12.52" 误拒，策略卡死在"有仓位但平不掉"。
// 修复：paper 单的余额锁改用 paper 交易所账户（唯一持久化真实账本）。

// setupPaperCloseTest 初始化临时 DB + 完整 app 上下文，并把 paper 账户
// 重置为给定快照（模拟"重启后"：paper 账户经 RestoreAccount 恢复了历史
// 资产，而 PortfolioManager 内存镜像只有 USDT 初始余额）。
func setupPaperCloseTest(t *testing.T, snap paper.AccountSnapshot) *Context {
	t.Helper()
	dir, err := os.MkdirTemp("", "app_restore_close_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		store.CloseDB()
		_ = os.RemoveAll(dir)
	})
	t.Setenv("DB_PATH", filepath.Join(dir, "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-not-for-production-use-only")
	if err := store.InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	ctx := Get()
	ctx.Shutdown()
	if err := ctx.Init(config.Default()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	pe := paper.GetPaperExchange()
	orig := pe.SnapshotAccount()
	pe.RestoreAccount(snap)
	t.Cleanup(func() { pe.RestoreAccount(orig) })
	return ctx
}

func solAccountSnap(amount float64) paper.AccountSnapshot {
	return paper.AccountSnapshot{
		Enabled: true, Balance: 96119, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 96119, Free: 96119},
			"SOL":  {Currency: "SOL", Total: amount, Free: amount},
		},
		Positions: []paper.PositionSnapshot{{Data: model.PositionData{
			ID: "SOLUSDT-spot", Symbol: "SOLUSDT", Side: "LONG",
			Quantity: amount, AvgEntryPrice: 119.37,
		}}},
	}
}

// paperFree 读取 paper 账户可用余额（经 SnapshotAccount，兼容修复前代码，
// 保证本测试在旧代码上可编译运行——红绿验证）。
func paperFree(pe *paper.PaperExchange, currency string) float64 {
	snap := pe.SnapshotAccount()
	if b := snap.Balances[currency]; b != nil {
		return b.Free
	}
	return 0
}

func seedClosedTestLedger(t *testing.T, strategyID, symbol string, qty, price float64) {
	t.Helper()
	nowMs := time.Now().UnixMilli()
	if err := store.NewOrderRepo().Create(&store.OrderRecord{
		ID: "ord-close-seed-" + strategyID, Symbol: symbol, Side: "BUY",
		OrderType: "MARKET", Quantity: qty, Filled: qty, Status: "FILLED",
		Exchange: "paper", ClientOID: "sig:" + strategyID + ":seed",
		AvgFillPrice: price, CreatedAt: nowMs, UpdatedAt: nowMs,
	}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
}

// OMS 直复现：paper 市价卖出账户实际持有的 base 必须成交
// （修复前：PortfolioManager 镜像无 SOL → REJECTED "insufficient SOL
// balance: 0.00 < 12.52"）。
func TestPaperSellWithRestoredAccountSucceeds(t *testing.T) {
	setupPaperCloseTest(t, solAccountSnap(16.824301207189286))

	req := &order.Request{
		Symbol: "SOLUSDT", Side: model.SideSell, OrderType: model.TypeMarket,
		Quantity: 12.519152, Exchange: "paper",
		ClientOID: "sig:oms-direct:1",
	}
	ord, err := order.GetOrderManager().PlaceOrder(req)
	if err != nil {
		t.Fatalf("paper 平仓单必须成交（生产误拒回归）: %v", err)
	}
	if ord.Status != model.StatusFilled {
		t.Fatalf("order status = %s, want FILLED", ord.Status)
	}

	// 账平：SOL 只剩 16.824-12.519，且无锁定残留；USDT 增加。
	pe := paper.GetPaperExchange()
	if got := paperFree(pe, "SOL"); got < 4.30 || got > 4.31 {
		t.Fatalf("SOL free = %v, want ≈4.305", got)
	}
	snap := pe.SnapshotAccount()
	if b := snap.Balances["SOL"]; b.Used != 0 {
		t.Fatalf("SOL used = %v, want 0（锁定必须随结算释放）", b.Used)
	}
	if got := paperFree(pe, "USDT"); got <= 96119 {
		t.Fatalf("USDT free = %v, 卖出后必须增加", got)
	}
}

// 信号平仓链路（closePositionFromSignal 账本兜底）：恢复仓位 → CLOSE 信号 →
// 成交；账户持仓与账本同步减少。
func TestClosePositionFromSignalAfterRestore(t *testing.T) {
	ctx := setupPaperCloseTest(t, solAccountSnap(16.824301207189286))
	seedClosedTestLedger(t, "rst-close", "SOLUSDT", 12.519152, 119.64)

	ctx.closePositionFromSignal(model.Signal{
		Symbol: "SOLUSDT", Direction: "CLOSE", Strategy: "rst-close",
	})

	pe := paper.GetPaperExchange()
	if got := paperFree(pe, "SOL"); got < 4.30 || got > 4.31 {
		t.Fatalf("平仓后 SOL free = %v, want ≈4.305（修复前拒单仍是 16.82）", got)
	}
	// 账本出现一笔 FILLED 的卖出（不是 REJECTED）。
	recs, err := store.GetOrderRepo().List(map[string]any{"symbol": "SOLUSDT"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundSell := false
	for _, r := range recs {
		if r.Side == "SELL" && r.Status == "FILLED" {
			foundSell = true
		}
		if r.Side == "SELL" && r.Status == "REJECTED" {
			t.Fatalf("平仓单不得被拒: %+v", r)
		}
	}
	if !foundSell {
		t.Fatal("缺少 FILLED 的平仓卖出单")
	}
}

// 钳制平仓：账本净额 12.52 但 paper 账户只剩 5.0（恢复注入被钳/账户被
// 部分划转）——按可卖背书平掉 5.0，不得拒单空转。
func TestClosePositionFromSignalClampedToBacking(t *testing.T) {
	ctx := setupPaperCloseTest(t, solAccountSnap(5.0))
	seedClosedTestLedger(t, "rst-clamp", "SOLUSDT", 12.519152, 119.64)

	ctx.closePositionFromSignal(model.Signal{
		Symbol: "SOLUSDT", Direction: "CLOSE", Strategy: "rst-clamp",
	})

	pe := paper.GetPaperExchange()
	if got := paperFree(pe, "SOL"); got != 0 {
		t.Fatalf("钳制平仓后 SOL free = %v, want 0", got)
	}
	recs, _ := store.GetOrderRepo().List(map[string]any{"symbol": "SOLUSDT"}, 0)
	for _, r := range recs {
		if r.Side == "SELL" && r.Status == "REJECTED" {
			t.Fatalf("钳制后仍按 12.52 下单被拒（未钳）: %+v", r)
		}
		if r.Side == "SELL" && r.Status == "FILLED" && r.Filled != 5.0 {
			t.Fatalf("平仓量 = %v, want 5.0（账户背书）", r.Filled)
		}
	}
}

// 零背书跳过：账户没有 SOL 时不再制造拒单（账本净额是历史残留），
// 策略已随 CLOSE 信号复位为空仓，不卡死。
func TestClosePositionFromSignalSkippedWithoutBacking(t *testing.T) {
	ctx := setupPaperCloseTest(t, paper.AccountSnapshot{
		Enabled: true, Balance: 96119, InitialBalance: 100000,
		Balances: map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: 96119, Free: 96119},
		},
	})
	seedClosedTestLedger(t, "rst-skip", "SOLUSDT", 12.519152, 119.64)

	ctx.closePositionFromSignal(model.Signal{
		Symbol: "SOLUSDT", Direction: "CLOSE", Strategy: "rst-skip",
	})

	recs, _ := store.GetOrderRepo().List(map[string]any{"symbol": "SOLUSDT"}, 0)
	for _, r := range recs {
		if r.Side == "SELL" {
			t.Fatalf("零背书不得下单（拒单空转回归）: %+v", r)
		}
	}
}

// swap 策略的账本兜底平仓：订单必须带 MarketType=swap（修复前按现货处理，
// 锁错 base 钱包）。
func TestClosePositionFromSignalSwapCarriesMarketType(t *testing.T) {
	ctx := setupPaperCloseTest(t, solAccountSnap(16.824301207189286))
	seedClosedTestLedger(t, "rst-swap", "SOLUSDT", 12.519152, 119.64)
	store.SetStrategyConfig("rst-swap", map[string]any{
		"id": "rst-swap", "strategy_type": "macd", "symbol": "SOLUSDT",
		"execution_mode": "paper", "market_type": "swap", "direction": "dual",
		"config_json": `{"market_type":"swap","leverage":5,"direction":"dual"}`,
	})
	t.Cleanup(func() { store.DeleteStrategyConfig("rst-swap") })

	ctx.closePositionFromSignal(model.Signal{
		Symbol: "SOLUSDT", Direction: "CLOSE", Strategy: "rst-swap",
	})

	history := order.GetOrderManager().GetOrderHistory("SOLUSDT", 50)
	var closeOrd *model.OrderData
	for _, o := range history {
		if o.Side == model.SideSell && strings.HasPrefix(o.ClientOID, "sig:rst-swap:") {
			closeOrd = o
		}
	}
	if closeOrd == nil {
		t.Fatal("缺少平仓单")
	}
	if closeOrd.Status != model.StatusFilled {
		t.Fatalf("swap 平仓单 status = %s, want FILLED", closeOrd.Status)
	}
	if closeOrd.MarketType != model.MarketSwap {
		t.Fatalf("swap 策略平仓单 MarketType = %q, want swap（现货钱包误锁回归）", closeOrd.MarketType)
	}
	if closeOrd.Exchange != "paper" {
		t.Fatalf("paper 策略平仓单 Exchange = %q, want paper", closeOrd.Exchange)
	}
}
