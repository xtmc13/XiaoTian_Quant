//go:build !cgo
// +build !cgo

// Pure-Go matching engine (price-time priority) — the non-cgo production path.
// Activated when CGO_ENABLED=0 (no Rust engine); behavior is aligned with the
// Rust FFI version (gateway/internal/adapter/../engine/src/ffi.rs): identical
// JSON result/snapshot/trade format, plus exchange-like balance enforcement —
// when a BalanceProvider is wired, orders are validated against available
// funds (insufficient → reject or partial fill, never a negative balance).

package adapter

import (
	"fmt"
	"math"
	"sort"
	"strings"
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

	// balance 可选资金校验（nil = 不校验，兼容旧行为/模拟做市）。
	balance BalanceProvider

	// onFill 可选成交回写钩子（C2.3）：每笔成交后对买卖双方各触发一次，
	// 推送该订单的累计成交量与 VWAP 均价（绝对量语义，重复/乱序事件收敛，
	// 天然幂等）。paper LIMIT 挂单（maker）被后续对手单成交时，OMS 靠它回写
	// 状态。注意：回调在引擎锁内触发，实现不得再调用本引擎方法（防死锁），
	// 重活应异步化。
	onFill func(orderID uint64, filledQty, avgPrice float64)
}

// SetOnFill 注入成交回写钩子（生产由撮合服务接到 OMS）。
func (e *MatchingEngine) SetOnFill(fn func(orderID uint64, filledQty, avgPrice float64)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onFill = fn
}

// SetBalanceProvider 注入可用资金校验（生产由撮合服务接上层账本）。
func (e *MatchingEngine) SetBalanceProvider(p BalanceProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.balance = p
}

// available 取用户可用资产；无 provider 或模拟做市（userID=0）返回 +Inf。
func (e *MatchingEngine) available(userID uint64, asset string) float64 {
	if e.balance == nil || userID == 0 {
		return math.Inf(1)
	}
	return e.balance.Available(userID, asset)
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
	notional  float64 // 累计成交额（Fill 回写算 VWAP 用）
	UserID    uint64
	Timestamp int64
}

// BalanceProvider 供给撮合引擎做可用资金校验（上层注入真实账本）。
// asset 为币种（如 "USDT"/"BTC"），返回该用户当前可用数量。
// 未注入时引擎保持原行为（不校验，模拟盘做市商 userID=0 永远豁免）。
type BalanceProvider interface {
	Available(userID uint64, asset string) float64
}

// quoteAssets 从交易对推导 base/quote 的后缀表（长后缀优先，避免 BTC 吃掉 BTCUSDT）。
var quoteAssets = []string{"USDT", "USDC", "FDUSD", "TUSD", "BUSD", "DAI", "EUR", "GBP", "ETH", "BTC", "BNB"}

func splitSymbolAssets(symbol string) (base, quote string) {
	s := strings.ToUpper(symbol)
	for _, q := range quoteAssets {
		if strings.HasSuffix(s, q) && len(s) > len(q) {
			return s[:len(s)-len(q)], q
		}
	}
	return s, "USDT"
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
//
// 资金校验（注入 BalanceProvider 后生效，行为对齐真实交易所）：
//   - 限价卖出 / 市价卖出：可用 base 持仓不足 → 拒绝（-2010 式）
//   - 限价买入：可用 quote 不足支付 price*qty → 拒绝
//   - 市价买入（price=0 无法预判成本）：撮合时按可用 quote 截断 → 部分成交
func (e *MatchingEngine) SubmitOrder(side, orderType string, price, quantity float64, userID uint64) (map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}

	base, quote := splitSymbolAssets(e.symbol)

	// ── 前置资金校验 ──
	if orderType == "market" {
		if side == "sell" {
			// 市价卖出：持仓为零直接拒绝；不足部分撮合时截断（部分成交）。
			if avail := e.available(userID, base); avail <= 0 {
				return nil, fmt.Errorf("insufficient balance: no %s available to sell", base)
			}
		}
	} else {
		if side == "sell" {
			if avail := e.available(userID, base); quantity > avail {
				return nil, fmt.Errorf("insufficient balance: need %.8f %s, available %.8f", quantity, base, math.Max(avail, 0))
			}
		} else {
			cost := price * quantity
			if avail := e.available(userID, quote); cost > avail {
				return nil, fmt.Errorf("insufficient balance: need %.8f %s, available %.8f", cost, quote, math.Max(avail, 0))
			}
		}
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

	// Result mirrors the Rust FFI format: status/side/price/quantity/filled
	// alongside order_id/trades.
	status := "NEW"
	if orderType == "market" {
		// Market orders always report FILLED (possibly with 0 quantity when
		// the book is empty), matching the Rust engine contract.
		status = "FILLED"
	} else if ord.Filled >= ord.Quantity {
		status = "FILLED"
	} else if ord.Filled > 0 {
		status = "PARTIALLY_FILLED"
	}

	result := map[string]any{
		"status":   status,
		"order_id": id,
		"side":     side,
		"price":    price,
		"quantity": quantity,
		"filled":   ord.Filled,
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
	base, quote := splitSymbolAssets(e.symbol)
	var spentQuote float64 // 市价买入：按可用 quote 截断，杜绝负余额
	var soldBase float64   // 市价卖出：按可用 base 截断（部分成交）

	if maker.Side == "buy" {
		for len(e.asks) > 0 && maker.Filled < maker.Quantity {
			if len(e.asks[0].Orders) == 0 {
				e.asks = e.asks[1:]
				continue
			}
			counter := e.asks[0].Orders[0]
			fillQty := math.Min(maker.Quantity-maker.Filled, counter.Quantity-counter.Filled)
			tradePrice := e.asks[0].Price
			// 资金截断：累计成交额不得超过可用 quote。
			if avail := e.available(maker.UserID, quote); !math.IsInf(avail, 1) {
				remainingQuote := avail - spentQuote
				if remainingQuote <= 0 {
					break
				}
				fillQty = math.Min(fillQty, remainingQuote/tradePrice)
				if fillQty <= 0 {
					break
				}
			}
			maker.Filled += fillQty
			counter.Filled += fillQty
			spentQuote += tradePrice * fillQty
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
			// 持仓截断：累计卖出不得超过可用 base（纯下跌行情防负余额）。
			if avail := e.available(maker.UserID, base); !math.IsInf(avail, 1) {
				remainingBase := avail - soldBase
				if remainingBase <= 0 {
					break
				}
				fillQty = math.Min(fillQty, remainingBase)
				if fillQty <= 0 {
					break
				}
			}
			maker.Filled += fillQty
			counter.Filled += fillQty
			soldBase += fillQty
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
	// Fill 回写钩子：向双方推送累计成交量/VWAP（绝对量语义，OMS 端幂等收敛）。
	for _, ord := range []*order{buyer, seller} {
		ord.notional += price * qty
		if e.onFill != nil && ord.Filled > 0 {
			e.onFill(ord.ID, ord.Filled, ord.notional/ord.Filled)
		}
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
