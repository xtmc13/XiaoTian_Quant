package store

import (
	"path/filepath"
	"testing"
	"time"
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

// TestSocialProfitShareMigration 冒烟验证 0017 迁移：社交市场 + 分成相关表创建与幂等重跑。
func TestSocialProfitShareMigration(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "gateway.db"))
	t.Setenv("SECRET_KEY", "test-secret-key-social-profit-share")
	if err := InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer CloseDB()
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	for _, tbl := range []string{
		"xt_social_providers", "xt_social_subscriptions", "xt_social_copy_pnl",
		"xt_social_profit_snapshots", "xt_social_withdrawals",
	} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name); err != nil {
			t.Fatalf("table %s missing: %v", tbl, err)
		}
	}
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO xt_social_providers (user_id, name, profit_share_pct, fee_mode, apply_status, created_at, updated_at)
		VALUES (1, 'p', 20, 'profit_share', 'pending', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert xt_social_providers: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO xt_social_subscriptions (provider_id, follower_user_id, fee_mode, status, created_at)
		VALUES (1, 2, 'profit_share', 'active', ?)`, now); err != nil {
		t.Fatalf("insert xt_social_subscriptions: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO xt_social_copy_pnl (provider_id, follower_user_id, pnl, created_at)
		VALUES (1, 2, 100.5, ?)`, now); err != nil {
		t.Fatalf("insert xt_social_copy_pnl: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO xt_social_profit_snapshots (id, provider_id, follower_user_id, window_date, copied_pnl, share_pct, share_amount, status, locked_until, created_at)
		VALUES ('ps-1-2-2026-09-20', 1, 2, '2026-09-20', 100.5, 20, 20.1, 'pending', 0, ?)`, now); err != nil {
		t.Fatalf("insert xt_social_profit_snapshots: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO xt_social_withdrawals (id, provider_user_id, amount, chain, address, status, created_at)
		VALUES ('wd-1', 1, 20.1, 'trc20', 'TX', 'pending', ?)`, now); err != nil {
		t.Fatalf("insert xt_social_withdrawals: %v", err)
	}
	// 唯一键：同日同 follower 同 provider 不允许第二份快照。
	if _, err := db.Exec(`INSERT INTO xt_social_profit_snapshots (id, provider_id, follower_user_id, window_date, created_at)
		VALUES ('ps-dup', 1, 2, '2026-09-20', ?)`, now); err == nil {
		t.Fatalf("expected unique violation on (provider_id, follower_user_id, window_date)")
	}
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}
