package store

import (
	"database/sql"
	"errors"
	"testing"
)

// TestMLTrainingRunRepoCRUD 记录/列表/最新/按模型聚合。
func TestMLTrainingRunRepoCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewMLTrainingRunRepo()
	mk := func(jobID int64, model, status, trainer string) *MLTrainingRun {
		return &MLTrainingRun{
			JobID: jobID, ModelName: model, TriggerSrc: "schedule", Trainer: trainer,
			Status: status, Symbol: "BTCUSDT", Interval: "1h",
			BarsLoaded: 720, TrainSamples: 500, TestSamples: 100, FeatureCount: 42,
			MetricsJSON: `{"test_rmse":0.01}`, ModelVersion: "abc123def456", DurationMs: 1234,
		}
	}

	if _, err := repo.Record(mk(1, "BTCUSDT_1h", "success", "python")); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if _, err := repo.Record(mk(2, "ETHUSDT_1h", "failed", "python")); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if _, err := repo.Record(mk(1, "BTCUSDT_1h", "success", "go_fallback")); err != nil {
		t.Fatalf("record 3: %v", err)
	}

	// 全量列表（admin 视角 userID=0）
	runs, err := repo.List(0, "", 10)
	if err != nil || len(runs) != 3 {
		t.Fatalf("list all: runs=%d err=%v", len(runs), err)
	}
	if runs[0].Trainer != "go_fallback" { // 新的在前
		t.Fatalf("list order: first trainer=%s", runs[0].Trainer)
	}

	// 按模型过滤
	runs, err = repo.List(0, "BTCUSDT_1h", 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("list by model: runs=%d err=%v", len(runs), err)
	}

	// Latest
	last, err := repo.Latest("BTCUSDT_1h")
	if err != nil || last.Trainer != "go_fallback" {
		t.Fatalf("latest: %+v err=%v", last, err)
	}
	if _, err := repo.Latest("NO_SUCH"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("latest missing: want ErrNoRows, got %v", err)
	}

	// 每模型最近成功
	per, err := repo.LatestSuccessPerModel()
	if err != nil || len(per) != 1 { // ETHUSDT_1h 只有 failed
		t.Fatalf("latest success per model: %+v err=%v", per, err)
	}
	if per[0].ModelName != "BTCUSDT_1h" || per[0].Trainer != "go_fallback" {
		t.Fatalf("latest success content: %+v", per[0])
	}
}

// TestMLTrainingRunRepoOwnerFilter 普通用户只见本人任务 + job_id=0 公共运行。
func TestMLTrainingRunRepoOwnerFilter(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	jobs := NewMLRetrainRepo()
	jobUser1, err := jobs.CreateJob(1, "BTCUSDT_1h", "", 1440)
	if err != nil {
		t.Fatalf("create job u1: %v", err)
	}
	jobUser2, err := jobs.CreateJob(2, "ETHUSDT_1h", "", 1440)
	if err != nil {
		t.Fatalf("create job u2: %v", err)
	}

	repo := NewMLTrainingRunRepo()
	mk := func(jobID int64, model string) *MLTrainingRun {
		return &MLTrainingRun{JobID: jobID, ModelName: model, Status: "success", Trainer: "python"}
	}
	_, _ = repo.Record(mk(jobUser1, "BTCUSDT_1h"))
	_, _ = repo.Record(mk(jobUser2, "ETHUSDT_1h"))
	_, _ = repo.Record(mk(0, "PUBLIC_MODEL")) // 无任务关联的公共运行

	runs, err := repo.List(1, "", 10)
	if err != nil {
		t.Fatalf("list u1: %v", err)
	}
	if len(runs) != 2 { // 本人任务 + 公共运行，不见 u2 的
		t.Fatalf("u1 sees %d runs, want 2", len(runs))
	}
	for _, r := range runs {
		if r.ModelName == "ETHUSDT_1h" {
			t.Fatalf("u1 should not see u2 run: %+v", r)
		}
	}

	// admin（0）全量
	runs, err = repo.List(0, "", 10)
	if err != nil || len(runs) != 3 {
		t.Fatalf("admin sees %d runs, want 3 (err=%v)", len(runs), err)
	}
}
