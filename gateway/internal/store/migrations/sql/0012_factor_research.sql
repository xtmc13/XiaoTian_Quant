-- 0012: A6.1 因子研究框架（对标 QuantDinger 研究能力）
-- 日期: 2026-09-19
--
-- factors_evaluations 因子评价结果落库：
--   kind=ic      POST /api/factors/evaluate 的 IC/RankIC/ICIR 结果
--   kind=layers  POST /api/factors/layers 的分层回测结果
-- user_id 归属（多用户越权防护），params_json 记录评价时使用的因子参数。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS factors_evaluations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL DEFAULT 0,
	kind TEXT NOT NULL DEFAULT 'ic',
	factor_name TEXT NOT NULL,
	factor_version INTEGER NOT NULL DEFAULT 1,
	category TEXT DEFAULT '',
	symbol TEXT NOT NULL,
	tf TEXT NOT NULL DEFAULT '1h',
	forward_bars INTEGER NOT NULL DEFAULT 1,
	params_json TEXT DEFAULT '',
	samples INTEGER NOT NULL DEFAULT 0,
	result_json TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_factors_eval_user ON factors_evaluations(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_factors_eval_factor ON factors_evaluations(factor_name, factor_version, created_at);
CREATE INDEX IF NOT EXISTS idx_factors_eval_kind ON factors_evaluations(kind, created_at);
