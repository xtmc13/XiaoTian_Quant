//go:build !cgo

package adapter

import (
	"fmt"
	"testing"
)

/* ── Helpers ─────────────────────────────────────────────────── */

var testCounter int

// newTest creates a matching engine with a unique symbol to avoid test pollution.
func newTest() *MatchingEngine {
	testCounter++
	return NewMatchingEngine(fmt.Sprintf("TEST_%d", testCounter))
}

func assert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func assertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func assertFloat(t *testing.T, got, want float64, msg string) {
	t.Helper()
	const eps = 0.0001
	if got < want-eps || got > want+eps {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

// getID extracts order_id from SubmitOrder result as uint64.
func getID(result map[string]any) uint64 {
	return result["order_id"].(uint64)
}

/* ── Limit Order Tests ───────────────────────────────────────── */

func TestLimitOrderGoesToBook(t *testing.T) {
	eng := newTest()

	result, err := eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 1)
	assert(t, err == nil, "SubmitOrder should not error")
	assertEq(t, result["status"].(string), "NEW", "limit order should be NEW")
	assertEq(t, result["side"].(string), "buy", "side should be buy")
	assertFloat(t, result["price"].(float64), 50000.0, "price")
	assertFloat(t, result["quantity"].(float64), 1.0, "quantity")
	assertFloat(t, result["filled"].(float64), 0.0, "filled should be 0")

	snap, _ := eng.Snapshot(5)
	bids := snap["bids"].([][]float64)
	assertEq(t, len(bids), 1, "should have 1 bid level")
	assertFloat(t, bids[0][0], 50000.0, "bid price")
	assertFloat(t, bids[0][1], 1.0, "bid quantity")
}

func TestLimitOrderCrossingMatch(t *testing.T) {
	eng := newTest()

	// Place sell at 50000, then buy at 50001 (cross — buy price >= ask)
	eng.SubmitOrder("sell", "limit", 50000.0, 0.5, 1)
	eng.SubmitOrder("buy", "limit", 50001.0, 0.5, 2)

	// Both fully filled, book empty
	snap, _ := eng.Snapshot(5)
	assertEq(t, len(snap["bids"].([][]float64)), 0, "no bids remain")
	assertEq(t, len(snap["asks"].([][]float64)), 0, "no asks remain")
	assertEq(t, int(eng.TradeCount()), 1, "1 trade recorded")

	trades, _ := eng.GetTrades(10)
	assertFloat(t, trades[0]["price"].(float64), 50000.0, "trade at ask price")
	assertFloat(t, trades[0]["quantity"].(float64), 0.5, "trade quantity")
}

func TestLimitOrderNoCross(t *testing.T) {
	eng := newTest()

	// Sell at 51000, buy at 50000 — no crossing
	eng.SubmitOrder("sell", "limit", 51000.0, 1.0, 1)
	eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 2)

	assertEq(t, int(eng.TradeCount()), 0, "no trade should occur")

	snap, _ := eng.Snapshot(5)
	assertEq(t, len(snap["bids"].([][]float64)), 1, "1 bid level present")
	assertEq(t, len(snap["asks"].([][]float64)), 1, "1 ask level present")
}

func TestLimitOrderMultiLevel(t *testing.T) {
	eng := newTest()

	eng.SubmitOrder("sell", "limit", 50100.0, 1.0, 1)
	eng.SubmitOrder("sell", "limit", 50050.0, 0.5, 2)
	eng.SubmitOrder("sell", "limit", 50200.0, 2.0, 3)
	eng.SubmitOrder("buy", "limit", 49900.0, 1.0, 4)
	eng.SubmitOrder("buy", "limit", 50000.0, 0.3, 5)

	snap, _ := eng.Snapshot(10)
	bids := snap["bids"].([][]float64)
	asks := snap["asks"].([][]float64)

	assertEq(t, len(bids), 2, "2 bid levels")
	assertFloat(t, bids[0][0], 50000.0, "best bid")
	assertFloat(t, bids[1][0], 49900.0, "second bid")

	assertEq(t, len(asks), 3, "3 ask levels")
	assertFloat(t, asks[0][0], 50050.0, "best ask")
	assertFloat(t, asks[1][0], 50100.0, "second ask")
	assertFloat(t, asks[2][0], 50200.0, "third ask")
}

func TestLimitOrderPartialFill(t *testing.T) {
	eng := newTest()

	// Sell 2.0 at 50000
	eng.SubmitOrder("sell", "limit", 50000.0, 2.0, 1)
	// Buy 1.0 at 50000 — crosses, partial fill
	result, _ := eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 2)

	assertFloat(t, result["filled"].(float64), 1.0, "buyer filled 1.0")
	assertEq(t, int(eng.TradeCount()), 1, "1 trade")

	snap, _ := eng.Snapshot(5)
	asks := snap["asks"].([][]float64)
	assertEq(t, len(asks), 1, "1 ask level remains")
	assertFloat(t, asks[0][1], 1.0, "1.0 remaining on ask")
}

func TestLimitOrderMultiMatch(t *testing.T) {
	eng := newTest()

	eng.SubmitOrder("sell", "limit", 50000.0, 0.5, 1)
	eng.SubmitOrder("sell", "limit", 50000.0, 0.3, 2)
	result, _ := eng.SubmitOrder("buy", "limit", 50000.0, 0.6, 3)

	assertFloat(t, result["filled"].(float64), 0.6, "buyer filled 0.6")
	// Two sells filled against one buy: 0.5 from first seller + 0.1 from second = 2 trades
	assertEq(t, int(eng.TradeCount()), 2, "2 trades (0.5 to seller1 + 0.1 to seller2)")

	// Second seller partially remaining (0.3 - 0.1 = 0.2)
	snap, _ := eng.Snapshot(5)
	asks := snap["asks"].([][]float64)
	assertEq(t, len(asks), 1, "1 ask level remains")
	assertFloat(t, asks[0][1], 0.2, "0.2 remaining")
}

/* ── Market Order Tests ───────────────────────────────────────── */

func TestMarketBuyFillsBestAsk(t *testing.T) {
	eng := newTest()

	eng.SubmitOrder("sell", "limit", 50000.0, 1.0, 1)
	result, _ := eng.SubmitOrder("buy", "market", 0, 0.5, 2)

	assertEq(t, result["status"].(string), "FILLED", "market order filled")
	assertFloat(t, result["filled"].(float64), 0.5, "filled 0.5")

	trades, _ := eng.GetTrades(10)
	assertFloat(t, trades[0]["price"].(float64), 50000.0, "trade at ask price")
}

func TestMarketBuyMultipleLevels(t *testing.T) {
	eng := newTest()

	eng.SubmitOrder("sell", "limit", 50000.0, 0.3, 1)
	eng.SubmitOrder("sell", "limit", 50100.0, 1.0, 2)
	result, _ := eng.SubmitOrder("buy", "market", 0, 0.5, 3)

	assertFloat(t, result["filled"].(float64), 0.5, "filled 0.5")
	// First fills 0.3 at 50000, then 0.2 at 50100
	assertEq(t, int(eng.TradeCount()), 2, "2 trades across 2 levels")

	trades, _ := eng.GetTrades(10)
	assertFloat(t, trades[0]["price"].(float64), 50000.0, "first trade @50000")
	assertFloat(t, trades[0]["quantity"].(float64), 0.3, "qty 0.3")
	assertFloat(t, trades[1]["price"].(float64), 50100.0, "second trade @50100")
	assertFloat(t, trades[1]["quantity"].(float64), 0.2, "qty 0.2")
}

func TestMarketSellFillsBestBid(t *testing.T) {
	eng := newTest()

	eng.SubmitOrder("buy", "limit", 49900.0, 1.0, 1)
	result, _ := eng.SubmitOrder("sell", "market", 0, 0.7, 2)

	assertEq(t, result["status"].(string), "FILLED", "market order filled")
	assertFloat(t, result["filled"].(float64), 0.7, "filled 0.7")

	trades, _ := eng.GetTrades(10)
	assertFloat(t, trades[0]["price"].(float64), 49900.0, "trade at bid price")
}

func TestMarketOrderEmptyBook(t *testing.T) {
	eng := newTest()

	result, _ := eng.SubmitOrder("buy", "market", 0, 1.0, 1)

	assertEq(t, result["status"].(string), "FILLED", "status is FILLED")
	assertFloat(t, result["filled"].(float64), 0.0, "filled 0 (no liquidity)")
	assertEq(t, int(eng.TradeCount()), 0, "no trades")
}

func TestMarketOrderLargeSize(t *testing.T) {
	eng := newTest()

	eng.SubmitOrder("sell", "limit", 50000.0, 0.5, 1)
	eng.SubmitOrder("sell", "limit", 50100.0, 0.3, 2)
	result, _ := eng.SubmitOrder("buy", "market", 0, 2.0, 3)

	assertFloat(t, result["filled"].(float64), 0.8, "filled only available 0.8")
	assertEq(t, int(eng.TradeCount()), 2, "2 trades")

	snap, _ := eng.Snapshot(5)
	assertEq(t, len(snap["asks"].([][]float64)), 0, "all asks consumed")
}

/* ── Cancel Order Tests ───────────────────────────────────────── */

func TestCancelOrder(t *testing.T) {
	eng := newTest()

	result, _ := eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 1)
	orderID := getID(result)

	err := eng.CancelOrder(orderID)
	assert(t, err == nil, "cancel should succeed")

	snap, _ := eng.Snapshot(5)
	assertEq(t, len(snap["bids"].([][]float64)), 0, "order removed from book")
}

func TestCancelNonExistentOrder(t *testing.T) {
	eng := newTest()
	err := eng.CancelOrder(99999)
	assert(t, err != nil, "cancel non-existent should error")
}

func TestCancelFilledOrder(t *testing.T) {
	eng := newTest()

	eng.SubmitOrder("sell", "limit", 50000.0, 1.0, 1)
	result, _ := eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 2)
	orderID := getID(result)

	err := eng.CancelOrder(orderID)
	assert(t, err != nil, "cancel filled order should error")
}

func TestCancelThenPlaceNew(t *testing.T) {
	eng := newTest()

	result1, _ := eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 1)
	eng.CancelOrder(getID(result1))

	result2, _ := eng.SubmitOrder("buy", "limit", 50000.0, 0.5, 1)
	assert(t, getID(result2) > getID(result1), "new order gets larger ID")

	snap, _ := eng.Snapshot(5)
	bids := snap["bids"].([][]float64)
	assertEq(t, len(bids), 1, "1 bid level after cancel + new")
	assertFloat(t, bids[0][1], 0.5, "only 0.5 quantity")
}

/* ── Edge Case Tests ──────────────────────────────────────────── */

func TestZeroQuantity(t *testing.T) {
	eng := newTest()
	_, err := eng.SubmitOrder("buy", "limit", 50000.0, 0, 1)
	assert(t, err != nil, "zero quantity should error")
}

func TestNegativeQuantity(t *testing.T) {
	eng := newTest()
	_, err := eng.SubmitOrder("buy", "limit", 50000.0, -1.0, 1)
	assert(t, err != nil, "negative quantity should error")
}

func TestManyOrders(t *testing.T) {
	eng := newTest()

	for i := 0; i < 100; i++ {
		price := 49000.0 + float64(i)*1.0
		eng.SubmitOrder("buy", "limit", price, 0.1, uint64(i+1))
	}

	snap, _ := eng.Snapshot(200)
	bids := snap["bids"].([][]float64)
	assertEq(t, len(bids), 100, "100 price levels")

	for i := 1; i < len(bids); i++ {
		assert(t, bids[i-1][0] > bids[i][0],
			fmt.Sprintf("bids[%d]=%v > bids[%d]=%v", i-1, bids[i-1][0], i, bids[i][0]))
	}
}

func TestSnapshotDepthCap(t *testing.T) {
	eng := newTest()

	for i := 0; i < 20; i++ {
		price := 50000.0 + float64(i)*10.0
		eng.SubmitOrder("sell", "limit", price, 0.1, uint64(i+1))
	}

	snap, _ := eng.Snapshot(5)
	asks := snap["asks"].([][]float64)
	assertEq(t, len(asks), 5, "snapshot capped at 5 levels")
}

func TestEmptySnapshot(t *testing.T) {
	eng := newTest()
	snap, _ := eng.Snapshot(10)
	assertEq(t, len(snap["bids"].([][]float64)), 0, "empty bids")
	assertEq(t, len(snap["asks"].([][]float64)), 0, "empty asks")
}

func TestGetTradesLimit(t *testing.T) {
	eng := newTest()

	for i := 0; i < 5; i++ {
		eng.SubmitOrder("sell", "limit", 50000.0, 1.0, uint64(i*2+1))
		eng.SubmitOrder("buy", "limit", 50000.0, 1.0, uint64(i*2+2))
	}

	assertEq(t, int(eng.TradeCount()), 5, "5 trades")
	trades, _ := eng.GetTrades(3)
	assertEq(t, len(trades), 3, "limit 3 returns 3")
	tradesAll, _ := eng.GetTrades(100)
	assertEq(t, len(tradesAll), 5, "limit 100 returns all 5")
}

func TestTradeHistoryCapped(t *testing.T) {
	eng := newTest()

	for i := 0; i < 1005; i++ {
		eng.SubmitOrder("sell", "limit", 50000.0, 1.0, uint64(i*2+1))
		eng.SubmitOrder("buy", "limit", 50000.0, 1.0, uint64(i*2+2))
	}

	tradesAll, _ := eng.GetTrades(0)
	assert(t, len(tradesAll) <= 1000, "trade history capped at 1000")
	assertEq(t, int(eng.TradeCount()), 1005, "trade sequence still counts all")
}

func TestEngineIsolation(t *testing.T) {
	eng1 := newTest()
	eng2 := newTest()

	eng1.SubmitOrder("buy", "limit", 50000.0, 1.0, 1)
	eng2.SubmitOrder("sell", "limit", 3000.0, 2.0, 1)

	snap1, _ := eng1.Snapshot(5)
	snap2, _ := eng2.Snapshot(5)
	assertEq(t, len(snap1["bids"].([][]float64)), 1, "eng1 has 1 bid")
	assertEq(t, len(snap2["asks"].([][]float64)), 1, "eng2 has 1 ask")
}

func TestEngineDestroyAndRecreate(t *testing.T) {
	eng := NewMatchingEngine("DESTROY_TEST_SYMBOL")
	eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 1)
	eng.Destroy()

	// New engine with same symbol should be fresh
	eng2 := NewMatchingEngine("DESTROY_TEST_SYMBOL")
	snap, _ := eng2.Snapshot(5)
	assertEq(t, len(snap["bids"].([][]float64)), 0, "fresh engine after destroy")
}

func TestPriceTimePriorityLargerBuy(t *testing.T) {
	eng := newTest()

	// Two sells at same price, placed at different times
	eng.SubmitOrder("sell", "limit", 50000.0, 1.0, 1)
	eng.SubmitOrder("sell", "limit", 50000.0, 2.0, 2)

	// Buy crosses, should fill first seller first (price-time priority)
	eng.SubmitOrder("buy", "limit", 50000.0, 1.5, 3)

	snap, _ := eng.Snapshot(5)
	asks := snap["asks"].([][]float64)
	// First seller fully filled (1.0), second partially (0.5)
	assertEq(t, len(asks), 1, "1 ask level remains")
	assertFloat(t, asks[0][1], 1.5, "1.5 remaining (2.0 - 0.5)")
}

/* ── Benchmark ────────────────────────────────────────────────── */

func BenchmarkSubmitLimitOrder(b *testing.B) {
	eng := NewMatchingEngine("BENCH_LIMIT")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		price := 50000.0 + float64(i%100)*10.0
		eng.SubmitOrder("buy", "limit", price, 0.1, uint64(i))
	}
}

func BenchmarkSubmitAndMatch(b *testing.B) {
	eng := NewMatchingEngine("BENCH_MATCH")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng.SubmitOrder("sell", "limit", 50000.0, 1.0, uint64(i*2+1))
		eng.SubmitOrder("buy", "limit", 50000.0, 1.0, uint64(i*2+2))
	}
}
