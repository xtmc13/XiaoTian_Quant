"""
FeatureEngine — Automatic feature generation from OHLCV data.
Aligns with FreqAI's feature pipeline: price, volume, volatility,
trend, statistical features, and multi-timeframe aggregation.
"""
import numpy as np
import pandas as pd
from typing import Any, Dict, List, Optional


class FeatureEngine:
    """Generates technical and statistical features from OHLCV bars."""

    def __init__(self, config: Optional[Dict[str, Any]] = None):
        self.config = config or {}
        self._feature_names: List[str] = []

        # Default: enable all feature groups
        self.enable_price = self.config.get("price", True)
        self.enable_volume = self.config.get("volume", True)
        self.enable_volatility = self.config.get("volatility", True)
        self.enable_trend = self.config.get("trend", True)
        self.enable_statistical = self.config.get("statistical", True)

        # Periods for rolling calculations
        self.periods = self.config.get("periods", [5, 10, 20, 50])

    def transform(self, df: pd.DataFrame) -> pd.DataFrame:
        """Apply all feature engineering to the DataFrame."""
        df = df.copy()
        self._feature_names = []

        if len(df) < max(self.periods) + 1:
            return df  # Not enough data

        # Ensure basic columns exist
        for col in ["open", "high", "low", "close", "volume"]:
            if col not in df.columns:
                df[col] = 0.0

        # 1. Price features
        if self.enable_price:
            df = self._add_price_features(df)

        # 2. Volume features
        if self.enable_volume:
            df = self._add_volume_features(df)

        # 3. Volatility features
        if self.enable_volatility:
            df = self._add_volatility_features(df)

        # 4. Trend features
        if self.enable_trend:
            df = self._add_trend_features(df)

        # 5. Statistical features
        if self.enable_statistical:
            df = self._add_statistical_features(df)

        return df

    def get_feature_names(self) -> List[str]:
        """Return the list of generated feature column names."""
        return self._feature_names

    # ── Price Features ────────────────────────────────────────────

    def _add_price_features(self, df: pd.DataFrame) -> pd.DataFrame:
        c = df["close"]

        # Returns
        for p in self.periods:
            name = f"return_{p}"
            df[name] = c.pct_change(p)
            self._feature_names.append(name)

        # Log returns
        for p in self.periods:
            name = f"log_return_{p}"
            df[name] = np.log(c / c.shift(p))
            self._feature_names.append(name)

        # Price vs moving average
        for p in self.periods:
            name = f"price_vs_ma_{p}"
            ma = c.rolling(p).mean()
            df[name] = (c - ma) / ma
            self._feature_names.append(name)

        # High-low range
        name = "hl_range"
        df[name] = (df["high"] - df["low"]) / df["close"]
        self._feature_names.append(name)

        # Open-close gap
        name = "oc_gap"
        df[name] = (df["close"] - df["open"]) / df["open"]
        self._feature_names.append(name)

        return df

    # ── Volume Features ───────────────────────────────────────────

    def _add_volume_features(self, df: pd.DataFrame) -> pd.DataFrame:
        v = df["volume"]

        # Volume ratio vs moving average
        for p in self.periods:
            name = f"volume_ratio_{p}"
            ma = v.rolling(p).mean()
            df[name] = v / ma.replace(0, 1)
            self._feature_names.append(name)

        # Volume change
        for p in self.periods:
            name = f"volume_change_{p}"
            df[name] = v.pct_change(p)
            self._feature_names.append(name)

        # OBV (On-Balance Volume)
        name = "obv"
        direction = np.where(df["close"] > df["close"].shift(1), 1,
                    np.where(df["close"] < df["close"].shift(1), -1, 0))
        df[name] = (direction * v).cumsum()
        self._feature_names.append(name)

        return df

    # ── Volatility Features ───────────────────────────────────────

    def _add_volatility_features(self, df: pd.DataFrame) -> pd.DataFrame:
        c = df["close"]
        returns = c.pct_change()

        # Rolling volatility (std of returns)
        for p in self.periods:
            name = f"volatility_{p}"
            df[name] = returns.rolling(p).std() * np.sqrt(p)
            self._feature_names.append(name)

        # ATR (Average True Range) proxy
        tr = pd.concat([
            df["high"] - df["low"],
            (df["high"] - df["close"].shift(1)).abs(),
            (df["low"] - df["close"].shift(1)).abs(),
        ], axis=1).max(axis=1)

        for p in self.periods:
            name = f"atr_{p}"
            df[name] = tr.rolling(p).mean() / c
            self._feature_names.append(name)

        # Bollinger Band position
        for p in self.periods:
            name = f"bb_position_{p}"
            ma = c.rolling(p).mean()
            std = c.rolling(p).std()
            df[name] = (c - ma) / std.replace(0, 1)
            self._feature_names.append(name)

        # Bollinger Band width
        for p in self.periods:
            name = f"bb_width_{p}"
            ma = c.rolling(p).mean()
            std = c.rolling(p).std()
            df[name] = (2 * std) / ma.replace(0, 1)
            self._feature_names.append(name)

        return df

    # ── Trend Features ────────────────────────────────────────────

    def _add_trend_features(self, df: pd.DataFrame) -> pd.DataFrame:
        c = df["close"]

        # Moving average crossover signals
        for i, p1 in enumerate(self.periods[:-1]):
            p2 = self.periods[i + 1]
            name = f"ma_cross_{p1}_{p2}"
            ma1 = c.rolling(p1).mean()
            ma2 = c.rolling(p2).mean()
            df[name] = (ma1 - ma2) / ma2.replace(0, 1)
            self._feature_names.append(name)

        # RSI proxy (price momentum)
        for p in self.periods:
            name = f"momentum_{p}"
            df[name] = c - c.shift(p)
            self._feature_names.append(name)

        # MACD components
        ema12 = c.ewm(span=12, adjust=False).mean()
        ema26 = c.ewm(span=26, adjust=False).mean()
        df["macd"] = ema12 - ema26
        self._feature_names.append("macd")

        df["macd_signal"] = df["macd"].ewm(span=9, adjust=False).mean()
        self._feature_names.append("macd_signal")

        df["macd_hist"] = df["macd"] - df["macd_signal"]
        self._feature_names.append("macd_hist")

        # ADX proxy (directional movement)
        for p in self.periods:
            name = f"adx_proxy_{p}"
            plus_dm = df["high"].diff().clip(lower=0)
            minus_dm = (-df["low"].diff()).clip(lower=0)
            df[name] = (plus_dm - minus_dm).rolling(p).mean() / c
            self._feature_names.append(name)

        return df

    # ── Statistical Features ──────────────────────────────────────

    def _add_statistical_features(self, df: pd.DataFrame) -> pd.DataFrame:
        c = df["close"]
        returns = c.pct_change()

        # Z-score (price deviation from rolling mean)
        for p in self.periods:
            name = f"zscore_{p}"
            ma = c.rolling(p).mean()
            std = c.rolling(p).std()
            df[name] = (c - ma) / std.replace(0, 1)
            self._feature_names.append(name)

        # Skewness of returns
        for p in self.periods:
            name = f"skew_{p}"
            df[name] = returns.rolling(p).skew()
            self._feature_names.append(name)

        # Kurtosis of returns
        for p in self.periods:
            name = f"kurt_{p}"
            df[name] = returns.rolling(p).kurt()
            self._feature_names.append(name)

        return df
