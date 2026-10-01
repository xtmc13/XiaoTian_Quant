package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/pairlist"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// PairlistManager is the global pairlist manager instance.
var PairlistManager *pairlist.Manager

// pairlistSourceDeps 是 producer/manager 的生产数据源接线（CoinGecko 市值经
// dataprovider 限流熔断 + Binance universe + KlineFeeder K 线）。构建无 IO，
// CoinGecko 在 Generate 时惰性解析 dataprovider.Default()，可安全包级初始化。
var pairlistSourceDeps = pairlist.ProductionSourceDeps()

// pairlistConfigBody 是 ConfigurePairlist 的请求体（同时用于持久化与启动恢复）。
type pairlistConfigBody struct {
	Producers []struct {
		Name   string         `json:"name"`
		Params map[string]any `json:"params"`
	} `json:"producers"`
	Filters []struct {
		Name   string         `json:"name"`
		Params map[string]any `json:"params"`
	} `json:"filters"`
}

// buildManagerFromConfig 按配置体重建 manager（producer/filter 均经包工厂构建并接线生产数据源）。
func buildManagerFromConfig(body *pairlistConfigBody) (*pairlist.Manager, error) {
	cfg := pairlist.DefaultManagerConfig()
	m := pairlist.NewManager(cfg)
	for _, pc := range body.Producers {
		producer, err := pairlist.BuildProducerFromConfig(pc.Name, pc.Params)
		if err != nil {
			return nil, err
		}
		pairlist.WireProducer(producer, pairlistSourceDeps)
		m.AddProducer(producer)
	}
	for _, fc := range body.Filters {
		filter, err := pairlist.BuildFilterFromConfig(fc.Name, fc.Params)
		if err != nil {
			return nil, err
		}
		m.AddFilter(filter)
	}
	return m, nil
}

// applyPairlistConfig 用配置体替换全局 manager（含过滤器取数通道接线）。
func applyPairlistConfig(body *pairlistConfigBody) error {
	m, err := buildManagerFromConfig(body)
	if err != nil {
		return err
	}
	pairlist.WireManager(m, pairlistSourceDeps)
	PairlistManager = m
	return nil
}

// applyDefaultPairlistConfig 启动时应用默认配置（成交量配对列表）。
func applyDefaultPairlistConfig() {
	defaultProducer := pairlist.NewVolumePairList(0, 0, nil)
	pairlist.WireProducer(defaultProducer, pairlistSourceDeps)
	PairlistManager.AddProducer(defaultProducer)
}

func init() {
	cfg := pairlist.DefaultManagerConfig()
	PairlistManager = pairlist.NewManager(cfg)
	pairlist.WireManager(PairlistManager, pairlistSourceDeps)
	// 默认注册成交量配对列表 producer，保证 /api/pairlist/whitelist 开箱即用
	// （对齐 freqtrade 默认 VolumePairList 行为）。用户保存配置后会整体重建 manager。
	applyDefaultPairlistConfig()
}

// RestorePairlistConfig 在 store.InitDB() 之后调用：加载 DB 持久化的用户配置并
// 重建 manager（P1：重启后 pairlist 配置不再丢失）。须在 main() 中显式调用——
// init() 阶段 store.db 尚未打开，读不到持久化配置。
func RestorePairlistConfig() {
	saved := store.GetPairlistConfigJSON()
	if saved == "" {
		return
	}
	var body pairlistConfigBody
	if err := json.Unmarshal([]byte(saved), &body); err != nil {
		log.Printf("[pairlist] 持久化配置解析失败，保持默认成交量列表: %v", err)
		return
	}
	if len(body.Producers) == 0 {
		return
	}
	if err := applyPairlistConfig(&body); err != nil {
		log.Printf("[pairlist] 持久化配置恢复失败，保持默认成交量列表: %v", err)
		return
	}
	log.Printf("[pairlist] 已从 DB 恢复 %d producers / %d filters", len(body.Producers), len(body.Filters))
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
	var body pairlistConfigBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if PairlistManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pairlist manager not initialized"})
		return
	}

	// Rebuild the manager with new configuration（构建失败保持旧配置不动）
	if err := applyPairlistConfig(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 持久化到 DB（P1：重启后恢复；保存失败不阻塞本次生效，仅记日志）
	if raw, err := json.Marshal(&body); err == nil {
		if err := store.SavePairlistConfigJSON(string(raw)); err != nil {
			log.Printf("[pairlist] 配置持久化失败（本次已生效，重启将丢失）: %v", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "configured",
		"producers": PairlistManager.Producers(),
		"filters":   PairlistManager.Filters(),
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
