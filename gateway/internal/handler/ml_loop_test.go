package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/ml"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// TestMLLoopAPIs 闭环观测 API：训练历史 + 状态汇总。
func TestMLLoopAPIs(t *testing.T) {
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	// 造训练档案 + 重训任务 + 已加载模型
	runs := store.NewMLTrainingRunRepo()
	if _, err := runs.Record(&store.MLTrainingRun{
		JobID: 0, ModelName: "LOOP_M1", TriggerSrc: "manual", Trainer: "python",
		Status: "success", Symbol: "BTCUSDT", Interval: "1h",
		TrainSamples: 500, TestSamples: 100, FeatureCount: 42,
		MetricsJSON: `{"test_rmse":0.01}`, ModelVersion: "v123", DurationMs: 99,
	}); err != nil {
		t.Fatalf("record run: %v", err)
	}
	jobs := store.NewMLRetrainRepo()
	if _, err := jobs.CreateJob(1, "LOOP_M1", `{"symbol":"BTCUSDT"}`, 1440); err != nil {
		t.Fatalf("create job: %v", err)
	}

	registry := ml.NewModelRegistry("")
	leaf := 0.001
	if _, err := registry.HotLoad("LOOP_M1", &ml.ExportedModel{
		Success: true, ModelID: "LOOP_M1", ModelType: "lightgbm", TaskType: "regression",
		FeatureNames: []string{"return_5"},
		Trees:        []ml.TreeNode{{Feature: "return_5", Threshold: 0, Left: &ml.TreeNode{Leaf: &leaf}, Right: &ml.TreeNode{Leaf: &leaf}}},
	}); err != nil {
		t.Fatalf("hotload: %v", err)
	}
	SetMLLoopDeps(nil, nil, registry, nil, runs)
	t.Cleanup(func() { SetMLLoopDeps(nil, nil, nil, nil, nil) })

	r := setupRouter()
	r.GET("/ml/training-runs", MLTrainingRuns)
	r.GET("/ml/loop-status", MLLoopStatus)

	// ── 训练历史 ──
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ml/training-runs", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "training-runs status")
	var runsResp struct {
		Runs []store.MLTrainingRun `json:"runs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &runsResp); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(runsResp.Runs) != 1 || runsResp.Runs[0].ModelName != "LOOP_M1" || runsResp.Runs[0].TrainSamples != 500 {
		t.Fatalf("bad runs payload: %+v", runsResp.Runs)
	}

	// model 过滤
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/ml/training-runs?model=NOPE", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "training-runs filter status")
	var empty struct {
		Runs []store.MLTrainingRun `json:"runs"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &empty)
	if len(empty.Runs) != 0 {
		t.Fatalf("filter should be empty: %+v", empty.Runs)
	}

	// ── 闭环状态汇总 ──
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/ml/loop-status", nil)
	r.ServeHTTP(w, req)
	assertEq(t, w.Code, http.StatusOK, "loop-status status")
	var status map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	server, ok := status["ml_server"].(map[string]any)
	if !ok {
		t.Fatalf("missing ml_server: %v", status)
	}
	if server["managed"] != false { // 测试未注入管理器
		t.Fatalf("managed should be false: %v", server)
	}
	jobList, ok := status["jobs"].([]any)
	if !ok || len(jobList) == 0 {
		t.Fatalf("missing jobs: %v", status)
	}
	firstJob := jobList[0].(map[string]any)
	if firstJob["model_name"] != "LOOP_M1" {
		t.Fatalf("bad job entry: %v", firstJob)
	}
	if _, hasNext := firstJob["next_run_at"]; !hasNext {
		t.Fatalf("job missing next_run_at: %v", firstJob)
	}
	models, ok := status["models_loaded"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("missing models_loaded: %v", status)
	}
	if models[0].(map[string]any)["model_id"] != "LOOP_M1" {
		t.Fatalf("bad loaded model: %v", models[0])
	}
	lastRun, ok := status["last_run"].(map[string]any)
	if !ok || lastRun["model_name"] != "LOOP_M1" {
		t.Fatalf("bad last_run: %v", status["last_run"])
	}
}
