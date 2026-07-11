package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/arbitrage"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Global Arbitrage Engine ─────────────────────────────────────

var (
	arbEngine   *arbitrage.Engine
	arbEngineMu sync.RWMutex
)

// GetArbEngine returns the global arbitrage engine (lazy init).
// On first call it restores any persisted config from config.yaml and auto-registers
// exchanges that are configured in the unified exchange settings.
func GetArbEngine() *arbitrage.Engine {
	arbEngineMu.RLock()
	if arbEngine != nil {
		defer arbEngineMu.RUnlock()
		return arbEngine
	}
	arbEngineMu.RUnlock()

	arbEngineMu.Lock()
	defer arbEngineMu.Unlock()
	// Double-check after acquiring write lock.
	if arbEngine != nil {
		return arbEngine
	}

	cfg := arbitrage.DefaultEngineConfig()
	if saved := store.LoadArbitrageConfig(); saved != nil {
		if data, err := json.Marshal(saved); err == nil {
			_ = json.Unmarshal(data, &cfg)
		}
	}
	arbEngine = arbitrage.NewEngine(cfg)
	autoRegisterExchanges(arbEngine)
	return arbEngine
}

// wireEngineCallbacks attaches notification callbacks to the engine.
func wireEngineCallbacks(engine *arbitrage.Engine) {
	engine.OnOpportunity = func(opp arbitrage.Opportunity) {
		broadcaster := notify.NewBroadcaster()
		broadcaster.System("arbitrage_opportunity", fmt.Sprintf(
			"Spread %.2f%%: buy %s @ %.2f, sell %s @ %.2f",
			opp.SpreadPct, opp.BuyExchange, opp.BuyPrice, opp.SellExchange, opp.SellPrice,
		))
	}

	engine.OnTrade = func(trade arbitrage.TradePair) {
		broadcaster := notify.NewBroadcaster()
		if trade.Status == "dry_run" {
			broadcaster.System("arbitrage_dry_run", fmt.Sprintf(
				"Simulated arb: %s buy@%s sell@%s profit=%.2f",
				trade.Symbol, trade.BuyExchange, trade.SellExchange, trade.NetProfit,
			))
		} else {
			broadcaster.Trade(trade.Symbol, "ARB", trade.SellPrice, trade.Quantity, trade.NetProfit)
		}
	}
}

// startArbitrageStreams starts WebSocket market streams for all registered clients asynchronously.
func startArbitrageStreams(engine *arbitrage.Engine) {
	bus := app.Get().EventBus
	if bus == nil {
		return
	}
	cfg := engine.GetConfig()
	engine.IterateClients(func(name string, client arbitrage.ExchangeClient) {
		client.WireToEventBus(bus)
		go func(n string, c arbitrage.ExchangeClient) {
			if err := c.StartMarketStream(cfg.SymbolsList()); err != nil {
				log.Printf("[arbitrage] %s stream start failed: %v", n, err)
			}
		}(name, client)
	})
}

// stopArbitrageStreams stops all client market streams.
func stopArbitrageStreams(engine *arbitrage.Engine) {
	engine.IterateClients(func(name string, client arbitrage.ExchangeClient) {
		_ = client.StopStream()
	})
}

// restartArbitrageStreams restarts streams after a symbol/config change.
func restartArbitrageStreams(engine *arbitrage.Engine) {
	stopArbitrageStreams(engine)
	startArbitrageStreams(engine)
}

// ── Config ─────────────────────────────────────────────────────

// GetArbitrageConfig returns current arbitrage configuration.
func GetArbitrageConfig(c *gin.Context) {
	engine := GetArbEngine()
	c.JSON(http.StatusOK, gin.H{
		"config": engine.GetConfig(),
	})
}

// UpdateArbitrageConfig updates arbitrage configuration.
// Only fields explicitly present in the request body are overwritten; missing fields keep their current values.
func UpdateArbitrageConfig(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var raw map[string]any
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var body arbitrage.EngineConfig
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	arbEngineMu.Lock()
	defer arbEngineMu.Unlock()

	oldCfg := arbitrage.DefaultEngineConfig()
	if arbEngine != nil {
		oldCfg = arbEngine.GetConfig()
	}

	merged := mergeArbitrageConfig(oldCfg, body, raw)
	if err := validateArbitrageConfig(merged); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	wasRunning := arbEngine != nil && arbEngine.IsRunning()
	if wasRunning {
		arbEngine.Stop()
		stopArbitrageStreams(arbEngine)
	}

	oldEngine := arbEngine
	arbEngine = arbitrage.NewEngine(merged)
	// Migrate registered exchange clients so they survive config updates.
	if oldEngine != nil {
		oldEngine.IterateClients(func(name string, client arbitrage.ExchangeClient) {
			arbEngine.RegisterExchange(name, client)
		})
	}
	wireEngineCallbacks(arbEngine)
	if wasRunning {
		startArbitrageStreams(arbEngine)
		if err := arbEngine.Start(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to restart engine: %v", err)})
			return
		}
	}

	// Persist configuration so it survives gateway restarts.
	if data, err := json.Marshal(merged); err == nil {
		var persisted map[string]any
		if err := json.Unmarshal(data, &persisted); err == nil {
			_ = store.SaveArbitrageConfig(persisted)
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated", "config": merged})
}

// mergeArbitrageConfig overlays update onto old, but only for fields present in raw JSON.
func mergeArbitrageConfig(old, update arbitrage.EngineConfig, raw map[string]any) arbitrage.EngineConfig {
	merged := old

	if hasKey(raw, "symbol") {
		merged.Symbol = update.Symbol
	}
	if hasKey(raw, "symbols") {
		merged.Symbols = update.Symbols
	}
	if hasKey(raw, "min_spread_pct") {
		merged.MinSpreadPct = update.MinSpreadPct
	}
	if hasKey(raw, "order_size") {
		merged.OrderSize = update.OrderSize
	}
	if hasKey(raw, "max_positions") {
		merged.MaxPositions = update.MaxPositions
	}
	if hasKey(raw, "fee_a") {
		merged.FeeA = update.FeeA
	}
	if hasKey(raw, "fee_b") {
		merged.FeeB = update.FeeB
	}
	if hasKey(raw, "poll_interval") {
		merged.PollInterval = update.PollInterval
	}
	if hasKey(raw, "auto_execute") {
		merged.AutoExecute = update.AutoExecute
	}
	if hasKey(raw, "dry_run") {
		merged.DryRun = update.DryRun
	}
	if hasKey(raw, "adaptive_qty_enabled") {
		merged.AdaptiveQtyEnabled = update.AdaptiveQtyEnabled
	}
	if hasKey(raw, "max_slippage_pct") {
		merged.MaxSlippagePct = update.MaxSlippagePct
	}
	if hasKey(raw, "min_order_qty") {
		merged.MinOrderQty = update.MinOrderQty
	}
	if hasKey(raw, "min_order_value") {
		merged.MinOrderValue = update.MinOrderValue
	}

	return merged
}

func hasKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

// validateArbitrageConfig rejects unreasonable or dangerous values.
func validateArbitrageConfig(cfg arbitrage.EngineConfig) error {
	if cfg.MinSpreadPct < 0 {
		return fmt.Errorf("min_spread_pct must be non-negative")
	}
	if cfg.OrderSize <= 0 {
		return fmt.Errorf("order_size must be positive")
	}
	if cfg.MaxPositions <= 0 {
		return fmt.Errorf("max_positions must be positive")
	}
	if cfg.FeeA < 0 || cfg.FeeB < 0 {
		return fmt.Errorf("fee_a and fee_b must be non-negative")
	}
	if cfg.MaxSlippagePct < 0 {
		return fmt.Errorf("max_slippage_pct must be non-negative")
	}
	if cfg.MinOrderQty < 0 {
		return fmt.Errorf("min_order_qty must be non-negative")
	}
	if cfg.MinOrderValue < 0 {
		return fmt.Errorf("min_order_value must be non-negative")
	}
	if cfg.PollInterval < 0 {
		return fmt.Errorf("poll_interval must be non-negative")
	}
	return nil
}

// ── Engine Control ───────────────────────────────────────────

// StartArbitrage starts the arbitrage monitoring engine.
func StartArbitrage(c *gin.Context) {
	engine := GetArbEngine()

	wireEngineCallbacks(engine)
	startArbitrageStreams(engine)

	if err := engine.Start(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "started",
		"exchanges": engine.GetClientCount(),
	})
}

// StopArbitrage stops the arbitrage engine.
func StopArbitrage(c *gin.Context) {
	engine := GetArbEngine()
	engine.Stop()
	stopArbitrageStreams(engine)
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// GetArbitrageStatus returns engine status and stats.
func GetArbitrageStatus(c *gin.Context) {
	engine := GetArbEngine()
	c.JSON(http.StatusOK, gin.H{
		"running": engine.IsRunning(),
		"stats":   engine.GetStats(),
	})
}

// GetArbitragePerformance returns aggregated arbitrage performance metrics.
func GetArbitragePerformance(c *gin.Context) {
	engine := GetArbEngine()
	c.JSON(http.StatusOK, engine.GetPerformance())
}

// ── Opportunities ────────────────────────────────────────────────

// GetArbitrageOpportunity returns the latest detected opportunity.
func GetArbitrageOpportunity(c *gin.Context) {
	engine := GetArbEngine()
	opp := engine.GetLastOpportunity()
	if opp == nil {
		c.JSON(http.StatusOK, gin.H{"opportunity": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"opportunity": opp})
}

// ── Positions & History ────────────────────────────────────────

// GetArbitragePositions returns active trade pairs.
func GetArbitragePositions(c *gin.Context) {
	engine := GetArbEngine()
	positions := engine.GetPositions()
	if positions == nil {
		positions = []*arbitrage.TradePair{}
	}
	c.JSON(http.StatusOK, gin.H{"positions": positions, "count": len(positions)})
}

// GetArbitrageHistory returns completed trade history.
func GetArbitrageHistory(c *gin.Context) {
	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	engine := GetArbEngine()
	history := engine.GetHistory(limit)
	if history == nil {
		history = []*arbitrage.TradePair{}
	}
	c.JSON(http.StatusOK, gin.H{"history": history, "count": len(history)})
}

// ── Exchanges ──────────────────────────────────────────────────

// getExchangeCredentials reads exchange API credentials from the unified config store.
// Falls back to adapter.GetCredential for API key/secret/passphrase, then reads testnet flag.
func getExchangeCredentials(name string) (apiKey, secret, passphrase string, testnet bool) {
	apiKey, secret, passphrase = adapter.GetCredential(name)

	cfg := store.GetConfig()
	if exchanges, ok := cfg["exchanges"].(map[string]any); ok {
		if ex, ok := exchanges[name].(map[string]any); ok {
			if v, ok := ex["testnet"].(bool); ok {
				testnet = v
			}
		}
	}
	return
}

// createArbitrageClient builds an exchange client for the arbitrage engine.
func createArbitrageClient(name, apiKey, secret, passphrase string, testnet bool) (arbitrage.ExchangeClient, error) {
	switch strings.ToLower(name) {
	case "binance":
		return &arbitrage.BinanceClient{BinanceAdapter: adapter.NewBinanceAdapter(apiKey, secret, testnet)}, nil
	case "okx":
		return &arbitrage.OKXClient{OKXAdapter: adapter.NewOKXAdapter(apiKey, secret, passphrase, testnet)}, nil
	case "mexc":
		return &arbitrage.MEXCClient{MEXCAdapter: adapter.NewMEXCAdapter(apiKey, secret)}, nil
	case "gateio", "gate":
		return &arbitrage.GateIOClient{GateIOAdapter: adapter.NewGateIOAdapter(apiKey, secret)}, nil
	case "bybit":
		return &arbitrage.BybitClient{BybitAdapter: adapter.NewBybitAdapter(apiKey, secret, testnet)}, nil
	case "coinbase":
		return &arbitrage.CoinbaseClient{CoinbaseAdapter: adapter.NewCoinbaseAdapter(apiKey, secret)}, nil
	case "kraken":
		return &arbitrage.KrakenClient{KrakenAdapter: adapter.NewKrakenAdapter(apiKey, secret)}, nil
	case "bitget":
		return &arbitrage.BitgetClient{BitgetAdapter: adapter.NewBitgetAdapter(apiKey, secret, passphrase)}, nil
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", name)
	}
}

// autoRegisterExchanges registers all enabled exchanges from the unified config
// so the arbitrage engine is ready after gateway restart without manual registration.
func autoRegisterExchanges(engine *arbitrage.Engine) {
	cfg := store.GetConfig()
	exchanges, ok := cfg["exchanges"].(map[string]any)
	if !ok {
		return
	}

	for name, raw := range exchanges {
		ex, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if enabled, _ := ex["enabled"].(bool); !enabled {
			continue
		}

		apiKey, secret, passphrase, testnet := getExchangeCredentials(name)
		if apiKey == "" || secret == "" {
			continue
		}

		client, err := createArbitrageClient(name, apiKey, secret, passphrase, testnet)
		if err != nil {
			continue
		}
		engine.RegisterExchange(name, client)
	}
}

// RegisterArbitrageExchange registers an exchange for arbitrage.
// Credentials are read from the unified config store unless explicitly provided in the request.
func RegisterArbitrageExchange(c *gin.Context) {
	var body struct {
		Name       string `json:"name"`
		APIKey     string `json:"api_key"`
		Secret     string `json:"secret"`
		Passphrase string `json:"passphrase,omitempty"`
		Testnet    bool   `json:"testnet"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	apiKey, secret, passphrase, testnet := getExchangeCredentials(body.Name)

	// If the request provides explicit credentials, use them as an override.
	if body.APIKey != "" || body.Secret != "" {
		if body.APIKey != "" {
			apiKey = body.APIKey
		}
		if body.Secret != "" {
			secret = body.Secret
		}
		if body.Passphrase != "" {
			passphrase = body.Passphrase
		}
		testnet = body.Testnet
	}

	if apiKey == "" || secret == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("no credentials found for %s; configure it in Settings first", body.Name),
		})
		return
	}

	client, err := createArbitrageClient(body.Name, apiKey, secret, passphrase, testnet)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	engine := GetArbEngine()
	engine.RegisterExchange(body.Name, client)
	if engine.IsRunning() {
		bus := app.Get().EventBus
		if bus != nil {
			client.WireToEventBus(bus)
			symbols := engine.GetConfig().SymbolsList()
			go func(name string) {
				if err := client.StartMarketStream(symbols); err != nil {
					log.Printf("[arbitrage] %s stream start failed after registration: %v", name, err)
				}
			}(body.Name)
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "registered", "exchange": body.Name})
}

// ListArbitrageExchanges returns registered exchanges.
func ListArbitrageExchanges(c *gin.Context) {
	engine := GetArbEngine()
	stats := engine.GetStats()

	var names []string
	engine.IterateClients(func(name string, _ arbitrage.ExchangeClient) {
		names = append(names, name)
	})

	c.JSON(http.StatusOK, gin.H{
		"registered_count": stats["exchanges"],
		"exchanges":        names,
	})
}

// ── Manual Execution ───────────────────────────────────────────

// ExecuteArbitrage manually triggers an arbitrage execution.
func ExecuteArbitrage(c *gin.Context) {
	var body struct {
		Symbol       string  `json:"symbol"`
		BuyExchange  string  `json:"buy_exchange"`
		SellExchange string  `json:"sell_exchange"`
		BuyPrice     float64 `json:"buy_price"`
		SellPrice    float64 `json:"sell_price"`
		Quantity     float64 `json:"quantity"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if strings.TrimSpace(body.Symbol) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is required"})
		return
	}
	if strings.TrimSpace(body.BuyExchange) == "" || strings.TrimSpace(body.SellExchange) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "buy_exchange and sell_exchange are required"})
		return
	}
	if strings.EqualFold(body.BuyExchange, body.SellExchange) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "buy_exchange and sell_exchange must be different"})
		return
	}
	if body.BuyPrice <= 0 || body.SellPrice <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "buy_price and sell_price must be positive"})
		return
	}
	if body.Quantity < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be non-negative"})
		return
	}

	engine := GetArbEngine()

	// Ensure both exchanges are registered before attempting execution.
	registered := make(map[string]bool)
	engine.IterateClients(func(name string, _ arbitrage.ExchangeClient) {
		registered[name] = true
	})
	if !registered[body.BuyExchange] {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("buy_exchange %s is not registered", body.BuyExchange)})
		return
	}
	if !registered[body.SellExchange] {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("sell_exchange %s is not registered", body.SellExchange)})
		return
	}

	opp := arbitrage.Opportunity{
		Symbol:       body.Symbol,
		BuyExchange:  body.BuyExchange,
		SellExchange: body.SellExchange,
		BuyPrice:     body.BuyPrice,
		SellPrice:    body.SellPrice,
		AdjustedQty:  body.Quantity,
		Timestamp:    time.Now().UnixMilli(),
	}
	opp.SpreadAbs = body.SellPrice - body.BuyPrice
	opp.SpreadPct = opp.SpreadAbs / body.BuyPrice * 100

	engine.ExecuteLive(opp)

	c.JSON(http.StatusOK, gin.H{"status": "executed", "opportunity": opp})
}

// CloseArbitragePosition manually closes an open arbitrage position.
func CloseArbitragePosition(c *gin.Context) {
	id := c.Param("id")
	var body struct {
		SellPrice float64 `json:"sell_price"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	engine := GetArbEngine()
	if err := engine.ClosePosition(id, body.SellPrice); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "closed", "id": id})
}

// FailArbitragePosition marks an open arbitrage position as failed.
func FailArbitragePosition(c *gin.Context) {
	id := c.Param("id")

	engine := GetArbEngine()
	if err := engine.FailPosition(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "failed", "id": id})
}
