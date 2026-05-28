"""
小天量化交易 - 交易所抽象基类
所有交易所(币安/OKX/回测)必须实现此接口
"""

import asyncio
import time
from abc import ABC, abstractmethod
from typing import Dict, List, Optional
import logging

from ..core.event import EventBus, MarketEvent
from ..core.data import Tick, OrderBook, OrderData, Balance, Position

logger = logging.getLogger("xtquant.exchange")


class BaseExchange(ABC):
    """
    统一交易所接口

    设计原则:
      • 所有行情数据通过EventBus发布，不直接回调策略
      • 所有操作异步化，支持并发
      • 内置连接状态管理
    """

    def __init__(self, name: str, api_key: str = "", secret: str = "", 
                 passphrase: str = "", testnet: bool = False):
        self.name = name
        self.api_key = api_key
        self.secret = secret
        self.passphrase = passphrase
        self.testnet = testnet

        # REST timeout / retry (applied by subclasses in session calls)
        self._request_timeout_sec = 10
        self._max_retries = 3

        self._running = False
        self._connected = False
        self._ws_tasks: List[asyncio.Task] = []
        self._event_bus: Optional[EventBus] = None

        # 数据缓存
        self._tick_cache: Dict[str, Tick] = {}
        self._book_cache: Dict[str, OrderBook] = {}
        self._balance_cache: Dict[str, Balance] = {}
        self._position_cache: Dict[str, Position] = {}

        # 统计
        self._ws_reconnect_count = 0
        self._last_ws_msg_time = 0

    def set_event_bus(self, bus: EventBus):
        self._event_bus = bus

    def _emit(self, event: MarketEvent):
        """发布事件到总线（非阻塞，背压由 EventBus 队列控制）"""
        if self._event_bus:
            self._event_bus.emit_nowait(event)

    @property
    def is_connected(self) -> bool:
        return self._connected and self._running

    @abstractmethod
    async def connect_market_data(self, symbols: List[str]):
        """连接行情WebSocket"""
        raise NotImplementedError(f"{self.__class__.__name__} must implement connect_market_data")

    @abstractmethod
    async def place_order(self, symbol: str, side: str, order_type: str,
                         price: float, quantity: float, client_id: str = "", **kwargs) -> OrderData:
        """下单"""
        raise NotImplementedError(f"{self.__class__.__name__} must implement place_order")

    @abstractmethod
    async def cancel_order(self, order_id: str, symbol: str) -> bool:
        """撤单"""
        raise NotImplementedError(f"{self.__class__.__name__} must implement cancel_order")

    @abstractmethod
    async def get_balance(self) -> Dict[str, float]:
        """获取账户余额"""
        raise NotImplementedError(f"{self.__class__.__name__} must implement get_balance")

    @abstractmethod
    async def get_position(self, symbol: str) -> Optional[Position]:
        """获取持仓"""
        raise NotImplementedError(f"{self.__class__.__name__} must implement get_position")

    @abstractmethod
    async def get_open_orders(self, symbol: str) -> List[OrderData]:
        """获取未成交订单"""
        raise NotImplementedError(f"{self.__class__.__name__} must implement get_open_orders")

    async def stop(self):
        """停止交易所连接"""
        self._running = False
        self._connected = False
        for task in self._ws_tasks:
            task.cancel()
        self._ws_tasks.clear()
        logger.info(f"[{self.name}] 交易所已停止")

    def get_stats(self) -> dict:
        return {
            "name": self.name,
            "connected": self.is_connected,
            "testnet": self.testnet,
            "ws_reconnects": self._ws_reconnect_count,
            "tick_cache_size": len(self._tick_cache),
            "book_cache_size": len(self._book_cache),
            "last_msg_age": time.time() - self._last_ws_msg_time if self._last_ws_msg_time else -1
        }
