package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/protection"
)

// ProtectionManager is the global protection manager instance.
var ProtectionManager *protection.ProtectionManager

func init() {
	ProtectionManager = protection.NewProtectionManager()
}

// GetProtectionStatus returns the current protection status.
func GetProtectionStatus(c *gin.Context) {
	if ProtectionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "protection manager not initialized"})
		return
	}

	status := ProtectionManager.Status(time.Now())
	c.JSON(http.StatusOK, status)
}

// ConfigureProtection sets up protections from JSON configuration.
func ConfigureProtection(c *gin.Context) {
	var body struct {
		Protections []struct {
			Name   string         `json:"name"`
			Params map[string]any `json:"params"`
		} `json:"protections"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// Build new manager
	cfg := protection.Config{
		Protections: make([]protection.ProtectionConfig, 0, len(body.Protections)),
	}
	for _, p := range body.Protections {
		cfg.Protections = append(cfg.Protections, protection.ProtectionConfig{
			Name:   p.Name,
			Params: p.Params,
		})
	}

	mgr, err := protection.BuildManagerFromConfig(cfg)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ProtectionManager = mgr

	c.JSON(http.StatusOK, gin.H{
		"status":      "configured",
		"protections": mgr.Protections(),
	})
}

// ResetProtection clears all protection blocks.
func ResetProtection(c *gin.Context) {
	if ProtectionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "protection manager not initialized"})
		return
	}

	scope := c.Query("scope")
	symbol := c.Query("symbol")

	switch scope {
	case "global":
		ProtectionManager.ResetGlobal()
	case "pair":
		if symbol == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "symbol required for pair scope"})
			return
		}
		ProtectionManager.ResetPair(symbol)
	default:
		ProtectionManager.ResetAll()
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "reset",
		"scope":   scope,
		"symbol":  symbol,
	})
}

// RecordTrade records a trade for protection evaluation.
func RecordTrade(c *gin.Context) {
	if ProtectionManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "protection manager not initialized"})
		return
	}

	var body struct {
		Symbol     string  `json:"symbol"`
		Side       string  `json:"side"`
		EntryPrice float64 `json:"entry_price"`
		ExitPrice  float64 `json:"exit_price"`
		Quantity   float64 `json:"quantity"`
		PnL        float64 `json:"pnl"`
		PnLPct     float64 `json:"pnl_pct"`
		IsStoploss bool    `json:"is_stoploss"`
		ExitTime   int64   `json:"exit_time"` // Unix milliseconds
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	exitTime := time.Now()
	if body.ExitTime > 0 {
		exitTime = time.UnixMilli(body.ExitTime)
	}

	trade := protection.TradeRecord{
		Symbol:     body.Symbol,
		Side:       body.Side,
		EntryPrice: body.EntryPrice,
		ExitPrice:  body.ExitPrice,
		Quantity:   body.Quantity,
		PnL:        body.PnL,
		PnLPct:     body.PnLPct,
		IsStoploss: body.IsStoploss,
		ExitTime:   exitTime,
	}

	// Notify all protections that support trade recording
	for _, p := range ProtectionManager.Protections() {
		switch prot := p.(type) {
		case *protection.CooldownPeriod:
			prot.RecordExit(body.Symbol, exitTime)
		case *protection.StoplossGuard:
			if body.IsStoploss {
				prot.RecordStoploss(body.Symbol, exitTime)
			}
		case *protection.LowProfitPairs:
			prot.RecordTrade(trade)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "recorded",
		"trade":  trade,
	})
}
