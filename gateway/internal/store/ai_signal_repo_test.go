package store

import (
	"path/filepath"
	"testing"
	"time"
)

func setupAISignalTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-ai-signal-repo")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(CloseDB)
}

func TestAISignalRepoInsertAndList(t *testing.T) {
	setupAISignalTestDB(t)
	repo := NewAISignalRepo()

	now := time.Now().Unix()
	rec := &AISignalRecord{
		UserID: 1, Symbol: "BTCUSDT", Signal: "long", Confidence: 72.5,
		Reason: "breakout", Filters: []string{"high_volatility:12>10"},
		MarketCondition: "volatile", Mode: "fast", Provider: "deepseek", CreatedAt: now,
	}
	if err := repo.Insert(rec); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if rec.ID == "" {
		t.Error("insert 应补 id")
	}

	list, err := repo.ListByUser(1, "", 0, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	got := list[0]
	if got.Signal != "long" || got.Confidence != 72.5 || got.Symbol != "BTCUSDT" {
		t.Errorf("got %+v", got)
	}
	if len(got.Filters) != 1 || got.Filters[0] != "high_volatility:12>10" {
		t.Errorf("filters 反序列化失败: %v", got.Filters)
	}
}

func TestAISignalRepoListFilters(t *testing.T) {
	setupAISignalTestDB(t)
	repo := NewAISignalRepo()
	now := time.Now().Unix()

	for i, sym := range []string{"BTCUSDT", "ETHUSDT", "BTCUSDT"} {
		rec := &AISignalRecord{
			UserID: 2, Symbol: sym, Signal: "long", Confidence: 60,
			CreatedAt: now - int64(i*100),
		}
		if err := repo.Insert(rec); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	// symbol 过滤
	list, err := repo.ListByUser(2, "BTCUSDT", 0, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("symbol filter: len=%d err=%v", len(list), err)
	}
	// since 过滤（只留最新一条）
	list, err = repo.ListByUser(2, "", now-50, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("since filter: len=%d err=%v", len(list), err)
	}
	// limit
	list, err = repo.ListByUser(2, "", 0, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("limit: len=%d err=%v", len(list), err)
	}
	// 时间倒序
	list, _ = repo.ListByUser(2, "", 0, 10)
	if list[0].CreatedAt < list[1].CreatedAt {
		t.Error("应按 created_at 倒序")
	}
}

func TestAISignalRepoLatestByUserSymbol(t *testing.T) {
	setupAISignalTestDB(t)
	repo := NewAISignalRepo()
	now := time.Now().Unix()

	repo.Insert(&AISignalRecord{UserID: 3, Symbol: "SOLUSDT", Signal: "long", Confidence: 55, CreatedAt: now - 100})
	repo.Insert(&AISignalRecord{UserID: 3, Symbol: "SOLUSDT", Signal: "short", Confidence: 80, CreatedAt: now})

	latest, err := repo.LatestByUserSymbol(3, "SOLUSDT")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest == nil || latest.Signal != "short" || latest.Confidence != 80 {
		t.Errorf("latest = %+v, want short/80", latest)
	}

	// 无记录 → (nil, nil)
	latest, err = repo.LatestByUserSymbol(3, "DOGEUSDT")
	if err != nil || latest != nil {
		t.Errorf("空结果应 (nil,nil), got %v/%v", latest, err)
	}
	// 用户隔离
	latest, _ = repo.LatestByUserSymbol(4, "SOLUSDT")
	if latest != nil {
		t.Errorf("不应读到他人信号: %+v", latest)
	}
}

func TestAISignalRepoCountByUserSince(t *testing.T) {
	setupAISignalTestDB(t)
	repo := NewAISignalRepo()
	now := time.Now().Unix()
	repo.Insert(&AISignalRecord{UserID: 5, Symbol: "BTCUSDT", Signal: "long", CreatedAt: now - 100})
	repo.Insert(&AISignalRecord{UserID: 5, Symbol: "ETHUSDT", Signal: "long", CreatedAt: now})

	n, err := repo.CountByUserSince(5, "", now-50)
	if err != nil || n != 1 {
		t.Errorf("count all = %d/%v, want 1", n, err)
	}
	n, _ = repo.CountByUserSince(5, "BTCUSDT", 0)
	if n != 1 {
		t.Errorf("count symbol = %d, want 1", n)
	}
	n, _ = repo.CountByUserSince(5, "", 0)
	if n != 2 {
		t.Errorf("count total = %d, want 2", n)
	}
}

func TestAISignalRepoNoDBSafe(t *testing.T) {
	repo := NewAISignalRepo()
	// 无 DB（db 为 nil）时不 panic、返回错误。
	if err := repo.Insert(&AISignalRecord{}); err == nil {
		t.Error("无 DB 应报错")
	}
	if _, err := repo.ListByUser(1, "", 0, 10); err == nil {
		t.Error("无 DB 应报错")
	}
	if _, err := repo.LatestByUserSymbol(1, "BTCUSDT"); err == nil {
		t.Error("无 DB 应报错")
	}
}

func TestAISignalSchemaIdempotent(t *testing.T) {
	setupAISignalTestDB(t)
	for i := 0; i < 3; i++ {
		if err := EnsureAISignalsSchema(); err != nil {
			t.Fatalf("ensure %d: %v", i, err)
		}
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='xt_ai_signals'`).Scan(&name); err != nil {
		t.Fatalf("table missing: %v", err)
	}
}

// ── AI 机器人配置 repo ─────────────────────────────────────────

func TestAIRobotConfigRepoRoundTrip(t *testing.T) {
	setupAISignalTestDB(t)
	repo := NewAIRobotConfigRepo()

	// 未保存过 → nil
	cfg, err := repo.Get(1)
	if err != nil || cfg != nil {
		t.Fatalf("空配置应 (nil,nil), got %v/%v", cfg, err)
	}

	want := map[string]any{
		"provider":              "deepseek",
		"scan_interval_seconds": 300.0,
		"confidence_threshold":  60.0,
		"enabled":               true,
		"watchlist":             []any{"BTCUSDT", "ETHUSDT"},
		"mode":                  "deep",
	}
	if err := repo.Save(1, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.Get(1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got["provider"] != "deepseek" || got["mode"] != "deep" {
		t.Errorf("roundtrip 失败: %v", got)
	}
	if wl, ok := got["watchlist"].([]any); !ok || len(wl) != 2 {
		t.Errorf("watchlist 反序列化失败: %v", got["watchlist"])
	}

	// upsert 覆盖
	want["mode"] = "fast"
	if err := repo.Save(1, want); err != nil {
		t.Fatalf("save2: %v", err)
	}
	got, _ = repo.Get(1)
	if got["mode"] != "fast" {
		t.Errorf("upsert 未生效: %v", got["mode"])
	}

	// 用户隔离
	other, _ := repo.Get(2)
	if other != nil {
		t.Errorf("用户 2 不应有配置: %v", other)
	}
}

func TestAIRobotConfigSchemaIdempotent(t *testing.T) {
	setupAISignalTestDB(t)
	for i := 0; i < 3; i++ {
		if err := EnsureAIRobotConfigSchema(); err != nil {
			t.Fatalf("ensure %d: %v", i, err)
		}
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='xt_ai_robot_configs'`).Scan(&name); err != nil {
		t.Fatalf("table missing: %v", err)
	}
}
