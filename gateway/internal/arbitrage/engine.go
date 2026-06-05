package arbitrage

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/adapter"
)

// ── Exchange Client Interface ─────────────────────────────────

// ExchangeClient defines the minimal interface for arbitrage.
type ExchangeClient interface {
	Name() string
	GetTicker(symbol string) (map[string]any, error)
	PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error)
	GetBalance() ([]map[string]any, error)
}

// ── Opportunity ─────────────────────────────────────────────────

// Opportunity represents a detected arbitrage spread.
type Opportunity struct {
	Symbol       string  `json:"symbol"`
	BuyExchange  string  `json:"buy_exchange"`
	SellExchange string  `json:"sell_exchange"`
	BuyPrice     float64 `json:"buy_price"`
	SellPrice    float64 `json:"sell_price"`
	SpreadPct    float64 `json:"spread_pct"`
	SpreadAbs    float64 `json:"spread_abs"`
	Timestamp    int64   `json:"timestamp"`
}

// IsProfitable returns true if spread exceeds threshold after fees.
func (o *Opportunity) IsProfitable(minSpreadPct, feeA, feeB float64) bool {
	netSpread := o.SpreadPct - (feeA+feeB)*100
	return netSpread >= minSpreadPct
}

// ── Trade Pair ──────────────────────────────────────────────────

// TradePair tracks a completed arbitrage round-trip.
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
	Status       string  `json:"status"` // pending, completed, failed, dry_run
	OpenedAt     int64   `json:"opened_at"`
	ClosedAt     int64   `json:"closed_at"`
}

// ── Engine Config ───────────────────────────────────────────────

type EngineConfig struct {
	Symbol       string        `json:"symbol"`
	MinSpreadPct float64       `json:"min_spread_pct"` // minimum spread to trigger
	OrderSize    float64       `json:"order_size"`     // quote currency amount per leg
	MaxPositions int           `json:"max_positions"`  // max concurrent arbitrage pairs
	FeeA         float64       `json:"fee_a"`          // taker fee exchange A (decimal)
	FeeB         float64       `json:"fee_b"`          // taker fee exchange B (decimal)
	PollInterval time.Duration `json:"poll_interval"`  // price poll interval
	AutoExecute  bool          `json:"auto_execute"`   // auto-place orders on detection
	DryRun       bool          `json:"dry_run"`        // log only, don't place orders
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
	}
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
	stopCh  chan struct{}
	lastOpp *Opportunity

	// Callbacks
	OnOpportunity func(opp Opportunity)
	OnTrade       func(trade TradePair)
	OnError       func(err error)
}

// NewEngine creates an arbitrage engine.
func NewEngine(cfg EngineConfig) *Engine {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	return &Engine{
		config:  cfg,
		clients: make(map[string]ExchangeClient),
		stopCh:  make(chan struct{}),
	}
}

// RegisterExchange adds an exchange client.
func (e *Engine) RegisterExchange(name string, client ExchangeClient) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clients[name] = client
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

// Start begins monitoring.
func (e *Engine) Start() error {
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

	go e.loop()
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
	close(e.stopCh)
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

// ── Main Loop ──────────────────────────────────────────────────

func (e *Engine) loop() {
	ticker := time.NewTicker(e.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.tick()
		}
	}
}

func (e *Engine) tick() {
	// Fetch prices from all exchanges
	prices := e.fetchPrices()
	if len(prices) < 2 {
		return
	}

	// Find best opportunity
	opp := e.findBestOpportunity(prices)
	if opp == nil {
		return
	}

	e.mu.Lock()
	e.lastOpp = opp
	e.mu.Unlock()

	if e.OnOpportunity != nil {
		e.OnOpportunity(*opp)
	}

	// Auto-execute if enabled and profitable
	if e.config.AutoExecute && opp.IsProfitable(e.config.MinSpreadPct, e.config.FeeA, e.config.FeeB) {
		e.execute(*opp)
	}
}

// fetchPrices retrieves current prices from all registered exchanges.
func (e *Engine) fetchPrices() map[string]float64 {
	e.mu.RLock()
	clients := make(map[string]ExchangeClient, len(e.clients))
	for k, v := range e.clients {
		clients[k] = v
	}
	e.mu.RUnlock()

	prices := make(map[string]float64)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for name, client := range clients {
		wg.Add(1)
		go func(n string, c ExchangeClient) {
			defer wg.Done()
			ticker, err := c.GetTicker(e.config.Symbol)
			if err != nil {
				if e.OnError != nil {
					e.OnError(fmt.Errorf("%s ticker: %w", n, err))
				}
				return
			}
			price := extractPrice(ticker)
			if price > 0 {
				mu.Lock()
				prices[n] = price
				mu.Unlock()
			}
		}(name, client)
	}

	wg.Wait()
	return prices
}

// findBestOpportunity scans all exchange pairs for the highest spread.
func (e *Engine) findBestOpportunity(prices map[string]float64) *Opportunity {
	var best *Opportunity
	names := make([]string, 0, len(prices))
	for n := range prices {
		names = append(names, n)
	}

	for i, a := range names {
		for j, b := range names {
			if i >= j {
				continue
			}
			priceA := prices[a]
			priceB := prices[b]
			if priceA <= 0 || priceB <= 0 {
				continue
			}

			var buyEx, sellEx string
			var buyPrice, sellPrice float64
			if priceA < priceB {
				buyEx, sellEx = a, b
				buyPrice, sellPrice = priceA, priceB
			} else {
				buyEx, sellEx = b, a
				buyPrice, sellPrice = priceB, priceA
			}

			spreadAbs := sellPrice - buyPrice
			spreadPct := spreadAbs / buyPrice * 100

			if best == nil || spreadPct > best.SpreadPct {
				best = &Opportunity{
					Symbol:       e.config.Symbol,
					BuyExchange:  buyEx,
					SellExchange: sellEx,
					BuyPrice:     buyPrice,
					SellPrice:    sellPrice,
					SpreadPct:    spreadPct,
					SpreadAbs:    spreadAbs,
					Timestamp:    time.Now().UnixMilli(),
				}
			}
		}
	}

	return best
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

	// Calculate quantity
	qty := e.config.OrderSize / opp.BuyPrice
	qty = math.Floor(qty*1e6) / 1e6

	pair := &TradePair{
		ID:           pairID,
		Symbol:       opp.Symbol,
		BuyExchange:  opp.BuyExchange,
		SellExchange: opp.SellExchange,
		BuyPrice:     opp.BuyPrice,
		SellPrice:    opp.SellPrice,
		Quantity:     qty,
		Status:       "pending",
		OpenedAt:     time.Now().UnixMilli(),
	}

	if e.config.DryRun {
		pair.Status = "dry_run"
		pair.GrossProfit = (opp.SellPrice - opp.BuyPrice) * qty
		pair.Fees = (opp.BuyPrice*qty*e.config.FeeA) + (opp.SellPrice*qty*e.config.FeeB)
		pair.NetProfit = pair.GrossProfit - pair.Fees
		e.recordPair(pair)
		if e.OnTrade != nil {
			e.OnTrade(*pair)
		}
		return
	}

	// Place buy order
	buyClient := e.clients[opp.BuyExchange]
	sellClient := e.clients[opp.SellExchange]

	if buyClient == nil || sellClient == nil {
		pair.Status = "failed"
		pair.NetProfit = 0
		e.recordPair(pair)
		return
	}

	// Buy on cheaper exchange
	buyResult, err := buyClient.PlaceOrder(opp.Symbol, "BUY", "MARKET", 0, qty)
	if err != nil {
		pair.Status = "failed"
		pair.NetProfit = 0
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

	// Sell on expensive exchange
	sellResult, err := sellClient.PlaceOrder(opp.Symbol, "SELL", "MARKET", 0, qty)
	if err != nil {
		pair.Status = "failed"
		pair.NetProfit = 0
		e.recordPair(pair)
		if e.OnError != nil {
			e.OnError(fmt.Errorf("sell order failed: %w", err))
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
	pair.GrossProfit = (opp.SellPrice - opp.BuyPrice) * qty
	pair.Fees = (opp.BuyPrice*qty*e.config.FeeA) + (opp.SellPrice*qty*e.config.FeeB)
	pair.NetProfit = pair.GrossProfit - pair.Fees

	e.recordPair(pair)
	if e.OnTrade != nil {
		e.OnTrade(*pair)
	}
}

func (e *Engine) recordPair(pair *TradePair) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if pair.Status == "pending" || pair.Status == "dry_run" || pair.Status == "completed" {
		e.positions = append(e.positions, pair)
	}
	if pair.Status == "completed" || pair.Status == "failed" || pair.Status == "dry_run" {
		e.history = append(e.history, pair)
		// Remove from active positions
		var active []*TradePair
		for _, p := range e.positions {
			if p.ID != pair.ID {
				active = append(active, p)
			}
		}
		e.positions = active
	}
}

// ── Helpers ─────────────────────────────────────────────────────

func extractPrice(ticker map[string]any) float64 {
	// Try common field names across exchanges
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

// ── Adapter Wrappers ────────────────────────────────────────────

// BinanceClient wraps BinanceAdapter for the ExchangeClient interface.
type BinanceClient struct {
	*adapter.BinanceAdapter
}

func (b *BinanceClient) Name() string { return "binance" }

// OKXClient wraps OKXAdapter for the ExchangeClient interface.
type OKXClient struct {
	*adapter.OKXAdapter
}

func (o *OKXClient) Name() string { return "okx" }

// MEXCClient wraps MEXCAdapter for the ExchangeClient interface.
type MEXCClient struct {
	*adapter.MEXCAdapter
}

func (m *MEXCClient) Name() string { return "mexc" }

// GateIOClient wraps GateIOAdapter for the ExchangeClient interface.
type GateIOClient struct {
	*adapter.GateIOAdapter
}

func (g *GateIOClient) Name() string { return "gateio" }

// BybitClient wraps BybitAdapter for the ExchangeClient interface.
type BybitClient struct {
	*adapter.BybitAdapter
}

func (b *BybitClient) Name() string { return "bybit" }

// CoinbaseClient wraps CoinbaseAdapter for the ExchangeClient interface.
type CoinbaseClient struct {
	*adapter.CoinbaseAdapter
}

func (c *CoinbaseClient) Name() string { return "coinbase" }

// KrakenClient wraps KrakenAdapter for the ExchangeClient interface.
type KrakenClient struct {
	*adapter.KrakenAdapter
}

func (k *KrakenClient) Name() string { return "kraken" }

// BitgetClient wraps BitgetAdapter for the ExchangeClient interface.
type BitgetClient struct {
	*adapter.BitgetAdapter
}

func (b *BitgetClient) Name() string { return "bitget" }
