package ml_test

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ml"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 训练闭环端到端集成测试（fake ml_server + 合成 K 线，不依赖真实 Python）──

// fakeBarLoader 合成 K 线数据源（实现 ml.BarLoader）。
type fakeBarLoader struct {
	bars []model.Bar
}

func (f *fakeBarLoader) LoadBarsForBacktest(symbol, interval string, fromMs, toMs int64) []model.Bar {
	var out []model.Bar
	for _, b := range f.bars {
		if b.Symbol == symbol && b.Interval == interval && b.Time >= fromMs && b.Time <= toMs {
			out = append(out, b)
		}
	}
	return out
}

// syntheticLoopBars 以当前时间为终点生成 n 根小时 K 线（确定性正弦 + 微噪声）。
// volScale 用于制造漂移场景（放大近期波动）。
func syntheticLoopBars(symbol, interval string, n int, volScale float64) []model.Bar {
	end := time.Now().Truncate(time.Hour)
	bars := make([]model.Bar, n)
	price := 100.0
	for i := 0; i < n; i++ {
		ret := volScale * (0.003*math.Sin(float64(i)/6) + 0.001*math.Sin(float64(i)*2.7))
		prev := price
		price *= 1 + ret
		bars[i] = model.Bar{
			Symbol: symbol, Interval: interval,
			Time:   end.Add(-time.Duration(n-i) * time.Hour).UnixMilli(),
			Open:   prev,
			High:   math.Max(prev, price) * 1.001,
			Low:    math.Min(prev, price) * 0.999,
			Close:  price,
			Volume: 1000 + float64(i%50),
		}
	}
	return bars
}

// fakeMLServer 模拟 ml_server 的 /health /train /models/:id/export。
// version 控制导出模型叶值——验证热加载后预测输出变化。
type loopFakeMLServer struct {
	mu         sync.Mutex
	version    int
	trainCalls int
	srv        *httptest.Server
}

func newLoopFakeMLServer() *loopFakeMLServer {
	f := &loopFakeMLServer{version: 1}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	mux.HandleFunc("/train", func(w http.ResponseWriter, r *http.Request) {
		var cfg ml.TrainConfig
		_ = json.NewDecoder(r.Body).Decode(&cfg)
		f.mu.Lock()
		f.trainCalls++
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(ml.TrainResult{
			Success:      true,
			ModelID:      cfg.ModelID,
			ModelType:    "lightgbm",
			Metrics:      map[string]any{"test_rmse": 0.012, "test_r2": 0.55, "test_directional_accuracy": 58.3},
			FeatureCount: 3,
			TrainSamples: 500,
			TestSamples:  120,
		})
	})
	mux.HandleFunc("/models/", func(w http.ResponseWriter, r *http.Request) {
		// 仅处理 /models/{id}/export
		path := strings.TrimPrefix(r.URL.Path, "/models/")
		parts := strings.Split(path, "/")
		if len(parts) != 2 || parts[1] != "export" {
			http.Error(w, "not found", 404)
			return
		}
		f.mu.Lock()
		leaf := 0.001 * float64(f.version)
		f.mu.Unlock()
		neg := -leaf
		_ = json.NewEncoder(w).Encode(ml.ExportedModel{
			Success:      true,
			ModelID:      parts[0],
			ModelType:    "lightgbm",
			TaskType:     "regression",
			FeatureNames: []string{"return_5"},
			Trees: []ml.TreeNode{{
				Feature:   "return_5",
				Threshold: 0,
				Left:      &ml.TreeNode{Leaf: &neg},
				Right:     &ml.TreeNode{Leaf: &leaf},
			}},
		})
	})
	f.srv = httptest.NewServer(mux)
	return f
}

func (f *loopFakeMLServer) close()       { f.srv.Close() }
func (f *loopFakeMLServer) url() string  { return f.srv.URL }
func (f *loopFakeMLServer) bumpVersion() { f.mu.Lock(); f.version++; f.mu.Unlock() }
func (f *loopFakeMLServer) calls() int   { f.mu.Lock(); defer f.mu.Unlock(); return f.trainCalls }

// newLoopJob 建一个闭环重训任务（合成数据规格）。
func newLoopJob(t *testing.T, repo *store.MLRetrainRepo, modelName, symbol string) int64 {
	t.Helper()
	featureSet := `{"symbol":"` + symbol + `","interval":"1h","lookback_days":30,"feature_periods":[5,10,20,50],"label_horizon":5}`
	id, err := repo.CreateJob(1, modelName, featureSet, 1440)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	return id
}

// TestTrainingLoopEndToEnd 闭环主链路：
// 触发重训 → fake ml_server 训练 → 导出 JSON → 模型文件出现 → predictor 热加载
// → 训练档案落库（指标/样本/特征/版本）→ 再训一版 → 预测输出变化（热加载生效）。
func TestTrainingLoopEndToEnd(t *testing.T) {
	repo := setupMLTestDB(t)
	trainingRepo := store.NewMLTrainingRunRepo()

	fake := newLoopFakeMLServer()
	defer fake.close()
	client := ml.NewClient(fake.url())

	symbol := "LOOPUSDT"
	modelName := "LOOPUSDT_1h_loop"
	loader := &fakeBarLoader{bars: syntheticLoopBars(symbol, "1h", 720, 1.0)}
	regDir := t.TempDir()
	registry := ml.NewModelRegistry(regDir)

	loop := ml.NewTrainingLoop(ml.NewTrainingPipeline(client, loader), client, registry, trainingRepo)
	retrainer := ml.NewRetrainer(repo, loop, &fakeNotifier{}, nil)

	jobID := newLoopJob(t, repo, modelName, symbol)

	// ── 第一轮：手动触发闭环 ──
	run, err := retrainer.RunJob(jobID)
	if err != nil {
		t.Fatalf("run job: %v", err)
	}
	if run.Status != "success" {
		t.Fatalf("run status: %s", run.Status)
	}
	if fake.calls() != 1 {
		t.Fatalf("train calls: %d", fake.calls())
	}

	// 模型文件出现（闭环的"导出"环节落盘）
	if _, err := os.Stat(filepath.Join(regDir, modelName+".json")); err != nil {
		t.Fatalf("exported model file missing: %v", err)
	}

	// predictor 已热加载，可本地推理
	pred, ok := registry.Get(modelName)
	if !ok || !pred.IsLoaded() {
		t.Fatalf("predictor not hot-loaded")
	}
	p1, err := pred.PredictFromMap(map[string]float64{"return_5": 0.01})
	if err != nil || p1 != 0.001 {
		t.Fatalf("prediction v1: %v err=%v", p1, err)
	}

	// 训练档案落库：指标/样本/特征数/耗时/版本齐全
	runs, err := trainingRepo.List(0, modelName, 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("training runs: %d err=%v", len(runs), err)
	}
	rec := runs[0]
	if rec.Status != "success" || rec.Trainer != "python" || rec.TriggerSrc != "manual" {
		t.Fatalf("bad record: %+v", rec)
	}
	if rec.TrainSamples != 500 || rec.FeatureCount == 0 || rec.ModelVersion == "" || rec.DurationMs < 0 {
		t.Fatalf("record missing fields: %+v", rec)
	}
	if !strings.Contains(rec.MetricsJSON, "test_rmse") {
		t.Fatalf("metrics not persisted: %s", rec.MetricsJSON)
	}

	// 漂移参考分布已重建（样本 >= MinSamples）
	drift := loop.DriftStatus()
	if len(drift) != 1 || drift[0]["has_baseline"] != true {
		t.Fatalf("drift baseline missing: %+v", drift)
	}

	// ── 第二轮：fake server 出新版模型 → 热加载后预测输出变化 ──
	fake.bumpVersion()
	run2, err := retrainer.RunJob(jobID)
	if err != nil || run2.Status != "success" {
		t.Fatalf("second run: %v status=%v", err, run2.Status)
	}
	pred2, ok := registry.Get(modelName)
	if !ok {
		t.Fatalf("predictor missing after retrain")
	}
	p2, err := pred2.PredictFromMap(map[string]float64{"return_5": 0.01})
	if err != nil || p2 != 0.002 {
		t.Fatalf("prediction v2: %v err=%v（热加载未生效）", p2, err)
	}
	if p1 == p2 {
		t.Fatalf("prediction did not change after retrain: %v", p1)
	}

	// 两版档案版本号不同（内容 hash 对账）
	runs, _ = trainingRepo.List(0, modelName, 10)
	if len(runs) != 2 {
		t.Fatalf("training runs after 2nd: %d", len(runs))
	}
	if runs[0].ModelVersion == runs[1].ModelVersion {
		t.Fatalf("model versions should differ: %s vs %s", runs[0].ModelVersion, runs[1].ModelVersion)
	}
}

// TestTrainingLoopGoFallback ml_server 不可达 + ML_TRAIN_FALLBACK=go：
// 自动降级 Go 原生训练，闭环照常走完（训练→热加载→落档，trainer=go_fallback）。
func TestTrainingLoopGoFallback(t *testing.T) {
	repo := setupMLTestDB(t)
	trainingRepo := store.NewMLTrainingRunRepo()

	// 指向一个已关闭的 server → 连接拒绝（不可达）
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	client := ml.NewClient(deadURL)

	symbol := "FBUSDT"
	modelName := "FBUSDT_1h_fb"
	loader := &fakeBarLoader{bars: syntheticLoopBars(symbol, "1h", 720, 1.0)}
	registry := ml.NewModelRegistry(t.TempDir())

	loop := ml.NewTrainingLoop(ml.NewTrainingPipeline(client, loader), client, registry, trainingRepo)
	loop.FallbackMode = "go"
	retrainer := ml.NewRetrainer(repo, loop, &fakeNotifier{}, nil)

	jobID := newLoopJob(t, repo, modelName, symbol)
	run, err := retrainer.RunJob(jobID)
	if err != nil {
		t.Fatalf("fallback run should succeed, got: %v", err)
	}
	if run.Status != "success" {
		t.Fatalf("fallback run status: %s", run.Status)
	}

	pred, ok := registry.Get(modelName)
	if !ok || !pred.IsLoaded() {
		t.Fatalf("fallback model not hot-loaded")
	}
	if _, err := pred.PredictFromMap(map[string]float64{"return_5": 0.01}); err != nil {
		t.Fatalf("fallback predict: %v", err)
	}

	runs, _ := trainingRepo.List(0, modelName, 10)
	if len(runs) != 1 || runs[0].Trainer != "go_fallback" || runs[0].Status != "success" {
		t.Fatalf("fallback record: %+v", runs)
	}
	if runs[0].ModelVersion == "" || !strings.Contains(runs[0].MetricsJSON, "test_rmse") {
		t.Fatalf("fallback record missing version/metrics: %+v", runs[0])
	}
}

// TestTrainingLoopSkippedWhenServerDown ml_server 不可达 + 降级关闭：
// 记 skipped 档案并返回明确错误（retrainer 侧走告警），绝不 panic。
func TestTrainingLoopSkippedWhenServerDown(t *testing.T) {
	repo := setupMLTestDB(t)
	trainingRepo := store.NewMLTrainingRunRepo()

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	client := ml.NewClient(deadURL)

	symbol := "SKIPUSDT"
	modelName := "SKIPUSDT_1h_skip"
	loader := &fakeBarLoader{bars: syntheticLoopBars(symbol, "1h", 720, 1.0)}

	loop := ml.NewTrainingLoop(ml.NewTrainingPipeline(client, loader), client, ml.NewModelRegistry(t.TempDir()), trainingRepo)
	loop.FallbackMode = "off"
	notifier := &fakeNotifier{}
	retrainer := ml.NewRetrainer(repo, loop, notifier, nil)

	jobID := newLoopJob(t, repo, modelName, symbol)
	// retrainer 手动触发返回运行记录（错误不向上抛，体现在 run.Status/Error），
	// 闭环档案记 skipped，并发出 WARN 告警——整个链路无 panic。
	run, err := retrainer.RunJob(jobID)
	if err != nil {
		t.Fatalf("run job should return record, got err: %v", err)
	}
	if run.Status != "failed" {
		t.Fatalf("expected failed retrain run, got %s", run.Status)
	}
	if !strings.Contains(run.Error, "ml_server") && !strings.Contains(run.Error, "不可达") {
		t.Fatalf("error should mention ml_server unreachable: %s", run.Error)
	}

	runs, _ := trainingRepo.List(0, modelName, 10)
	if len(runs) != 1 || runs[0].Status != "skipped" {
		t.Fatalf("expected skipped record: %+v", runs)
	}
	// retrainer 已发 WARN 告警（跳过并告警，无 panic）
	if len(notifier.Messages()) == 0 {
		t.Fatalf("expected WARN notification for skipped run")
	}
}

// TestTrainingLoopDriftCheck 基线建立后，分布剧变的近期数据触发漂移检出。
func TestTrainingLoopDriftCheck(t *testing.T) {
	repo := setupMLTestDB(t)
	trainingRepo := store.NewMLTrainingRunRepo()

	fake := newLoopFakeMLServer()
	defer fake.close()
	client := ml.NewClient(fake.url())

	symbol := "DRIFTUSDT"
	modelName := "DRIFTUSDT_1h_drift"
	loader := &fakeBarLoader{bars: syntheticLoopBars(symbol, "1h", 720, 1.0)}

	loop := ml.NewTrainingLoop(ml.NewTrainingPipeline(client, loader), client, ml.NewModelRegistry(t.TempDir()), trainingRepo)
	retrainer := ml.NewRetrainer(repo, loop, &fakeNotifier{}, nil)
	jobID := newLoopJob(t, repo, modelName, symbol)

	if _, err := retrainer.RunJob(jobID); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	job, err := repo.GetJob(jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	// 同分布近期窗口：不应误报
	drifted, err := loop.CheckJobDrift(job)
	if err != nil {
		t.Fatalf("check drift: %v", err)
	}
	if drifted {
		t.Fatalf("same-distribution window should not drift")
	}

	// 分布剧变（30 倍波动）：应检出漂移
	loader.bars = syntheticLoopBars(symbol, "1h", 720, 30.0)
	drifted, err = loop.CheckJobDrift(job)
	if err != nil {
		t.Fatalf("check drift (shifted): %v", err)
	}
	if !drifted {
		t.Fatalf("30x volatility shift should be detected as drift")
	}
}
