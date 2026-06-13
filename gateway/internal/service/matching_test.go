package service

import (
	"testing"
)

func TestMatchingServicePlaceOrder(t *testing.T) {
	ms := GetMatchingService()

	result, err := ms.PlaceOrder("BTCUSDT", "buy", "limit", 50000.0, 0.1, 1)
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

func TestMatchingServiceGetOrderBook(t *testing.T) {
	ms := GetMatchingService()
	symbol := "ETHUSDT"

	// Seed the book
	ms.PlaceOrder(symbol, "sell", "limit", 3500.0, 1.0, 1)
	ms.PlaceOrder(symbol, "buy", "limit", 3490.0, 1.0, 2)

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

	result, err := ms.PlaceOrder(symbol, "buy", "limit", 150.0, 1.0, 1)
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
