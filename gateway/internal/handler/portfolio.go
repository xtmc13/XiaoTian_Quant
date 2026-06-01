package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// PortfolioSummary returns real-time portfolio state.
func PortfolioSummary(c *gin.Context) {
	mgr := portfolio.GetManager()
	if mgr == nil {
		c.JSON(http.StatusOK, gin.H{"error": "portfolio not initialized"})
		return
	}

	// Load exchange configs
	cfg := store.GetConfig()
	exchanges := []gin.H{}
	if exMap, ok := cfg["exchanges"].(map[string]any); ok {
		for name, v := range exMap {
			ex, _ := v.(map[string]any)
			hasKey := ex["api_key"] != nil && ex["api_key"] != ""
			hasSecret := ex["secret"] != nil && ex["secret"] != ""
			tested, _ := ex["tested"].(bool)
			exchanges = append(exchanges, gin.H{
				"name":       name,
				"configured": hasKey && hasSecret,
				"connected":  tested,
				"enabled":    ex["enabled"],
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_equity":      mgr.TotalEquity(),
		"total_pnl":         mgr.TotalPnL(),
		"available_balance": mgr.AvailableBalance(),
		"margin_used":       mgr.MarginUsed(),
		"drawdown_pct":      mgr.Drawdown(),
		"net_exposure_pct":  mgr.NetExposure(),
		"position_count":    len(mgr.GetPositions()),
		"exchanges":         exchanges,
	})
}

// PortfolioPositions returns all current positions.
func PortfolioPositions(c *gin.Context) {
	mgr := portfolio.GetManager()
	if mgr == nil {
		c.JSON(http.StatusOK, gin.H{"positions": []any{}})
		return
	}

	positions := mgr.GetPositions()
	posList := make([]gin.H, 0, len(positions))
	for _, p := range positions {
		posList = append(posList, gin.H{
			"symbol":          p.Symbol,
			"quantity":        p.Quantity,
			"avg_entry_price": p.AvgEntryPrice,
			"current_price":   p.CurrentPrice,
			"unrealized_pnl":  p.UnrealizedPnL,
			"realized_pnl":    p.RealizedPnL,
		})
	}
	c.JSON(http.StatusOK, gin.H{"positions": posList})
}

// PortfolioSnapshots returns recent equity snapshots.
func PortfolioSnapshots(c *gin.Context) {
	mgr := portfolio.GetManager()
	if mgr == nil {
		c.JSON(http.StatusOK, gin.H{"snapshots": []any{}})
		return
	}

	snapshots := mgr.GetSnapshots()
	snapList := make([]gin.H, 0, len(snapshots))
	for _, s := range snapshots {
		snapList = append(snapList, gin.H{
			"timestamp":    s.Timestamp,
			"total_equity": s.TotalEquity,
			"drawdown":     s.Drawdown,
		})
	}
	c.JSON(http.StatusOK, gin.H{"snapshots": snapList})
}
