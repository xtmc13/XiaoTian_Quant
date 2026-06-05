package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/arbitrage"
	"github.com/xiaotian-quant/gateway/internal/notify"
)

// ── Global Arbitrage Engine ─────────────────────────────────────

var arbEngine *arbitrage.Engine

// GetArbEngine returns the global arbitrage engine (lazy init).
func GetArbEngine() *arbitrage.Engine {
	if arbEngine == nil {
		arbEngine = arbitrage.NewEngine(arbitrage.DefaultEngineConfig())
	}
	return arbEngine
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
func UpdateArbitrageConfig(c *gin.Context) {
	var body arbitrage.EngineConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	wasRunning := arbEngine != nil && arbEngine.IsRunning()
	if wasRunning {
		arbEngine.Stop()
	}

	arbEngine = arbitrage.NewEngine(body)
	if wasRunning {
		_ = arbEngine.Start()
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated", "config": body})
}

// ── Engine Control ───────────────────────────────────────────

// StartArbitrage starts the arbitrage monitoring engine.
func StartArbitrage(c *gin.Context) {
	engine := GetArbEngine()

	// Wire notification on opportunity
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

// RegisterArbitrageExchange registers an exchange for arbitrage.
func RegisterArbitrageExchange(c *gin.Context) {
	var body struct {
		Name    string `json:"name"`
		APIKey  string `json:"api_key"`
		Secret  string `json:"secret"`
		Passphrase string `json:"passphrase,omitempty"`
		Testnet bool   `json:"testnet"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	engine := GetArbEngine()

	var client arbitrage.ExchangeClient
	switch body.Name {
	case "binance":
		client = &arbitrage.BinanceClient{BinanceAdapter: adapter.NewBinanceAdapter(body.APIKey, body.Secret, body.Testnet)}
	case "okx":
		client = &arbitrage.OKXClient{OKXAdapter: adapter.NewOKXAdapter(body.APIKey, body.Secret, body.Passphrase, body.Testnet)}
	case "mexc":
		client = &arbitrage.MEXCClient{MEXCAdapter: adapter.NewMEXCAdapter(body.APIKey, body.Secret)}
	case "gateio":
		client = &arbitrage.GateIOClient{GateIOAdapter: adapter.NewGateIOAdapter(body.APIKey, body.Secret)}
	case "bybit":
		client = &arbitrage.BybitClient{BybitAdapter: adapter.NewBybitAdapter(body.APIKey, body.Secret, body.Testnet)}
	case "coinbase":
		client = &arbitrage.CoinbaseClient{CoinbaseAdapter: adapter.NewCoinbaseAdapter(body.APIKey, body.Secret)}
	case "kraken":
		client = &arbitrage.KrakenClient{KrakenAdapter: adapter.NewKrakenAdapter(body.APIKey, body.Secret)}
	case "bitget":
		client = &arbitrage.BitgetClient{BitgetAdapter: adapter.NewBitgetAdapter(body.APIKey, body.Secret, body.Passphrase)}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported exchange: " + body.Name})
		return
	}

	engine.RegisterExchange(body.Name, client)
	c.JSON(http.StatusOK, gin.H{"status": "registered", "exchange": body.Name})
}

// ListArbitrageExchanges returns registered exchanges.
func ListArbitrageExchanges(c *gin.Context) {
	engine := GetArbEngine()
	stats := engine.GetStats()
	exchanges := []string{}
	if ex, ok := stats["exchanges"].(int); ok && ex > 0 {
		// We don't store names in stats, return count only
	}
	c.JSON(http.StatusOK, gin.H{
		"registered_count": stats["exchanges"],
		"exchanges":        exchanges,
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

	engine := GetArbEngine()
	opp := arbitrage.Opportunity{
		Symbol:       body.Symbol,
		BuyExchange:  body.BuyExchange,
		SellExchange: body.SellExchange,
		BuyPrice:     body.BuyPrice,
		SellPrice:    body.SellPrice,
		Timestamp:    time.Now().UnixMilli(),
	}
	opp.SpreadAbs = body.SellPrice - body.BuyPrice
	opp.SpreadPct = opp.SpreadAbs / body.BuyPrice * 100

	// Temporarily enable execution
	oldDryRun := engine.GetConfig().DryRun
	engine.SetDryRun(false)
	engine.Execute(opp)
	engine.SetDryRun(oldDryRun)

	c.JSON(http.StatusOK, gin.H{"status": "executed", "opportunity": opp})
}
