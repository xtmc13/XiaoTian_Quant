package experiment

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/indicator"
	"github.com/xiaotian-quant/gateway/internal/middleware"
)

// RunExperimentHandler godoc
// POST /api/experiment/run
// Runs a full parameter optimization experiment (DE or TPE) with OOS validation.
func RunExperimentHandler(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	_, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	var req ExperimentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error(), "data": nil})
		return
	}

	if req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "code is required", "data": nil})
		return
	}
	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}
	if req.Optimizer == "" {
		req.Optimizer = "de"
	}

	result, err := RunExperiment(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"result":      result,
		"data":        result,
		"best_params": result.BestParams,
	})
}

// SensitivityAnalysisHandler godoc
// POST /api/experiment/sensitivity
// Performs one-at-a-time sensitivity analysis around best params.
func SensitivityAnalysisHandler(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	_, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	var req struct {
		Code        string            `json:"code" binding:"required"`
		Symbol      string            `json:"symbol"`
		Interval    string            `json:"interval"`
		Klines      []map[string]any  `json:"klines,omitempty"`
		BaseParams  map[string]any    `json:"base_params"`
		ParamSpace  ParamSpace        `json:"param_space,omitempty"`
		BacktestConfig backtest.RunnerConfig `json:"backtest_config,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}

	space := req.ParamSpace
	if len(space) == 0 && req.Code != "" {
		parsed := indicator.ParseSource(req.Code)
		space = FromIndicatorParseResult(parsed)
	}

	bars := klinesToBars(req.Klines, req.Symbol, req.Interval)
	if len(bars) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "no bar data"})
		return
	}

	btCfg := req.BacktestConfig
	if btCfg.InitialBalance <= 0 {
		btCfg.InitialBalance = 10000
	}
	scoreCfg := DefaultScoreConfig()

	result := SensitivityAnalysis(req.Code, req.Symbol, req.BaseParams, space, bars, btCfg, scoreCfg)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result":  result,
		"data":    result,
	})
}

// ExperimentStatusHandler godoc
// GET /api/experiment/status/:id
// Returns the status of a running experiment (placeholder for async execution).
func ExperimentStatusHandler(c *gin.Context) {
	id := c.Param("id")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result":  gin.H{"experiment_id": id, "status": "completed"},
		"data":    gin.H{"experiment_id": id, "status": "completed"},
	})
}

// WalkForwardHandler godoc
// POST /api/experiment/walk-forward
// Runs walk-forward validation on an indicator.
func WalkForwardHandler(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	_, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized"})
		return
	}

	var req struct {
		Code           string            `json:"code" binding:"required"`
		Symbol         string            `json:"symbol"`
		Interval       string            `json:"interval"`
		Klines         []map[string]any  `json:"klines,omitempty"`
		Params         map[string]any    `json:"params"`
		WindowSize     int               `json:"window_size"`
		TestSize       int               `json:"test_size"`
		StepSize       int               `json:"step_size"`
		BacktestConfig backtest.RunnerConfig `json:"backtest_config,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}
	if req.WindowSize <= 0 {
		req.WindowSize = 200
	}
	if req.TestSize <= 0 {
		req.TestSize = 50
	}
	if req.StepSize <= 0 {
		req.StepSize = req.TestSize
	}

	bars := klinesToBars(req.Klines, req.Symbol, req.Interval)
	if len(bars) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "no bar data"})
		return
	}

	strategy := NewIndicatorStrategy(req.Code, req.Symbol, req.Params)
	if err := strategy.Precompute(bars); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	btCfg := req.BacktestConfig
	if btCfg.InitialBalance <= 0 {
		btCfg.InitialBalance = 10000
	}
	scoreCfg := DefaultScoreConfig()
	wfCfg := WalkForwardConfig{
		WindowSize: req.WindowSize,
		TestSize:   req.TestSize,
		StepSize:   req.StepSize,
	}

	result := WalkForward(bars, strategy, btCfg, scoreCfg, wfCfg)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result":  result,
		"data":    result,
	})
}
