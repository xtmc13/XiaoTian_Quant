-- 0019: ML 自动滚动重训（对标 FreqAI live_retrain_hours）+ 预测落盘复用
-- 日期: 2026-09-20
--
-- 背景: 对标 freqtrade FreqAI 的两项能力。
--   xt_ml_retrain_jobs   重训任务表：到点触发 pipeline 重训，
--                        连续失败自动暂停（active=0），失败走 notify 告警
--   xt_ml_retrain_runs   重训运行记录（最近运行历史 + 连续失败计数来源）
--   xt_ml_predictions    预测缓存表：回测/hyperopt 复用模型推理结果，
--                        (model_name, symbol, bar_time, features_hash) 唯一去重
--   ml_settings          ML 配置覆盖键值表（env 之外的持久化配置，
--                        如预测保留天数 prediction_ttl_d）
--
-- 语义约定:
--   xt_ml_retrain_jobs.last_status: success | failed | ''（从未运行）
--   xt_ml_retrain_runs.status:      success | failed
--   features_hash: 特征向量稳定 hash（sha1 序列化取前 16 位 hex）
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

-- ── 重训任务 ──
CREATE TABLE IF NOT EXISTS xt_ml_retrain_jobs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER DEFAULT 0,
	model_name TEXT NOT NULL,
	feature_set TEXT DEFAULT '',
	interval_minutes INTEGER NOT NULL DEFAULT 1440,
	last_run_at INTEGER DEFAULT 0,
	last_status TEXT DEFAULT '',
	last_error TEXT DEFAULT '',
	created_at INTEGER NOT NULL,
	active INTEGER DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_ml_retrain_jobs_active ON xt_ml_retrain_jobs(active);
CREATE INDEX IF NOT EXISTS idx_ml_retrain_jobs_user ON xt_ml_retrain_jobs(user_id, created_at);

-- ── 重训运行记录 ──
CREATE TABLE IF NOT EXISTS xt_ml_retrain_runs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	job_id INTEGER NOT NULL,
	status TEXT NOT NULL,
	error TEXT DEFAULT '',
	duration_ms INTEGER DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ml_retrain_runs_job ON xt_ml_retrain_runs(job_id, created_at);

-- ── 预测缓存 ──
CREATE TABLE IF NOT EXISTS xt_ml_predictions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	job_id INTEGER DEFAULT 0,
	model_name TEXT NOT NULL,
	symbol TEXT NOT NULL,
	bar_time INTEGER NOT NULL,
	features_hash TEXT NOT NULL,
	prediction REAL NOT NULL,
	created_at INTEGER NOT NULL,
	UNIQUE(model_name, symbol, bar_time, features_hash)
);

CREATE INDEX IF NOT EXISTS idx_ml_predictions_created ON xt_ml_predictions(created_at);
CREATE INDEX IF NOT EXISTS idx_ml_predictions_model ON xt_ml_predictions(model_name, symbol);

-- ── ML 配置覆盖（键值）──
CREATE TABLE IF NOT EXISTS ml_settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);
