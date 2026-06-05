package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/pairlist"
)

// PairlistManager is the global pairlist manager instance.
var PairlistManager *pairlist.Manager

func init() {
	cfg := pairlist.DefaultManagerConfig()
	PairlistManager = pairlist.NewManager(cfg)
}

// GetPairlistWhitelist returns the current pairlist whitelist.
func GetPairlistWhitelist(c *gin.Context) {
	exchange := c.Query("exchange")
	quoteAsset := c.Query("quote_asset")
	if exchange == "" {
		exchange = "binance"
	}
	if quoteAsset == "" {
		quoteAsset = "USDT"
	}

	if PairlistManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pairlist manager not initialized"})
		return
	}

	result, err := PairlistManager.Whitelist(exchange, quoteAsset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"exchange":    exchange,
		"quote_asset": quoteAsset,
		"pairs":       result,
		"count":       len(result),
		"last_update": PairlistManager.LastUpdate(),
	})
}

// RefreshPairlist forces a refresh of the pairlist whitelist.
func RefreshPairlist(c *gin.Context) {
	exchange := c.Query("exchange")
	quoteAsset := c.Query("quote_asset")
	if exchange == "" {
		exchange = "binance"
	}
	if quoteAsset == "" {
		quoteAsset = "USDT"
	}

	if PairlistManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pairlist manager not initialized"})
		return
	}

	result, err := PairlistManager.Refresh(exchange, quoteAsset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"exchange":    exchange,
		"quote_asset": quoteAsset,
		"pairs":       result,
		"count":       len(result),
		"refreshed":   true,
	})
}

// GetPairlistConfig returns the current pairlist configuration.
func GetPairlistConfig(c *gin.Context) {
	if PairlistManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pairlist manager not initialized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"producers": PairlistManager.Producers(),
		"filters":   PairlistManager.Filters(),
		"cached":    PairlistManager.Cached(),
		"last_update": PairlistManager.LastUpdate(),
	})
}

// ConfigurePairlist sets up the pairlist chain from a JSON configuration.
func ConfigurePairlist(c *gin.Context) {
	var body struct {
		Producers []struct {
			Name   string         `json:"name"`
			Params map[string]any `json:"params"`
		} `json:"producers"`
		Filters []struct {
			Name   string         `json:"name"`
			Params map[string]any `json:"params"`
		} `json:"filters"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if PairlistManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pairlist manager not initialized"})
		return
	}

	// Rebuild the manager with new configuration
	cfg := pairlist.DefaultManagerConfig()
	newManager := pairlist.NewManager(cfg)

	// Add producers
	for _, pc := range body.Producers {
		switch pc.Name {
		case "StaticPairList":
			raw, ok := pc.Params["pairs"].([]any)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "StaticPairList requires 'pairs' array"})
				return
			}
			pairs := make([]string, 0, len(raw))
			for _, v := range raw {
				if s, ok := v.(string); ok {
					pairs = append(pairs, s)
				}
			}
			newManager.AddProducer(pairlist.NewStaticPairList(pairs))
		case "VolumePairList":
			topN := 30
			minVol := 0.0
			if v, ok := pc.Params["top_n"].(float64); ok {
				topN = int(v)
			}
			if v, ok := pc.Params["min_volume"].(float64); ok {
				minVol = v
			}
			newManager.AddProducer(pairlist.NewVolumePairList(topN, minVol, nil))
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown producer: " + pc.Name})
			return
		}
	}

	// Add filters
	for _, fc := range body.Filters {
		switch fc.Name {
		case "PriceFilter":
			minPrice := 0.0
			maxPrice := 0.0
			if v, ok := fc.Params["min_price"].(float64); ok {
				minPrice = v
			}
			if v, ok := fc.Params["max_price"].(float64); ok {
				maxPrice = v
			}
			newManager.AddFilter(pairlist.NewPriceFilter(minPrice, maxPrice))
		case "SpreadFilter":
			maxSpread := 0.5
			if v, ok := fc.Params["max_spread_pct"].(float64); ok {
				maxSpread = v
			}
			newManager.AddFilter(pairlist.NewSpreadFilter(maxSpread))
		case "VolatilityFilter":
			minVol := 0.0
			maxVol := 0.0
			if v, ok := fc.Params["min_volatility_pct"].(float64); ok {
				minVol = v
			}
			if v, ok := fc.Params["max_volatility_pct"].(float64); ok {
				maxVol = v
			}
			newManager.AddFilter(pairlist.NewVolatilityFilter(minVol, maxVol))
		case "PrecisionFilter":
			minPricePrec := 0
			minQtyPrec := 0
			if v, ok := fc.Params["min_price_precision"].(float64); ok {
				minPricePrec = int(v)
			}
			if v, ok := fc.Params["min_qty_precision"].(float64); ok {
				minQtyPrec = int(v)
			}
			newManager.AddFilter(pairlist.NewPrecisionFilter(minPricePrec, minQtyPrec))
		case "MaxPairsFilter":
			maxPairs := 0
			if v, ok := fc.Params["max_pairs"].(float64); ok {
				maxPairs = int(v)
			}
			newManager.AddFilter(pairlist.NewMaxPairsFilter(maxPairs))
		case "ShuffleFilter":
			seed := int64(0)
			if v, ok := fc.Params["seed"].(float64); ok {
				seed = int64(v)
			}
			newManager.AddFilter(pairlist.NewShuffleFilter(seed))
		case "CorrelationFilter":
			maxCorr := 2
			if v, ok := fc.Params["max_correlated"].(float64); ok {
				maxCorr = int(v)
			}
			newManager.AddFilter(pairlist.NewCorrelationFilter(maxCorr))
		case "AgeFilter":
			minAge := 0
			if v, ok := fc.Params["min_age_days"].(float64); ok {
				minAge = int(v)
			}
			newManager.AddFilter(pairlist.NewAgeFilter(minAge))
		case "PerformanceFilter":
			topN := 0
			if v, ok := fc.Params["top_n"].(float64); ok {
				topN = int(v)
			}
			newManager.AddFilter(pairlist.NewPerformanceFilter(topN))
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown filter: " + fc.Name})
			return
		}
	}

	// Replace the global manager
	PairlistManager = newManager

	c.JSON(http.StatusOK, gin.H{
		"status":    "configured",
		"producers": newManager.Producers(),
		"filters":   newManager.Filters(),
	})
}

// PairlistHandlerConfig is a helper type for JSON binding.
type PairlistHandlerConfig struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params"`
}
