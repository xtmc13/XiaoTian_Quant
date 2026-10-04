package store

import "testing"

func TestComboConfigRepoCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewComboConfigRepo()
	rec := &ComboConfigRecord{
		ID:              "combo-1",
		UserID:          7,
		Name:            "三策略投票",
		Symbol:          "BTCUSDT",
		MembersJSON:     `[{"strategy_name":"martin","weight":0.5,"enabled":true}]`,
		AggregationMode: "vote",
		Status:          "stopped",
	}
	if err := repo.Create(rec); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.CreatedAt == 0 || rec.UpdatedAt == 0 {
		t.Fatalf("timestamps not assigned: %+v", rec)
	}

	got, err := repo.GetByID("combo-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected record")
	}
	if got.Name != "三策略投票" || got.Symbol != "BTCUSDT" || got.UserID != 7 {
		t.Errorf("unexpected record: %+v", got)
	}
	if got.MembersJSON != rec.MembersJSON {
		t.Errorf("members = %v", got.MembersJSON)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}

	rec.Name = "updated"
	rec.Status = "running"
	prevUpdated := rec.UpdatedAt
	if err := repo.Update(rec); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = repo.GetByID("combo-1")
	if got.Name != "updated" || got.Status != "running" {
		t.Errorf("after update: %+v", got)
	}
	if got.UpdatedAt < prevUpdated {
		t.Errorf("updated_at went backwards: %d < %d", got.UpdatedAt, prevUpdated)
	}

	if err := repo.Delete("combo-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = repo.GetByID("combo-1")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}
