"""
数据库连接管理 + 迁移
使用 aiosqlite 支持异步操作，单文件 SQLite
"""

import asyncio
import logging
from pathlib import Path
from typing import Optional

try:
    import aiosqlite
    HAS_AIOSQLITE = True
except ImportError:
    HAS_AIOSQLITE = False

logger = logging.getLogger("xtquant.db")

# Singleton database instance
_db_instance: Optional["Database"] = None
_db_lock = asyncio.Lock()


async def get_db(db_path: str = "./data/xtquant.db") -> "Database":
    """Get or create the singleton database instance."""
    global _db_instance
    if _db_instance is not None:
        return _db_instance
    async with _db_lock:
        if _db_instance is not None:
            return _db_instance
        _db_instance = Database(db_path)
        await _db_instance.connect()
        return _db_instance


async def init_db(db_path: str = "./data/xtquant.db"):
    """Initialize the database (call on startup)."""
    db = await get_db(db_path)
    logger.info("Database initialized")
    return db


async def close_db():
    """Close the database connection."""
    global _db_instance
    if _db_instance:
        await _db_instance.close()
        _db_instance = None


SCHEMA_SQL = """
CREATE TABLE IF NOT EXISTS orders (
    id TEXT PRIMARY KEY,
    client_order_id TEXT,
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL,
    order_type TEXT NOT NULL,
    price REAL NOT NULL DEFAULT 0,
    quantity REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'CREATED',
    filled_qty REAL NOT NULL DEFAULT 0,
    remaining_qty REAL NOT NULL DEFAULT 0,
    avg_fill_price REAL NOT NULL DEFAULT 0,
    fee REAL NOT NULL DEFAULT 0,
    fee_asset TEXT DEFAULT '',
    strategy TEXT DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    completed_at INTEGER DEFAULT 0,
    error_msg TEXT DEFAULT '',
    raw_json TEXT DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id TEXT NOT NULL,
    trade_id TEXT NOT NULL,
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL,
    price REAL NOT NULL,
    quantity REAL NOT NULL,
    fee REAL NOT NULL DEFAULT 0,
    fee_asset TEXT DEFAULT '',
    realized_pnl REAL DEFAULT 0,
    timestamp INTEGER NOT NULL,
    FOREIGN KEY (order_id) REFERENCES orders(id)
);

CREATE TABLE IF NOT EXISTS positions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL DEFAULT 'LONG',
    size REAL NOT NULL DEFAULT 0,
    entry_price REAL NOT NULL DEFAULT 0,
    mark_price REAL NOT NULL DEFAULT 0,
    unrealized_pnl REAL NOT NULL DEFAULT 0,
    realized_pnl REAL NOT NULL DEFAULT 0,
    leverage REAL NOT NULL DEFAULT 1.0,
    updated_at INTEGER NOT NULL,
    UNIQUE(exchange, symbol, side)
);

CREATE TABLE IF NOT EXISTS position_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    size REAL NOT NULL,
    entry_price REAL NOT NULL,
    mark_price REAL NOT NULL,
    unrealized_pnl REAL NOT NULL,
    total_equity REAL NOT NULL,
    timestamp INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange TEXT NOT NULL,
    asset TEXT NOT NULL,
    free REAL NOT NULL DEFAULT 0,
    locked REAL NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL,
    UNIQUE(exchange, asset)
);

CREATE TABLE IF NOT EXISTS account_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange TEXT NOT NULL,
    total_equity REAL NOT NULL,
    available_balance REAL NOT NULL,
    margin_used REAL NOT NULL DEFAULT 0,
    margin_ratio REAL DEFAULT 0,
    timestamp INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS signals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    strategy TEXT NOT NULL,
    symbol TEXT NOT NULL,
    signal_type TEXT NOT NULL,
    strength REAL NOT NULL DEFAULT 0,
    price REAL NOT NULL DEFAULT 0,
    metadata TEXT DEFAULT '{}',
    timestamp INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS risk_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    check_name TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'WARNING',
    symbol TEXT DEFAULT '',
    strategy TEXT DEFAULT '',
    detail TEXT NOT NULL,
    order_id TEXT DEFAULT '',
    timestamp INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS market_bars (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    interval TEXT NOT NULL,
    open_time INTEGER NOT NULL,
    open REAL NOT NULL,
    high REAL NOT NULL,
    low REAL NOT NULL,
    close REAL NOT NULL,
    volume REAL NOT NULL,
    quote_volume REAL DEFAULT 0,
    trade_count INTEGER DEFAULT 0,
    UNIQUE(exchange, symbol, interval, open_time)
);

CREATE INDEX IF NOT EXISTS idx_orders_exchange_symbol ON orders(exchange, symbol);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_strategy ON orders(strategy);
CREATE INDEX IF NOT EXISTS idx_orders_created ON orders(created_at);
CREATE INDEX IF NOT EXISTS idx_trades_order_id ON trades(order_id);
CREATE INDEX IF NOT EXISTS idx_trades_timestamp ON trades(timestamp);
CREATE INDEX IF NOT EXISTS idx_signals_timestamp ON signals(timestamp);
CREATE INDEX IF NOT EXISTS idx_risk_events_timestamp ON risk_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_market_bars_lookup ON market_bars(exchange, symbol, interval, open_time);
CREATE INDEX IF NOT EXISTS idx_position_snapshots_ts ON position_snapshots(timestamp);
CREATE INDEX IF NOT EXISTS idx_account_snapshots_ts ON account_snapshots(timestamp);

-- Strategy management tables
CREATE TABLE IF NOT EXISTS strategy_configs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'spot',
    strategy_type TEXT NOT NULL,
    coin TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'stopped',
    direction TEXT DEFAULT 'long',
    leverage REAL DEFAULT 1.0,
    config_json TEXT NOT NULL DEFAULT '{}',
    is_template INTEGER DEFAULT 0,
    template_name TEXT DEFAULT '',
    pnl REAL DEFAULT 0.0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    started_at INTEGER DEFAULT 0,
    stopped_at INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS strategy_trade_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    strategy_id TEXT NOT NULL,
    strategy_name TEXT NOT NULL,
    coin TEXT NOT NULL,
    event_type TEXT NOT NULL,
    direction TEXT DEFAULT '',
    price REAL NOT NULL,
    quantity REAL NOT NULL,
    pnl REAL DEFAULT 0.0,
    note TEXT DEFAULT '',
    timestamp INTEGER NOT NULL,
    FOREIGN KEY (strategy_id) REFERENCES strategy_configs(id)
);

CREATE TABLE IF NOT EXISTS strategy_run_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    strategy_id TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'INFO',
    message TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    FOREIGN KEY (strategy_id) REFERENCES strategy_configs(id)
);

CREATE TABLE IF NOT EXISTS strategy_global_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    profit_protection_enabled INTEGER DEFAULT 0,
    max_concurrent_orders INTEGER DEFAULT 5,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_strategy_configs_category ON strategy_configs(category);
CREATE INDEX IF NOT EXISTS idx_strategy_configs_status ON strategy_configs(status);
CREATE INDEX IF NOT EXISTS idx_strategy_configs_template ON strategy_configs(is_template);
CREATE INDEX IF NOT EXISTS idx_strategy_run_logs_sid ON strategy_run_logs(strategy_id);
CREATE INDEX IF NOT EXISTS idx_strategy_run_logs_ts ON strategy_run_logs(timestamp);
CREATE INDEX IF NOT EXISTS idx_strategy_trade_logs_sid ON strategy_trade_logs(strategy_id);

CREATE TABLE IF NOT EXISTS xt_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    nickname TEXT DEFAULT '',
    email TEXT DEFAULT '',
    role TEXT DEFAULT 'user' CHECK (role IN ('admin', 'manager', 'user', 'viewer')),
    token_version INTEGER DEFAULT 1,
    is_active INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS xt_portfolio_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    total_equity REAL NOT NULL,
    available_balance REAL DEFAULT 0,
    margin_used REAL DEFAULT 0,
    unrealized_pnl REAL DEFAULT 0,
    snapshot_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS xt_backtests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT DEFAULT '',
    symbol TEXT NOT NULL,
    strategy_type TEXT DEFAULT '',
    params TEXT DEFAULT '{}',
    metrics TEXT DEFAULT '{}',
    equity_curve TEXT DEFAULT '[]',
    trades TEXT DEFAULT '[]',
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS agent_tokens (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    token_name TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT '["read_market","run_backtest"]',
    paper_only INTEGER NOT NULL DEFAULT 1,
    rate_limit INTEGER NOT NULL DEFAULT 60,
    expires_at INTEGER DEFAULT 0,
    created_by TEXT DEFAULT '',
    created_at INTEGER NOT NULL,
    revoked INTEGER NOT NULL DEFAULT 0,
    revoked_at INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS agent_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_id TEXT DEFAULT '',
    token_name TEXT DEFAULT '',
    endpoint TEXT NOT NULL,
    method TEXT NOT NULL,
    params_summary TEXT DEFAULT '',
    status_code INTEGER NOT NULL,
    ip TEXT DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_tokens_hash ON agent_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_agent_audit_token ON agent_audit_log(token_id);

-- Default admin user (password: admin123 — CHANGE IMMEDIATELY)
INSERT OR IGNORE INTO xt_users (username, password_hash, nickname, role)
VALUES ('admin', 'sha256$xt_default$402e69d2eb7b30a4d73f1770b9f548531484fd8fc268aa065d470e4d215ea5a1', 'Administrator', 'admin');
"""


class Database:
    """SQLite 异步数据库管理器"""

    def __init__(self, db_path: str = "./data/xtquant.db"):
        self.db_path = Path(db_path)
        self._conn: Optional["aiosqlite.Connection"] = None
        self._lock = asyncio.Lock()

    @property
    def conn(self) -> "aiosqlite.Connection":
        if self._conn is None:
            raise RuntimeError("Database not connected. Call await db.connect() first.")
        return self._conn

    async def connect(self):
        if not HAS_AIOSQLITE:
            raise ImportError("需要安装 aiosqlite: pip install aiosqlite")
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._conn = await aiosqlite.connect(str(self.db_path))
        self._conn.row_factory = aiosqlite.Row
        await self._conn.execute("PRAGMA journal_mode=WAL")
        await self._conn.execute("PRAGMA foreign_keys=ON")
        await self._migrate()
        logger.info(f"数据库已连接: {self.db_path}")

    async def _migrate(self):
        """执行SQL迁移"""
        async with self._lock:
            for stmt in SCHEMA_SQL.split(";"):
                stmt = stmt.strip()
                if stmt:
                    await self._conn.execute(stmt)
            await self._conn.commit()
        logger.info("数据库迁移完成")

    async def execute(self, sql: str, *params) -> "aiosqlite.Cursor":
        async with self._lock:
            # Unwrap single-tuple arg: support both execute(sql, a, b) and execute(sql, (a, b))
            if len(params) == 1 and isinstance(params[0], tuple):
                params = params[0]
            return await self._conn.execute(sql, params)

    async def execute_many(self, sql: str, params_list: list):
        async with self._lock:
            await self._conn.executemany(sql, params_list)

    async def fetch_one(self, sql: str, *params) -> Optional[dict]:
        async with self._lock:
            if len(params) == 1 and isinstance(params[0], tuple):
                params = params[0]
            cursor = await self._conn.execute(sql, params)
            row = await cursor.fetchone()
            return dict(row) if row else None

    async def fetch_all(self, sql: str, *params) -> list[dict]:
        async with self._lock:
            if len(params) == 1 and isinstance(params[0], tuple):
                params = params[0]
            cursor = await self._conn.execute(sql, params)
            rows = await cursor.fetchall()
            return [dict(r) for r in rows]

    async def insert(self, table: str, data: dict) -> int:
        """插入一行并返回 rowid"""
        columns = ", ".join(data.keys())
        placeholders = ", ".join("?" for _ in data)
        sql = f"INSERT OR REPLACE INTO {table} ({columns}) VALUES ({placeholders})"
        async with self._lock:
            cursor = await self._conn.execute(sql, tuple(data.values()))
            await self._conn.commit()
            return cursor.lastrowid

    async def update(self, table: str, data: dict, where: str, *where_params):
        """更新行"""
        sets = ", ".join(f"{k} = ?" for k in data)
        sql = f"UPDATE {table} SET {sets} WHERE {where}"
        params = tuple(data.values()) + where_params
        async with self._lock:
            await self._conn.execute(sql, params)
            await self._conn.commit()

    async def close(self):
        if self._conn:
            await self._conn.close()
            self._conn = None
            logger.info("数据库已关闭")
