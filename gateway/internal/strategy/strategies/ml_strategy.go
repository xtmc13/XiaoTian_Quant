package strategies

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/ml"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/strategy"
)

func minInt(a, b int) int {
	if a < b { return a }
	return b
}

// ── MLStrategy ─────────────────────────────────────────────────
// Uses a trained ML model to generate trading signals on each bar.
// Automatically retrains the model at intervals for online learning.

type MLStrategy struct {
	strategy.BaseStrategy
	name    string
	symbol  string
	running bool
	mu      sync.RWMutex

	// ML config
	modelID       string
	mlClient      *ml.Client
	predictor     *ml.Predictor  // local ONNX-style inference (no HTTP)
	useLocal      bool           // true = use local Predictor, false = use HTTP
	predictBars   int
	minConfidence float64

	// State
	bars        []model.Bar
	inPosition bool
	entryPrice float64
	direction   string

	// Retrain
	retrainInterval time.Duration
	lastRetrain     time.Time
	retrainBars     int

	// Parameter registry
	params *strategy.ParamRegistry
}

func NewMLStrategy(modelID string, mlClient *ml.Client) *MLStrategy {
	s := &MLStrategy{
		name:            "ml_" + modelID[:minInt(12, len(modelID))],
		symbol:          "BTCUSDT",
		modelID:         modelID,
		mlClient:        mlClient,
		predictor:       ml.NewPredictor(),
		predictBars:     200,
		minConfidence:   0.3,
		retrainInterval: 24 * time.Hour,
		retrainBars:     500,
	}
	// Register parameters for hyperopt and frontend configuration
	s.params = strategy.NewParamRegistry()
	s.params.Register(strategy.IntParameter("predict_bars", 200, 50, 1000, "buy"))
	s.params.Register(strategy.FloatParameter("min_confidence", 0.3, 0.05, 0.9, 0.05, "buy"))
	s.params.Register(strategy.IntParameter("retrain_bars", 500, 100, 5000, "buy"))
	return s
}

func (s *MLStrategy) Name() string   { return s.name }
func (s *MLStrategy) Symbol() string { return s.symbol }

func (s *MLStrategy) Params() map[string]any {
	return map[string]any{
		"model_id":        s.modelID,
		"symbol":          s.symbol,
		"predict_bars":    s.predictBars,
		"min_confidence":  s.minConfidence,
		"retrain_interval": s.retrainInterval.String(),
	}
}

func (s *MLStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *MLStrategy) Start(params map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if v, ok := params["symbol"].(string); ok && v != "" {
		s.symbol = v
	}
	if v, ok := params["model_id"].(string); ok && v != "" {
		s.modelID = v
	}

	if err := s.ApplyParams(params); err != nil {
		return fmt.Errorf("ml strategy apply params: %w", err)
	}

	if s.predictBars <= 0 {
		s.predictBars = 200
	}

	s.running = true
	s.bars = s.bars[:0]
	s.inPosition = false
	s.lastRetrain = time.Now()

	// Try to load model locally for zero-latency inference
	if s.mlClient != nil {
		exported, err := s.mlClient.ExportModel(s.modelID)
		if err == nil && exported != nil {
			data, _ := json.Marshal(exported)
			if err := s.predictor.Load(data); err == nil {
				s.useLocal = true
				log.Printf("[ml_strategy] %s: loaded model locally (%d trees, %d features)",
					s.name, len(exported.Trees), len(exported.FeatureNames))
			}
		}
	}
	if !s.useLocal {
		log.Printf("[ml_strategy] %s: using HTTP mode (local load failed or skipped)", s.name)
	}

	log.Printf("[ml_strategy] %s started: model=%s symbol=%s mode=%s",
		s.name, s.modelID, s.symbol, map[bool]string{true: "local", false: "http"}[s.useLocal])
	return nil
}

// ── Parameter system implementation ────────────────────────────

func (s *MLStrategy) GetParameters() *strategy.ParamRegistry {
	return s.params
}

func (s *MLStrategy) ValidateParams() error {
	if s.params == nil {
		return nil
	}
	return s.params.Validate()
}

func (s *MLStrategy) ApplyParams(m map[string]any) error {
	if s.params == nil {
		return nil
	}
	if err := s.params.FromMap(m); err != nil {
		return err
	}
	if p := s.params.Get("predict_bars"); p != nil {
		s.predictBars = p.GetInt()
	}
	if p := s.params.Get("min_confidence"); p != nil {
		s.minConfidence = p.GetFloat()
	}
	if p := s.params.Get("retrain_bars"); p != nil {
		s.retrainBars = p.GetInt()
	}
	return nil
}

func (s *MLStrategy) ParamDefs() []map[string]any {
	if s.params == nil {
		return nil
	}
	return s.params.ToJSONDefs()
}

func (s *MLStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.inPosition = false
	log.Printf("[ml_strategy] %s stopped", s.name)
	return nil
}

func (s *MLStrategy) OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *MLStrategy) OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *MLStrategy) OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error) {
	return nil, nil
}

func (s *MLStrategy) OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil, nil
	}

	// Track recent bars
	s.bars = append(s.bars, bar)
	if len(s.bars) > s.predictBars*2 {
		s.bars = s.bars[len(s.bars)-s.predictBars*2:]
	}

	// Need enough bars to make a prediction
	if len(s.bars) < s.predictBars {
		return nil, nil
	}

	// Check if we should retrain
	if time.Since(s.lastRetrain) > s.retrainInterval {
		s.mu.Unlock()
		s.retrain()
		s.mu.Lock()
		s.lastRetrain = time.Now()
	}

	// Check exit conditions for open position
	if s.inPosition {
		return s.checkExit(bar), nil
	}

	// Make ML prediction
	prediction, err := s.predict()
	if err != nil {
		log.Printf("[ml_strategy] predict error: %v", err)
		return nil, nil
	}

	// Generate signal based on prediction
	if prediction > s.minConfidence {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "LONG"
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "LONG",
			Strength:  math.Min(prediction, 1.0),
			Strategy:  s.name,
			Reason:    fmt.Sprintf("ML prediction: %.4f", prediction),
		}, nil
	} else if prediction < -s.minConfidence {
		s.inPosition = true
		s.entryPrice = bar.Close
		s.direction = "SHORT"
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "SHORT",
			Strength:  math.Min(-prediction, 1.0),
			Strategy:  s.name,
			Reason:    fmt.Sprintf("ML prediction: %.4f", prediction),
		}, nil
	}

	return nil, nil
}

func (s *MLStrategy) checkExit(bar model.Bar) *model.Signal {
	// Make a fresh prediction — if direction changes, exit
	prediction, err := s.predict()
	if err != nil {
		return nil
	}

	shouldExit := false
	if s.direction == "LONG" && prediction < -0.1 {
		shouldExit = true
	} else if s.direction == "SHORT" && prediction > 0.1 {
		shouldExit = true
	}

	if shouldExit {
		s.inPosition = false
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  1.0,
			Strategy:  s.name,
			Reason:    fmt.Sprintf("ML signal reversed: %.4f", prediction),
		}
	}

	// Stop loss: 5%
	if s.direction == "LONG" && bar.Close <= s.entryPrice*0.95 {
		s.inPosition = false
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  1.0,
			Strategy:  s.name,
			Reason:    "ML strategy stop loss (5%)",
		}
	}
	if s.direction == "SHORT" && bar.Close >= s.entryPrice*1.05 {
		s.inPosition = false
		return &model.Signal{
			Symbol:    s.symbol,
			Direction: "CLOSE",
			Strength:  1.0,
			Strategy:  s.name,
			Reason:    "ML strategy stop loss (5%)",
		}
	}

	return nil
}

// predict calls either the local predictor or the ML server.
func (s *MLStrategy) predict() (float64, error) {
	bars := s.bars
	if len(bars) > s.predictBars {
		bars = bars[len(bars)-s.predictBars:]
	}

	if s.useLocal && s.predictor.IsLoaded() {
		// Local mode: compute features + predict (no HTTP, microseconds)
		ohlcv := make([]ml.OHLCV, len(bars))
		for i, b := range bars {
			ohlcv[i] = ml.OHLCV{Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume}
		}
		calc := ml.NewFeatureCalculator([]int{5, 10, 20, 50})
		features := calc.Compute(ohlcv)

		return s.predictor.PredictFromMap(features)
	}

	// HTTP mode: call ML server
	if s.mlClient == nil {
		return 0, fmt.Errorf("no ML client")
	}

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

	result, err := s.mlClient.Predict(ml.PredictInput{
		ModelID: s.modelID,
		Bars:    barMaps,
	})
	if err != nil {
		return 0, err
	}

	return result.Prediction, nil
}

// retrain triggers model retraining with recent data.
func (s *MLStrategy) retrain() {
	log.Printf("[ml_strategy] %s: retraining model %s...", s.name, s.modelID)

	bars := s.bars
	if len(bars) > s.retrainBars {
		bars = bars[len(bars)-s.retrainBars:]
	}

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

	_, err := s.mlClient.Train(ml.TrainConfig{
		ModelID:   s.modelID,
		ModelType: "lightgbm",
		TaskType:  "regression",
		Symbol:    s.symbol,
		Interval:  "1h",
		Bars:      barMaps,
		LabelConfig: map[string]any{
			"label_type": "regression",
			"horizon":    5,
		},
	})
	if err != nil {
		log.Printf("[ml_strategy] retrain failed: %v", err)
	} else {
		log.Printf("[ml_strategy] %s: retrain complete", s.name)
	}
}

// ── ML Strategy Creator ─────────────────────────────────────────

// MLCreator is a factory that creates ML strategies for different models.
type MLCreator struct {
	mlClient *ml.Client
}

func NewMLCreator(mlServerURL string) *MLCreator {
	return &MLCreator{
		mlClient: ml.NewClient(mlServerURL),
	}
}

// Create makes a new ML strategy for the given model ID.
func (c *MLCreator) Create(modelID string) *MLStrategy {
	return NewMLStrategy(modelID, c.mlClient)
}

// ListAvailable returns model IDs available for strategy creation.
func (c *MLCreator) ListAvailable() []string {
	models, err := c.mlClient.ListModels()
	if err != nil {
		return nil
	}
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ModelID
	}
	return ids
}

