-- 0017: 社交跟单开放信号市场 + 利润分成（对标 CryptoRobotics 商业模式）
-- 日期: 2026-09-20
--
-- 背景：现有 social 模块仅 social_signals / copy_trades 两张表（schema.go V6，
-- 引擎 provider 为内存态），无 provider 入驻、订阅、盈亏归因的持久化。本迁移新增：
--   xt_social_providers        开放入驻的 signal provider（审核 + 收费模式）
--   xt_social_subscriptions    follower 对 provider 的订阅（分成结算的作用范围）
--   xt_social_copy_pnl         follower 归因到 provider 的已实现跟单盈亏（结算数据源）
--   xt_social_profit_snapshots 每日分成快照（pending→locked→payable→paid / void）
--   xt_social_withdrawals      provider 提现申请
--
-- fee_mode: monthly（仅月费）| profit_share（仅分成）| hybrid（月费+分成）
-- apply_status: pending | approved | rejected
-- 快照 status: pending | locked | payable | paid | void
-- 提现 status: pending | paid | rejected
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_social_providers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	monthly_fee REAL NOT NULL DEFAULT 0,
	profit_share_pct REAL,
	fee_mode TEXT NOT NULL DEFAULT 'profit_share',
	apply_status TEXT NOT NULL DEFAULT 'pending',
	apply_note TEXT NOT NULL DEFAULT '',
	approved_at INTEGER,
	is_public INTEGER NOT NULL DEFAULT 1,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_social_providers_user ON xt_social_providers(user_id, apply_status);
CREATE INDEX IF NOT EXISTS idx_social_providers_status ON xt_social_providers(apply_status);

CREATE TABLE IF NOT EXISTS xt_social_subscriptions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	provider_id INTEGER NOT NULL,
	follower_user_id INTEGER NOT NULL,
	fee_mode TEXT NOT NULL DEFAULT 'profit_share',
	status TEXT NOT NULL DEFAULT 'active',
	created_at INTEGER NOT NULL,
	cancelled_at INTEGER
);

CREATE INDEX IF NOT EXISTS idx_social_subs_provider ON xt_social_subscriptions(provider_id, status);
CREATE INDEX IF NOT EXISTS idx_social_subs_follower ON xt_social_subscriptions(follower_user_id, status);

CREATE TABLE IF NOT EXISTS xt_social_copy_pnl (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	provider_id INTEGER NOT NULL,
	follower_user_id INTEGER NOT NULL,
	pnl REAL NOT NULL DEFAULT 0,
	note TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_social_copy_pnl_attr ON xt_social_copy_pnl(provider_id, follower_user_id, created_at);

CREATE TABLE IF NOT EXISTS xt_social_profit_snapshots (
	id TEXT PRIMARY KEY,
	provider_id INTEGER NOT NULL,
	follower_user_id INTEGER NOT NULL,
	window_date TEXT NOT NULL,
	copied_pnl REAL NOT NULL DEFAULT 0,
	share_pct REAL NOT NULL DEFAULT 0,
	share_amount REAL NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'pending',
	locked_until INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_social_profit_snap_uq ON xt_social_profit_snapshots(provider_id, follower_user_id, window_date);
CREATE INDEX IF NOT EXISTS idx_social_profit_snap_status ON xt_social_profit_snapshots(status, locked_until);
CREATE INDEX IF NOT EXISTS idx_social_profit_snap_provider ON xt_social_profit_snapshots(provider_id, follower_user_id, window_date);

CREATE TABLE IF NOT EXISTS xt_social_withdrawals (
	id TEXT PRIMARY KEY,
	provider_user_id INTEGER NOT NULL,
	amount REAL NOT NULL DEFAULT 0,
	chain TEXT NOT NULL DEFAULT '',
	address TEXT NOT NULL DEFAULT '',
	tx_hash TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	admin_note TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	processed_at INTEGER
);

CREATE INDEX IF NOT EXISTS idx_social_withdrawals_user ON xt_social_withdrawals(provider_user_id, status);
CREATE INDEX IF NOT EXISTS idx_social_withdrawals_status ON xt_social_withdrawals(status);
