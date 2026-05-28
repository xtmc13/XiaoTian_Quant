"""
PostgreSQL Database Adapter for XiaoTianQuant.
Provides connection pooling and backward-compatible cursor wrapper.

Uses asyncpg for async operations, keeping compatibility with the
existing aiosqlite-based Repository pattern.
"""

import os
import asyncio
import logging
from typing import Optional, Any, List, Dict, AsyncGenerator
from contextlib import asynccontextmanager

logger = logging.getLogger("xtquant.db.postgres")

HAS_ASYNCPG = False
try:
    import asyncpg
    HAS_ASYNCPG = True
except ImportError:
    logger.info("asyncpg not installed — PostgreSQL support disabled. pip install asyncpg")

# Connection pool (global singleton)
_pool: Optional[Any] = None
_pool_lock = asyncio.Lock()


def _get_database_url() -> str:
    return os.getenv("DATABASE_URL", "").strip()


def _is_postgres_url(url: str) -> bool:
    return url.startswith("postgresql://") or url.startswith("postgres://")


def _parse_postgres_url(url: str) -> Dict[str, Any]:
    """Parse postgresql://user:pass@host:port/dbname into connection kwargs."""
    url = url.replace("postgresql://", "").replace("postgres://", "")
    result = {"host": "localhost", "port": 5432, "user": "xtquant", "password": "", "database": "xtquant"}

    if "@" in url:
        auth, hostpart = url.rsplit("@", 1)
        if ":" in auth:
            result["user"], result["password"] = auth.split(":", 1)
        else:
            result["user"] = auth
    else:
        hostpart = url

    if "/" in hostpart:
        hostport, result["database"] = hostpart.split("/", 1)
    else:
        hostport = hostpart

    if ":" in hostport:
        result["host"], port_str = hostport.split(":", 1)
        result["port"] = int(port_str)

    return result


async def get_pool() -> Optional[Any]:  # asyncpg.Pool
    """Get or create the asyncpg connection pool."""
    global _pool

    if _pool is not None:
        return _pool

    if not HAS_ASYNCPG:
        return None

    db_url = _get_database_url()
    if not db_url or not _is_postgres_url(db_url):
        return None

    async with _pool_lock:
        if _pool is not None:
            return _pool

        params = _parse_postgres_url(db_url)
        try:
            _pool = await asyncpg.create_pool(
                host=params["host"],
                port=params["port"],
                user=params["user"],
                password=params["password"],
                database=params["database"],
                min_size=int(os.getenv("DB_POOL_MIN", "2")),
                max_size=int(os.getenv("DB_POOL_MAX", "20")),
                command_timeout=30,
            )
            logger.info(f"PostgreSQL pool created: {params['host']}:{params['port']}/{params['database']}")
        except Exception as e:
            logger.error(f"Failed to create PostgreSQL pool: {e}")
            return None

    return _pool


async def close_pool():
    """Close the connection pool (call on app shutdown)."""
    global _pool
    if _pool:
        await _pool.close()
        _pool = None
        logger.info("PostgreSQL pool closed")


@asynccontextmanager
async def get_pg_connection() -> AsyncGenerator[Any, None]:  # asyncpg.Connection
    """Context manager for acquiring a PostgreSQL connection from the pool."""
    pool = await get_pool()
    if not pool:
        raise RuntimeError("PostgreSQL pool unavailable")

    async with pool.acquire() as conn:
        yield conn


async def execute_sql(sql: str, *args) -> List[Dict[str, Any]]:
    """Execute SQL and return results as list of dicts."""
    pool = await get_pool()
    if not pool:
        raise RuntimeError("PostgreSQL unavailable")

    async with pool.acquire() as conn:
        # Convert ? placeholders to $1, $2, ... for asyncpg
        if "?" in sql:
            idx = [0]

            def replace_q(m):
                idx[0] += 1
                return f"${idx[0]}"

            import re
            sql = re.sub(r"\?", replace_q, sql)

        rows = await conn.fetch(sql, *args)
        return [dict(r) for r in rows]


async def execute_sql_sync(sql: str, *args) -> None:
    """Execute SQL without returning rows."""
    pool = await get_pool()
    if not pool:
        raise RuntimeError("PostgreSQL unavailable")

    async with pool.acquire() as conn:
        if "?" in sql:
            idx = [0]

            def replace_q(m):
                idx[0] += 1
                return f"${idx[0]}"

            import re
            sql = re.sub(r"\?", replace_q, sql)

        await conn.execute(sql, *args)


async def is_postgres_available() -> bool:
    """Check if PostgreSQL is configured and reachable."""
    pool = await get_pool()
    if not pool:
        return False
    try:
        async with pool.acquire() as conn:
            await conn.fetchval("SELECT 1")
        return True
    except Exception:
        return False
