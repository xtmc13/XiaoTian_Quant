"""
小天量化交易 - 策略基类
所有策略(传统/AI/多因子)继承此类
同一套代码回测和实盘无缝切换
"""

import asyncio
from abc import ABC, abstractmethod
from typing import Dict, List, Optional
import logging

from ..core.event import EventBus, EventType, MarketEvent
from ..core.data import Tick, OrderBook, Bar, OrderData
from ..core.engine import TradingEngine

logger = logging.getLogger("xtquant.strategy")


class BaseStrategy(ABC):
    """
    策略基类

    生命周期:
      1. __init__ -> 配置参数
      2. set_event_bus -> 绑定事件总线
      3. set_engine -> 绑定交易引擎
      4. start -> 启动策略
      5. on_tick/on_book/on_bar/on_order -> 接收事件
      6. run -> 主循环(可选)
      7. stop -> 停止策略

    便捷方法:
      • buy/sell -> 快捷下单
      • get_price -> 获取最新价格
      • get_book -> 获取订单簿
    """

    def __init__(self, name: str, symbols: List[str]):
        self.name = name
        self.symbols = symbols
        self._event_bus: Optional[EventBus] = None
        self._engine: Optional[TradingEngine] = None
        self._running = False
        self._task: Optional[asyncio.Task] = None

        # 数据缓存
        self.tick_cache: Dict[str, Tick] = {}
        self.book_cache: Dict[str, OrderBook] = {}
        self.bar_cache: Dict[str, Bar] = {}
        self.order_cache: Dict[str, OrderData] = {}

        # 策略状态
        self.positions: Dict[str, float] = {}
        self.pending_orders: Dict[str, OrderData] = {}
        self.trade_count = 0
        self.pnl = 0.0

    def set_event_bus(self, bus: EventBus):
        """绑定事件总线"""
        self._event_bus = bus
        bus.subscribe(EventType.TICK, self._on_tick_wrapper, priority=10)
        bus.subscribe(EventType.ORDERBOOK, self._on_book_wrapper, priority=10)
        bus.subscribe(EventType.BAR, self._on_bar_wrapper, priority=10)
        bus.subscribe(EventType.ORDER_STATUS, self._on_order_wrapper, priority=10)

    def set_engine(self, engine: TradingEngine):
        """绑定交易引擎"""
        self._engine = engine

    async def _on_tick_wrapper(self, event: MarketEvent):
        if event.symbol in self.symbols:
            self.tick_cache[event.symbol] = Tick(
                event.exchange, event.symbol,
                event.data["price"], event.data["volume"], event.timestamp,
                event.data.get("bid", 0), event.data.get("ask", 0)
            )
            try:
                await self.on_tick(self.tick_cache[event.symbol])
            except Exception as e:
                logger.error(f"[策略 {self.name}] on_tick异常: {e}")

    async def _on_book_wrapper(self, event: MarketEvent):
        if event.symbol in self.symbols:
            self.book_cache[event.symbol] = OrderBook(
                event.symbol, event.exchange,
                event.data["bids"], event.data["asks"], event.timestamp
            )
            try:
                await self.on_orderbook(self.book_cache[event.symbol])
            except Exception as e:
                logger.error(f"[策略 {self.name}] on_orderbook异常: {e}")

    async def _on_bar_wrapper(self, event: MarketEvent):
        if event.symbol in self.symbols:
            d = event.data
            self.bar_cache[event.symbol] = Bar(
                event.exchange, event.symbol, d["interval"],
                d["open"], d["high"], d["low"], d["close"],
                d["volume"], d.get("quote_volume", 0), d.get("trade_count", 0), event.timestamp
            )
            try:
                await self.on_bar(self.bar_cache[event.symbol])
            except Exception as e:
                logger.error(f"[策略 {self.name}] on_bar异常: {e}")

    async def _on_order_wrapper(self, event: MarketEvent):
        d = event.data
        order = OrderData(
            d["order_id"], event.exchange, event.symbol,
            d["side"], d["type"], d["price"], d["qty"],
            d["status"], d.get("filled", 0), d.get("remaining", 0),
            d.get("avg_price", 0), d.get("fee", 0), timestamp=event.timestamp,
            client_order_id=d.get("client_id", "")
        )
        self.order_cache[order.order_id] = order

        if order.is_active:
            self.pending_orders[order.order_id] = order
        else:
            self.pending_orders.pop(order.order_id, None)

        try:
            await self.on_order_update(order)
        except Exception as e:
            logger.error(f"[策略 {self.name}] on_order_update异常: {e}")

    # --- 子类可重写的事件回调 ---
    async def on_tick(self, tick: Tick):
        """收到Tick时触发"""
        pass

    async def on_orderbook(self, book: OrderBook):
        """收到订单簿时触发"""
        pass

    async def on_bar(self, bar: Bar):
        """收到K线时触发"""
        pass

    async def on_order_update(self, order: OrderData):
        """订单状态更新时触发"""
        pass

    @abstractmethod
    async def run(self):
        """策略主循环(定时任务等)"""
        pass

    # --- 便捷下单方法 ---
    async def buy(self, symbol: str, price: float, qty: float,
                  order_type: str = "LIMIT", exchange: str = None, **kwargs) -> Optional[OrderData]:
        """买入"""
        if self._engine:
            ex = exchange or list(self._engine.exchanges.keys())[0]
            return await self._engine.place_order(ex, symbol, "BUY", order_type, price, qty, **kwargs)
        return None

    async def sell(self, symbol: str, price: float, qty: float,
                   order_type: str = "LIMIT", exchange: str = None, **kwargs) -> Optional[OrderData]:
        """卖出"""
        if self._engine:
            ex = exchange or list(self._engine.exchanges.keys())[0]
            return await self._engine.place_order(ex, symbol, "SELL", order_type, price, qty, **kwargs)
        return None

    async def cancel(self, order_id: str, symbol: str, exchange: str = None) -> bool:
        """撤单"""
        if self._engine:
            ex = exchange or list(self._engine.exchanges.keys())[0]
            return await self._engine.cancel_order(ex, order_id, symbol)
        return False

    def get_price(self, symbol: str) -> float:
        """获取最新价格"""
        if symbol in self.tick_cache:
            return self.tick_cache[symbol].price
        if symbol in self.book_cache:
            return self.book_cache[symbol].mid_price()
        return 0.0

    def get_book(self, symbol: str) -> Optional[OrderBook]:
        """获取最新订单簿"""
        return self.book_cache.get(symbol)

    def get_position(self, symbol: str) -> float:
        """获取当前持仓(简化版)"""
        return self.positions.get(symbol, 0.0)

    async def start(self):
        """启动策略"""
        self._running = True
        self._task = asyncio.create_task(self.run())
        logger.info(f"[策略] {self.name} 已启动 | 关注: {self.symbols}")

    async def stop(self):
        """停止策略"""
        self._running = False
        if self._task:
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass
        logger.info(f"[策略] {self.name} 已停止 | 交易次数: {self.trade_count}")

    def get_stats(self) -> dict:
        return {
            "name": self.name,
            "symbols": self.symbols,
            "running": self._running,
            "trade_count": self.trade_count,
            "pnl": self.pnl,
            "pending_orders": len(self.pending_orders),
            "positions": dict(self.positions)
        }
