package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Types ──

type aiStatusResp struct {
	SignalsToday        int     `json:"signals_today"`
	AvgConfidence       float64 `json:"avg_confidence"`
	FilterRate          float64 `json:"filter_rate"`
	WinRate             float64 `json:"win_rate"`
	Model               string  `json:"model"`
	ScanInterval        int     `json:"scan_interval"`
	MarketFilter        bool    `json:"market_filter"`
	ConfidenceThreshold float64 `json:"confidence_threshold"`
	UpdatedAt           int64   `json:"updated_at"`
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

// AIRobotStatus returns real-time AI robot statistics computed from signals and trades.
func AIRobotStatus(c *gin.Context) {
	userID := aiBotUserID(c)

	// Default config values until /ai-robot/config is fully wired.
	confidenceThreshold := 60.0
	model := "deepseek"
	scanInterval := 300
	marketFilter := true

	// Count today's signals from social_signals.
	signalsToday := 0
	totalSignals := 0
	avgConfidence := 0.0
	filteredCount := 0

	// Query recent signals. Since there is no dedicated AI robot signals table,
	// we aggregate from social_signals for all providers.
	if db := store.GetDB(); db != nil {
		rows, err := db.Query("SELECT direction, confidence, reason FROM social_signals WHERE timestamp >= ? ORDER BY timestamp DESC LIMIT 200", (time.Now().Unix()-24*3600)*1000)
		if err == nil {
			defer rows.Close()
			var confidences []float64
			for rows.Next() {
				var direction, reason string
				var confidence float64
				if err := rows.Scan(&direction, &confidence, &reason); err != nil {
					continue
				}
				totalSignals++
				if confidence >= confidenceThreshold {
					signalsToday++
				}
				if reason != "" {
					filteredCount++
				}
				confidences = append(confidences, confidence)
			}
			if len(confidences) > 0 {
				var sum float64
				for _, c := range confidences {
					sum += c
				}
				avgConfidence = sum / float64(len(confidences))
			}
		}
	}

	// Compute win rate from AI bot trades over the last 30 days.
	winRate := 0.0
	if userID > 0 {
		instances := store.GetAIBotInstances(userID)
		var totalTrades, winTrades int
		since := time.Now().AddDate(0, 0, -30).Unix()
		for _, inst := range instances {
			id := getString(inst, "id", "")
			if id == "" {
				continue
			}
			trades := store.GetAIBotTrades(id, 1000)
			for _, t := range trades {
				closedAtVal := t["closed_at"]
				var closedAt int64
				switch v := closedAtVal.(type) {
				case int64:
					closedAt = v
				case int:
					closedAt = int64(v)
				case float64:
					closedAt = int64(v)
				}
				if closedAt > 0 && closedAt >= since {
					totalTrades++
					if getFloat(t, "pnl", 0) > 0 {
						winTrades++
					}
				}
			}
		}
		if totalTrades > 0 {
			winRate = float64(winTrades) / float64(totalTrades) * 100
		}
	}

	filterRate := 0.0
	if totalSignals > 0 {
		filterRate = float64(filteredCount) / float64(totalSignals) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": aiStatusResp{
			SignalsToday:        signalsToday,
			AvgConfidence:       avgConfidence,
			FilterRate:          filterRate,
			WinRate:             winRate,
			Model:               model,
			ScanInterval:        scanInterval,
			MarketFilter:        marketFilter,
			ConfidenceThreshold: confidenceThreshold,
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
