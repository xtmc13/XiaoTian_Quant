//go:build cgo && xtengine

package adapter

import (
	"fmt"
	"math"
	"testing"
)

// cgoTestCounter gives each test a unique symbol to avoid engine registry pollution.
var cgoTestCounter int

func newCGOTest(t *testing.T) *MatchingEngine {
	t.Helper()
	cgoTestCounter++
	return NewMatchingEngine(formatSymbol(cgoTestCounter))
}

func formatSymbol(n int) string {
	return fmt.Sprintf("CGO_TEST_%d", n)
}

func assertCGO(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func assertEqCGO[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func assertFloatCGO(t *testing.T, got, want float64, msg string) {
	t.Helper()
	const eps = 0.0001
	if got < want-eps || got > want+eps {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func TestCGOLimitOrderGoesToBook(t *testing.T) {
	eng := newCGOTest(t)

	result, err := eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 1)
	assertCGO(t, err == nil, "SubmitOrder should not error")
	assertEqCGO(t, result["status"].(string), "NEW", "new order status")
	orderID, ok := result["order_id"].(uint64)
	assertCGO(t, ok, "order_id should be normalized to uint64")
	assertEqCGO(t, orderID > 0, true, "order_id should be positive")

	snap, err := eng.Snapshot(5)
	assertCGO(t, err == nil, "Snapshot should not error")
	bids := snap["bids"].([]interface{})
	assertEqCGO(t, len(bids), 1, "should have 1 bid level")
}

func TestCGOLimitOrderCrossingMatch(t *testing.T) {
	eng := newCGOTest(t)

	eng.SubmitOrder("sell", "limit", 50000.0, 0.5, 1)
	result, err := eng.SubmitOrder("buy", "limit", 50000.0, 0.5, 2)
	assertCGO(t, err == nil, "SubmitOrder should not error")
	assertEqCGO(t, result["status"].(string), "FILLED", "crossing order status")

	trades := result["trades"].([]interface{})
	assertEqCGO(t, len(trades), 1, "1 trade should be generated")

	snap, _ := eng.Snapshot(5)
	bids := snap["bids"].([]interface{})
	asks := snap["asks"].([]interface{})
	assertEqCGO(t, len(bids), 0, "no bids remain")
	assertEqCGO(t, len(asks), 0, "no asks remain")
}

func TestCGOMarketBuyFillsBestAsk(t *testing.T) {
	eng := newCGOTest(t)

	eng.SubmitOrder("sell", "limit", 50000.0, 1.0, 1)
	result, err := eng.SubmitOrder("buy", "market", 0, 0.5, 2)
	assertCGO(t, err == nil, "market order should not error")
	assertEqCGO(t, result["status"].(string), "FILLED", "market order status")

	trades := result["trades"].([]interface{})
	assertEqCGO(t, len(trades), 1, "1 trade")
}

func TestCGOCancelOrder(t *testing.T) {
	eng := newCGOTest(t)

	result, _ := eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 1)
	orderID, ok := result["order_id"].(uint64)
	assertCGO(t, ok, "order_id should be normalized to uint64")

	err := eng.CancelOrder(orderID)
	assertCGO(t, err == nil, "cancel should succeed")

	snap, _ := eng.Snapshot(5)
	bids := snap["bids"].([]interface{})
	assertEqCGO(t, len(bids), 0, "order removed from book")
}

func TestCGOSnapshotBestPrices(t *testing.T) {
	eng := newCGOTest(t)

	eng.SubmitOrder("buy", "limit", 49900.0, 1.0, 1)
	eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 2)
	eng.SubmitOrder("sell", "limit", 50100.0, 1.0, 3)

	snap, err := eng.Snapshot(5)
	assertCGO(t, err == nil, "Snapshot should not error")
	assertFloatCGO(t, snap["best_bid"].(float64), 50000.0, "best bid")
	assertFloatCGO(t, snap["best_ask"].(float64), 50100.0, "best ask")
}

func TestCGOEngineIsolation(t *testing.T) {
	cgoTestCounter++
	sym1 := formatSymbol(cgoTestCounter)
	cgoTestCounter++
	sym2 := formatSymbol(cgoTestCounter)

	eng1 := NewMatchingEngine(sym1)
	eng2 := NewMatchingEngine(sym2)

	eng1.SubmitOrder("buy", "limit", 50000.0, 1.0, 1)
	eng2.SubmitOrder("sell", "limit", 3000.0, 2.0, 1)

	snap1, _ := eng1.Snapshot(5)
	snap2, _ := eng2.Snapshot(5)
	assertEqCGO(t, len(snap1["bids"].([]interface{})), 1, "eng1 has 1 bid")
	assertEqCGO(t, len(snap2["asks"].([]interface{})), 1, "eng2 has 1 ask")
}

func TestCGOTradeCount(t *testing.T) {
	eng := newCGOTest(t)

	assertEqCGO(t, int(eng.TradeCount()), 0, "initial trade count 0")

	eng.SubmitOrder("sell", "limit", 50000.0, 1.0, 1)
	eng.SubmitOrder("buy", "limit", 50000.0, 1.0, 2)

	assertEqCGO(t, int(eng.TradeCount()), 1, "1 trade")
}

type cgoStaticBalance struct {
	values map[string]float64
}

func (b cgoStaticBalance) Available(userID uint64, asset string) float64 {
	return b.values[asset]
}

func TestCGOBalanceProviderCapsMarketBuy(t *testing.T) {
	eng := newCGOTest(t)
	eng.SetBalanceProvider(cgoStaticBalance{values: map[string]float64{"USDT": 10000}})
	if _, err := eng.SubmitOrder("sell", "limit", 50000, 1, 0); err != nil {
		t.Fatalf("seed ask: %v", err)
	}

	result, err := eng.SubmitOrder("buy", "market", 0, 1, 2)
	if err != nil {
		t.Fatalf("market buy: %v", err)
	}
	assertFloatCGO(t, result["filled"].(float64), 0.2, "filled quantity capped by quote balance")
}

type cgoNaNBalance struct{}

func (cgoNaNBalance) Available(uint64, string) float64 { return math.NaN() }

func TestCGORejectsNaNAvailableBalance(t *testing.T) {
	eng := newCGOTest(t)
	eng.SetBalanceProvider(cgoNaNBalance{})
	cases := []struct {
		side, orderType string
		price, quantity float64
	}{
		{"buy", "limit", 100, 1},
		{"sell", "limit", 100, 1},
		{"buy", "market", 0, 1},
		{"sell", "market", 0, 1},
	}
	for _, tc := range cases {
		if _, err := eng.SubmitOrder(tc.side, tc.orderType, tc.price, tc.quantity, 1); err == nil {
			t.Errorf("expected invalid available balance to reject order: %+v", tc)
		}
	}
}
