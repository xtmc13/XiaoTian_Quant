-- 0011: A8 对账体系 + C4.1 billing 核验快照 + A2.2 成交执行阶段标记
-- 日期: 2026-09-19
--
-- 背景: 对标 QuantDinger 的实盘工程化（live guard 体系）。
--   reconcile_diffs      持仓/资金费对账差异（漂移检测落表 + 人工 resolve）
--   reconcile_deviations 实盘偏差监控（滑点 / 长时间未成交），订单+类型唯一去重
--   reconcile_funding    交易所资金费流水本地镜像（按唯一键去重，差异只报一次）
--   reconcile_settings   对账任务配置覆盖（键值，env 之外的持久化配置）
--   billing_verification billing 订单最近一次链上核验快照（GET /verification 数据源）
--   trades.exec_phase    成交所属执行阶段（limit/market/recovered），A2.2 区分补单两部分
--
-- 语义约定:
--   reconcile_diffs.status: open → resolved（resolution=accept_exchange|accept_local|ignore）
--   reconcile_deviations.kind: slippage（滑点超阈） | stuck（超时未成交）
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

-- ── 对账差异 ──
CREATE TABLE IF NOT EXISTS reconcile_diffs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER DEFAULT 0,
	exchange TEXT NOT NULL,
	symbol TEXT NOT NULL,
	diff_type TEXT NOT NULL,
	local_qty REAL DEFAULT 0,
	exchange_qty REAL DEFAULT 0,
	local_entry_price REAL DEFAULT 0,
	exchange_entry_price REAL DEFAULT 0,
	asset TEXT DEFAULT '',
	amount REAL DEFAULT 0,
	detail TEXT DEFAULT '',
	status TEXT DEFAULT 'open',
	resolution TEXT DEFAULT '',
	resolved_by TEXT DEFAULT '',
	created_at INTEGER NOT NULL,
	resolved_at INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_reconcile_diffs_status ON reconcile_diffs(status, created_at);
CREATE INDEX IF NOT EXISTS idx_reconcile_diffs_user ON reconcile_diffs(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_reconcile_diffs_exch ON reconcile_diffs(exchange, symbol);

-- ── 实盘偏差 ──
CREATE TABLE IF NOT EXISTS reconcile_deviations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	order_id TEXT NOT NULL,
	user_id INTEGER DEFAULT 0,
	symbol TEXT NOT NULL,
	exchange TEXT DEFAULT '',
	kind TEXT NOT NULL,
	expected_price REAL DEFAULT 0,
	avg_price REAL DEFAULT 0,
	slippage_pct REAL DEFAULT 0,
	detail TEXT DEFAULT '',
	status TEXT DEFAULT 'open',
	resolved_by TEXT DEFAULT '',
	created_at INTEGER NOT NULL,
	resolved_at INTEGER DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_reconcile_dev_ord_kind ON reconcile_deviations(order_id, kind);
CREATE INDEX IF NOT EXISTS idx_reconcile_dev_status ON reconcile_deviations(status, created_at);
CREATE INDEX IF NOT EXISTS idx_reconcile_dev_user ON reconcile_deviations(user_id, created_at);

-- ── 资金费流水本地镜像 ──
CREATE TABLE IF NOT EXISTS reconcile_funding (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	exchange TEXT NOT NULL,
	symbol TEXT NOT NULL,
	income_type TEXT NOT NULL,
	asset TEXT DEFAULT '',
	amount REAL NOT NULL,
	income_time INTEGER NOT NULL,
	extra TEXT DEFAULT '',
	notified INTEGER DEFAULT 0,
	created_at INTEGER NOT NULL,
	UNIQUE(exchange, symbol, income_type, income_time, amount)
);

CREATE INDEX IF NOT EXISTS idx_reconcile_funding_time ON reconcile_funding(exchange, income_time);

-- ── 对账配置覆盖（键值）──
CREATE TABLE IF NOT EXISTS reconcile_settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);

-- ── 自动修正审计日志 ──
CREATE TABLE IF NOT EXISTS reconcile_audit (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	action TEXT NOT NULL,
	exchange TEXT DEFAULT '',
	symbol TEXT DEFAULT '',
	user_id INTEGER DEFAULT 0,
	detail TEXT DEFAULT '',
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reconcile_audit_time ON reconcile_audit(created_at);

-- ── billing 核验快照（每订单一行，upsert）──
CREATE TABLE IF NOT EXISTS billing_verification (
	order_id TEXT PRIMARY KEY,
	chain TEXT DEFAULT '',
	tx_hash TEXT DEFAULT '',
	found INTEGER DEFAULT 0,
	valid INTEGER DEFAULT 0,
	confirmed INTEGER DEFAULT 0,
	confirmations INTEGER DEFAULT 0,
	required_confirmations INTEGER DEFAULT 0,
	block_number INTEGER DEFAULT 0,
	received_micro INTEGER DEFAULT 0,
	expected_micro INTEGER DEFAULT 0,
	fail_reason TEXT DEFAULT '',
	checked_at INTEGER NOT NULL
);

-- ── 成交执行阶段（A2.2 limit-then-market 区分两部分）──
ALTER TABLE trades ADD COLUMN exec_phase TEXT DEFAULT '';
