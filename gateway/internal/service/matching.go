package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
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

	// simOrderIDs 记录每个 symbol 最近一轮模拟做市单的引擎订单 id。
	// SimulateTrading 每轮先撤旧单再挂新单——修复旧实现每 3s 叠加 4 张
	// 限价单导致盘口（及 WS 订阅）无限堆积重复展示的问题。
	simOrderIDs   map[string][]uint64
	simOrderIDsMu sync.Mutex

	balanceProvider adapter.BalanceProvider
}

var (
	matchSvc     *MatchingService
	matchSvcOnce sync.Once
)

// dataFeedHTTPClient has a short timeout so the background simulated-trading
// feed skips a cycle quickly when the network is unreachable.
var dataFeedHTTPClient = &http.Client{Timeout: 3 * time.Second}

// GetMatchingService returns the singleton matching service.
func GetMatchingService() *MatchingService {
	matchSvcOnce.Do(func() {
		matchSvc = &MatchingService{
			engines:       make(map[string]*adapter.MatchingEngine),
			simOrderIDs:   make(map[string][]uint64),
		}
	})
	return matchSvc
}

// SetBalanceProvider 给引擎注入资金校验（生产由 app 上下文接上层账本）。
// 已创建的引擎同步注入；引擎在构建时也会继承（见 GetEngine）。
func (ms *MatchingService) SetBalanceProvider(p adapter.BalanceProvider) {
	ms.mu.Lock()
	ms.balanceProvider = p
	engs := make([]*adapter.MatchingEngine, 0, len(ms.engines))
	for _, e := range ms.engines {
		engs = append(engs, e)
	}
	ms.mu.Unlock()
	for _, e := range engs {
		e.SetBalanceProvider(p)
	}
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
	if ms.balanceProvider != nil {
		eng.SetBalanceProvider(ms.balanceProvider)
	}
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

// SimulateTrading places a fresh set of simulated market-making orders around
// the current price. Previous simulated orders for the symbol are cancelled
// first so the book (and any WS orderbook consumers) shows exactly one set of
// simulated levels instead of accumulating stale duplicates every tick.
func (ms *MatchingService) SimulateTrading(symbol string, price float64) {
	eng := ms.GetEngine(symbol)

	// Cancel the previous round (ignore errors: filled or gone orders are fine).
	ms.simOrderIDsMu.Lock()
	prev := ms.simOrderIDs[symbol]
	ms.simOrderIDsMu.Unlock()
	for _, id := range prev {
		_ = eng.CancelOrder(id)
	}

	go func() {
		// Place simulated buy/sell orders around the current price
		levels := []struct {
			side      string
			offsetPct float64
			qty       float64
		}{
			{"buy", -0.001, 0.1},
			{"buy", -0.002, 0.2},
			{"sell", 0.001, 0.1},
			{"sell", 0.002, 0.2},
		}

		var newIDs []uint64
		for _, level := range levels {
			px := price * (1 + level.offsetPct)
			res, err := eng.SubmitOrder(level.side, "limit", px, level.qty, 0)
			if err != nil {
				log.Printf("[Matching] Simulated order error for %s: %v", symbol, err)
				continue
			}
			if id, ok := res["order_id"].(uint64); ok {
				newIDs = append(newIDs, id)
			}
		}
		ms.simOrderIDsMu.Lock()
		ms.simOrderIDs[symbol] = newIDs
		ms.simOrderIDsMu.Unlock()
	}()
}

// StartDataFeed begins periodic simulated trading for configured symbols.
// Prices are fetched from Binance public API; hard-coded fake prices are no
// longer used. Symbols without a real price are skipped.
func (ms *MatchingService) StartDataFeed() {
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	lastPrices := make(map[string]float64)
	var priceMu sync.Mutex

	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for range ticker.C {
			for _, sym := range symbols {
				price := fetchLastPrice(sym)
				if price <= 0 {
					// Fallback to last known price if available.
					priceMu.Lock()
					price = lastPrices[sym]
					priceMu.Unlock()
					if price <= 0 {
						log.Printf("[Matching] No price available for %s, skipping simulation", sym)
						continue
					}
				}
				priceMu.Lock()
				lastPrices[sym] = price
				priceMu.Unlock()
				ms.SimulateTrading(sym, price)
			}
		}
	}()
}

// fetchLastPrice retrieves the latest price from Binance public API.
// Short timeout so a dead network skips a cycle instead of wedging the feed.
func fetchLastPrice(symbol string) float64 {
	resp, err := dataFeedHTTPClient.Get("https://api.binance.com/api/v3/ticker/price?symbol=" + symbol)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return 0
	}
	if priceStr, ok := result["price"].(string); ok {
		f, _ := strconv.ParseFloat(priceStr, 64)
		return f
	}
	return 0
}
