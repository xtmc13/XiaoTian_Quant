"""
LabelCreator — Converts OHLCV data into supervised learning labels.
Aligns with FreqAI: regression (future return), classification (up/down),
multi-class (strong up / weak up / flat / weak down / strong down).
"""
import numpy as np
import pandas as pd
from typing import Any, Dict, Optional


class LabelCreator:
    """Creates target labels for supervised ML from price data."""

    def __init__(self, config: Optional[Dict[str, Any]] = None):
        self.config = config or {}
        self.label_type = self.config.get("label_type", "regression")  # regression, classification, multi_class
        self.horizon = self.config.get("horizon", 5)                   # predict N bars ahead
        self.threshold = self.config.get("threshold", 0.01)            # 1% for classification

    def create_labels(self, df: pd.DataFrame) -> tuple:
        """Create labels and return (DataFrame, label_column_name).

        Returns:
            (df_with_labels, label_column_name)
        """
        if "close" not in df.columns:
            raise ValueError("DataFrame must have 'close' column")

        close = df["close"].values
        label_col = f"label_{self.label_type}_{self.horizon}"

        # Future return: percentage change over horizon
        future_close = np.roll(close, -self.horizon)
        future_return = (future_close - close) / close

        # Last N bars will have NaN labels (no future data)
        future_return[-self.horizon:] = np.nan

        if self.label_type == "regression":
            df[label_col] = future_return

        elif self.label_type == "classification":
            # Binary: 1 = up, 0 = down
            df[label_col] = np.where(future_return > self.threshold, 1,
                            np.where(future_return < -self.threshold, 0, np.nan))

        elif self.label_type == "multi_class":
            # 0 = strong down, 1 = weak down, 2 = flat, 3 = weak up, 4 = strong up
            df[label_col] = np.where(future_return > self.threshold * 3, 4,
                            np.where(future_return > self.threshold, 3,
                            np.where(future_return > -self.threshold, 2,
                            np.where(future_return > -self.threshold * 3, 1, 0))))

        return df, label_col
