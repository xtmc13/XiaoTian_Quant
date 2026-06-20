//go:build !cgo
// +build !cgo

// (dev mock) Pure-Go fallback matching engine — NOT for production.
// Activated only when CGO_ENABLED=0 (no Rust engine).
// JSON output format is kept strictly identical to the Rust FFI version
// (gateway/internal/adapter/../engine/src/ffi.rs) so that upper-layer
// handlers can treat both engines interchangeably.

package adapter

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
)

// MatchingEngine is a pure-Go order matching engine (price-time priority).
// Used when the Rust engine is not available (non-cgo builds).
type MatchingEngine struct {
	symbol string
	mu     sync.Mutex

	bids     []orderLevel
	asks     []orderLevel
	orders   map[uint64]*order
	nextID   uint64
	trades   []map[string]any
	tradeSeq uint64
}

type orderLevel struct {
	Price  float64
	Orders []*order
}

type order struct {
	ID        uint64
	Side      string
	Price     float64
	Quantity  float64
	Filled    float64
	UserID    uint64
	Timestamp int64
}

var (
	engines   = make(map[string]*MatchingEngine)
	enginesMu sync.Mutex
)

// NewMatchingEngine creates a new pure-Go matching engine for a symbol.
func NewMatchingEngine(symbol string) *MatchingEngine {
	enginesMu.Lock()
	defer enginesMu.Unlock()
	if eng, ok := engines[symbol]; ok {
		return eng
	}
	eng := &MatchingEngine{
		symbol: symbol,
		orders: make(map[uint64]*order),
	}
	engines[symbol] = eng
	return eng
}

// SubmitOrder submits an order and returns the result in Rust FFI format:
// {"status":"ok","order_id":<id>,"trades":[...]}
func (e *MatchingEngine) SubmitOrder(side, orderType string, price, quantity float64, userID uint64) (map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}

	id := atomic.AddUint64(&e.nextID, 1)
	ord := &order{
		ID:       id,
		Side:     side,
		Price:    price,
		Quantity: quantity,
		UserID:   userID,
	}
	e.orders[id] = ord

	// Record trade sequence before matching so we can capture
	// trades produced by this order only.
	preTradeSeq := e.tradeSeq

	if orderType == "market" {
		e.matchMarket(ord)
	} else {
		e.addToBook(ord)
		e.matchLimit(ord)
	}

	// Collect trades produced by this order submission.
	var orderTrades []map[string]any
	if e.tradeSeq > preTradeSeq {
		newCount := int(e.tradeSeq - preTradeSeq)
		start := len(e.trades) - newCount
		if start < 0 {
			start = 0
		}
		orderTrades = make([]map[string]any, newCount)
		copy(orderTrades, e.trades[start:])
	}

	result := map[string]any{
		"status":   "ok",
		"order_id": id,
		"trades":   orderTrades,
	}

	return result, nil
}

func (e *MatchingEngine) addToBook(ord *order) {
	var levels *[]orderLevel
	if ord.Side == "buy" {
		levels = &e.bids
	} else {
		levels = &e.asks
	}
	for i, lvl := range *levels {
		if lvl.Price == ord.Price {
			(*levels)[i].Orders = append(lvl.Orders, ord)
			return
		}
	}
	lvl := orderLevel{Price: ord.Price, Orders: []*order{ord}}
	*levels = append(*levels, lvl)
	if ord.Side == "buy" {
		sort.Slice(e.bids, func(i, j int) bool { return e.bids[i].Price > e.bids[j].Price })
	} else {
		sort.Slice(e.asks, func(i, j int) bool { return e.asks[i].Price < e.asks[j].Price })
	}
}

func (e *MatchingEngine) matchLimit(taker *order) {
	e.matchAll()
}

func (e *MatchingEngine) matchMarket(maker *order) {
	if maker.Side == "buy" {
		for len(e.asks) > 0 && maker.Filled < maker.Quantity {
			if len(e.asks[0].Orders) == 0 {
				e.asks = e.asks[1:]
				continue
			}
			counter := e.asks[0].Orders[0]
			fillQty := math.Min(maker.Quantity-maker.Filled, counter.Quantity-counter.Filled)
			tradePrice := e.asks[0].Price
			maker.Filled += fillQty
			counter.Filled += fillQty
			e.recordTrade(maker, counter, tradePrice, fillQty)
			if counter.Filled >= counter.Quantity {
				e.asks[0].Orders = e.asks[0].Orders[1:]
			}
			if len(e.asks[0].Orders) == 0 {
				e.asks = e.asks[1:]
			}
		}
	} else {
		for len(e.bids) > 0 && maker.Filled < maker.Quantity {
			if len(e.bids[0].Orders) == 0 {
				e.bids = e.bids[1:]
				continue
			}
			counter := e.bids[0].Orders[0]
			fillQty := math.Min(maker.Quantity-maker.Filled, counter.Quantity-counter.Filled)
			tradePrice := e.bids[0].Price
			maker.Filled += fillQty
			counter.Filled += fillQty
			e.recordTrade(maker, counter, tradePrice, fillQty)
			if counter.Filled >= counter.Quantity {
				e.bids[0].Orders = e.bids[0].Orders[1:]
			}
			if len(e.bids[0].Orders) == 0 {
				e.bids = e.bids[1:]
			}
		}
	}
}

func (e *MatchingEngine) matchAll() {
	for {
		if len(e.bids) == 0 || len(e.asks) == 0 {
			break
		}
		if len(e.bids[0].Orders) == 0 {
			e.bids = e.bids[1:]
			continue
		}
		if len(e.asks[0].Orders) == 0 {
			e.asks = e.asks[1:]
			continue
		}
		if e.bids[0].Price < e.asks[0].Price {
			break
		}
		buyer := e.bids[0].Orders[0]
		seller := e.asks[0].Orders[0]
		fillQty := math.Min(buyer.Quantity-buyer.Filled, seller.Quantity-seller.Filled)
		tradePrice := e.asks[0].Price // price-time priority: earlier order sets price
		buyer.Filled += fillQty
		seller.Filled += fillQty
		e.recordTrade(buyer, seller, tradePrice, fillQty)
		if buyer.Filled >= buyer.Quantity {
			e.bids[0].Orders = e.bids[0].Orders[1:]
		}
		if seller.Filled >= seller.Quantity {
			e.asks[0].Orders = e.asks[0].Orders[1:]
		}
		if len(e.bids[0].Orders) == 0 {
			e.bids = e.bids[1:]
		}
		if len(e.asks[0].Orders) == 0 {
			e.asks = e.asks[1:]
		}
	}
}

// recordTrade records a trade using Rust FFI compatible field names.
func (e *MatchingEngine) recordTrade(buyer, seller *order, price, qty float64) {
	e.tradeSeq++
	trade := map[string]any{
		"id":            e.tradeSeq,
		"buy_order_id":  buyer.ID,
		"sell_order_id": seller.ID,
		"price":         price,
		"quantity":      qty,
	}
	e.trades = append(e.trades, trade)
	if len(e.trades) > 1000 {
		e.trades = e.trades[len(e.trades)-1000:]
	}
}

func (e *MatchingEngine) CancelOrder(orderID uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	ord, ok := e.orders[orderID]
	if !ok {
		return fmt.Errorf("order %d not found", orderID)
	}
	if ord.Filled >= ord.Quantity {
		return fmt.Errorf("order %d already filled", orderID)
	}

	// Remove from book
	removeFromLevels := func(levels []orderLevel) []orderLevel {
		for i, lvl := range levels {
			for j, o := range lvl.Orders {
				if o.ID == orderID {
					lvl.Orders = append(lvl.Orders[:j], lvl.Orders[j+1:]...)
					if len(lvl.Orders) == 0 {
						levels = append(levels[:i], levels[i+1:]...)
					}
					return levels
				}
			}
		}
		return levels
	}

	e.bids = removeFromLevels(e.bids)
	e.asks = removeFromLevels(e.asks)
	delete(e.orders, orderID)
	return nil
}

// Snapshot returns the order-book snapshot in Rust FFI compatible format:
// {"symbol":"...","best_bid":...,"best_ask":...,"spread":...,"bids":[[p,q],...],"asks":[[p,q],...]}
func (e *MatchingEngine) Snapshot(depth int) (map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	bids := make([][]float64, 0, depth)
	for _, lvl := range e.bids {
		total := 0.0
		for _, o := range lvl.Orders {
			total += o.Quantity - o.Filled
		}
		bids = append(bids, []float64{lvl.Price, total})
		if len(bids) >= depth {
			break
		}
	}

	asks := make([][]float64, 0, depth)
	for _, lvl := range e.asks {
		total := 0.0
		for _, o := range lvl.Orders {
			total += o.Quantity - o.Filled
		}
		asks = append(asks, []float64{lvl.Price, total})
		if len(asks) >= depth {
			break
		}
	}

	// Compute best_bid, best_ask and spread exactly like the Rust engine.
	var bestBid, bestAsk float64
	if len(e.bids) > 0 {
		bestBid = e.bids[0].Price
	}
	if len(e.asks) > 0 {
		bestAsk = e.asks[0].Price
	}
	spread := bestAsk - bestBid
	if bestAsk == 0 || bestBid == 0 {
		spread = 0
	}

	return map[string]any{
		"symbol":   e.symbol,
		"best_bid": bestBid,
		"best_ask": bestAsk,
		"spread":   spread,
		"bids":     bids,
		"asks":     asks,
	}, nil
}

func (e *MatchingEngine) TradeCount() uint64 {
	return e.tradeSeq
}

func (e *MatchingEngine) GetTrades(limit int) ([]map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if limit <= 0 || limit > len(e.trades) {
		limit = len(e.trades)
	}
	result := make([]map[string]any, limit)
	copy(result, e.trades[len(e.trades)-limit:])
	return result, nil
}

func (e *MatchingEngine) Destroy() {
	enginesMu.Lock()
	defer enginesMu.Unlock()
	delete(engines, e.symbol)
}
