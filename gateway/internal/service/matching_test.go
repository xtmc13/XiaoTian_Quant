package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

func TestMain(m *testing.M) {
	testDir, err := os.MkdirTemp("", "xiaotian-service-tests-*")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("DB_PATH", filepath.Join(testDir, "gateway.db")); err != nil {
		panic(err)
	}
	_ = os.Setenv("SECRET_KEY", "test-secret-key-for-unit-tests")
	if err := store.InitDB(); err != nil {
		panic(err)
	}
	code := m.Run()
	store.CloseDB()
	if err := os.RemoveAll(testDir); err != nil {
		fmt.Fprintf(os.Stderr, "remove service test directory: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func TestMatchingServicePlaceOrder(t *testing.T) {
	ms := GetMatchingService()

	result, err := ms.PlaceOrder("BTCUSDT", "buy", "limit", 50000.0, 0.1, 1, "")
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	if result["order_id"] == nil {
		t.Fatal("expected order_id in result")
	}
	if result["store_order_id"] == nil {
		t.Fatal("expected store_order_id in result")
	}
}

// SimulateTrading 每轮先撤旧模拟单再挂新单：盘口不得随轮数无限堆积（重复展示回归）。
func TestSimulateTradingDoesNotAccumulateLimitOrders(t *testing.T) {
	ms := GetMatchingService()
	symbol := "SIMDUP_TEST_SYMBOL"
	t.Cleanup(func() { ms.GetEngine(symbol).Destroy() })

	prices := []float64{50000, 50100, 49900, 50200}
	for _, p := range prices {
		ms.SimulateTrading(symbol, p)
		time.Sleep(200 * time.Millisecond) // 等异步挂单完成
	}

	snap, err := ms.GetOrderBook(symbol, 50)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	bids := snap["bids"].([][]float64)
	asks := snap["asks"].([][]float64)
	if len(bids) > 2 || len(asks) > 2 {
		t.Fatalf("模拟单重复堆积: bids=%d asks=%d（每轮应先撤旧单）", len(bids), len(asks))
	}
	if len(bids) == 0 || len(asks) == 0 {
		t.Fatalf("最新一轮模拟单应仍在盘口: bids=%d asks=%d", len(bids), len(asks))
	}
}

func TestMatchingServiceGetOrderBook(t *testing.T) {
	ms := GetMatchingService()
	symbol := "ETHUSDT"

	// Seed the book
	ms.PlaceOrder(symbol, "sell", "limit", 3500.0, 1.0, 1, "")
	ms.PlaceOrder(symbol, "buy", "limit", 3490.0, 1.0, 2, "")

	snap, err := ms.GetOrderBook(symbol, 5)
	if err != nil {
		t.Fatalf("GetOrderBook failed: %v", err)
	}

	if snap["symbol"] != symbol {
		t.Fatalf("expected symbol %s, got %v", symbol, snap["symbol"])
	}
}

func TestMatchingServiceCancelOrder(t *testing.T) {
	ms := GetMatchingService()
	symbol := "SOLUSDT"

	result, err := ms.PlaceOrder(symbol, "buy", "limit", 150.0, 1.0, 1, "")
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	storeOrderID, ok := result["store_order_id"].(string)
	if !ok || storeOrderID == "" {
		t.Fatal("expected store_order_id in result")
	}

	if err := ms.CancelOrder(symbol, storeOrderID); err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}
}

func TestRemoveOrderFromBookKeepsOMSStateSeparate(t *testing.T) {
	ms := GetMatchingService()
	symbol := "PAPER_BOOK_CLEANUP"
	t.Cleanup(func() { ms.GetEngine(symbol).Destroy() })

	if _, err := ms.PlaceOrder(symbol, "buy", "limit", 100, 1, 1, "cleanup-order"); err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if err := ms.RemoveOrderFromBook(symbol, "cleanup-order"); err != nil {
		t.Fatalf("RemoveOrderFromBook: %v", err)
	}

	snapshot, err := ms.GetOrderBook(symbol, 5)
	if err != nil {
		t.Fatalf("GetOrderBook: %v", err)
	}
	if bids := snapshot["bids"].([][]float64); len(bids) != 0 {
		t.Fatalf("book entry remains after external paper fill: %v", bids)
	}
}
