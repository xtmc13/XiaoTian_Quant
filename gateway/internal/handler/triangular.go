package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/arbitrage"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Global Triangular Arbitrage Engine ──────────────────────────

var triangularEngine *arbitrage.TriangularEngine

// GetTriangularEngine returns the global triangular arbitrage engine (lazy init).
func GetTriangularEngine() *arbitrage.TriangularEngine {
	if triangularEngine == nil {
		cfg := arbitrage.DefaultTriangularEngineConfig()
		if saved := store.LoadTriangularConfig(); saved != nil {
			if data, err := json.Marshal(saved); err == nil {
				_ = json.Unmarshal(data, &cfg)
			}
		}
		triangularEngine = arbitrage.NewTriangularEngine(cfg)
		autoRegisterTriangularExchange(triangularEngine)
	}
	return triangularEngine
}

func wireTriangularCallbacks(engine *arbitrage.TriangularEngine) {
	engine.OnOpportunity = func(opp arbitrage.TriangularOpportunity) {
		broadcaster := notify.NewBroadcaster()
		broadcaster.System("triangular_opportunity", fmt.Sprintf(
			"Triangular profit %.2f%%: %s on %s",
			opp.NetProfitPct, strings.Join(opp.Cycle, " → "), opp.Exchange,
		))
	}

	engine.OnTrade = func(trade arbitrage.TriangularTrade) {
		broadcaster := notify.NewBroadcaster()
		if trade.Status == "dry_run" {
			broadcaster.System("triangular_dry_run", fmt.Sprintf(
				"Simulated triangular: %s profit=%.2f",
				strings.Join(trade.Cycle, " → "), trade.NetProfit,
			))
		} else {
			broadcaster.Trade(trade.Cycle[0], "TRI", 0, trade.StartQty, trade.NetProfit)
		}
	}
}

func startTriangularStreams(engine *arbitrage.TriangularEngine) {
	bus := app.Get().EventBus
	if bus == nil {
		return
	}
	client := engine.GetClient()
	if client == nil {
		return
	}
	client.WireToEventBus(bus)
	_ = client.StartMarketStream(engine.GetConfig().Symbols)
}

func stopTriangularStreams(engine *arbitrage.TriangularEngine) {
	if client := engine.GetClient(); client != nil {
		_ = client.StopStream()
	}
}

func restartTriangularStreams(engine *arbitrage.TriangularEngine) {
	stopTriangularStreams(engine)
	startTriangularStreams(engine)
}

func autoRegisterTriangularExchange(engine *arbitrage.TriangularEngine) {
	cfg := engine.GetConfig()
	if cfg.Exchange == "" {
		return
	}
	apiKey, secret, passphrase, testnet := getExchangeCredentials(cfg.Exchange)
	if apiKey == "" || secret == "" {
		return
	}
	client, err := createArbitrageClient(cfg.Exchange, apiKey, secret, passphrase, testnet)
	if err != nil {
		return
	}
	engine.RegisterClient(client)
}

// ── Config ─────────────────────────────────────────────────────

// GetTriangularConfig returns current triangular arbitrage configuration.
func GetTriangularConfig(c *gin.Context) {
	engine := GetTriangularEngine()
	c.JSON(http.StatusOK, gin.H{"config": engine.GetConfig()})
}

// UpdateTriangularConfig updates triangular arbitrage configuration.
func UpdateTriangularConfig(c *gin.Context) {
	var body arbitrage.TriangularEngineConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	wasRunning := triangularEngine != nil && triangularEngine.IsRunning()
	if wasRunning {
		triangularEngine.Stop()
		stopTriangularStreams(triangularEngine)
	}

	triangularEngine = arbitrage.NewTriangularEngine(body)
	autoRegisterTriangularExchange(triangularEngine)
	wireTriangularCallbacks(triangularEngine)
	if wasRunning {
		startTriangularStreams(triangularEngine)
		_ = triangularEngine.Start()
	}

	if data, err := json.Marshal(body); err == nil {
		var persisted map[string]any
		if err := json.Unmarshal(data, &persisted); err == nil {
			_ = store.SaveTriangularConfig(persisted)
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated", "config": body})
}

// ── Engine Control ─────────────────────────────────────────────

// StartTriangular starts the triangular arbitrage monitoring engine.
func StartTriangular(c *gin.Context) {
	engine := GetTriangularEngine()

	wireTriangularCallbacks(engine)
	startTriangularStreams(engine)

	if err := engine.Start(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "started", "exchange": engine.GetConfig().Exchange})
}

// StopTriangular stops the triangular arbitrage engine.
func StopTriangular(c *gin.Context) {
	engine := GetTriangularEngine()
	engine.Stop()
	stopTriangularStreams(engine)
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// GetTriangularStatus returns engine status and stats.
func GetTriangularStatus(c *gin.Context) {
	engine := GetTriangularEngine()
	c.JSON(http.StatusOK, gin.H{
		"running": engine.IsRunning(),
		"stats":   engine.GetStats(),
	})
}

// GetTriangularPerformance returns aggregated triangular arbitrage performance metrics.
func GetTriangularPerformance(c *gin.Context) {
	engine := GetTriangularEngine()
	c.JSON(http.StatusOK, engine.GetPerformance())
}

// ── Opportunities ───────────────────────────────────────────────

// GetTriangularOpportunity returns the latest detected opportunity.
func GetTriangularOpportunity(c *gin.Context) {
	engine := GetTriangularEngine()
	opp := engine.GetLastOpportunity()
	if opp == nil {
		c.JSON(http.StatusOK, gin.H{"opportunity": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"opportunity": opp})
}

// ── Positions & History ────────────────────────────────────────

// GetTriangularPositions returns active triangular trades.
func GetTriangularPositions(c *gin.Context) {
	engine := GetTriangularEngine()
	positions := engine.GetPositions()
	if positions == nil {
		positions = []*arbitrage.TriangularTrade{}
	}
	c.JSON(http.StatusOK, gin.H{"positions": positions, "count": len(positions)})
}

// GetTriangularHistory returns completed triangular trade history.
func GetTriangularHistory(c *gin.Context) {
	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	engine := GetTriangularEngine()
	history := engine.GetHistory(limit)
	if history == nil {
		history = []*arbitrage.TriangularTrade{}
	}
	c.JSON(http.StatusOK, gin.H{"history": history, "count": len(history)})
}

// ── Manual Execution ───────────────────────────────────────────

// ExecuteTriangular manually triggers a triangular arbitrage execution.
func ExecuteTriangular(c *gin.Context) {
	var body struct {
		Exchange string   `json:"exchange"`
		Cycle    []string `json:"cycle"`
		StartQty float64  `json:"start_qty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	engine := GetTriangularEngine()

	// Build a minimal opportunity from the request.
	opp := arbitrage.TriangularOpportunity{
		ID:         fmt.Sprintf("tri-manual-%d", time.Now().UnixMilli()),
		Exchange:   body.Exchange,
		Cycle:      body.Cycle,
		StartAsset: body.Cycle[0],
		StartQty:   body.StartQty,
		Viable:     true,
		Timestamp:  time.Now().UnixMilli(),
	}

	oldDryRun := engine.GetConfig().DryRun
	engine.SetDryRun(false)
	engine.Execute(opp)
	engine.SetDryRun(oldDryRun)

	c.JSON(http.StatusOK, gin.H{"status": "executed", "opportunity": opp})
}

// CloseTriangularPosition manually closes an active triangular trade.
func CloseTriangularPosition(c *gin.Context) {
	id := c.Param("id")
	engine := GetTriangularEngine()
	if err := engine.ClosePosition(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "closed", "id": id})
}

// FailTriangularPosition marks an active triangular trade as failed.
func FailTriangularPosition(c *gin.Context) {
	id := c.Param("id")
	engine := GetTriangularEngine()
	if err := engine.FailPosition(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "failed", "id": id})
}
