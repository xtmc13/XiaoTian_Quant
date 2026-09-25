package store

import (
	"path/filepath"
	"testing"
)

// TestAnalysisJobMigrationAndRepo 冒烟验证 0025 迁移：表创建、Create/Finish/GetByID/List 与幂等重跑。
func TestAnalysisJobMigrationAndRepo(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-for-analysis-jobs")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='xt_analysis_jobs'`).Scan(&name); err != nil {
		t.Fatalf("table xt_analysis_jobs missing: %v", err)
	}

	repo := NewAnalysisJobRepo()
	job := &AnalysisJobRecord{
		ID: "ana-test-1", UserID: 7, Kind: "lookahead", Status: "running",
		Symbol: "BTCUSDT", Interval: "1h", StrategyType: "sma_cross",
	}
	if err := repo.Create(job); err != nil {
		t.Fatalf("create: %v", err)
	}
	if job.CreatedAt == 0 || job.UpdatedAt == 0 {
		t.Fatalf("create should fill timestamps: %+v", job)
	}
	if err := repo.Create(&AnalysisJobRecord{ID: "ana-test-2", UserID: 8, Kind: "recursive", Status: "running"}); err != nil {
		t.Fatalf("create 2: %v", err)
	}

	if err := repo.Finish("ana-test-1", "completed", `{"biased":true}`, ""); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, err := repo.GetByID("ana-test-1")
	if err != nil || got == nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Status != "completed" || got.ResultJSON != `{"biased":true}` || got.CompletedAt == 0 {
		t.Fatalf("finish round-trip failed: %+v", got)
	}
	missing, err := repo.GetByID("ana-nope")
	if err != nil || missing != nil {
		t.Fatalf("missing job should be (nil,nil), got %v %v", missing, err)
	}

	list, err := repo.List(7, "", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "ana-test-1" {
		t.Fatalf("user 7 should see own job only, got %+v", list)
	}
	byKind, err := repo.List(8, "recursive", 10)
	if err != nil || len(byKind) != 1 {
		t.Fatalf("kind filter failed: %v %+v", err, byKind)
	}

	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}
