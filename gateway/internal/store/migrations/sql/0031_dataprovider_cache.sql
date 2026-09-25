-- 0031: 外部数据生态缓存（对标 QuantDinger data_providers 缓存层）
-- 日期: 2026-09-25
--
-- xt_dataprovider_cache 统一 kv 缓存表，供 gateway/internal/dataprovider 各外部
-- 数据源（情绪/宏观/新闻/热力图/经济日历）的定时采集结果落库：
--   source      源 id（fear_greed / coinglass / fred / news / heatmap / calendar）
--   cache_key   源内细分键（当前恒为 'default'，预留如 news:BTC 维度）
--   payload     序列化后的 JSON 数据（绝不包含 API key）
--   fetched_at  采集成功时间（epoch 毫秒）
--   expires_at  兜底过期时间（epoch 毫秒，采集侧按 24×TTL 写入，供 stale 兜底）
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_dataprovider_cache (
    source TEXT NOT NULL,
    cache_key TEXT NOT NULL DEFAULT 'default',
    payload TEXT NOT NULL,
    fetched_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    PRIMARY KEY (source, cache_key)
);

CREATE INDEX IF NOT EXISTS idx_dataprovider_cache_expiry ON xt_dataprovider_cache(expires_at);
