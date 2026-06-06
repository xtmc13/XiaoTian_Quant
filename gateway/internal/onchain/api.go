package onchain

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers on-chain data HTTP endpoints.
func RegisterRoutes(r *gin.RouterGroup, client MetricsClient) {
	h := &handler{client: client}

	r.GET("/eth/metrics", h.getETHMetrics)
	r.GET("/btc/metrics", h.getBTCMetrics)
	r.GET("/exchange-flow", h.getExchangeFlow)
	r.GET("/whale-alerts", h.getWhaleAlerts)
	r.GET("/signal/btc", h.getBTCSignal)
	r.GET("/signal/eth", h.getETHSignal)
}

type handler struct {
	client MetricsClient
}

func (h *handler) getETHMetrics(c *gin.Context) {
	metrics, err := h.client.GetETHMetrics()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, metrics)
}

func (h *handler) getBTCMetrics(c *gin.Context) {
	metrics, err := h.client.GetBTCMetrics()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, metrics)
}

func (h *handler) getExchangeFlow(c *gin.Context) {
	exchange := c.Query("exchange")
	if exchange == "" {
		exchange = "binance"
	}
	// Type assertion to access GetExchangeFlow
	if fc, ok := h.client.(interface{ GetExchangeFlow(string) (*ExchangeFlow, error) }); ok {
		flow, err := fc.GetExchangeFlow(exchange)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, flow)
		return
	}
	c.JSON(http.StatusOK, gin.H{"exchange": exchange, "note": "exchange flow not available for this client"})
}

func (h *handler) getWhaleAlerts(c *gin.Context) {
	minUSD, _ := strconv.ParseFloat(c.DefaultQuery("min_usd", "1000000"), 64)
	if fc, ok := h.client.(interface{ GetWhaleAlerts(float64) ([]WhaleAlert, error) }); ok {
		alerts, err := fc.GetWhaleAlerts(minUSD)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"alerts": alerts})
		return
	}
	c.JSON(http.StatusOK, gin.H{"alerts": []WhaleAlert{}})
}

func (h *handler) getBTCSignal(c *gin.Context) {
	agg := NewAggregator(h.client)
	signal, err := agg.GetBTCOnChainSignal()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, signal)
}

func (h *handler) getETHSignal(c *gin.Context) {
	agg := NewAggregator(h.client)
	signal, err := agg.GetETHOnChainSignal()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, signal)
}
