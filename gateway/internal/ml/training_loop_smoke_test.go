package ml_test

import (
	"os"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/ml"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// TestTrainingLoopRealServerSmoke 真实 ml_server 冒烟（opt-in）：
// 设置 ML_SMOKE_URL=http://127.0.0.1:8001 后运行，驱动真实 Python 训练链路
// （合成 K 线 → /train → /models/:id/export → 热加载 → 本地预测）。
// 默认跳过，CI/单测不依赖 Python 环境。
//
// 手动验证步骤：
//   1. pip3 install -r sandbox/ml_server/requirements.txt
//   2. python3 sandbox/ml_server/server.py --port 8001 &
//   3. cd gateway && ML_SMOKE_URL=http://127.0.0.1:8001 go test ./internal/ml/ -run TestTrainingLoopRealServerSmoke -v
func TestTrainingLoopRealServerSmoke(t *testing.T) {
	url := os.Getenv("ML_SMOKE_URL")
	if url == "" {
		t.Skip("ML_SMOKE_URL 未设置，跳过真实 ml_server 冒烟")
	}

	repo := setupMLTestDB(t)
	trainingRepo := store.NewMLTrainingRunRepo()
	client := ml.NewClient(url)

	if err := client.Health(); err != nil {
		t.Fatalf("ml_server 不可达 %s: %v", url, err)
	}

	symbol := "SMOKEUSDT"
	modelName := "smoke_real_" + t.Name()
	loader := &fakeBarLoader{bars: syntheticLoopBars(symbol, "1h", 720, 1.0)}
	registry := ml.NewModelRegistry(t.TempDir())
	loop := ml.NewTrainingLoop(ml.NewTrainingPipeline(client, loader), client, registry, trainingRepo)
	retrainer := ml.NewRetrainer(repo, loop, &fakeNotifier{}, nil)

	jobID := newLoopJob(t, repo, modelName, symbol)
	run, err := retrainer.RunJob(jobID)
	if err != nil {
		t.Fatalf("real smoke run: %v", err)
	}
	if run.Status != "success" {
		t.Fatalf("real smoke run status: %s (%s)", run.Status, run.Error)
	}

	// 热加载生效：Go predictor 可直接推理
	pred, ok := registry.Get(modelName)
	if !ok || !pred.IsLoaded() {
		t.Fatalf("real model not hot-loaded")
	}
	p, err := pred.PredictFromMap(map[string]float64{"return_5": 0.01, "return_10": 0.02})
	if err != nil {
		t.Fatalf("real predict: %v", err)
	}
	t.Logf("real model prediction: %v", p)

	// 档案齐全
	runs, _ := trainingRepo.List(0, modelName, 5)
	if len(runs) != 1 || runs[0].Status != "success" || runs[0].ModelVersion == "" {
		t.Fatalf("real smoke record: %+v", runs)
	}
	t.Logf("real smoke ok: trainer=%s version=%s samples=%d metrics=%s",
		runs[0].Trainer, runs[0].ModelVersion, runs[0].TrainSamples, runs[0].MetricsJSON)

	// 清理真实 server 上的模型
	_ = client.DeleteModel(modelName)
}
