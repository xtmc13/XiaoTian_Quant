-- 0024: 信号机器人（SignalExecutor）真实化
-- 日期: 2026-09-26
--
-- 1) xt_signals 扩展：来源/保证金模式/阶梯止盈/盈亏落库
--    webhook 收到的信号此前不落库（全库无 SignalRepo 调用方），
--    本迁移补齐信号→执行→盈亏的完整数据链所需字段。
-- 2) xt_signal_executions：单条信号的执行全过程记录
--    （入场 + TP1/TP2/TP3 阶梯 + 止损 + 追踪 + 盈亏，对齐
--    THREE_BOTS_DESIGN.md 的 ExecutionRecord）。
-- 3) xt_signal_sources / xt_signal_source_subscriptions /
--    xt_signal_source_bills：信号源 + 订阅定价（free /
--    fixed_monthly / profit_share，对齐 ai_bot_catalog 的
--    fee_model 体系）+ 账单流水。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

ALTER TABLE xt_signals ADD COLUMN source_id TEXT NOT NULL DEFAULT '';

ALTER TABLE xt_signals ADD COLUMN margin_mode TEXT NOT NULL DEFAULT 'cross';

ALTER TABLE xt_signals ADD COLUMN tp1 DOUBLE PRECISION NOT NULL DEFAULT 0;

ALTER TABLE xt_signals ADD COLUMN tp2 DOUBLE PRECISION NOT NULL DEFAULT 0;

ALTER TABLE xt_signals ADD COLUMN tp3 DOUBLE PRECISION NOT NULL DEFAULT 0;

ALTER TABLE xt_signals ADD COLUMN tp1_pct DOUBLE PRECISION NOT NULL DEFAULT 0.4;

ALTER TABLE xt_signals ADD COLUMN tp2_pct DOUBLE PRECISION NOT NULL DEFAULT 0.3;

ALTER TABLE xt_signals ADD COLUMN tp3_pct DOUBLE PRECISION NOT NULL DEFAULT 0.3;

ALTER TABLE xt_signals ADD COLUMN realized_pnl DOUBLE PRECISION NOT NULL DEFAULT 0;

ALTER TABLE xt_signals ADD COLUMN closed_at BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS xt_signal_executions (
	id TEXT PRIMARY KEY,
	signal_id BIGINT NOT NULL,
	source_id TEXT NOT NULL DEFAULT '',
	symbol TEXT NOT NULL,
	direction TEXT NOT NULL,
	margin_mode TEXT NOT NULL DEFAULT 'cross',
	position_side TEXT NOT NULL DEFAULT '',
	position_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	entry_order_id TEXT NOT NULL DEFAULT '',
	entry_price DOUBLE PRECISION NOT NULL DEFAULT 0,
	entry_qty DOUBLE PRECISION NOT NULL DEFAULT 0,
	entry_time BIGINT NOT NULL DEFAULT 0,
	tp1_price DOUBLE PRECISION NOT NULL DEFAULT 0,
	tp1_qty DOUBLE PRECISION NOT NULL DEFAULT 0,
	tp1_time BIGINT NOT NULL DEFAULT 0,
	tp1_filled BIGINT NOT NULL DEFAULT 0,
	tp2_price DOUBLE PRECISION NOT NULL DEFAULT 0,
	tp2_qty DOUBLE PRECISION NOT NULL DEFAULT 0,
	tp2_time BIGINT NOT NULL DEFAULT 0,
	tp2_filled BIGINT NOT NULL DEFAULT 0,
	tp3_price DOUBLE PRECISION NOT NULL DEFAULT 0,
	tp3_qty DOUBLE PRECISION NOT NULL DEFAULT 0,
	tp3_time BIGINT NOT NULL DEFAULT 0,
	tp3_filled BIGINT NOT NULL DEFAULT 0,
	sl_price DOUBLE PRECISION NOT NULL DEFAULT 0,
	sl_triggered BIGINT NOT NULL DEFAULT 0,
	sl_time BIGINT NOT NULL DEFAULT 0,
	move_sl_after DOUBLE PRECISION NOT NULL DEFAULT 0,
	move_sl_to DOUBLE PRECISION NOT NULL DEFAULT 0,
	trailing_active BIGINT NOT NULL DEFAULT 0,
	trailing_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
	trailing_peak DOUBLE PRECISION NOT NULL DEFAULT 0,
	current_tp DOUBLE PRECISION NOT NULL DEFAULT 0,
	current_sl DOUBLE PRECISION NOT NULL DEFAULT 0,
	remaining_qty DOUBLE PRECISION NOT NULL DEFAULT 0,
	realized_pnl DOUBLE PRECISION NOT NULL DEFAULT 0,
	close_reason TEXT NOT NULL DEFAULT '',
	closed_at BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sigexec_signal ON xt_signal_executions(signal_id);

CREATE INDEX IF NOT EXISTS idx_sigexec_symbol ON xt_signal_executions(symbol);

CREATE INDEX IF NOT EXISTS idx_sigexec_status ON xt_signal_executions(status);

CREATE INDEX IF NOT EXISTS idx_sigexec_created ON xt_signal_executions(created_at);

CREATE TABLE IF NOT EXISTS xt_signal_sources (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	type TEXT NOT NULL DEFAULT 'webhook',
	owner_user_id BIGINT NOT NULL DEFAULT 0,
	enabled BIGINT NOT NULL DEFAULT 1,
	fee_model TEXT NOT NULL DEFAULT 'free',
	fee_percent DOUBLE PRECISION NOT NULL DEFAULT 0,
	monthly_fee DOUBLE PRECISION NOT NULL DEFAULT 0,
	tp_sl_json TEXT NOT NULL DEFAULT '{}',
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS xt_signal_source_subscriptions (
	id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	source_id TEXT NOT NULL,
	user_id BIGINT NOT NULL,
	fee_model TEXT NOT NULL DEFAULT 'free',
	fee_percent DOUBLE PRECISION NOT NULL DEFAULT 0,
	monthly_fee DOUBLE PRECISION NOT NULL DEFAULT 0,
	next_billing_at BIGINT NOT NULL DEFAULT 0,
	pending_share DOUBLE PRECISION NOT NULL DEFAULT 0,
	settled_share DOUBLE PRECISION NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'active',
	created_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sigsub_source ON xt_signal_source_subscriptions(source_id);

CREATE INDEX IF NOT EXISTS idx_sigsub_user ON xt_signal_source_subscriptions(user_id);

CREATE TABLE IF NOT EXISTS xt_signal_source_bills (
	id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	source_id TEXT NOT NULL,
	user_id BIGINT NOT NULL,
	subscription_id BIGINT NOT NULL DEFAULT 0,
	bill_type TEXT NOT NULL,
	amount DOUBLE PRECISION NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'pending',
	created_at BIGINT NOT NULL,
	settled_at BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sigbill_source ON xt_signal_source_bills(source_id);

CREATE INDEX IF NOT EXISTS idx_sigbill_user ON xt_signal_source_bills(user_id);
