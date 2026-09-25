package service

import (
	"os"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("DB_PATH", "./data/test_service.db")
	_ = os.Setenv("SECRET_KEY", "test-secret-key-for-unit-tests")
	if err := store.InitDB(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.Remove("./data/test_service.db")
	_ = os.Remove("./data/test_service.db-shm")
	_ = os.Remove("./data/test_service.db-wal")
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
