package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ml"
)

// RLTaskQueue is the global RL task queue for async training.
var RLTaskQueue *ml.RLTaskQueue

func init() {
	RLTaskQueue = ml.NewRLTaskQueue()
}

// ── RL Training Handlers ───────────────────────────────────────

// RLTrainRequest configures an RL training run.
type RLTrainRequest struct {
	ModelID        string           `json:"model_id"`
	Algorithm      string           `json:"algorithm" binding:"required"` // qlearning, ppo, a2c, sac
	NActions       int              `json:"n_actions"`                    // 3 or 5
	Symbol         string           `json:"symbol" binding:"required"`
	Interval       string           `json:"interval" binding:"required"`
	Bars           []map[string]any `json:"bars"`
	LookbackDays   int              `json:"lookback_days"`
	Episodes       int              `json:"episodes"`
	LearningRate   float64          `json:"learning_rate"`
	Discount       float64          `json:"discount"`
	Epsilon        float64          `json:"epsilon"`
	WindowSize     int              `json:"window_size"`
	InitialBalance float64          `json:"initial_balance"`
	Commission     float64          `json:"commission"`
	UseTensorBoard bool             `json:"use_tensorboard"`
}

// RLTrainResponse returns the RL training result.
type RLTrainResponse struct {
	Success         bool           `json:"success"`
	ModelID         string         `json:"model_id"`
	Algorithm       string         `json:"algorithm"`
	NActions        int            `json:"n_actions"`
	Episodes        int            `json:"episodes"`
	FinalBalance    float64        `json:"final_balance"`
	TotalPnL        float64        `json:"total_pnl"`
	BestReward      float64        `json:"best_reward"`
	AvgRewardLast10 float64        `json:"avg_reward_last_10"`
	QTableSize      int            `json:"q_table_size,omitempty"`
	EpisodeRewards  []float64      `json:"episode_rewards"`
	TensorBoardURL  string         `json:"tensorboard_url,omitempty"`
	DurationMs      int64          `json:"duration_ms"`
	Error           string         `json:"error,omitempty"`
}

// RLTrain starts reinforcement learning training.
// For qlearning: synchronous via ML Server HTTP.
// For ppo/a2c/sac: asynchronous via Redis task queue + independent Python worker.
func RLTrain(c *gin.Context) {
	var req RLTrainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Defaults
	if req.Algorithm == "" {
		req.Algorithm = "qlearning"
	}
	if req.NActions <= 0 {
		req.NActions = 3
	}
	if req.Episodes <= 0 {
		req.Episodes = 100
	}
	if req.LearningRate <= 0 {
		req.LearningRate = 0.01
	}
	if req.Discount <= 0 {
		req.Discount = 0.99
	}
	if req.WindowSize <= 0 {
		req.WindowSize = 50
	}
	if req.InitialBalance <= 0 {
		req.InitialBalance = 10000
	}
	if req.Commission <= 0 {
		req.Commission = 0.001
	}
	if req.ModelID == "" {
		req.ModelID = "rl_" + req.Algorithm + "_" + req.Symbol + "_" + time.Now().Format("20060102_150405")
	}

	// If bars not provided, load from downloader
	bars := req.Bars
	if len(bars) == 0 && DataDownloader != nil {
		toMs := time.Now().UnixMilli()
		fromMs := time.Now().AddDate(0, 0, -req.LookbackDays).UnixMilli()
		if req.LookbackDays <= 0 {
			fromMs = time.Now().AddDate(0, 0, -90).UnixMilli()
		}
		loaded := DataDownloader.LoadBarsForBacktest(req.Symbol, req.Interval, fromMs, toMs)
		if len(loaded) > 0 {
			bars = make([]map[string]any, len(loaded))
			for i, b := range loaded {
				bars[i] = map[string]any{
					"time":   b.Time,
					"open":   b.Open,
					"high":   b.High,
					"low":    b.Low,
					"close":  b.Close,
					"volume": b.Volume,
				}
			}
		}
	}

	if len(bars) < 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient bars (< 200) for RL training"})
		return
	}

	// ── Algorithm routing ────────────────────────────────────────

	if ml.IsAdvancedAlgorithm(req.Algorithm) {
		// Async: submit to Redis task queue for independent Python worker
		config := map[string]any{
			"model_id":         req.ModelID,
			"episodes":         req.Episodes,
			"learning_rate":    req.LearningRate,
			"discount":         req.Discount,
			"epsilon":          req.Epsilon,
			"window_size":      req.WindowSize,
			"initial_balance":  req.InitialBalance,
			"commission":       req.Commission,
			"use_tensorboard":  req.UseTensorBoard,
			"tensorboard_run_id": req.ModelID,
		}

		job, err := RLTaskQueue.SubmitJob(req.Algorithm, req.Symbol, req.Interval, req.NActions, bars, config)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusAccepted, gin.H{
			"success":   true,
			"job_id":    job.JobID,
			"algorithm": req.Algorithm,
			"status":    job.Status,
			"message":   "Training job submitted to worker queue. Poll /rl/jobs/:id for status.",
		})
		return
	}

	// Sync: Q-learning via direct HTTP to ML Server
	start := time.Now()
	result, err := MLClient.RLTrain(ml.RLTrainConfig{
		ModelID:          req.ModelID,
		Algorithm:        req.Algorithm,
		NActions:         req.NActions,
		Symbol:           req.Symbol,
		Interval:         req.Interval,
		Bars:             bars,
		Episodes:         req.Episodes,
		LearningRate:     req.LearningRate,
		Discount:         req.Discount,
		Epsilon:          req.Epsilon,
		WindowSize:       req.WindowSize,
		InitialBalance:   req.InitialBalance,
		Commission:       req.Commission,
		UseTensorBoard:   req.UseTensorBoard,
		TensorBoardRunID: req.ModelID,
	})

	resp := RLTrainResponse{
		Success:         result != nil && result.Success,
		ModelID:         req.ModelID,
		Algorithm:       req.Algorithm,
		NActions:        req.NActions,
		Episodes:        req.Episodes,
		FinalBalance:    result.FinalBalance,
		TotalPnL:        result.TotalPnL,
		BestReward:      result.BestReward,
		AvgRewardLast10: result.AvgRewardLast10,
		QTableSize:      result.QTableSize,
		EpisodeRewards:  result.EpisodeRewards,
		TensorBoardURL:  result.TensorBoardURL,
		DurationMs:      time.Since(start).Milliseconds(),
	}
	if err != nil {
		resp.Error = err.Error()
		c.JSON(http.StatusInternalServerError, resp)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ── Async Job Management ───────────────────────────────────────

// GetRLJob returns the status of an async RL training job.
func GetRLJob(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job_id required"})
		return
	}

	job, err := RLTaskQueue.GetJob(jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, job)
}

// CancelRLJob cancels a pending or running RL training job.
func CancelRLJob(c *gin.Context) {
	jobID := c.Param("id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job_id required"})
		return
	}

	if err := RLTaskQueue.CancelJob(jobID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "job_id": jobID, "status": "cancelled"})
}

// RLPredictRequest sends bars for RL inference.
type RLPredictRequest struct {
	ModelID string           `json:"model_id" binding:"required"`
	Bars    []map[string]any `json:"bars" binding:"required"`
}

// RLPredictResponse returns the RL agent's action.
type RLPredictResponse struct {
	Success    bool    `json:"success"`
	ModelID    string  `json:"model_id"`
	Action     int     `json:"action"`
	ActionName string  `json:"action_name"`
	Confidence float64 `json:"confidence"`
	Position   float64 `json:"position"`
	Error      string  `json:"error,omitempty"`
}

// RLPredict runs inference using a trained RL model.
func RLPredict(c *gin.Context) {
	var req RLPredictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := MLClient.RLPredict(ml.RLPredictInput{
		ModelID: req.ModelID,
		Bars:    req.Bars,
	})

	resp := RLPredictResponse{
		Success: result != nil && result.Success,
		ModelID: req.ModelID,
	}
	if err != nil {
		resp.Error = err.Error()
		c.JSON(http.StatusInternalServerError, resp)
		return
	}

	resp.Action = result.Action
	resp.ActionName = result.ActionName
	resp.Confidence = result.Confidence
	resp.Position = result.Position
	c.JSON(http.StatusOK, resp)
}

// RLEvaluateRequest configures RL evaluation.
type RLEvaluateRequest struct {
	ModelID      string `json:"model_id" binding:"required"`
	Symbol       string `json:"symbol" binding:"required"`
	Interval     string `json:"interval" binding:"required"`
	LookbackDays int    `json:"lookback_days"`
}

// RLEvaluateResponse returns evaluation metrics.
type RLEvaluateResponse struct {
	Success        bool           `json:"success"`
	ModelID        string         `json:"model_id"`
	TotalReturnPct float64        `json:"total_return_pct"`
	SharpeRatio    float64        `json:"sharpe_ratio"`
	MaxDrawdownPct float64        `json:"max_drawdown_pct"`
	WinRate        float64        `json:"win_rate"`
	Trades         int            `json:"trades"`
	AvgTradeReturn float64        `json:"avg_trade_return"`
	Metrics        map[string]any `json:"metrics"`
	Error          string         `json:"error,omitempty"`
}

// RLEvaluate evaluates a trained RL agent on out-of-sample data.
func RLEvaluate(c *gin.Context) {
	var req RLEvaluateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LookbackDays <= 0 {
		req.LookbackDays = 30
	}

	// Load test data
	toMs := time.Now().UnixMilli()
	fromMs := time.Now().AddDate(0, 0, -req.LookbackDays).UnixMilli()
	bars := DataDownloader.LoadBarsForBacktest(req.Symbol, req.Interval, fromMs, toMs)
	if len(bars) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no test data available"})
		return
	}

	// Convert to map format
	barMaps := make([]map[string]any, len(bars))
	for i, b := range bars {
		barMaps[i] = map[string]any{
			"time":   b.Time,
			"open":   b.Open,
			"high":   b.High,
			"low":    b.Low,
			"close":  b.Close,
			"volume": b.Volume,
		}
	}

	result, err := MLClient.RLEvaluate(req.ModelID, barMaps)

	resp := RLEvaluateResponse{
		Success: result != nil && result.Success,
		ModelID: req.ModelID,
	}
	if err != nil {
		resp.Error = err.Error()
		c.JSON(http.StatusInternalServerError, resp)
		return
	}

	resp.TotalReturnPct = result.TotalReturnPct
	resp.SharpeRatio = result.SharpeRatio
	resp.MaxDrawdownPct = result.MaxDrawdownPct
	resp.WinRate = result.WinRate
	resp.Trades = result.Trades
	resp.AvgTradeReturn = result.AvgTradeReturn
	resp.Metrics = result.Metrics
	c.JSON(http.StatusOK, resp)
}

// ListRLModels returns all trained RL models.
func ListRLModels(c *gin.Context) {
	models, err := MLClient.ListRLModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

// DeleteRLModel removes a trained RL model.
func DeleteRLModel(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_id required"})
		return
	}
	if err := MLClient.DeleteRLModel(modelID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "model_id": modelID})
}
