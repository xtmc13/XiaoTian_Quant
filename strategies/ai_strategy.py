"""
小天量化交易 - AI策略示例
基于强化学习的智能交易策略
"""

import asyncio
import logging
import numpy as np
from typing import Dict, Optional
from collections import deque

from xtquant.strategy.base import BaseStrategy
from xtquant.core.data import Tick, Bar

logger = logging.getLogger("xtquant.strategy.ai")


class RLStrategy(BaseStrategy):
    """
    简化版Q-Learning强化学习策略

    状态空间: [价格区间, 失衡度区间, RSI区间, 持仓状态]
    动作空间: [持有, 买入, 卖出]
    """

    def __init__(self, symbols: list, learning_rate: float = 0.1,
                 discount_factor: float = 0.95, epsilon: float = 0.1,
                 qty: float = 0.001):
        super().__init__("RL_Qlearning", symbols)
        self.lr = learning_rate
        self.gamma = discount_factor
        self.epsilon = epsilon
        self.qty = qty

        # Q表: state -> {action: value}
        self.q_table: Dict[str, Dict[int, float]] = {}

        # 状态跟踪
        self._last_state: Optional[str] = None
        self._last_action: int = 0
        self._last_price: float = 0.0
        self._position: float = 0.0

        # 历史数据
        self._prices: deque = deque(maxlen=100)
        self._rsis: deque = deque(maxlen=20)

    def _get_state(self, symbol: str) -> str:
        """构建离散化状态"""
        tick = self.tick_cache.get(symbol)
        book = self.book_cache.get(symbol)

        if not tick or not book:
            return "unknown"

        # 价格区间 (每1000美元一个区间)
        price_bucket = int(tick.price / 1000)

        # 失衡度区间
        imb = book.imbalance()
        imb_bucket = "high" if imb > 0.3 else "low" if imb < -0.3 else "mid"

        # 趋势区间 (基于最近价格)
        trend = "up" if len(self._prices) > 10 and tick.price > np.mean(list(self._prices)[-10:]) else "down"

        # 持仓状态
        pos_bucket = "long" if self._position > 0 else "short" if self._position < 0 else "flat"

        return f"{symbol}_{price_bucket}_{imb_bucket}_{trend}_{pos_bucket}"

    def _get_q_values(self, state: str) -> Dict[int, float]:
        """获取状态的Q值"""
        if state not in self.q_table:
            self.q_table[state] = {0: 0.0, 1: 0.0, 2: 0.0}
        return self.q_table[state]

    def _choose_action(self, state: str) -> int:
        """ε-贪心选择动作"""
        if np.random.random() < self.epsilon:
            return np.random.choice([0, 1, 2])

        q_values = self._get_q_values(state)
        return max(q_values, key=q_values.get)

    def _update_q(self, state: str, action: int, reward: float, next_state: str):
        """更新Q值"""
        q_values = self._get_q_values(state)
        next_q = self._get_q_values(next_state)

        old_q = q_values[action]
        max_next = max(next_q.values())
        new_q = old_q + self.lr * (reward + self.gamma * max_next - old_q)
        q_values[action] = new_q

    async def on_tick(self, tick: Tick):
        """Tick驱动决策"""
        self._prices.append(tick.price)

        state = self._get_state(tick.symbol)
        action = self._choose_action(state)

        # 计算奖励 (基于价格变化)
        reward = 0.0
        if self._last_price > 0:
            price_change = (tick.price - self._last_price) / self._last_price
            if self._position > 0:
                reward = price_change  # 多头: 涨价=正奖励
            elif self._position < 0:
                reward = -price_change  # 空头: 跌价=正奖励

        # 更新Q表
        if self._last_state:
            self._update_q(self._last_state, self._last_action, reward, state)

        # 执行动作
        if action == 1 and self._position <= 0:
            logger.info(f"[RL] 买入信号: {tick.symbol} @ {tick.price:.2f}")
            order = await self.buy(tick.symbol, tick.price, self.qty)
            if order:
                self._position += self.qty
                self.trade_count += 1

        elif action == 2 and self._position > 0:
            logger.info(f"[RL] 卖出信号: {tick.symbol} @ {tick.price:.2f}")
            order = await self.sell(tick.symbol, tick.price, self._position)
            if order:
                self._position -= self.qty
                self.trade_count += 1

        self._last_state = state
        self._last_action = action
        self._last_price = tick.price

    async def run(self):
        """主循环"""
        logger.info("[RL] 强化学习策略启动")
        while self._running:
            await asyncio.sleep(1)

    def save_model(self, path: str):
        """保存Q表"""
        import json
        with open(path, 'w') as f:
            json.dump(self.q_table, f)
        logger.info(f"[RL] 模型已保存: {path}")

    def load_model(self, path: str):
        """加载Q表"""
        import json
        try:
            with open(path, 'r') as f:
                self.q_table = json.load(f)
            logger.info(f"[RL] 模型已加载: {path}")
        except FileNotFoundError:
            logger.warning(f"[RL] 模型文件不存在: {path}")


class FactorMLStrategy(BaseStrategy):
    """
    基于多因子的机器学习策略
    使用因子特征向量训练模型，预测价格方向
    """

    def __init__(self, symbols: list, model=None, threshold: float = 0.6, qty: float = 0.001):
        super().__init__("FactorML", symbols)
        self.model = model  # 外部传入的ML模型
        self.threshold = threshold
        self.qty = qty
        self._features: deque = deque(maxlen=100)
        self._labels: deque = deque(maxlen=100)
        self._position: float = 0.0
        self._feature_dim = 7  # 假设7个因子

    def _extract_features(self) -> np.ndarray:
        """提取当前特征向量（单个交易对，取首个有数据的交易对）"""
        if self._engine and self._engine.factor_pipeline:
            pipe_features = self._engine.factor_pipeline.get_feature_vector()
            if pipe_features is not None and len(pipe_features) > 0:
                return np.array(pipe_features[:self._feature_dim])

        # 简化版特征：取首个有数据的交易对
        for symbol in self.symbols:
            tick = self.tick_cache.get(symbol)
            book = self.book_cache.get(symbol)
            if tick and book:
                return np.array([
                    tick.price / 100000,
                    book.spread() * 100,
                    book.imbalance(),
                    0.5, 0.5, 0.5, 0.5
                ])

        return np.zeros(self._feature_dim)

    async def on_bar(self, bar: Bar):
        """K线驱动决策"""
        features = self._extract_features()

        if self.model is None:
            # 无模型时使用简单规则
            signal = self._rule_based_signal(features)
        else:
            # 使用ML模型预测
            signal = self.model.predict(features.reshape(1, -1))[0]

        # 交易逻辑
        if signal > self.threshold and self._position <= 0:
            logger.info(f"[FactorML] 强买入信号: {signal:.3f}")
            await self.buy(bar.symbol, bar.close, self.qty)
            self._position += self.qty
            self.trade_count += 1

        elif signal < -self.threshold and self._position > 0:
            logger.info(f"[FactorML] 强卖出信号: {signal:.3f}")
            await self.sell(bar.symbol, bar.close, self._position)
            self._position -= self.qty
            self.trade_count += 1

    def _rule_based_signal(self, features: np.ndarray) -> float:
        """基于规则的简单信号"""
        # 简化规则: 失衡度 > 0.2 买入, < -0.2 卖出
        if len(features) > 2:
            imbalance = features[2]
            return imbalance * 2  # 放大到 [-1, 1]
        return 0.0

    async def run(self):
        """主循环"""
        logger.info("[FactorML] 因子ML策略启动")
        while self._running:
            await asyncio.sleep(5)
