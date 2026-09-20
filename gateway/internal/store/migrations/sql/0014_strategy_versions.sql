-- 0014: 策略配置版本化不可变快照（对标 QuantDinger strategy_v2 版本快照能力）
-- 日期: 2026-09-20
--
-- xt_strategy_versions 策略配置历史版本：
--   payload  完整配置快照（strategy_configs 记录的 JSON 视图，恢复时写回）
--   version  同策略内递增整数（Create 时取 max(version)+1）
--   (strategy_id, version) 唯一——历史版本不可变，恢复只追加新版本
-- user_id 冗余策略属主，便于列表过滤与越权校验。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_strategy_versions (
	id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL DEFAULT 0,
	strategy_id TEXT NOT NULL,
	version INTEGER NOT NULL DEFAULT 0,
	payload TEXT NOT NULL DEFAULT '',
	note TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_strategy_versions_strategy_version ON xt_strategy_versions(strategy_id, version);
