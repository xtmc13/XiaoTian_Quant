package paper

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// PaperConfig configures the paper trading exchange.
type PaperConfig struct {
	InitialBalance float64       `json:"initial_balance"`
	FeeRate        float64       `json:"fee_rate"`
	Slippage       float64       `json:"slippage"`
	MinLatency     time.Duration `json:"min_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
}

func DefaultPaperConfig() PaperConfig {
	return PaperConfig{
		InitialBalance: 100000.0,
		FeeRate:        0.001,
		Slippage:       0.0005,
		MinLatency:     50 * time.Millisecond,
		MaxLatency:     300 * time.Millisecond,
	}
}

// PaperOrder mirrors a live order in simulation.
type PaperOrder struct {
	model.OrderData
	FilledPrice float64 `json:"filled_price"`
}

// PaperPosition tracks a position in the paper exchange.
type PaperPosition struct {
	model.PositionData
	trades []model.TradeData
}

func (p *PaperPosition) AddTrade(trade model.TradeData) {
	p.trades = append(p.trades, trade)
	totalQty := 0.0
	totalCost := 0.0
	for _, t := range p.trades {
		if t.Side == "BUY" {
			totalQty += t.Quantity
			totalCost += t.Price * t.Quantity
		} else {
			totalQty -= t.Quantity
			totalCost -= t.Price * t.Quantity
		}
	}
	p.Quantity = totalQty
	if totalQty > 0 {
		p.AvgEntryPrice = totalCost / totalQty
		p.CostBasis = totalCost
	} else {
		p.AvgEntryPrice = 0
		p.CostBasis = 0
	}
}

// ── Go-native Order Book (matches same logic as Rust engine) ──

type orderBookLevel struct {
	price  float64
	orders []*PaperOrder
}

type goOrderBook struct {
	symbol     string
	bids       []*orderBookLevel // descending by price
	asks       []*orderBookLevel // ascending by price
	tradeCount uint64
	orderCount uint64
	mu         sync.Mutex
}

func newGoOrderBook(symbol string) *goOrderBook {
	return &goOrderBook{symbol: symbol}
}

func (ob *goOrderBook) bestBid() float64 {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	if len(ob.bids) == 0 {
		return 0
	}
	return ob.bids[0].price
}

func (ob *goOrderBook) bestAsk() float64 {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	if len(ob.asks) == 0 {
		return 0
	}
	return ob.asks[0].price
}

func (ob *goOrderBook) addOrder(order *PaperOrder) uint64 {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.orderCount++
	order.ID = fmt.Sprintf("paper-%d", ob.orderCount)

	var levels *[]*orderBookLevel
	if order.Side == model.SideBuy {
		levels = &ob.bids
	} else {
		levels = &ob.asks
	}

	// Find or create price level
	var level *orderBookLevel
	for _, l := range *levels {
		if l.price == order.Price {
			level = l
			break
		}
	}
	if level == nil {
		level = &orderBookLevel{price: order.Price}
		*levels = append(*levels, level)
		// Re-sort
		if order.Side == model.SideBuy {
			sort.Slice(*levels, func(i, j int) bool { return (*levels)[i].price > (*levels)[j].price })
		} else {
			sort.Slice(*levels, func(i, j int) bool { return (*levels)[i].price < (*levels)[j].price })
		}
	}
	level.orders = append(level.orders, order)
	return ob.orderCount
}

func (ob *goOrderBook) cancelOrder(orderID string) *PaperOrder {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	for _, levels := range [][]*orderBookLevel{ob.bids, ob.asks} {
		for _, level := range levels {
			for i, o := range level.orders {
				if o.ID == orderID {
					level.orders = append(level.orders[:i], level.orders[i+1:]...)
					o.Status = model.StatusCancelled
					return o
				}
			}
		}
	}
	return nil
}

// ── Paper Exchange ──

// PaperExchange is a simulated exchange using an in-memory order book.
type PaperExchange struct {
	config    PaperConfig
	books     map[string]*goOrderBook
	positions map[string]map[string]*PaperPosition // symbol -> positionID -> position
	balances  map[string]*model.Balance
	orders    map[string]*PaperOrder
	equity    []model.PortfolioSnapshot
	rng       *rand.Rand
	mu        sync.RWMutex

	// Event callbacks
	onOrderUpdate func(order model.OrderData)
	onTrade       func(trade model.TradeData)
	onPosition    func(pos model.PositionData)
}

var (
	paperInst     *PaperExchange
	paperInstOnce sync.Once
)

// GetPaperExchange returns the global paper trading exchange.
func GetPaperExchange() *PaperExchange {
	paperInstOnce.Do(func() {
		paperInst = NewPaperExchange(DefaultPaperConfig())
	})
	return paperInst
}

func NewPaperExchange(cfg PaperConfig) *PaperExchange {
	pe := &PaperExchange{
		config:    cfg,
		books:     make(map[string]*goOrderBook),
		positions: make(map[string]map[string]*PaperPosition),
		balances:  make(map[string]*model.Balance),
		orders:    make(map[string]*PaperOrder),
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	pe.balances["USDT"] = &model.Balance{
		Currency: "USDT",
		Total:    cfg.InitialBalance,
		Free:     cfg.InitialBalance,
		Used:     0,
	}
	return pe
}

func (pe *PaperExchange) Name() string { return "paper" }
func (pe *PaperExchange) Start() error {
	log.Printf("[Paper] Paper trading exchange started with initial balance: $%.2f", pe.config.InitialBalance)
	return nil
}
func (pe *PaperExchange) Stop() error { return nil }
func (pe *PaperExchange) IsConnected() bool { return true }

// Callback setters
func (pe *PaperExchange) OnOrderUpdate(fn func(order model.OrderData)) { pe.onOrderUpdate = fn }
func (pe *PaperExchange) OnTrade(fn func(trade model.TradeData))      { pe.onTrade = fn }
func (pe *PaperExchange) OnPosition(fn func(pos model.PositionData))   { pe.onPosition = fn }

// getOrCreateBook returns the order book for a symbol.
func (pe *PaperExchange) getOrCreateBook(symbol string) *goOrderBook {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	if book, ok := pe.books[symbol]; ok {
		return book
	}
	book := newGoOrderBook(symbol)
	pe.books[symbol] = book
	if pe.positions[symbol] == nil {
		pe.positions[symbol] = make(map[string]*PaperPosition)
	}
	return book
}

// ── Exchange Interface Implementation ──

func (pe *PaperExchange) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	// Simulate latency
	pe.simulateLatency()

	book := pe.getOrCreateBook(symbol)

	order := &PaperOrder{
		OrderData: model.OrderData{
			Symbol:    symbol,
			Side:      model.OrderSide(side),
			OrderType: model.OrderType(orderType),
			Price:     price,
			Quantity:  quantity,
			Status:    model.StatusNew,
			Exchange:  "paper",
			CreatedAt: time.Now().UnixMilli(),
			UpdatedAt: time.Now().UnixMilli(),
		},
	}

	if orderType == "MARKET" {
		return pe.executeMarketOrder(book, order)
	}

	// Limit order: try to match first
	trades := pe.matchOrder(book, order)
	if len(trades) > 0 {
		pe.applyTrades(trades)
	}

	if !order.IsDone() {
		book.addOrder(order)
		order.Status = model.StatusNew
	} else {
		order.Status = model.StatusFilled
	}

	pe.mu.Lock()
	pe.orders[order.ID] = order
	pe.mu.Unlock()

	if pe.onOrderUpdate != nil {
		pe.onOrderUpdate(order.OrderData)
	}

	return map[string]any{
		"order_id": order.ID,
		"status":   string(order.Status),
		"filled":   order.Filled,
		"trades":   tradesToMaps(trades),
	}, nil
}

func (pe *PaperExchange) CancelOrder(symbol, orderID string) (map[string]any, error) {
	pe.simulateLatency()

	book := pe.getOrCreateBook(symbol)
	order := book.cancelOrder(orderID)
	if order == nil {
		return nil, fmt.Errorf("order %s not found", orderID)
	}

	// Unlock balance
	pe.unlockFunds(order)

	pe.mu.Lock()
	pe.orders[orderID] = order
	pe.mu.Unlock()

	if pe.onOrderUpdate != nil {
		pe.onOrderUpdate(order.OrderData)
	}

	return map[string]any{
		"order_id": orderID,
		"status":   "CANCELLED",
	}, nil
}

func (pe *PaperExchange) GetBalance() ([]map[string]any, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	var result []map[string]any
	for _, b := range pe.balances {
		result = append(result, map[string]any{
			"currency": b.Currency,
			"total":    b.Total,
			"free":     b.Free,
			"used":     b.Used,
		})
	}
	return result, nil
}

func (pe *PaperExchange) GetPositions() ([]map[string]any, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	var result []map[string]any
	for _, symPos := range pe.positions {
		for _, pos := range symPos {
			result = append(result, map[string]any{
				"symbol":          pos.Symbol,
				"quantity":        pos.Quantity,
				"avg_entry_price": pos.AvgEntryPrice,
				"unrealized_pnl":  pos.UnrealizedPnL,
				"realized_pnl":    pos.RealizedPnL,
			})
		}
	}
	return result, nil
}

func (pe *PaperExchange) GetOpenOrders(symbol string) ([]map[string]any, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	var result []map[string]any
	for _, order := range pe.orders {
		if order.IsActive() && (symbol == "" || order.Symbol == symbol) {
			result = append(result, map[string]any{
				"id":         order.ID,
				"symbol":     order.Symbol,
				"side":       string(order.Side),
				"order_type": string(order.OrderType),
				"price":      order.Price,
				"quantity":   order.Quantity,
				"filled":     order.Filled,
				"status":     string(order.Status),
			})
		}
	}
	return result, nil
}

// Klines and ticker are not applicable for paper trading
func (pe *PaperExchange) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	return nil, fmt.Errorf("paper exchange does not provide klines")
}
func (pe *PaperExchange) GetTicker(symbol string) (map[string]any, error) {
	return nil, fmt.Errorf("paper exchange does not provide ticker")
}
func (pe *PaperExchange) StartMarketStream(symbols []string) error { return nil }
func (pe *PaperExchange) StartUserStream() error                   { return nil }

// ── Matching Engine ──

func (pe *PaperExchange) executeMarketOrder(book *goOrderBook, order *PaperOrder) (map[string]any, error) {
	trades := pe.matchMarketOrder(book, order)
	pe.applyTrades(trades)

	order.Status = model.StatusFilled
	if order.Filled < order.Quantity {
		order.Status = model.StatusCancelled // Partial fill for market
	}

	pe.mu.Lock()
	pe.orders[order.ID] = order
	pe.mu.Unlock()

	if pe.onOrderUpdate != nil {
		pe.onOrderUpdate(order.OrderData)
	}

	return map[string]any{
		"order_id": order.ID,
		"status":   string(order.Status),
		"filled":   order.Filled,
		"trades":   tradesToMaps(trades),
	}, nil
}

func (pe *PaperExchange) matchOrder(book *goOrderBook, taker *PaperOrder) []model.TradeData {
	var trades []model.TradeData

	for taker.Remaining() > 0 {
		var bestPrice float64
		var levels *[]*orderBookLevel

		if taker.Side == model.SideBuy {
			bestPrice = book.bestAsk()
			levels = &book.asks
		} else {
			bestPrice = book.bestBid()
			levels = &book.bids
		}

		if bestPrice == 0 {
			break
		}

		canMatch := false
		if taker.OrderType == model.TypeMarket {
			canMatch = true
		} else if taker.Side == model.SideBuy {
			canMatch = taker.Price >= bestPrice
		} else {
			canMatch = taker.Price <= bestPrice
		}

		if !canMatch {
			break
		}

		// Execute against maker orders at best price
		for _, level := range *levels {
			if level.price != bestPrice {
				continue
			}
			var remaining []*PaperOrder
			for _, maker := range level.orders {
				if taker.Remaining() <= 0 {
					remaining = append(remaining, maker)
					continue
				}
				tradeQty := math.Min(taker.Remaining(), maker.Remaining())
				maker.Filled += tradeQty
				taker.Filled += tradeQty

				if maker.Remaining() <= 0 {
					maker.Status = model.StatusFilled
				} else {
					maker.Status = model.StatusPartiallyFilled
					remaining = append(remaining, maker)
				}

				book.tradeCount++
				trade := model.TradeData{
					ID:        fmt.Sprintf("t-%d", book.tradeCount),
					Symbol:    book.symbol,
					Price:     bestPrice,
					Quantity:  tradeQty,
					Timestamp: time.Now().UnixMilli(),
				}
				if taker.Side == model.SideBuy {
					trade.Side = "BUY"
				} else {
					trade.Side = "SELL"
				}
				trades = append(trades, trade)
			}
			level.orders = remaining
		}
	}

	// Clean up empty levels
	book.mu.Lock()
	var cleanBids []*orderBookLevel
	for _, l := range book.bids {
		if len(l.orders) > 0 {
			cleanBids = append(cleanBids, l)
		}
	}
	book.bids = cleanBids

	var cleanAsks []*orderBookLevel
	for _, l := range book.asks {
		if len(l.orders) > 0 {
			cleanAsks = append(cleanAsks, l)
		}
	}
	book.asks = cleanAsks
	book.mu.Unlock()

	return trades
}

func (pe *PaperExchange) matchMarketOrder(book *goOrderBook, taker *PaperOrder) []model.TradeData {
	// Market orders match at the best available price across all levels
	var trades []model.TradeData

	for taker.Remaining() > 0 {
		var bestPrice float64
		var levels *[]*orderBookLevel

		if taker.Side == model.SideBuy {
			bestPrice = book.bestAsk()
			levels = &book.asks
		} else {
			bestPrice = book.bestBid()
			levels = &book.bids
		}

		if bestPrice == 0 {
			break
		}

		for _, level := range *levels {
			if level.price != bestPrice {
				continue
			}
			var remaining []*PaperOrder
			for _, maker := range level.orders {
				if taker.Remaining() <= 0 {
					remaining = append(remaining, maker)
					continue
				}
				tradeQty := math.Min(taker.Remaining(), maker.Remaining())
				maker.Filled += tradeQty
				taker.Filled += tradeQty

				if maker.Remaining() <= 0 {
					maker.Status = model.StatusFilled
				} else {
					maker.Status = model.StatusPartiallyFilled
					remaining = append(remaining, maker)
				}

				book.tradeCount++
				trade := model.TradeData{
					ID:        fmt.Sprintf("t-%d", book.tradeCount),
					Symbol:    book.symbol,
					Price:     bestPrice,
					Quantity:  tradeQty,
					Timestamp: time.Now().UnixMilli(),
				}
				if taker.Side == model.SideBuy {
					trade.Side = "BUY"
				} else {
					trade.Side = "SELL"
				}
				trades = append(trades, trade)
			}
			level.orders = remaining
		}
	}

	return trades
}

// ── Trade Application ──

func (pe *PaperExchange) applyTrades(trades []model.TradeData) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	for _, trade := range trades {
		// Update position
		pe.updatePosition(trade)

		// Update balance
		pe.updateBalance(trade)

		if pe.onTrade != nil {
			pe.onTrade(trade)
		}
	}

	// Snapshot equity
	pe.snapshotEquity()
}

func (pe *PaperExchange) updatePosition(trade model.TradeData) {
	symbol := trade.Symbol
	if pe.positions[symbol] == nil {
		pe.positions[symbol] = make(map[string]*PaperPosition)
	}

	posID := symbol + "-spot"
	pos, ok := pe.positions[symbol][posID]
	if !ok {
		pos = &PaperPosition{
			PositionData: model.PositionData{
				ID:       posID,
				Symbol:   symbol,
				Exchange: "paper",
				OpenedAt: trade.Timestamp,
			},
		}
		pe.positions[symbol][posID] = pos
	}

	pos.AddTrade(trade)
	pos.CurrentPrice = trade.Price

	if pos.Quantity > 0 {
		pos.UnrealizedPnL = (trade.Price - pos.AvgEntryPrice) * pos.Quantity
		pos.Side = "LONG"
	} else if pos.Quantity < 0 {
		pos.UnrealizedPnL = (pos.AvgEntryPrice - trade.Price) * math.Abs(pos.Quantity)
		pos.Side = "SHORT"
	} else {
		pos.UnrealizedPnL = 0
		pos.Side = "FLAT"
	}

	if pe.onPosition != nil {
		pe.onPosition(pos.PositionData)
	}
}

func (pe *PaperExchange) updateBalance(trade model.TradeData) {
	fee := trade.Price * trade.Quantity * pe.config.FeeRate

	if trade.Side == "BUY" {
		// Buying: decrease USDT, increase crypto
		cost := trade.Price*trade.Quantity + fee
		if bal, ok := pe.balances["USDT"]; ok {
			bal.Free -= cost
			bal.Total = bal.Free + bal.Used
		}

		currency := pe.getBaseCurrency(trade.Symbol)
		if currency != "" {
			if _, ok := pe.balances[currency]; !ok {
				pe.balances[currency] = &model.Balance{Currency: currency}
			}
			pe.balances[currency].Free += trade.Quantity
			pe.balances[currency].Total = pe.balances[currency].Free + pe.balances[currency].Used
		}
	} else {
		// Selling: increase USDT, decrease crypto
		proceeds := trade.Price*trade.Quantity - fee
		if bal, ok := pe.balances["USDT"]; ok {
			bal.Free += proceeds
			bal.Total = bal.Free + bal.Used
		}

		currency := pe.getBaseCurrency(trade.Symbol)
		if currency != "" {
			if bal, ok := pe.balances[currency]; ok {
				bal.Free -= trade.Quantity
				bal.Total = bal.Free + bal.Used
			}
		}
	}
}

func (pe *PaperExchange) lockFunds(order *PaperOrder) bool {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if order.Side == model.SideBuy {
		cost := order.Price * order.Quantity
		bal, ok := pe.balances["USDT"]
		if !ok || bal.Free < cost {
			return false
		}
		bal.Free -= cost
		bal.Used += cost
	} else {
		currency := pe.getBaseCurrency(order.Symbol)
		bal, ok := pe.balances[currency]
		if !ok || bal.Free < order.Quantity {
			return false
		}
		bal.Free -= order.Quantity
		bal.Used += order.Quantity
	}
	return true
}

func (pe *PaperExchange) unlockFunds(order *PaperOrder) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if order.Side == model.SideBuy {
		cost := order.Price * order.Remaining()
		if bal, ok := pe.balances["USDT"]; ok {
			bal.Free += cost
			bal.Used -= cost
		}
	} else {
		currency := pe.getBaseCurrency(order.Symbol)
		if bal, ok := pe.balances[currency]; ok {
			bal.Free += order.Remaining()
			bal.Used -= order.Remaining()
		}
	}
}

func (pe *PaperExchange) getBaseCurrency(symbol string) string {
	// Extract base from e.g. "BTCUSDT" -> "BTC"
	if len(symbol) > 4 {
		return symbol[:len(symbol)-4]
	}
	return ""
}

// ── Equity Tracking ──

func (pe *PaperExchange) snapshotEquity() {
	totalEquity := pe.calculateTotalEquity()
	available := pe.balances["USDT"].Free
	margin := pe.balances["USDT"].Used

	snapshot := model.PortfolioSnapshot{
		TotalEquity:      totalEquity,
		AvailableBalance: available,
		MarginUsed:       margin,
		Timestamp:        time.Now().UnixMilli(),
	}

	pe.equity = append(pe.equity, snapshot)
	if len(pe.equity) > 5000 {
		pe.equity = pe.equity[len(pe.equity)-5000:]
	}

	// Update drawdown
	peak := totalEquity
	for _, s := range pe.equity {
		if s.TotalEquity > peak {
			peak = s.TotalEquity
		}
	}
	if peak > 0 {
		snapshot.Drawdown = (peak - totalEquity) / peak * 100
	}
}

func (pe *PaperExchange) calculateTotalEquity() float64 {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	total := 0.0
	for _, bal := range pe.balances {
		total += bal.Total
	}
	for _, symPos := range pe.positions {
		for _, pos := range symPos {
			total += pos.UnrealizedPnL
		}
	}
	return total
}

// GetEquity returns the current total equity.
func (pe *PaperExchange) GetEquity() float64 {
	return pe.calculateTotalEquity()
}

// GetSnapshot returns the latest portfolio snapshot.
func (pe *PaperExchange) GetSnapshot() *model.PortfolioSnapshot {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	if len(pe.equity) == 0 {
		return nil
	}
	s := pe.equity[len(pe.equity)-1]
	return &s
}

// GetEquityCurve returns equity history.
func (pe *PaperExchange) GetEquityCurve() []model.PortfolioSnapshot {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	curve := make([]model.PortfolioSnapshot, len(pe.equity))
	copy(curve, pe.equity)
	return curve
}

// ── Helpers ──

func (pe *PaperExchange) simulateLatency() {
	latency := pe.config.MinLatency +
		time.Duration(pe.rng.Int63n(int64(pe.config.MaxLatency-pe.config.MinLatency)))
	time.Sleep(latency)
}

func tradesToMaps(trades []model.TradeData) []map[string]any {
	var result []map[string]any
	for _, t := range trades {
		result = append(result, map[string]any{
			"id":        t.ID,
			"price":     t.Price,
			"quantity":  t.Quantity,
			"side":      t.Side,
			"timestamp": t.Timestamp,
		})
	}
	return result
}
