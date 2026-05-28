"""
向量化策略基类 — FreqTrade 风格 DataFrame 信号生成模式

策略只需实现三个方法:
  - populate_indicators: 批量计算技术指标列
  - populate_entry_trend:  标记入场信号 (enter_long / enter_short)
  - populate_exit_trend:   标记出场信号 (exit_long / exit_short)

与事件驱动的 BaseStrategy 并行存在，互不影响。
"""

from __future__ import annotations
from abc import ABC, abstractmethod
from typing import Dict, List, Optional
import pandas as pd
import numpy as np


class VectorizedStrategy(ABC):
    """FreqTrade 风格向量化策略基类

    子类配置示例:
        class MyStrategy(VectorizedStrategy):
            timeframe = "1h"
            stoploss = -0.05
            minimal_roi = {"60": 0.01, "30": 0.02, "0": 0.04}
            startup_candle_count = 20

            def populate_indicators(self, dataframe, metadata):
                dataframe['ema_20'] = dataframe['close'].ewm(span=20).mean()
                dataframe['rsi_14'] = ta.RSI(dataframe, 14)
                return dataframe

            def populate_entry_trend(self, dataframe, metadata):
                dataframe.loc[
                    (dataframe['close'] > dataframe['ema_20']) & (dataframe['rsi_14'] < 30),
                    'enter_long'
                ] = 1
                return dataframe

            def populate_exit_trend(self, dataframe, metadata):
                dataframe.loc[dataframe['rsi_14'] > 70, 'exit_long'] = 1
                return dataframe
    """

    # —— 子类可覆盖的类属性 ——
    timeframe: str = "1h"
    can_short: bool = False
    stoploss: float = -0.10
    minimal_roi: Dict[str, float] = {"0": 0.10}
    trailing_stop: bool = False
    trailing_stop_positive: Optional[float] = None
    trailing_stop_positive_offset: float = 0.0
    startup_candle_count: int = 20

    def __init__(self, name: str = "", symbols: list = None, params: dict = None):
        self.name = name or self.__class__.__name__
        self.symbols = symbols or ["BTCUSDT"]
        self.params = params or {}

    # ============================================================
    #  三个核心抽象方法
    # ============================================================

    @abstractmethod
    def populate_indicators(self, dataframe: pd.DataFrame,
                            metadata: dict = None) -> pd.DataFrame:
        """计算所有技术指标，作为新列加到 DataFrame 上。"""
        ...

    @abstractmethod
    def populate_entry_trend(self, dataframe: pd.DataFrame,
                             metadata: dict = None) -> pd.DataFrame:
        """标记入场信号列: enter_long=1 / enter_short=1"""
        ...

    @abstractmethod
    def populate_exit_trend(self, dataframe: pd.DataFrame,
                            metadata: dict = None) -> pd.DataFrame:
        """标记出场信号列: exit_long=1 / exit_short=1"""
        ...

    # ============================================================
    #  便捷方法
    # ============================================================

    def generate_signals(self, dataframe: pd.DataFrame,
                         metadata: dict = None) -> pd.DataFrame:
        """一键执行完整信号流水线: 指标 → 入场 → 出场"""
        if metadata is None:
            metadata = {"pair": self.symbols[0], "timeframe": self.timeframe}

        df = self.populate_indicators(dataframe, metadata)
        df = self.populate_entry_trend(df, metadata)
        df = self.populate_exit_trend(df, metadata)

        for col in ("enter_long", "enter_short", "exit_long", "exit_short"):
            if col not in df.columns:
                df[col] = 0

        return df

    def get_hyperopt_space(self) -> dict:
        """返回可超参优化的参数空间。子类覆盖此方法来暴露优化参数。

        Returns:
            {"rsi_buy_low": (1, 50, 30, 5),   # (min, max, default, step)
             "rsi_sell_high": (50, 100, 70, 5)}
        """
        return {}

    # ============================================================
    #  止损/止盈计算 (回测引擎调用)
    # ============================================================

    def should_exit_for_stoploss(self, current_price: float,
                                  entry_price: float,
                                  position_type: str = "long") -> float:
        """检查是否触发止损。返回 0 表示未触发，>0 表示应出场。"""
        if position_type == "long":
            pnl_pct = (current_price - entry_price) / entry_price
            return 1.0 if pnl_pct <= self.stoploss else 0.0
        else:
            pnl_pct = (entry_price - current_price) / entry_price
            return 1.0 if pnl_pct <= self.stoploss else 0.0

    def should_exit_for_roi(self, current_price: float,
                             entry_price: float,
                             minutes_held: float,
                             position_type: str = "long") -> float:
        """检查是否触发 minimal_roi 止盈。返回比率，0 表示未触发。"""
        if not self.minimal_roi:
            return 0.0

        pnl_pct = ((current_price - entry_price) / entry_price
                   if position_type == "long"
                   else (entry_price - current_price) / entry_price)

        for min_str, target in sorted(self.minimal_roi.items(),
                                       key=lambda x: int(x[0])):
            if minutes_held >= int(min_str) and pnl_pct >= target:
                return pnl_pct
        return 0.0
