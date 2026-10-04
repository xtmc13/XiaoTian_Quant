package handler

import (
	"math"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// TestLocalTradeHistoryCommission 本地成交记录的手续费列（2026-10-04 修）：
// xt_orders 无手续费列，commission 曾恒 0——现按 paper 结算同公式
// （price×qty×feeRate，与 store.StrategyPaperPnL 双边扣费同口径）逐行输出，
// 且 BUY 开多行也扣当笔手续费（此前漏扣，行盈亏之和高于账户实际）。
func TestLocalTradeHistoryCommission(t *testing.T) {
	repo := store.NewOrderRepo()
	nowMs := time.Now().UnixMilli()
	mk := func(id, side, oid string, qty, avg float64, ts int64) *store.OrderRecord {
		return &store.OrderRecord{
			ID: id, Symbol: "BTCUSDT", Side: side, OrderType: "MARKET",
			Quantity: qty, Filled: qty, Status: "FILLED", Exchange: "paper",
			ClientOID: oid, AvgFillPrice: avg, CreatedAt: ts, UpdatedAt: ts,
		}
	}
	// 买 0.1@50000 + 买 0.2@51000 → 成本 50666.67；卖 0.1@52000。
	rows := []*store.OrderRecord{
		mk("ord-lt-1", "BUY", "sig:cfgLT:1", 0.1, 50000, nowMs-3000),
		mk("ord-lt-2", "BUY", "sig:cfgLT:2", 0.2, 51000, nowMs-2000),
		mk("ord-lt-3", "SELL", "sig:cfgLT:3", 0.1, 52000, nowMs-1000),
	}
	for _, r := range rows {
		if err := repo.Create(r); err != nil {
			t.Fatal(err)
		}
	}

	const feeRate = 0.001 // paper 默认费率（PaperConfig.FeeRate）
	trades := localTradeHistory("", 50, "cfgLT")
	if len(trades) != 3 {
		t.Fatalf("trades = %d, want 3", len(trades))
	}
	byID := map[string]map[string]any{}
	for _, tr := range trades {
		byID[tr["id"].(string)] = tr
	}

	close := func(got, want float64) bool { return math.Abs(got-want) < 1e-6 }
	check := func(id string, wantCommission, wantPnl float64) {
		t.Helper()
		tr, ok := byID[id]
		if !ok {
			t.Fatalf("missing trade %s", id)
		}
		if got := tr["commission"].(float64); !close(got, wantCommission) {
			t.Errorf("%s commission = %v, want %v", id, got, wantCommission)
		}
		if got := tr["pnl"].(float64); !close(got, wantPnl) {
			t.Errorf("%s pnl = %v, want %v", id, got, wantPnl)
		}
		if tr["commission_asset"].(string) != "USDT" {
			t.Errorf("%s commission_asset = %v, want USDT", id, tr["commission_asset"])
		}
	}
	check("ord-lt-1", 50000*0.1*feeRate, -50000*0.1*feeRate) // 开多：pnl=-费
	check("ord-lt-2", 51000*0.2*feeRate, -51000*0.2*feeRate)
	avgCost := (0.1*50000 + 0.2*51000) / 0.3
	check("ord-lt-3", 52000*0.1*feeRate, (52000-avgCost)*0.1-52000*0.1*feeRate)

	// 口径一致性：行 pnl 之和 == StrategyPaperPnL 的总盈亏（无浮动时）。
	sum := 0.0
	for _, tr := range trades {
		sum += tr["pnl"].(float64)
	}
	_, _, _, _, total, ok := store.StrategyPaperPnL("cfgLT", "BTCUSDT", 0, feeRate, false)
	if !ok {
		t.Fatal("StrategyPaperPnL: expected trades")
	}
	if !close(sum, total) {
		t.Fatalf("Σrow pnl = %v, StrategyPaperPnL total = %v（口径必须一致）", sum, total)
	}
}
