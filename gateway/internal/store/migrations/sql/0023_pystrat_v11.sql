-- 0023: 用户 Python 策略契约化运行时 v1.1（合约杠杆执行）
-- 日期: 2026-09-20
--
-- xt_pystrategies 新增三列（合约执行声明，market='futures' 且 paper=0 时
-- 买入走 OMS 合约链路 market_type=swap；manifest risk.leverage/margin_mode
-- 为策略侧覆盖，优先级高于本表列）：
--   market       spot（默认，现货执行）| futures（合约执行）
--   leverage     合约杠杆倍数（1-125，spot 下无意义）
--   margin_mode  cross（默认全仓）| isolated（逐仓）
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

ALTER TABLE xt_pystrategies ADD COLUMN market TEXT NOT NULL DEFAULT 'spot';

ALTER TABLE xt_pystrategies ADD COLUMN leverage INTEGER NOT NULL DEFAULT 1;

ALTER TABLE xt_pystrategies ADD COLUMN margin_mode TEXT NOT NULL DEFAULT 'cross';
