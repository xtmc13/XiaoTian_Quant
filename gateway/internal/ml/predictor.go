package ml

import (
	"encoding/json"
	"fmt"
	"math"
)

// ── Tree Model ─────────────────────────────────────────────────

// TreeNode is a single node in a decision tree.
// Either "leaf" is set (leaf node), or "feature"+"threshold"+"left"+"right" (split node).
type TreeNode struct {
	Leaf      *float64  `json:"leaf,omitempty"`
	Feature   string    `json:"feature,omitempty"`
	Threshold float64   `json:"threshold,omitempty"`
	Left      *TreeNode `json:"left,omitempty"`
	Right     *TreeNode `json:"right,omitempty"`
}

// TreeEnsemble is a collection of decision trees (gradient boosting ensemble).
type TreeEnsemble struct {
	Trees        []TreeNode `json:"trees"`
	FeatureNames []string   `json:"feature_names"`
	TaskType     string     `json:"task_type"` // regression or classification
	BaseScore    float64    `json:"base_score"` // initial prediction (usually mean of y)
}

// ExportedModel is the format exported from Python ML server.
type ExportedModel struct {
	Success       bool            `json:"success"`
	ModelID       string          `json:"model_id"`
	ModelType     string          `json:"model_type"`
	TaskType      string          `json:"task_type"`
	FeatureNames  []string        `json:"feature_names"`
	FeatureConfig map[string]any  `json:"feature_config,omitempty"`
	LabelConfig   map[string]any  `json:"label_config,omitempty"`
	Trees         []TreeNode      `json:"trees"`
}

// Predictor performs inference using loaded tree ensembles.
type Predictor struct {
	ensemble *TreeEnsemble
	loaded   bool
}

// NewPredictor creates a new predictor.
func NewPredictor() *Predictor {
	return &Predictor{}
}

// Load loads an exported model from JSON bytes.
func (p *Predictor) Load(data []byte) error {
	var exported ExportedModel
	if err := json.Unmarshal(data, &exported); err != nil {
		return fmt.Errorf("predictor load: %w", err)
	}

	if len(exported.Trees) == 0 {
		return fmt.Errorf("predictor load: no trees in model")
	}

	p.ensemble = &TreeEnsemble{
		Trees:        exported.Trees,
		FeatureNames: exported.FeatureNames,
		TaskType:     exported.TaskType,
		BaseScore:    0,
	}
	p.loaded = true
	return nil
}

// LoadFromFile loads a model from a JSON file.
func (p *Predictor) LoadFromFile(path string) error {
	return fmt.Errorf("not implemented: use Load with bytes from ML server export")
}

// IsLoaded returns true if a model is loaded.
func (p *Predictor) IsLoaded() bool {
	return p.loaded
}

// Predict makes a single prediction given feature values.
// features must be in the same order as FeatureNames.
func (p *Predictor) Predict(features []float64) (float64, error) {
	if !p.loaded {
		return 0, fmt.Errorf("predictor: no model loaded")
	}

	if len(features) != len(p.ensemble.FeatureNames) {
		return 0, fmt.Errorf("predictor: expected %d features, got %d",
			len(p.ensemble.FeatureNames), len(features))
	}

	// Build feature index map
	featIdx := make(map[string]int, len(p.ensemble.FeatureNames))
	for i, name := range p.ensemble.FeatureNames {
		featIdx[name] = i
	}

	// Sum predictions from all trees (gradient boosting)
	var total float64
	for i := range p.ensemble.Trees {
		total += p.predictTree(&p.ensemble.Trees[i], features, featIdx)
	}

	return total + p.ensemble.BaseScore, nil
}

// PredictFromMap predicts using named features (maps feature name → value).
func (p *Predictor) PredictFromMap(features map[string]float64) (float64, error) {
	vals := make([]float64, len(p.ensemble.FeatureNames))
	for i, name := range p.ensemble.FeatureNames {
		v, ok := features[name]
		if !ok {
			v = 0 // missing feature defaults to 0
		}
		vals[i] = v
	}
	return p.Predict(vals)
}

// predictTree traverses a single tree and returns the leaf value.
func (p *Predictor) predictTree(node *TreeNode, features []float64, featIdx map[string]int) float64 {
	if node.Leaf != nil {
		return *node.Leaf
	}

	// Get feature value
	idx, ok := featIdx[node.Feature]
	if !ok {
		// Unknown feature — go left by default
		if node.Left != nil {
			return p.predictTree(node.Left, features, featIdx)
		}
		return 0
	}

	value := features[idx]
	if value <= node.Threshold {
		if node.Left != nil {
			return p.predictTree(node.Left, features, featIdx)
		}
	} else {
		if node.Right != nil {
			return p.predictTree(node.Right, features, featIdx)
		}
	}

	// Fallback: shouldn't happen with well-formed trees
	return 0
}

// ── Feature Calculator ─────────────────────────────────────────

// FeatureCalculator computes the same features used during training from raw OHLCV bars.
// This mirrors FeatureEngine.transform() in Python.
type FeatureCalculator struct {
	periods []int
}

// NewFeatureCalculator creates a feature calculator with default periods.
func NewFeatureCalculator(periods []int) *FeatureCalculator {
	if len(periods) == 0 {
		periods = []int{5, 10, 20, 50}
	}
	return &FeatureCalculator{periods: periods}
}

// OHLCV is a single price bar.
type OHLCV struct {
	Open, High, Low, Close, Volume float64
}

// Compute calculates features from a window of OHLCV bars.
// Returns a map of feature name → value.
func (fc *FeatureCalculator) Compute(bars []OHLCV) map[string]float64 {
	if len(bars) < 2 {
		return nil
	}

	n := len(bars)
	features := make(map[string]float64)
	closePrices := make([]float64, n)
	volumes := make([]float64, n)

	for i, b := range bars {
		closePrices[i] = b.Close
		volumes[i] = b.Volume
	}

	c := closePrices[n-1] // latest close

	// Returns
	for _, p := range fc.periods {
		if n > p {
			prev := closePrices[n-1-p]
			if prev > 0 {
				features[fmt.Sprintf("return_%d", p)] = (c - prev) / prev
				features[fmt.Sprintf("log_return_%d", p)] = math.Log(c / prev)
			}
		}
	}

	// Price vs MA
	for _, p := range fc.periods {
		ma := fc.sma(closePrices, p)
		if ma > 0 {
			features[fmt.Sprintf("price_vs_ma_%d", p)] = (c - ma) / ma
		}
	}

	// HL range
	features["hl_range"] = (bars[n-1].High - bars[n-1].Low) / c

	// OC gap
	if bars[n-1].Open > 0 {
		features["oc_gap"] = (c - bars[n-1].Open) / bars[n-1].Open
	}

	// Volume ratio
	for _, p := range fc.periods {
		vma := fc.sma(volumes, p)
		if vma > 0 {
			features[fmt.Sprintf("volume_ratio_%d", p)] = volumes[n-1] / vma
		}
	}

	// Volatility (rolling std of returns)
	returns := make([]float64, n-1)
	for i := 1; i < n; i++ {
		if closePrices[i-1] > 0 {
			returns[i-1] = (closePrices[i] - closePrices[i-1]) / closePrices[i-1]
		}
	}
	for _, p := range fc.periods {
		if n > p {
			features[fmt.Sprintf("volatility_%d", p)] = fc.std(returns[n-p:], p) * math.Sqrt(float64(p))
		}
	}

	// BB position
	for _, p := range fc.periods {
		ma := fc.sma(closePrices, p)
		std := fc.std(closePrices[n-p:], p)
		if std > 0 {
			features[fmt.Sprintf("bb_position_%d", p)] = (c - ma) / std
		}
		features[fmt.Sprintf("bb_width_%d", p)] = (2 * std) / max(ma, 0.01)
	}

	// MACD
	ema12 := fc.ema(closePrices, 12)
	ema26 := fc.ema(closePrices, 26)
	features["macd"] = ema12 - ema26

	// Z-score
	for _, p := range fc.periods {
		ma := fc.sma(closePrices, p)
		std := fc.std(closePrices[n-p:], p)
		if std > 0 {
			features[fmt.Sprintf("zscore_%d", p)] = (c - ma) / std
		}
	}

	return features
}

func (fc *FeatureCalculator) sma(data []float64, period int) float64 {
	if period > len(data) {
		period = len(data)
	}
	if period == 0 {
		return 0
	}
	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i]
	}
	return sum / float64(period)
}

func (fc *FeatureCalculator) ema(data []float64, period int) float64 {
	if len(data) < period {
		return fc.sma(data, len(data))
	}
	alpha := 2.0 / float64(period+1)
	ema := data[0]
	for i := 1; i < len(data); i++ {
		ema = alpha*data[i] + (1-alpha)*ema
	}
	return ema
}

func (fc *FeatureCalculator) std(data []float64, period int) float64 {
	if period > len(data) {
		period = len(data)
	}
	if period < 2 {
		return 0
	}
	slice := data[len(data)-period:]
	mean := fc.sma(slice, period)
	sumSq := 0.0
	for _, v := range slice {
		sumSq += (v - mean) * (v - mean)
	}
	return math.Sqrt(sumSq / float64(period-1))
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
