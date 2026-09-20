-- 0021: 用户 Python 策略契约化运行时 v1（对标 QuantDinger Strategy API V2 / freqtrade IStrategy）
-- 日期: 2026-09-20
--
-- xt_pystrategies 用户 Python 策略：
--   code        策略源码全文（唯一事实来源，CRUD 直读直写）
--   version     当前源码版本号，快照本体复用 xt_strategy_versions 表
--               （strategy_id = 本表 id，payload 存整行 JSON 快照），与 0014 同一机制
--   params_json manifest 默认参数与运行时覆盖的合并值（保存时由前端/后端写入）
--   status      draft / active / paused / error
--   paper       1=模拟盘（强制 paper 账户） 0=实盘（启动前必须过实盘总闸）
--   bot_id      运行实例 id（预留：独立执行机器人实体，可空）
--   error       最近一次校验/运行错误（行号+消息）
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_pystrategies (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL DEFAULT 0,
    name TEXT NOT NULL DEFAULT '',
    symbol TEXT NOT NULL DEFAULT '',
    interval TEXT NOT NULL DEFAULT '15m',
    direction TEXT NOT NULL DEFAULT 'long',
    params_json TEXT NOT NULL DEFAULT '{}',
    code TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'draft',
    error TEXT NOT NULL DEFAULT '',
    paper INTEGER NOT NULL DEFAULT 1,
    bot_id TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pystrategies_user ON xt_pystrategies(user_id, updated_at);

CREATE INDEX IF NOT EXISTS idx_pystrategies_status ON xt_pystrategies(status);
