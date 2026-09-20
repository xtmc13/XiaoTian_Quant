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

	got.Name = "改名"
	got.Paper = false
	got.Version = 2
	if err := repo.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := repo.GetByID("ps-1")
	if got2.Name != "改名" || got2.Paper || got2.Version != 2 {
		t.Fatalf("update mismatch: %+v", got2)
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
