package ml

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/data"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func init() {
	store.InitDB()
}

func TestDefaultPipelineConfig(t *testing.T) {
	cfg := DefaultPipelineConfig()
	if cfg.Symbol != "BTCUSDT" {
		t.Errorf("expected BTCUSDT, got %s", cfg.Symbol)
	}
	if cfg.Interval != "1h" {
		t.Errorf("expected 1h, got %s", cfg.Interval)
	}
	if cfg.LookbackDays != 90 {
		t.Errorf("expected 90, got %d", cfg.LookbackDays)
	}
	if len(cfg.FeaturePeriods) != 4 {
		t.Errorf("expected 4 periods, got %d", len(cfg.FeaturePeriods))
	}
	if cfg.LabelHorizon != 5 {
		t.Errorf("expected 5, got %d", cfg.LabelHorizon)
	}
}

func TestTrainingPipeline_loadBars(t *testing.T) {
	client := NewClient("")
	store := data.NewStorage()
	downloader := data.NewDownloader(store)
	pipeline := NewTrainingPipeline(client, downloader)

	// Create test data
	bars := []data.OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: time.Now().Add(-24 * time.Hour).UnixMilli(), Open: 100, High: 101, Low: 99, Close: 100, Volume: 1000},
		{Symbol: "BTCUSDT", Interval: "1h", Time: time.Now().Add(-23 * time.Hour).UnixMilli(), Open: 100, High: 101, Low: 99, Close: 101, Volume: 1000},
	}
	store.SaveOHLCV(bars)

	cfg := PipelineConfig{
		Symbol:       "BTCUSDT",
		Interval:     "1h",
		LookbackDays: 2,
	}

	loaded, err := pipeline.loadBars(cfg)
	if err != nil {
		t.Fatalf("loadBars failed: %v", err)
	}
	if len(loaded) < 2 {
		t.Errorf("expected at least 2 bars, got %d", len(loaded))
	}
}

func TestTrainingPipeline_generateFeaturesAndLabels(t *testing.T) {
	client := NewClient("")
	pipeline := NewTrainingPipeline(client, nil)

	// Generate 200 bars of synthetic data
	bars := make([]data.OHLCV, 200)
	for i := range bars {
		bars[i] = data.OHLCV{
			Open:   100 + float64(i)*0.1,
			High:   101 + float64(i)*0.1,
			Low:    99 + float64(i)*0.1,
			Close:  100 + float64(i)*0.1,
			Volume: 1000,
		}
	}

	periods := []int{5, 10, 20}
	horizon := 5

	featureBars, labels := pipeline.generateFeaturesAndLabels(bars, periods, horizon)

	if len(featureBars) == 0 {
		t.Fatal("expected feature bars, got none")
	}
	if len(labels) == 0 {
		t.Fatal("expected labels, got none")
	}
	if len(featureBars) != len(labels) {
		t.Errorf("featureBars and labels length mismatch: %d vs %d", len(featureBars), len(labels))
	}

	// Check that features contain expected keys
	expectedKeys := []string{"return_5", "price_vs_ma_5", "hl_range", "macd", "zscore_5"}
	for _, key := range expectedKeys {
		if _, ok := featureBars[0][key]; !ok {
			t.Errorf("expected feature %s not found", key)
		}
	}

	// Check label is present
	if _, ok := featureBars[0]["label"]; !ok {
		t.Error("expected label not found in feature bar")
	}
}

func TestTrainingPipeline_getFeatureNames(t *testing.T) {
	client := NewClient("")
	pipeline := NewTrainingPipeline(client, nil)

	names := pipeline.getFeatureNames([]int{5, 10, 20})
	if len(names) == 0 {
		t.Fatal("expected feature names, got none")
	}

	// Should contain return, price_vs_ma, zscore features
	hasReturn := false
	for _, n := range names {
		if n == "return_5" || n == "return_10" || n == "return_20" {
			hasReturn = true
			break
		}
	}
	if !hasReturn {
		t.Error("expected return features in names")
	}
}

func TestComputeMetrics(t *testing.T) {
	pred := []float64{0.1, 0.2, -0.1, 0.05, -0.05}
	actual := []float64{0.12, 0.18, -0.08, 0.03, -0.02}

	result := computeMetrics(pred, actual)
	if result == nil {
		t.Fatal("expected metrics, got nil")
	}

	if result.Samples != 5 {
		t.Errorf("expected 5 samples, got %d", result.Samples)
	}
	if result.MSE <= 0 {
		t.Error("expected positive MSE")
	}
	if result.RMSE <= 0 {
		t.Error("expected positive RMSE")
	}
	if result.MAE <= 0 {
		t.Error("expected positive MAE")
	}
	if result.Directional < 0 || result.Directional > 100 {
		t.Errorf("directional accuracy out of range: %f", result.Directional)
	}
}

func TestSharpeRatio(t *testing.T) {
	returns := []float64{0.01, 0.02, -0.01, 0.015, -0.005, 0.01}
	sharpe := sharpeRatio(returns)
	if sharpe <= 0 {
		t.Errorf("expected positive sharpe, got %f", sharpe)
	}

	// Empty returns
	sharpe = sharpeRatio([]float64{})
	if sharpe != 0 {
		t.Errorf("expected 0 for empty returns, got %f", sharpe)
	}
}

func TestMaxInt(t *testing.T) {
	if maxInt([]int{3, 1, 4, 1, 5}) != 5 {
		t.Error("expected 5")
	}
	if maxInt([]int{}) != 0 {
		t.Error("expected 0 for empty slice")
	}
}
