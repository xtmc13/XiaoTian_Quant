package ml

import (
	"math"
	"sort"
	"time"
)

// ── Drift Detection ────────────────────────────────────────────

// DriftConfig configures drift detection thresholds.
type DriftConfig struct {
	PSIThreshold      float64       `json:"psi_threshold"`       // default 0.25
	FeatureThreshold  float64       `json:"feature_threshold"`   // per-feature PSI alert
	MinSamples        int           `json:"min_samples"`         // min reference samples
	CheckInterval     time.Duration `json:"check_interval"`      // how often to check
	AutoRetrain       bool          `json:"auto_retrain"`        // trigger retrain on drift
}

func DefaultDriftConfig() DriftConfig {
	return DriftConfig{
		PSIThreshold:     0.25,
		FeatureThreshold: 0.10,
		MinSamples:       100,
		CheckInterval:    1 * time.Hour,
		AutoRetrain:      true,
	}
}

// FeatureDistribution holds the reference distribution for a single feature.
type FeatureDistribution struct {
	Name      string    `json:"name"`
	Bins      []float64 `json:"bins"`       // bin boundaries (10 equal-frequency)
	RefCounts []int     `json:"ref_counts"` // reference sample counts per bin
	RefTotal  int       `json:"ref_total"`
	UpdatedAt int64     `json:"updated_at"`
}

// DriftDetector monitors feature distributions and detects drift via PSI.
type DriftDetector struct {
	cfg            DriftConfig
	reference      map[string]*FeatureDistribution
	currentWindow  map[string][]float64 // feature name → recent values
}

// NewDriftDetector creates a drift detector.
func NewDriftDetector(cfg DriftConfig) *DriftDetector {
	return &DriftDetector{
		cfg:           cfg,
		reference:     make(map[string]*FeatureDistribution),
		currentWindow: make(map[string][]float64),
	}
}

// BuildReference computes the reference distribution from a set of feature vectors.
func (dd *DriftDetector) BuildReference(vectors []map[string]float64) {
	if len(vectors) < dd.cfg.MinSamples {
		return
	}

	// Collect all feature names
	featureNames := make(map[string]bool)
	for _, v := range vectors {
		for name := range v {
			featureNames[name] = true
		}
	}

	for name := range featureNames {
		// Extract values
		values := make([]float64, 0, len(vectors))
		for _, v := range vectors {
			if val, ok := v[name]; ok && !math.IsNaN(val) {
				values = append(values, val)
			}
		}
		if len(values) < dd.cfg.MinSamples {
			continue
		}

		// Sort and create 10 equal-frequency bins
		sort.Float64s(values)
		numBins := 10
		if len(values) < numBins {
			numBins = len(values)
		}
		bins := make([]float64, numBins+1)
		bins[0] = values[0]
		for i := 1; i < numBins; i++ {
			idx := i * len(values) / numBins
			if idx >= len(values) {
				idx = len(values) - 1
			}
			bins[i] = values[idx]
		}
		bins[numBins] = values[len(values)-1]

		// Count reference samples per bin
		counts := make([]int, numBins)
		for _, v := range values {
			for b := 0; b < numBins; b++ {
				if v >= bins[b] && v < bins[b+1] {
					counts[b]++
					break
				}
				if b == numBins-1 && v >= bins[b] && v <= bins[b+1] {
					counts[b]++
					break
				}
			}
		}

		dd.reference[name] = &FeatureDistribution{
			Name:      name,
			Bins:      bins,
			RefCounts: counts,
			RefTotal:  len(values),
			UpdatedAt: time.Now().Unix(),
		}
	}
}

// AddSample adds a new feature vector to the current monitoring window.
func (dd *DriftDetector) AddSample(features map[string]float64) {
	for name, val := range features {
		if math.IsNaN(val) || math.IsInf(val, 0) {
			continue
		}
		dd.currentWindow[name] = append(dd.currentWindow[name], val)
	}
}

// CheckDrift computes PSI for all features and returns overall + per-feature results.
func (dd *DriftDetector) CheckDrift() DriftResult {
	result := DriftResult{
		Timestamp:     time.Now().Unix(),
		FeaturePSI:    make(map[string]float64),
		Drifted:       false,
		OverallPSI:    0,
	}

	var totalPSI float64
	var featureCount int

	for name, ref := range dd.reference {
		values, ok := dd.currentWindow[name]
		if !ok || len(values) < dd.cfg.MinSamples/2 {
			continue
		}

		// Count current samples per bin
		currCounts := make([]int, len(ref.RefCounts))
		for _, v := range values {
			for b := 0; b < len(ref.Bins)-1; b++ {
				if v >= ref.Bins[b] && v < ref.Bins[b+1] {
					currCounts[b]++
					break
				}
				if b == len(ref.Bins)-2 && v >= ref.Bins[b] && v <= ref.Bins[b+1] {
					currCounts[b]++
					break
				}
			}
		}

		psi := computePSI(ref.RefCounts, currCounts)
		result.FeaturePSI[name] = psi
		totalPSI += psi
		featureCount++

		if psi > dd.cfg.FeatureThreshold {
			result.DriftedFeatures = append(result.DriftedFeatures, name)
		}
	}

	if featureCount > 0 {
		result.OverallPSI = totalPSI / float64(featureCount)
	}
	result.Drifted = result.OverallPSI > dd.cfg.PSIThreshold

	return result
}

// ResetWindow clears the current monitoring window.
func (dd *DriftDetector) ResetWindow() {
	dd.currentWindow = make(map[string][]float64)
}

// ── DriftResult ──────────────────────────────────────────────

// DriftResult holds the outcome of a drift check.
type DriftResult struct {
	Timestamp       int64              `json:"timestamp"`
	OverallPSI      float64            `json:"overall_psi"`
	FeaturePSI      map[string]float64 `json:"feature_psi"`
	Drifted         bool               `json:"drifted"`
	DriftedFeatures []string           `json:"drifted_features"`
}

// ── PSI Computation ────────────────────────────────────────────

// computePSI calculates the Population Stability Index between two distributions.
// PSI = Σ (Actual% - Expected%) × ln(Actual% / Expected%)
// PSI < 0.1: no significant change
// PSI 0.1–0.25: moderate change
// PSI > 0.25: significant change (drift)
func computePSI(expected, actual []int) float64 {
	if len(expected) != len(actual) {
		return math.Inf(1)
	}

	var expTotal, actTotal int
	for _, c := range expected {
		expTotal += c
	}
	for _, c := range actual {
		actTotal += c
	}
	if expTotal == 0 || actTotal == 0 {
		return math.Inf(1)
	}

	psi := 0.0
	epsilon := 1e-10 // avoid log(0)
	for i := range expected {
		expPct := float64(expected[i]) / float64(expTotal)
		actPct := float64(actual[i]) / float64(actTotal)

		// Smoothing: if a bin is empty in either distribution, add epsilon
		if expPct == 0 {
			expPct = epsilon
		}
		if actPct == 0 {
			actPct = epsilon
		}

		diff := actPct - expPct
		psi += diff * math.Log(actPct/expPct)
	}

	return psi
}

// ── Prediction Drift (model output monitoring) ─────────────────

// PredictionDrift tracks the distribution of model predictions over time.
type PredictionDrift struct {
	cfg       DriftConfig
	refMean   float64
	refStd    float64
	refCount  int
	window    []float64
}

// NewPredictionDrift creates a prediction drift monitor.
func NewPredictionDrift(cfg DriftConfig) *PredictionDrift {
	return &PredictionDrift{cfg: cfg}
}

// SetReference establishes the baseline from initial predictions.
func (pd *PredictionDrift) SetReference(predictions []float64) {
	if len(predictions) < pd.cfg.MinSamples {
		return
	}
	pd.refMean = mean(predictions)
	pd.refStd = stddev(predictions)
	pd.refCount = len(predictions)
}

// AddPrediction adds a new prediction to the monitoring window.
func (pd *PredictionDrift) AddPrediction(p float64) {
	pd.window = append(pd.window, p)
}

// Check returns true if the current prediction distribution has drifted.
func (pd *PredictionDrift) Check() (drifted bool, zscore float64) {
	if pd.refStd == 0 || len(pd.window) < pd.cfg.MinSamples/2 {
		return false, 0
	}
	currMean := mean(pd.window)
	zscore = (currMean - pd.refMean) / pd.refStd
	// Drift if mean shifted by more than 2 standard deviations
	return math.Abs(zscore) > 2.0, zscore
}

// Reset clears the monitoring window.
func (pd *PredictionDrift) Reset() {
	pd.window = pd.window[:0]
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func stddev(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	m := mean(vals)
	var sumSq float64
	for _, v := range vals {
		d := v - m
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(vals)-1))
}
