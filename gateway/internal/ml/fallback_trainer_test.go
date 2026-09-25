package ml_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/ml"
)

// TestGoFallbackTrainerFitsSignal 强特征数据上训练：拟合优于常数基线，
// 导出模型可被 Go predictor 原生加载，预测方向与特征符号一致。
func TestGoFallbackTrainerFitsSignal(t *testing.T) {
	trainer := ml.NewGoFallbackTrainer()

	n := 400
	X := make([][]float64, n)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		x1 := math.Sin(float64(i)/7) + 0.05*math.Sin(float64(i)*3.1)
		x2 := math.Cos(float64(i)/11) // 无关特征
		X[i] = []float64{x1, x2}
		y[i] = 0.01*x1 + 0.0005*math.Sin(float64(i)*1.7) // 标签 ≈ 2% * x1
	}

	exported, metrics, err := trainer.Train("fb_model", []string{"trend", "noise"}, X, y)
	if err != nil {
		t.Fatalf("train: %v", err)
	}
	if len(exported.Trees) < 2 {
		t.Fatalf("too few trees: %d", len(exported.Trees))
	}
	rmse, _ := metrics["test_rmse"].(float64)
	if rmse <= 0 || rmse > 0.006 {
		t.Fatalf("test_rmse out of expected range: %v (metrics=%v)", rmse, metrics)
	}

	// Go predictor 原生加载 + 预测方向正确
	p := ml.NewPredictor()
	data, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := p.Load(data); err != nil {
		t.Fatalf("predictor load: %v", err)
	}
	up, err := p.PredictFromMap(map[string]float64{"trend": 1.0, "noise": 0})
	if err != nil {
		t.Fatalf("predict up: %v", err)
	}
	down, err := p.PredictFromMap(map[string]float64{"trend": -1.0, "noise": 0})
	if err != nil {
		t.Fatalf("predict down: %v", err)
	}
	if !(up > down) {
		t.Fatalf("direction wrong: up=%v down=%v", up, down)
	}
	if up <= 0 {
		t.Fatalf("expected positive prediction for trend=1: %v", up)
	}
}

// TestGoFallbackTrainerBadInput 非法输入如实报错（不 panic）。
func TestGoFallbackTrainerBadInput(t *testing.T) {
	trainer := ml.NewGoFallbackTrainer()
	if _, _, err := trainer.Train("m", []string{"a"}, nil, nil); err == nil {
		t.Fatalf("expected error for empty dataset")
	}
	if _, _, err := trainer.Train("m", []string{"a"}, [][]float64{{1}}, []float64{1, 2}); err == nil {
		t.Fatalf("expected error for length mismatch")
	}
	if _, _, err := trainer.Train("m", nil, [][]float64{{1}}, []float64{1}); err == nil {
		t.Fatalf("expected error for no features")
	}
}
