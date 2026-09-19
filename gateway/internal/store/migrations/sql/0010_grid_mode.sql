-- 0010: 网格机器人模式（long/short/neutral 中性对冲双网格）+ 合约参数 + 成交腿标识
-- 日期: 2026-09-19
--
-- 背景: A1.3 网格中性模式——对标 QuantDinger：long 保持现货做多网格现状；
-- short 为合约做空网格；neutral 为永续对冲双网格（多头腿+空头腿各自独立
-- 格子挂单与独立盈亏核算，整体净头寸在详情可查）。合约腿下单走现有 OMS
-- 合约链路（leverage/margin_mode），实盘仍受 canPlaceLiveOrder 安全闸约束。
-- mode 默认 'long' 兼容存量机器人；grid_bot_trades.leg 默认 'long' 兼容存量成交。

ALTER TABLE grid_bots ADD COLUMN mode TEXT NOT NULL DEFAULT 'long';
ALTER TABLE grid_bots ADD COLUMN leverage REAL NOT NULL DEFAULT 1;
ALTER TABLE grid_bots ADD COLUMN margin_mode TEXT NOT NULL DEFAULT 'cross';

ALTER TABLE grid_bot_trades ADD COLUMN leg TEXT NOT NULL DEFAULT 'long';
