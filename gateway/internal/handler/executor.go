package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ── Types ──

type executorStatusResp struct {
	Status          string  `json:"status"`
	ActivePositions int     `json:"active_positions"`
	PendingSignals  int     `json:"pending_signals"`
	TodayExecuted   int     `json:"today_executed"`
	TodayPnL        float64 `json:"today_pnl"`
	Tp1Executed     int     `json:"tp1_executed"`
	Tp2Executed     int     `json:"tp2_executed"`
	Tp3Executed     int     `json:"tp3_executed"`
	SlTriggered     int     `json:"sl_triggered"`
	UpdatedAt       int64   `json:"updated_at"`
}

type executorPosition struct {
	ID             string  `json:"id"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	EntryPrice     float64 `json:"entry_price"`
	CurrentPrice   float64 `json:"current_price"`
	Quantity       float64 `json:"quantity"`
	RealizedPnL    float64 `json:"realized_pnl"`
	UnrealizedPnL  float64 `json:"unrealized_pnl"`
	Tp1Price       float64 `json:"tp1_price,omitempty"`
	Tp2Price       float64 `json:"tp2_price,omitempty"`
	Tp3Price       float64 `json:"tp3_price,omitempty"`
	SlPrice        float64 `json:"sl_price,omitempty"`
	Tp1Hit         bool    `json:"tp1_hit"`
	Tp2Hit         bool    `json:"tp2_hit"`
	Tp3Hit         bool    `json:"tp3_hit"`
	TrailingActive bool    `json:"trailing_active"`
	CurrentTP      float64 `json:"current_tp"`
	CurrentSL      float64 `json:"current_sl"`
}

type executionRecord struct {
	ID         string  `json:"id"`
	Symbol     string  `json:"symbol"`
	Type       string  `json:"type"`
	Side       string  `json:"side"`
	Price      float64 `json:"price"`
	PnL        float64 `json:"pnl"`
	ExecutedAt int64   `json:"executed_at"`
}

type signalSource struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	Enabled        bool            `json:"enabled"`
	WebhookURL     string          `json:"webhook_url,omitempty"`
	SignalCountToday   int         `json:"signal_count_today"`
	SignalCountTotal   int         `json:"signal_count_total"`
	TPSLConfig     *tpslConfig     `json:"tp_sl_config,omitempty"`
}

type tpslConfig struct {
	Tp1Pct float64 `json:"tp1_pct"`
	Tp2Pct float64 `json:"tp2_pct"`
	Tp3Pct float64 `json:"tp3_pct"`
	SlPct  float64 `json:"sl_pct"`
}

// ExecutorStatus godoc
// GET /executor/status
func ExecutorStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": executorStatusResp{
			Status:          "running",
			ActivePositions: 0,
			PendingSignals:  0,
			TodayExecuted:   0,
			TodayPnL:        0,
			Tp1Executed:     0,
			Tp2Executed:     0,
			Tp3Executed:     0,
			SlTriggered:     0,
			UpdatedAt:       time.Now().Unix(),
		},
	})
}

// ExecutorPositions godoc
// GET /executor/positions
func ExecutorPositions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"positions": []executorPosition{},
	})
}

// ExecutionRecords godoc
// GET /executor/records
func ExecutionRecords(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"records": []executionRecord{},
	})
}

// ExecutorSignalSources godoc
// GET /executor/signal-sources
func ExecutorSignalSources(c *gin.Context) {
	sources := []signalSource{
		{
			ID:               "webhook-default",
			Name:             "默认Webhook",
			Type:             "webhook",
			Enabled:          true,
			WebhookURL:       "https://api.xiaotianquant.com/webhook/signal/default",
			SignalCountToday: 0,
			SignalCountTotal: 0,
			TPSLConfig: &tpslConfig{
				Tp1Pct: 40,
				Tp2Pct: 30,
				Tp3Pct: 30,
				SlPct:  3,
			},
		},
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"sources": sources,
	})
}
