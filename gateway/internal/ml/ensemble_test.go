package ml

import (
	"encoding/json"
	"math"
	"testing"
)

func makeTestPredictor(prediction float64) *Predictor {
	model := ExportedModel{
		Success:      true,
		ModelType:    "lightgbm",
		TaskType:     "regression",
		FeatureNames: []string{"f1"},
		Trees: []TreeNode{
			{Leaf: &prediction},
		},
	}
	data, _ := json.Marshal(model)
	p := NewPredictor()
	p.Load(data)
	return p
}

func TestEnsembleVoterWeighted(t *testing.T) {
	ev := NewEnsembleVoter("weighted")
	ev.AddModel("model-a", "lightgbm", 2.0, makeTestPredictor(0.5))
	ev.AddModel("model-b", "xgboost", 1.0, makeTestPredictor(-0.2))

	if ev.ModelCount() != 2 {
		t.Fatalf("expected 2 models, got %d", ev.ModelCount())
	}

	result, err := ev.Predict(map[string]float64{"f1": 1.0})
	if err != nil {
		t.Fatalf("predict failed: %v", err)
	}

	// Weighted: (0.5*2 + (-0.2)*1) / 3 = 0.8/3 = 0.2667
	expected := (0.5*2.0 + (-0.2)*1.0) / 3.0
	if math.Abs(result.FinalPrediction-expected) > 0.001 {
		t.Errorf("expected %.4f, got %.4f", expected, result.FinalPrediction)
	}
	if result.Direction != "up" {
		t.Errorf("expected up, got %s", result.Direction)
	}
	if len(result.ModelPredictions) != 2 {
		t.Errorf("expected 2 predictions, got %d", len(result.ModelPredictions))
	}
}

func TestEnsembleVoterAverage(t *testing.T) {
	ev := NewEnsembleVoter("average")
	ev.AddModel("m1", "lightgbm", 1.0, makeTestPredictor(0.4))
	ev.AddModel("m2", "xgboost", 1.0, makeTestPredictor(0.6))

	result, err := ev.Predict(map[string]float64{"f1": 1.0})
	if err != nil {
		t.Fatalf("predict failed: %v", err)
	}

	expected := 0.5 // (0.4 + 0.6) / 2
	if math.Abs(result.FinalPrediction-expected) > 0.001 {
		t.Errorf("expected %.4f, got %.4f", expected, result.FinalPrediction)
	}
}

func TestEnsembleVoterMedian(t *testing.T) {
	ev := NewEnsembleVoter("median")
	ev.AddModel("m1", "lightgbm", 1.0, makeTestPredictor(-0.5))
	ev.AddModel("m2", "xgboost", 1.0, makeTestPredictor(0.2))
	ev.AddModel("m3", "custom", 1.0, makeTestPredictor(0.8))

	result, err := ev.Predict(map[string]float64{"f1": 1.0})
	if err != nil {
		t.Fatalf("predict failed: %v", err)
	}

	expected := 0.2 // median of [-0.5, 0.2, 0.8]
	if math.Abs(result.FinalPrediction-expected) > 0.001 {
		t.Errorf("expected %.4f, got %.4f", expected, result.FinalPrediction)
	}
}

func TestEnsembleVoterStacking(t *testing.T) {
	ev := NewEnsembleVoter("stacking")
	ev.AddModel("tree1", "lightgbm", 1.0, makeTestPredictor(0.5))
	ev.AddModel("tree2", "xgboost", 1.0, makeTestPredictor(0.6))

	result, err := ev.Predict(map[string]float64{"f1": 1.0})
	if err != nil {
		t.Fatalf("predict failed: %v", err)
	}

	// Trees agree (both positive) → trust tree average = 0.55
	expected := 0.55
	if math.Abs(result.FinalPrediction-expected) > 0.001 {
		t.Errorf("expected %.4f, got %.4f", expected, result.FinalPrediction)
	}
}

func TestEnsembleVoterStackingDisagreement(t *testing.T) {
	ev := NewEnsembleVoter("stacking")
	ev.AddModel("tree1", "lightgbm", 1.0, makeTestPredictor(0.5))
	ev.AddModel("tree2", "xgboost", 1.0, makeTestPredictor(-0.3))
	ev.AddModel("other", "custom", 1.0, makeTestPredictor(0.1))

	result, err := ev.Predict(map[string]float64{"f1": 1.0})
	if err != nil {
		t.Fatalf("predict failed: %v", err)
	}

	// Trees disagree → blend: 0.7*tree_avg + 0.3*other
	// tree_avg = (0.5 + (-0.3))/2 = 0.1
	// other = 0.1
	// result = 0.7*0.1 + 0.3*0.1 = 0.1
	expected := 0.7*0.1 + 0.3*0.1
	if math.Abs(result.FinalPrediction-expected) > 0.001 {
		t.Errorf("expected %.4f, got %.4f", expected, result.FinalPrediction)
	}
}

func TestEnsembleVoterNoModels(t *testing.T) {
	ev := NewEnsembleVoter("weighted")
	_, err := ev.Predict(map[string]float64{"f1": 1.0})
	if err == nil {
		t.Error("expected error with no models")
	}
}

func TestEnsembleVoterRemoveModel(t *testing.T) {
	ev := NewEnsembleVoter("weighted")
	ev.AddModel("m1", "lightgbm", 1.0, makeTestPredictor(0.5))
	ev.AddModel("m2", "xgboost", 1.0, makeTestPredictor(-0.2))

	if ev.ModelCount() != 2 {
		t.Fatalf("expected 2 models")
	}

	ev.RemoveModel("m1")
	if ev.ModelCount() != 1 {
		t.Errorf("expected 1 model after removal, got %d", ev.ModelCount())
	}

	result, err := ev.Predict(map[string]float64{"f1": 1.0})
	if err != nil {
		t.Fatalf("predict failed: %v", err)
	}
	if math.Abs(result.FinalPrediction-(-0.2)) > 0.001 {
		t.Errorf("expected -0.2, got %.4f", result.FinalPrediction)
	}
}

func TestEnsembleVoterAgreement(t *testing.T) {
	ev := NewEnsembleVoter("weighted")
	ev.AddModel("m1", "lightgbm", 1.0, makeTestPredictor(0.5))
	ev.AddModel("m2", "xgboost", 1.0, makeTestPredictor(0.3))
	ev.AddModel("m3", "custom", 1.0, makeTestPredictor(-0.1))

	result, err := ev.Predict(map[string]float64{"f1": 1.0})
	if err != nil {
		t.Fatalf("predict failed: %v", err)
	}

	// 2 out of 3 agree with "up" direction
	expectedAgreement := 2.0 / 3.0 * 100.0
	if math.Abs(result.Agreement-expectedAgreement) > 0.1 {
		t.Errorf("expected agreement %.1f%%, got %.1f%%", expectedAgreement, result.Agreement)
	}
}

func TestEnsembleManager(t *testing.T) {
	em := NewEnsembleManager()

	ev1 := em.GetOrCreate("BTCUSDT_1h", "weighted")
	if ev1 == nil {
		t.Fatal("GetOrCreate should return voter")
	}

	ev2 := em.GetOrCreate("BTCUSDT_1h", "weighted")
	if ev1 != ev2 {
		t.Error("should return same instance for same key")
	}

	ev3 := em.GetOrCreate("ETHUSDT_1h", "average")
	if ev3 == ev1 {
		t.Error("different key should return different instance")
	}

	keys := em.List()
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}

	if em.Get("BTCUSDT_1h") == nil {
		t.Error("BTCUSDT_1h should exist")
	}

	em.Remove("BTCUSDT_1h")
	if em.Get("BTCUSDT_1h") != nil {
		t.Error("BTCUSDT_1h should be removed")
	}
}

func TestEnsembleResultNeutral(t *testing.T) {
	ev := NewEnsembleVoter("average")
	ev.AddModel("m1", "lightgbm", 1.0, makeTestPredictor(0.0))

	result, err := ev.Predict(map[string]float64{"f1": 1.0})
	if err != nil {
		t.Fatalf("predict failed: %v", err)
	}
	if result.Direction != "neutral" {
		t.Errorf("expected neutral, got %s", result.Direction)
	}
	if result.Strength != 0 {
		t.Errorf("expected strength 0, got %.2f", result.Strength)
	}
}
