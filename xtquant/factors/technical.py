"""
小天量化交易 - 技术指标因子库
包含常用技术分析因子
"""

import numpy as np
from typing import Optional
from collections import deque

from .base import BaseFactor
from ..core.event import MarketEvent, EventType


class PriceFactor(BaseFactor):
    """价格因子 - 记录最新价格"""
    def __init__(self):
        super().__init__("price")
        self._prices: deque = deque(maxlen=1000)

    def calculate(self, event: MarketEvent) -> Optional[float]:
        if event.event_type in (EventType.TICK, EventType.BAR):
            price = event.data.get("price", event.data.get("close", 0))
            if price > 0:
                self._prices.append(price)
                return price
        return None


class VolumeFactor(BaseFactor):
    """成交量因子"""
    def __init__(self):
        super().__init__("volume")
        self._volumes: deque = deque(maxlen=1000)

    def calculate(self, event: MarketEvent) -> Optional[float]:
        if event.event_type in (EventType.TICK, EventType.BAR):
            vol = event.data.get("volume", event.data.get("vol", 0))
            if vol > 0:
                self._volumes.append(vol)
                return vol
        return None


class RSIFactor(BaseFactor):
    """RSI相对强弱指标"""
    def __init__(self, period: int = 14):
        super().__init__(f"RSI_{period}", {"period": period})
        self.period = period
        self._prices: deque = deque(maxlen=period + 1)

    def calculate(self, event: MarketEvent) -> Optional[float]:
        if event.event_type not in (EventType.TICK, EventType.BAR):
            return None
        price = event.data.get("price", event.data.get("close", 0))
        if price <= 0:
            return None

        self._prices.append(price)
        if len(self._prices) < self.period + 1:
            return None

        deltas = np.diff(list(self._prices))
        gains = deltas[deltas > 0]
        losses = -deltas[deltas < 0]
        avg_gain = np.mean(gains) if len(gains) > 0 else 0
        avg_loss = np.mean(losses) if len(losses) > 0 else 0

        if avg_loss == 0:
            return 100.0
        rs = avg_gain / avg_loss
        return 100.0 - (100.0 / (1.0 + rs))


class MACDFactor(BaseFactor):
    """MACD因子 - 返回MACD柱状图值"""
    def __init__(self, fast: int = 12, slow: int = 26, signal: int = 9):
        super().__init__(f"MACD_{fast}_{slow}", {"fast": fast, "slow": slow, "signal": signal})
        self.fast = fast
        self.slow = slow
        self.signal = signal
        self._prices: deque = deque(maxlen=slow + signal + 10)

    def _ema(self, data: np.ndarray, period: int) -> np.ndarray:
        alpha = 2.0 / (period + 1)
        ema = np.zeros_like(data)
        ema[0] = data[0]
        for i in range(1, len(data)):
            ema[i] = alpha * data[i] + (1 - alpha) * ema[i-1]
        return ema

    def calculate(self, event: MarketEvent) -> Optional[float]:
        if event.event_type not in (EventType.TICK, EventType.BAR):
            return None
        price = event.data.get("price", event.data.get("close", 0))
        if price <= 0:
            return None

        self._prices.append(price)
        if len(self._prices) < self.slow + self.signal:
            return None

        prices = np.array(list(self._prices))
        ema_fast = self._ema(prices, self.fast)
        ema_slow = self._ema(prices, self.slow)
        macd_line = ema_fast[-1] - ema_slow[-1]

        # 简化signal line计算
        macd_hist = prices[-self.signal:]
        signal_line = np.mean(macd_hist)  # 简化版，实际应做EMA

        return macd_line - signal_line


class OrderBookImbalanceFactor(BaseFactor):
    """订单簿失衡因子"""
    def __init__(self, depth: int = 5):
        super().__init__(f"OB_imbalance_{depth}", {"depth": depth})
        self.depth = depth

    def calculate(self, event: MarketEvent) -> Optional[float]:
        if event.event_type != EventType.ORDERBOOK:
            return None
        bids = event.data.get("bids", [])
        asks = event.data.get("asks", [])
        bid_vol = sum(float(q) for _, q in bids[:self.depth])
        ask_vol = sum(float(q) for _, q in asks[:self.depth])
        total = bid_vol + ask_vol
        return (bid_vol - ask_vol) / total if total > 0 else 0.0


class SpreadFactor(BaseFactor):
    """买卖价差因子 (bps)"""
    def __init__(self):
        super().__init__("spread_bps")

    def calculate(self, event: MarketEvent) -> Optional[float]:
        if event.event_type != EventType.ORDERBOOK:
            return None
        bids = event.data.get("bids", [])
        asks = event.data.get("asks", [])
        if not bids or not asks:
            return None
        bid = float(bids[0][0])
        ask = float(asks[0][0])
        mid = (bid + ask) / 2
        return (ask - bid) / mid * 10000 if mid > 0 else 0.0


class VWAPFactor(BaseFactor):
    """成交量加权平均价格因子"""
    def __init__(self, window: int = 20):
        super().__init__(f"VWAP_{window}", {"window": window})
        self.window = window
        self._prices: deque = deque(maxlen=window)
        self._volumes: deque = deque(maxlen=window)

    def calculate(self, event: MarketEvent) -> Optional[float]:
        if event.event_type not in (EventType.TICK, EventType.BAR):
            return None
        price = event.data.get("price", event.data.get("close", 0))
        volume = event.data.get("volume", event.data.get("vol", 0))
        if price <= 0 or volume <= 0:
            return None

        self._prices.append(price)
        self._volumes.append(volume)

        if len(self._prices) < self.window:
            return None

        prices = np.array(list(self._prices))
        volumes = np.array(list(self._volumes))
        return np.sum(prices * volumes) / np.sum(volumes)


class MomentumFactor(BaseFactor):
    """动量因子 - N期收益率"""
    def __init__(self, period: int = 20):
        super().__init__(f"momentum_{period}", {"period": period})
        self.period = period
        self._prices: deque = deque(maxlen=period + 1)

    def calculate(self, event: MarketEvent) -> Optional[float]:
        if event.event_type not in (EventType.TICK, EventType.BAR):
            return None
        price = event.data.get("price", event.data.get("close", 0))
        if price <= 0:
            return None

        self._prices.append(price)
        if len(self._prices) < self.period + 1:
            return None

        old_price = list(self._prices)[0]
        return (price - old_price) / old_price if old_price > 0 else 0.0


class VolatilityFactor(BaseFactor):
    """波动率因子 - N期收益率标准差"""
    def __init__(self, period: int = 20):
        super().__init__(f"volatility_{period}", {"period": period})
        self.period = period
        self._returns: deque = deque(maxlen=period)
        self._last_price: Optional[float] = None

    def calculate(self, event: MarketEvent) -> Optional[float]:
        if event.event_type not in (EventType.TICK, EventType.BAR):
            return None
        price = event.data.get("price", event.data.get("close", 0))
        if price <= 0:
            return None

        if self._last_price is not None and self._last_price > 0:
            ret = (price - self._last_price) / self._last_price
            self._returns.append(ret)

        self._last_price = price

        if len(self._returns) < self.period:
            return None

        return float(np.std(list(self._returns)))
