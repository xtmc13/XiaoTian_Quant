"""
小天量化交易 - 网格交易策略
在价格区间内自动低买高卖
"""

import asyncio
import logging
from typing import List, Dict

from xtquant.strategy.base import BaseStrategy
from xtquant.core.data import Tick

logger = logging.getLogger("xtquant.strategy.grid")


class GridTradingStrategy(BaseStrategy):
    """
    网格交易策略

    参数:
      upper_price: 网格上限
      lower_price: 网格下限
      grid_num: 网格数量
      qty_per_grid: 每格交易量

    逻辑:
      1. 在上下限之间均匀划分网格
      2. 价格跌破网格线 -> 买入
      3. 价格涨破网格线 -> 卖出
      4. 每个网格只交易一次，避免重复
    """

    def __init__(self, symbols: list, upper_price: float, lower_price: float,
                 grid_num: int = 10, qty_per_grid: float = 0.001):
        super().__init__("GridTrading", symbols)
        self.upper_price = upper_price
        self.lower_price = lower_price
        self.grid_num = grid_num
        self.qty_per_grid = qty_per_grid

        # 生成网格
        self.grids: List[float] = []
        self._generate_grids()

        # 网格状态: price -> {bought: bool, sold: bool}
        self.grid_states: Dict[float, dict] = {}
        self._init_grid_states()

        self._last_grid_idx: int = -1

    def _generate_grids(self):
        """生成网格价格"""
        step = (self.upper_price - self.lower_price) / self.grid_num
        self.grids = [self.lower_price + i * step for i in range(self.grid_num + 1)]
        self.grids.sort()
        logger.info(f"[Grid] 生成 {len(self.grids)} 个网格: {self.grids[0]:.2f} ~ {self.grids[-1]:.2f}")

    def _init_grid_states(self):
        """初始化网格状态"""
        for price in self.grids:
            self.grid_states[price] = {"bought": False, "sold": False}

    def _get_grid_idx(self, price: float) -> int:
        """获取价格所在网格索引"""
        for i, grid_price in enumerate(self.grids):
            if price <= grid_price:
                return i
        return len(self.grids) - 1

    async def on_tick(self, tick: Tick):
        """Tick驱动网格交易"""
        current_idx = self._get_grid_idx(tick.price)

        if self._last_grid_idx < 0:
            self._last_grid_idx = current_idx
            return

        # 价格向下穿越网格线 -> 买入
        if current_idx < self._last_grid_idx:
            for i in range(current_idx, self._last_grid_idx):
                grid_price = self.grids[i]
                if not self.grid_states[grid_price]["bought"]:
                    logger.info(f"[Grid] 买入信号: 价格 {tick.price:.2f} 跌破网格 {grid_price:.2f}")
                    order = await self.buy(tick.symbol, tick.price, self.qty_per_grid)
                    if order:
                        self.grid_states[grid_price]["bought"] = True
                        self.grid_states[grid_price]["sold"] = False
                        self.trade_count += 1

        # 价格向上穿越网格线 -> 卖出
        elif current_idx > self._last_grid_idx:
            for i in range(self._last_grid_idx + 1, current_idx + 1):
                grid_price = self.grids[i]
                if not self.grid_states[grid_price]["sold"]:
                    logger.info(f"[Grid] 卖出信号: 价格 {tick.price:.2f} 涨破网格 {grid_price:.2f}")
                    order = await self.sell(tick.symbol, tick.price, self.qty_per_grid)
                    if order:
                        self.grid_states[grid_price]["sold"] = True
                        self.grid_states[grid_price]["bought"] = False
                        self.trade_count += 1

        self._last_grid_idx = current_idx

    async def run(self):
        """主循环"""
        logger.info(f"[Grid] 网格策略启动 | 网格数: {self.grid_num}")
        while self._running:
            await asyncio.sleep(5)

    def get_grid_status(self) -> dict:
        """获取网格状态"""
        return {
            "grids": self.grids,
            "states": self.grid_states,
            "current_idx": self._last_grid_idx
        }
