"""
做市策略 — 在买卖两侧挂限价单，赚取价差

策略逻辑:
  1. 获取当前买一卖一价
  2. 在买一上方和卖一下方各挂单
  3. 定期撤单重挂，跟随价格移动
  4. 控制净敞口，避免单边持仓过大
"""

import asyncio
import logging
from typing import List, Optional

from xtquant.strategy.base import BaseStrategy
from xtquant.core.data import Tick, OrderBook

logger = logging.getLogger("xtquant.strategy.mm")


class MarketMakingStrategy(BaseStrategy):
    """
    双边做市策略

    参数:
      base_spread: 基础价差 (相对值，如0.002=0.2%)
      qty: 每边挂单量
      rebalance_interval: 撤单重挂间隔(秒)
      max_position: 最大净持仓
    """

    def __init__(self, symbols: List[str], base_spread: float = 0.002,
                 qty: float = 0.001, rebalance_interval: float = 5.0,
                 max_position: float = 0.01):
        super().__init__("MarketMaking", symbols)
        self.base_spread = base_spread
        self.qty = qty
        self.rebalance_interval = rebalance_interval
        self.max_position = max_position

        # 当前挂单 (bid/ask)
        self._active_bid_order: Optional[str] = None
        self._active_ask_order: Optional[str] = None
        self._bid_price: float = 0.0
        self._ask_price: float = 0.0

    async def on_orderbook(self, book: OrderBook):
        """订单簿更新时评估做市机会"""
        pass  # 主循环统一处理

    async def on_tick(self, tick: Tick):
        """Tick驱动快速响应"""
        pass  # 主循环统一处理

    async def run(self):
        """做市主循环"""
        logger.info(f"[MarketMaking] 做市策略启动 | "
                    f"价差:{self.base_spread*100:.2f}% | "
                    f"数量:{self.qty} | "
                    f"刷新间隔:{self.rebalance_interval}s")
        while self._running:
            try:
                for symbol in self.symbols:
                    await self._do_market_make(symbol)
                await asyncio.sleep(self.rebalance_interval)
            except Exception as e:
                logger.error(f"[MarketMaking] 异常: {e}")
                await asyncio.sleep(1)

    async def _do_market_make(self, symbol: str):
        """执行一次做市操作"""
        book = self.get_book(symbol)
        if not book:
            return

        mid = book.mid_price()
        if mid <= 0:
            return

        spread = max(self.base_spread, book.spread() * 1.5)  # 至少比市场价差宽

        new_bid = mid * (1 - spread / 2)
        new_ask = mid * (1 + spread / 2)

        # 检查持仓限制
        position = self.get_position(symbol)
        if abs(position) >= self.max_position:
            return  # 持仓已达上限

        # 如果价格变动不大，保持现有挂单
        if (self._active_bid_order and
            abs(new_bid - self._bid_price) / self._bid_price < 0.001):
            return  # 价格变化太小，不重挂

        # 取消旧单
        if self._active_bid_order:
            await self.cancel(self._active_bid_order, symbol)
            self._active_bid_order = None

        if self._active_ask_order:
            await self.cancel(self._active_ask_order, symbol)
            self._active_ask_order = None

        # 挂新买单 (做多方向)
        if position < self.max_position:
            order = await self.buy(symbol, new_bid, self.qty, order_type="LIMIT")
            if order:
                self._active_bid_order = order.id if hasattr(order, 'id') else order.order_id
                self._bid_price = new_bid
                logger.info(f"[MarketMaking] 挂买单 {symbol} @ {new_bid:.2f} x {self.qty}")

        # 挂新卖单 (做空方向)
        if position > -self.max_position:
            order = await self.sell(symbol, new_ask, self.qty, order_type="LIMIT")
            if order:
                self._active_ask_order = order.id if hasattr(order, 'id') else order.order_id
                self._ask_price = new_ask
                logger.info(f"[MarketMaking] 挂卖单 {symbol} @ {new_ask:.2f} x {self.qty}")

    def get_stats(self) -> dict:
        base = super().get_stats()
        base.update({
            "bid_price": self._bid_price,
            "ask_price": self._ask_price,
            "spread": self.base_spread,
        })
        return base


class SimpleBreakoutStrategy(BaseStrategy):
    """
    简单突破策略

    当价格突破N期高点时买入，跌破N期低点时卖出
    """

    def __init__(self, symbols: List[str], period: int = 20, qty: float = 0.001):
        super().__init__("Breakout", symbols)
        self.period = period
        self.qty = qty
        self._highs: dict = {}
        self._lows: dict = {}
        self._bar_counts: dict = {}
        self._position: float = 0.0

    async def on_bar(self, bar):
        """K线驱动突破检测"""
        symbol = bar.symbol
        if symbol not in self._highs:
            self._highs[symbol] = []
            self._lows[symbol] = []
            self._bar_counts[symbol] = 0

        self._highs[symbol].append(bar.high)
        self._lows[symbol].append(bar.low)
        self._bar_counts[symbol] += 1

        if self._bar_counts[symbol] < self.period + 1:
            return

        # 保持窗口（保留 period+1 根，[:-1] 排除当前K线取前N根）
        self._highs[symbol] = self._highs[symbol][-(self.period + 1):]
        self._lows[symbol] = self._lows[symbol][-(self.period + 1):]

        highest = max(self._highs[symbol][:-1])  # 前N根的最高价
        lowest = min(self._lows[symbol][:-1])    # 前N根的最低价

        # 突破买入
        if bar.close > highest and self._position <= 0:
            order = await self.buy(symbol, bar.close, self.qty)
            if order:
                self._position += self.qty
                self.trade_count += 1
                logger.info(f"[Breakout] 突破买入 {symbol} @ {bar.close:.2f} "
                           f"| 突破 {highest:.2f}")

        # 跌破卖出
        elif bar.close < lowest and self._position > 0:
            order = await self.sell(symbol, bar.close, self._position)
            if order:
                self._position = 0.0
                self.trade_count += 1
                logger.info(f"[Breakout] 跌破卖出 {symbol} @ {bar.close:.2f} "
                           f"| 跌破 {lowest:.2f}")

    async def run(self):
        logger.info(f"[Breakout] 突破策略启动 | 周期:{self.period} | 数量:{self.qty}")
        while self._running:
            await asyncio.sleep(5)
