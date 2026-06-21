package handler

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/pythonstrategy"
)

// PythonStrategyClient is the global client for the Python strategy engine.
var PythonStrategyClient = pythonstrategy.NewClient("")

type pythonStrategyRequest struct {
	Mode     string         `json:"mode"` // indicator | script
	Code     string         `json:"code"`
	Symbol   string         `json:"symbol"`
	Interval string         `json:"interval"`
	Params   map[string]any `json:"params"`
	Bars     []barJSON      `json:"bars"`
}

type barJSON struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// RunPythonStrategy executes a Python strategy via the Python strategy engine.
func RunPythonStrategy(c *gin.Context) {
	var req pythonStrategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[pythonstrategy] bind error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "strategy code is required"})
		return
	}
	if req.Mode == "" {
		req.Mode = "indicator"
	}
	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}
	if req.Interval == "" {
		req.Interval = "1h"
	}

	if PythonStrategyClient == nil {
		log.Printf("[pythonstrategy] client not initialized")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "python strategy engine is not available"})
		return
	}

	bars := make([]model.Bar, len(req.Bars))
	for i, b := range req.Bars {
		bars[i] = model.Bar{
			Symbol:   req.Symbol,
			Interval: req.Interval,
			Time:     b.Time,
			Open:     b.Open,
			High:     b.High,
			Low:      b.Low,
			Close:    b.Close,
			Volume:   b.Volume,
		}
	}
	log.Printf("[pythonstrategy] running mode=%s symbol=%s bars=%d", req.Mode, req.Symbol, len(bars))

	var signals []pythonstrategy.Signal
	var err error
	switch req.Mode {
	case "indicator":
		signals, err = PythonStrategyClient.RunIndicatorStrategy(req.Code, bars, req.Params, req.Symbol, req.Interval)
	case "script":
		signals, err = PythonStrategyClient.RunScriptStrategy(req.Code, bars, req.Params, req.Symbol, req.Interval)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode must be 'indicator' or 'script'"})
		return
	}

	if err != nil {
		log.Printf("[pythonstrategy] execution error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[pythonstrategy] returned %d signals", len(signals))
	c.JSON(http.StatusOK, gin.H{
		"count":   len(signals),
		"signals": signals,
	})
}
