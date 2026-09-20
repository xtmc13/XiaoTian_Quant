package store

import "testing"

func TestPyStrategyRepoCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	repo := NewPyStrategyRepo()
	rec := &PyStrategyRecord{
		ID: "ps-1", UserID: 42, Name: "双均线", Symbol: "BTCUSDT",
		Interval: "15m", Direction: "long", ParamsJSON: `{"fast":9}`,
		Code: "STRATEGY_MANIFEST = {}", Version: 1, Status: PyStratStatusDraft,
		Paper: true, BotID: "",
		Market: PyStratMarketFutures, Leverage: 20, MarginMode: PyStratMarginIsolated,
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.ID == "" || rec.CreatedAt == 0 {
		t.Fatalf("create must fill id/timestamps: %+v", rec)
	}
	if rec.Status != PyStratStatusDraft {
		t.Fatalf("default status = %q", rec.Status)
	}

	got, err := repo.GetByID("ps-1")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.UserID != 42 || got.Name != "双均线" || !got.Paper || got.ParamsJSON != `{"fast":9}` {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	// 0023 v1.1 列回环（迁移后新增 market/leverage/margin_mode）
	if got.Market != PyStratMarketFutures || got.Leverage != 20 || got.MarginMode != PyStratMarginIsolated {
		t.Fatalf("v1.1 columns roundtrip mismatch: %+v", got)
	}

	// 未显式设置的记录应落库列默认值（spot/1/cross）
	def := &PyStrategyRecord{Name: "默认", Symbol: "ETHUSDT", Code: "x"}
	if err := repo.Create(def); err != nil {
		t.Fatalf("create default: %v", err)
	}
	defGot, _ := repo.GetByID(def.ID)
	if defGot.Market != PyStratMarketSpot || defGot.Leverage != 1 || defGot.MarginMode != PyStratMarginCross {
		t.Fatalf("v1.1 default columns mismatch: %+v", defGot)
	}
	if err := repo.Delete(def.ID); err != nil {
		t.Fatalf("cleanup default: %v", err)
	}

	got.Name = "改名"
	got.Paper = false
	got.Version = 2
	got.Leverage = 125
	got.MarginMode = PyStratMarginCross
	if err := repo.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := repo.GetByID("ps-1")
	if got2.Name != "改名" || got2.Paper || got2.Version != 2 {
		t.Fatalf("update mismatch: %+v", got2)
	}
	if got2.Leverage != 125 || got2.MarginMode != PyStratMarginCross {
		t.Fatalf("update v1.1 columns mismatch: %+v", got2)
	}

	if err := repo.UpdateStatus("ps-1", PyStratStatusActive, ""); err != nil {
		t.Fatalf("update status: %v", err)
	}
	got3, _ := repo.GetByID("ps-1")
	if got3.Status != PyStratStatusActive {
		t.Fatalf("status = %q", got3.Status)
	}
	if got3.UpdatedAt < got2.UpdatedAt {
		t.Fatal("updated_at must refresh")
	}

	list, err := repo.List(map[string]any{"user_id": 42}, 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("list by user: %v len=%d", err, len(list))
	}
	other, err := repo.List(map[string]any{"user_id": 99}, 0)
	if err != nil || len(other) != 0 {
		t.Fatalf("list other user: %v len=%d", err, len(other))
	}

	if err := repo.Delete("ps-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, _ := repo.GetByID("ps-1"); got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestPyStrategyRepoGetMissing(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	repo := NewPyStrategyRepo()
	got, err := repo.GetByID("nope")
	if err != nil || got != nil {
		t.Fatalf("expected (nil,nil), got (%v,%v)", got, err)
	}
}
