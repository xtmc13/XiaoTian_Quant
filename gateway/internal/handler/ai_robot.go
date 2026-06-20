package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ── Types ──

type aiStatusResp struct {
	SignalsToday    int     `json:"signals_today"`
	AvgConfidence   float64 `json:"avg_confidence"`
	FilterRate      float64 `json:"filter_rate"`
	WinRate         float64 `json:"win_rate"`
	Model           string  `json:"model"`
	ScanInterval    int     `json:"scan_interval"`
	MarketFilter    bool    `json:"market_filter"`
	ConfidenceThreshold float64 `json:"confidence_threshold"`
	UpdatedAt       int64   `json:"updated_at"`
}

type aiSignal struct {
	ID              string   `json:"id"`
	Symbol          string   `json:"symbol"`
	Signal          string   `json:"signal"` // buy/sell/neutral
	Confidence      float64  `json:"confidence"`
	Reason          string   `json:"reason"`
	Filters         []string `json:"filters"`
	MarketCondition string   `json:"market_condition"`
	Timestamp       int64    `json:"timestamp"`
}

// AIRobotStatus godoc
// GET /ai/status
func AIRobotStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": aiStatusResp{
			SignalsToday:        0,
			AvgConfidence:       0,
			FilterRate:          0,
			WinRate:             0,
			Model:               "deepseek",
			ScanInterval:        300,
			MarketFilter:        true,
			ConfidenceThreshold: 60,
			UpdatedAt:           time.Now().Unix(),
		},
	})
}

// AISignals godoc
// GET /ai/signals
func AISignals(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"signals": []aiSignal{},
	})
}
