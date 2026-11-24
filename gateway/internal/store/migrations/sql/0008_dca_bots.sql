-- 0008: DCA 定投机器人（对标 QuantDinger DCA bot）数据表
-- 日期: 2026-09-19
--
-- 背景: A1.2 定投机器人——按 interval_minutes 定时买入固定 quote_amount 的
-- 现货（只做多），达 max_orders / period_budget 停止买入；持仓均价之上
-- take_profit_pct 整体止盈卖出，stop_loss_pct 硬止损，trailing_enabled 开启
-- 追踪止盈。引擎见 internal/dca，本文件只负责表结构。
-- dca_bots 为主表（含持仓与累计投入等运行时状态列）；dca_bot_orders 记录
-- 每笔买入/卖出成交。

CREATE TABLE IF NOT EXISTS dca_bots (
    id TEXT PRIMARY KEY,
    user_id BIGINT DEFAULT 0,
    name TEXT,
    symbol TEXT,
    exchange TEXT DEFAULT 'paper',
    quote_amount DOUBLE PRECISION,
    interval_minutes BIGINT,
    max_orders BIGINT DEFAULT 0,
    period_budget DOUBLE PRECISION DEFAULT 0,
    take_profit_pct DOUBLE PRECISION DEFAULT 0,
    stop_loss_pct DOUBLE PRECISION DEFAULT 0,
    trailing_enabled BIGINT DEFAULT 0,
    status TEXT DEFAULT 'stopped',
    filled_orders BIGINT DEFAULT 0,
    total_invested DOUBLE PRECISION DEFAULT 0,
    base_qty DOUBLE PRECISION DEFAULT 0,
    avg_price DOUBLE PRECISION DEFAULT 0,
    realized_pnl DOUBLE PRECISION DEFAULT 0,
    last_buy_at BIGINT DEFAULT 0,
    highest_price DOUBLE PRECISION DEFAULT 0,
    created_at BIGINT,
    updated_at BIGINT,
    started_at BIGINT DEFAULT 0,
    stopped_at BIGINT DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_dca_bots_user_status ON dca_bots(user_id, status);
CREATE INDEX IF NOT EXISTS idx_dca_bots_symbol_status ON dca_bots(symbol, status);

CREATE TABLE IF NOT EXISTS dca_bot_orders (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bot_id TEXT,
    side TEXT,
    price DOUBLE PRECISION,
    quantity DOUBLE PRECISION,
    quote_qty DOUBLE PRECISION,
    reason TEXT DEFAULT '',
    ts BIGINT
);

CREATE INDEX IF NOT EXISTS idx_dca_bot_orders_bot_ts ON dca_bot_orders(bot_id, ts);
