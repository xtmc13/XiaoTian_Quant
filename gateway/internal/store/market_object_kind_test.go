package store

import (
	"path/filepath"
	"testing"
)

// TestMarketObjectKindMigration 冒烟验证 0058 迁移：xt_market_listings 增加
// object_kind/object_id 列与索引，存量行默认 ai_bot 且 object_id 回填为
// bot_instance_id，幂等重跑。
func TestMarketObjectKindMigration(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-market-object-kind")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	for _, col := range []string{"object_kind", "object_id"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(1) FROM pragma_table_info('xt_market_listings') WHERE name=?`, col).Scan(&n); err != nil || n != 1 {
			t.Fatalf("xt_market_listings missing %s column: n=%d err=%v", col, n, err)
		}
	}
	var idx string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_market_listings_object'`).Scan(&idx); err != nil {
		t.Fatalf("index idx_market_listings_object missing: %v", err)
	}

	// 存量形态行（不带新列的 INSERT）：object_kind 默认 ai_bot、object_id 默认空。
	if _, err := db.Exec(`INSERT INTO xt_market_listings (id, author_user_id, bot_instance_id, kind, name, status, created_at, updated_at)
		VALUES ('ml-legacy', 1, 'aibot-legacy', 'robot', 'legacy', 'draft', 1, 1)`); err != nil {
		t.Fatalf("insert legacy listing: %v", err)
	}
	var kind, objectID string
	if err := db.QueryRow(`SELECT object_kind, object_id FROM xt_market_listings WHERE id='ml-legacy'`).Scan(&kind, &objectID); err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if kind != "ai_bot" || objectID != "" {
		t.Fatalf("legacy defaults: kind=%s object_id=%s, want ai_bot/''", kind, objectID)
	}

	// 回填语义（与迁移内 UPDATE 相同）：object_id = bot_instance_id。
	if _, err := db.Exec(`UPDATE xt_market_listings SET object_id = bot_instance_id WHERE object_kind = 'ai_bot' AND object_id = ''`); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if err := db.QueryRow(`SELECT object_id FROM xt_market_listings WHERE id='ml-legacy'`).Scan(&objectID); err != nil || objectID != "aibot-legacy" {
		t.Fatalf("backfilled object_id: got %s err=%v, want aibot-legacy", objectID, err)
	}

	// repo 读写新字段 roundtrip。
	repo := NewMarketListingRepo()
	rec := &MarketListingRecord{
		ID: "ml-grid-1", AuthorUserID: 2, ObjectKind: "grid_bot", ObjectID: "grid-1",
		Kind: "robot", Name: "网格", Status: "draft",
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	got := repo.GetByID("ml-grid-1")
	if got == nil || got.ObjectKind != "grid_bot" || got.ObjectID != "grid-1" {
		t.Fatalf("repo roundtrip: %+v", got)
	}
	got.ObjectID = "grid-2"
	if err := repo.Update(got); err != nil {
		t.Fatalf("repo update: %v", err)
	}
	if again := repo.GetByID("ml-grid-1"); again == nil || again.ObjectID != "grid-2" {
		t.Fatalf("repo update roundtrip: %+v", again)
	}

	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}
