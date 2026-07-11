package store

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	_ = os.Setenv("DB_PATH", filepath.Join(dir, "test.db"))
	_ = os.Setenv("SECRET_KEY", "test-secret-key-for-unit-tests")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	return func() {
		CloseDB()
	}
}

func TestStrategyConfigRepoCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewStrategyConfigRepo()
	rec := &StrategyConfigRecord{
		ID:           "cfg-1",
		UserID:       1,
		Name:         "马丁趋势",
		Category:     "spot",
		StrategyType: "martin_trend",
		Symbol:       "BTCUSDT",
		Coin:         "BTC",
		Direction:    "long",
		Leverage:     1,
		MarketType:   "spot",
		Status:       "stopped",
		ConfigJSON:   `{"first_order_amount":10}`,
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetByID("cfg-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected record")
	}
	if got.Name != "马丁趋势" {
		t.Errorf("name = %v", got.Name)
	}

	rec.Name = "updated"
	if err := repo.Update(rec); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = repo.GetByID("cfg-1")
	if got.Name != "updated" {
		t.Errorf("updated name = %v", got.Name)
	}

	items, err := repo.List(map[string]any{"user_id": int64(1), "category": "spot"}, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("list len = %d", len(items))
	}

	if err := repo.Delete("cfg-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = repo.GetByID("cfg-1")
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestStrategyTemplateRepoCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewStrategyTemplateRepo()
	rec := &StrategyTemplateRecord{
		UserID:            2,
		Name:              "合约模板",
		Category:          "contract",
		StrategyType:      "trend_long",
		Description:       "test",
		DefaultConfigJSON: `{"leverage":10}`,
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.ID == "" {
		t.Error("expected generated id")
	}

	items, err := repo.List(2, "contract", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("list len = %d", len(items))
	}

	deleted, err := repo.Delete(rec.ID, 2)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Error("expected delete true")
	}

	items, _ = repo.List(2, "contract", 0)
	if len(items) != 0 {
		t.Errorf("list after delete = %d", len(items))
	}
}
