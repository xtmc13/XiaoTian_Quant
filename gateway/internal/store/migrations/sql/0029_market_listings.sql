-- 0029: 机器人/信号市场上架准入机制（对标 CryptoRobotics 创作者市场）
-- 日期: 2026-09-25
--
-- xt_market_listings 市场条目上架生命周期状态机：
--   status         draft → probation → pending_review → listed / rejected → delisted
--   bot_instance_id 考核期关联的 paper/live AI 机器人实例（统计口径数据源）
--   rule_*         提交考核时的规则快照（防全局规则改动溯及在审条目）
--   kind           robot | signal（indicator 预留）
--
-- xt_market_listing_stats 标准化统计日快照（卡片 = 最新一行）：
--   口径统一走 backtest.MetricsFromEquity（Sharpe/回撤/胜率/盈亏比与回测一致）
--
-- xt_market_settings 考核规则键值（min_days/min_trades/max_drawdown_pct），
--   管理员可调（演示可缩短 min_days）。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_market_listings (
    id TEXT PRIMARY KEY,
    author_user_id INTEGER NOT NULL DEFAULT 0,
    bot_instance_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'robot',
    name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    fee_model TEXT NOT NULL DEFAULT 'free',
    fee_percent REAL NOT NULL DEFAULT 0,
    monthly_fee REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'draft',
    reject_reason TEXT NOT NULL DEFAULT '',
    probation_started_at INTEGER NOT NULL DEFAULT 0,
    rule_min_days INTEGER NOT NULL DEFAULT 30,
    rule_min_trades INTEGER NOT NULL DEFAULT 10,
    rule_max_drawdown_pct REAL NOT NULL DEFAULT 50,
    reviewed_by INTEGER NOT NULL DEFAULT 0,
    reviewed_at INTEGER NOT NULL DEFAULT 0,
    listed_at INTEGER NOT NULL DEFAULT 0,
    delist_reason TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_market_listings_author ON xt_market_listings(author_user_id, updated_at);

CREATE INDEX IF NOT EXISTS idx_market_listings_status ON xt_market_listings(status, updated_at);

CREATE TABLE IF NOT EXISTS xt_market_listing_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    listing_id TEXT NOT NULL,
    date TEXT NOT NULL,
    total_return_pct REAL NOT NULL DEFAULT 0,
    annualized_return_pct REAL NOT NULL DEFAULT 0,
    max_drawdown_pct REAL NOT NULL DEFAULT 0,
    win_rate REAL NOT NULL DEFAULT 0,
    profit_factor REAL NOT NULL DEFAULT 0,
    sharpe_ratio REAL NOT NULL DEFAULT 0,
    total_trades INTEGER NOT NULL DEFAULT 0,
    monthly_return_pct REAL NOT NULL DEFAULT 0,
    followers INTEGER NOT NULL DEFAULT 0,
    running_days INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    UNIQUE(listing_id, date)
);

CREATE INDEX IF NOT EXISTS idx_market_listing_stats_listing ON xt_market_listing_stats(listing_id, date);

CREATE TABLE IF NOT EXISTS xt_market_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL
);
