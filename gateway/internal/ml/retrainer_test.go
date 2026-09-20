package ml_test

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/ml"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// setupMLTestDB 初始化临时 SQLite（含全部迁移），返回重训仓库。
func setupMLTestDB(t *testing.T) *store.MLRetrainRepo {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "ml_test.db"))
	t.Setenv("SECRET_KEY", "ml-test-secret")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := store.RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	t.Cleanup(func() { store.CloseDB() })
	return store.NewMLRetrainRepo()
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

// fakeRunner 可编排结果的 PipelineRunner mock。
type fakeRunner struct {
	mu      sync.Mutex
	calls   int
	errs    []error
	results []*ml.PipelineResult
	lastCfg ml.PipelineConfig
}

func (f *fakeRunner) Run(cfg ml.PipelineConfig) (*ml.PipelineResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	f.calls++
	f.lastCfg = cfg
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	var res *ml.PipelineResult
	if i < len(f.results) {
		res = f.results[i]
	} else {
		res = &ml.PipelineResult{Success: true, ModelID: cfg.ModelID, TrainSamples: 42}
	}
	return res, err
}

func (f *fakeRunner) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeNotifier 捕获告警的 NotifySender mock。
type fakeNotifier struct {
	mu   sync.Mutex
	msgs []notify.Message
}

func (f *fakeNotifier) Send(m notify.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, m)
}

func (f *fakeNotifier) Messages() []notify.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]notify.Message, len(f.msgs))
	copy(out, f.msgs)
	return out
}

func (f *fakeNotifier) CountLevel(level string) int {
	n := 0
	for _, m := range f.Messages() {
		if m.Level == level {
			n++
		}
	}
	return n
}

// TestRetrainerScheduledTrigger 调度触发：到期任务被自动执行并记账。
func TestRetrainerScheduledTrigger(t *testing.T) {
	repo := setupMLTestDB(t)
	t.Setenv("ML_RETRAIN_CHECK_SEC", "1")

	jobID, err := repo.CreateJob(1, "BTCUSDT_1h", "", 1440)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	runner := &fakeRunner{}
	notifier := &fakeNotifier{}
	r := ml.NewRetrainer(repo, runner, notifier, nil)
	r.Start()
	defer r.Stop()

	waitFor(t, 5*time.Second, func() bool { return runner.Calls() >= 1 }, "scheduled retrain run")

	job, err := repo.GetJob(jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.LastStatus != "success" {
		t.Fatalf("last_status = %q, want success (err=%q)", job.LastStatus, job.LastError)
	}
	if job.LastRunAt == 0 {
		t.Fatalf("last_run_at not updated")
	}
	runs, err := repo.ListRuns(jobID, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != "success" {
		t.Fatalf("runs = %v, err = %v, want 1 success", runs, err)
	}
	if n := notifier.CountLevel("WARN"); n != 0 {
		t.Fatalf("unexpected WARN notifications: %d", n)
	}
	// 成功后不再到期：间隔 1440 分钟，5s 内不应有第二次调用。
	time.Sleep(2200 * time.Millisecond)
	if runner.Calls() != 1 {
		t.Fatalf("calls = %d, want 1 (interval not reached)", runner.Calls())
	}
}

// TestRetrainerRespectsInterval 未到期任务不触发。
func TestRetrainerRespectsInterval(t *testing.T) {
	repo := setupMLTestDB(t)
	t.Setenv("ML_RETRAIN_CHECK_SEC", "1")

	jobID, err := repo.CreateJob(1, "BTCUSDT_1h", "", 1440)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	runner := &fakeRunner{}
	r := ml.NewRetrainer(repo, runner, nil, nil)
	// 手动跑一轮把 last_run_at 刷成 now，之后任务未到期。
	if _, err := r.RunJob(jobID); err != nil {
		t.Fatalf("manual run: %v", err)
	}
	if runner.Calls() != 1 {
		t.Fatalf("calls = %d, want 1", runner.Calls())
	}

	r.Start()
	defer r.Stop()
	time.Sleep(2200 * time.Millisecond)
	if runner.Calls() != 1 {
		t.Fatalf("calls = %d, want 1 (job not due)", runner.Calls())
	}
}

// TestRetrainerFailureCountingAndAutoPause 失败计数 + 连续 3 次自动暂停 + 告警。
func TestRetrainerFailureCountingAndAutoPause(t *testing.T) {
	repo := setupMLTestDB(t)

	jobID, err := repo.CreateJob(1, "BTCUSDT_1h", "", 1)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	runner := &fakeRunner{errs: []error{errBoom(), errBoom(), errBoom()}}
	notifier := &fakeNotifier{}
	r := ml.NewRetrainer(repo, runner, notifier, nil)

	// 前两次失败：任务保持启用，每次都有 WARN 告警。
	for i := 0; i < 2; i++ {
		run, err := r.RunJob(jobID)
		if err != nil {
			t.Fatalf("manual run %d: %v", i, err)
		}
		if run.Status != "failed" || run.Error == "" {
			t.Fatalf("run %d = %+v, want failed with error", i, run)
		}
		job, _ := repo.GetJob(jobID)
		if !job.Active {
			t.Fatalf("job should stay active after %d failures", i+1)
		}
	}
	if n := notifier.CountLevel("WARN"); n != 2 {
		t.Fatalf("WARN count = %d, want 2", n)
	}

	// 第三次失败：连续失败达阈值 → 自动暂停 + CRITICAL 告警。
	if _, err := r.RunJob(jobID); err != nil {
		t.Fatalf("manual run 3: %v", err)
	}
	job, err := repo.GetJob(jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Active {
		t.Fatalf("job should be auto-paused after 3 consecutive failures")
	}
	if job.LastStatus != "failed" || job.LastError == "" {
		t.Fatalf("job last_* = %q/%q", job.LastStatus, job.LastError)
	}
	if n := notifier.CountLevel("WARN"); n != 3 {
		t.Fatalf("WARN count = %d, want 3", n)
	}
	if n := notifier.CountLevel("CRITICAL"); n != 1 {
		t.Fatalf("CRITICAL count = %d, want 1 (auto-pause alert)", n)
	}
	if fails, _ := repo.ConsecutiveFailures(jobID, 3); fails != 3 {
		t.Fatalf("consecutive failures = %d, want 3", fails)
	}
	runs, _ := repo.ListRuns(jobID, 10)
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(runs))
	}
}

// TestRetrainerSuccessResetsFailureCount 成功一次后连续失败计数清零，不暂停。
func TestRetrainerSuccessResetsFailureCount(t *testing.T) {
	repo := setupMLTestDB(t)

	jobID, err := repo.CreateJob(1, "BTCUSDT_1h", "", 1)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	runner := &fakeRunner{errs: []error{errBoom(), errBoom(), nil}}
	notifier := &fakeNotifier{}
	r := ml.NewRetrainer(repo, runner, notifier, nil)

	for i := 0; i < 3; i++ {
		if _, err := r.RunJob(jobID); err != nil {
			t.Fatalf("manual run %d: %v", i, err)
		}
	}
	job, _ := repo.GetJob(jobID)
	if !job.Active {
		t.Fatalf("job should stay active: 2 fails then success resets the count")
	}
	if fails, _ := repo.ConsecutiveFailures(jobID, 3); fails != 0 {
		t.Fatalf("consecutive failures = %d, want 0", fails)
	}
}

// TestRetrainerSuccessInvalidatesPredictions 重训成功后该模型旧预测缓存失效，其他模型不受影响。
func TestRetrainerSuccessInvalidatesPredictions(t *testing.T) {
	repo := setupMLTestDB(t)
	predRepo := store.NewMLPredictionRepo()

	if err := predRepo.Insert("BTCUSDT_1h", "BTCUSDT", 1000, "h1", 0.5); err != nil {
		t.Fatalf("insert prediction: %v", err)
	}
	if err := predRepo.Insert("OTHER_model", "ETHUSDT", 1000, "h2", -0.2); err != nil {
		t.Fatalf("insert prediction: %v", err)
	}

	jobID, err := repo.CreateJob(1, "BTCUSDT_1h", "", 1)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	r := ml.NewRetrainer(repo, &fakeRunner{}, nil, nil)
	if _, err := r.RunJob(jobID); err != nil {
		t.Fatalf("manual run: %v", err)
	}

	if _, ok, _ := predRepo.Get("BTCUSDT_1h", "BTCUSDT", 1000, "h1"); ok {
		t.Fatalf("retrained model predictions should be invalidated")
	}
	if _, ok, _ := predRepo.Get("OTHER_model", "ETHUSDT", 1000, "h2"); !ok {
		t.Fatalf("other model predictions should be kept")
	}
}

// TestRetrainerTTLCleanup 预测缓存 TTL 清理（默认 30 天，可用 ML_PREDICTION_TTL_D 覆盖）。
func TestRetrainerTTLCleanup(t *testing.T) {
	repo := setupMLTestDB(t)
	t.Setenv("ML_PREDICTION_TTL_D", "30")

	predRepo := store.NewMLPredictionRepo()
	r := ml.NewRetrainer(repo, &fakeRunner{}, nil, nil)

	// 新行：不清理。
	if err := predRepo.Insert("m1", "BTCUSDT", 1000, "h1", 0.5); err != nil {
		t.Fatalf("insert: %v", err)
	}
	deleted, err := r.CleanupPredictions()
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0 (fresh row kept)", deleted)
	}
	if n, _ := predRepo.Count(); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}

	// TTL 覆盖生效：配置进 cfg（这里直接验证 env 解析进配置）。
	if r.Config().PredictionTTL != 30*24*time.Hour {
		t.Fatalf("ttl = %s, want 720h", r.Config().PredictionTTL)
	}
}

// TestJobPipelineConfig feature_set 解析：空/完整/非法 JSON。
func TestJobPipelineConfig(t *testing.T) {
	def := ml.DefaultPipelineConfig()

	// 空 feature_set → 默认值，ModelID 固定为任务模型名。
	job := &store.MLRetrainJob{ID: 1, ModelName: "BTCUSDT_1h"}
	cfg := ml.JobPipelineConfig(job)
	if cfg.ModelID != "BTCUSDT_1h" || cfg.Symbol != def.Symbol || cfg.Interval != def.Interval ||
		cfg.ModelType != def.ModelType || cfg.TaskType != def.TaskType ||
		cfg.LookbackDays != def.LookbackDays || cfg.LabelHorizon != def.LabelHorizon {
		t.Fatalf("empty feature_set should fall back to defaults: %+v", cfg)
	}

	// 完整 JSON → 逐项覆盖。
	job = &store.MLRetrainJob{ID: 2, ModelName: "ETH_15m",
		FeatureSet: `{"symbol":"ETHUSDT","interval":"15m","model_type":"xgboost","task_type":"classification","lookback_days":30,"feature_periods":[3,7],"label_horizon":3,"model_params":{"n_estimators":50}}`}
	cfg = ml.JobPipelineConfig(job)
	if cfg.Symbol != "ETHUSDT" || cfg.Interval != "15m" || cfg.ModelType != "xgboost" ||
		cfg.TaskType != "classification" || cfg.LookbackDays != 30 ||
		len(cfg.FeaturePeriods) != 2 || cfg.FeaturePeriods[0] != 3 || cfg.LabelHorizon != 3 ||
		cfg.ModelParams["n_estimators"] != float64(50) {
		t.Fatalf("feature_set not applied: %+v", cfg)
	}

	// 非法 JSON → 回落默认，不得 panic。
	job = &store.MLRetrainJob{ID: 3, ModelName: "m3", FeatureSet: "{not-json"}
	cfg = ml.JobPipelineConfig(job)
	if cfg.ModelID != "m3" || cfg.Symbol != def.Symbol {
		t.Fatalf("garbage feature_set should fall back: %+v", cfg)
	}
}

type boomError struct{}

func (boomError) Error() string { return "boom" }
func errBoom() error            { return boomError{} }
