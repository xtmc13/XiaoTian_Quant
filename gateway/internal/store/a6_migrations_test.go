package store

import (
	"path/filepath"
	"testing"
)

// TestA6MigrationsApply 冒烟验证 0012/0013 迁移：表创建、写入与幂等重跑。
func TestA6MigrationsApply(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-for-migrations-smoke")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	for _, tbl := range []string{"factors_evaluations", "xt_portfolio_backtests"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name); err != nil {
			t.Fatalf("table %s missing: %v", tbl, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO factors_evaluations (user_id, kind, factor_name, factor_version, symbol, tf, forward_bars, samples, result_json, created_at) VALUES (1,'ic','roc',1,'BTCUSDT','1h',5,100,'{}',1)`); err != nil {
		t.Fatalf("insert factors_evaluations: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO xt_portfolio_backtests (id, user_id, name, timeframe, rebalance, created_at) VALUES ('pb-1',1,'t','1h','none',1)`); err != nil {
		t.Fatalf("insert xt_portfolio_backtests: %v", err)
	}
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}
