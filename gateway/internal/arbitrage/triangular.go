package arbitrage

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Triangular Engine Config ────────────────────────────────────

// TriangularEngineConfig controls single-exchange triangular arbitrage.
type TriangularEngineConfig struct {
	Exchange           string   `json:"exchange"`
	Symbols            []string `json:"symbols"`              // monitored symbols, e.g. BTCUSDT, ETHUSDT, ETHBTC
	QuoteAsset         string   `json:"quote_asset"`          // starting asset for cycles, e.g. USDT
	MinProfitPct       float64  `json:"min_profit_pct"`       // minimum net profit % to trigger
	OrderSize          float64  `json:"order_size"`           // starting quote amount per cycle
	MaxPositions       int      `json:"max_positions"`        // max concurrent cycles
	FeeRate            float64  `json:"fee_rate"`             // taker fee (decimal)
	AutoExecute        bool     `json:"auto_execute"`         // auto-place orders on detection
	DryRun             bool     `json:"dry_run"`              // log only
	AdaptiveQtyEnabled bool     `json:"adaptive_qty_enabled"` // shrink qty to fit depth
	MaxSlippagePct     float64  `json:"max_slippage_pct"`     // hard reject if any leg slippage exceeds this
	MinOrderQty        float64  `json:"min_order_qty"`        // minimum quantity for any leg
	ExecutionMode      string   `json:"execution_mode"`       // "sequential" (MVP)
	MaxExecutionMs     int      `json:"max_execution_ms"`     // timeout for full cycle
}

// DefaultTriangularEngineConfig returns sensible defaults.
func DefaultTriangularEngineConfig() TriangularEngineConfig {
	return TriangularEngineConfig{
		Symbols:            []string{"BTCUSDT", "ETHUSDT", "ETHBTC"},
		QuoteAsset:         "USDT",
		MinProfitPct:       0.3,
		OrderSize:          500,
		MaxPositions:       2,
		FeeRate:            0.001,
		AutoExecute:        false,
		DryRun:             true,
		AdaptiveQtyEnabled: false,
		MaxSlippagePct:     0.5,
		MinOrderQty:        0.001,
		ExecutionMode:      "sequential",
		MaxExecutionMs:     5000,
	}
}

// ── Triangular Leg ──────────────────────────────────────────────

// TriangularLeg represents one side of a 3-cycle.
type TriangularLeg struct {
	Symbol          string  `json:"symbol"`
	Side            string  `json:"side"`             // BUY or SELL
	OrderType       string  `json:"order_type"`       // MARKET
	Price           float64 `json:"price"`            // best ask/bid at detection
	ExecutablePrice float64 `json:"executable_price"` // depth-weighted average fill
	Quantity        float64 `json:"quantity"`         // planned quantity
	FilledQty       float64 `json:"filled_qty"`       // actual filled quantity
	OrderID         string  `json:"order_id"`
	Status          string  `json:"status"` // pending, filled, partial, failed
	Fee             float64 `json:"fee"`
	SlippagePct     float64 `json:"slippage_pct"`
}

// ── Triangular Opportunity ──────────────────────────────────────

// TriangularOpportunity represents a detected 3-cycle arbitrage.
type TriangularOpportunity struct {
	ID           string          `json:"id"`
	Exchange     string          `json:"exchange"`
	Cycle        []string        `json:"cycle"` // asset path, e.g. ["USDT","BTC","ETH","USDT"]
	Legs         []TriangularLeg `json:"legs"`
	StartAsset   string          `json:"start_asset"`
	StartQty     float64         `json:"start_qty"`
	EndQty       float64         `json:"end_qty"`
	GrossProfit  float64         `json:"gross_profit"`
	NetProfit    float64         `json:"net_profit"`
	NetProfitPct float64         `json:"net_profit_pct"`
	TotalFees    float64         `json:"total_fees"`
	Viable       bool            `json:"viable"`
	Timestamp    int64           `json:"timestamp"`
}

// ── Triangular Trade ────────────────────────────────────────────

// TriangularTrade tracks an executed or simulated 3-leg cycle.
type TriangularTrade struct {
	ID          string          `json:"id"`
	Exchange    string          `json:"exchange"`
	Cycle       []string        `json:"cycle"`
	Legs        []TriangularLeg `json:"legs"`
	StartAsset  string          `json:"start_asset"`
	StartQty    float64         `json:"start_qty"`
	EndQty      float64         `json:"end_qty"`
	GrossProfit float64         `json:"gross_profit"`
	NetProfit   float64         `json:"net_profit"`
	TotalFees   float64         `json:"total_fees"`
	Status      string          `json:"status"` // pending, executing, completed, failed, dry_run
	OpenedAt    int64           `json:"opened_at"`
	ClosedAt    int64           `json:"closed_at"`
}

// IsActive returns true if the trade is not yet closed.
func (t *TriangularTrade) IsActive() bool {
	switch t.Status {
	case "pending", "executing":
		return true
	}
	return false
}

// ── Triangular Performance ──────────────────────────────────────

// TriangularPerformance aggregates completed/dry-run triangular trades.
type TriangularPerformance struct {
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

// ── Market State ────────────────────────────────────────────────

type triangularMarketState struct {
	tick      model.Tick
	orderBook model.OrderBookData
	tickTime  int64
	obTime    int64
}

// ── Engine ──────────────────────────────────────────────────────

// TriangularEngine monitors a single exchange for 3-cycle arbitrage opportunities.
type TriangularEngine struct {
	config    TriangularEngineConfig
	client    ExchangeClient
	positions []*TriangularTrade
	history   []*TriangularTrade
	mu        sync.RWMutex
	seq       int64

	running bool
	lastOpp *TriangularOpportunity

	marketState map[string]*triangularMarketState
	stateMu     sync.RWMutex
	obSubID     event.SubscriptionID
	tickSubID   event.SubscriptionID

	repo *store.TriangularTradeRepo

	OnOpportunity func(opp TriangularOpportunity)
	OnTrade       func(trade TriangularTrade)
	OnError       func(err error)
}

// NewTriangularEngine creates a triangular arbitrage engine and restores state.
func NewTriangularEngine(cfg TriangularEngineConfig) *TriangularEngine {
	e := &TriangularEngine{
		config:      cfg,
		marketState: make(map[string]*triangularMarketState),
	}
	if store.GetDB() != nil {
		e.repo = store.GetTriangularTradeRepo()
		e.restore()
	}
	return e
}

func (e *TriangularEngine) restore() {
	if e.repo == nil {
		return
	}
	active, err := e.repo.ListActive()
	if err == nil {
		for _, r := range active {
			e.positions = append(e.positions, recordToTriangularTrade(r))
		}
	}
	history, err := e.repo.ListHistory(0)
	if err == nil {
		for _, r := range history {
			e.history = append(e.history, recordToTriangularTrade(r))
		}
	}
}

// RegisterClient registers the single exchange client for triangular arbitrage.
func (e *TriangularEngine) RegisterClient(client ExchangeClient) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.client = client
}

// GetClient returns the registered exchange client.
func (e *TriangularEngine) GetClient() ExchangeClient {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.client
}

// GetConfig returns the engine configuration.
func (e *TriangularEngine) GetConfig() TriangularEngineConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// SetConfig replaces the engine configuration while preserving client/history.
func (e *TriangularEngine) SetConfig(cfg TriangularEngineConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = cfg
}

// SetDryRun sets dry-run mode.
func (e *TriangularEngine) SetDryRun(v bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config.DryRun = v
}

// Start begins monitoring via the event bus.
func (e *TriangularEngine) Start() error {
	bus := app.Get().EventBus
	if bus == nil {
		return fmt.Errorf("event bus not initialized")
	}

	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("already running")
	}
	if e.client == nil {
		e.mu.Unlock()
		return fmt.Errorf("no exchange client registered")
	}
	e.running = true
	e.mu.Unlock()

	e.tickSubID = bus.Subscribe(e.config.Exchange, event.PrioNormal, e.handleTick, event.TypeTick)
	e.obSubID = bus.Subscribe(e.config.Exchange, event.PrioNormal, e.handleOrderBook, event.TypeOrderBook)

	e.client.WireToEventBus(bus)
	_ = e.client.StartMarketStream(e.config.Symbols)

	return nil
}

// Stop halts monitoring.
func (e *TriangularEngine) Stop() {
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

	if e.client != nil {
		_ = e.client.StopStream()
	}

	e.stateMu.Lock()
	e.marketState = make(map[string]*triangularMarketState)
	e.stateMu.Unlock()
}

// IsRunning returns true if the engine is active.
func (e *TriangularEngine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// Execute manually triggers execution of an opportunity.
func (e *TriangularEngine) Execute(opp TriangularOpportunity) {
	e.execute(opp)
}

// GetLastOpportunity returns the most recent opportunity.
func (e *TriangularEngine) GetLastOpportunity() *TriangularOpportunity {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.lastOpp == nil {
		return nil
	}
	cp := *e.lastOpp
	return &cp
}

// GetPositions returns active trades.
func (e *TriangularEngine) GetPositions() []*TriangularTrade {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*TriangularTrade, len(e.positions))
	copy(result, e.positions)
	return result
}

// GetHistory returns completed trade history.
func (e *TriangularEngine) GetHistory(limit int) []*TriangularTrade {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if limit <= 0 || limit > len(e.history) {
		limit = len(e.history)
	}
	start := len(e.history) - limit
	if start < 0 {
		start = 0
	}
	result := make([]*TriangularTrade, limit)
	copy(result, e.history[start:])
	return result
}

// GetStats returns engine statistics.
func (e *TriangularEngine) GetStats() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var totalProfit, totalFees float64
	var wins, losses int
	for _, t := range e.history {
		totalProfit += t.NetProfit
		totalFees += t.TotalFees
		if t.NetProfit > 0 {
			wins++
		} else {
			losses++
		}
	}

	lastProfitPct := 0.0
	if e.lastOpp != nil {
		lastProfitPct = e.lastOpp.NetProfitPct
	}

	return map[string]any{
		"running":         e.running,
		"exchange":        e.config.Exchange,
		"active_cycles":   len(e.positions),
		"total_trades":    len(e.history),
		"total_profit":    Round(totalProfit, 4),
		"total_fees":      Round(totalFees, 4),
		"win_count":       wins,
		"loss_count":      losses,
		"win_rate":        WinRate(wins, losses),
		"last_profit_pct": lastProfitPct,
	}
}

// GetPerformance returns aggregated triangular arbitrage performance metrics.
func (e *TriangularEngine) GetPerformance() TriangularPerformance {
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
		totalFees += t.TotalFees
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

	sort.Slice(points, func(i, j int) bool {
		return points[i].ts < points[j].ts
	})

	equityCurve := make([]TimeValuePoint, 0, len(points))
	dailyMap := make(map[string]float64)
	dailyTs := make(map[string]int64)
	var cumulative float64
	for _, p := range points {
		cumulative += p.pnl
		sec := p.ts / 1000
		equityCurve = append(equityCurve, TimeValuePoint{Time: sec, Value: Round(cumulative, 4)})
		day := time.Unix(sec, 0).Format("2006-01-02")
		dailyMap[day] += p.pnl
		if _, ok := dailyTs[day]; !ok {
			dailyTs[day] = sec
		}
	}

	dailyPnL := make([]TimeValuePoint, 0, len(dailyMap))
	for day, pnl := range dailyMap {
		dailyPnL = append(dailyPnL, TimeValuePoint{Time: dailyTs[day], Value: Round(pnl, 4)})
	}
	sort.Slice(dailyPnL, func(i, j int) bool {
		return dailyPnL[i].Time < dailyPnL[j].Time
	})

	totalTrades := wins + losses
	avgPnL := 0.0
	if totalTrades > 0 {
		avgPnL = totalPnL / float64(totalTrades)
	}

	return TriangularPerformance{
		TotalPnL:    Round(totalPnL, 4),
		TotalTrades: totalTrades,
		WinTrades:   wins,
		LossTrades:  losses,
		WinRate:     WinRate(wins, losses),
		AvgPnL:      Round(avgPnL, 4),
		MaxWin:      Round(maxWin, 4),
		MaxLoss:     Round(maxLoss, 4),
		TotalFees:   Round(totalFees, 4),
		EquityCurve: equityCurve,
		DailyPnL:    dailyPnL,
	}
}

// ClosePosition manually closes an active triangular trade.
func (e *TriangularEngine) ClosePosition(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var trade *TriangularTrade
	for _, t := range e.positions {
		if t.ID == id {
			trade = t
			break
		}
	}
	if trade == nil {
		return fmt.Errorf("position not found: %s", id)
	}
	if !trade.IsActive() {
		return fmt.Errorf("position %s is not active", id)
	}

	trade.Status = "completed"
	trade.ClosedAt = time.Now().UnixMilli()
	e.recordTriangularTradeLocked(trade)
	if e.OnTrade != nil {
		e.OnTrade(*trade)
	}
	return nil
}

// FailPosition marks an active triangular trade as failed.
func (e *TriangularEngine) FailPosition(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var trade *TriangularTrade
	for _, t := range e.positions {
		if t.ID == id {
			trade = t
			break
		}
	}
	if trade == nil {
		return fmt.Errorf("position not found: %s", id)
	}
	if !trade.IsActive() {
		return fmt.Errorf("position %s is not active", id)
	}

	trade.Status = "failed"
	trade.ClosedAt = time.Now().UnixMilli()
	e.recordTriangularTradeLocked(trade)
	return nil
}

// ── Event Handlers ──────────────────────────────────────────────

func (e *TriangularEngine) handleTick(evt event.Event) {
	tick, ok := evt.Data.(model.Tick)
	if !ok {
		return
	}
	if !e.isWatchedSymbol(tick.Symbol) {
		return
	}

	e.stateMu.Lock()
	state, ok := e.marketState[tick.Symbol]
	if !ok {
		state = &triangularMarketState{}
		e.marketState[tick.Symbol] = state
	}
	state.tick = tick
	state.tickTime = time.Now().UnixMilli()
	e.stateMu.Unlock()

	e.evaluate()
}

func (e *TriangularEngine) handleOrderBook(evt event.Event) {
	ob, ok := evt.Data.(model.OrderBookData)
	if !ok {
		return
	}
	if !e.isWatchedSymbol(ob.Symbol) {
		return
	}

	e.stateMu.Lock()
	state, ok := e.marketState[ob.Symbol]
	if !ok {
		state = &triangularMarketState{}
		e.marketState[ob.Symbol] = state
	}
	state.orderBook = ob
	state.obTime = time.Now().UnixMilli()
	e.stateMu.Unlock()

	e.evaluate()
}

func (e *TriangularEngine) isWatchedSymbol(symbol string) bool {
	for _, s := range e.GetConfig().Symbols {
		if strings.EqualFold(s, symbol) {
			return true
		}
	}
	return false
}

func (e *TriangularEngine) evaluate() {
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

	if e.config.AutoExecute && opp.Viable {
		if len(e.positions) < e.config.MaxPositions {
			go e.execute(*opp)
		}
	}
}

// ── Graph & Cycle Detection ─────────────────────────────────────

type triangularEdge struct {
	from      string
	to        string
	symbol    string
	side      string
	price     float64
	orderBook model.OrderBookData
}

func (e *TriangularEngine) buildEdgesLocked() []triangularEdge {
	e.stateMu.RLock()
	defer e.stateMu.RUnlock()

	var edges []triangularEdge
	for _, symbol := range e.config.Symbols {
		state, ok := e.marketState[symbol]
		if !ok {
			continue
		}
		if state.obTime == 0 || len(state.orderBook.Asks) == 0 || len(state.orderBook.Bids) == 0 {
			continue
		}
		base, quote := SplitSymbol(symbol)
		if base == "" || quote == "" {
			continue
		}

		bestAsk := state.orderBook.Asks[0][0]
		bestBid := state.orderBook.Bids[0][0]
		if bestAsk <= 0 || bestBid <= 0 {
			continue
		}

		// quote -> base: BUY at ask (spend quote, receive base)
		edges = append(edges, triangularEdge{
			from:      quote,
			to:        base,
			symbol:    symbol,
			side:      "BUY",
			price:     bestAsk,
			orderBook: state.orderBook,
		})
		// base -> quote: SELL at bid (spend base, receive quote)
		edges = append(edges, triangularEdge{
			from:      base,
			to:        quote,
			symbol:    symbol,
			side:      "SELL",
			price:     bestBid,
			orderBook: state.orderBook,
		})
	}
	return edges
}

func (e *TriangularEngine) findBestOpportunityLocked() *TriangularOpportunity {
	edges := e.buildEdgesLocked()
	if len(edges) < 6 {
		return nil
	}

	adj := make(map[string][]triangularEdge)
	for _, edge := range edges {
		adj[edge.from] = append(adj[edge.from], edge)
	}

	var best *TriangularOpportunity
	seen := make(map[string]bool)

	for _, e1 := range edges {
		for _, e2 := range adj[e1.to] {
			if e2.from != e1.to {
				continue
			}
			if e2.to == e1.from {
				continue
			}
			for _, e3 := range adj[e2.to] {
				if e3.from != e2.to || e3.to != e1.from {
					continue
				}
				// Found a 3-cycle e1.from -> e1.to -> e2.to -> e1.from
				cycle := []string{e1.from, e1.to, e2.to, e1.from}
				key := cycleKey(cycle)
				if seen[key] {
					continue
				}
				seen[key] = true

				opp := e.evaluateCycleLocked(e1, e2, e3, cycle)
				if opp == nil {
					continue
				}
				if !opp.Viable {
					continue
				}
				if best == nil || opp.NetProfitPct > best.NetProfitPct {
					best = opp
				}
			}
		}
	}

	return best
}

func cycleKey(cycle []string) string {
	// Normalize by rotating to start with the lexicographically smallest asset.
	if len(cycle) < 4 {
		return strings.Join(cycle, "/")
	}
	// cycle includes closing node; work with first 3 for rotation.
	nodes := cycle[:3]
	start := 0
	for i := 1; i < len(nodes); i++ {
		if nodes[i] < nodes[start] {
			start = i
		}
	}
	rotated := make([]string, 0, 4)
	for i := 0; i < 3; i++ {
		rotated = append(rotated, nodes[(start+i)%len(nodes)])
	}
	rotated = append(rotated, rotated[0])
	return strings.Join(rotated, "/")
}

func (e *TriangularEngine) evaluateCycleLocked(e1, e2, e3 triangularEdge, cycle []string) *TriangularOpportunity {
	cfg := e.config
	if cfg.QuoteAsset != "" && cycle[0] != cfg.QuoteAsset {
		return nil
	}

	startQty := cfg.OrderSize
	if e1.side == "BUY" {
		// For BUY leg, OrderSize is already in quote terms.
		startQty = cfg.OrderSize
	} else {
		// For SELL leg as first, OrderSize is quote value; convert to base qty.
		if e1.price > 0 {
			startQty = cfg.OrderSize / e1.price
		}
	}

	legs := []triangularEdge{e1, e2, e3}
	result, viable := e.resolveCycleQuantityLocked(legs, startQty)
	if !viable {
		return nil
	}

	if result.netProfitPct < cfg.MinProfitPct {
		return nil
	}

	oppID := fmt.Sprintf("tri-%d-%d", time.Now().UnixMilli(), e.seq)
	triLegs := make([]TriangularLeg, 3)
	for i, leg := range legs {
		triLegs[i] = TriangularLeg{
			Symbol:          leg.symbol,
			Side:            leg.side,
			OrderType:       "MARKET",
			Price:           leg.price,
			ExecutablePrice: result.execPrices[i],
			Quantity:        result.quantities[i],
			FilledQty:       result.quantities[i],
			Status:          "pending",
			Fee:             result.fees[i],
			SlippagePct:     result.slippages[i],
		}
	}

	return &TriangularOpportunity{
		ID:           oppID,
		Exchange:     cfg.Exchange,
		Cycle:        cycle,
		Legs:         triLegs,
		StartAsset:   cycle[0],
		StartQty:     result.startQty,
		EndQty:       result.endQty,
		GrossProfit:  result.grossProfit,
		NetProfit:    result.netProfit,
		NetProfitPct: result.netProfitPct,
		TotalFees:    result.totalFees,
		Viable:       true,
		Timestamp:    time.Now().UnixMilli(),
	}
}

type cycleResult struct {
	startQty     float64
	endQty       float64
	quantities   []float64
	execPrices   []float64
	slippages    []float64
	fees         []float64
	grossProfit  float64
	netProfit    float64
	netProfitPct float64
	totalFees    float64
}

func (e *TriangularEngine) resolveCycleQuantityLocked(legs []triangularEdge, startQty float64) (cycleResult, bool) {
	cfg := e.config
	qty := startQty
	adaptive := cfg.AdaptiveQtyEnabled

	for attempt := 0; attempt < 100; attempt++ {
		if qty <= 0 {
			return cycleResult{}, false
		}
		res, ok := e.simulateCycleLocked(legs, qty)
		if !ok {
			if !adaptive {
				return cycleResult{}, false
			}
			qty *= 0.9
			continue
		}
		if cfg.MaxSlippagePct > 0 {
			overSlippage := false
			for _, s := range res.slippages {
				if s > cfg.MaxSlippagePct {
					overSlippage = true
					break
				}
			}
			if overSlippage {
				if !adaptive {
					return cycleResult{}, false
				}
				qty *= 0.9
				continue
			}
		}
		if cfg.MinOrderQty > 0 {
			underMin := false
			for _, q := range res.quantities {
				if q < cfg.MinOrderQty {
					underMin = true
					break
				}
			}
			if underMin {
				return cycleResult{}, false
			}
		}
		return res, true
	}

	return cycleResult{}, false
}

func (e *TriangularEngine) simulateCycleLocked(legs []triangularEdge, startQty float64) (cycleResult, bool) {
	cfg := e.config
	feeRate := cfg.FeeRate
	if feeRate < 0 {
		feeRate = 0
	}

	quantities := make([]float64, len(legs))
	execPrices := make([]float64, len(legs))
	slippages := make([]float64, len(legs))
	fees := make([]float64, len(legs))

	amount := startQty
	for i, leg := range legs {
		output, execPrice, slippage, filled := WalkDepth(leg.orderBook, leg.side, amount)
		if !filled || execPrice <= 0 {
			return cycleResult{}, false
		}
		execPrices[i] = execPrice
		slippages[i] = slippage

		var nextAmount float64
		if leg.side == "BUY" {
			// amount was quote value; output is base qty received
			quantities[i] = output
			nextAmount = output * (1 - feeRate)
			fees[i] = (amount / execPrice) * feeRate // approximate fee in base terms
		} else {
			// amount was base qty; output is quote value received
			quantities[i] = amount
			nextAmount = output * (1 - feeRate)
			fees[i] = amount * execPrice * feeRate // fee in quote terms
		}
		amount = nextAmount
	}

	endQty := amount
	startValue := startQty
	if legs[0].side == "SELL" {
		// startQty was base qty; convert to quote value at first leg price
		startValue = startQty * legs[0].price
	}

	grossProfit := endQty - startValue
	totalFees := 0.0
	for _, f := range fees {
		totalFees += f
	}
	netProfit := grossProfit - totalFees
	netProfitPct := 0.0
	if startValue > 0 {
		netProfitPct = netProfit / startValue * 100
	}

	return cycleResult{
		startQty:     startQty,
		endQty:       endQty,
		quantities:   quantities,
		execPrices:   execPrices,
		slippages:    slippages,
		fees:         fees,
		grossProfit:  grossProfit,
		netProfit:    netProfit,
		netProfitPct: netProfitPct,
		totalFees:    totalFees,
	}, true
}

// ── Execution ───────────────────────────────────────────────────

func (e *TriangularEngine) execute(opp TriangularOpportunity) {
	e.mu.Lock()
	if len(e.positions) >= e.config.MaxPositions {
		e.mu.Unlock()
		return
	}
	e.seq++
	tradeID := fmt.Sprintf("tri-%d-%d", time.Now().UnixMilli(), e.seq)
	e.mu.Unlock()

	trade := &TriangularTrade{
		ID:         tradeID,
		Exchange:   opp.Exchange,
		Cycle:      opp.Cycle,
		Legs:       opp.Legs,
		StartAsset: opp.StartAsset,
		StartQty:   opp.StartQty,
		EndQty:     opp.EndQty,
		Status:     "pending",
		OpenedAt:   time.Now().UnixMilli(),
	}

	if err := e.checkBalance(opp); err != nil {
		trade.Status = "failed"
		trade.ClosedAt = time.Now().UnixMilli()
		e.recordTriangularTrade(trade)
		if e.OnError != nil {
			e.OnError(fmt.Errorf("balance check failed for %s: %w", tradeID, err))
		}
		return
	}

	// Persist pending state.
	e.recordTriangularTrade(trade)

	if e.config.DryRun {
		trade.Status = "dry_run"
		trade.EndQty = opp.EndQty
		trade.GrossProfit = opp.GrossProfit
		trade.NetProfit = opp.NetProfit
		trade.TotalFees = opp.TotalFees
		trade.ClosedAt = time.Now().UnixMilli()
		e.recordTriangularTrade(trade)
		if e.OnTrade != nil {
			e.OnTrade(*trade)
		}
		return
	}

	client := e.GetClient()
	if client == nil {
		trade.Status = "failed"
		trade.ClosedAt = time.Now().UnixMilli()
		e.recordTriangularTrade(trade)
		if e.OnError != nil {
			e.OnError(fmt.Errorf("exchange client missing for %s", tradeID))
		}
		return
	}

	trade.Status = "executing"
	e.recordTriangularTrade(trade)

	startTime := time.Now()
	amount := opp.StartQty
	failed := false

	for i := range trade.Legs {
		leg := &trade.Legs[i]
		qty := leg.Quantity
		if leg.Side == "BUY" {
			qty = amount // quote amount
		} else {
			qty = amount // base qty
		}
		leg.Quantity = qty

		if e.config.MaxExecutionMs > 0 && time.Since(startTime).Milliseconds() > int64(e.config.MaxExecutionMs) {
			leg.Status = "failed"
			failed = true
			if e.OnError != nil {
				e.OnError(fmt.Errorf("execution timeout for %s", tradeID))
			}
			break
		}

		result, err := client.PlaceOrder(leg.Symbol, leg.Side, "MARKET", 0, qty)
		if err != nil {
			leg.Status = "failed"
			failed = true
			if e.OnError != nil {
				e.OnError(fmt.Errorf("leg %d order failed: %w", i, err))
			}
			break
		}

		leg.OrderID = extractOrderID(result)
		leg.FilledQty = qty // MVP: assume immediate full fill
		leg.Status = "filled"

		// Compute next leg input amount.
		if leg.Side == "BUY" {
			amount = (qty / leg.ExecutablePrice) * (1 - e.config.FeeRate)
		} else {
			amount = qty * leg.ExecutablePrice * (1 - e.config.FeeRate)
		}
	}

	if failed {
		trade.Status = "failed"
		trade.ClosedAt = time.Now().UnixMilli()
	} else {
		trade.Status = "completed"
		trade.ClosedAt = time.Now().UnixMilli()
		trade.EndQty = amount
		trade.GrossProfit = opp.GrossProfit
		trade.NetProfit = opp.NetProfit
		trade.TotalFees = opp.TotalFees
	}

	e.recordTriangularTrade(trade)
	if e.OnTrade != nil {
		e.OnTrade(*trade)
	}
}

func extractOrderID(result map[string]any) string {
	for _, key := range []string{"order_id", "id", "orderId", "orderID"} {
		if id, ok := result[key].(string); ok && id != "" {
			return id
		}
	}
	return ""
}

// checkBalance verifies that the exchange holds sufficient starting asset.
func (e *TriangularEngine) checkBalance(opp TriangularOpportunity) error {
	client := e.GetClient()
	if client == nil {
		return fmt.Errorf("missing exchange client")
	}

	balances, err := client.GetBalance()
	if err != nil {
		return fmt.Errorf("balance fetch failed: %w", err)
	}

	free := ExtractFreeBalance(balances, opp.StartAsset)
	if free < opp.StartQty {
		return fmt.Errorf("insufficient %s: need %.4f, have %.4f", opp.StartAsset, opp.StartQty, free)
	}

	return nil
}

// ── Persistence ─────────────────────────────────────────────────

func (e *TriangularEngine) recordTriangularTrade(trade *TriangularTrade) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recordTriangularTradeLocked(trade)
}

func (e *TriangularEngine) recordTriangularTradeLocked(trade *TriangularTrade) {
	if trade.IsActive() {
		found := false
		for _, t := range e.positions {
			if t.ID == trade.ID {
				found = true
				break
			}
		}
		if !found {
			e.positions = append(e.positions, trade)
		}
	}

	if trade.Status == "completed" || trade.Status == "failed" || trade.Status == "dry_run" {
		e.history = append(e.history, trade)
		var active []*TriangularTrade
		for _, t := range e.positions {
			if t.ID != trade.ID {
				active = append(active, t)
			}
		}
		e.positions = active
	}

	e.persistTriangularTrade(trade)
}

func (e *TriangularEngine) persistTriangularTrade(trade *TriangularTrade) {
	if e.repo == nil {
		return
	}
	rec := triangularTradeToRecord(trade)
	if _, err := e.repo.GetByID(rec.ID); err == nil {
		_ = e.repo.Update(rec)
	} else {
		_ = e.repo.Create(rec)
	}
}

func triangularTradeToRecord(t *TriangularTrade) *store.TriangularTradeRecord {
	cycleJSON, _ := json.Marshal(t.Cycle)
	legsJSON, _ := json.Marshal(t.Legs)
	return &store.TriangularTradeRecord{
		ID:          t.ID,
		Exchange:    t.Exchange,
		CycleJSON:   string(cycleJSON),
		LegsJSON:    string(legsJSON),
		StartAsset:  t.StartAsset,
		StartQty:    t.StartQty,
		EndQty:      t.EndQty,
		GrossProfit: t.GrossProfit,
		NetProfit:   t.NetProfit,
		TotalFees:   t.TotalFees,
		Status:      t.Status,
		OpenedAt:    t.OpenedAt,
		ClosedAt:    t.ClosedAt,
	}
}

func recordToTriangularTrade(r *store.TriangularTradeRecord) *TriangularTrade {
	t := &TriangularTrade{
		ID:          r.ID,
		Exchange:    r.Exchange,
		StartAsset:  r.StartAsset,
		StartQty:    r.StartQty,
		EndQty:      r.EndQty,
		GrossProfit: r.GrossProfit,
		NetProfit:   r.NetProfit,
		TotalFees:   r.TotalFees,
		Status:      r.Status,
		OpenedAt:    r.OpenedAt,
		ClosedAt:    r.ClosedAt,
	}
	_ = json.Unmarshal([]byte(r.CycleJSON), &t.Cycle)
	_ = json.Unmarshal([]byte(r.LegsJSON), &t.Legs)
	return t
}
