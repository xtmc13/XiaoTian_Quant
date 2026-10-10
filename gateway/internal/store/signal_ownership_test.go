package store

import (
	"path/filepath"
	"testing"
	"time"
)

// TestSignalUserOwnershipMigration 冒烟验证 0057 迁移：xt_signals /
// xt_signal_executions 增加 user_id 列与索引，历史行默认 0，幂等重跑。
func TestSignalUserOwnershipMigration(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-signal-ownership")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	for _, tbl := range []string{"xt_signals", "xt_signal_executions"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(1) FROM pragma_table_info(?) WHERE name='user_id'`, tbl).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s missing user_id column: n=%d err=%v", tbl, n, err)
		}
	}
	for _, idx := range []string{"idx_signals_user", "idx_sigexec_user"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name); err != nil {
			t.Fatalf("index %s missing: %v", idx, err)
		}
	}
	// 历史行（不带 user_id 的 INSERT）默认落 0。
	if _, err := db.Exec(`INSERT INTO xt_signals (symbol, direction, strategy, status, created_at) VALUES ('LEGACYUSDT','LONG','legacy','PENDING',1)`); err != nil {
		t.Fatalf("insert legacy signal: %v", err)
	}
	var uid int
	if err := db.QueryRow(`SELECT user_id FROM xt_signals WHERE symbol='LEGACYUSDT'`).Scan(&uid); err != nil || uid != 0 {
		t.Fatalf("legacy row user_id: got %d err=%v, want 0", uid, err)
	}
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}

// TestSignalRepoUserIsolation 验证 user_id 列的写入回填与按用户读取隔离。
func TestSignalRepoUserIsolation(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-signal-repo-isolation")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()

	now := time.Now().UnixMilli()
	sigRepo := NewSignalRepo()
	for _, s := range []*SignalRecord{
		{Symbol: "BTCUSDT", Direction: "LONG", Status: "CLOSED", UserID: 11, CreatedAt: now},
		{Symbol: "ETHUSDT", Direction: "LONG", Status: "CLOSED", UserID: 11, CreatedAt: now},
		{Symbol: "SOLUSDT", Direction: "SHORT", Status: "CLOSED", UserID: 22, CreatedAt: now},
	} {
		if err := sigRepo.Create(s); err != nil {
			t.Fatalf("create signal: %v", err)
		}
	}
	execRepo := NewSignalExecutionRepo()
	for _, e := range []*SignalExecution{
		{SignalID: 1, Symbol: "BTCUSDT", Status: "closed", RealizedPnL: 10, UserID: 11, CreatedAt: now},
		{SignalID: 3, Symbol: "SOLUSDT", Status: "closed", RealizedPnL: -5, UserID: 22, CreatedAt: now},
	} {
		if err := execRepo.Create(e); err != nil {
			t.Fatalf("create execution: %v", err)
		}
	}

	// 读回 user_id 字段。
	got, err := sigRepo.GetByID("1")
	if err != nil || got.UserID != 11 {
		t.Fatalf("signal user_id roundtrip: got %+v err=%v", got, err)
	}

	// ListSinceByUser 严格按属主过滤。
	sigs11, err := sigRepo.ListSinceByUser(11, 0, 0)
	if err != nil || len(sigs11) != 2 {
		t.Fatalf("user 11 signals: n=%d err=%v, want 2", len(sigs11), err)
	}
	sigs22, err := sigRepo.ListSinceByUser(22, 0, 0)
	if err != nil || len(sigs22) != 1 || sigs22[0].Symbol != "SOLUSDT" {
		t.Fatalf("user 22 signals: %+v err=%v", sigs22, err)
	}
	execs11, err := execRepo.ListSinceByUser(11, 0, 0)
	if err != nil || len(execs11) != 1 || execs11[0].RealizedPnL != 10 {
		t.Fatalf("user 11 execs: %+v err=%v", execs11, err)
	}
	// userID<=0 退化为全量（admin 口径）。
	all, err := sigRepo.ListSinceByUser(0, 0, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("admin signals: n=%d err=%v, want 3", len(all), err)
	}

	// filter map 支持 user_id。
	filtered, err := sigRepo.List(map[string]any{"user_id": 22}, 10)
	if err != nil || len(filtered) != 1 {
		t.Fatalf("filter user_id=22: n=%d err=%v, want 1", len(filtered), err)
	}
	fexec, err := execRepo.List(map[string]any{"user_id": 22}, 10)
	if err != nil || len(fexec) != 1 {
		t.Fatalf("exec filter user_id=22: n=%d err=%v, want 1", len(fexec), err)
	}
}
