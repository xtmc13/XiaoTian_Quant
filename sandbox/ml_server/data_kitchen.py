"""
DataKitchen — Data preparation pipeline (aligns with FreqAI DataKitchen).
Handles: OHLCV→DataFrame conversion, standardization, train/test split.
"""
import numpy as np
import pandas as pd
from typing import Any, Dict, List, Optional, Tuple
from sklearn.preprocessing import StandardScaler


class DataKitchen:
    """Prepares data for ML training and prediction."""

    def __init__(self):
        self.scaler = StandardScaler()

    def bars_to_dataframe(self, bars: List[Dict[str, Any]]) -> pd.DataFrame:
        """Convert OHLCV bar dicts to a pandas DataFrame with standard columns."""
        if not bars:
            return pd.DataFrame()

        df = pd.DataFrame(bars)

        # Normalize column names
        col_map = {
            "time": "timestamp", "timestamp": "timestamp",
            "open": "open", "o": "open",
            "high": "high", "h": "high",
            "low": "low", "l": "low",
            "close": "close", "c": "close",
            "volume": "volume", "v": "volume",
        }
        df = df.rename(columns={k: v for k, v in col_map.items() if k in df.columns})

        # Ensure numeric types
        for col in ["open", "high", "low", "close", "volume"]:
            if col in df.columns:
                df[col] = pd.to_numeric(df[col], errors="coerce")

        # Drop rows with NaN in price columns
        df = df.dropna(subset=["open", "high", "low", "close"])

        # Sort by timestamp if available
        if "timestamp" in df.columns:
            df = df.sort_values("timestamp").reset_index(drop=True)

        return df

    def prepare_data(
        self, df: pd.DataFrame, label_col: str, train_ratio: float = 0.8
    ) -> Tuple[Dict[str, np.ndarray], Dict[str, np.ndarray]]:
        """Split data into train/test sets, standardize features.

        Returns:
            train_data: {"X": np.ndarray, "y": np.ndarray}
            test_data: {"X": np.ndarray, "y": np.ndarray}
        """
        if label_col not in df.columns:
            raise ValueError(f"Label column '{label_col}' not found in dataframe")

        # Drop rows with NaN labels
        df = df.dropna(subset=[label_col])

        # Separate features from labels
        feature_cols = [c for c in df.columns if c not in
                       [label_col, "timestamp", "symbol", "interval",
                        "open", "high", "low", "close", "volume"]]
        feature_cols = [c for c in feature_cols if df[c].dtype in
                       [np.float64, np.float32, np.int64, np.int32]]

        if len(feature_cols) == 0:
            raise ValueError("No numeric feature columns found")

        X = df[feature_cols].values.astype(np.float64)
        y = df[label_col].values.astype(np.float64)

        # Handle NaN/Inf in features
        X = np.nan_to_num(X, nan=0.0, posinf=0.0, neginf=0.0)

        # Train/test split (chronological — no shuffle for time series)
        split_idx = int(len(X) * train_ratio)
        X_train, X_test = X[:split_idx], X[split_idx:]
        y_train, y_test = y[:split_idx], y[split_idx:]

        # Standardize features
        self.scaler.fit(X_train)
        X_train = self.scaler.transform(X_train)
        X_test = self.scaler.transform(X_test)

        return {
            "X": X_train,
            "y": y_train,
        }, {
            "X": X_test,
            "y": y_test,
        }
