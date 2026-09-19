-- 0009: 分层马丁格尔机器人（对标 QuantDinger layered martingale）数据表
-- 日期: 2026-09-19
--
-- 背景: A1.3 多分配组马丁机器人——每个分组独立跑一条马丁阶梯（首单
-- quote_amount、层间 multiplier 倍投、max_layers 层数硬上限、budget_cap
-- 组预算硬限），只有成交回报确认后才推进到下一层；组内持仓整体止盈
-- （支持追踪止盈）/硬止损。引擎见 internal/lmartin，本文件只负责表结构。
-- layered_martin_bots 为机器人主表；layered_martin_groups 为每组运行状态；
-- layered_martin_orders 记录每笔买入/卖出成交。

CREATE TABLE IF NOT EXISTS layered_martin_bots (
    id TEXT PRIMARY KEY,
    user_id INTEGER DEFAULT 0,
    name TEXT,
    symbol TEXT,
    exchange TEXT DEFAULT 'paper',
    price_deviation_pct REAL DEFAULT 0.03,
    take_profit_pct REAL DEFAULT 0,
    stop_loss_pct REAL DEFAULT 0,
    trailing_enabled INTEGER DEFAULT 0,
    status TEXT DEFAULT 'stopped',
    realized_pnl REAL DEFAULT 0,
    total_trades INTEGER DEFAULT 0,
    created_at INTEGER,
    updated_at INTEGER,
    started_at INTEGER DEFAULT 0,
    stopped_at INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_layered_martin_bots_user_status ON layered_martin_bots(user_id, status);
CREATE INDEX IF NOT EXISTS idx_layered_martin_bots_symbol_status ON layered_martin_bots(symbol, status);

CREATE TABLE IF NOT EXISTS layered_martin_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bot_id TEXT,
    group_index INTEGER,
    quote_amount REAL,
    multiplier REAL DEFAULT 2,
    max_layers INTEGER DEFAULT 5,
    budget_cap REAL DEFAULT 0,
    layer INTEGER DEFAULT 0,
    total_invested REAL DEFAULT 0,
    base_qty REAL DEFAULT 0,
    avg_price REAL DEFAULT 0,
    entry_price REAL DEFAULT 0,
    highest_price REAL DEFAULT 0,
    pending_layer INTEGER DEFAULT 0,
    status TEXT DEFAULT 'idle',
    created_at INTEGER,
    updated_at INTEGER
);

CREATE INDEX IF NOT EXISTS idx_layered_martin_groups_bot ON layered_martin_groups(bot_id, group_index);

CREATE TABLE IF NOT EXISTS layered_martin_orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bot_id TEXT,
    group_index INTEGER,
    side TEXT,
    layer INTEGER DEFAULT 0,
    price REAL,
    quantity REAL,
    quote_qty REAL,
    reason TEXT DEFAULT '',
    ts INTEGER
);

CREATE INDEX IF NOT EXISTS idx_layered_martin_orders_bot_ts ON layered_martin_orders(bot_id, ts);
