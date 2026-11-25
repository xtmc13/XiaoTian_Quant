package store

import (
	"testing"
	"time"
)

// TestA6MigrationsApply 冒烟验证 0012/0013 迁移：表创建、写入与幂等重跑。
func TestA6MigrationsApply(t *testing.T) {
	SetupTestDB(t)
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	for _, tbl := range []string{"factors_evaluations", "xt_portfolio_backtests"} {
		var name string
		if err := db.QueryRow(`SELECT table_name FROM information_schema.tables WHERE table_name=$1`, tbl).Scan(&name); err != nil {
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
	SetupTestDB(t)
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("run sql migrations: %v", err)
	}
	for _, tbl := range []string{
		"xt_social_providers", "xt_social_subscriptions", "xt_social_copy_pnl",
		"xt_social_profit_snapshots", "xt_social_withdrawals",
	} {
		var name string
		if err := db.QueryRow(`SELECT table_name FROM information_schema.tables WHERE table_name=$1`, tbl).Scan(&name); err != nil {
			t.Fatalf("table %s missing: %v", tbl, err)
		}
	}
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO xt_social_providers (user_id, name, profit_share_pct, fee_mode, apply_status, created_at, updated_at)
		VALUES (1, 'p', 20, 'profit_share', 'pending', $1, $2)`, now, now); err != nil {
		t.Fatalf("insert xt_social_providers: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO xt_social_subscriptions (provider_id, follower_user_id, fee_mode, status, created_at)
		VALUES (1, 2, 'profit_share', 'active', $1)`, now); err != nil {
		t.Fatalf("insert xt_social_subscriptions: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO xt_social_copy_pnl (provider_id, follower_user_id, pnl, created_at)
		VALUES (1, 2, 100.5, $1)`, now); err != nil {
		t.Fatalf("insert xt_social_copy_pnl: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO xt_social_profit_snapshots (id, provider_id, follower_user_id, window_date, copied_pnl, share_pct, share_amount, status, locked_until, created_at)
		VALUES ('ps-1-2-2026-09-20', 1, 2, '2026-09-20', 100.5, 20, 20.1, 'pending', 0, $1)`, now); err != nil {
		t.Fatalf("insert xt_social_profit_snapshots: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO xt_social_withdrawals (id, provider_user_id, amount, chain, address, status, created_at)
		VALUES ('wd-1', 1, 20.1, 'trc20', 'TX', 'pending', $1)`, now); err != nil {
		t.Fatalf("insert xt_social_withdrawals: %v", err)
	}
	// 唯一键：同日同 follower 同 provider 不允许第二份快照。
	if _, err := db.Exec(`INSERT INTO xt_social_profit_snapshots (id, provider_id, follower_user_id, window_date, created_at)
		VALUES ('ps-dup', 1, 2, '2026-09-20', $1)`, now); err == nil {
		t.Fatalf("expected unique violation on (provider_id, follower_user_id, window_date)")
	}
	if err := RunSQLMigrations(); err != nil {
		t.Fatalf("re-run sql migrations (idempotent): %v", err)
	}
}
