"""
小天量化交易 - 事件系统
基于Qbot的事件驱动架构，所有模块通过EventBus通信
"""

import asyncio
from dataclasses import dataclass
from typing import Dict, List, Optional, Callable, Any
from enum import Enum
from collections import defaultdict
import logging

logger = logging.getLogger("xtquant.event")


class EventType(Enum):
    """事件类型枚举"""
    TICK = "tick"               # 最新成交价
    ORDERBOOK = "orderbook"     # 订单簿深度
    BAR = "bar"                 # K线
    TRADE = "trade"             # 逐笔成交
    ORDER_STATUS = "order_status"   # 订单状态更新
    BALANCE = "balance"         # 余额更新
    POSITION = "position"       # 持仓更新
    FACTOR = "factor"           # 因子更新
    SIGNAL = "signal"           # 交易信号
    RISK_ALERT = "risk_alert"   # 风控告警
    SYSTEM = "system"           # 系统事件


@dataclass
class MarketEvent:
    """
    市场事件统一封装
    所有数据流的唯一格式，实现交易所与策略的彻底解耦
    """
    event_type: EventType
    exchange: str               # BINANCE / OKX / BACKTEST
    symbol: str
    timestamp: int              # 毫秒级时间戳
    data: Dict[str, Any]        # 具体载荷
    raw: Any = None             # 原始数据（调试/溯源用）

    def to_dict(self) -> dict:
        return {
            "event_type": self.event_type.value,
            "exchange": self.exchange,
            "symbol": self.symbol,
            "timestamp": self.timestamp,
            "data": self.data
        }

    def __repr__(self):
        return f"MarketEvent({self.event_type.value}|{self.exchange}|{self.symbol}|{self.timestamp})"


class EventBus:
    """
    异步事件总线 - Qbot风格的事件驱动核心

    特性:
      • 支持同步/异步回调
      • 事件队列缓冲，防止背压
      • 按事件类型分发，高效路由
      • 支持优先级订阅
    """

    def __init__(self, max_queue_size: int = 10000):
        self._subscribers: Dict[EventType, List[Callable]] = defaultdict(list)
        self._queue: asyncio.Queue = asyncio.Queue(maxsize=max_queue_size)
        self._running = False
        self._task: Optional[asyncio.Task] = None
        self._dropped_events = 0
        self._processed_events = 0

    def subscribe(self, event_type: EventType, callback: Callable, priority: int = 0):
        """
        订阅事件
        priority: 数字越小优先级越高，风控类建议设为0（最先处理）
        """
        self._subscribers[event_type].append((priority, callback))
        # 按优先级排序
        self._subscribers[event_type].sort(key=lambda x: x[0])
        logger.info(f"[EventBus] 订阅 {event_type.value} -> {callback.__name__} (priority={priority})")

    def unsubscribe(self, event_type: EventType, callback: Callable):
        """取消订阅"""
        subs = self._subscribers[event_type]
        self._subscribers[event_type] = [(p, cb) for p, cb in subs if cb != callback]

    async def emit(self, event: MarketEvent):
        """发布事件到总线"""
        if self._queue.full():
            self._dropped_events += 1
            if self._dropped_events % 100 == 1:
                logger.warning(f"[EventBus] 事件队列已满，已丢弃 {self._dropped_events} 个事件")
            return
        await self._queue.put(event)

    def emit_nowait(self, event: MarketEvent):
        """非阻塞发布（同步方法，可能丢弃）"""
        try:
            self._queue.put_nowait(event)
        except asyncio.QueueFull:
            self._dropped_events += 1

    async def _dispatcher(self):
        """事件分发器主循环"""
        while self._running:
            try:
                event = await asyncio.wait_for(self._queue.get(), timeout=1.0)
                self._processed_events += 1

                callbacks = self._subscribers.get(event.event_type, [])
                for priority, cb in callbacks:
                    try:
                        if asyncio.iscoroutinefunction(cb):
                            asyncio.create_task(cb(event))
                        else:
                            cb(event)
                    except Exception as e:
                        logger.error(f"[EventBus] 回调异常 {cb.__name__}: {e}")

            except asyncio.TimeoutError:
                continue
            except Exception as e:
                logger.error(f"[EventBus] 分发器异常: {e}")

    def get_stats(self) -> dict:
        return {
            "queue_size": self._queue.qsize(),
            "processed": self._processed_events,
            "dropped": self._dropped_events,
            "subscribers": {et.value: len(cbs) for et, cbs in self._subscribers.items()}
        }

    async def start(self):
        self._running = True
        self._task = asyncio.create_task(self._dispatcher())
        logger.info("[EventBus] 事件总线已启动")

    async def stop(self):
        self._running = False
        if self._task:
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass
        logger.info(f"[EventBus] 已停止，共处理 {self._processed_events} 个事件")
