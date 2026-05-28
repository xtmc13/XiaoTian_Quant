"""
PyTorch 模型定义 — MLP / LSTM / Transformer
用于 TorchStrategy 的推理和训练
"""

import json
import logging
from pathlib import Path
from typing import Optional, Tuple, Dict, Any
from collections import deque

import numpy as np

try:
    import torch
    import torch.nn as nn
    import torch.optim as optim
    HAS_TORCH = True
except ImportError:
    HAS_TORCH = False

    class _FakeNN:
        """Placeholder so module-level class definitions don't crash when torch is absent."""
        class Module:
            def __init__(self, *args, **kwargs): pass
        class Linear: pass
        class BatchNorm1d: pass
        class ReLU: pass
        class Dropout: pass
        class Sequential: pass
        class LSTM: pass
        class TransformerEncoderLayer: pass
        class TransformerEncoder: pass
        class Parameter: pass
        class CrossEntropyLoss: pass

    class _FakeTorch:
        Tensor = object
        FloatTensor = object
        LongTensor = object
        @staticmethod
        def randn(*a, **kw): return None
        @staticmethod
        def save(*a, **kw): pass
        @staticmethod
        def load(*a, **kw): return {}

    class _FakeOptim:
        @staticmethod
        def Adam(*a, **kw): pass

    nn = _FakeNN()
    torch = _FakeTorch()
    optim = _FakeOptim()

logger = logging.getLogger("xtquant.models")


# ============================================================
#  MLP 方向预测器
# ============================================================

class PriceDirectionMLP(nn.Module):
    """
    3分类价格方向预测: 跌(0) / 平(1) / 涨(2)

    Args:
        input_dim: 特征维度 (默认7，对应7个因子)
        hidden_dims: 隐藏层维度列表
        dropout: Dropout比例
    """

    def __init__(self, input_dim: int = 7, hidden_dims: list = None,
                 dropout: float = 0.3, num_classes: int = 3):
        super().__init__()
        if not HAS_TORCH:
            raise ImportError("请安装 PyTorch: pip install torch")

        hidden_dims = hidden_dims or [64, 32]
        layers = []
        in_dim = input_dim
        for h in hidden_dims:
            layers.extend([
                nn.Linear(in_dim, h),
                nn.BatchNorm1d(h),
                nn.ReLU(),
                nn.Dropout(dropout),
            ])
            in_dim = h
        layers.append(nn.Linear(in_dim, num_classes))
        self.net = nn.Sequential(*layers)

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        return self.net(x)


# ============================================================
#  LSTM 时序预测器
# ============================================================

class LSTMPredictor(nn.Module):
    """
    LSTM 价格方向预测

    Args:
        input_dim: 每个时间步的特征维度
        hidden_dim: LSTM隐藏层维度
        num_layers: LSTM层数
        dropout: Dropout比例
        bidirectional: 是否双向
    """

    def __init__(self, input_dim: int = 7, hidden_dim: int = 64,
                 num_layers: int = 2, dropout: float = 0.3,
                 bidirectional: bool = False):
        super().__init__()
        if not HAS_TORCH:
            raise ImportError("请安装 PyTorch: pip install torch")

        self.lstm = nn.LSTM(
            input_dim, hidden_dim, num_layers,
            batch_first=True, dropout=dropout if num_layers > 1 else 0,
            bidirectional=bidirectional,
        )
        self.fc = nn.Sequential(
            nn.Linear(hidden_dim * (2 if bidirectional else 1), 32),
            nn.ReLU(),
            nn.Dropout(dropout),
            nn.Linear(32, 3),
        )

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        # x: (batch, seq_len, input_dim)
        out, (hn, cn) = self.lstm(x)
        last_out = out[:, -1, :]  # 最后一个时间步
        return self.fc(last_out)


# ============================================================
#  Simple Transformer
# ============================================================

class SimpleTransformer(nn.Module):
    """
    简易Transformer预测器

    Args:
        input_dim: 特征维度
        d_model: Transformer维度
        nhead: 注意力头数
        num_layers: Encoder层数
        dropout: Dropout比例
        max_seq_len: 最大序列长度
    """

    def __init__(self, input_dim: int = 7, d_model: int = 64, nhead: int = 4,
                 num_layers: int = 2, dropout: float = 0.3, max_seq_len: int = 100):
        super().__init__()
        if not HAS_TORCH:
            raise ImportError("请安装 PyTorch: pip install torch")

        self.input_proj = nn.Linear(input_dim, d_model)
        self.pos_embedding = nn.Parameter(torch.randn(1, max_seq_len, d_model))

        encoder_layer = nn.TransformerEncoderLayer(
            d_model=d_model, nhead=nhead, dropout=dropout,
            batch_first=True,
        )
        self.transformer = nn.TransformerEncoder(encoder_layer, num_layers=num_layers)
        self.fc = nn.Sequential(
            nn.Linear(d_model, 32),
            nn.ReLU(),
            nn.Dropout(dropout),
            nn.Linear(32, 3),
        )

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        # x: (batch, seq_len, input_dim)
        x = self.input_proj(x)  # (batch, seq_len, d_model)
        seq_len = x.size(1)
        x = x + self.pos_embedding[:, :seq_len, :]
        x = self.transformer(x)
        last = x[:, -1, :]
        return self.fc(last)


# ============================================================
#  Price Regressor (回归代替分类)
# ============================================================

class PriceRegressor(nn.Module):
    """价格回归器 — 预测未来收益率"""

    def __init__(self, input_dim: int = 7, hidden_dims: list = None,
                 dropout: float = 0.3):
        super().__init__()
        if not HAS_TORCH:
            raise ImportError("请安装 PyTorch: pip install torch")

        hidden_dims = hidden_dims or [64, 32, 16]
        layers = []
        in_dim = input_dim
        for h in hidden_dims:
            layers.extend([
                nn.Linear(in_dim, h),
                nn.BatchNorm1d(h),
                nn.ReLU(),
                nn.Dropout(dropout),
            ])
            in_dim = h
        layers.append(nn.Linear(in_dim, 1))  # 输出收益率
        self.net = nn.Sequential(*layers)

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        return self.net(x)


# ============================================================
#  模型训练器
# ============================================================

class ModelTrainer:
    """模型训练器 — 支持在线增量训练和离线批量训练"""

    def __init__(self, model: nn.Module, lr: float = 0.001,
                 model_dir: str = "./data/models"):
        if not HAS_TORCH:
            raise ImportError("请安装 PyTorch: pip install torch")

        self.model = model
        self.model_dir = Path(model_dir)
        self.model_dir.mkdir(parents=True, exist_ok=True)

        self.optimizer = optim.Adam(model.parameters(), lr=lr)
        self.criterion = nn.CrossEntropyLoss()
        self._samples: deque = deque(maxlen=10000)  # (features, label)
        self._train_count = 0

    def add_sample(self, features: np.ndarray, label: int):
        """添加训练样本"""
        self._samples.append((features.copy(), label))

    def add_labeled_sample(self, features: np.ndarray, label: int):
        """添加标注样本 (同 add_sample)"""
        self.add_sample(features, label)

    def train(self, epochs: int = 5, batch_size: int = 32) -> Dict[str, float]:
        """训练模型"""
        if len(self._samples) < batch_size:
            return {"error": f"样本不足 ({len(self._samples)} < {batch_size})"}

        self.model.train()
        device = next(self.model.parameters()).device

        # 准备数据
        all_features = np.array([s[0] for s in self._samples])
        all_labels = np.array([s[1] for s in self._samples])

        n_samples = len(self._samples)
        total_loss = 0.0
        correct = 0

        for epoch in range(epochs):
            indices = np.random.permutation(n_samples)
            epoch_loss = 0.0

            for i in range(0, n_samples, batch_size):
                batch_idx = indices[i:i + batch_size]
                x = torch.FloatTensor(all_features[batch_idx]).to(device)
                y = torch.LongTensor(all_labels[batch_idx]).to(device)

                self.optimizer.zero_grad()
                logits = self.model(x.unsqueeze(0) if x.dim() == 1 else x)
                loss = self.criterion(logits, y)
                loss.backward()
                self.optimizer.step()

                epoch_loss += loss.item()
                preds = logits.argmax(dim=1)
                correct += (preds == y).sum().item()

            total_loss += epoch_loss

        self._train_count += 1
        avg_loss = total_loss / (epochs * max(1, n_samples / batch_size))
        accuracy = correct / (epochs * n_samples) if n_samples > 0 else 0

        return {
            "loss": round(avg_loss, 6),
            "accuracy": round(accuracy * 100, 2),
            "samples": n_samples,
            "train_count": self._train_count,
        }

    def save(self, filename: str):
        """保存模型"""
        path = self.model_dir / filename
        torch.save({
            "model_state_dict": self.model.state_dict(),
            "optimizer_state_dict": self.optimizer.state_dict(),
            "train_count": self._train_count,
        }, path)
        logger.info(f"[Trainer] 模型已保存: {path}")

    def load(self, filename: str):
        """加载模型"""
        path = self.model_dir / filename
        if not path.exists():
            return False
        checkpoint = torch.load(path, map_location="cpu")
        self.model.load_state_dict(checkpoint["model_state_dict"])
        self.optimizer.load_state_dict(checkpoint["optimizer_state_dict"])
        self._train_count = checkpoint.get("train_count", 0)
        logger.info(f"[Trainer] 模型已加载: {path}")
        return True

    def get_stats(self) -> dict:
        return {
            "samples": len(self._samples),
            "train_count": self._train_count,
        }
