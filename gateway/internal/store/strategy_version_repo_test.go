package store

import (
	"testing"
)

func TestStrategyVersionRepoCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewStrategyVersionRepo()

	// 同策略连打三条快照：version 必须 1/2/3 递增。
	var versions []int
	for _, note := range []string{"初始版本", "调参后", "再调参"} {
		rec := &StrategyVersionRecord{
			UserID:     1,
			StrategyID: "cfg-v1",
			Payload:    `{"leverage":10}`,
			Note:       note,
		}
		if err := repo.Create(rec); err != nil {
			t.Fatalf("create %q: %v", note, err)
		}
		if rec.ID == "" {
			t.Fatalf("create %q: expected generated id", note)
		}
		versions = append(versions, rec.Version)
	}
	if versions[0] != 1 || versions[1] != 2 || versions[2] != 3 {
		t.Fatalf("versions = %v, want [1 2 3]", versions)
	}

	// 另一个策略的版本独立递增，不影响 cfg-v1。
	other := &StrategyVersionRecord{UserID: 1, StrategyID: "cfg-v2", Payload: `{}`}
	if err := repo.Create(other); err != nil {
		t.Fatalf("create other: %v", err)
	}
	if other.Version != 1 {
		t.Fatalf("other version = %d, want 1", other.Version)
	}

	// ListByStrategy：倒序、数量正确、互相隔离。
	items, err := repo.ListByStrategy("cfg-v1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("list len = %d, want 3", len(items))
	}
	if items[0].Version != 3 || items[2].Version != 1 {
		t.Fatalf("list order = %d,%d, want 3,1", items[0].Version, items[2].Version)
	}
	items, _ = repo.ListByStrategy("cfg-v2")
	if len(items) != 1 {
		t.Fatalf("cfg-v2 list len = %d, want 1", len(items))
	}

	// Get：命中与未命中。
	got, err := repo.Get("cfg-v1", 2)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.Note != "调参后" || got.Payload != `{"leverage":10}` {
		t.Fatalf("get v2 = %+v", got)
	}
	got, err = repo.Get("cfg-v1", 99)
	if err != nil {
		t.Fatalf("get missing: %v", err)
	}
	if got != nil {
		t.Fatalf("get missing = %+v, want nil", got)
	}

	// GetLatest / FindByID。
	latest, err := repo.GetLatest("cfg-v1")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest == nil || latest.Version != 3 {
		t.Fatalf("latest = %+v, want version 3", latest)
	}
	latest, err = repo.GetLatest("no-such-strategy")
	if err != nil {
		t.Fatalf("latest empty: %v", err)
	}
	if latest != nil {
		t.Fatalf("latest empty = %+v, want nil", latest)
	}
	byID, err := repo.FindByID(items[0].ID)
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if byID == nil || byID.StrategyID != "cfg-v2" {
		t.Fatalf("find by id = %+v", byID)
	}
	byID, err = repo.FindByID("no-such-id")
	if err != nil {
		t.Fatalf("find by id missing: %v", err)
	}
	if byID != nil {
		t.Fatalf("find by id missing = %+v, want nil", byID)
	}

	// 唯一约束：(strategy_id, version) 不允许重复（绕过 Create 的版本分配，
	// 直插冲突版本号验证 ux_strategy_versions_strategy_version）。
	if _, err := db.Exec(
		`INSERT INTO xt_strategy_versions (id, user_id, strategy_id, version, payload, note, created_at)
		 VALUES ('dup-1', 1, 'cfg-v1', 3, '{}', '', 1)`,
	); err == nil {
		t.Fatal("expected unique constraint error for duplicate (strategy_id, version)")
	}
}
