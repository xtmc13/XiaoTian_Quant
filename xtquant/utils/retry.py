"""
重试工具 — 指数退避 + 抖动，用于API调用和WebSocket重连
"""

import asyncio
import random
import time
import functools
import logging
from typing import Type, Tuple, Callable, Optional

logger = logging.getLogger("xtquant.retry")


def exponential_backoff(
    attempt: int,
    base_delay: float = 1.0,
    max_delay: float = 60.0,
    jitter: bool = True,
) -> float:
    """
    指数退避延迟计算

    Args:
        attempt: 当前重试次数 (1-based)
        base_delay: 基础延迟秒数
        max_delay: 最大延迟上限
        jitter: 是否加随机抖动
    """
    delay = min(base_delay * (2 ** (attempt - 1)), max_delay)
    if jitter:
        delay = delay * (0.5 + random.random())
    return delay


async def async_retry(
    func: Callable,
    *args,
    retries: int = 3,
    base_delay: float = 1.0,
    max_delay: float = 60.0,
    retryable_exceptions: Tuple[Type[BaseException], ...] = (
        ConnectionError,
        TimeoutError,
        OSError,
    ),
    on_retry: Optional[Callable] = None,
    **kwargs,
):
    """
    异步重试装饰器实现

    Args:
        func: 异步函数
        retries: 最大重试次数
        base_delay: 基础延迟
        max_delay: 最大延迟
        retryable_exceptions: 可重试的异常类型
        on_retry: 重试时的回调 (exception, attempt, delay) -> None
    """
    last_exc = None
    for attempt in range(1, retries + 2):
        try:
            return await func(*args, **kwargs)
        except retryable_exceptions as e:
            last_exc = e
            if attempt > retries:
                raise
            delay = exponential_backoff(attempt, base_delay, max_delay)
            logger.warning(
                f"重试 {attempt}/{retries} | {func.__name__} | "
                f"{type(e).__name__}: {e} | 等待 {delay:.1f}s"
            )
            if on_retry:
                on_retry(e, attempt, delay)
            await asyncio.sleep(delay)
    raise last_exc  # type: ignore


def retry(
    retries: int = 3,
    base_delay: float = 1.0,
    max_delay: float = 60.0,
    retryable_exceptions: Tuple[Type[BaseException], ...] = (
        ConnectionError,
        TimeoutError,
        OSError,
    ),
):
    """同步重试装饰器"""
    def decorator(func):
        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            last_exc = None
            for attempt in range(1, retries + 2):
                try:
                    return func(*args, **kwargs)
                except retryable_exceptions as e:
                    last_exc = e
                    if attempt > retries:
                        raise
                    delay = exponential_backoff(attempt, base_delay, max_delay)
                    logger.warning(
                        f"重试 {attempt}/{retries} | {func.__name__} | "
                        f"{type(e).__name__} | 等待 {delay:.1f}s"
                    )
                    time.sleep(delay)
            raise last_exc
        return wrapper
    return decorator


class RateLimiter:
    """异步速率限制器 (Token Bucket)"""

    def __init__(self, rate: float, burst: int = 1):
        self.rate = rate
        self.burst = burst
        self._tokens = float(burst)
        self._last_refill = time.monotonic()
        self._lock = asyncio.Lock()

    async def acquire(self) -> float:
        """获取令牌，返回等待的秒数"""
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
