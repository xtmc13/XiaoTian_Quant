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

	// Price provider for market data
	priceProvider func(symbol string) (price float64, ok bool)

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

// PlaceOrder 下单。生产化行为（与真实交易所一致）：
//   - 限价单/市价卖出：先锁定资金（买锁 quote，卖锁 base），不足直接拒绝
//   - 市价买入：成本无法预判，撮合时按可用 quote 截断（部分成交），永不负余额
//   - 成交即结算：锁定部分按实际成交额多退少补，撤单释放剩余锁定
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
		if order.Side == model.SideSell {
			// 市价卖出：可用 base 不足时按可用截断（部分成交），一点没有才拒绝
			// （杜绝纯下跌行情卖空记负余额，同时保留与交易所一致的 partiał-fill 行为）。
			avail := pe.availableBase(pe.getBaseCurrency(symbol))
			if avail <= 0 {
				order.Status = model.StatusRejected
				return map[string]any{
					"order_id": "",
					"status":   string(order.Status),
					"filled":   0.0,
					"trades":   []map[string]any{},
				}, fmt.Errorf("insufficient balance: not enough %s to sell", symbol)
			}
			if avail < quantity {
				order.Quantity = avail // 只卖持有的部分
			}
			if !pe.lockFunds(order) {
				order.Status = model.StatusRejected
				return map[string]any{
					"order_id": "",
					"status":   string(order.Status),
					"filled":   0.0,
					"trades":   []map[string]any{},
				}, fmt.Errorf("insufficient balance for market sell")
			}
		}
		return pe.executeMarketOrder(book, order)
	}

	// 限价单：挂出前先锁资金（锁不住=余额不足，拒绝，与交易所 -2010 一致）。
	if !pe.lockFunds(order) {
		order.Status = model.StatusRejected
		return map[string]any{
			"order_id": "",
			"status":   string(order.Status),
			"filled":   0.0,
			"trades":   []map[string]any{},
		}, fmt.Errorf("insufficient balance for limit order")
	}

	// Limit order: try to match first
	fills := pe.matchOrder(book, order)
	if len(fills) > 0 {
		pe.applyFills(fills)
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
		"trades":   fillsToMaps(fills),
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

// SetPriceProvider injects a real-time price source for paper trading market data.
func (pe *PaperExchange) SetPriceProvider(provider func(symbol string) (price float64, ok bool)) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.priceProvider = provider
}

// GetKlines returns synthetic OHLCV based on the current market price from the price provider.
func (pe *PaperExchange) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	pe.mu.RLock()
	provider := pe.priceProvider
	pe.mu.RUnlock()

	if provider == nil {
		return nil, fmt.Errorf("paper exchange: no price provider configured")
	}
	price, ok := provider(symbol)
	if !ok {
		return nil, fmt.Errorf("paper exchange: no price available for %s", symbol)
	}
	if limit <= 0 {
		limit = 100
	}
	nowMs := time.Now().UnixMilli()
	klines := make([][]any, limit)
	for i := 0; i < limit; i++ {
		// Synthetic kline: use current price with tiny noise for realistic look
		noise := 1.0 + (pe.rng.Float64()-0.5)*0.0002
		o, h, l, c := price*noise, price*noise*1.0001, price*noise*0.9999, price*noise
		klines[i] = []any{
			float64(nowMs - int64((limit-i))*60000), // timestamp
			o, h, l, c,
			0.0, // volume (unavailable for paper)
		}
	}
	return klines, nil
}

// GetTicker returns the current ticker from the price provider.
func (pe *PaperExchange) GetTicker(symbol string) (map[string]any, error) {
	pe.mu.RLock()
	provider := pe.priceProvider
	pe.mu.RUnlock()

	if provider == nil {
		return nil, fmt.Errorf("paper exchange: no price provider configured")
	}
	price, ok := provider(symbol)
	if !ok {
		return nil, fmt.Errorf("paper exchange: no price available for %s", symbol)
	}
	return map[string]any{
		"symbol": symbol,
		"last":   price,
		"bid":    price * 0.9999,
		"ask":    price * 1.0001,
		"source": "paper",
	}, nil
}

func (pe *PaperExchange) StartMarketStream(symbols []string) error { return nil }
func (pe *PaperExchange) StartUserStream() error                   { return nil }

// ── Matching Engine ──

func (pe *PaperExchange) executeMarketOrder(book *goOrderBook, order *PaperOrder) (map[string]any, error) {
	fills := pe.matchMarketOrder(book, order)
	pe.applyFills(fills)

	order.Status = model.StatusFilled
	if order.Filled < order.Quantity {
		order.Status = model.StatusCancelled // Partial fill for market
		// 未成交部分的锁定（仅卖出锁仓）要释放。
		pe.unlockFunds(&PaperOrder{OrderData: model.OrderData{
			Symbol:   order.Symbol,
			Side:     order.Side,
			Price:    order.Price,
			Quantity: order.Quantity - order.Filled,
		}})
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
		"trades":   fillsToMaps(fills),
	}, nil
}

// paperFill 一笔撮合成交：taker 视角的 trade + 双方订单引用（结算锁仓用）。
type paperFill struct {
	trade model.TradeData
	taker *PaperOrder
	maker *PaperOrder
}

func (pe *PaperExchange) matchOrder(book *goOrderBook, taker *PaperOrder) []paperFill {
	var fills []paperFill

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
				fills = append(fills, paperFill{trade: trade, taker: taker, maker: maker})
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

	return fills
}

func (pe *PaperExchange) matchMarketOrder(book *goOrderBook, taker *PaperOrder) []paperFill {
	// Market orders match at the best available price across all levels.
	// 市价买入按可用 quote 截断，杜绝余额不足时的负余额（部分成交，行为对齐交易所）。
	// 注意：结算在撮合后（applyFills），循环内须自行累计已匹配金额。
	var fills []paperFill
	var spentQuote float64

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

		capped := false
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

				// 资金截断（仅市价买入需要：卖出已在上单前锁仓校验）。
				if taker.Side == model.SideBuy {
					avail := pe.availableQuote() - spentQuote
					if avail <= 0 {
						capped = true
						break
					}
					maxAfford := avail / bestPrice
					if tradeQty > maxAfford {
						tradeQty = maxAfford
					}
					if tradeQty <= 0 {
						capped = true
						break
					}
				}

				maker.Filled += tradeQty
				taker.Filled += tradeQty
				spentQuote += bestPrice * tradeQty

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
				fills = append(fills, paperFill{trade: trade, taker: taker, maker: maker})
			}
			level.orders = remaining
			if capped {
				break
			}
		}
		if capped {
			break
		}
	}

	return fills
}

// ── Trade Application ──

// applyFills 应用撮合结果：更新持仓 + 结算资金（锁仓多退少补）+ 回调。
// 全部成交都过结算，任何路径都不会把余额记成负数。
// 注意：结算持 pe.mu，快照在锁外做（snapshotEquity 自带锁，不可重入）。
func (pe *PaperExchange) applyFills(fills []paperFill) {
	if len(fills) == 0 {
		return
	}
	pe.mu.Lock()
	for _, f := range fills {
		pe.updatePosition(f.trade)
		pe.settleFill(f)
		if pe.onTrade != nil {
			pe.onTrade(f.trade)
		}
	}
	pe.mu.Unlock()

	pe.snapshotEquity()
}

// availableQuote 当前可用 quote（USDT）余额。
func (pe *PaperExchange) availableQuote() float64 {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	if bal, ok := pe.balances["USDT"]; ok {
		return bal.Free
	}
	return 0
}

// availableBase 当前可用 base 余额。
func (pe *PaperExchange) availableBase(base string) float64 {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	if bal, ok := pe.balances[base]; ok {
		return bal.Free
	}
	return 0
}

// settleFill 单笔成交结算（在 pe.mu 保护下调用）：
//   - 买入：Free 减少 成交额+手续费；限价买入已按限价锁仓 → 释放锁定并按成交价多退少补
//   - 卖出：Free 增加 成交额-手续费；锁定 base（限价/市价卖出均已锁）→ 释放 Used
func (pe *PaperExchange) settleFill(f paperFill) {
	trade := f.trade
	fee := trade.Price * trade.Quantity * pe.config.FeeRate
	baseCur := pe.getBaseCurrency(trade.Symbol)

	quote, ok := pe.balances["USDT"]
	if !ok {
		quote = &model.Balance{Currency: "USDT"}
		pe.balances["USDT"] = quote
	}
	base, baseOK := pe.balances[baseCur]
	if trade.Side == "BUY" {
		cost := trade.Price*trade.Quantity + fee
		if f.taker != nil && f.taker.OrderType == model.TypeLimit && f.taker.Price > 0 {
			locked := f.taker.Price * trade.Quantity
			quote.Used -= locked
			quote.Free += locked - cost // 成交价优于限价 → 退还差额
		} else {
			quote.Free -= cost // 市价买入（已按可用截断）
		}
		if !baseOK {
			base = &model.Balance{Currency: baseCur}
			pe.balances[baseCur] = base
		}
		base.Free += trade.Quantity
	} else {
		proceeds := trade.Price*trade.Quantity - fee
		quote.Free += proceeds
		// taker 卖出（限价/市价都已锁 base）释放锁定。
		if f.taker != nil && (f.taker.OrderType == model.TypeLimit || f.taker.OrderType == model.TypeMarket) {
			if baseOK {
				base.Used -= trade.Quantity
			}
		}
	}

	// maker 侧结算：resting 限价单的锁定按成交价释放。
	if f.maker != nil {
		if f.maker.Side == model.SideBuy {
			locked := f.maker.Price * trade.Quantity
			makerCost := trade.Price*trade.Quantity + fee
			quote.Used -= locked
			quote.Free += locked - makerCost
			makerBase, ok := pe.balances[pe.getBaseCurrency(f.maker.Symbol)]
			if !ok {
				makerBase = &model.Balance{Currency: pe.getBaseCurrency(f.maker.Symbol)}
				pe.balances[pe.getBaseCurrency(f.maker.Symbol)] = makerBase
			}
			makerBase.Free += trade.Quantity
		} else {
			makerProceeds := trade.Price*trade.Quantity - fee
			quote.Free += makerProceeds
			makerBase, ok := pe.balances[pe.getBaseCurrency(f.maker.Symbol)]
			if ok {
				makerBase.Used -= trade.Quantity
			}
		}
	}

	// 数值防护：浮点尾差不允许把余额推成负。
	for _, b := range pe.balances {
		if b.Free < 0 && b.Free > -1e-9 {
			b.Free = 0
		}
		if b.Used < 0 && b.Used > -1e-9 {
			b.Used = 0
		}
		b.Total = b.Free + b.Used
	}
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
	// Extract base from e.g. "BTCUSDT" -> "BTC", "ETHBTC" -> "ETH", "LINKDAI" -> "LINK"
	// Try common quote currencies first (longest first)
	quoteCurrencies := []string{"USDT", "USDC", "BUSD", "UST", "DAI", "WBTC", "BTC", "ETH"}
	for _, q := range quoteCurrencies {
		if len(symbol) > len(q) && symbol[len(symbol)-len(q):] == q {
			return symbol[:len(symbol)-len(q)]
		}
	}
	// Fallback: if symbol > 4 chars, trim last 4 (most quotes are 4-char)
	if len(symbol) >= 4 {
		return symbol[:len(symbol)-4]
	}
	return ""
}

// ── Equity Tracking ──

// snapshotEquity 自包含加锁：先读快照所需数据，再单独写 equity 切片。
func (pe *PaperExchange) snapshotEquity() {
	pe.mu.RLock()
	totalEquity := pe.calculateTotalEquityLocked()
	available := pe.balances["USDT"].Free
	margin := pe.balances["USDT"].Used
	pe.mu.RUnlock()

	snapshot := model.PortfolioSnapshot{
		TotalEquity:      totalEquity,
		AvailableBalance: available,
		MarginUsed:       margin,
		Timestamp:        time.Now().UnixMilli(),
	}

	pe.mu.Lock()
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
	pe.mu.Unlock()
}

// calculateTotalEquity 当前总权益（GetEquity 独立入口）。
func (pe *PaperExchange) calculateTotalEquity() float64 {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	return pe.calculateTotalEquityLocked()
}

// calculateTotalEquityLocked 计算总权益（调用方须已持有 pe.mu 读/写锁）。
func (pe *PaperExchange) calculateTotalEquityLocked() float64 {
	// Start with USDT balance
	total := 0.0
	if bal, ok := pe.balances["USDT"]; ok {
		total = bal.Total
	}

	// Convert non-USDT crypto balances to USDT value using position current prices
	for cur, bal := range pe.balances {
		if cur == "USDT" {
			continue
		}
		// Try to find a position for this asset to get current price
		p := 0.0
		sym := cur + "USDT"
		if symPos, ok := pe.positions[sym]; ok {
			for _, pos := range symPos {
				if pos.CurrentPrice > 0 && pos.Quantity > 0 {
					p = pos.CurrentPrice
					break
				}
			}
		}
		if p > 0 {
			total += bal.Total * p
		}
		// If no position price, the asset has no market value in our system
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

// fillsToMaps 把内部撮合明细转成对外成交列表。
func fillsToMaps(fills []paperFill) []map[string]any {
	trades := make([]model.TradeData, 0, len(fills))
	for _, f := range fills {
		trades = append(trades, f.trade)
	}
	return tradesToMaps(trades)
}
