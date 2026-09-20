package store

import (
	"path/filepath"
	"testing"
	"time"
)

// TestMLRetrainMigration 冒烟验证 0019 迁移：表创建、唯一约束、幂等重跑。
func TestMLRetrainMigration(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-ml-retrain")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	for _, tbl := range []string{"xt_ml_retrain_jobs", "xt_ml_retrain_runs", "xt_ml_predictions", "ml_settings"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name); err != nil {
			t.Fatalf("table %s missing: %v", tbl, err)
		}
	}

	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO xt_ml_retrain_jobs
		(user_id, model_name, feature_set, interval_minutes, last_run_at, last_status, last_error, created_at, active)
		VALUES (1,'BTCUSDT_1h','',1440,0,'','',?,1)`, now); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	// 唯一约束：(model_name, symbol, bar_time, features_hash) 不允许重复。
	if _, err := db.Exec(`INSERT INTO xt_ml_predictions
		(job_id, model_name, symbol, bar_time, features_hash, prediction, created_at)
		VALUES (0,'m1','BTCUSDT',1000,'abc123',0.5,?)`, now); err != nil {
		t.Fatalf("insert prediction: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO xt_ml_predictions
		(job_id, model_name, symbol, bar_time, features_hash, prediction, created_at)
		VALUES (0,'m1','BTCUSDT',1000,'abc123',0.9,?)`, now); err == nil {
		t.Fatalf("expected unique violation on (model_name, symbol, bar_time, features_hash)")
	}

	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}

// TestMLPredictionRepoRetention 预测保留策略：过期删除、新行保留、按模型失效。
func TestMLPredictionRepoRetention(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-ml-prediction")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}

	repo := NewMLPredictionRepo()
	if err := repo.Insert("m1", "BTCUSDT", 1000, "hash_a", 0.5); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := repo.Insert("m1", "ETHUSDT", 1000, "hash_b", -0.1); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// 同 key 重复插入被唯一约束忽略。
	if err := repo.Insert("m1", "BTCUSDT", 1000, "hash_a", 0.9); err != nil {
		t.Fatalf("duplicate insert should be ignored: %v", err)
	}
	if n, _ := repo.Count(); n != 2 {
		t.Fatalf("count = %d, want 2 (duplicate ignored)", n)
	}
	if v, ok, _ := repo.Get("m1", "BTCUSDT", 1000, "hash_a"); !ok || v != 0.5 {
		t.Fatalf("get = (%v,%v), want (0.5,true)", v, ok)
	}

	// 老化一行后按 cutoff 清理。
	old := time.Now().Add(-48 * time.Hour).UnixMilli()
	if _, err := db.Exec(`INSERT INTO xt_ml_predictions
		(job_id, model_name, symbol, bar_time, features_hash, prediction, created_at)
		VALUES (0,'m1','BTCUSDT',2000,'hash_old',0.1,?)`, old); err != nil {
		t.Fatalf("insert old row: %v", err)
	}
	cutoff := time.Now().Add(-24 * time.Hour).UnixMilli()
	deleted, err := repo.DeleteOlderThan(cutoff)
	if err != nil {
		t.Fatalf("delete older than: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, ok, _ := repo.Get("m1", "BTCUSDT", 2000, "hash_old"); ok {
		t.Fatalf("old prediction should be gone")
	}
	if _, ok, _ := repo.Get("m1", "BTCUSDT", 1000, "hash_a"); !ok {
		t.Fatalf("fresh prediction should be kept")
	}

	// 按模型失效：m1 的两行被清，m2 保留。
	if err := repo.Insert("m2", "SOLUSDT", 1000, "hash_c", 0.3); err != nil {
		t.Fatalf("insert m2: %v", err)
	}
	if n, err := repo.DeleteForModel("m1"); err != nil || n != 2 {
		t.Fatalf("delete for model: n=%d err=%v, want 2 rows", n, err)
	}
	if n, _ := repo.Count(); n != 1 {
		t.Fatalf("count = %d, want 1 (other model kept)", n)
	}
	if _, ok, _ := repo.Get("m2", "SOLUSDT", 1000, "hash_c"); !ok {
		t.Fatalf("m2 prediction should be kept")
	}
}

// TestMLRetrainRepoRuns 运行记录与连续失败计数。
func TestMLRetrainRepoRuns(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-ml-runs")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}

	repo := NewMLRetrainRepo()
	id, err := repo.CreateJob(7, "BTCUSDT_1h", "", 1440)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	job, err := repo.GetJob(id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.UserID != 7 || job.ModelName != "BTCUSDT_1h" || job.IntervalMinutes != 1440 || !job.Active {
		t.Fatalf("job fields mismatch: %+v", job)
	}

	// failed, failed, success → 连续失败计数归零。
	for i := 0; i < 2; i++ {
		if _, err := repo.RecordRun(id, "failed", "boom", 10); err != nil {
			t.Fatalf("record failed run: %v", err)
		}
	}
	if _, err := repo.RecordRun(id, "success", "", 20); err != nil {
		t.Fatalf("record success run: %v", err)
	}
	if n, _ := repo.ConsecutiveFailures(id, 3); n != 0 {
		t.Fatalf("consecutive failures = %d, want 0 after success", n)
	}

	// success 后再失败两次 → 计数 2（阈值 3，未暂停）。
	for i := 0; i < 2; i++ {
		if _, err := repo.RecordRun(id, "failed", "boom", 10); err != nil {
			t.Fatalf("record failed run: %v", err)
		}
	}
	if n, _ := repo.ConsecutiveFailures(id, 3); n != 2 {
		t.Fatalf("consecutive failures = %d, want 2", n)
	}

	// job 行回写 last_*。
	job, _ = repo.GetJob(id)
	if job.LastStatus != "failed" || job.LastError != "boom" || job.LastRunAt == 0 {
		t.Fatalf("job last_* not updated: %+v", job)
	}

	// 运行记录倒序返回。
	runs, err := repo.ListRuns(id, 10)
	if err != nil || len(runs) != 5 {
		t.Fatalf("runs = %v, err = %v, want 5", len(runs), err)
	}

	// 启停 + 属主过滤。
	if err := repo.SetJobActive(id, false); err != nil {
		t.Fatalf("set active: %v", err)
	}
	jobs, _ := repo.ListJobs(0, true)
	if len(jobs) != 0 {
		t.Fatalf("active-only list = %d, want 0", len(jobs))
	}
	jobs, _ = repo.ListJobs(7, false)
	if len(jobs) != 1 {
		t.Fatalf("owner list = %d, want 1", len(jobs))
	}
	jobs, _ = repo.ListJobs(9, false)
	if len(jobs) != 0 {
		t.Fatalf("other owner list = %d, want 0", len(jobs))
	}
}
