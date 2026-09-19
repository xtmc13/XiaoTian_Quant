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
			"币种套利利润 %.2f%%: %s @ %s",
			opp.NetProfitPct, strings.Join(opp.Cycle, " → "), opp.Exchange,
		))
	}

	engine.OnTrade = func(trade arbitrage.TriangularTrade) {
		broadcaster := notify.NewBroadcaster()
		if trade.Status == "dry_run" {
			broadcaster.System("triangular_dry_run", fmt.Sprintf(
				"币种套利模拟: %s 利润=%.2f",
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
	engine.IterateClients(func(_ string, client arbitrage.ExchangeClient) {
		client.WireToEventBus(bus)
		_ = client.StartMarketStream(engine.GetConfig().Symbols)
	})
}

func stopTriangularStreams(engine *arbitrage.TriangularEngine) {
	engine.IterateClients(func(_ string, client arbitrage.ExchangeClient) {
		_ = client.StopStream()
	})
}

func restartTriangularStreams(engine *arbitrage.TriangularEngine) {
	stopTriangularStreams(engine)
	startTriangularStreams(engine)
}

// autoRegisterTriangularExchange registers a client for every configured
// exchange that has credentials; missing ones are skipped silently.
func autoRegisterTriangularExchange(engine *arbitrage.TriangularEngine) {
	for _, exName := range engine.GetConfig().ExchangeList() {
		apiKey, secret, passphrase, testnet := getExchangeCredentials(exName)
		if apiKey == "" || secret == "" {
			continue
		}
		client, err := createArbitrageClient(exName, apiKey, secret, passphrase, testnet)
		if err != nil {
			continue
		}
		engine.RegisterClient(exName, client)
	}
}

// triTradesForUser 按属主过滤三角套利仓位/历史：admin/未注入用户看全部，
// 普通用户看本人 + 历史无属主(UserID=0)。
func triTradesForUser(c *gin.Context, trades []*arbitrage.TriangularTrade) []*arbitrage.TriangularTrade {
	uid, injected := ctxUserID(c)
	if !injected || ctxIsAdmin(c) {
		return trades
	}
	filtered := make([]*arbitrage.TriangularTrade, 0, len(trades))
	for _, t := range trades {
		if t.UserID == 0 || t.UserID == int64(uid) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// ── Config ─────────────────────────────────────────────────────

// GetTriangularConfig returns current triangular arbitrage configuration.
func GetTriangularConfig(c *gin.Context) {
	engine := GetTriangularEngine()
	c.JSON(http.StatusOK, gin.H{"config": engine.GetConfig()})
}

// UpdateTriangularConfig updates triangular arbitrage configuration.
// 引擎是全局系统资源（配置存 config.yaml）：仅 admin 可改（未注入用户保持单用户兼容）。
func UpdateTriangularConfig(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var body arbitrage.TriangularEngineConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Normalize: migrate legacy single exchange into the exchanges list.
	if len(body.Exchanges) == 0 && body.Exchange != "" {
		body.Exchanges = []string{body.Exchange}
	}
	if len(body.Exchanges) > 0 {
		body.Exchange = body.Exchanges[0]
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
// 全局系统资源：仅 admin 可启停（未注入用户保持单用户兼容）。
func StartTriangular(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
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
// 全局系统资源：仅 admin 可启停（未注入用户保持单用户兼容）。
func StopTriangular(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
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
// 按属主过滤：非 admin 只见本人 + 历史无属主仓位。
func GetTriangularPositions(c *gin.Context) {
	engine := GetTriangularEngine()
	positions := triTradesForUser(c, engine.GetPositions())
	if positions == nil {
		positions = []*arbitrage.TriangularTrade{}
	}
	c.JSON(http.StatusOK, gin.H{"positions": positions, "count": len(positions)})
}

// GetTriangularHistory returns completed triangular trade history.
// 按属主过滤：非 admin 只见本人 + 历史无属主记录。
func GetTriangularHistory(c *gin.Context) {
	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	engine := GetTriangularEngine()
	history := triTradesForUser(c, engine.GetHistory(limit))
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
	// 记录执行属主：新产生的套利记录归当前用户（0=系统，未注入时保持现状）。
	if uid, injected := ctxUserID(c); injected {
		engine.SetOwnerUserID(int64(uid))
	} else {
		engine.SetOwnerUserID(0)
	}

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
// 存在但属他人（且非 admin）→ 403。
func CloseTriangularPosition(c *gin.Context) {
	id := c.Param("id")
	engine := GetTriangularEngine()
	for _, t := range engine.GetPositions() {
		if t.ID == id {
			if !requireOwner(c, t.UserID) {
				return
			}
			break
		}
	}
	if err := engine.ClosePosition(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "closed", "id": id})
}

// FailTriangularPosition marks an active triangular trade as failed.
// 存在但属他人（且非 admin）→ 403。
func FailTriangularPosition(c *gin.Context) {
	id := c.Param("id")
	engine := GetTriangularEngine()
	for _, t := range engine.GetPositions() {
		if t.ID == id {
			if !requireOwner(c, t.UserID) {
				return
			}
			break
		}
	}
	if err := engine.FailPosition(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "failed", "id": id})
}
