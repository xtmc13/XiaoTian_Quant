package handler

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 马丁策略 ──

type martinConfig struct {
	ID                   string  `json:"id"`
	FirstOrderAmount     float64 `json:"first_order_amount" binding:"required,min=10,max=10000"`
	OrderCount           int     `json:"order_count" binding:"required,min=3,max=10"`
	AddPositionSpread    float64 `json:"add_position_spread" binding:"min=0.5,max=50"`
	AddPositionCallback  float64 `json:"add_position_callback" binding:"min=0.01,max=0.5"`
	TakeProfitRatio      float64 `json:"take_profit_ratio" binding:"min=0.1,max=10"`
	ProfitCallback       float64 `json:"profit_callback" binding:"min=0.01,max=0.5"`
	DoubleFirstOrder     bool    `json:"double_first_order"`
	LoopType             string  `json:"loop_type" binding:"oneof=single cycle"`
	LoopCount            int     `json:"loop_count" binding:"min=0,max=100"`
	EnableAddPosition    bool    `json:"enable_add_position"`
	FlashCrashProtection float64 `json:"flash_crash_protection" binding:"min=0.5,max=20"`
	Symbol               string  `json:"symbol" binding:"required"`
	Exchange             string  `json:"exchange" binding:"required"`
	Status               string  `json:"status"`
}

// StrategyMartinList godoc
// GET /strategies/martin
func StrategyMartinList(c *gin.Context) {
	strategyListHandler[martinConfig](c, "martin")
}

// StrategyMartinCreate godoc
// POST /strategies/martin
func StrategyMartinCreate(c *gin.Context) {
	strategyCreateHandler[martinConfig](c, "martin")
}

// StrategyMartinUpdate godoc
// PUT /strategies/martin/:id
func StrategyMartinUpdate(c *gin.Context) {
	strategyUpdateHandler[martinConfig](c, "martin")
}

// StrategyMartinDelete godoc
// DELETE /strategies/martin/:id
func StrategyMartinDelete(c *gin.Context) {
	strategyDeleteHandler(c)
}

// ── 华尔街策略 ──

type wallStreetConfig struct {
	ID                   string  `json:"id"`
	FirstOrderAmount     float64 `json:"first_order_amount" binding:"required,min=10,max=10000"`
	OrderCount           int     `json:"order_count" binding:"required,min=3,max=10"`
	AddPositionSpread    float64 `json:"add_position_spread" binding:"min=0.5,max=50"`
	AddPositionCallback  float64 `json:"add_position_callback" binding:"min=0.01,max=0.5"`
	TakeProfitRatio      float64 `json:"take_profit_ratio" binding:"min=0.1,max=10"`
	ProfitCallback       float64 `json:"profit_callback" binding:"min=0.01,max=0.5"`
	DoubleFirstOrder     bool    `json:"double_first_order"`
	LoopType             string  `json:"loop_type" binding:"oneof=single cycle"`
	LoopCount            int     `json:"loop_count" binding:"min=0,max=100"`
	EnableAddPosition    bool    `json:"enable_add_position"`
	FlashCrashProtection float64 `json:"flash_crash_protection" binding:"min=0.5,max=20"`
	Symbol               string  `json:"symbol" binding:"required"`
	Exchange             string  `json:"exchange" binding:"required"`
	Status               string  `json:"status"`
}

// StrategyWallStreetList godoc
// GET /strategies/wallstreet
func StrategyWallStreetList(c *gin.Context) {
	strategyListHandler[wallStreetConfig](c, "wallstreet")
}

// StrategyWallStreetCreate godoc
// POST /strategies/wallstreet
func StrategyWallStreetCreate(c *gin.Context) {
	strategyCreateHandler[wallStreetConfig](c, "wallstreet")
}

// StrategyWallStreetUpdate godoc
// PUT /strategies/wallstreet/:id
func StrategyWallStreetUpdate(c *gin.Context) {
	strategyUpdateHandler[wallStreetConfig](c, "wallstreet")
}

// StrategyWallStreetDelete godoc
// DELETE /strategies/wallstreet/:id
func StrategyWallStreetDelete(c *gin.Context) {
	strategyDeleteHandler(c)
}

// ── 策略机器人持久化 ──

// strategyBotConfig mirrors the shared payload shape of martinConfig and
// wallStreetConfig so the generic handlers can read/write the fields that are
// also mirrored into dedicated DB columns. It must stay field-for-field
// identical (including tags) to those structs.
type strategyBotConfig struct {
	ID                   string  `json:"id"`
	FirstOrderAmount     float64 `json:"first_order_amount" binding:"required,min=10,max=10000"`
	OrderCount           int     `json:"order_count" binding:"required,min=3,max=10"`
	AddPositionSpread    float64 `json:"add_position_spread" binding:"min=0.5,max=50"`
	AddPositionCallback  float64 `json:"add_position_callback" binding:"min=0.01,max=0.5"`
	TakeProfitRatio      float64 `json:"take_profit_ratio" binding:"min=0.1,max=10"`
	ProfitCallback       float64 `json:"profit_callback" binding:"min=0.01,max=0.5"`
	DoubleFirstOrder     bool    `json:"double_first_order"`
	LoopType             string  `json:"loop_type" binding:"oneof=single cycle"`
	LoopCount            int     `json:"loop_count" binding:"min=0,max=100"`
	EnableAddPosition    bool    `json:"enable_add_position"`
	FlashCrashProtection float64 `json:"flash_crash_protection" binding:"min=0.5,max=20"`
	Symbol               string  `json:"symbol" binding:"required"`
	Exchange             string  `json:"exchange" binding:"required"`
	Status               string  `json:"status"`
}

type strategyBotConstraint interface {
	martinConfig | wallStreetConfig
}

// strategyListHandler lists persisted configs of one strategy category.
func strategyListHandler[T strategyBotConstraint](c *gin.Context, category string) {
	items, err := store.NewStrategyConfigRepo().List(map[string]any{"category": category}, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	strategies := make([]T, 0, len(items))
	for _, rec := range items {
		var cfg T
		if rec.ConfigJSON != "" {
			if err := json.Unmarshal([]byte(rec.ConfigJSON), &cfg); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
				return
			}
		}
		// The dedicated DB columns are authoritative over config_json.
		plain := strategyBotConfig(cfg)
		plain.ID = rec.ID
		plain.Symbol = rec.Symbol
		plain.Status = rec.Status
		strategies = append(strategies, T(plain))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "strategies": strategies})
}

// strategyCreateHandler persists a new config of one strategy category.
func strategyCreateHandler[T strategyBotConstraint](c *gin.Context, category string) {
	var req T
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	cfg := strategyBotConfig(req)
	payload, err := json.Marshal(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	status := cfg.Status
	if status == "" {
		status = "stopped"
	}
	rec := &store.StrategyConfigRecord{
		UserID:       getUserID(c),
		Name:         cfg.Symbol + "-" + category,
		Category:     category,
		StrategyType: category,
		Symbol:       cfg.Symbol,
		Direction:    "long",
		MarketType:   "spot",
		Timeframe:    "15m",
		Status:       status,
		ConfigJSON:   string(payload),
	}
	if err := store.NewStrategyConfigRepo().Create(rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "id": rec.ID})
}

// strategyUpdateHandler overwrites the persisted config of one strategy category.
func strategyUpdateHandler[T strategyBotConstraint](c *gin.Context, category string) {
	repo := store.NewStrategyConfigRepo()
	rec, err := repo.GetByID(c.Param("id"))
	if err == sql.ErrNoRows || rec == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "strategy not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	var cfg T
	if rec.ConfigJSON != "" {
		if err := json.Unmarshal([]byte(rec.ConfigJSON), &cfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
	}
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	plain := strategyBotConfig(cfg)
	plain.ID = rec.ID
	payload, err := json.Marshal(plain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	// Keep the original record identity (ID/UserID/CreatedAt); refresh the
	// fields mirrored into dedicated columns.
	rec.Symbol = plain.Symbol
	if plain.Status != "" {
		rec.Status = plain.Status
	}
	rec.ConfigJSON = string(payload)
	if err := repo.Update(rec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// strategyDeleteHandler removes a persisted strategy config by ID.
func strategyDeleteHandler(c *gin.Context) {
	if err := store.NewStrategyConfigRepo().Delete(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ── 防瀑布检查 ──

// FlashCrashCheckV2 godoc
// POST /strategies/flash-crash-check
func FlashCrashCheckV2(c *gin.Context) {
	var req struct {
		Symbol    string  `json:"symbol" binding:"required"`
		Threshold float64 `json:"threshold" binding:"min=0.1,max=50"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"is_flash_crash": false,
		"max_drop_1min":  0,
	})
}

// ── 仓位计算 ──

// PositionSizesCalculate godoc
// POST /strategies/position-sizes
func PositionSizesCalculate(c *gin.Context) {
	var req struct {
		StrategyType     string  `json:"strategy_type" binding:"required,oneof=martin wallstreet"`
		FirstOrderAmount float64 `json:"first_order_amount" binding:"required,min=1"`
		OrderCount       int     `json:"order_count" binding:"required,min=1,max=15"`
		DoubleFirstOrder bool    `json:"double_first_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	sizes := make([]float64, req.OrderCount)
	first := req.FirstOrderAmount
	if req.DoubleFirstOrder {
		first *= 2
	}

	if req.StrategyType == "martin" {
		// 倍投: 2, 4, 8, 16...
		for i := 0; i < req.OrderCount; i++ {
			sizes[i] = first * math.Pow(2, float64(i)) // 2^i
		}
	} else {
		// 华尔街: 斐波那契 1, 2, 3, 5, 8...
		fib := []float64{1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233, 377, 610, 987}
		for i := 0; i < req.OrderCount && i < len(fib); i++ {
			sizes[i] = first * fib[i]
		}
	}

	// 计算总投入和占总仓位比例
	total := 0.0
	for _, s := range sizes {
		total += s
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"sizes":       sizes,
		"total":       total,
		"first_order": first,
	})
}
