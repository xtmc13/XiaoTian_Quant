-- 0013: A6.2 组合回测（对标 QuantDinger 组合级研究能力）
-- 日期: 2026-09-19
--
-- xt_portfolio_backtests 组合回测运行记录：
--   config_json  legs 配置（strategy_type/symbol/weight/params）+ timeframe + 区间 + 再平衡方式
--   result_json  组合级指标（复用 stats.go 口径）、各 leg 贡献、权重漂移记录、权益曲线
-- user_id 归属（多用户越权防护）。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_portfolio_backtests (
	id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL DEFAULT 0,
	name TEXT NOT NULL DEFAULT '',
	timeframe TEXT NOT NULL DEFAULT '1h',
	rebalance TEXT NOT NULL DEFAULT 'none',
	start_time INTEGER NOT NULL DEFAULT 0,
	end_time INTEGER NOT NULL DEFAULT 0,
	initial_capital REAL NOT NULL DEFAULT 0,
	final_equity REAL NOT NULL DEFAULT 0,
	total_return_pct REAL NOT NULL DEFAULT 0,
	max_drawdown_pct REAL NOT NULL DEFAULT 0,
	sharpe_ratio REAL NOT NULL DEFAULT 0,
	sortino_ratio REAL NOT NULL DEFAULT 0,
	calmar_ratio REAL NOT NULL DEFAULT 0,
	win_rate REAL NOT NULL DEFAULT 0,
	profit_factor REAL NOT NULL DEFAULT 0,
	total_trades INTEGER NOT NULL DEFAULT 0,
	legs_json TEXT NOT NULL DEFAULT '',
	result_json TEXT NOT NULL DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_portfolio_bt_user ON xt_portfolio_backtests(user_id, created_at);
