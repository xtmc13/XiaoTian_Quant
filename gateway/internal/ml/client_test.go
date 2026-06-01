package ml_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/ml"
)

/* ── Helpers ─────────────────────────────────────────────────── */

func mtAssert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func mtAssertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

// fakeMLServer creates an httptest server that mimics the ML Python service.
func fakeMLServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/health":
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

		case r.URL.Path == "/models" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"model_id": "test_model_1", "model_type": "lightgbm", "task_type": "regression",
						"trained_at": "2024-01-01T00:00:00", "metrics": map[string]any{"test_rmse": 0.023}},
				},
			})

		case r.URL.Path == "/train" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]any{
				"success":       true,
				"model_id":      "test_model_1",
				"model_type":    "lightgbm",
				"metrics":       map[string]any{"test_rmse": 0.023, "train_rmse": 0.018},
				"feature_count": 48,
				"train_samples": 400,
				"test_samples":  100,
			})

		case r.URL.Path == "/predict" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]any{
				"success":    true,
				"model_id":   "test_model_1",
				"prediction": 0.025,
				"direction":  "LONG",
				"strength":   0.85,
			})

		case r.URL.Path == "/models/test_model_1" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{
				"model_id":   "test_model_1",
				"model_type": "lightgbm",
				"task_type":  "regression",
				"trained_at": "2024-01-01T00:00:00",
				"metrics":    map[string]any{"test_rmse": 0.023},
				"feature_count": 48,
			})

		case r.URL.Path == "/models/test_model_1/importance" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"importance": []map[string]any{
					{"name": "return_5", "importance": 0.15},
					{"name": "bb_position_20", "importance": 0.12},
				},
			})

		case r.URL.Path == "/models/test_model_1" && r.Method == "DELETE":
			json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

		default:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		}
	}))
}

/* ── Client Tests ────────────────────────────────────────────── */

func TestMLClientHealth(t *testing.T) {
	srv := fakeMLServer(t)
	defer srv.Close()

	c := ml.NewClient(srv.URL)
	err := c.Health()
	mtAssert(t, err == nil, "health should succeed")
}

func TestMLClientTrain(t *testing.T) {
	srv := fakeMLServer(t)
	defer srv.Close()

	c := ml.NewClient(srv.URL)
	result, err := c.Train(ml.TrainConfig{
		ModelID:   "test_model_1",
		ModelType: "lightgbm",
		TaskType:  "regression",
		Symbol:    "BTCUSDT",
		Bars:      []map[string]any{{"close": 50000.0}},
	})
	mtAssert(t, err == nil, "train should succeed")
	mtAssert(t, result.Success, "train successful")
	mtAssertEq(t, result.ModelID, "test_model_1", "model id")
	mtAssertEq(t, result.FeatureCount, 48, "feature count")
}

func TestMLClientPredict(t *testing.T) {
	srv := fakeMLServer(t)
	defer srv.Close()

	c := ml.NewClient(srv.URL)
	result, err := c.Predict(ml.PredictInput{
		ModelID: "test_model_1",
		Bars:    []map[string]any{{"close": 50000.0}},
	})
	mtAssert(t, err == nil, "predict should succeed")
	mtAssert(t, result.Success, "predict successful")
	mtAssertEq(t, result.Direction, "LONG", "direction LONG")
	mtAssertEq(t, result.Prediction > 0, true, "positive prediction")
}

func TestMLClientListModels(t *testing.T) {
	srv := fakeMLServer(t)
	defer srv.Close()

	c := ml.NewClient(srv.URL)
	models, err := c.ListModels()
	mtAssert(t, err == nil, "list should succeed")
	mtAssertEq(t, len(models), 1, "1 model")
	mtAssertEq(t, models[0].ModelID, "test_model_1", "model id")
}

func TestMLClientGetModel(t *testing.T) {
	srv := fakeMLServer(t)
	defer srv.Close()

	c := ml.NewClient(srv.URL)
	model, err := c.GetModel("test_model_1")
	mtAssert(t, err == nil, "get should succeed")
	mtAssertEq(t, model.ModelType, "lightgbm", "model type")
}

func TestMLClientFeatureImportance(t *testing.T) {
	srv := fakeMLServer(t)
	defer srv.Close()

	c := ml.NewClient(srv.URL)
	items, err := c.FeatureImportance("test_model_1")
	mtAssert(t, err == nil, "importance should succeed")
	mtAssertEq(t, len(items), 2, "2 importance items")
	mtAssertEq(t, items[0].Name, "return_5", "top feature")
}

func TestMLClientDeleteModel(t *testing.T) {
	srv := fakeMLServer(t)
	defer srv.Close()

	c := ml.NewClient(srv.URL)
	err := c.DeleteModel("test_model_1")
	mtAssert(t, err == nil, "delete should succeed")
}

/* ── End-to-end: Train → Predict flow ────────────────────────── */

func TestE2ETrainPredictFlow(t *testing.T) {
	srv := fakeMLServer(t)
	defer srv.Close()

	c := ml.NewClient(srv.URL)

	// 1. Health check
	mtAssert(t, c.Health() == nil, "health")

	// 2. Train
	bars := []map[string]any{
		{"close": 50000, "open": 49900, "high": 50100, "low": 49800, "volume": 100},
		{"close": 50100, "open": 50000, "high": 50200, "low": 49900, "volume": 120},
	}
	trainRes, err := c.Train(ml.TrainConfig{
		ModelID: "e2e_test", ModelType: "lightgbm", TaskType: "regression",
		Symbol: "BTCUSDT", Interval: "1h", Bars: bars,
		LabelConfig: map[string]any{"label_type": "regression", "horizon": 5},
	})
	mtAssert(t, err == nil, "train step")
	mtAssert(t, trainRes.Success, "train success")

	// 3. List models — verify our model appears
	models, _ := c.ListModels()
	mtAssert(t, len(models) >= 1, "models available")

	// 4. Predict
	predRes, err := c.Predict(ml.PredictInput{
		ModelID: "test_model_1",
		Bars:    bars,
	})
	mtAssert(t, err == nil, "predict step")
	mtAssertEq(t, predRes.Direction, "LONG", "LONG signal")

	// 5. Feature importance
	imp, _ := c.FeatureImportance("test_model_1")
	mtAssert(t, len(imp) > 0, "importance available")

	// 6. Delete
	mtAssert(t, c.DeleteModel("test_model_1") == nil, "cleanup")
}

/* ── Error handling tests ────────────────────────────────────── */

func TestMLClientUnreachable(t *testing.T) {
	c := ml.NewClient("http://127.0.0.1:19999") // nothing listening
	err := c.Health()
	mtAssert(t, err != nil, "unreachable should error")
}

func TestMLClientInvalidURL(t *testing.T) {
	c := ml.NewClient("not-a-valid-url")
	_, err := c.ListModels()
	mtAssert(t, err != nil, "invalid URL should error")
}
