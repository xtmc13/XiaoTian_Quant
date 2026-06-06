package ml

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

// ── Ensemble Voter ─────────────────────────────────────────────

// EnsembleModel wraps a loaded model with its weight and metadata.
type EnsembleModel struct {
	Name      string    `json:"name"`
	ModelType string    `json:"model_type"` // lightgbm, xgboost, custom
	Weight    float64   `json:"weight"`       // voting weight
	Predictor *Predictor `json:"-"`           // loaded model
}

// EnsembleVoter combines predictions from multiple models.
type EnsembleVoter struct {
	mu      sync.RWMutex
	models  []EnsembleModel
	method  string // "weighted", "average", "median", "stacking"
}

// NewEnsembleVoter creates an ensemble voter.
func NewEnsembleVoter(method string) *EnsembleVoter {
	if method == "" {
		method = "weighted"
	}
	return &EnsembleVoter{method: method}
}

// AddModel adds a model to the ensemble.
func (ev *EnsembleVoter) AddModel(name, modelType string, weight float64, predictor *Predictor) {
	ev.mu.Lock()
	defer ev.mu.Unlock()
	ev.models = append(ev.models, EnsembleModel{
		Name:      name,
		ModelType: modelType,
		Weight:    weight,
		Predictor: predictor,
	})
}

// RemoveModel removes a model by name.
func (ev *EnsembleVoter) RemoveModel(name string) {
	ev.mu.Lock()
	defer ev.mu.Unlock()
	filtered := make([]EnsembleModel, 0, len(ev.models))
	for _, m := range ev.models {
		if m.Name != name {
			filtered = append(filtered, m)
		}
	}
	ev.models = filtered
}

// ModelCount returns the number of loaded models.
func (ev *EnsembleVoter) ModelCount() int {
	ev.mu.RLock()
	defer ev.mu.RUnlock()
	return len(ev.models)
}

// Predict combines predictions from all models.
func (ev *EnsembleVoter) Predict(features map[string]float64) (*EnsembleResult, error) {
	ev.mu.RLock()
	defer ev.mu.RUnlock()

	if len(ev.models) == 0 {
		return nil, fmt.Errorf("ensemble: no models loaded")
	}

	var predictions []modelPrediction
	for _, m := range ev.models {
		if m.Predictor == nil || !m.Predictor.IsLoaded() {
			continue
		}
		pred, err := m.Predictor.PredictFromMap(features)
		if err != nil {
			continue
		}
		predictions = append(predictions, modelPrediction{
			Name:       m.Name,
			ModelType:  m.ModelType,
			Weight:     m.Weight,
			Prediction: pred,
		})
	}

	if len(predictions) == 0 {
		return nil, fmt.Errorf("ensemble: no model produced a valid prediction")
	}

	result := &EnsembleResult{
		ModelPredictions: predictions,
	}

	switch ev.method {
	case "average":
		result.FinalPrediction = ev.average(predictions)
	case "median":
		result.FinalPrediction = ev.median(predictions)
	case "weighted":
		result.FinalPrediction = ev.weighted(predictions)
	case "stacking":
		result.FinalPrediction = ev.stacking(predictions)
	default:
		result.FinalPrediction = ev.weighted(predictions)
	}

	// Direction: positive = up, negative = down
	if result.FinalPrediction > 0 {
		result.Direction = "up"
		result.Strength = math.Min(math.Abs(result.FinalPrediction)*100, 100)
	} else if result.FinalPrediction < 0 {
		result.Direction = "down"
		result.Strength = math.Min(math.Abs(result.FinalPrediction)*100, 100)
	} else {
		result.Direction = "neutral"
		result.Strength = 0
	}

	// Agreement: % of models agreeing with final direction
	agreeCount := 0
	for _, p := range predictions {
		if (result.Direction == "up" && p.Prediction > 0) ||
			(result.Direction == "down" && p.Prediction < 0) ||
			(result.Direction == "neutral" && p.Prediction == 0) {
			agreeCount++
		}
	}
	result.Agreement = float64(agreeCount) / float64(len(predictions)) * 100

	return result, nil
}

// ── Ensemble Result ──────────────────────────────────────────

type modelPrediction struct {
	Name       string  `json:"name"`
	ModelType  string  `json:"model_type"`
	Weight     float64 `json:"weight"`
	Prediction float64 `json:"prediction"`
}

// EnsembleResult holds the combined prediction and per-model details.
type EnsembleResult struct {
	FinalPrediction  float64           `json:"final_prediction"`
	Direction        string            `json:"direction"`     // up, down, neutral
	Strength         float64           `json:"strength"`      // 0-100
	Agreement        float64           `json:"agreement"`     // % models agreeing
	ModelPredictions []modelPrediction `json:"predictions"`
}

// ── Aggregation Methods ────────────────────────────────────────

func (ev *EnsembleVoter) average(preds []modelPrediction) float64 {
	var sum float64
	for _, p := range preds {
		sum += p.Prediction
	}
	return sum / float64(len(preds))
}

func (ev *EnsembleVoter) median(preds []modelPrediction) float64 {
	vals := make([]float64, len(preds))
	for i, p := range preds {
		vals[i] = p.Prediction
	}
	sort.Float64s(vals)
	mid := len(vals) / 2
	if len(vals)%2 == 0 {
		return (vals[mid-1] + vals[mid]) / 2
	}
	return vals[mid]
}

func (ev *EnsembleVoter) weighted(preds []modelPrediction) float64 {
	var sumWeight, sumWeighted float64
	for _, p := range preds {
		sumWeight += p.Weight
		sumWeighted += p.Prediction * p.Weight
	}
	if sumWeight == 0 {
		return 0
	}
	return sumWeighted / sumWeight
}

// stacking uses a simple meta-rule: if all tree models agree, trust them;
// if they disagree, reduce confidence by averaging with lower weight.
func (ev *EnsembleVoter) stacking(preds []modelPrediction) float64 {
	// Separate tree models (LightGBM/XGBoost) from others
	var treePreds, otherPreds []modelPrediction
	for _, p := range preds {
		if p.ModelType == "lightgbm" || p.ModelType == "xgboost" {
			treePreds = append(treePreds, p)
		} else {
			otherPreds = append(otherPreds, p)
		}
	}

	if len(treePreds) == 0 {
		return ev.weighted(preds)
	}

	treeAvg := ev.average(treePreds)

	// Check tree agreement
	allSameSign := true
	for _, p := range treePreds {
		if (treeAvg > 0 && p.Prediction < 0) || (treeAvg < 0 && p.Prediction > 0) {
			allSameSign = false
			break
		}
	}

	if allSameSign || len(otherPreds) == 0 {
		return treeAvg
	}

	// Disagreement: blend tree average with other models at reduced weight
	otherAvg := ev.weighted(otherPreds)
	return 0.7*treeAvg + 0.3*otherAvg
}

// ── Ensemble Manager (multi-symbol) ────────────────────────────

// EnsembleManager manages ensemble voters per symbol/strategy.
type EnsembleManager struct {
	mu       sync.RWMutex
	ensembles map[string]*EnsembleVoter // key: "BTCUSDT_1h"
}

// NewEnsembleManager creates a manager.
func NewEnsembleManager() *EnsembleManager {
	return &EnsembleManager{
		ensembles: make(map[string]*EnsembleVoter),
	}
}

// GetOrCreate returns an existing ensemble or creates a new one.
func (em *EnsembleManager) GetOrCreate(key, method string) *EnsembleVoter {
	em.mu.Lock()
	defer em.mu.Unlock()
	if ev, ok := em.ensembles[key]; ok {
		return ev
	}
	ev := NewEnsembleVoter(method)
	em.ensembles[key] = ev
	return ev
}

// Get returns an ensemble by key.
func (em *EnsembleManager) Get(key string) *EnsembleVoter {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return em.ensembles[key]
}

// List returns all ensemble keys.
func (em *EnsembleManager) List() []string {
	em.mu.RLock()
	defer em.mu.RUnlock()
	keys := make([]string, 0, len(em.ensembles))
	for k := range em.ensembles {
		keys = append(keys, k)
	}
	return keys
}

// Remove deletes an ensemble.
func (em *EnsembleManager) Remove(key string) {
	em.mu.Lock()
	defer em.mu.Unlock()
	delete(em.ensembles, key)
}
