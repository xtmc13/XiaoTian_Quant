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
	if totalQty != 0 {
		// 空单（totalQty<0）均价 = 负成本/负量 = 正开仓价（此前 totalQty>0 才
		// 赋值，空单均价恒 0、浮亏显示全错）。CostBasis 取名义绝对值（与
		// service/store 侧 Quantity×AvgEntryPrice 的量纲一致，PnL% 分母为正）。
		p.AvgEntryPrice = totalCost / totalQty
		p.CostBasis = math.Abs(totalCost)
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
	enabled   bool // 模拟盘账户开关：false 时拒绝新下单
	// filledApplied 记录各订单已入账成交量：OMS 成交回报是累计语义（RecordFill/
	// OnOrderUpdate 可能多次触发），入账只取增量，天然幂等防重复记账。
	filledApplied map[string]float64
	mu            sync.RWMutex

	// Price provider for market data
	priceProvider func(symbol string) (price float64, ok bool)

	// Event callbacks
	onOrderUpdate func(order model.OrderData)
	onTrade       func(trade model.TradeData)
	onPosition    func(pos model.PositionData)
	onStateChange func() // 账户状态变更（成交/重置/开关），锁外触发持久化
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
		config:        cfg,
		books:         make(map[string]*goOrderBook),
		positions:     make(map[string]map[string]*PaperPosition),
		balances:      make(map[string]*model.Balance),
		orders:        make(map[string]*PaperOrder),
		filledApplied: make(map[string]float64),
		rng:           rand.New(rand.NewSource(time.Now().UnixNano())),
		enabled:       true,
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

// IsEnabled 返回模拟盘账户开关状态。
func (pe *PaperExchange) IsEnabled() bool {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	return pe.enabled
}

// SetEnabled 设置模拟盘账户开关；停用后 PlaceOrder 拒绝新单。
func (pe *PaperExchange) SetEnabled(v bool) {
	pe.mu.Lock()
	pe.enabled = v
	pe.mu.Unlock()
	pe.notifyStateChange()
}

// GetAccount 返回模拟盘账户状态（开关 + USDT 余额 + 初始余额）。
func (pe *PaperExchange) GetAccount() map[string]any {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	bal := 0.0
	if b := pe.balances["USDT"]; b != nil {
		bal = b.Total
	}
	return map[string]any{
		"enabled":         pe.enabled,
		"balance":         bal,
		"initial_balance": pe.config.InitialBalance,
	}
}

// SetBalance 重置 USDT 余额，并清空全部持仓/挂单/订单簿（全新起点）。
func (pe *PaperExchange) SetBalance(usdt float64) {
	if usdt < 0 {
		usdt = 0
	}
	pe.mu.Lock()
	pe.balances = map[string]*model.Balance{
		"USDT": {Currency: "USDT", Total: usdt, Free: usdt, Used: 0},
	}
	pe.positions = make(map[string]map[string]*PaperPosition)
	pe.orders = make(map[string]*PaperOrder)
	pe.books = make(map[string]*goOrderBook)
	pe.filledApplied = make(map[string]float64)
	pe.config.InitialBalance = usdt
	pe.mu.Unlock()
	pe.notifyStateChange()
	log.Printf("[Paper] 账户余额已重置为 $%.2f，持仓/挂单已清空", usdt)
}

// PositionSnapshot 单个持仓 + 成交历史（重建均价/成本用）。
type PositionSnapshot struct {
	Data   model.PositionData `json:"data"`
	Trades []model.TradeData  `json:"trades"`
}

// AccountSnapshot 模拟盘账户持久化快照（重启恢复用：余额/持仓随重启存活）。
// 2026-10-03 前只持久化 {enabled,balance}，重启经 SetBalance 清空持仓——
// 策略账本（xt_orders sig: 净持仓）失去镜像，每次重启触发一次
// "Close from strategy ledger" 失败 WARN，且观察仓位重启即丢。
type AccountSnapshot struct {
	Enabled        bool                      `json:"enabled"`
	Balance        float64                   `json:"balance"` // USDT 总余额（冗余+旧格式兼容）
	InitialBalance float64                   `json:"initial_balance"`
	Balances       map[string]*model.Balance `json:"balances,omitempty"`
	Positions      []PositionSnapshot        `json:"positions,omitempty"`
}

// SnapshotAccount 导出账户全量快照（持锁拷贝）。零数量持仓不导出（已平仓残留）。
func (pe *PaperExchange) SnapshotAccount() AccountSnapshot {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	snap := AccountSnapshot{
		Enabled:        pe.enabled,
		InitialBalance: pe.config.InitialBalance,
		Balances:       make(map[string]*model.Balance, len(pe.balances)),
	}
	for k, v := range pe.balances {
		cp := *v
		snap.Balances[k] = &cp
	}
	if b, ok := pe.balances["USDT"]; ok {
		snap.Balance = b.Total
	}
	for _, byID := range pe.positions {
		for _, pos := range byID {
			if pos.Quantity == 0 {
				continue
			}
			snap.Positions = append(snap.Positions, PositionSnapshot{
				Data:   pos.PositionData,
				Trades: append([]model.TradeData(nil), pos.trades...),
			})
		}
	}
	return snap
}

// RestoreAccount 无清空恢复（与 SetBalance 的"全新起点"语义相反）：余额/持仓/
// 开关直接落位，用于重启恢复。Balances 为空且 Balance>0 时按旧格式兼容
// （只有 USDT 余额、无持仓）。
func (pe *PaperExchange) RestoreAccount(snap AccountSnapshot) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.enabled = snap.Enabled
	if snap.InitialBalance > 0 {
		pe.config.InitialBalance = snap.InitialBalance
	}
	if len(snap.Balances) > 0 {
		pe.balances = snap.Balances
	} else if snap.Balance > 0 {
		pe.balances = map[string]*model.Balance{
			"USDT": {Currency: "USDT", Total: snap.Balance, Free: snap.Balance, Used: 0},
		}
	}
	pe.positions = make(map[string]map[string]*PaperPosition)
	for _, ps := range snap.Positions {
		if ps.Data.Quantity == 0 {
			continue
		}
		pos := &PaperPosition{
			PositionData: ps.Data,
			trades:       append([]model.TradeData(nil), ps.Trades...),
		}
		// 旧格式/手工快照只有聚合 Quantity 没有逐笔 trades：AddTrade 的持仓量
		// 由 trades 序列重算，空 trades 会在恢复后第一笔成交时把持仓量整个
		// 吃掉（恢复 16.82 的多仓卖 12.52 会被记成 -12.52）。按快照聚合量
		// 补一笔合成开仓成交（方向随持仓符号、价格取均价），保持"trades 是
		// 真值"的不变量。
		if len(pos.trades) == 0 {
			side := "BUY"
			qty := ps.Data.Quantity
			if qty < 0 {
				side = "SELL"
				qty = -qty
			}
			pos.trades = []model.TradeData{{
				Symbol: ps.Data.Symbol, Side: side, Quantity: qty,
				Price: ps.Data.AvgEntryPrice, Timestamp: ps.Data.OpenedAt,
			}}
		}
		if pe.positions[pos.Symbol] == nil {
			pe.positions[pos.Symbol] = make(map[string]*PaperPosition)
		}
		pe.positions[pos.Symbol][pos.ID] = pos
	}
	pe.filledApplied = make(map[string]float64) // 重启后 OMS 从累计值重放，重新取增量
	log.Printf("[Paper] 账户状态已恢复: USDT=%.2f 初始=%.2f 持仓=%d 只 enabled=%v",
		snap.Balance, snap.InitialBalance, len(snap.Positions), snap.Enabled)
}

// OnStateChange 注册账户状态变更回调（成交结算/余额重置/开关切换后、锁外
// 触发），由上层把 SnapshotAccount 落盘。
func (pe *PaperExchange) OnStateChange(fn func()) { pe.onStateChange = fn }

// notifyStateChange 触发持久化回调。须在锁外调用（回调内会回取快照，持锁
// 调用会自死锁）。
func (pe *PaperExchange) notifyStateChange() {
	if pe.onStateChange != nil {
		pe.onStateChange()
	}
}

// FeeRate 返回当前账户的手续费率（绩效核算扣费用）。
func (pe *PaperExchange) FeeRate() float64 {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	return pe.config.FeeRate
}

// ApplySimulatedFill 把一笔 OMS 模拟成交记入账户：持仓（AddTrade）+ 资金
// 结算 + 状态变更通知。策略/手动 paper 单不经 PlaceOrder 撮合——
// app.simulatePaperFill 对市价单直接成交、限价单走撮合引擎后经 OMS
// RecordFill 回报——两条路径都从这里入账，账户才是真实状态（此前资产页
// 余额恒为初始值、快照无持仓、重启恢复后镜像对不上，2026-10-03 实证）。
// orderID 为 OMS 订单号（去重键）；trade.Quantity 为累计成交量，入账只取
// 增量，重复/乱序回报天然幂等。
func (pe *PaperExchange) ApplySimulatedFill(orderID string, trade model.TradeData) {
	if orderID == "" || trade.Quantity <= 0 || trade.Price <= 0 {
		return
	}
	pe.mu.Lock()
	applied := pe.filledApplied[orderID]
	if trade.Quantity <= applied {
		pe.mu.Unlock()
		return // 重复/旧回报
	}
	trade.Quantity -= applied
	pe.filledApplied[orderID] = applied + trade.Quantity
	fee := trade.Price * trade.Quantity * pe.config.FeeRate
	pe.updatePosition(trade)
	pe.settleSimulatedFill(trade, fee)
	pe.mu.Unlock()

	pe.snapshotEquity()
	pe.notifyStateChange()
}

// settleSimulatedFill OMS 模拟成交的资金结算：买 = quote 减少(含费) + base
// 增加；卖 = base 减少 + quote 增加(扣费)。OMS 下单链路在成交前已通过
// LockOrderFunds 把资金从 Free 锁到 Used——结算先释放对应锁定再按实扣，
// 否则同一笔成交会被扣两次（锁定一次、结算一次）。无锁定的旧路径
// （Used=0）行为与之前完全一致。防御性截断，任何路径不得把余额记成负。
func (pe *PaperExchange) settleSimulatedFill(trade model.TradeData, fee float64) {
	baseCur := pe.getBaseCurrency(trade.Symbol)
	quote, ok := pe.balances["USDT"]
	if !ok {
		quote = &model.Balance{Currency: "USDT"}
		pe.balances["USDT"] = quote
	}
	base, baseOK := pe.balances[baseCur]
	if trade.Side == "BUY" {
		// 释放 OMS 锁定的 quote 成本（锁定价与成交价尾差多退少补）。
		if quote.Used > 0 {
			release := trade.Price * trade.Quantity
			if release > quote.Used {
				release = quote.Used
			}
			quote.Used -= release
			quote.Free += release
		}
		cost := trade.Price*trade.Quantity + fee
		if quote.Free < cost {
			cost = quote.Free // 防御截断（OMS 风控已 gate，正常到不了这里）
		}
		quote.Free -= cost
		if !baseOK {
			base = &model.Balance{Currency: baseCur}
			pe.balances[baseCur] = base
		}
		base.Free += trade.Quantity
	} else {
		if !baseOK {
			base = &model.Balance{Currency: baseCur}
			pe.balances[baseCur] = base
		}
		// 释放 OMS 锁定的 base（平多卖出已在 LockOrderFunds 锁过；
		// 合约开空无锁定，release=0）。
		if base.Used > 0 {
			release := trade.Quantity
			if release > base.Used {
				release = base.Used
			}
			base.Used -= release
			base.Free += release
		}
		// 卖出：base 无条件扣减（合约开空时允许为负 = 空头负债，买平回补）。
		base.Free -= trade.Quantity
		proceeds := trade.Price*trade.Quantity - fee
		if proceeds > 0 {
			quote.Free += proceeds
		}
	}
	quote.Total = quote.Free + quote.Used
	if base != nil {
		base.Total = base.Free + base.Used
	}
}

func (pe *PaperExchange) Start() error {
	log.Printf("[Paper] Paper trading exchange started with initial balance: $%.2f", pe.config.InitialBalance)
	return nil
}
func (pe *PaperExchange) Stop() error       { return nil }
func (pe *PaperExchange) IsConnected() bool { return true }

// Callback setters
func (pe *PaperExchange) OnOrderUpdate(fn func(order model.OrderData)) { pe.onOrderUpdate = fn }
func (pe *PaperExchange) OnTrade(fn func(trade model.TradeData))       { pe.onTrade = fn }
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
	// 模拟盘账户开关：停用即拒绝新下单（撤单/查询不受影响）
	pe.mu.RLock()
	enabled := pe.enabled
	pe.mu.RUnlock()
	if !enabled {
		return nil, fmt.Errorf("模拟盘账户已停用，请在资产页开启后再下单")
	}

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
	pe.notifyStateChange()
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

// FreeBalance 某币种当前可用余额（OMS 锁仓与重启恢复注入的背书读取）。
func (pe *PaperExchange) FreeBalance(currency string) float64 {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	if bal, ok := pe.balances[currency]; ok {
		return bal.Free
	}
	return 0
}

// NetPositionQuantity 某 symbol 镜像持仓的净数量（无持仓返回 0；合约开空
// 结算允许为负，此处如实返回）。
func (pe *PaperExchange) NetPositionQuantity(symbol string) float64 {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	net := 0.0
	for _, pos := range pe.positions[symbol] {
		net += pos.Quantity
	}
	return net
}

// LockOrderFunds OMS 下单链路（app Context wiring）对 paper 单的资金锁。
// 口径与 ApplySimulatedFill 的结算严格一一对应：
//   - 买入：锁 quote（USDT）成本 price*qty（结算扣 成本+手续费，多退少补）；
//   - 现货卖出：必须持有足额 base，锁 qty，不足即拒单（此前 OMS 锁的是
//     PortfolioManager 内存镜像——每次重启只重建 USDT 初始余额、base 归零，
//     重启恢复持仓的平仓单全被"余额不足"误拒，2026-10-08 生产实锤）；
//   - 合约（swap）卖出：base 足额时锁 qty（平多），不足时放行不锁（开空——
//     结算允许 base 记负=空头负债，与 settleSimulatedFill 同口径）。
//
// 锁定的资金在结算（ApplySimulatedFill）或 UnlockOrderFunds（拒单/撤单）
// 时释放。price 对市价单须由调用方先解析（last/synthetic 价）。
func (pe *PaperExchange) LockOrderFunds(symbol string, side model.OrderSide, marketType model.MarketType, price, qty float64) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if side == model.SideBuy {
		cost := price * qty
		quote, ok := pe.balances["USDT"]
		if !ok {
			quote = &model.Balance{Currency: "USDT"}
			pe.balances["USDT"] = quote
		}
		if quote.Free < cost {
			return fmt.Errorf("insufficient %s balance: %.2f < %.2f", "USDT", quote.Free, cost)
		}
		quote.Free -= cost
		quote.Used += cost
		quote.Total = quote.Free + quote.Used
		return nil
	}

	baseCur := pe.getBaseCurrency(symbol)
	base, ok := pe.balances[baseCur]
	if !ok {
		base = &model.Balance{Currency: baseCur}
		pe.balances[baseCur] = base
	}
	if marketType == model.MarketSwap {
		// 平多锁已有的部分；开空无锁放行。
		lockQty := qty
		if base.Free < lockQty {
			lockQty = base.Free
		}
		if lockQty < 0 {
			lockQty = 0
		}
		base.Free -= lockQty
		base.Used += lockQty
		base.Total = base.Free + base.Used
		return nil
	}
	if base.Free < qty {
		return fmt.Errorf("insufficient %s balance: %.2f < %.2f", baseCur, base.Free, qty)
	}
	base.Free -= qty
	base.Used += qty
	base.Total = base.Free + base.Used
	return nil
}

// UnlockOrderFunds 撤销 LockOrderFunds（拒单/撤单回滚）：买入按锁定时同价
// 释放 quote 成本，卖出释放 base（不超过当前 Used，合约开空无锁时为 0）。
func (pe *PaperExchange) UnlockOrderFunds(symbol string, side model.OrderSide, price, qty float64) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if side == model.SideBuy {
		cost := price * qty
		quote, ok := pe.balances["USDT"]
		if !ok || quote.Used <= 0 {
			return
		}
		if cost > quote.Used {
			cost = quote.Used
		}
		quote.Used -= cost
		quote.Free += cost
		quote.Total = quote.Free + quote.Used
		return
	}

	baseCur := pe.getBaseCurrency(symbol)
	base, ok := pe.balances[baseCur]
	if !ok || base.Used <= 0 {
		return
	}
	release := qty
	if release > base.Used {
		release = base.Used
	}
	base.Used -= release
	base.Free += release
	base.Total = base.Free + base.Used
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
