//go:build !cgo

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

	bids      []orderLevel
	asks      []orderLevel
	orders    map[uint64]*order
	nextID    uint64
	trades    []map[string]any
	tradeSeq  uint64
}

type orderLevel struct {
	Price    float64
	Orders   []*order
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

	result := map[string]any{
		"order_id":  id,
		"symbol":    e.symbol,
		"side":      side,
		"price":     price,
		"quantity":  quantity,
		"status":    "NEW",
		"filled":    0.0,
	}

	if orderType == "market" {
		e.matchMarket(ord)
		result["status"] = "FILLED"
		result["filled"] = ord.Filled
	} else {
		e.addToBook(ord)
		e.matchLimit(ord)
		result["filled"] = ord.Filled
		if ord.Filled > 0 && ord.Filled < ord.Quantity {
			result["status"] = "PARTIALLY_FILLED"
		} else if ord.Filled >= ord.Quantity {
			result["status"] = "FILLED"
		}
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

func (e *MatchingEngine) recordTrade(buyer, seller *order, price, qty float64) {
	e.tradeSeq++
	trade := map[string]any{
		"id":          e.tradeSeq,
		"buy_order":   buyer.ID,
		"sell_order":  seller.ID,
		"price":       price,
		"quantity":    qty,
		"symbol":      e.symbol,
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

	return map[string]any{
		"symbol": e.symbol,
		"bids":   bids,
		"asks":   asks,
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
