"""
Redis Cache Layer for XiaoTianQuant.
Provides caching for market data, analysis results, and session state.
"""

import os
import json
import logging
from typing import Optional, Any

logger = logging.getLogger("xtquant.cache")

HAS_REDIS = False
try:
    import redis.asyncio as redis
    HAS_REDIS = True
except ImportError:
    try:
        import aioredis
        redis = aioredis
        HAS_REDIS = True
    except ImportError:
        logger.info("redis/aioredis not installed — caching disabled. pip install redis")

_client: Optional[Any] = None


def _is_enabled() -> bool:
    return os.getenv("CACHE_ENABLED", "false").lower() in ("true", "1", "yes")


async def get_client():
    """Get or create the Redis client."""
    global _client

    if not HAS_REDIS or not _is_enabled():
        return None

    if _client is not None:
        return _client

    host = os.getenv("REDIS_HOST", "localhost")
    port = int(os.getenv("REDIS_PORT", "6379"))

    try:
        _client = redis.Redis(
            host=host,
            port=port,
            db=0,
            decode_responses=True,
            socket_connect_timeout=3,
        )
        await _client.ping()
        logger.info(f"Redis connected: {host}:{port}")
    except Exception as e:
        logger.warning(f"Redis unavailable ({host}:{port}): {e}")
        _client = None

    return _client


async def close_client():
    """Close the Redis connection."""
    global _client
    if _client:
        await _client.close()
        _client = None


async def cache_get(key: str) -> Optional[Any]:
    """Get a value from cache. Returns None on miss."""
    client = await get_client()
    if not client:
        return None
    try:
        val = await client.get(f"xt:{key}")
        if val:
            return json.loads(val)
    except Exception:
        pass
    return None


async def cache_set(key: str, value: Any, ttl: int = 300):
    """Set a value in cache with TTL in seconds."""
    client = await get_client()
    if not client:
        return
    try:
        await client.setex(f"xt:{key}", ttl, json.dumps(value, default=str))
    except Exception:
        pass


async def cache_delete(key: str):
    """Delete a key from cache."""
    client = await get_client()
    if not client:
        return
    try:
        await client.delete(f"xt:{key}")
    except Exception:
        pass


async def cache_invalidate_pattern(pattern: str):
    """Invalidate all keys matching a pattern."""
    client = await get_client()
    if not client:
        return
    try:
        keys = await client.keys(f"xt:{pattern}")
        if keys:
            await client.delete(*keys)
    except Exception:
        pass
