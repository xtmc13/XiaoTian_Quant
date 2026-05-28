"""
小天量化交易 - 因子基类
支持技术指标因子、基本面因子、另类数据因子
"""

from abc import ABC, abstractmethod
from typing import Dict, List, Optional, Tuple
from collections import deque
import numpy as np
import logging

from ..core.event import MarketEvent

logger = logging.getLogger("xtquant.factor")


class BaseFactor(ABC):
    """
    因子基类

    所有因子继承此类，实现 calculate 方法
    因子自动订阅事件并更新历史值
    """

    def __init__(self, name: str, params: dict = None, max_history: int = 1000):
        self.name = name
        self.params = params or {}
        self.max_history = max_history
        self._history: deque = deque(maxlen=max_history)
        self._current_value: Optional[float] = None
        self._last_update = 0

    @abstractmethod
    def calculate(self, event: MarketEvent) -> Optional[float]:
        """
        计算因子值
        返回 None 表示本次事件不产生因子值
        """
        pass

    def update(self, event: MarketEvent) -> Optional[float]:
        """更新因子（内部调用）"""
        val = self.calculate(event)
        if val is not None:
            self._history.append((event.timestamp, val))
            self._current_value = val
            self._last_update = event.timestamp
        return val

    def get_current(self) -> Optional[float]:
        return self._current_value

    def get_history(self, n: int = 100) -> List[Tuple[int, float]]:
        return list(self._history)[-n:]

    def get_series(self, n: int = 100) -> np.ndarray:
        """获取最近n个值作为numpy数组"""
        hist = self.get_history(n)
        return np.array([v for _, v in hist]) if hist else np.array([])

    def get_stats(self) -> dict:
        series = self.get_series()
        if len(series) == 0:
            return {"name": self.name, "count": 0}
        return {
            "name": self.name,
            "current": self._current_value,
            "count": len(series),
            "mean": float(np.mean(series)),
            "std": float(np.std(series)),
            "min": float(np.min(series)),
            "max": float(np.max(series)),
            "last_update": self._last_update
        }


class FactorPipeline:
    """
    因子管道
    组合多个因子，提供统一的特征向量输出
    """

    def __init__(self):
        self.factors: Dict[str, BaseFactor] = {}
        self._feature_names: List[str] = []

    def add_factor(self, factor: BaseFactor):
        self.factors[factor.name] = factor
        self._feature_names = list(self.factors.keys())
        logger.info(f"[FactorPipeline] 添加因子: {factor.name}")

    def on_event(self, event: MarketEvent) -> Dict[str, float]:
        """处理事件，更新所有因子"""
        results = {}
        for name, factor in self.factors.items():
            val = factor.update(event)
            if val is not None:
                results[name] = val
        return results

    def get_feature_vector(self) -> np.ndarray:
        """获取当前特征向量"""
        return np.array([f.get_current() or 0.0 for f in self.factors.values()])

    def get_feature_names(self) -> List[str]:
        return self._feature_names.copy()

    def get_feature_dict(self) -> Dict[str, float]:
        return {name: f.get_current() for name, f in self.factors.items()}

    def get_all_stats(self) -> Dict[str, dict]:
        return {name: f.get_stats() for name, f in self.factors.items()}
