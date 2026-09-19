//go:build !cgo

package adapter

import (
	"fmt"
	"strings"
	"testing"
)

// staticBalance 测试用固定余额提供者。
type staticBalance struct {
	m map[uint64]map[string]float64
}

func (s *staticBalance) Available(userID uint64, asset string) float64 {
	if s.m[userID] == nil {
		return 0
	}
	return s.m[userID][asset]
}

var balTestCounter int

// newBalTest 建一个带余额校验的引擎。userBalances 用规范币种键（BTC/USDT），
// 内部按引擎 symbol 推导出的 base/quote 重写（测试 symbol 带唯一后缀避免串台）。
func newBalTest(t *testing.T, userBalances map[uint64]map[string]float64) (*MatchingEngine, string, string) {
	t.Helper()
	balTestCounter++
	eng := NewMatchingEngine(fmt.Sprintf("BTCUSDT_T%d", balTestCounter))
	base, quote := splitSymbolAssets(eng.symbol)
	if quote != "USDT" {
		t.Fatalf("测试 symbol 推导异常: %s", eng.symbol)
	}
	t.Cleanup(eng.Destroy)
	remapped := make(map[uint64]map[string]float64, len(userBalances))
	for uid, assets := range userBalances {
		m := make(map[string]float64, len(assets))
		for asset, amt := range assets {
			switch strings.ToUpper(asset) {
			case "BTC":
				m[base] = amt
			case "USDT":
				m[quote] = amt
			default:
				m[asset] = amt
			}
		}
		remapped[uid] = m
	}
	eng.SetBalanceProvider(&staticBalance{m: remapped})
	return eng, base, quote
}

// ample 给做市用户无穷余额（测试关注点在被测方的截断/拒绝）。
func ample() map[string]float64 { return map[string]float64{"BTC": 1e9, "USDT": 1e12} }

func TestMarketSellRejectedWithoutHoldings(t *testing.T) {
	eng, base, _ := newBalTest(t, map[uint64]map[string]float64{
		1: ample(),       // 做市方
		7: {"USDT": 1e6}, // 被测方：有 USDT，无 BTC
	})

	// 盘口有买单，纯下跌行情下用户尝试市价卖出不存在的持仓。
	if _, err := eng.SubmitOrder("buy", "limit", 49000.0, 1.0, 1); err != nil {
		t.Fatalf("做市买单: %v", err)
	}
	_, err := eng.SubmitOrder("sell", "market", 0, 0.5, 7)
	if err == nil {
		t.Fatalf("无持仓市价卖出必须被拒绝（不允许负余额, base=%s）", base)
	}
}

func TestMarketSellPartialFillCappedByHoldings(t *testing.T) {
	balances := map[uint64]map[string]float64{
		1: ample(),
		7: {"BTC": 0.3, "USDT": 0},
	}
	eng, _, _ := newBalTest(t, balances)

	if _, err := eng.SubmitOrder("buy", "limit", 49000.0, 1.0, 1); err != nil {
		t.Fatalf("做市买单: %v", err)
	}
	res, err := eng.SubmitOrder("sell", "market", 0, 1.0, 7)
	if err != nil {
		t.Fatalf("持有 0.3 卖出 1.0 应部分成交而非拒绝: %v", err)
	}
	if got := res["filled"].(float64); got != 0.3 {
		t.Fatalf("卖出应被持仓截断在 0.3，实际 %.4f", got)
	}
	assertEq(t, int(eng.TradeCount()), 1, "1 trade")
}

func TestMarketBuyPartialFillCappedByQuote(t *testing.T) {
	balances := map[uint64]map[string]float64{
		1: ample(),
		7: {"USDT": 10000},
	}
	eng, _, _ := newBalTest(t, balances)

	if _, err := eng.SubmitOrder("sell", "limit", 50000.0, 1.0, 1); err != nil {
		t.Fatalf("做市卖单: %v", err)
	}
	res, err := eng.SubmitOrder("buy", "market", 0, 1.0, 7) // 需要 50000，只有 10000
	if err != nil {
		t.Fatalf("市价买入应允许部分成交: %v", err)
	}
	if got := res["filled"].(float64); got != 0.2 {
		t.Fatalf("买入应按可用 quote 截断到 0.2，实际 %.4f", got)
	}
}

func TestLimitSellRejectedWithoutHoldings(t *testing.T) {
	balances := map[uint64]map[string]float64{
		7: {"USDT": 100000},
	}
	eng, _, _ := newBalTest(t, balances)
	_, err := eng.SubmitOrder("sell", "limit", 50000.0, 1.0, 7)
	if err == nil {
		t.Fatal("无持仓限价卖出必须被拒绝")
	}
}

func TestLimitBuyRejectedWithoutQuote(t *testing.T) {
	balances := map[uint64]map[string]float64{
		7: {"BTC": 1},
	}
	eng, _, _ := newBalTest(t, balances)
	_, err := eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 7) // 需要 5 万 USDT
	if err == nil {
		t.Fatal("quote 不足限价买入必须被拒绝")
	}
}

func TestSimulatedMarketMakerExemptFromBalanceChecks(t *testing.T) {
	eng, _, _ := newBalTest(t, map[uint64]map[string]float64{})
	// userID=0 的模拟做市单不校验余额（otherwise SimulateTrading 无法挂双边）。
	if _, err := eng.SubmitOrder("sell", "limit", 50000.0, 1.0, 0); err != nil {
		t.Fatalf("做市单应豁免余额校验: %v", err)
	}
}

func TestNoProviderKeepsLegacyBehavior(t *testing.T) {
	eng := newTest()
	// 无 provider 时不校验（向后兼容）。
	if _, err := eng.SubmitOrder("sell", "market", 0, 1.0, 99); err != nil {
		t.Fatalf("无 provider 不得拒绝: %v", err)
	}
}
