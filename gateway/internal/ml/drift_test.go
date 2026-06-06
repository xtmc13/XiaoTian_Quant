package ml

import (
	"math"
	"testing"
)

func TestComputePSI(t *testing.T) {
	// Identical distributions → PSI = 0
	expected := []int{100, 100, 100, 100, 100}
	actual := []int{100, 100, 100, 100, 100}
	psi := computePSI(expected, actual)
	if psi > 0.001 {
		t.Errorf("identical dist should have PSI≈0, got %.4f", psi)
	}

	// Slightly shifted → PSI small
	actual2 := []int{90, 105, 100, 100, 105}
	psi2 := computePSI(expected, actual2)
	if psi2 > 0.1 {
		t.Errorf("slight shift should have PSI<0.1, got %.4f", psi2)
	}

	// Significantly shifted → PSI > 0.25
	actual3 := []int{10, 20, 50, 150, 270}
	psi3 := computePSI(expected, actual3)
	if psi3 < 0.25 {
		t.Errorf("significant shift should have PSI>0.25, got %.4f", psi3)
	}
}

func TestComputePSIEmpty(t *testing.T) {
	psi := computePSI([]int{0, 0}, []int{0, 0})
	if !math.IsInf(psi, 1) {
		t.Error("empty distributions should return +Inf")
	}
}

func TestDriftDetectorBuildReference(t *testing.T) {
	dd := NewDriftDetector(DefaultDriftConfig())

	// Build reference from 200 samples
	vectors := make([]map[string]float64, 200)
	for i := 0; i < 200; i++ {
		vectors[i] = map[string]float64{
			"feature_a": float64(i) * 0.1,
			"feature_b": float64(i%50) * 2.0,
		}
	}
	dd.BuildReference(vectors)

	if len(dd.reference) != 2 {
		t.Errorf("expected 2 features, got %d", len(dd.reference))
	}

	refA, ok := dd.reference["feature_a"]
	if !ok {
		t.Fatal("feature_a should exist")
	}
	if len(refA.Bins) != 11 { // 10 bins = 11 boundaries
		t.Errorf("expected 11 bins, got %d", len(refA.Bins))
	}
	if refA.RefTotal != 200 {
		t.Errorf("expected 200 ref total, got %d", refA.RefTotal)
	}
}

func TestDriftDetectorCheckNoDrift(t *testing.T) {
	dd := NewDriftDetector(DefaultDriftConfig())

	// Reference: uniform distribution
	vectors := make([]map[string]float64, 200)
	for i := 0; i < 200; i++ {
		vectors[i] = map[string]float64{"x": float64(i) * 0.1}
	}
	dd.BuildReference(vectors)

	// Current: same distribution
	for i := 0; i < 200; i++ {
		dd.AddSample(map[string]float64{"x": float64(i) * 0.1})
	}

	result := dd.CheckDrift()
	if result.Drifted {
		t.Errorf("no drift expected, got PSI=%.4f", result.OverallPSI)
	}
	if result.OverallPSI > 0.05 {
		t.Errorf("PSI should be very small, got %.4f", result.OverallPSI)
	}
}

func TestDriftDetectorCheckWithDrift(t *testing.T) {
	dd := NewDriftDetector(DefaultDriftConfig())

	// Reference: values 0-20
	vectors := make([]map[string]float64, 200)
	for i := 0; i < 200; i++ {
		vectors[i] = map[string]float64{"x": float64(i) * 0.1}
	}
	dd.BuildReference(vectors)

	// Current: shifted to 50-70 (drift)
	for i := 0; i < 200; i++ {
		dd.AddSample(map[string]float64{"x": 50.0 + float64(i)*0.1})
	}

	result := dd.CheckDrift()
	if !result.Drifted {
		t.Errorf("drift expected, got PSI=%.4f", result.OverallPSI)
	}
	if len(result.DriftedFeatures) == 0 {
		t.Error("x should be in drifted features")
	}
}

func TestDriftDetectorResetWindow(t *testing.T) {
	dd := NewDriftDetector(DefaultDriftConfig())
	dd.AddSample(map[string]float64{"x": 1.0})
	if len(dd.currentWindow) == 0 {
		t.Error("window should have data")
	}
	dd.ResetWindow()
	if len(dd.currentWindow) != 0 {
		t.Error("window should be empty after reset")
	}
}

func TestDriftDetectorInsufficientSamples(t *testing.T) {
	dd := NewDriftDetector(DefaultDriftConfig())

	// Only 50 samples (MinSamples=100)
	vectors := make([]map[string]float64, 50)
	for i := 0; i < 50; i++ {
		vectors[i] = map[string]float64{"x": float64(i)}
	}
	dd.BuildReference(vectors)

	if len(dd.reference) != 0 {
		t.Error("should not build reference with insufficient samples")
	}
}

func TestPredictionDrift(t *testing.T) {
	pd := NewPredictionDrift(DefaultDriftConfig())

	// Reference: predictions centered at 0.5
	ref := make([]float64, 200)
	for i := 0; i < 200; i++ {
		ref[i] = 0.5 + float64(i%20-10)*0.01
	}
	pd.SetReference(ref)

	if pd.refMean == 0 {
		t.Error("reference mean should be set")
	}
	if pd.refStd == 0 {
		t.Error("reference std should be set")
	}

	// Current: same distribution → no drift
	for i := 0; i < 200; i++ {
		pd.AddPrediction(0.5 + float64(i%20-10)*0.01)
	}
	drifted, zscore := pd.Check()
	if drifted {
		t.Errorf("no drift expected, zscore=%.2f", zscore)
	}

	// Reset and test shifted distribution
	pd.Reset()
	for i := 0; i < 200; i++ {
		pd.AddPrediction(0.8 + float64(i%20-10)*0.01) // shifted mean
	}
	drifted, zscore = pd.Check()
	if !drifted {
		t.Errorf("drift expected, zscore=%.2f", zscore)
	}
}

func TestPredictionDriftInsufficientData(t *testing.T) {
	pd := NewPredictionDrift(DefaultDriftConfig())
	pd.SetReference([]float64{0.5, 0.6, 0.4}) // too few

	pd.AddPrediction(0.5)
	drifted, zscore := pd.Check()
	if drifted {
		t.Error("should not flag drift with insufficient data")
	}
	if zscore != 0 {
		t.Error("zscore should be 0 with insufficient data")
	}
}

func TestMeanStddev(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5}
	m := mean(vals)
	if m != 3 {
		t.Errorf("mean expected 3, got %.2f", m)
	}
	s := stddev(vals)
	if s == 0 {
		t.Error("stddev should not be 0")
	}
}
