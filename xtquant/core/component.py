"""
组件基类 — 统一生命周期管理
所有组件(交易所/策略/风控/OMS)继承此类
"""

from __future__ import annotations
import logging
import time
from abc import ABC, abstractmethod
from enum import Enum


class ComponentStatus(Enum):
    CREATED = "created"
    INITIALIZING = "initializing"
    RUNNING = "running"
    STOPPING = "stopping"
    STOPPED = "stopped"
    ERROR = "error"


class HealthStatus(Enum):
    HEALTHY = "healthy"
    DEGRADED = "degraded"
    UNHEALTHY = "unhealthy"


class Component(ABC):
    """
    组件基类

    生命周期:
      __init__ → initialize → start → (running) → stop → (stopped)

    每个组件自动获得:
      - 状态追踪
      - 心跳上报
      - 健康检查
      - 统计信息
    """

    def __init__(self, name: str):
        self.name = name
        self.status = ComponentStatus.CREATED
        self.health = HealthStatus.HEALTHY
        self._start_time: float = 0
        self._stop_time: float = 0
        self._last_heartbeat: float = 0
        self._error_count: int = 0
        self._last_error: str = ""
        self.logger = logging.getLogger(f"xtquant.{name}")

    # --- 子类必须实现 ---

    @abstractmethod
    async def _on_start(self):
        """组件启动逻辑"""
        ...

    @abstractmethod
    async def _on_stop(self):
        """组件停止逻辑"""
        ...

    # --- 可选覆盖 ---

    async def _on_initialize(self):
        """初始化逻辑（在start之前执行）"""
        pass

    async def check_health(self) -> HealthStatus:
        """健康检查 - 子类可覆盖"""
        return HealthStatus.HEALTHY

    def get_stats(self) -> dict:
        """统计信息 - 子类可覆盖"""
        return {"name": self.name, "status": self.status.value}

    # --- 生命周期管理 ---

    async def initialize(self):
        self.status = ComponentStatus.INITIALIZING
        try:
            await self._on_initialize()
            self.logger.info(f"[{self.name}] 初始化完成")
        except Exception as e:
            self.status = ComponentStatus.ERROR
            self._last_error = str(e)
            self._error_count += 1
            self.logger.error(f"[{self.name}] 初始化失败: {e}")
            raise

    async def start(self):
        if self.status == ComponentStatus.RUNNING:
            return
        self.status = ComponentStatus.RUNNING
        self._start_time = time.time()
        self._last_heartbeat = time.time()
        try:
            await self._on_start()
            self.logger.info(f"[{self.name}] 已启动")
        except Exception as e:
            self.status = ComponentStatus.ERROR
            self._last_error = str(e)
            self._error_count += 1
            self.logger.error(f"[{self.name}] 启动失败: {e}")
            raise

    async def stop(self):
        if self.status == ComponentStatus.STOPPED:
            return
        self.status = ComponentStatus.STOPPING
        try:
            await self._on_stop()
        except Exception as e:
            self.logger.error(f"[{self.name}] 停止时异常: {e}")
        finally:
            self.status = ComponentStatus.STOPPED
            self._stop_time = time.time()
            self.logger.info(f"[{self.name}] 已停止 | 运行时长: {self.uptime:.0f}s")

    def heartbeat(self):
        """更新心跳时间"""
        self._last_heartbeat = time.time()

    # --- 属性 ---

    @property
    def uptime(self) -> float:
        if self._start_time == 0:
            return 0
        end = self._stop_time if self._stop_time > 0 else time.time()
        return end - self._start_time

    @property
    def is_running(self) -> bool:
        return self.status == ComponentStatus.RUNNING

    @property
    def heartbeat_age(self) -> float:
        return time.time() - self._last_heartbeat

    @property
    def is_alive(self) -> bool:
        """心跳在60秒内则视为存活"""
        return self.is_running and self.heartbeat_age < 60

    def __repr__(self):
        return f"Component({self.name}|{self.status.value})"
