package ml_test

import (
	"encoding/json"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/ml"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func ptAssert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond { t.Fatal(msg) }
}

func ptAssertFloat(t *testing.T, got, want float64, msg string) {
	t.Helper()
	if got < want-0.001 || got > want+0.001 {
		t.Fatalf("%s: got %.6f, want %.6f", msg, got, want)
	}
}

// makeSimpleModel creates a minimal 2-tree ensemble for testing.
func makeSimpleModelJSON() []byte {
	model := ml.ExportedModel{
		Success:      true,
		ModelType:    "lightgbm",
		TaskType:     "regression",
		FeatureNames: []string{"feature_a", "feature_b"},
		Trees: []ml.TreeNode{
			// Tree 1: if feature_a <= 0.5 → 0.1 else 0.3
			{Feature: "feature_a", Threshold: 0.5,
				Left:  &ml.TreeNode{Leaf: ptr(0.1)},
				Right: &ml.TreeNode{Leaf: ptr(0.3)},
			},
			// Tree 2: if feature_b <= -0.2 → -0.05 else 0.15
			{Feature: "feature_b", Threshold: -0.2,
				Left:  &ml.TreeNode{Leaf: ptr(-0.05)},
				Right: &ml.TreeNode{Leaf: ptr(0.15)},
			},
		},
	}
	data, _ := json.Marshal(model)
	return data
}

func ptr(v float64) *float64 { return &v }

/* ── Predictor Tests ─────────────────────────────────────────── */

func TestPredictorLoad(t *testing.T) {
	p := ml.NewPredictor()
	err := p.Load(makeSimpleModelJSON())
	ptAssert(t, err == nil, "load should succeed")
	ptAssert(t, p.IsLoaded(), "should be loaded")
}

func TestPredictorPredict(t *testing.T) {
	p := ml.NewPredictor()
	p.Load(makeSimpleModelJSON())

	// feature_a=0 (<=0.5), feature_b=0 (>= -0.2)
	// Tree1: 0 -> left=0.1, Tree2: 0 -> right=0.15, total=0.25
	pred, err := p.Predict([]float64{0.0, 0.0})
	ptAssert(t, err == nil, "predict should succeed")
	ptAssertFloat(t, pred, 0.25, "0.1 + 0.15 = 0.25")
}

func TestPredictorPredictRightPath(t *testing.T) {
	p := ml.NewPredictor()
	p.Load(makeSimpleModelJSON())

	// feature_a=1 (>0.5), feature_b=0 (>= -0.2)
	// Tree1: 1 -> right=0.3, Tree2: 0 -> right=0.15, total=0.45
	pred, _ := p.Predict([]float64{1.0, 0.0})
	ptAssertFloat(t, pred, 0.45, "0.3 + 0.15 = 0.45")
}

func TestPredictorPredictMixed(t *testing.T) {
	p := ml.NewPredictor()
	p.Load(makeSimpleModelJSON())

	// feature_a=0 (<=0.5), feature_b=-1 (<-0.2)
	// Tree1: 0 -> left=0.1, Tree2: -1 -> left=-0.05, total=0.05
	pred, _ := p.Predict([]float64{0.0, -1.0})
	ptAssertFloat(t, pred, 0.05, "0.1 + (-0.05) = 0.05")
}

func TestPredictorPredictFromMap(t *testing.T) {
	p := ml.NewPredictor()
	p.Load(makeSimpleModelJSON())

	pred, err := p.PredictFromMap(map[string]float64{
		"feature_a": 0.0,
		"feature_b": 0.0,
	})
	ptAssert(t, err == nil, "predictFromMap should succeed")
	ptAssertFloat(t, pred, 0.25, "same as array version")
}

func TestPredictorNotLoaded(t *testing.T) {
	p := ml.NewPredictor()
	_, err := p.Predict([]float64{0})
	ptAssert(t, err != nil, "not loaded should error")
}

func TestPredictorWrongFeatureCount(t *testing.T) {
	p := ml.NewPredictor()
	p.Load(makeSimpleModelJSON())
	_, err := p.Predict([]float64{0}) // only 1, need 2
	ptAssert(t, err != nil, "wrong count should error")
}

func TestPredictorEmptyModel(t *testing.T) {
	p := ml.NewPredictor()
	err := p.Load([]byte(`{"trees":[]}`))
	ptAssert(t, err != nil, "empty trees should error")
}

/* ── Feature Calculator Tests ────────────────────────────────── */

func TestFeatureCalculator(t *testing.T) {
	fc := ml.NewFeatureCalculator([]int{5, 10})

	// 20 bars of linearly increasing prices
	bars := make([]ml.OHLCV, 20)
	for i := 0; i < 20; i++ {
		price := 100.0 + float64(i)*0.5
		bars[i] = ml.OHLCV{Open: price - 0.1, High: price + 1, Low: price - 1, Close: price, Volume: 100 + float64(i)*10}
	}

	features := fc.Compute(bars)
	ptAssert(t, len(features) > 0, "features computed")
	// Should have return_5, return_10, etc.
	_, ok := features["return_5"]
	ptAssert(t, ok, "return_5 exists")
	_, ok = features["price_vs_ma_5"]
	ptAssert(t, ok, "price_vs_ma exists")
	_, ok = features["bb_position_5"]
	ptAssert(t, ok, "bb_position exists")
	_, ok = features["macd"]
	ptAssert(t, ok, "macd exists")
}

func TestFeatureCalculatorInsufficientData(t *testing.T) {
	fc := ml.NewFeatureCalculator([]int{5, 10})
	bars := []ml.OHLCV{{Close: 100}} // only 1 bar
	features := fc.Compute(bars)
	ptAssert(t, features == nil, "nil for insufficient data")
}

/* ── Integration: Predictor + FeatureCalculator ──────────────── */

func TestPredictorWithFeatureCalc(t *testing.T) {
	p := ml.NewPredictor()
	p.Load(makeSimpleModelJSON())

	// The model expects feature_a and feature_b
	// We'll manually create a feature map as if FeatureCalculator computed it
	features := map[string]float64{
		"feature_a": 0.2, // <= 0.5 → left path = 0.1
		"feature_b": 0.5, // >= -0.2 → right path = 0.15
	}
	pred, err := p.PredictFromMap(features)
	ptAssert(t, err == nil, "predict should succeed")
	ptAssertFloat(t, pred, 0.25, "0.1 + 0.15")
}
