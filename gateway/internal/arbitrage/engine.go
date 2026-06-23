package arbitrage

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Exchange Client Interface ─────────────────────────────────

// ExchangeClient defines the minimal interface for arbitrage.
type ExchangeClient interface {
	Name() string
	GetTicker(symbol string) (map[string]any, error)
	PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error)
	GetBalance() ([]map[string]any, error)
	StartMarketStream(symbols []string) error
	StopStream() error
	WireToEventBus(bus *event.EventBus)
}

// ── Opportunity ─────────────────────────────────────────────────

// Opportunity represents a detected arbitrage spread with order-book depth.
type Opportunity struct {
	Symbol              string  `json:"symbol"`
	BuyExchange         string  `json:"buy_exchange"`
	SellExchange        string  `json:"sell_exchange"`
	BuyPrice            float64 `json:"buy_price"`             // best ask (surface)
	SellPrice           float64 `json:"sell_price"`            // best bid (surface)
	SpreadPct           float64 `json:"spread_pct"`
	SpreadAbs           float64 `json:"spread_abs"`
	ExecutableBuyPrice  float64 `json:"executable_buy_price"`  // avg fill after walking asks
	ExecutableSellPrice float64 `json:"executable_sell_price"` // avg fill after walking bids
	BuyDepthQty         float64 `json:"buy_depth_qty"`         // total asks qty examined
	SellDepthQty        float64 `json:"sell_depth_qty"`        // total bids qty examined
	SlippageBuyPct      float64 `json:"slippage_buy_pct"`
	SlippageSellPct     float64 `json:"slippage_sell_pct"`
	MaxExecutableQty    float64 `json:"max_executable_qty"`
	AdjustedQty         float64 `json:"adjusted_qty"` // final qty after depth adaptation
	Viable              bool    `json:"viable"`
	Timestamp           int64   `json:"timestamp"`
}

// IsProfitable returns true if executable spread exceeds threshold after fees.
func (o *Opportunity) IsProfitable(minSpreadPct, feeA, feeB float64) bool {
	if !o.Viable || o.ExecutableBuyPrice <= 0 || o.ExecutableSellPrice <= 0 {
		return false
	}
	netSpread := (o.ExecutableSellPrice-o.ExecutableBuyPrice)/o.ExecutableBuyPrice*100 - (feeA+feeB)*100
	return netSpread >= minSpreadPct
}

// NetSpreadPct returns the executable spread after fees.
func (o *Opportunity) NetSpreadPct(feeA, feeB float64) float64 {
	if !o.Viable || o.ExecutableBuyPrice <= 0 || o.ExecutableSellPrice <= 0 {
		return 0
	}
	return (o.ExecutableSellPrice-o.ExecutableBuyPrice)/o.ExecutableBuyPrice*100 - (feeA+feeB)*100
}

// ── Trade Pair ──────────────────────────────────────────────────

// TradePair tracks an arbitrage round-trip through its lifecycle.
type TradePair struct {
	ID           string  `json:"id"`
	Symbol       string  `json:"symbol"`
	BuyExchange  string  `json:"buy_exchange"`
	SellExchange string  `json:"sell_exchange"`
	BuyPrice     float64 `json:"buy_price"`
	SellPrice    float64 `json:"sell_price"`
	Quantity     float64 `json:"quantity"`
	BuyOrderID   string  `json:"buy_order_id"`
	SellOrderID  string  `json:"sell_order_id"`
	GrossProfit  float64 `json:"gross_profit"`
	NetProfit    float64 `json:"net_profit"`
	Fees         float64 `json:"fees"`
	Status       string  `json:"status"` // pending, open_buy, open, completed, failed, dry_run
	OpenedAt     int64   `json:"opened_at"`
	ClosedAt     int64   `json:"closed_at"`
}

// IsActive returns true if the pair is not yet closed.
func (p *TradePair) IsActive() bool {
	switch p.Status {
	case "pending", "open_buy", "open", "open_sell":
		return true
	}
	return false
}

// ── Performance ─────────────────────────────────────────────────

// TimeValuePoint is a single {time, value} sample used for chart series.
type TimeValuePoint struct {
	Time  int64   `json:"time"`
	Value float64 `json:"value"`
}

// ArbitragePerformance aggregates completed/dry-run arbitrage trades for dashboards.
type ArbitragePerformance struct {
	TotalPnL    float64          `json:"total_pnl"`
	TotalTrades int              `json:"total_trades"`
	WinTrades   int              `json:"win_trades"`
	LossTrades  int              `json:"loss_trades"`
	WinRate     float64          `json:"win_rate"`
	AvgPnL      float64          `json:"avg_pnl"`
	MaxWin      float64          `json:"max_win"`
	MaxLoss     float64          `json:"max_loss"`
	TotalFees   float64          `json:"total_fees"`
	EquityCurve []TimeValuePoint `json:"equity_curve"`
	DailyPnL    []TimeValuePoint `json:"daily_pnl"`
}

// ── Engine Config ───────────────────────────────────────────────

type EngineConfig struct {
	Symbol       string        `json:"symbol"`         // deprecated, kept for backward compat
	Symbols      []string      `json:"symbols"`        // monitored symbols
	MinSpreadPct float64       `json:"min_spread_pct"` // minimum spread to trigger
	OrderSize    float64       `json:"order_size"`     // quote currency amount per leg
	MaxPositions int           `json:"max_positions"`  // max concurrent arbitrage pairs
	FeeA         float64       `json:"fee_a"`          // taker fee exchange A (decimal)
	FeeB         float64       `json:"fee_b"`          // taker fee exchange B (decimal)
	PollInterval time.Duration `json:"poll_interval"`  // legacy, ignored in WS mode
	AutoExecute  bool          `json:"auto_execute"`   // auto-place orders on detection
	DryRun       bool          `json:"dry_run"`        // log only, don't place orders

	// Depth-adaptive sizing
	AdaptiveQtyEnabled bool    `json:"adaptive_qty_enabled"` // auto-shrink qty to fit depth
	MaxSlippagePct     float64 `json:"max_slippage_pct"`     // hard reject if buy/sell slippage exceeds this (%)
	MinOrderQty        float64 `json:"min_order_qty"`        // minimum base qty after adaptation
	MinOrderValue      float64 `json:"min_order_value"`      // minimum quote value after adaptation
}

// SymbolsList returns the list of monitored symbols, falling back to Symbol.
func (c EngineConfig) SymbolsList() []string {
	if len(c.Symbols) > 0 {
		return c.Symbols
	}
	if c.Symbol != "" {
		return []string{c.Symbol}
	}
	return []string{"BTCUSDT"}
}

func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		Symbol:       "BTCUSDT",
		MinSpreadPct: 0.3,
		OrderSize:    500,
		MaxPositions: 3,
		FeeA:         0.001,
		FeeB:         0.001,
		PollInterval: 2 * time.Second,
		AutoExecute:  false,
		DryRun:       true,

		AdaptiveQtyEnabled: false,
		MaxSlippagePct:     0.5,
		MinOrderQty:        0.001,
		MinOrderValue:      10.0,
	}
}

// ── Market State ────────────────────────────────────────────────

type exchangeMarketState struct {
	tick      model.Tick
	orderBook model.OrderBookData
	tickTime  int64
	obTime    int64
}

// ── Engine ─────────────────────────────────────────────────────

// Engine monitors multiple exchanges for arbitrage opportunities.
type Engine struct {
	config    EngineConfig
	clients   map[string]ExchangeClient // exchange name -> client
	positions []*TradePair
	history   []*TradePair
	mu        sync.RWMutex
	seq       int64

	// State
	running bool
	lastOpp *Opportunity

	// Real-time market state
	marketState map[string]map[string]*exchangeMarketState // symbol -> exchange -> state
	stateMu     sync.RWMutex
	tickSubID   event.SubscriptionID
	obSubID     event.SubscriptionID

	// Persistence
	repo *store.ArbitrageTradeRepo

	// Callbacks
	OnOpportunity func(opp Opportunity)
	OnTrade       func(trade TradePair)
	OnError       func(err error)
}

// NewEngine creates an arbitrage engine and restores persisted state.
func NewEngine(cfg EngineConfig) *Engine {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	e := &Engine{
		config:      cfg,
		clients:     make(map[string]ExchangeClient),
		marketState: make(map[string]map[string]*exchangeMarketState),
	}

	if store.GetDB() != nil {
		e.repo = store.GetArbitrageTradeRepo()
		e.restore()
	}

	return e
}

// restore loads active positions and history from the database.
func (e *Engine) restore() {
	if e.repo == nil {
		return
	}

	active, err := e.repo.ListActive()
	if err == nil {
		for _, r := range active {
			e.positions = append(e.positions, recordToPair(r))
		}
	}

	history, err := e.repo.ListHistory(0)
	if err == nil {
		for _, r := range history {
			e.history = append(e.history, recordToPair(r))
		}
	}
}

// RegisterExchange adds an exchange client.
func (e *Engine) RegisterExchange(name string, client ExchangeClient) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clients[name] = client
}

// IterateClients safely iterates registered clients.
func (e *Engine) IterateClients(fn func(name string, client ExchangeClient)) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for name, client := range e.clients {
		fn(name, client)
	}
}

// GetClientCount returns the number of registered exchanges.
func (e *Engine) GetClientCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.clients)
}

// GetConfig returns the engine configuration.
func (e *Engine) GetConfig() EngineConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// SetConfig replaces the engine configuration while preserving clients/history.
func (e *Engine) SetConfig(cfg EngineConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = cfg
}

// Execute manually triggers execution of an opportunity.
func (e *Engine) Execute(opp Opportunity) {
	e.execute(opp)
}

// SetDryRun sets the dry-run mode.
func (e *Engine) SetDryRun(v bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config.DryRun = v
}

// Start begins monitoring via WebSocket event bus.
func (e *Engine) Start() error {
	bus := app.Get().EventBus
	if bus == nil {
		return fmt.Errorf("event bus not initialized")
	}

	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("already running")
	}
	if len(e.clients) < 2 {
		e.mu.Unlock()
		return fmt.Errorf("need at least 2 exchanges, got %d", len(e.clients))
	}
	e.running = true
	e.mu.Unlock()

	// Subscribe to real-time tick and orderbook events for all symbols.
	e.tickSubID = bus.Subscribe("", event.PrioNormal, e.handleTick, event.TypeTick)
	e.obSubID = bus.Subscribe("", event.PrioNormal, e.handleOrderBook, event.TypeOrderBook)

	// Start market streams for each registered exchange.
	symbols := e.GetConfig().SymbolsList()
	e.IterateClients(func(name string, client ExchangeClient) {
		client.WireToEventBus(bus)
		if err := client.StartMarketStream(symbols); err != nil {
			if e.OnError != nil {
				e.OnError(fmt.Errorf("%s stream start failed: %w", name, err))
			}
		}
	})

	return nil
}

// Stop halts monitoring.
func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	e.mu.Unlock()

	if bus := app.Get().EventBus; bus != nil {
		if e.tickSubID != 0 {
			bus.Unsubscribe(e.tickSubID)
			e.tickSubID = 0
		}
		if e.obSubID != 0 {
			bus.Unsubscribe(e.obSubID)
			e.obSubID = 0
		}
	}

	e.IterateClients(func(name string, client ExchangeClient) {
		_ = client.StopStream()
	})

	e.stateMu.Lock()
	e.marketState = make(map[string]map[string]*exchangeMarketState)
	e.stateMu.Unlock()
}

// IsRunning returns true if the engine is active.
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// GetLastOpportunity returns the most recent opportunity.
func (e *Engine) GetLastOpportunity() *Opportunity {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.lastOpp == nil {
		return nil
	}
	cp := *e.lastOpp
	return &cp
}

// GetPositions returns active trade pairs.
func (e *Engine) GetPositions() []*TradePair {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*TradePair, len(e.positions))
	copy(result, e.positions)
	return result
}

// GetHistory returns completed trade pairs.
func (e *Engine) GetHistory(limit int) []*TradePair {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if limit <= 0 || limit > len(e.history) {
		limit = len(e.history)
	}
	start := len(e.history) - limit
	if start < 0 {
		start = 0
	}
	result := make([]*TradePair, limit)
	copy(result, e.history[start:])
	return result
}

// GetStats returns engine statistics.
func (e *Engine) GetStats() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var totalProfit, totalFees float64
	var wins, losses int
	for _, t := range e.history {
		totalProfit += t.NetProfit
		totalFees += t.Fees
		if t.NetProfit > 0 {
			wins++
		} else {
			losses++
		}
	}

	return map[string]any{
		"running":         e.running,
		"exchanges":       len(e.clients),
		"active_pairs":    len(e.positions),
		"total_trades":    len(e.history),
		"total_profit":    round(totalProfit, 4),
		"total_fees":      round(totalFees, 4),
		"win_count":       wins,
		"loss_count":      losses,
		"win_rate":        winRate(wins, losses),
		"last_spread_pct": func() float64 {
			if e.lastOpp != nil {
				return e.lastOpp.SpreadPct
			}
			return 0
		}(),
	}
}

// GetPerformance returns aggregated arbitrage performance metrics and chart series.
func (e *Engine) GetPerformance() ArbitragePerformance {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var totalPnL, totalFees, maxWin, maxLoss float64
	var wins, losses int
	type point struct {
		ts  int64
		pnl float64
	}
	points := make([]point, 0, len(e.history))

	for _, t := range e.history {
		if t.Status != "completed" && t.Status != "dry_run" {
			continue
		}
		totalPnL += t.NetProfit
		totalFees += t.Fees
		if t.NetProfit > 0 {
			wins++
		} else {
			losses++
		}
		if t.NetProfit > maxWin {
			maxWin = t.NetProfit
		}
		if t.NetProfit < maxLoss {
			maxLoss = t.NetProfit
		}
		ts := t.ClosedAt
		if ts <= 0 {
			ts = t.OpenedAt
		}
		points = append(points, point{ts: ts, pnl: t.NetProfit})
	}

	// Sort by closing/opening time so the equity curve is chronological.
	sort.Slice(points, func(i, j int) bool {
		return points[i].ts < points[j].ts
	})

	equityCurve := make([]TimeValuePoint, 0, len(points))
	dailyMap := make(map[string]float64)
	dailyTs := make(map[string]int64)
	var cumulative float64
	for _, p := range points {
		cumulative += p.pnl
		// Use seconds for frontend consistency with existing dashboard series.
		sec := p.ts / 1000
		equityCurve = append(equityCurve, TimeValuePoint{Time: sec, Value: round(cumulative, 4)})

		day := time.Unix(sec, 0).Format("2006-01-02")
		dailyMap[day] += p.pnl
		if _, ok := dailyTs[day]; !ok {
			dailyTs[day] = sec
		}
	}

	dailyPnL := make([]TimeValuePoint, 0, len(dailyMap))
	for day, pnl := range dailyMap {
		dailyPnL = append(dailyPnL, TimeValuePoint{Time: dailyTs[day], Value: round(pnl, 4)})
	}
	sort.Slice(dailyPnL, func(i, j int) bool {
		return dailyPnL[i].Time < dailyPnL[j].Time
	})

	totalTrades := wins + losses
	avgPnL := 0.0
	if totalTrades > 0 {
		avgPnL = totalPnL / float64(totalTrades)
	}

	return ArbitragePerformance{
		TotalPnL:    round(totalPnL, 4),
		TotalTrades: totalTrades,
		WinTrades:   wins,
		LossTrades:  losses,
		WinRate:     winRate(wins, losses),
		AvgPnL:      round(avgPnL, 4),
		MaxWin:      round(maxWin, 4),
		MaxLoss:     round(maxLoss, 4),
		TotalFees:   round(totalFees, 4),
		EquityCurve: equityCurve,
		DailyPnL:    dailyPnL,
	}
}

// ClosePosition manually closes an active arbitrage position.
func (e *Engine) ClosePosition(id string, sellPrice float64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var pair *TradePair
	for _, p := range e.positions {
		if p.ID == id {
			pair = p
			break
		}
	}
	if pair == nil {
		return fmt.Errorf("position not found: %s", id)
	}
	if !pair.IsActive() {
		return fmt.Errorf("position %s is not active", id)
	}

	if sellPrice > 0 {
		pair.SellPrice = sellPrice
	}
	if pair.SellPrice <= 0 {
		return fmt.Errorf("position %s has no sell price", id)
	}

	pair.Status = "completed"
	pair.ClosedAt = time.Now().UnixMilli()
	pair.GrossProfit = (pair.SellPrice - pair.BuyPrice) * pair.Quantity
	pair.Fees = (pair.BuyPrice*pair.Quantity*e.config.FeeA) + (pair.SellPrice*pair.Quantity*e.config.FeeB)
	pair.NetProfit = pair.GrossProfit - pair.Fees

	e.recordPairLocked(pair)
	if e.OnTrade != nil {
		e.OnTrade(*pair)
	}
	return nil
}

// FailPosition marks an active arbitrage position as failed.
func (e *Engine) FailPosition(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var pair *TradePair
	for _, p := range e.positions {
		if p.ID == id {
			pair = p
			break
		}
	}
	if pair == nil {
		return fmt.Errorf("position not found: %s", id)
	}
	if !pair.IsActive() {
		return fmt.Errorf("position %s is not active", id)
	}

	pair.Status = "failed"
	pair.ClosedAt = time.Now().UnixMilli()
	e.recordPairLocked(pair)
	return nil
}

// ── Event Handlers ──────────────────────────────────────────────

func (e *Engine) handleTick(evt event.Event) {
	tick, ok := evt.Data.(model.Tick)
	if !ok {
		return
	}
	if !e.isWatchedSymbol(tick.Symbol) || tick.Exchange == "" {
		return
	}

	e.stateMu.Lock()
	symMap := e.marketState[tick.Symbol]
	if symMap == nil {
		symMap = make(map[string]*exchangeMarketState)
		e.marketState[tick.Symbol] = symMap
	}
	state, ok := symMap[tick.Exchange]
	if !ok {
		state = &exchangeMarketState{}
		symMap[tick.Exchange] = state
	}
	state.tick = tick
	state.tickTime = time.Now().UnixMilli()
	e.stateMu.Unlock()

	e.evaluate()
}

func (e *Engine) handleOrderBook(evt event.Event) {
	ob, ok := evt.Data.(model.OrderBookData)
	if !ok {
		return
	}
	if !e.isWatchedSymbol(ob.Symbol) || ob.Exchange == "" {
		return
	}

	e.stateMu.Lock()
	symMap := e.marketState[ob.Symbol]
	if symMap == nil {
		symMap = make(map[string]*exchangeMarketState)
		e.marketState[ob.Symbol] = symMap
	}
	state, ok := symMap[ob.Exchange]
	if !ok {
		state = &exchangeMarketState{}
		symMap[ob.Exchange] = state
	}
	state.orderBook = ob
	state.obTime = time.Now().UnixMilli()
	e.stateMu.Unlock()

	e.evaluate()
}

func (e *Engine) isWatchedSymbol(symbol string) bool {
	for _, s := range e.GetConfig().SymbolsList() {
		if strings.EqualFold(s, symbol) {
			return true
		}
	}
	return false
}

func (e *Engine) evaluate() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return
	}

	opp := e.findBestOpportunityLocked()
	if opp == nil {
		return
	}
	e.lastOpp = opp

	if e.OnOpportunity != nil {
		e.OnOpportunity(*opp)
	}

	if e.config.AutoExecute && opp.IsProfitable(e.config.MinSpreadPct, e.config.FeeA, e.config.FeeB) {
		if len(e.positions) < e.config.MaxPositions {
			go e.execute(*opp)
		}
	}
}

// findBestOpportunityLocked scans market state for the best viable arbitrage opportunity.
// Caller must hold e.mu.
func (e *Engine) findBestOpportunityLocked() *Opportunity {
	e.stateMu.RLock()
	defer e.stateMu.RUnlock()

	symbols := e.config.SymbolsList()
	var best *Opportunity

	for _, symbol := range symbols {
		symMap := e.marketState[symbol]
		if len(symMap) < 2 {
			continue
		}

		type snap struct {
			name string
			ob   model.OrderBookData
			tick model.Tick
		}
		var snaps []snap
		for name, state := range symMap {
			if state.obTime == 0 || len(state.orderBook.Asks) == 0 || len(state.orderBook.Bids) == 0 {
				continue
			}
			snaps = append(snaps, snap{name: name, ob: state.orderBook, tick: state.tick})
		}
		if len(snaps) < 2 {
			continue
		}

		for i, a := range snaps {
			for j, b := range snaps {
				if i >= j {
					continue
				}

				var buyEx, sellEx string
				var buySnap, sellSnap snap

				if a.tick.Ask < b.tick.Bid {
					buyEx, sellEx = a.name, b.name
					buySnap, sellSnap = a, b
				} else if b.tick.Ask < a.tick.Bid {
					buyEx, sellEx = b.name, a.name
					buySnap, sellSnap = b, a
				} else {
					continue
				}

				buyPrice := buySnap.tick.Ask
				sellPrice := sellSnap.tick.Bid
				if buyPrice <= 0 || sellPrice <= 0 {
					continue
				}

				targetQty := e.config.OrderSize / buyPrice
				execQty, execBuy, execSell, buyDepth, sellDepth, slipBuy, slipSell, maxQty, viable :=
					resolveQuantity(buySnap.ob, sellSnap.ob, targetQty, buyPrice, e.config)

				if !viable || execBuy <= 0 || execSell <= 0 || execQty <= 0 {
					continue
				}

				netSpread := (execSell-execBuy)/execBuy*100 - (e.config.FeeA+e.config.FeeB)*100
				if netSpread < e.config.MinSpreadPct {
					continue
				}

				spreadPct := (execSell - execBuy) / execBuy * 100
				if best == nil || netSpread > best.NetSpreadPct(e.config.FeeA, e.config.FeeB) {
					best = &Opportunity{
						Symbol:              symbol,
						BuyExchange:         buyEx,
						SellExchange:        sellEx,
						BuyPrice:            buyPrice,
						SellPrice:           sellPrice,
						SpreadPct:           spreadPct,
						SpreadAbs:           execSell - execBuy,
						ExecutableBuyPrice:  execBuy,
						ExecutableSellPrice: execSell,
						BuyDepthQty:         buyDepth,
						SellDepthQty:        sellDepth,
						SlippageBuyPct:      slipBuy,
						SlippageSellPct:     slipSell,
						MaxExecutableQty:    maxQty,
						AdjustedQty:         execQty,
						Viable:              true,
						Timestamp:           time.Now().UnixMilli(),
					}
				}
			}
		}
	}

	return best
}

// depthMetrics holds the result of walking order books for a specific quantity.
type depthMetrics struct {
	execBuy      float64
	execSell     float64
	buyDepth     float64
	sellDepth    float64
	slippageBuy  float64
	slippageSell float64
	buyFilled    float64
	sellFilled   float64
}

// computeDepthMetrics walks the order books for exactly qty and returns detailed metrics.
// Unlike calculateDepthMetrics, it does not enforce full fill and always returns filled amounts.
func computeDepthMetrics(buyOB, sellOB model.OrderBookData, qty float64) depthMetrics {
	if qty <= 0 {
		return depthMetrics{}
	}
	var m depthMetrics

	var buyCost float64
	for _, ask := range buyOB.Asks {
		price, avail := ask[0], ask[1]
		if avail <= 0 {
			continue
		}
		m.buyDepth += avail
		if m.buyFilled >= qty {
			continue
		}
		take := avail
		if m.buyFilled+take > qty {
			take = qty - m.buyFilled
		}
		buyCost += price * take
		m.buyFilled += take
	}
	if m.buyFilled > 0 {
		m.execBuy = buyCost / m.buyFilled
	}

	var sellProceeds float64
	for _, bid := range sellOB.Bids {
		price, avail := bid[0], bid[1]
		if avail <= 0 {
			continue
		}
		m.sellDepth += avail
		if m.sellFilled >= qty {
			continue
		}
		take := avail
		if m.sellFilled+take > qty {
			take = qty - m.sellFilled
		}
		sellProceeds += price * take
		m.sellFilled += take
	}
	if m.sellFilled > 0 {
		m.execSell = sellProceeds / m.sellFilled
	}

	if len(buyOB.Asks) > 0 && buyOB.Asks[0][0] > 0 {
		m.slippageBuy = (m.execBuy - buyOB.Asks[0][0]) / buyOB.Asks[0][0] * 100
	}
	if len(sellOB.Bids) > 0 && sellOB.Bids[0][0] > 0 {
		m.slippageSell = (sellOB.Bids[0][0] - m.execSell) / sellOB.Bids[0][0] * 100
	}
	return m
}

// calculateDepthMetrics walks the order book to compute executable prices.
// It returns viable=true only when the full targetBaseQty can be filled on both sides.
func calculateDepthMetrics(buyOB, sellOB model.OrderBookData, targetBaseQty float64) (
	execBuy, execSell, buyDepth, sellDepth, slippageBuy, slippageSell, maxQty float64, viable bool,
) {
	m := computeDepthMetrics(buyOB, sellOB, targetBaseQty)
	execBuy, execSell, buyDepth, sellDepth, slippageBuy, slippageSell =
		m.execBuy, m.execSell, m.buyDepth, m.sellDepth, m.slippageBuy, m.slippageSell
	maxQty = m.buyFilled
	if m.sellFilled < maxQty {
		maxQty = m.sellFilled
	}
	if m.buyFilled >= targetBaseQty && m.sellFilled >= targetBaseQty {
		viable = true
	}
	return
}

// resolveQuantity decides the final executable quantity and its metrics.
// When AdaptiveQtyEnabled is true it shrinks qty to fit depth and slippage limits.
// When false it acts as a hard gate: full fill + slippage limit required.
func resolveQuantity(buyOB, sellOB model.OrderBookData, targetQty, buyPrice float64, cfg EngineConfig) (
	execQty, execBuy, execSell, buyDepth, sellDepth, slipBuy, slipSell, maxQty float64, viable bool,
) {
	m := computeDepthMetrics(buyOB, sellOB, targetQty)
	maxQty = m.buyFilled
	if m.sellFilled < maxQty {
		maxQty = m.sellFilled
	}

	if !cfg.AdaptiveQtyEnabled {
		execQty = targetQty
		if m.buyFilled < targetQty || m.sellFilled < targetQty {
			return
		}
		if cfg.MaxSlippagePct > 0 && (m.slippageBuy > cfg.MaxSlippagePct || m.slippageSell > cfg.MaxSlippagePct) {
			return
		}
		return execQty, m.execBuy, m.execSell, m.buyDepth, m.sellDepth, m.slippageBuy, m.slippageSell, maxQty, true
	}

	// Adaptive path: start from the largest qty the combined depth allows.
	execQty = targetQty
	if maxQty > 0 && maxQty < execQty {
		execQty = maxQty
	}

	// Back off if slippage is still too high for the chosen qty.
	for i := 0; i < 50; i++ {
		if execQty <= 0 {
			return
		}
		if cfg.MinOrderQty > 0 && execQty < cfg.MinOrderQty {
			return
		}
		m = computeDepthMetrics(buyOB, sellOB, execQty)
		if cfg.MaxSlippagePct <= 0 || (m.slippageBuy <= cfg.MaxSlippagePct && m.slippageSell <= cfg.MaxSlippagePct) {
			break
		}
		execQty *= 0.9
	}

	if execQty <= 0 {
		return
	}
	if cfg.MinOrderQty > 0 && execQty < cfg.MinOrderQty {
		return
	}
	if cfg.MinOrderValue > 0 && execQty*buyPrice < cfg.MinOrderValue {
		return
	}

	// Recompute final metrics at the chosen qty.
	m = computeDepthMetrics(buyOB, sellOB, execQty)
	return execQty, m.execBuy, m.execSell, m.buyDepth, m.sellDepth, m.slippageBuy, m.slippageSell, maxQty, true
}

// execute places the buy and sell orders for an arbitrage opportunity.
func (e *Engine) execute(opp Opportunity) {
	e.mu.Lock()
	if len(e.positions) >= e.config.MaxPositions {
		e.mu.Unlock()
		return
	}
	e.seq++
	pairID := fmt.Sprintf("arb-%d-%d", time.Now().UnixMilli(), e.seq)
	e.mu.Unlock()

	pair := &TradePair{
		ID:           pairID,
		Symbol:       opp.Symbol,
		BuyExchange:  opp.BuyExchange,
		SellExchange: opp.SellExchange,
		BuyPrice:     opp.BuyPrice,
		SellPrice:    opp.SellPrice,
		Status:       "pending",
		OpenedAt:     time.Now().UnixMilli(),
	}

	// Balance check before calculating quantity.
	if err := e.checkBalance(opp); err != nil {
		pair.Status = "failed"
		pair.ClosedAt = time.Now().UnixMilli()
		e.recordPair(pair)
		if e.OnError != nil {
			e.OnError(fmt.Errorf("balance check failed for %s: %w", pairID, err))
		}
		return
	}

	buyPrice := opp.BuyPrice
	sellPrice := opp.SellPrice
	if opp.ExecutableBuyPrice > 0 {
		buyPrice = opp.ExecutableBuyPrice
	}
	if opp.ExecutableSellPrice > 0 {
		sellPrice = opp.ExecutableSellPrice
	}

	qty := opp.AdjustedQty
	if qty <= 0 {
		qty = e.config.OrderSize / buyPrice
	}
	qty = math.Floor(qty*1e6) / 1e6
	pair.Quantity = qty

	// Persist pending state.
	e.recordPair(pair)

	if e.config.DryRun {
		pair.Status = "dry_run"
		pair.GrossProfit = (sellPrice - buyPrice) * qty
		pair.Fees = (buyPrice*qty*e.config.FeeA) + (sellPrice*qty*e.config.FeeB)
		pair.NetProfit = pair.GrossProfit - pair.Fees
		pair.ClosedAt = time.Now().UnixMilli()
		e.recordPair(pair)
		if e.OnTrade != nil {
			e.OnTrade(*pair)
		}
		return
	}

	buyClient := e.clients[opp.BuyExchange]
	sellClient := e.clients[opp.SellExchange]

	if buyClient == nil || sellClient == nil {
		pair.Status = "failed"
		pair.ClosedAt = time.Now().UnixMilli()
		e.recordPair(pair)
		if e.OnError != nil {
			e.OnError(fmt.Errorf("exchange client missing for %s", pairID))
		}
		return
	}

	// Buy on cheaper exchange.
	buyResult, err := buyClient.PlaceOrder(opp.Symbol, "BUY", "MARKET", 0, qty)
	if err != nil {
		pair.Status = "failed"
		pair.ClosedAt = time.Now().UnixMilli()
		e.recordPair(pair)
		if e.OnError != nil {
			e.OnError(fmt.Errorf("buy order failed: %w", err))
		}
		return
	}
	if id, ok := buyResult["order_id"].(string); ok {
		pair.BuyOrderID = id
	} else if id, ok := buyResult["id"].(string); ok {
		pair.BuyOrderID = id
	}
	pair.Status = "open_buy"
	e.recordPair(pair)

	// Sell on expensive exchange.
	sellResult, err := sellClient.PlaceOrder(opp.Symbol, "SELL", "MARKET", 0, qty)
	if err != nil {
		// Buy is filled but sell failed; position stays open for manual resolution.
		pair.Status = "open"
		e.recordPair(pair)
		if e.OnError != nil {
			e.OnError(fmt.Errorf("sell order failed (position %s now open): %w", pairID, err))
		}
		return
	}
	if id, ok := sellResult["order_id"].(string); ok {
		pair.SellOrderID = id
	} else if id, ok := sellResult["id"].(string); ok {
		pair.SellOrderID = id
	}

	pair.Status = "completed"
	pair.ClosedAt = time.Now().UnixMilli()
	pair.GrossProfit = (sellPrice - buyPrice) * qty
	pair.Fees = (buyPrice*qty*e.config.FeeA) + (sellPrice*qty*e.config.FeeB)
	pair.NetProfit = pair.GrossProfit - pair.Fees

	e.recordPair(pair)
	if e.OnTrade != nil {
		e.OnTrade(*pair)
	}
}

// checkBalance verifies that both exchanges hold sufficient funds.
func (e *Engine) checkBalance(opp Opportunity) error {
	buyClient := e.clients[opp.BuyExchange]
	sellClient := e.clients[opp.SellExchange]
	if buyClient == nil || sellClient == nil {
		return fmt.Errorf("missing exchange client")
	}

	base, quote := splitSymbol(opp.Symbol)
	if base == "" || quote == "" {
		return fmt.Errorf("unable to parse symbol %s", opp.Symbol)
	}

	buyPrice := opp.BuyPrice
	if opp.ExecutableBuyPrice > 0 {
		buyPrice = opp.ExecutableBuyPrice
	}
	qty := opp.AdjustedQty
	if qty <= 0 {
		qty = e.config.OrderSize / buyPrice
	}
	qty = math.Floor(qty*1e6) / 1e6
	requiredQuote := buyPrice * qty

	buyBalances, err := buyClient.GetBalance()
	if err != nil {
		return fmt.Errorf("buy exchange %s balance fetch failed: %w", opp.BuyExchange, err)
	}
	quoteFree := extractFreeBalance(buyBalances, quote)
	if quoteFree < requiredQuote {
		return fmt.Errorf("insufficient %s on %s: need %.4f, have %.4f", quote, opp.BuyExchange, requiredQuote, quoteFree)
	}

	sellBalances, err := sellClient.GetBalance()
	if err != nil {
		return fmt.Errorf("sell exchange %s balance fetch failed: %w", opp.SellExchange, err)
	}
	baseFree := extractFreeBalance(sellBalances, base)
	if baseFree < qty {
		return fmt.Errorf("insufficient %s on %s: need %.6f, have %.6f", base, opp.SellExchange, qty, baseFree)
	}

	return nil
}

func (e *Engine) recordPair(pair *TradePair) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recordPairLocked(pair)
}

func (e *Engine) recordPairLocked(pair *TradePair) {
	if pair.IsActive() {
		found := false
		for _, p := range e.positions {
			if p.ID == pair.ID {
				found = true
				break
			}
		}
		if !found {
			e.positions = append(e.positions, pair)
		}
	}

	if pair.Status == "completed" || pair.Status == "failed" || pair.Status == "dry_run" {
		e.history = append(e.history, pair)
		var active []*TradePair
		for _, p := range e.positions {
			if p.ID != pair.ID {
				active = append(active, p)
			}
		}
		e.positions = active
	}

	e.persistPair(pair)
}

func (e *Engine) persistPair(pair *TradePair) {
	if e.repo == nil {
		return
	}
	rec := pairToRecord(pair)
	if _, err := e.repo.GetByID(rec.ID); err == nil {
		_ = e.repo.Update(rec)
	} else {
		_ = e.repo.Create(rec)
	}
}

// ── Helpers ─────────────────────────────────────────────────────

func extractPrice(ticker map[string]any) float64 {
	for _, key := range []string{"lastPrice", "last", "price", "close", "last_price", "c"} {
		if v, ok := ticker[key]; ok {
			return toFloat(v)
		}
	}
	return 0
}

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	}
	return 0
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func winRate(wins, losses int) float64 {
	total := wins + losses
	if total == 0 {
		return 0
	}
	return float64(wins) / float64(total) * 100
}

// splitSymbol extracts base and quote assets from a trading pair.
func splitSymbol(symbol string) (base, quote string) {
	quotes := []string{"USDT", "USDC", "BUSD", "TUSD", "BTC", "ETH", "USD"}
	for _, q := range quotes {
		if strings.HasSuffix(symbol, q) {
			return strings.TrimSuffix(symbol, q), q
		}
	}
	return "", ""
}

// extractFreeBalance tries common balance field names used by exchanges.
func extractFreeBalance(balances []map[string]any, asset string) float64 {
	assetKeys := []string{"asset", "ccy", "coin", "currency"}
	freeKeys := []string{"free", "availEq", "cashBal", "walletBalance", "available"}

	for _, bal := range balances {
		var balAsset string
		for _, k := range assetKeys {
			if v, ok := bal[k].(string); ok && strings.EqualFold(v, asset) {
				balAsset = v
				break
			}
		}
		if balAsset == "" {
			continue
		}

		for _, k := range freeKeys {
			if v, ok := bal[k]; ok {
				free := toFloat(v)
				if free > 0 {
					return free
				}
			}
		}
	}
	return 0
}

func pairToRecord(pair *TradePair) *store.ArbitrageTradeRecord {
	return &store.ArbitrageTradeRecord{
		ID:           pair.ID,
		Symbol:       pair.Symbol,
		BuyExchange:  pair.BuyExchange,
		SellExchange: pair.SellExchange,
		BuyPrice:     pair.BuyPrice,
		SellPrice:    pair.SellPrice,
		Quantity:     pair.Quantity,
		BuyOrderID:   pair.BuyOrderID,
		SellOrderID:  pair.SellOrderID,
		GrossProfit:  pair.GrossProfit,
		NetProfit:    pair.NetProfit,
		Fees:         pair.Fees,
		Status:       pair.Status,
		OpenedAt:     pair.OpenedAt,
		ClosedAt:     pair.ClosedAt,
	}
}

func recordToPair(r *store.ArbitrageTradeRecord) *TradePair {
	return &TradePair{
		ID:           r.ID,
		Symbol:       r.Symbol,
		BuyExchange:  r.BuyExchange,
		SellExchange: r.SellExchange,
		BuyPrice:     r.BuyPrice,
		SellPrice:    r.SellPrice,
		Quantity:     r.Quantity,
		BuyOrderID:   r.BuyOrderID,
		SellOrderID:  r.SellOrderID,
		GrossProfit:  r.GrossProfit,
		NetProfit:    r.NetProfit,
		Fees:         r.Fees,
		Status:       r.Status,
		OpenedAt:     r.OpenedAt,
		ClosedAt:     r.ClosedAt,
	}
}

// ── Adapter Wrappers ────────────────────────────────────────────

// BinanceClient wraps BinanceAdapter for the ExchangeClient interface.
type BinanceClient struct {
	*adapter.BinanceAdapter
}

func (b *BinanceClient) Name() string { return "binance" }
func (b *BinanceClient) StartMarketStream(symbols []string) error { return b.BinanceAdapter.StartMarketStream(symbols) }
func (b *BinanceClient) StopStream() error                       { return b.BinanceAdapter.Stop() }
func (b *BinanceClient) WireToEventBus(bus *event.EventBus) {
	b.OnTicker(func(tick model.Tick) {
		tick.Exchange = b.Name()
		bus.Publish(event.Event{Type: event.TypeTick, Symbol: tick.Symbol, Data: tick})
	})
	b.OnOrderBook(func(ob model.OrderBookData) {
		ob.Exchange = b.Name()
		bus.Publish(event.Event{Type: event.TypeOrderBook, Symbol: ob.Symbol, Data: ob})
	})
}

// OKXClient wraps OKXAdapter for the ExchangeClient interface.
type OKXClient struct {
	*adapter.OKXAdapter
}

func (o *OKXClient) Name() string { return "okx" }
func (o *OKXClient) StartMarketStream(symbols []string) error { return o.OKXAdapter.StartMarketStream(symbols) }
func (o *OKXClient) StopStream() error                       { return o.OKXAdapter.Stop() }
func (o *OKXClient) WireToEventBus(bus *event.EventBus) {
	o.OnTicker(func(tick model.Tick) {
		tick.Exchange = o.Name()
		bus.Publish(event.Event{Type: event.TypeTick, Symbol: tick.Symbol, Data: tick})
	})
	o.OnOrderBook(func(ob model.OrderBookData) {
		ob.Exchange = o.Name()
		bus.Publish(event.Event{Type: event.TypeOrderBook, Symbol: ob.Symbol, Data: ob})
	})
}

// MEXCClient wraps MEXCAdapter for the ExchangeClient interface.
type MEXCClient struct {
	*adapter.MEXCAdapter
}

func (m *MEXCClient) Name() string { return "mexc" }
func (m *MEXCClient) StartMarketStream(symbols []string) error { return m.MEXCAdapter.StartMarketStream(symbols) }
func (m *MEXCClient) StopStream() error                       { return m.MEXCAdapter.Stop() }
func (m *MEXCClient) WireToEventBus(bus *event.EventBus) {
	m.OnTicker(func(tick model.Tick) {
		tick.Exchange = m.Name()
		bus.Publish(event.Event{Type: event.TypeTick, Symbol: tick.Symbol, Data: tick})
	})
	m.OnOrderBook(func(ob model.OrderBookData) {
		ob.Exchange = m.Name()
		bus.Publish(event.Event{Type: event.TypeOrderBook, Symbol: ob.Symbol, Data: ob})
	})
}

// GateIOClient wraps GateIOAdapter for the ExchangeClient interface.
type GateIOClient struct {
	*adapter.GateIOAdapter
}

func (g *GateIOClient) Name() string { return "gateio" }
func (g *GateIOClient) StartMarketStream(symbols []string) error { return g.GateIOAdapter.StartMarketStream(symbols) }
func (g *GateIOClient) StopStream() error                       { return g.GateIOAdapter.Stop() }
func (g *GateIOClient) WireToEventBus(bus *event.EventBus) {
	g.OnTicker(func(tick model.Tick) {
		tick.Exchange = g.Name()
		bus.Publish(event.Event{Type: event.TypeTick, Symbol: tick.Symbol, Data: tick})
	})
	g.OnOrderBook(func(ob model.OrderBookData) {
		ob.Exchange = g.Name()
		bus.Publish(event.Event{Type: event.TypeOrderBook, Symbol: ob.Symbol, Data: ob})
	})
}

// BybitClient wraps BybitAdapter for the ExchangeClient interface.
type BybitClient struct {
	*adapter.BybitAdapter
}

func (b *BybitClient) Name() string { return "bybit" }
func (b *BybitClient) StartMarketStream(symbols []string) error { return b.BybitAdapter.StartMarketStream(symbols) }
func (b *BybitClient) StopStream() error                       { return b.BybitAdapter.Stop() }
func (b *BybitClient) WireToEventBus(bus *event.EventBus) {
	b.OnTicker(func(tick model.Tick) {
		tick.Exchange = b.Name()
		bus.Publish(event.Event{Type: event.TypeTick, Symbol: tick.Symbol, Data: tick})
	})
	b.OnOrderBook(func(ob model.OrderBookData) {
		ob.Exchange = b.Name()
		bus.Publish(event.Event{Type: event.TypeOrderBook, Symbol: ob.Symbol, Data: ob})
	})
}

// CoinbaseClient wraps CoinbaseAdapter for the ExchangeClient interface.
type CoinbaseClient struct {
	*adapter.CoinbaseAdapter
}

func (c *CoinbaseClient) Name() string { return "coinbase" }
func (c *CoinbaseClient) StartMarketStream(symbols []string) error { return c.CoinbaseAdapter.StartMarketStream(symbols) }
func (c *CoinbaseClient) StopStream() error                       { return c.CoinbaseAdapter.Stop() }
func (c *CoinbaseClient) WireToEventBus(bus *event.EventBus) {
	c.OnTicker(func(tick model.Tick) {
		tick.Exchange = c.Name()
		bus.Publish(event.Event{Type: event.TypeTick, Symbol: tick.Symbol, Data: tick})
	})
	c.OnOrderBook(func(ob model.OrderBookData) {
		ob.Exchange = c.Name()
		bus.Publish(event.Event{Type: event.TypeOrderBook, Symbol: ob.Symbol, Data: ob})
	})
}

// KrakenClient wraps KrakenAdapter for the ExchangeClient interface.
type KrakenClient struct {
	*adapter.KrakenAdapter
}

func (k *KrakenClient) Name() string { return "kraken" }
func (k *KrakenClient) StartMarketStream(symbols []string) error { return k.KrakenAdapter.StartMarketStream(symbols) }
func (k *KrakenClient) StopStream() error                       { return k.KrakenAdapter.Stop() }
func (k *KrakenClient) WireToEventBus(bus *event.EventBus) {
	k.OnTicker(func(tick model.Tick) {
		tick.Exchange = k.Name()
		bus.Publish(event.Event{Type: event.TypeTick, Symbol: tick.Symbol, Data: tick})
	})
	k.OnOrderBook(func(ob model.OrderBookData) {
		ob.Exchange = k.Name()
		bus.Publish(event.Event{Type: event.TypeOrderBook, Symbol: ob.Symbol, Data: ob})
	})
}

// BitgetClient wraps BitgetAdapter for the ExchangeClient interface.
type BitgetClient struct {
	*adapter.BitgetAdapter
}

func (b *BitgetClient) Name() string { return "bitget" }
func (b *BitgetClient) StartMarketStream(symbols []string) error { return b.BitgetAdapter.StartMarketStream(symbols) }
func (b *BitgetClient) StopStream() error                       { return b.BitgetAdapter.Stop() }
func (b *BitgetClient) WireToEventBus(bus *event.EventBus) {
	b.OnTicker(func(tick model.Tick) {
		tick.Exchange = b.Name()
		bus.Publish(event.Event{Type: event.TypeTick, Symbol: tick.Symbol, Data: tick})
	})
	b.OnOrderBook(func(ob model.OrderBookData) {
		ob.Exchange = b.Name()
		bus.Publish(event.Event{Type: event.TypeOrderBook, Symbol: ob.Symbol, Data: ob})
	})
}
