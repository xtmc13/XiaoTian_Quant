package handler

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ml"
	"github.com/xiaotian-quant/gateway/internal/strategy"
	"github.com/xiaotian-quant/gateway/internal/strategy/strategies"
)

var mlClient = ml.NewClient(os.Getenv("ML_SERVER_URL"))
var mlCreator = strategies.NewMLCreator(os.Getenv("ML_SERVER_URL"))

// mlStrategyAdapter wraps MLStrategy to satisfy strategy.Strategy interface.
type mlStrategyAdapter struct {
	*strategies.MLStrategy
	strategy.BaseStrategy
}

func (a *mlStrategyAdapter) GetParameters() *strategy.ParamRegistry   { return nil }
func (a *mlStrategyAdapter) InformativePairs() []strategy.InformativePair { return nil }

// ── ML Train ──

func MLTrain(c *gin.Context) {
	var cfg ml.TrainConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if cfg.ModelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_id required"})
		return
	}
	if len(cfg.Bars) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bars required"})
		return
	}

	result, err := mlClient.Train(cfg)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ML server error", "detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ── ML Predict ──

func MLPredict(c *gin.Context) {
	var input ml.PredictInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	result, err := mlClient.Predict(input)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ML server error", "detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ── ML Models ──

func MLModels(c *gin.Context) {
	models, err := mlClient.ListModels()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ML server error", "detail": err.Error()})
		return
	}
	if models == nil {
		models = []ml.ModelInfo{}
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

// ── ML Model Detail ──

func MLModelDetail(c *gin.Context) {
	modelID := c.Param("id")
	model, err := mlClient.GetModel(modelID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, model)
}

// ── ML Delete Model ──

func MLDeleteModel(c *gin.Context) {
	modelID := c.Param("id")
	if err := mlClient.DeleteModel(modelID); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// ── ML Feature Importance ──

func MLFeatureImportance(c *gin.Context) {
	modelID := c.Param("id")
	importance, err := mlClient.FeatureImportance(modelID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"model_id": modelID, "importance": importance})
}

// ── ML Generate Features ──

func MLGenerateFeatures(c *gin.Context) {
	var body struct {
		Bars   []map[string]any `json:"bars"`
		Config map[string]any   `json:"config"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	result, err := mlClient.GenerateFeatures(body.Bars, body.Config)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ML server error", "detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ── ML Health ──

// ── ML Deploy to Strategy Engine ──

type mlDeployReq struct {
	ModelID       string  `json:"model_id"`
	Symbol        string  `json:"symbol"`
	MinConfidence float64 `json:"min_confidence"`
}

func MLDeployStrategy(c *gin.Context) {
	var req mlDeployReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.ModelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_id required"})
		return
	}
	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}
	if req.MinConfidence <= 0 {
		req.MinConfidence = 0.3
	}

	// Create ML strategy
	s := mlCreator.Create(req.ModelID)
	if s == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create ML strategy"})
		return
	}

	// Register with strategy engine
	eng := strategy.GetEngine(nil)
	if eng == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "strategy engine not initialized"})
		return
	}

	// Wrap in adapter to satisfy full Strategy interface
	adapter := &mlStrategyAdapter{MLStrategy: s}

	if err := eng.Register(adapter); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	// Start it
	params := map[string]any{
		"symbol":         req.Symbol,
		"model_id":       req.ModelID,
		"min_confidence": req.MinConfidence,
	}
	if err := eng.Start(adapter.Name(), params); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "deployed",
		"strategy_name": s.Name(),
		"model_id":     req.ModelID,
		"symbol":       req.Symbol,
	})
}

// ── ML Available Models for Strategy ──

func MLStrategyModels(c *gin.Context) {
	ids := mlCreator.ListAvailable()
	if ids == nil {
		ids = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"models": ids})
}

func MLHealth(c *gin.Context) {
	if err := mlClient.Health(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
