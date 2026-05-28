-- XiaoTianQuant Database Initialization

-- Users table
CREATE TABLE IF NOT EXISTS xt_users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(64) UNIQUE NOT NULL,
    password_hash VARCHAR(256) NOT NULL,
    nickname VARCHAR(128),
    email VARCHAR(256),
    role VARCHAR(32) DEFAULT 'user' CHECK (role IN ('admin', 'manager', 'user', 'viewer')),
    token_version INTEGER DEFAULT 1,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- API keys table
CREATE TABLE IF NOT EXISTS xt_api_keys (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES xt_users(id) ON DELETE CASCADE,
    exchange VARCHAR(32) NOT NULL,
    api_key VARCHAR(256) NOT NULL,
    secret_key_encrypted VARCHAR(512) NOT NULL,
    passphrase_encrypted VARCHAR(512),
    is_testnet BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Strategies table
CREATE TABLE IF NOT EXISTS xt_strategies (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES xt_users(id) ON DELETE CASCADE,
    strategy_id VARCHAR(64) UNIQUE NOT NULL,
    name VARCHAR(256) NOT NULL,
    symbol VARCHAR(32) NOT NULL,
    strategy_type VARCHAR(32) NOT NULL,
    market_type VARCHAR(16) DEFAULT 'spot',
    status VARCHAR(32) DEFAULT 'stopped',
    config JSONB DEFAULT '{}',
    code TEXT,
    pnl REAL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Orders table
CREATE TABLE IF NOT EXISTS xt_orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES xt_users(id) ON DELETE CASCADE,
    strategy_id VARCHAR(64),
    exchange_order_id VARCHAR(128),
    symbol VARCHAR(32) NOT NULL,
    side VARCHAR(8) NOT NULL CHECK (side IN ('buy', 'sell')),
    order_type VARCHAR(16) DEFAULT 'limit',
    price REAL,
    quantity REAL NOT NULL,
    filled REAL DEFAULT 0,
    status VARCHAR(32) DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Trades table
CREATE TABLE IF NOT EXISTS xt_trades (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES xt_users(id) ON DELETE CASCADE,
    order_id INTEGER REFERENCES xt_orders(id) ON DELETE SET NULL,
    strategy_id VARCHAR(64),
    symbol VARCHAR(32) NOT NULL,
    side VARCHAR(8) NOT NULL,
    price REAL NOT NULL,
    quantity REAL NOT NULL,
    fee REAL DEFAULT 0,
    fee_asset VARCHAR(16),
    pnl REAL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Portfolio snapshots
CREATE TABLE IF NOT EXISTS xt_portfolio_snapshots (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES xt_users(id) ON DELETE CASCADE,
    total_equity REAL NOT NULL,
    available_balance REAL,
    margin_used REAL,
    unrealized_pnl REAL,
    snapshot_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Agent tokens
CREATE TABLE IF NOT EXISTS xt_agent_tokens (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES xt_users(id) ON DELETE CASCADE,
    token_hash VARCHAR(512) UNIQUE NOT NULL,
    name VARCHAR(128),
    scopes JSONB DEFAULT '[]',
    expires_at TIMESTAMP WITH TIME ZONE,
    last_used_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Market data cache
CREATE TABLE IF NOT EXISTS xt_market_cache (
    id SERIAL PRIMARY KEY,
    symbol VARCHAR(32) NOT NULL,
    interval VARCHAR(8) NOT NULL,
    kline_data JSONB NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(symbol, interval)
);

-- Backtest records
CREATE TABLE IF NOT EXISTS xt_backtests (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES xt_users(id) ON DELETE CASCADE,
    name VARCHAR(256),
    symbol VARCHAR(32) NOT NULL,
    strategy_type VARCHAR(32),
    params JSONB DEFAULT '{}',
    metrics JSONB DEFAULT '{}',
    equity_curve JSONB,
    trades JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Default admin user (password: admin123 — CHANGE IMMEDIATELY)
INSERT INTO xt_users (username, password_hash, nickname, role)
VALUES ('admin', '$2b$12$LJ3m4ys3GZfnYMz8kVsKaOTSxGHLfEhCSUcsBNPJmFWsCSJhKqPci', 'Administrator', 'admin')
ON CONFLICT (username) DO NOTHING;

-- Indexes
CREATE INDEX IF NOT EXISTS idx_orders_user ON xt_orders(user_id);
CREATE INDEX IF NOT EXISTS idx_orders_strategy ON xt_orders(strategy_id);
CREATE INDEX IF NOT EXISTS idx_orders_symbol ON xt_orders(symbol);
CREATE INDEX IF NOT EXISTS idx_trades_user ON xt_trades(user_id);
CREATE INDEX IF NOT EXISTS idx_trades_strategy ON xt_trades(strategy_id);
CREATE INDEX IF NOT EXISTS idx_trades_created ON xt_trades(created_at);
CREATE INDEX IF NOT EXISTS idx_strategies_user ON xt_strategies(user_id);
CREATE INDEX IF NOT EXISTS idx_strategies_status ON xt_strategies(status);
CREATE INDEX IF NOT EXISTS idx_portfolio_user ON xt_portfolio_snapshots(user_id);
