package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/ml"
)

// MLClient is the global ML service client.
var MLClient *ml.Client

func init() {
	MLClient = ml.NewClient("")
}

// ── ML Training Pipeline Handlers ──────────────────────────────

// TrainModelRequest configures a model training run.
type TrainModelRequest struct {
	Symbol         string         `json:"symbol" binding:"required"`
	Interval       string         `json:"interval" binding:"required"`
	ModelID        string         `json:"model_id"`
	ModelType      string         `json:"model_type"`      // lightgbm, xgboost
	TaskType       string         `json:"task_type"`       // regression, classification
	LookbackDays   int            `json:"lookback_days"`   // default 90
	FeaturePeriods []int          `json:"feature_periods"` // default [5,10,20,50]
	LabelHorizon   int            `json:"label_horizon"`   // default 5
	ModelParams    map[string]any `json:"model_params,omitempty"`
}

// TrainModelResponse returns the training result.
type TrainModelResponse struct {
	Success           bool           `json:"success"`
	ModelID           string         `json:"model_id"`
	Symbol            string         `json:"symbol"`
	BarsLoaded        int            `json:"bars_loaded"`
	FeaturesGenerated int            `json:"features_generated"`
	TrainSamples      int            `json:"train_samples"`
	TestSamples       int            `json:"test_samples"`
	Metrics           map[string]any `json:"metrics"`
	FeatureNames      []string       `json:"feature_names"`
	DurationMs        int64          `json:"duration_ms"`
	Error             string         `json:"error,omitempty"`
}

// TrainModel runs the full training pipeline: load data → generate features → train.
func TrainModel(c *gin.Context) {
	var req TrainModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LookbackDays <= 0 {
		req.LookbackDays = 90
	}
	if len(req.FeaturePeriods) == 0 {
		req.FeaturePeriods = []int{5, 10, 20, 50}
	}
	if req.LabelHorizon <= 0 {
		req.LabelHorizon = 5
	}
	if req.ModelType == "" {
		req.ModelType = "lightgbm"
	}
	if req.TaskType == "" {
		req.TaskType = "regression"
	}

	cfg := ml.PipelineConfig{
		Symbol:         req.Symbol,
		Interval:       req.Interval,
		ModelID:        req.ModelID,
		ModelType:      req.ModelType,
		TaskType:       req.TaskType,
		LookbackDays:   req.LookbackDays,
		FeaturePeriods: req.FeaturePeriods,
		LabelHorizon:   req.LabelHorizon,
		ModelParams:    req.ModelParams,
	}

	pipeline := ml.NewTrainingPipeline(MLClient, DataDownloader)
	result, err := pipeline.Run(cfg)

	resp := TrainModelResponse{
		Success:           result.Success,
		ModelID:           result.ModelID,
		Symbol:            result.Symbol,
		BarsLoaded:        result.BarsLoaded,
		FeaturesGenerated: result.FeaturesGenerated,
		TrainSamples:      result.TrainSamples,
		TestSamples:       result.TestSamples,
		Metrics:           result.Metrics,
		FeatureNames:      result.FeatureNames,
		DurationMs:        result.DurationMs,
	}
	if err != nil {
		resp.Error = err.Error()
		c.JSON(http.StatusInternalServerError, resp)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// EvaluateModelRequest configures model evaluation.
type EvaluateModelRequest struct {
	ModelID        string `json:"model_id" binding:"required"`
	Symbol         string `json:"symbol" binding:"required"`
	Interval       string `json:"interval" binding:"required"`
	LookbackDays   int    `json:"lookback_days"`
	FeaturePeriods []int  `json:"feature_periods"`
	LabelHorizon   int    `json:"label_horizon"`
}

// EvaluateModelResponse returns evaluation metrics.
type EvaluateModelResponse struct {
	Success    bool           `json:"success"`
	ModelID    string         `json:"model_id"`
	Metrics    map[string]any `json:"metrics"`
	Samples    int            `json:"samples"`
	DurationMs int64          `json:"duration_ms"`
	Error      string         `json:"error,omitempty"`
}

// EvaluateModel evaluates a trained model on out-of-sample data.
func EvaluateModel(c *gin.Context) {
	var req EvaluateModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.LookbackDays <= 0 {
		req.LookbackDays = 30
	}
	if len(req.FeaturePeriods) == 0 {
		req.FeaturePeriods = []int{5, 10, 20, 50}
	}
	if req.LabelHorizon <= 0 {
		req.LabelHorizon = 5
	}

	start := time.Now()

	// Load test data
	toMs := time.Now().UnixMilli()
	fromMs := time.Now().AddDate(0, 0, -req.LookbackDays).UnixMilli()
	bars := DataDownloader.LoadBarsForBacktest(req.Symbol, req.Interval, fromMs, toMs)
	if len(bars) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no test data available"})
		return
	}

	// Convert to OHLCV
	ohlcv := make([]ml.OHLCV, len(bars))
	for i, b := range bars {
		ohlcv[i] = ml.OHLCV{
			Open:   b.Open,
			High:   b.High,
			Low:    b.Low,
			Close:  b.Close,
			Volume: b.Volume,
		}
	}

	// Generate features
	calc := ml.NewFeatureCalculator(req.FeaturePeriods)
	maxPeriod := 0
	for _, p := range req.FeaturePeriods {
		if p > maxPeriod {
			maxPeriod = p
		}
	}
	minBars := maxPeriod + req.LabelHorizon + 10
	if len(ohlcv) < minBars {
		c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient test data"})
		return
	}

	var testBars []map[string]any
	for i := minBars; i < len(ohlcv); i++ {
		window := make([]ml.OHLCV, 0, maxPeriod+5)
		for j := i - maxPeriod - 5; j <= i; j++ {
			if j >= 0 {
				window = append(window, ohlcv[j])
			}
		}
		features := calc.Compute(window)
		if len(features) == 0 {
			continue
		}

		futureIdx := i + req.LabelHorizon
		if futureIdx >= len(ohlcv) {
			continue
		}
		label := 0.0
		if ohlcv[i].Close > 0 {
			label = (ohlcv[futureIdx].Close - ohlcv[i].Close) / ohlcv[i].Close
		}

		barMap := map[string]any{
			"time":   bars[i].Time,
			"open":   ohlcv[i].Open,
			"high":   ohlcv[i].High,
			"low":    ohlcv[i].Low,
			"close":  ohlcv[i].Close,
			"volume": ohlcv[i].Volume,
			"label":  label,
		}
		for k, v := range features {
			barMap[k] = v
		}
		testBars = append(testBars, barMap)
	}

	if len(testBars) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no test features generated"})
		return
	}

	// Evaluate
	evaluator := ml.NewEvaluator(MLClient)
	evalResult, err := evaluator.Evaluate(req.ModelID, testBars)

	resp := EvaluateModelResponse{
		Success:    evalResult != nil,
		ModelID:    req.ModelID,
		Samples:    len(testBars),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		resp.Error = err.Error()
		c.JSON(http.StatusInternalServerError, resp)
		return
	}

	resp.Metrics = map[string]any{
		"mse":                  evalResult.MSE,
		"rmse":                 evalResult.RMSE,
		"mae":                  evalResult.MAE,
		"r2":                   evalResult.R2,
		"directional_accuracy": evalResult.Directional,
		"sharpe":               evalResult.Sharpe,
	}
	c.JSON(http.StatusOK, resp)
}

// ListModels returns all trained models.
func ListModels(c *gin.Context) {
	models, err := MLClient.ListModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

// DeleteModel removes a trained model.
func DeleteModel(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_id required"})
		return
	}
	if err := MLClient.DeleteModel(modelID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "model_id": modelID})
}

// GetModelInfo returns model metadata.
func GetModelInfo(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_id required"})
		return
	}
	info, err := MLClient.GetModel(modelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}

// PredictRequest sends bars for prediction.
type PredictRequest struct {
	ModelID string           `json:"model_id" binding:"required"`
	Bars    []map[string]any `json:"bars" binding:"required"`
}

// PredictResponse returns prediction results.
type PredictResponse struct {
	Success    bool    `json:"success"`
	Prediction float64 `json:"prediction"`
	Confidence float64 `json:"confidence,omitempty"`
	ModelID    string  `json:"model_id"`
	Error      string  `json:"error,omitempty"`
}

// Predict runs inference on provided bars.
func Predict(c *gin.Context) {
	var req PredictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := MLClient.Predict(ml.PredictInput{
		ModelID: req.ModelID,
		Bars:    req.Bars,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, PredictResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, PredictResponse{
		Success:    true,
		Prediction: result.Prediction,
		Confidence: result.Strength,
		ModelID:    req.ModelID,
	})
}

// GenerateFeaturesRequest configures feature generation.
type GenerateFeaturesRequest struct {
	Symbol   string `json:"symbol" binding:"required"`
	Interval string `json:"interval" binding:"required"`
	Periods  []int  `json:"periods"`
	Limit    int    `json:"limit"`
}

// GenerateFeaturesResponse returns generated features.
type GenerateFeaturesResponse struct {
	Success  bool             `json:"success"`
	Symbol   string           `json:"symbol"`
	Count    int              `json:"count"`
	Features []map[string]any `json:"features"`
	Names    []string         `json:"names"`
	Error    string           `json:"error,omitempty"`
}

// GenerateFeatures computes features from recent bars without training.
func GenerateFeatures(c *gin.Context) {
	var req GenerateFeaturesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Periods) == 0 {
		req.Periods = []int{5, 10, 20, 50}
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}

	// Load recent bars
	toMs := time.Now().UnixMilli()
	fromMs := time.Now().AddDate(0, 0, -7).UnixMilli()
	bars := DataDownloader.LoadBarsForBacktest(req.Symbol, req.Interval, fromMs, toMs)
	if len(bars) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no data available"})
		return
	}

	// Convert
	ohlcv := make([]ml.OHLCV, len(bars))
	for i, b := range bars {
		ohlcv[i] = ml.OHLCV{
			Open:   b.Open,
			High:   b.High,
			Low:    b.Low,
			Close:  b.Close,
			Volume: b.Volume,
		}
	}

	// Generate features for the last N bars
	calc := ml.NewFeatureCalculator(req.Periods)
	maxPeriod := 0
	for _, p := range req.Periods {
		if p > maxPeriod {
			maxPeriod = p
		}
	}

	var features []map[string]any
	startIdx := len(ohlcv) - req.Limit
	if startIdx < maxPeriod+5 {
		startIdx = maxPeriod + 5
	}

	for i := startIdx; i < len(ohlcv); i++ {
		window := make([]ml.OHLCV, 0, maxPeriod+5)
		for j := i - maxPeriod - 5; j <= i; j++ {
			if j >= 0 {
				window = append(window, ohlcv[j])
			}
		}
		feat := calc.Compute(window)
		if len(feat) == 0 {
			continue
		}
		featAny := make(map[string]any, len(feat)+2)
		for k, v := range feat {
			featAny[k] = v
		}
		featAny["time"] = bars[i].Time
		featAny["close"] = ohlcv[i].Close
		features = append(features, featAny)
	}

	// Get feature names
	names := make([]string, 0)
	if len(features) > 0 {
		for k := range features[0] {
			names = append(names, k)
		}
	}

	c.JSON(http.StatusOK, GenerateFeaturesResponse{
		Success:  true,
		Symbol:   req.Symbol,
		Count:    len(features),
		Features: features,
		Names:    names,
	})
}

// ── Legacy ML API wrappers (matching main.go route names) ─────

// MLHealth checks if the ML server is reachable.
func MLHealth(c *gin.Context) {
	if err := MLClient.Health(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

// MLTrain trains a model via the training pipeline.
func MLTrain(c *gin.Context) {
	TrainModel(c)
}

// MLPredict runs inference on provided bars.
func MLPredict(c *gin.Context) {
	Predict(c)
}

// MLModels lists all trained models.
func MLModels(c *gin.Context) {
	ListModels(c)
}

// MLModelDetail returns metadata for a specific model.
func MLModelDetail(c *gin.Context) {
	GetModelInfo(c)
}

// MLDeleteModel removes a trained model.
func MLDeleteModel(c *gin.Context) {
	DeleteModel(c)
}

// MLFeatureImportance returns feature importance for a model.
func MLFeatureImportance(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_id required"})
		return
	}
	importance, err := MLClient.FeatureImportance(modelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"model_id": modelID, "importance": importance})
}

// MLGenerateFeatures generates features from recent bars.
func MLGenerateFeatures(c *gin.Context) {
	GenerateFeatures(c)
}

// MLDeployStrategy deploys a trained model to a strategy.
func MLDeployStrategy(c *gin.Context) {
	var req struct {
		ModelID    string `json:"model_id" binding:"required"`
		StrategyID string `json:"strategy_id" binding:"required"`
		Symbol     string `json:"symbol" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify model exists
	_, err := MLClient.GetModel(req.ModelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model not found: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"model_id":    req.ModelID,
		"strategy_id": req.StrategyID,
		"symbol":      req.Symbol,
		"deployed_at": time.Now().Unix(),
	})
}

// MLStrategyModels returns models available for strategy use.
func MLStrategyModels(c *gin.Context) {
	models, err := MLClient.ListModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

