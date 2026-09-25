package store

import (
	"path/filepath"
	"testing"
)

// TestExchangeHealthMigrationAndRepo 冒烟验证 0024 迁移：表创建、Insert/Latest/History 与幂等重跑。
func TestExchangeHealthMigrationAndRepo(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-for-exchange-health")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='xt_exchange_health_checks'`).Scan(&name); err != nil {
		t.Fatalf("table xt_exchange_health_checks missing: %v", err)
	}

	repo := NewExchangeHealthRepo()
	r1 := &ExchangeHealthRecord{Exchange: "binance", JobID: "job-1", Overall: "healthy", Configured: true, DurationMs: 120, ResultJSON: `{"overall":"healthy"}`}
	if err := repo.Insert(r1); err != nil {
		t.Fatalf("insert r1: %v", err)
	}
	if r1.ID == "" || r1.CreatedAt == 0 {
		t.Fatalf("insert should fill id/created_at: %+v", r1)
	}
	// 同所第二条（更晚）→ latest 应指向它
	r2 := &ExchangeHealthRecord{Exchange: "binance", JobID: "job-2", Overall: "degraded", Configured: true, DurationMs: 200, ResultJSON: `{"overall":"degraded"}`, CreatedAt: r1.CreatedAt + 1000}
	if err := repo.Insert(r2); err != nil {
		t.Fatalf("insert r2: %v", err)
	}
	r3 := &ExchangeHealthRecord{Exchange: "okx", JobID: "job-2", Overall: "not_configured", ResultJSON: `{}`, CreatedAt: r1.CreatedAt + 500}
	if err := repo.Insert(r3); err != nil {
		t.Fatalf("insert r3: %v", err)
	}

	latest, err := repo.LatestPerExchange()
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("latest should have 2 exchanges, got %d", len(latest))
	}
	byEx := map[string]*ExchangeHealthRecord{}
	for _, rec := range latest {
		byEx[rec.Exchange] = rec
	}
	if byEx["binance"] == nil || byEx["binance"].ID != r2.ID {
		t.Fatalf("binance latest should be r2, got %+v", byEx["binance"])
	}
	if !byEx["binance"].Configured {
		t.Fatalf("configured flag should round-trip true")
	}
	if byEx["okx"] == nil || byEx["okx"].Overall != "not_configured" {
		t.Fatalf("okx latest wrong: %+v", byEx["okx"])
	}

	hist, err := repo.History("binance", 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 2 || hist[0].ID != r2.ID {
		t.Fatalf("history should be 2 rows desc, got %+v", hist)
	}
	all, err := repo.History("", 10)
	if err != nil || len(all) != 3 {
		t.Fatalf("history all should be 3 rows, got %d err=%v", len(all), err)
	}

	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}
