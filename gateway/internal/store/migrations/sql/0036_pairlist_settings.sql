-- 0036: pairlist 配置持久化（P1：重启后保留用户配置的 producer/filter 链）
-- 与 reconcile_settings/ml_settings 同款的键值表，value 为配置 JSON。

CREATE TABLE IF NOT EXISTS pairlist_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);
