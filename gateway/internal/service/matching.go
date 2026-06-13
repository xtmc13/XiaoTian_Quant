package service

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// MatchingService provides order matching across multiple symbols.
// Uses the Rust matching engine via CGo when built with cgo tags,
// otherwise falls back to an in-memory Go implementation.
type MatchingService struct {
	engines map[string]*adapter.MatchingEngine
	mu      sync.RWMutex
}

var (
	matchSvc     *MatchingService
	matchSvcOnce sync.Once
)

// GetMatchingService returns the singleton matching service.
func GetMatchingService() *MatchingService {
	matchSvcOnce.Do(func() {
		matchSvc = &MatchingService{
			engines: make(map[string]*adapter.MatchingEngine),
		}
	})
	return matchSvc
}

// GetEngine returns or creates an engine for a symbol.
func (ms *MatchingService) GetEngine(symbol string) *adapter.MatchingEngine {
	ms.mu.RLock()
	eng, ok := ms.engines[symbol]
	ms.mu.RUnlock()
	if ok {
		return eng
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	// Double-check
	if eng, ok = ms.engines[symbol]; ok {
		return eng
	}
	eng = adapter.NewMatchingEngine(symbol)
	ms.engines[symbol] = eng
	return eng
}

// PlaceOrder places an order and matches it against the book.
func (ms *MatchingService) PlaceOrder(symbol, side, orderType string, price, quantity float64, userID uint64) (map[string]any, error) {
	eng := ms.GetEngine(symbol)
	result, err := eng.SubmitOrder(side, orderType, price, quantity, userID)
	if err != nil {
		return nil, err
	}

	// Also record in the Go store for API access
	engineOrderID, _ := result["order_id"].(uint64)
	storeOrder := map[string]any{
		"symbol":         symbol,
		"side":           side,
		"order_type":     orderType,
		"price":          price,
		"quantity":       quantity,
		"exchange":       "MATCHING",
		"engine_order_id": engineOrderID,
	}
	orderID := store.PlaceOrder(storeOrder)

	result["store_order_id"] = orderID
	return result, nil
}

// CancelOrder cancels an order by store order ID.
func (ms *MatchingService) CancelOrder(symbol string, storeOrderID string) error {
	order := store.GetOrderByID(storeOrderID)
	if order == nil {
		return fmt.Errorf("order %s not found", storeOrderID)
	}

	// Cancel in matching engine first (if engine_order_id is available)
	if engineID, ok := order["engine_order_id"].(uint64); ok && engineID > 0 {
		eng := ms.GetEngine(symbol)
		if err := eng.CancelOrder(engineID); err != nil {
			return err
		}
	}

	return store.CancelOrder(storeOrderID)
}

// GetOrderBook returns the order book snapshot for a symbol.
func (ms *MatchingService) GetOrderBook(symbol string, depth int) (map[string]any, error) {
	eng := ms.GetEngine(symbol)
	return eng.Snapshot(depth)
}

// SimulateTrading runs a simple market simulation for real-time data feed.
func (ms *MatchingService) SimulateTrading(symbol string, price float64) {
	eng := ms.GetEngine(symbol)

	go func() {
		// Place simulated buy/sell orders around the current price
		levels := []struct {
			side     string
			offsetPct float64
			qty      float64
		}{
			{"buy", -0.001, 0.1},
			{"buy", -0.002, 0.2},
			{"sell", 0.001, 0.1},
			{"sell", 0.002, 0.2},
		}

		for _, level := range levels {
			px := price * (1 + level.offsetPct)
			_, err := eng.SubmitOrder(level.side, "limit", px, level.qty, 0)
			if err != nil {
				log.Printf("[Matching] Simulated order error for %s: %v", symbol, err)
			}
		}
	}()
}

// StartDataFeed begins periodic simulated trading for configured symbols.
func (ms *MatchingService) StartDataFeed() {
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	basePrices := map[string]float64{
		"BTCUSDT": 68000.0,
		"ETHUSDT": 3500.0,
		"SOLUSDT": 150.0,
	}

	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for range ticker.C {
			for _, sym := range symbols {
				base := basePrices[sym]
				ms.SimulateTrading(sym, base)
			}
		}
	}()
}
