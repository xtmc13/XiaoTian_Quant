-- 0016: A8.5 交易所回报 PnL 对账（对标 QuantDinger pnl_reconciliation）
-- 日期: 2026-09-20
--
-- 背景: 用交易所结算口径的已实现盈亏（Binance U 本位 GET /fapi/v1/income
-- incomeType=REALIZED_PNL）独立核对本地 trades 表 FIFO 口径已实现盈亏。
-- 窗口默认近 24h，diff_pct 超阈值（reconcile_settings 键 reported_pnl_pct，
-- 默认 1%）记 mismatch 并走 notify 告警；单所拉取失败记 error 行留痕，
-- 不中断整体对账轮。
--
-- 语义约定:
--   status: ok（一致） | mismatch（diff_pct 超阈） | error（交易所侧拉取失败）
--   user_id=0 表示全账户行：本系统每所一份全局凭证（凭证保险库 alias，即
--   交易所名），交易所回报口径为账户级，与 reconcile_diffs 无属主差异同惯例
--   credential_id 存交易所凭证标识（保险库 alias）
--   symbol 为空串表示全账户（全部合约），非空为单合约对账
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_reported_pnl_checks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER DEFAULT 0,
	credential_id TEXT NOT NULL DEFAULT '',
	exchange TEXT NOT NULL,
	symbol TEXT DEFAULT '',
	window_start INTEGER NOT NULL,
	window_end INTEGER NOT NULL,
	local_pnl REAL DEFAULT 0,
	reported_pnl REAL DEFAULT 0,
	diff REAL DEFAULT 0,
	diff_pct REAL DEFAULT 0,
	status TEXT DEFAULT 'ok',
	detail TEXT DEFAULT '',
	checked_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reported_pnl_status ON xt_reported_pnl_checks(status, checked_at);
CREATE INDEX IF NOT EXISTS idx_reported_pnl_user ON xt_reported_pnl_checks(user_id, checked_at);
CREATE INDEX IF NOT EXISTS idx_reported_pnl_exch ON xt_reported_pnl_checks(exchange, symbol, checked_at);
