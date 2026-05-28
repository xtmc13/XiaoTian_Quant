"""
进程守护 — 监控心跳、崩溃重启

Usage:
  watchdog = Watchdog(max_restarts_per_minute=3)
  await watchdog.watch(coro)  # 监控一个协程
"""

import asyncio
import logging
import time
import signal
from typing import Optional, Callable, List

logger = logging.getLogger("xtquant.watchdog")


class Watchdog:
    """
    进程守护器

    特性:
      - 监控组件心跳
      - 崩溃自动重启（有限次数）
      - 优雅关闭
      - 回调通知
    """

    def __init__(self, max_restarts_per_minute: int = 3,
                 heartbeat_interval: float = 5.0):
        self.max_restarts = max_restarts_per_minute
        self.heartbeat_interval = heartbeat_interval
        self._running = False
        self._shutdown_event = asyncio.Event()

        # 重启统计
        self._restart_window: List[float] = []  # 最近60s内的重启时间戳
        self._total_restarts = 0

        # 被监控的组件
        self._watched: dict = {}  # name -> last_heartbeat

        # 回调
        self._on_restart: Optional[Callable] = None
        self._on_crash: Optional[Callable] = None

    def watch(self, name: str, component):
        """监控一个组件"""
        self._watched[name] = time.time()

    def heartbeat(self, name: str):
        """组件心跳"""
        self._watched[name] = time.time()

    async def start(self):
        """启动守护"""
        self._running = True
        self._shutdown_event.clear()

        # 注册信号处理
        loop = asyncio.get_event_loop()
        for sig in (signal.SIGINT, signal.SIGTERM):
            try:
                loop.add_signal_handler(sig, lambda: self._shutdown_event.set())
            except NotImplementedError:
                pass  # Windows不支持add_signal_handler

        logger.info(f"[Watchdog] 守护已启动 | 最大重启: {self.max_restarts}/分钟")

    async def run_until_shutdown(self):
        """运行直到收到关闭信号"""
        try:
            await self._shutdown_event.wait()
        except asyncio.CancelledError:
            pass
        finally:
            await self.stop()

    def can_restart(self) -> bool:
        """检查是否可以重启"""
        now = time.time()
        # 清理60s前的记录
        self._restart_window = [t for t in self._restart_window if now - t < 60]
        return len(self._restart_window) < self.max_restarts

    def record_restart(self):
        """记录一次重启"""
        self._restart_window.append(time.time())
        self._total_restarts += 1

    async def restart_component(self, name: str, start_fn: Callable):
        """重启一个组件"""
        if not self.can_restart():
            logger.critical(f"[Watchdog] 已达最大重启次数({self.max_restarts}/分钟)，放弃重启 {name}")
            if self._on_crash:
                await self._on_crash(name)
            return False

        self.record_restart()
        logger.warning(f"[Watchdog] 重启 {name} | 近60s重启: {len(self._restart_window)}")

        if self._on_restart:
            await self._on_restart(name)

        try:
            await start_fn()
            self.heartbeat(name)
            return True
        except Exception as e:
            logger.error(f"[Watchdog] {name} 重启失败: {e}")
            return False

    async def stop(self):
        """停止守护"""
        self._running = False
        logger.info(f"[Watchdog] 守护已停止 | 总重启: {self._total_restarts}")

    @property
    def recent_restarts(self) -> int:
        now = time.time()
        return sum(1 for t in self._restart_window if now - t < 60)

    def get_stats(self) -> dict:
        return {
            "running": self._running,
            "watched_components": list(self._watched.keys()),
            "recent_restarts": self.recent_restarts,
            "total_restarts": self._total_restarts,
        }
