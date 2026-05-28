"""
小天量化交易 - 跨交易所套利策略
利用币安和OKX之间的价格差异进行套利
"""

import asyncio
import logging
from typing import Dict

from xtquant.strategy.base import BaseStrategy
from xtquant.core.data import Tick, OrderBook

logger = logging.getLogger("xtquant.strategy.arbitrage")


class CrossExchangeArbitrage(BaseStrategy):
    """
    跨交易所套利策略

    逻辑:
      1. 监控币安和OKX同一交易对的价格
      2. 当价差超过阈值时，在低价所买入，高价所卖出
      3. 考虑手续费后确保净利润
    """

    def __init__(self, symbols: list, min_spread_bps: float = 10.0,
                 qty: float = 0.001, fee_rate: float = 0.001):
        super().__init__("CrossArbitrage", symbols)
        self.min_spread_bps = min_spread_bps  # 最小价差 (基点)
        self.qty = qty
        self.fee_rate = fee_rate

        # 价格缓存: exchange -> symbol -> price
        self._prices: Dict[str, Dict[str, float]] = {}
        self._books: Dict[str, Dict[str, OrderBook]] = {}

    async def on_tick(self, tick: Tick):
        """Tick驱动套利检测"""
        if tick.exchange not in self._prices:
            self._prices[tick.exchange] = {}
        self._prices[tick.exchange][tick.symbol] = tick.price

        # 检查是否有两个交易所的价格
        symbol = tick.symbol
        prices = {ex: data.get(symbol, 0) for ex, data in self._prices.items() if symbol in data}

        if len(prices) < 2:
            return

        # 找出最高和最低价
        min_ex = min(prices, key=prices.get)
        max_ex = max(prices, key=prices.get)
        min_price = prices[min_ex]
        max_price = prices[max_ex]

        if min_price <= 0 or max_price <= 0:
            return

        # 计算价差 (基点)
        spread_bps = (max_price - min_price) / min_price * 10000

        # 考虑双边手续费后的净利润
        total_fee = min_price * self.fee_rate + max_price * self.fee_rate
        profit = max_price - min_price - total_fee

        if spread_bps >= self.min_spread_bps and profit > 0:
            logger.info(f"[Arbitrage] 套利机会! {symbol} | "
                       f"{min_ex}: {min_price:.2f} -> {max_ex}: {max_price:.2f} | "
                       f"价差: {spread_bps:.1f}bps | 预估利润: {profit:.4f} USDT")

            # 执行套利 (需要同时在两个交易所下单)
            buy_order = await self._engine.place_order(min_ex, symbol, "BUY", "LIMIT", min_price, self.qty)
            sell_order = await self._engine.place_order(max_ex, symbol, "SELL", "LIMIT", max_price, self.qty)
            if buy_order and sell_order:
                self.trade_count += 1

    async def on_orderbook(self, book: OrderBook):
        """订单簿驱动套利 (更精确)"""
        if book.exchange not in self._books:
            self._books[book.exchange] = {}
        self._books[book.exchange][book.symbol] = book

        # 使用买一卖一价计算套利空间
        symbol = book.symbol
        books = {ex: data.get(symbol) for ex, data in self._books.items() if symbol in data}

        if len(books) < 2:
            return

        # 找最佳买价(最高买价)和最佳卖价(最低卖价)
        best_bid = 0
        best_bid_ex = ""
        best_ask = float('inf')
        best_ask_ex = ""

        for ex, b in books.items():
            if b.best_bid() > best_bid:
                best_bid = b.best_bid()
                best_bid_ex = ex
            if b.best_ask() < best_ask:
                best_ask = b.best_ask()
                best_ask_ex = ex

        if best_bid <= 0 or best_ask >= float('inf') or best_bid_ex == best_ask_ex:
            return

        # 买价 > 卖价 才有套利空间
        if best_bid > best_ask:
            spread_bps = (best_bid - best_ask) / best_ask * 10000
            profit = (best_bid - best_ask) * self.qty - (best_bid + best_ask) * self.qty * self.fee_rate

            if spread_bps >= self.min_spread_bps and profit > 0:
                logger.info(f"[Arbitrage] 订单簿套利! {symbol} | "
                           f"买: {best_ask_ex}@{best_ask:.2f} -> 卖: {best_bid_ex}@{best_bid:.2f} | "
                           f"价差: {spread_bps:.1f}bps | 利润: {profit:.4f}")

    async def run(self):
        """主循环"""
        logger.info(f"[Arbitrage] 跨所套利策略启动 | 最小价差: {self.min_spread_bps}bps")
        while self._running:
            await asyncio.sleep(1)
