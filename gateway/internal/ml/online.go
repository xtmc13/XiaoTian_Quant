package ml

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/data"
)

// ── Online Learner ─────────────────────────────────────────────

// OnlineConfig configures the online learning behavior.
type OnlineConfig struct {
	ModelID        string        `json:"model_id"`         // base model identifier
	Symbol         string        `json:"symbol"`
	Interval       string        `json:"interval"`
	ModelType      string        `json:"model_type"`       // lightgbm, xgboost
	TaskType       string        `json:"task_type"`        // regression, classification
	RetrainEvery   time.Duration `json:"retrain_every"`    // e.g. 1h, 24h
	WindowDays     int           `json:"window_days"`      // training data window
	FeaturePeriods []int         `json:"feature_periods"`
	LabelHorizon int           `json:"label_horizon"`
	ModelParams    map[string]any `json:"model_params,omitempty"`
	Enabled        bool          `json:"enabled"`
}

func DefaultOnlineConfig() OnlineConfig {
	return OnlineConfig{
		Symbol:         "BTCUSDT",
		Interval:       "1h",
		ModelType:      "lightgbm",
		TaskType:       "regression",
		RetrainEvery:   24 * time.Hour,
		WindowDays:     90,
		FeaturePeriods: []int{5, 10, 20, 50},
		LabelHorizon:   5,
		Enabled:        true,
	}
}

// OnlineLearner manages incremental model updates via rolling-window retraining.
type OnlineLearner struct {
	cfg        OnlineConfig
	pipeline   *TrainingPipeline
	client     *Client
	downloader *data.Downloader

	mu         sync.RWMutex
	lastTrain  time.Time
	lastModelID string
	lastMetrics *PipelineResult
	running    bool
	cancel     context.CancelFunc

	// Callbacks
	OnRetrain   func(result *PipelineResult)
	OnError     func(err error)
	OnDrift     func(psi float64, triggered bool)
}

// NewOnlineLearner creates an online learner.
func NewOnlineLearner(cfg OnlineConfig, client *Client, downloader *data.Downloader) *OnlineLearner {
	pipeline := NewTrainingPipeline(client, downloader)
	return &OnlineLearner{
		cfg:        cfg,
		pipeline:   pipeline,
		client:     client,
		downloader: downloader,
	}
}

// Start begins the background retraining loop.
func (ol *OnlineLearner) Start(ctx context.Context) {
	ol.mu.Lock()
	if ol.running {
		ol.mu.Unlock()
		return
	}
	ol.running = true
	innerCtx, cancel := context.WithCancel(ctx)
	ol.cancel = cancel
	ol.mu.Unlock()

	// Immediate first training
	go ol.retrain(innerCtx)

	// Periodic retraining (skip if interval is zero or negative)
	if ol.cfg.RetrainEvery <= 0 {
		return
	}
	ticker := time.NewTicker(ol.cfg.RetrainEvery)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-innerCtx.Done():
				return
			case <-ticker.C:
				ol.retrain(innerCtx)
			}
		}
	}()
}

// Stop halts the retraining loop.
func (ol *OnlineLearner) Stop() {
	ol.mu.Lock()
	if ol.cancel != nil {
		ol.cancel()
	}
	ol.running = false
	ol.mu.Unlock()
}

// IsRunning returns true if the learner is active.
func (ol *OnlineLearner) IsRunning() bool {
	ol.mu.RLock()
	defer ol.mu.RUnlock()
	return ol.running
}

// LastResult returns the most recent training result.
func (ol *OnlineLearner) LastResult() *PipelineResult {
	ol.mu.RLock()
	defer ol.mu.RUnlock()
	return ol.lastMetrics
}

// LastModelID returns the ID of the most recently trained model.
func (ol *OnlineLearner) LastModelID() string {
	ol.mu.RLock()
	defer ol.mu.RUnlock()
	return ol.lastModelID
}

// TriggerRetrain forces an immediate retraining (non-blocking).
func (ol *OnlineLearner) TriggerRetrain() {
	ol.mu.RLock()
	ctx := context.Background()
	ol.mu.RUnlock()
	go ol.retrain(ctx)
}

// ── Internal ───────────────────────────────────────────────────

func (ol *OnlineLearner) retrain(ctx context.Context) {
	log.Printf("[online-learner] starting retrain for %s %s", ol.cfg.Symbol, ol.cfg.Interval)

	modelID := fmt.Sprintf("%s_online_%d", ol.cfg.ModelID, time.Now().Unix())

	cfg := PipelineConfig{
		Symbol:         ol.cfg.Symbol,
		Interval:       ol.cfg.Interval,
		ModelID:        modelID,
		ModelType:      ol.cfg.ModelType,
		TaskType:       ol.cfg.TaskType,
		LookbackDays:   ol.cfg.WindowDays,
		FeaturePeriods: ol.cfg.FeaturePeriods,
		LabelHorizon:   ol.cfg.LabelHorizon,
		ModelParams:    ol.cfg.ModelParams,
	}

	result, err := ol.pipeline.Run(cfg)
	if err != nil {
		log.Printf("[online-learner] retrain failed: %v", err)
		if ol.OnError != nil {
			ol.OnError(err)
		}
		return
	}

	if !result.Success {
		log.Printf("[online-learner] retrain unsuccessful: %s", result.Error)
		if ol.OnError != nil {
			ol.OnError(fmt.Errorf("training unsuccessful: %s", result.Error))
		}
		return
	}

	ol.mu.Lock()
	ol.lastTrain = time.Now()
	ol.lastModelID = result.ModelID
	ol.lastMetrics = result
	ol.mu.Unlock()

	log.Printf("[online-learner] retrain complete: model=%s samples=%d features=%d duration=%dms",
		result.ModelID, result.TrainSamples, result.FeaturesGenerated, result.DurationMs)

	if ol.OnRetrain != nil {
		ol.OnRetrain(result)
	}
}

// ── OnlineLearner Manager (multi-model) ──────────────────────

// OnlineManager manages multiple online learners keyed by model ID.
type OnlineManager struct {
	mu       sync.RWMutex
	learners map[string]*OnlineLearner
}

// NewOnlineManager creates a manager.
func NewOnlineManager() *OnlineManager {
	return &OnlineManager{
		learners: make(map[string]*OnlineLearner),
	}
}

// Register adds a new online learner.
func (om *OnlineManager) Register(cfg OnlineConfig, client *Client, downloader *data.Downloader) *OnlineLearner {
	om.mu.Lock()
	defer om.mu.Unlock()

	if cfg.ModelID == "" {
		cfg.ModelID = fmt.Sprintf("%s_%s", cfg.Symbol, cfg.Interval)
	}

	ol := NewOnlineLearner(cfg, client, downloader)
	om.learners[cfg.ModelID] = ol
	return ol
}

// Get returns a registered learner.
func (om *OnlineManager) Get(modelID string) *OnlineLearner {
	om.mu.RLock()
	defer om.mu.RUnlock()
	return om.learners[modelID]
}

// StartAll starts all registered learners.
func (om *OnlineManager) StartAll(ctx context.Context) {
	om.mu.RLock()
	defer om.mu.RUnlock()
	for _, ol := range om.learners {
		ol.Start(ctx)
	}
}

// StopAll stops all learners.
func (om *OnlineManager) StopAll() {
	om.mu.RLock()
	defer om.mu.RUnlock()
	for _, ol := range om.learners {
		ol.Stop()
	}
}

// List returns all registered model IDs.
func (om *OnlineManager) List() []string {
	om.mu.RLock()
	defer om.mu.RUnlock()
	ids := make([]string, 0, len(om.learners))
	for id := range om.learners {
		ids = append(ids, id)
	}
	return ids
}
