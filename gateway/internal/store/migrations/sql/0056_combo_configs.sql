-- 0056: 组合策略（combo）配置持久化
-- 此前组合配置只存全局内存 map（strategy.comboRegistry），网关重启即丢；
-- 单策略 configs 早已走 strategy_configs 表，本表为组合补齐同款持久化。
-- schema 跟随 strategy.ComboConfig 字段，members 为 ComboMember 数组 JSON；
-- 时间戳 INTEGER unix 毫秒（项目纪律）；status 如实反映最后一次启停操作，
-- 运行态本身仍在策略引擎内存，不做开机自动恢复。

CREATE TABLE IF NOT EXISTS xt_combo_configs (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL DEFAULT 0,
    name TEXT NOT NULL,
    symbol TEXT NOT NULL,
    members TEXT NOT NULL DEFAULT '[]',
    aggregation_mode TEXT NOT NULL DEFAULT 'vote',
    status TEXT NOT NULL DEFAULT 'stopped',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_combo_configs_user ON xt_combo_configs(user_id, updated_at);
