package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ── Types ──

type contractMarginInfo struct {
	Leverage           float64 `json:"leverage"`
	AvailableMargin    float64 `json:"available_margin"`
	MarginRatio        float64 `json:"margin_ratio"`
	LiquidationPrice   float64 `json:"liquidation_price"`
	Direction          string  `json:"direction"`
	MarginMode         string  `json:"margin_mode"`
	MaxPositions       int     `json:"max_positions"`
}

type liquidationPriceReq struct {
	EntryPrice float64 `json:"entry_price" binding:"required"`
	Side       string  `json:"side" binding:"required"`
	Leverage   float64 `json:"leverage" binding:"required,min=1,max=125"`
}

type contractParamsReq struct {
	Leverage         float64 `json:"leverage" binding:"min=1,max=125"`
	Direction        string  `json:"direction" binding:"oneof=long short both"`
	MarginMode       string  `json:"margin_mode" binding:"oneof=isolated cross"`
	OpenIndicator    string  `json:"open_indicator" binding:"oneof=macd_golden macd_death ema_counter ema_follow none"`
	IndicatorTimeframe string `json:"indicator_timeframe" binding:"oneof=5m 15m 30m 1h 4h 8h"`
	EnableTrendFollowing bool  `json:"enable_trend_following"`
	MaxPositions     int     `json:"max_positions" binding:"min=1,max=50"`
}

// ContractLeverageGet godoc
// GET /contract/leverage
func ContractLeverageGet(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"leverage": 20,
	})
}

// ContractLeverageSet godoc
// POST /contract/leverage
func ContractLeverageSet(c *gin.Context) {
	var req struct {
		Leverage float64 `json:"leverage" binding:"required,min=1,max=125"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ContractMarginInfo godoc
// GET /contract/margin
func ContractMarginInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": contractMarginInfo{
			Leverage:         20,
			AvailableMargin:  0,
			MarginRatio:      1.0,
			LiquidationPrice: 0,
			Direction:        "both",
			MarginMode:       "cross",
			MaxPositions:     10,
		},
	})
}

// ContractLiquidationPrice godoc
// GET /contract/liquidation-price
func ContractLiquidationPrice(c *gin.Context) {
	entryPrice, _ := strconv.ParseFloat(c.Query("entry_price"), 64)
	side := c.Query("side")
	leverage, _ := strconv.ParseFloat(c.Query("leverage"), 64)

	if entryPrice <= 0 || leverage <= 0 || side == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid params"})
		return
	}

	// Simplified liquidation price calculation
	var liqPrice float64
	if side == "LONG" || side == "long" {
		liqPrice = entryPrice * (1 - 0.9/leverage)
	} else {
		liqPrice = entryPrice * (1 + 0.9/leverage)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"entry_price":       entryPrice,
			"side":              side,
			"leverage":          leverage,
			"liquidation_price": liqPrice,
		},
	})
}

// ContractParamsSave godoc
// POST /contract/params
func ContractParamsSave(c *gin.Context) {
	var req contractParamsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ContractParamsGet godoc
// GET /contract/params
func ContractParamsGet(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": contractParamsReq{
			Leverage:             20,
			Direction:            "both",
			MarginMode:           "cross",
			OpenIndicator:        "macd_golden",
			IndicatorTimeframe:   "1h",
			EnableTrendFollowing: false,
			MaxPositions:         10,
		},
	})
}
