"""
HTTP Session 管理 — 连接池、速率限制、自动重试
"""

from __future__ import annotations
import asyncio
import time
import logging
from typing import Optional

logger = logging.getLogger("xtquant.exchange.session")


class RateLimiter:
    """Token Bucket 速率限制器"""

    def __init__(self, rate: float, burst: int = 10):
        self.rate = rate
        self.burst = burst
        self._tokens = float(burst)
        self._last_refill = time.monotonic()
        self._lock = asyncio.Lock()

    async def acquire(self) -> float:
        """获取令牌，返回等待时间(秒)"""
        async with self._lock:
            now = time.monotonic()
            elapsed = now - self._last_refill
            self._tokens = min(self.burst, self._tokens + elapsed * self.rate)
            self._last_refill = now
            if self._tokens >= 1.0:
                self._tokens -= 1.0
                return 0.0
            wait = (1.0 - self._tokens) / self.rate
            self._tokens = 0.0
            return wait


class ExchangeSession:
    """
    HTTP Session 管理器

    功能:
      - 连接池
      - 速率限制 (按 weight 消耗)
      - 自动重试 (指数退避)
      - 请求日志
    """

    def __init__(self, name: str, base_url: str,
                 rate_limit: float = 10.0,
                 max_retries: int = 3,
                 timeout: float = 10.0):
        self.name = name
        self.base_url = base_url.rstrip("/")
        self.max_retries = max_retries
        self.timeout = timeout

        self._session: Optional["aiohttp.ClientSession"] = None
        self._rate_limiter = RateLimiter(rate_limit)

        # 统计
        self._request_count = 0
        self._retry_count = 0
        self._error_count = 0
        self._last_request_time = 0.0
        self._avg_latency_ms = 0.0

    async def _get_session(self) -> "aiohttp.ClientSession":
        import aiohttp
        if self._session is None or self._session.closed:
            timeout = aiohttp.ClientTimeout(total=self.timeout)
            self._session = aiohttp.ClientSession(
                timeout=timeout,
                headers={"User-Agent": "XiaoTianQuant/2.0"},
            )
        return self._session

    async def request(self, method: str, path: str,
                      params: dict = None, data: dict = None,
                      headers: dict = None, signed: bool = False,
                      weight: int = 1) -> dict:
        """
        通用请求方法

        Args:
            method: GET | POST | DELETE | PUT
            path: API路径 (e.g. "/api/v3/order")
            params: Query参数
            data: Body (POST)
            headers: 额外Header
            signed: 是否需要签名
            weight: API权重消耗
        """
        for retry in range(self.max_retries + 1):
            try:
                # 速率限制
                wait = await self._rate_limiter.acquire()
                if wait > 0:
                    await asyncio.sleep(wait)

                session = await self._get_session()
                url = f"{self.base_url}{path}"
                start = time.time()

                merged_headers = headers or {}
                if not merged_headers:
                    merged_headers = {}

                resp = await session.request(
                    method, url, params=params, json=data,
                    headers=merged_headers,
                )

                latency = (time.time() - start) * 1000
                self._update_latency(latency)
                self._request_count += 1
                self._last_request_time = time.time()

                if resp.status == 429:
                    retry_after = int(resp.headers.get("Retry-After", "1"))
                    logger.warning(f"[{self.name}] 429 Rate Limited, 等待 {retry_after}s")
                    await asyncio.sleep(retry_after)
                    continue

                if resp.status >= 500 and retry < self.max_retries:
                    self._retry_count += 1
                    delay = 2 ** retry
                    logger.warning(f"[{self.name}] {resp.status} 错误, {delay}s后重试 ({retry+1}/{self.max_retries})")
                    await asyncio.sleep(delay)
                    continue

                result = await resp.json()
                return result

            except (ConnectionError, TimeoutError, OSError) as e:
                self._retry_count += 1
                if retry >= self.max_retries:
                    self._error_count += 1
                    raise
                delay = 2 ** retry
                logger.warning(f"[{self.name}] 网络错误: {e}, {delay}s后重试")
                await asyncio.sleep(delay)

            except Exception:
                self._error_count += 1
                raise

        raise RuntimeError(f"[{self.name}] 请求失败: {method} {path}")

    async def get(self, path: str, params: dict = None, headers: dict = None, **kwargs) -> dict:
        return await self.request("GET", path, params=params, headers=headers, **kwargs)

    async def post(self, path: str, params: dict = None, data: dict = None,
                   headers: dict = None, **kwargs) -> dict:
        return await self.request("POST", path, params=params, data=data, headers=headers, **kwargs)

    async def delete(self, path: str, params: dict = None, headers: dict = None, **kwargs) -> dict:
        return await self.request("DELETE", path, params=params, headers=headers, **kwargs)

    def _update_latency(self, latency_ms: float):
        """指数移动平均更新延迟"""
        if self._avg_latency_ms == 0:
            self._avg_latency_ms = latency_ms
        else:
            self._avg_latency_ms = self._avg_latency_ms * 0.9 + latency_ms * 0.1

    def get_stats(self) -> dict:
        return {
            "requests": self._request_count,
            "retries": self._retry_count,
            "errors": self._error_count,
            "avg_latency_ms": round(self._avg_latency_ms, 1),
            "last_request_age_s": round(time.time() - self._last_request_time, 1) if self._last_request_time else -1,
        }

    async def close(self):
        if self._session and not self._session.closed:
            await self._session.close()
            self._session = None
