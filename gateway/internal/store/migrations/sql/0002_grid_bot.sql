-- 0002: 真实网格交易机器人（等差现货网格）数据表
-- 日期: 2026-09-16
--
-- 背景: 机器人市场第一个真商品，替代 handler/ai_bot_simulator.go 的
-- syntheticPrice 随机游走模拟器。行情驱动 + paper 撮合，
-- 纯逻辑引擎见 internal/grid，本文件只负责表结构。
-- grid_bots 为机器人主表（含当前持仓与挂单阶梯状态 state_json）；
-- grid_bot_trades 记录每格成交；grid_bot_snapshots 周期性权益快照。

CREATE TABLE IF NOT EXISTS grid_bots (
    id TEXT PRIMARY KEY,
    user_id INTEGER DEFAULT 0,
    name TEXT,
    symbol TEXT,
    lower_price REAL,
    upper_price REAL,
    grid_count INTEGER,
    investment REAL,
    status TEXT DEFAULT 'stopped',
    exchange TEXT DEFAULT 'paper',
    fee_rate REAL DEFAULT 0.001,
    realized_pnl REAL DEFAULT 0,
    total_trades INTEGER DEFAULT 0,
    base_qty REAL DEFAULT 0,
    quote_balance REAL DEFAULT 0,
    state_json TEXT DEFAULT '{}',
    initial_equity REAL DEFAULT 0,
    created_at INTEGER,
    updated_at INTEGER,
    started_at INTEGER DEFAULT 0,
    stopped_at INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_grid_bots_user_status ON grid_bots(user_id, status);
CREATE INDEX IF NOT EXISTS idx_grid_bots_symbol_status ON grid_bots(symbol, status);

CREATE TABLE IF NOT EXISTS grid_bot_trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bot_id TEXT,
    level_index INTEGER,
    side TEXT,
    price REAL,
    quantity REAL,
    quote_qty REAL,
    fee REAL,
    pnl REAL DEFAULT 0,
    ts INTEGER
);

CREATE INDEX IF NOT EXISTS idx_grid_bot_trades_bot_ts ON grid_bot_trades(bot_id, ts);

CREATE TABLE IF NOT EXISTS grid_bot_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bot_id TEXT,
    equity REAL,
    price REAL,
    realized_pnl REAL,
    open_orders INTEGER,
    ts INTEGER
);

CREATE INDEX IF NOT EXISTS idx_grid_bot_snapshots_bot_ts ON grid_bot_snapshots(bot_id, ts);
