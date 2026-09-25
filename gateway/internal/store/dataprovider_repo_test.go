package store

import (
	"path/filepath"
	"testing"
)

// TestDataProviderCacheMigrationAndRepo 冒烟验证 0031 迁移：表创建、
// Upsert/Get/DeleteExpired 与冲突覆盖。
func TestDataProviderCacheMigrationAndRepo(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-for-dataprovider-cache")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='xt_dataprovider_cache'`).Scan(&name); err != nil {
		t.Fatalf("table xt_dataprovider_cache missing: %v", err)
	}

	repo := NewDataProviderRepo()

	// 未命中
	if _, _, _, err := repo.GetDataProviderCache("fear_greed", "default"); err == nil {
		t.Fatal("get on empty table should return error")
	}

	// Upsert + Get
	if err := repo.UpsertDataProviderCache("fear_greed", "default", `{"value":42}`, 1000, 2000); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	payload, fetchedAt, expiresAt, err := repo.GetDataProviderCache("fear_greed", "default")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if payload != `{"value":42}` || fetchedAt != 1000 || expiresAt != 2000 {
		t.Fatalf("roundtrip wrong: %s %d %d", payload, fetchedAt, expiresAt)
	}

	// 冲突覆盖
	if err := repo.UpsertDataProviderCache("fear_greed", "default", `{"value":43}`, 3000, 4000); err != nil {
		t.Fatalf("upsert conflict: %v", err)
	}
	payload, fetchedAt, _, err = repo.GetDataProviderCache("fear_greed", "default")
	if err != nil || payload != `{"value":43}` || fetchedAt != 3000 {
		t.Fatalf("overwrite wrong: %s %d %v", payload, fetchedAt, err)
	}

	// 空 cache_key 归一为 default
	if err := repo.UpsertDataProviderCache("news", "", `{"items":[]}`, 100, 200); err != nil {
		t.Fatalf("upsert empty key: %v", err)
	}
	if _, _, _, err := repo.GetDataProviderCache("news", "default"); err != nil {
		t.Fatalf("empty cache_key should normalize to default: %v", err)
	}

	// 过期清理
	if n, err := repo.DeleteExpiredDataProviderCache(); err != nil || n != 2 {
		t.Fatalf("delete expired should remove 2 rows, got %d err=%v", n, err)
	}
	if _, _, _, err := repo.GetDataProviderCache("fear_greed", "default"); err == nil {
		t.Fatal("expired row should be deleted")
	}
}
