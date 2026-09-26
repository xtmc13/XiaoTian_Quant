package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/pairlist"
)

// PairlistManager is the global pairlist manager instance.
var PairlistManager *pairlist.Manager

// pairlistSourceDeps 是 producer/manager 的生产数据源接线（CoinGecko 市值经
// dataprovider 限流熔断 + Binance universe + KlineFeeder K 线）。构建无 IO，
// CoinGecko 在 Generate 时惰性解析 dataprovider.Default()，可安全包级初始化。
var pairlistSourceDeps = pairlist.ProductionSourceDeps()

func init() {
	cfg := pairlist.DefaultManagerConfig()
	PairlistManager = pairlist.NewManager(cfg)
	pairlist.WireManager(PairlistManager, pairlistSourceDeps)
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
		"producers":   PairlistManager.Producers(),
		"filters":     PairlistManager.Filters(),
		"cached":      PairlistManager.Cached(),
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

	// Add producers（经 pairlist 包工厂构建，参数名与前端模板一致；
	// 构建后注入生产数据源——市值/universe/K线，未接线的源 Generate 时明确降级报错）
	for _, pc := range body.Producers {
		producer, err := pairlist.BuildProducerFromConfig(pc.Name, pc.Params)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		pairlist.WireProducer(producer, pairlistSourceDeps)
		newManager.AddProducer(producer)
	}

	// Add filters
	for _, fc := range body.Filters {
		filter, err := pairlist.BuildFilterFromConfig(fc.Name, fc.Params)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		newManager.AddFilter(filter)
	}

	// Replace the global manager（过滤器取数通道随新 manager 一起接线）
	pairlist.WireManager(newManager, pairlistSourceDeps)
	PairlistManager = newManager

	c.JSON(http.StatusOK, gin.H{
		"status":    "configured",
		"producers": newManager.Producers(),
		"filters":   newManager.Filters(),
	})
}

// GetPairlistSpecs 返回可用 producer/filter 的元数据（名称、参数定义），
// 供前端动态渲染配置表单，避免前端硬编码与后端能力漂移。
func GetPairlistSpecs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"producers": pairlist.ProducerSpecs(),
		"filters":   pairlist.FilterSpecs(),
	})
}

// PairlistHandlerConfig is a helper type for JSON binding.
type PairlistHandlerConfig struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params"`
}
