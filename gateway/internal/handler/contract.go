package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/adapter"
)

// ── Types ──

// contractMarginInfo 数值字段全部可空：网关没有接入交易所账户级保证金数据源，
// 无真实值时必须返回 null（前端显示 "--"），绝不编造（P1：合约页杠杆恒显 20 修复）。
// direction/margin_mode 为面板默认配置语义，非账户实时值。
type contractMarginInfo struct {
	Leverage         *float64 `json:"leverage"`
	AvailableMargin  *float64 `json:"available_margin"`
	MarginRatio      *float64 `json:"margin_ratio"`
	LiquidationPrice *float64 `json:"liquidation_price"`
	Direction        string   `json:"direction"`
	MarginMode       string   `json:"margin_mode"`
	MaxPositions     *int     `json:"max_positions"`
}

type liquidationPriceReq struct {
	EntryPrice float64 `json:"entry_price" binding:"required"`
	Side       string  `json:"side" binding:"required"`
	Leverage   float64 `json:"leverage" binding:"required,min=1,max=125"`
}

type contractParamsReq struct {
	Leverage             float64 `json:"leverage" binding:"min=1,max=125"`
	Direction            string  `json:"direction" binding:"oneof=long short both"`
	MarginMode           string  `json:"margin_mode" binding:"oneof=isolated cross"`
	OpenIndicator        string  `json:"open_indicator" binding:"oneof=macd_golden macd_death ema_counter ema_follow none"`
	IndicatorTimeframe   string  `json:"indicator_timeframe" binding:"oneof=5m 15m 30m 1h 4h 8h"`
	EnableTrendFollowing bool    `json:"enable_trend_following"`
	MaxPositions         int     `json:"max_positions" binding:"min=1,max=50"`
}

// ContractLeverageGet godoc
// GET /contract/leverage —— 用户杠杆无持久化/交易所账户数据源，返回 null
// （前端显示 "--"），绝不返回写死的 20（P1 修复）。
func ContractLeverageGet(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"leverage": nil,
	})
}

// ContractLeverageSet godoc
// POST /contract/leverage
//
// The leverage is applied on the exchange itself. A 200 response means the
// venue acknowledged the change; nothing is persisted locally, so GET
// /contract/leverage intentionally keeps returning null.
func ContractLeverageSet(c *gin.Context) {
	var req struct {
		Leverage float64 `json:"leverage" binding:"required,min=1,max=125"`
		Symbol   string  `json:"symbol"`
		Exchange string  `json:"exchange"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	req.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	req.Exchange = strings.ToLower(strings.TrimSpace(req.Exchange))
	if req.Exchange == "" {
		req.Exchange = "binance"
	}
	if req.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "symbol 必填（如 BTCUSDT）"})
		return
	}
	leverage := int(req.Leverage)
	if float64(leverage) != req.Leverage {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "leverage 必须为整数"})
		return
	}

	switch req.Exchange {
	case "binance", "bybit":
		apiKey, secret, _ := adapter.GetCredential(req.Exchange)
		if apiKey == "" || secret == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": req.Exchange + " API Key 未配置"})
			return
		}
		var err error
		if req.Exchange == "binance" {
			err = adapter.NewBinanceAdapter(apiKey, secret, false).SetLeverage(req.Symbol, leverage)
		} else {
			err = adapter.NewBybitAdapter(apiKey, secret, false).SetLeverage(req.Symbol, leverage)
		}
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": "设置杠杆失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"exchange": req.Exchange,
			"symbol":   req.Symbol,
			"leverage": leverage,
			"message":  "杠杆已在交易所生效",
		})
	case "okx":
		// OKX carries lever on each order; accepting a global setting here
		// without persisting it would be another fake success.
		c.JSON(http.StatusNotImplemented, gin.H{
			"success": false,
			"message": "OKX 杠杆随下单参数生效，暂不支持账户级预设",
		})
	default:
		c.JSON(http.StatusNotImplemented, gin.H{
			"success": false,
			"message": req.Exchange + " 暂不支持应用内设置杠杆",
		})
	}
}

// ContractMarginInfo godoc
// GET /contract/margin
func ContractMarginInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": contractMarginInfo{
			// 无交易所账户级数据源——数值全部 null（不编造）。
			Leverage:         nil,
			AvailableMargin:  nil,
			MarginRatio:      nil,
			LiquidationPrice: nil,
			Direction:        "both",
			MarginMode:       "cross",
			MaxPositions:     nil,
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
