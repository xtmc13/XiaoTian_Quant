package data

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func initTickTestDB(t *testing.T) {
	db := store.GetDB()
	if db == nil {
		t.Fatal("db not initialized")
	}
	db.Exec("DELETE FROM ticks")
}

func TestTickStorageSaveAndLoad(t *testing.T) {
	initTickTestDB(t)
	ts := NewTickStorage()

	ticks := []model.Tick{
		{Symbol: "BTCUSDT", Bid: 42000, Ask: 42001, Last: 42000.5, Volume: 1.5, Timestamp: 1704067200000},
		{Symbol: "BTCUSDT", Bid: 42100, Ask: 42101, Last: 42100.5, Volume: 2.0, Timestamp: 1704067201000},
		{Symbol: "BTCUSDT", Bid: 42200, Ask: 42201, Last: 42200.5, Volume: 0.5, Timestamp: 1704067202000},
	}

	saved, err := ts.SaveTicks("BTCUSDT", ticks)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if saved != 3 {
		t.Errorf("expected 3 saved, got %d", saved)
	}

	loaded, err := ts.LoadTicks("BTCUSDT", 0, 0)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded) != 3 {
		t.Errorf("expected 3 loaded, got %d", len(loaded))
	}

	// Verify order
	if loaded[0].Timestamp != 1704067200000 {
		t.Errorf("expected first timestamp 1704067200000, got %d", loaded[0].Timestamp)
	}
	if loaded[2].Last != 42200.5 {
		t.Errorf("expected last price 42200.5, got %f", loaded[2].Last)
	}
}

func TestTickStorageLoadWithRange(t *testing.T) {
	initTickTestDB(t)
	ts := NewTickStorage()

	ticks := []model.Tick{
		{Symbol: "ETHUSDT", Bid: 2200, Ask: 2201, Last: 2200.5, Volume: 10, Timestamp: 1704067200000},
		{Symbol: "ETHUSDT", Bid: 2210, Ask: 2211, Last: 2210.5, Volume: 15, Timestamp: 1704067260000},
		{Symbol: "ETHUSDT", Bid: 2220, Ask: 2221, Last: 2220.5, Volume: 20, Timestamp: 1704067320000},
	}
	_, _ = ts.SaveTicks("ETHUSDT", ticks)

	loaded, err := ts.LoadTicks("ETHUSDT", 1704067260000, 1704067320000)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 ticks in range, got %d", len(loaded))
	}
}

func TestTickStorageGetTickCount(t *testing.T) {
	initTickTestDB(t)
	ts := NewTickStorage()

	if ts.GetTickCount("BTCUSDT") != 0 {
		t.Error("expected 0 count for empty symbol")
	}

	ticks := []model.Tick{
		{Symbol: "BTCUSDT", Bid: 100, Ask: 101, Last: 100.5, Volume: 1, Timestamp: time.Now().UnixMilli()},
	}
	_, _ = ts.SaveTicks("BTCUSDT", ticks)

	if ts.GetTickCount("BTCUSDT") != 1 {
		t.Errorf("expected count 1, got %d", ts.GetTickCount("BTCUSDT"))
	}
}

func TestTickStorageDeleteOldTicks(t *testing.T) {
	initTickTestDB(t)
	ts := NewTickStorage()

	now := time.Now().UnixMilli()
	ticks := []model.Tick{
		{Symbol: "BTCUSDT", Bid: 100, Ask: 101, Last: 100.5, Volume: 1, Timestamp: now - 86400000},
		{Symbol: "BTCUSDT", Bid: 101, Ask: 102, Last: 101.5, Volume: 1, Timestamp: now},
	}
	_, _ = ts.SaveTicks("BTCUSDT", ticks)

	deleted, err := ts.DeleteOldTicks("BTCUSDT", now-1000)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	count := ts.GetTickCount("BTCUSDT")
	if count != 1 {
		t.Errorf("expected count 1 after delete, got %d", count)
	}
}

func TestTickStorageGetTickInfo(t *testing.T) {
	initTickTestDB(t)
	ts := NewTickStorage()

	info := ts.GetTickInfo("BTCUSDT")
	if info.Symbol != "BTCUSDT" {
		t.Error("symbol mismatch")
	}
	if info.Count != 0 {
		t.Errorf("expected count 0, got %d", info.Count)
	}

	ticks := []model.Tick{
		{Symbol: "BTCUSDT", Bid: 100, Ask: 101, Last: 100.5, Volume: 1, Timestamp: 1704067200000},
		{Symbol: "BTCUSDT", Bid: 101, Ask: 102, Last: 101.5, Volume: 1, Timestamp: 1704153600000},
	}
	_, _ = ts.SaveTicks("BTCUSDT", ticks)

	info = ts.GetTickInfo("BTCUSDT")
	if info.Count != 2 {
		t.Errorf("expected count 2, got %d", info.Count)
	}
	if info.Earliest != 1704067200000 {
		t.Errorf("expected earliest 1704067200000, got %d", info.Earliest)
	}
	if info.Latest != 1704153600000 {
		t.Errorf("expected latest 1704153600000, got %d", info.Latest)
	}
}

func TestTickStorageUpsert(t *testing.T) {
	initTickTestDB(t)
	ts := NewTickStorage()

	ticks := []model.Tick{
		{Symbol: "BTCUSDT", Bid: 100, Ask: 101, Last: 100.5, Volume: 1, Timestamp: 1704067200000},
	}
	_, _ = ts.SaveTicks("BTCUSDT", ticks)

	// Upsert same timestamp with different price
	ticks2 := []model.Tick{
		{Symbol: "BTCUSDT", Bid: 200, Ask: 201, Last: 200.5, Volume: 2, Timestamp: 1704067200000},
	}
	_, _ = ts.SaveTicks("BTCUSDT", ticks2)

	loaded, _ := ts.LoadTicks("BTCUSDT", 0, 0)
	if len(loaded) != 1 {
		t.Fatalf("expected 1 tick after upsert, got %d", len(loaded))
	}
	if loaded[0].Last != 200.5 {
		t.Errorf("expected updated last 200.5, got %f", loaded[0].Last)
	}
}
