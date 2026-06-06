package ml_test

import (
	"encoding/json"
	"math"
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

func TestFeatureCalculatorExtended(t *testing.T) {
	fc := ml.NewFeatureCalculator([]int{5, 10, 20})

	// 50 bars with realistic OHLCV patterns
	bars := make([]ml.OHLCV, 50)
	for i := 0; i < 50; i++ {
		price := 100.0 + float64(i)*0.3 + math.Sin(float64(i)*0.5)*2.0
		bars[i] = ml.OHLCV{
			Open:   price - 0.2,
			High:   price + 1.5,
			Low:    price - 1.5,
			Close:  price,
			Volume: 1000 + float64(i%10)*100,
		}
	}

	features := fc.Compute(bars)
	ptAssert(t, len(features) >= 40, "should have 40+ features")

	// RSI
	_, ok := features["rsi_5"]
	ptAssert(t, ok, "rsi_5 exists")
	_, ok = features["rsi_10"]
	ptAssert(t, ok, "rsi_10 exists")

	// ATR
	_, ok = features["atr_5"]
	ptAssert(t, ok, "atr_5 exists")

	// Williams %R
	_, ok = features["williams_r_5"]
	ptAssert(t, ok, "williams_r_5 exists")

	// Stochastic
	_, ok = features["stoch_k_5"]
	ptAssert(t, ok, "stoch_k_5 exists")
	_, ok = features["stoch_d_5"]
	ptAssert(t, ok, "stoch_d_5 exists")

	// CCI
	_, ok = features["cci_5"]
	ptAssert(t, ok, "cci_5 exists")

	// ADX
	_, ok = features["adx_5"]
	ptAssert(t, ok, "adx_5 exists")

	// OBV
	_, ok = features["obv"]
	ptAssert(t, ok, "obv exists")

	// CMF
	_, ok = features["cmf_5"]
	ptAssert(t, ok, "cmf_5 exists")

	// Price position
	_, ok = features["price_position_5"]
	ptAssert(t, ok, "price_position_5 exists")

	// Candlestick
	_, ok = features["body_pct"]
	ptAssert(t, ok, "body_pct exists")
	_, ok = features["upper_shadow_pct"]
	ptAssert(t, ok, "upper_shadow_pct exists")

	// Linear regression
	_, ok = features["linreg_slope_5"]
	ptAssert(t, ok, "linreg_slope_5 exists")
	_, ok = features["linreg_r2_5"]
	ptAssert(t, ok, "linreg_r2_5 exists")

	// Skewness & Kurtosis
	_, ok = features["skew_5"]
	ptAssert(t, ok, "skew_5 exists")
	_, ok = features["kurt_5"]
	ptAssert(t, ok, "kurt_5 exists")

	// Distance from high/low
	_, ok = features["dist_from_high_5"]
	ptAssert(t, ok, "dist_from_high_5 exists")
	_, ok = features["dist_from_low_5"]
	ptAssert(t, ok, "dist_from_low_5 exists")

	// VWAP deviation
	_, ok = features["vwap_dev_5"]
	ptAssert(t, ok, "vwap_dev_5 exists")

	// Momentum & ROC
	_, ok = features["momentum_5"]
	ptAssert(t, ok, "momentum_5 exists")
	_, ok = features["roc_5"]
	ptAssert(t, ok, "roc_5 exists")

	// Validate RSI range [0, 100]
	if rsi, ok := features["rsi_5"]; ok {
		ptAssert(t, rsi >= 0 && rsi <= 100, "rsi in [0,100]")
	}

	// Validate Williams %R range [-100, 0]
	if wr, ok := features["williams_r_5"]; ok {
		ptAssert(t, wr >= -100 && wr <= 0, "williams_r in [-100,0]")
	}

	// Validate Stochastic range [0, 100]
	if k, ok := features["stoch_k_5"]; ok {
		ptAssert(t, k >= 0 && k <= 100, "stoch_k in [0,100]")
	}

	// Validate ADX range [0, 100]
	if adx, ok := features["adx_5"]; ok {
		ptAssert(t, adx >= 0 && adx <= 100, "adx in [0,100]")
	}

	// Validate price_position range [0, 1]
	if pp, ok := features["price_position_5"]; ok {
		ptAssert(t, pp >= 0 && pp <= 1, "price_position in [0,1]")
	}

	// Validate body_pct is positive
	if bp, ok := features["body_pct"]; ok {
		ptAssert(t, bp >= 0, "body_pct >= 0")
	}
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
