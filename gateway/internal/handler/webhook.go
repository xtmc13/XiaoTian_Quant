package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// TradingViewWebhook receives alerts from TradingView Pine Script strategies.
//
// TradingView setup:
//   Alert → Webhook URL: http://your-server:8080/api/webhook/tv
//   Message format (JSON):
//     {"symbol":"BTCUSDT","action":"buy","price":"50000","quantity":"0.1","strategy":"TV_Strategy"}
//
// The webhook automatically places paper/real orders through the order manager.
func TradingViewWebhook(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	symbol := getString(body, "symbol", getString(body, "ticker", ""))
	action := strings.ToLower(getString(body, "action", getString(body, "side", "")))
	quantity := getFloat(body, "quantity", getFloat(body, "qty", 0.01))

	if symbol == "" || action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol and action required"})
		return
	}

	// Map TradingView action to order side
	var side string
	switch action {
	case "buy", "long", "enter_long":
		side = "BUY"
	case "sell", "short", "enter_short":
		side = "SELL"
	case "exit", "close", "exit_long", "exit_short", "flatten":
		// Close position — handled by order manager via cancel-all + market exit
		c.JSON(http.StatusOK, gin.H{"status": "ok", "action": "close", "symbol": symbol, "msg": "close signal received"})
		log.Printf("[webhook] TV signal: CLOSE %s", symbol)
		// TODO: call portfolio manager to close position
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown action: %s", action)})
		return
	}

	// Get price from alert or use 0 for market order
	price := getFloat(body, "price", getFloat(body, "limit", 0))
	orderType := "MARKET"
	if price > 0 {
		orderType = "LIMIT"
	}

	// Build order
	order := map[string]any{
		"symbol":   strings.ToUpper(symbol),
		"side":     side,
		"type":     orderType,
		"price":    price,
		"quantity": quantity,
		"source":   "tradingview",
		"strategy": getString(body, "strategy", "TV_Strategy"),
	}

	// Execute through the order fill pipeline
	fillOrderAndUpdatePortfolio(order)

	log.Printf("[webhook] TV signal: %s %s %s qty=%.4f price=%.2f",
		order["strategy"], side, symbol, quantity, price)

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"order":  order,
	})
}

// GenericWebhook receives signals from any external source (3Commas, custom bots, etc).
//
// POST /api/webhook/generic
// Body: {"symbol":"BTCUSDT","side":"BUY","type":"MARKET","quantity":0.1,"price":0}
func GenericWebhook(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	symbol := strings.ToUpper(getString(body, "symbol", ""))
	side := strings.ToUpper(getString(body, "side", "BUY"))
	orderType := strings.ToUpper(getString(body, "type", "MARKET"))
	price := getFloat(body, "price", 0)
	quantity := getFloat(body, "quantity", 0.01)

	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol required"})
		return
	}
	if quantity <= 0 {
		quantity = 0.01
	}

	order := map[string]any{
		"symbol":   symbol,
		"side":     side,
		"type":     orderType,
		"price":    price,
		"quantity": quantity,
		"source":   getString(body, "source", "webhook"),
	}

	fillOrderAndUpdatePortfolio(order)

	log.Printf("[webhook] generic: %s %s %s qty=%.4f", side, orderType, symbol, quantity)

	c.JSON(http.StatusOK, gin.H{"status": "ok", "order": order})
}
