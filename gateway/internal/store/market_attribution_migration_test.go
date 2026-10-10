package store

import (
	"path/filepath"
	"testing"
)

// TestMarketAttributionMigration 冒烟验证 0060 迁移：xt_market_listings 增加
// provider_id，ai_bot_instances/grid_bots 增加 origin_listing_id 与索引，
// 存量行默认值，repo 读写 roundtrip，幂等重跑。
func TestMarketAttributionMigration(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-market-attribution")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}

	for _, tc := range []struct{ table, col string }{
		{"xt_market_listings", "provider_id"},
		{"ai_bot_instances", "origin_listing_id"},
		{"grid_bots", "origin_listing_id"},
	} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(1) FROM pragma_table_info(?) WHERE name=?`, tc.table, tc.col).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s missing %s column: n=%d err=%v", tc.table, tc.col, n, err)
		}
	}
	for _, idx := range []string{"idx_aibot_inst_origin", "idx_grid_bots_origin"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name); err != nil {
			t.Fatalf("index %s missing: %v", idx, err)
		}
	}

	// 存量形态行（不带新列的 INSERT）：provider_id 默认 0、origin_listing_id 默认空。
	if _, err := db.Exec(`INSERT INTO xt_market_listings (id, author_user_id, bot_instance_id, kind, name, status, created_at, updated_at)
		VALUES ('ml-attr-legacy', 1, 'aibot-legacy', 'robot', 'legacy', 'draft', 1, 1)`); err != nil {
		t.Fatalf("insert legacy listing: %v", err)
	}
	var providerID int64
	if err := db.QueryRow(`SELECT provider_id FROM xt_market_listings WHERE id='ml-attr-legacy'`).Scan(&providerID); err != nil || providerID != 0 {
		t.Fatalf("legacy provider_id default: got %d err=%v, want 0", providerID, err)
	}

	// listing repo 读写 provider_id roundtrip。
	repo := NewMarketListingRepo()
	rec := &MarketListingRecord{
		ID: "ml-attr-1", AuthorUserID: 2, ObjectKind: "ai_bot", ObjectID: "aibot-1",
		Kind: "robot", Name: "归因", Status: "listed", ProviderID: 42,
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	if got := repo.GetByID("ml-attr-1"); got == nil || got.ProviderID != 42 {
		t.Fatalf("repo roundtrip provider_id: %+v", got)
	}

	// ai_bot 实例 origin 读写 + 按 origin 反查。
	SaveAIBotInstance(map[string]any{
		"id": "aibot-clone-1", "user_id": 3, "name": "克隆", "strategy_type": "ai_alpha",
		"symbol": "BTCUSDT", "origin_listing_id": "ml-attr-1",
		"created_at": 1, "updated_at": 1,
	})
	inst := GetAIBotInstanceByID("aibot-clone-1", 3)
	if inst == nil || getString(inst, "origin_listing_id", "") != "ml-attr-1" {
		t.Fatalf("ai bot origin roundtrip: %+v", inst)
	}
	if found := FindAIBotInstanceByOrigin(3, "ml-attr-1"); found == nil || getString(found, "id", "") != "aibot-clone-1" {
		t.Fatalf("FindAIBotInstanceByOrigin: %+v", found)
	}
	if found := FindAIBotInstanceByOrigin(4, "ml-attr-1"); found != nil {
		t.Fatalf("FindAIBotInstanceByOrigin other user must be nil, got %+v", found)
	}
	// 旧实例（无 origin 键的 map 写入）默认空串，重存不炸。
	SaveAIBotInstance(map[string]any{
		"id": "aibot-plain-1", "user_id": 3, "name": "普通", "strategy_type": "ai_alpha",
		"symbol": "ETHUSDT", "created_at": 1, "updated_at": 1,
	})
	if inst := GetAIBotInstanceByID("aibot-plain-1", 3); inst == nil || getString(inst, "origin_listing_id", "x") != "" {
		t.Fatalf("plain instance origin default: %+v", inst)
	}

	// grid repo origin 读写 + 按 origin 反查 + Update 全字段回写不丢 origin。
	grepo := NewGridRepo()
	grec := &GridBotRecord{
		UserID: 3, Name: "克隆网格", Symbol: "BTCUSDT", LowerPrice: 100, UpperPrice: 200,
		GridCount: 5, Investment: 1000, OriginListingID: "ml-attr-1",
	}
	if err := grepo.Create(grec); err != nil {
		t.Fatalf("grid create: %v", err)
	}
	got, err := grepo.GetByOrigin(3, "ml-attr-1")
	if err != nil || got == nil || got.ID != grec.ID {
		t.Fatalf("grid GetByOrigin: got=%+v err=%v", got, err)
	}
	got.Name = "改名"
	if err := grepo.Update(got); err != nil {
		t.Fatalf("grid update: %v", err)
	}
	again, err := grepo.GetByID(grec.ID)
	if err != nil || again == nil || again.OriginListingID != "ml-attr-1" || again.Name != "改名" {
		t.Fatalf("grid update must keep origin_listing_id: %+v err=%v", again, err)
	}

	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}
