package ml

import (
	"fmt"
	"math"
	"time"

	"github.com/xiaotian-quant/gateway/internal/data"
)

// ── Training Pipeline ──────────────────────────────────────────

// PipelineConfig configures the end-to-end training pipeline.
type PipelineConfig struct {
	Symbol       string            `json:"symbol"`
	Interval     string            `json:"interval"`
	ModelID      string            `json:"model_id"`
	ModelType    string            `json:"model_type"`   // lightgbm, xgboost
	TaskType     string            `json:"task_type"`    // regression, classification
	LookbackDays int               `json:"lookback_days"` // how many days of history to load
	FeaturePeriods []int           `json:"feature_periods"` // e.g., [5, 10, 20, 50]
	LabelHorizon int               `json:"label_horizon"`   // bars ahead for return label
	ModelParams  map[string]any    `json:"model_params,omitempty"`
}

func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		Symbol:         "BTCUSDT",
		Interval:       "1h",
		ModelType:      "lightgbm",
		TaskType:       "regression",
		LookbackDays:   90,
		FeaturePeriods: []int{5, 10, 20, 50},
		LabelHorizon:   5,
	}
}

// PipelineResult holds the outcome of a training pipeline run.
type PipelineResult struct {
	Success       bool              `json:"success"`
	ModelID       string            `json:"model_id"`
	Symbol        string            `json:"symbol"`
	BarsLoaded    int               `json:"bars_loaded"`
	FeaturesGenerated int           `json:"features_generated"`
	TrainSamples  int               `json:"train_samples"`
	TestSamples   int               `json:"test_samples"`
	Metrics       map[string]any    `json:"metrics"`
	FeatureNames  []string          `json:"feature_names"`
	DurationMs    int64             `json:"duration_ms"`
	Error         string            `json:"error,omitempty"`
}

// TrainingPipeline orchestrates data loading, feature generation, and model training.
type TrainingPipeline struct {
	client     *Client
	downloader *data.Downloader
}

// NewTrainingPipeline creates a pipeline with the given ML client and data downloader.
func NewTrainingPipeline(client *Client, downloader *data.Downloader) *TrainingPipeline {
	return &TrainingPipeline{
		client:     client,
		downloader: downloader,
	}
}

// Run executes the full training pipeline.
func (p *TrainingPipeline) Run(cfg PipelineConfig) (*PipelineResult, error) {
	start := time.Now()
	result := &PipelineResult{
		ModelID: cfg.ModelID,
		Symbol:  cfg.Symbol,
	}

	if cfg.ModelID == "" {
		cfg.ModelID = fmt.Sprintf("%s_%s_%d", cfg.Symbol, cfg.Interval, time.Now().Unix())
		result.ModelID = cfg.ModelID
	}

	// 1. Load historical data
	bars, err := p.loadBars(cfg)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	result.BarsLoaded = len(bars)

	if len(bars) < 100 {
		result.Error = "insufficient data (< 100 bars)"
		return result, fmt.Errorf("%s", result.Error)
	}

	// 2. Generate features locally
	featureBars, labels := p.generateFeaturesAndLabels(bars, cfg.FeaturePeriods, cfg.LabelHorizon)
	if len(featureBars) == 0 {
		result.Error = "no feature vectors generated"
		return result, fmt.Errorf("%s", result.Error)
	}
	result.FeaturesGenerated = len(featureBars)

	// 3. Train model via ML server
	trainResult, err := p.client.Train(TrainConfig{
		ModelID:       cfg.ModelID,
		ModelType:     cfg.ModelType,
		TaskType:      cfg.TaskType,
		Symbol:        cfg.Symbol,
		Interval:      cfg.Interval,
		Bars:          featureBars,
		FeatureConfig: map[string]any{"periods": cfg.FeaturePeriods},
		LabelConfig:   map[string]any{"horizon": cfg.LabelHorizon, "label_type": cfg.TaskType},
		ModelParams:   cfg.ModelParams,
	})
	if err != nil {
		result.Error = err.Error()
		return result, err
	}

	result.Success = trainResult.Success
	result.TrainSamples = trainResult.TrainSamples
	result.TestSamples = trainResult.TestSamples
	result.Metrics = trainResult.Metrics
	result.FeatureNames = p.getFeatureNames(cfg.FeaturePeriods)
	result.DurationMs = time.Since(start).Milliseconds()

	// Log label distribution for debugging
	_ = labels

	return result, nil
}

// loadBars loads historical bars from local storage or downloads from exchange.
func (p *TrainingPipeline) loadBars(cfg PipelineConfig) ([]data.OHLCV, error) {
	if p.downloader == nil {
		return nil, fmt.Errorf("downloader not available")
	}

	// Calculate time range
	toMs := time.Now().UnixMilli()
	fromMs := time.Now().AddDate(0, 0, -cfg.LookbackDays).UnixMilli()

	// Try local storage first
	bars := p.downloader.LoadBarsForBacktest(cfg.Symbol, cfg.Interval, fromMs, toMs)
	if len(bars) > 0 {
		// Convert model.Bar back to data.OHLCV
		ohlcv := make([]data.OHLCV, len(bars))
		for i, b := range bars {
			ohlcv[i] = data.OHLCV{
				Symbol:   b.Symbol,
				Interval: b.Interval,
				Time:     b.Time,
				Open:     b.Open,
				High:     b.High,
				Low:      b.Low,
				Close:    b.Close,
				Volume:   b.Volume,
			}
		}
		return ohlcv, nil
	}

	// Download from exchange is disabled — data must be pre-loaded into local storage
	return nil, fmt.Errorf("本地存储中没有 %s %s 的历史数据（%d天）。请通过数据导入功能或数据库 seeding 预先加载数据", cfg.Symbol, cfg.Interval, cfg.LookbackDays)
}

// generateFeaturesAndLabels computes feature vectors and labels from OHLCV bars.
// Returns bars formatted for ML server and label values for inspection.
func (p *TrainingPipeline) generateFeaturesAndLabels(bars []data.OHLCV, periods []int, horizon int) ([]map[string]any, []float64) {
	if len(periods) == 0 {
		periods = []int{5, 10, 20, 50}
	}
	maxPeriod := maxInt(periods)
	minBars := maxPeriod + horizon + 10
	if len(bars) < minBars {
		return nil, nil
	}

	calc := NewFeatureCalculator(periods)
	var featureBars []map[string]any
	var labels []float64

	for i := minBars; i < len(bars); i++ {
		window := make([]OHLCV, 0, maxPeriod+5)
		for j := i - maxPeriod - 5; j <= i; j++ {
			if j >= 0 {
				window = append(window, OHLCV{
					Open:   bars[j].Open,
					High:   bars[j].High,
					Low:    bars[j].Low,
					Close:  bars[j].Close,
					Volume: bars[j].Volume,
				})
			}
		}

		features := calc.Compute(window)
		if features == nil || len(features) == 0 {
			continue
		}

		// Label: future return at horizon
		futureIdx := i + horizon
		if futureIdx >= len(bars) {
			continue
		}
		currentClose := bars[i].Close
		futureClose := bars[futureIdx].Close
		label := 0.0
		if currentClose > 0 {
			label = (futureClose - currentClose) / currentClose
		}

		// Build bar map with features + label
		barMap := map[string]any{
			"time":   bars[i].Time,
			"open":   bars[i].Open,
			"high":   bars[i].High,
			"low":    bars[i].Low,
			"close":  bars[i].Close,
			"volume": bars[i].Volume,
			"label":  label,
		}
		for k, v := range features {
			barMap[k] = v
		}

		featureBars = append(featureBars, barMap)
		labels = append(labels, label)
	}

	return featureBars, labels
}

// getFeatureNames returns the list of feature names for the given periods.
// Uses FeatureNames() to derive names without requiring dummy data.
func (p *TrainingPipeline) getFeatureNames(periods []int) []string {
	if len(periods) == 0 {
		periods = []int{5, 10, 20, 50}
	}
	calc := NewFeatureCalculator(periods)
	return calc.FeatureNames()
}

// ── Model Evaluator ───────────────────────────────────────────

// Evaluator assesses model performance on out-of-sample data.
type Evaluator struct {
	client *Client
}

// NewEvaluator creates a model evaluator.
func NewEvaluator(client *Client) *Evaluator {
	return &Evaluator{client: client}
}

// Evaluate runs the model on test data and computes metrics.
func (e *Evaluator) Evaluate(modelID string, testBars []map[string]any) (*EvalResult, error) {
	if len(testBars) == 0 {
		return nil, fmt.Errorf("no test data")
	}

	predictions := make([]float64, 0, len(testBars))
	actuals := make([]float64, 0, len(testBars))

	for _, bar := range testBars {
		label, ok := bar["label"].(float64)
		if !ok {
			continue
		}

		// Remove label before prediction
		predictBar := make(map[string]any)
		for k, v := range bar {
			if k != "label" {
				predictBar[k] = v
			}
		}

		result, err := e.client.Predict(PredictInput{
			ModelID: modelID,
			Bars:    []map[string]any{predictBar},
		})
		if err != nil {
			continue
		}

		predictions = append(predictions, result.Prediction)
		actuals = append(actuals, label)
	}

	if len(predictions) == 0 {
		return nil, fmt.Errorf("no predictions made")
	}

	return computeMetrics(predictions, actuals), nil
}

// EvalResult holds evaluation metrics.
type EvalResult struct {
	Samples     int     `json:"samples"`
	MSE         float64 `json:"mse"`
	RMSE        float64 `json:"rmse"`
	MAE         float64 `json:"mae"`
	R2          float64 `json:"r2"`
	Directional float64 `json:"directional_accuracy"` // % of correct direction predictions
	Sharpe      float64 `json:"sharpe"`               // strategy sharpe from predictions
}

func computeMetrics(pred, actual []float64) *EvalResult {
	n := len(pred)
	if n == 0 {
		return nil
	}

	var sumSqErr, sumAbsErr, sumPred, sumActual float64
	correctDir := 0

	for i := 0; i < n; i++ {
		err := pred[i] - actual[i]
		sumSqErr += err * err
		sumAbsErr += math.Abs(err)
		sumPred += pred[i]
		sumActual += actual[i]

		if (pred[i] > 0 && actual[i] > 0) || (pred[i] < 0 && actual[i] < 0) || (pred[i] == 0 && actual[i] == 0) {
			correctDir++
		}
	}

	mse := sumSqErr / float64(n)
	rmse := math.Sqrt(mse)
	mae := sumAbsErr / float64(n)

	// R²
	meanActual := sumActual / float64(n)
	var ssTot float64
	for i := 0; i < n; i++ {
		d := actual[i] - meanActual
		ssTot += d * d
	}
	var r2 float64
	if ssTot > 0 {
		r2 = 1 - sumSqErr/ssTot
	}

	// Sharpe of prediction-based strategy
	returns := make([]float64, n-1)
	for i := 1; i < n; i++ {
		if pred[i-1] > 0 {
			returns[i-1] = actual[i]
		} else if pred[i-1] < 0 {
			returns[i-1] = -actual[i]
		}
	}
	sharpe := sharpeRatio(returns)

	return &EvalResult{
		Samples:     n,
		MSE:         mse,
		RMSE:        rmse,
		MAE:         mae,
		R2:          r2,
		Directional: float64(correctDir) / float64(n) * 100,
		Sharpe:      sharpe,
	}
}

func sharpeRatio(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	var variance float64
	for _, r := range returns {
		d := r - mean
		variance += d * d
	}
	std := math.Sqrt(variance / float64(len(returns)-1))
	if std == 0 {
		return 0
	}
	return mean / std * math.Sqrt(float64(len(returns)))
}

func maxInt(vals []int) int {
	m := 0
	for _, v := range vals {
		if v > m {
			m = v
		}
	}
	return m
}
