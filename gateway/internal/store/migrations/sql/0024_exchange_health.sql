-- 0024: 交易所体检结果（对标 freqtrade check_exchange 的一键体检工具）
-- 日期: 2026-09-25
--
-- xt_exchange_health_checks 每次体检落地一行：
--   exchange     交易所规范名（binance/okx/bybit/kraken/coinbase/gate/mexc/bitget/alpaca/ibkr）
--   job_id       触发本次体检的异步 job id（handler 内存 job，重启即失效，仅供轮询关联）
--   overall      汇总等级 healthy/degraded/unhealthy/not_configured
--   configured   1=已配置 API 凭证（L2/L3 实际执行） 0=未配置（L2/L3 记 skip）
--   duration_ms  本次体检总耗时
--   result_json  完整报告 JSON（分级/分项 status/duration_ms/error，已脱敏，绝不含密钥）
--   created_at   毫秒时间戳
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_exchange_health_checks (
    id TEXT PRIMARY KEY,
    exchange TEXT NOT NULL,
    job_id TEXT NOT NULL DEFAULT '',
    overall TEXT NOT NULL DEFAULT '',
    configured INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    result_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_exchange_health_ex_created ON xt_exchange_health_checks(exchange, created_at);

CREATE INDEX IF NOT EXISTS idx_exchange_health_created ON xt_exchange_health_checks(created_at);
