"""
小天量化交易 - PyTorch 策略 (新版)

完全替代旧版 FactorMLStrategy，使用 PyTorch 模型进行推理和训练

使用方式:
  from xtquant.models.pytorch_models import PriceDirectionMLP
  strategy = TorchStrategy(["BTCUSDT"], model=PriceDirectionMLP(input_dim=7))
"""

import asyncio
import logging
import numpy as np
from collections import deque
from pathlib import Path

from xtquant.strategy.base import BaseStrategy
from xtquant.core.data import Tick, Bar

try:
    import torch
    HAS_TORCH = True
except ImportError:
    HAS_TORCH = False

from xtquant.models import (
    PriceDirectionMLP, LSTMPredictor, SimpleTransformer,
    ModelTrainer,
)

logger = logging.getLogger("xtquant.strategy.torch")


# ============================================================
#  新版 TorchStrategy — 接入 PyTorch 模型
# ============================================================

class TorchStrategy(BaseStrategy):
    """
    基于 PyTorch 的交易策略
    
    数据流: 因子管道 → 特征向量 → PyTorch模型 → 交易信号
    
    支持:
      - MLP 方向预测 (PriceDirectionMLP)
      - LSTM 时序预测 (LSTMPredictor)
      - Transformer 预测 (SimpleTransformer)
      - 回测自动标注训练
      - 在线增量训练（可选）
    """

    def __init__(self, symbols: list, model=None,
                 threshold: float = 0.55, qty: float = 0.001,
                 feature_window: int = 20, 
                 train_interval: int = 500,
                 auto_train: bool = False,
                 model_path: str = "./data/models/torch_strategy.pt"):
        
        super().__init__("TorchStrategy", symbols)
        
        if not HAS_TORCH:
            raise ImportError("请安装 PyTorch: pip install torch")
        
        self.threshold = threshold          # 信号阈值 [0.5, 1.0]
        self.qty = qty                       # 每次交易量
        self.feature_window = feature_window # LSTM/Transformer 的序列长度
        self.train_interval = train_interval # 每N个样本训练一次
        self.auto_train = auto_train         # 是否自动训练
        self.model_path = Path(model_path)
        
        # 模型
        self.model = model or PriceDirectionMLP(input_dim=7)
        
        # 训练器
        self.trainer = ModelTrainer(self.model)
        self.trainer.model_dir.mkdir(parents=True, exist_ok=True)
        
        # 加载已有模型
        self.model_path.parent.mkdir(parents=True, exist_ok=True)
        if self.model_path.exists():
            self.trainer.load("torch_strategy.pt")
            logger.info(f"[Torch] 已加载预训练模型: {self.model_path}")
        
        # 时序特征缓冲区 (LSTM/Transformer 需要)
        self._feature_history: deque = deque(maxlen=feature_window)
        self._price_history: deque = deque(maxlen=feature_window + 50)
        self._label_window = 10  # 用未来10步的价格变化打标签
        
        # 策略状态
        self._position: float = 0.0
        self._last_signal: float = 0.0
        self._sample_count = 0
        self._train_count = 0

    # ============================================================
    #  核心: 特征提取
    # ============================================================

    def _extract_features(self) -> np.ndarray:
        """
        从因子管道 + 缓存中提取特征向量
        
        特征维度 (from FactorPipeline):
          0: RSI_14
          1: MACD_12_26
          2: OB_imbalance_5
          3: spread_bps
          4: VWAP_20
          5: momentum_20
          6: volatility_20
        
        Returns:
            np.ndarray shape [input_dim]
        """
        features = []
        
        # 1. 从引擎的因子管道获取实时因子值
        if self._engine and self._engine.factor_pipeline:
            fv = self._engine.factor_pipeline.get_feature_vector()
            features.extend(fv.tolist())
        
        # 2. 兜底: 如果因子管道不可用，从数据缓存计算
        if len(features) == 0:
            for symbol in self.symbols:
                tick = self.tick_cache.get(symbol)
                book = self.book_cache.get(symbol)
                
                if tick is None:
                    return np.zeros(7)
                
                # 价格特征
                features.append(tick.price / 100000)
                
                # 价差 (bps)
                if book:
                    features.append(book.spread_bps() / 100)
                    features.append(book.imbalance())
                else:
                    features.extend([0, 0])
                
                # 动量 (最近价格变化)
                self._price_history.append(tick.price)
                if len(self._price_history) >= 20:
                    prices = list(self._price_history)
                    momentum = (prices[-1] - prices[-20]) / prices[-20]
                    volatility = np.std(prices[-20:]) / np.mean(prices[-20:])
                else:
                    momentum, volatility = 0, 0
                
                features.append(momentum)
                features.append(volatility)
                
                # 填充到 7 维
                while len(features) < 7:
                    features.append(0)
        
        # 归一化
        features = np.array(features[:7], dtype=np.float32)
        features = np.nan_to_num(features, nan=0.0, posinf=0.0, neginf=0.0)
        
        return features

    # ============================================================
    #  核心: 模型推理
    # ============================================================

    def _infer(self, features: np.ndarray) -> float:
        """
        模型推理 → 交易信号
        
        Args:
            features: (input_dim,)  or  (seq_len, input_dim) for LSTM/Transformer
        
        Returns:
            float: 信号值 [-1.0, 1.0]
                   正=看多  负=看空  0=中性
        """
        self.model.eval()
        with torch.no_grad():
            if isinstance(self.model, (LSTMPredictor, SimpleTransformer)):
                # 时序模型: 需要 (seq_len, input_dim)
                x = torch.FloatTensor(features).unsqueeze(0)  # [1, seq, dim]
            else:
                # MLP 模型: 需要 (input_dim,)
                x = torch.FloatTensor(features).unsqueeze(0)  # [1, dim]
            
            logits = self.model(x)
            probs = torch.softmax(logits, dim=-1)[0]
            
            # probs = [P(down), P(flat), P(up)]
            p_down, p_flat, p_up = probs.tolist()
            
            # 转化为信号值 [-1, 1]
            signal = p_up - p_down
            
            return float(signal)

    # ============================================================
    #  核心: 自动标注 + 训练
    # ============================================================

    def _make_label(self, current_price: float, future_price: float) -> int:
        """根据价格变化生成标签: 0=跌 1=平 2=涨"""
        if future_price <= 0 or current_price <= 0:
            return 1
        
        change_pct = (future_price - current_price) / current_price
        
        # 阈值: 涨跌幅超过 0.1% 才算方向性变化
        thresh = 0.001
        if change_pct > thresh:
            return 2  # 涨
        elif change_pct < -thresh:
            return 0  # 跌
        else:
            return 1  # 平

    def _auto_label(self, features: np.ndarray, current_price: float):
        """
        自动标注逻辑:
          收集特征 → 等 label_window 个 tick 后 → 用未来价格打标签
        """
        self._sample_count += 1
        
        # 存入特征和当前价格 (配对存储)
        # 简化: 用 price_history 的旧价格 vs 当前价格
        self._price_history.append((features, current_price))
        
        # 每 train_interval 个样本触发一次训练
        if self.auto_train and self._sample_count % self.train_interval == 0:
            self._trigger_training()

    def _trigger_training(self):
        """触发训练"""
        logger.info(f"[Torch] 自动训练触发 | 样本数: {self._sample_count}")
        
        # 从 price_history 生成标签
        pairs = list(self._price_history)
        if len(pairs) < self._label_window + 10:
            return
        
        for i in range(len(pairs) - self._label_window):
            feat, _ = pairs[i]
            future_feat, future_price = pairs[i + self._label_window]
            label = self._make_label(feat, future_price)
            self.trainer.add_labeled_sample(feat, label)
        
        result = self.trainer.train(epochs=5, batch_size=32)
        self._train_count += 1

    # ============================================================
    #  事件回调
    # ============================================================

    async def on_tick(self, tick: Tick):
        """Tick驱动推理 + 交易"""
        features = self._extract_features()
        
        # 时序特征缓存
        self._feature_history.append(features)
        
        # 需要足够时序数据
        if isinstance(self.model, (LSTMPredictor, SimpleTransformer)):
            if len(self._feature_history) < self.feature_window:
                return
            infer_features = np.array(list(self._feature_history))
        else:
            infer_features = features
        
        # 推理
        signal = self._infer(infer_features)
        self._last_signal = signal
        
        # 自动标注 (回测场景)
        if self.auto_train:
            self._auto_label(features, tick.price)
        
        # 🔥 交易决策
        if abs(signal) < self.threshold - 0.5:  # 信号强度不足
            return
        
        if signal > 0 and self._position <= 0:
            # 买入信号
            logger.info(f"[Torch] 🔵 买入 {tick.symbol} @ {tick.price:.2f} | signal={signal:.3f}")
            order = await self.buy(tick.symbol, tick.price, self.qty)
            if order:
                self._position += self.qty
                self.trade_count += 1
        
        elif signal < 0 and self._position > 0:
            # 卖出信号
            logger.info(f"[Torch] 🔴 卖出 {tick.symbol} @ {tick.price:.2f} | signal={signal:.3f}")
            order = await self.sell(tick.symbol, tick.price, self._position)
            if order:
                self._position = 0
                self.trade_count += 1

    async def on_bar(self, bar: Bar):
        """K线驱动 (与 on_tick 逻辑相同, 但频率更低)"""
        features = self._extract_features()
        self._feature_history.append(features)
        
        if isinstance(self.model, (LSTMPredictor, SimpleTransformer)):
            if len(self._feature_history) < self.feature_window:
                return
            infer_features = np.array(list(self._feature_history))
        else:
            infer_features = features
        
        signal = self._infer(infer_features)
        self._last_signal = signal
        
        # 交易
        if signal > 0 and self._position <= 0:
            logger.info(f"[Torch] 🔵 买入 {bar.symbol} @ {bar.close:.2f} | signal={signal:.3f}")
            order = await self.buy(bar.symbol, bar.close, self.qty)
            if order:
                self._position += self.qty
                self.trade_count += 1
        
        elif signal < 0 and self._position > 0:
            logger.info(f"[Torch] 🔴 卖出 {bar.symbol} @ {bar.close:.2f} | signal={signal:.3f}")
            order = await self.sell(bar.symbol, bar.close, self._position)
            if order:
                self._position = 0
                self.trade_count += 1

    # ============================================================
    #  生命周期
    # ============================================================

    async def run(self):
        """主循环"""
        model_type = type(self.model).__name__
        logger.info(f"[Torch] 策略启动 | 模型: {model_type} | "
                   f"阈值: {self.threshold} | 自动训练: {self.auto_train}")
        
        while self._running:
            await asyncio.sleep(5)

    async def stop(self):
        """停止时自动保存模型"""
        self.trainer.save("torch_strategy.pt")
        logger.info(f"[Torch] 模型已保存 | 交易次数: {self.trade_count} | "
                   f"训练次数: {self._train_count}")
        await super().stop()

    def get_stats(self) -> dict:
        stats = super().get_stats()
        stats.update({
            "model_type": type(self.model).__name__,
            "threshold": self.threshold,
            "last_signal": round(self._last_signal, 4),
            "feature_window": self.feature_window,
            "position": self._position,
            "auto_train": self.auto_train,
            "sample_count": self._sample_count,
            "train_count": self._train_count,
            "trainer": self.trainer.get_stats()
        })
        return stats


# ============================================================
#  工厂函数: 快速创建不同模型配置的策略
# ============================================================

def create_mlp_strategy(symbols: list, **kwargs) -> TorchStrategy:
    """创建 MLP 策略 (最快, 适合高频)"""
    model = PriceDirectionMLP(input_dim=7, hidden_dims=[64, 32], dropout=0.3)
    return TorchStrategy(symbols, model=model, **kwargs)


def create_lstm_strategy(symbols: list, **kwargs) -> TorchStrategy:
    """创建 LSTM 策略 (适合中频)"""
    model = LSTMPredictor(input_dim=7, hidden_dim=64, num_layers=2)
    return TorchStrategy(symbols, model=model, feature_window=30, **kwargs)


def create_transformer_strategy(symbols: list, **kwargs) -> TorchStrategy:
    """创建 Transformer 策略 (适合低频, 捕捉复杂模式)"""
    model = SimpleTransformer(input_dim=7, d_model=64, nhead=4, num_layers=2)
    return TorchStrategy(symbols, model=model, feature_window=20, **kwargs)
