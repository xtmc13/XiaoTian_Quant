# -*- coding: utf-8 -*-
"""Technical factor tests — verify indicator correctness."""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import numpy as np
from xtquant.factors.technical import (
    RSIFactor, MACDFactor, OrderBookImbalanceFactor,
    SpreadFactor, VWAPFactor, MomentumFactor, VolatilityFactor
)
from xtquant.core.event import MarketEvent, EventType


def test_rsi_factor():
    rsi = RSIFactor(period=14)
    price = 100
    for i in range(20):
        price += 5
        event = MarketEvent(
            EventType.TICK, "BACKTEST", "BTCUSDT", 1716600000000 + i * 1000,
            {"price": price, "volume": 1}
        )
        rsi.update(event)
    assert rsi.get_current() is not None, "RSI should be calculated"
    assert rsi.get_current() > 50, f"RSI should > 50 in uptrend, got {rsi.get_current()}"
    print(f"PASS: RSI = {rsi.get_current():.2f}")


def test_orderbook_imbalance():
    obi = OrderBookImbalanceFactor(depth=5)
    event = MarketEvent(
        EventType.ORDERBOOK, "BINANCE", "BTCUSDT", 1716600000000,
        {"bids": [[68000, 10], [67990, 5], [67980, 3], [67970, 2], [67960, 1]],
         "asks": [[68010, 1], [68020, 2], [68030, 3], [68040, 4], [68050, 5]]}
    )
    val = obi.update(event)
    assert val is not None
    assert val > 0, f"Buy-side dominance should > 0, got {val}"

    event2 = MarketEvent(
        EventType.ORDERBOOK, "BINANCE", "BTCUSDT", 1716600001000,
        {"bids": [[68000, 1], [67990, 2], [67980, 3], [67970, 4], [67960, 5]],
         "asks": [[68010, 10], [68020, 5], [68030, 3], [68040, 2], [68050, 1]]}
    )
    val2 = obi.update(event2)
    assert val2 < 0, f"Sell-side dominance should < 0, got {val2}"
    print(f"PASS: OBI buy={val:.4f} sell={val2:.4f}")


def test_spread_factor():
    spread = SpreadFactor()
    event = MarketEvent(
        EventType.ORDERBOOK, "BINANCE", "BTCUSDT", 1716600000000,
        {"bids": [[68000, 1]], "asks": [[68010, 1]]}
    )
    val = spread.update(event)
    assert val is not None
    expected = (68010 - 68000) / 68000 * 10000
    assert abs(val - expected) < 0.1, f"Spread mismatch: {val} vs {expected}"
    print(f"PASS: Spread = {val:.2f} bps (expected {expected:.2f})")


def test_momentum_factor():
    mom = MomentumFactor(period=10)
    price = 100
    for i in range(15):
        price += 2
        event = MarketEvent(
            EventType.TICK, "BACKTEST", "BTCUSDT", 1716600000000 + i * 1000,
            {"price": price, "volume": 1}
        )
        mom.update(event)
    assert mom.get_current() is not None
    assert mom.get_current() > 0, f"Momentum should > 0 in uptrend, got {mom.get_current()}"
    print(f"PASS: Momentum = {mom.get_current():.4f}")


def test_factor_pipeline():
    from xtquant.factors.base import FactorPipeline
    pipeline = FactorPipeline()
    pipeline.add_factor(RSIFactor(14))
    pipeline.add_factor(MACDFactor(12, 26, 9))
    pipeline.add_factor(OrderBookImbalanceFactor(5))

    for i in range(30):
        event = MarketEvent(
            EventType.TICK, "BACKTEST", "BTCUSDT", 1716600000000 + i * 1000,
            {"price": 68000 + i * 10, "volume": 100}
        )
        pipeline.on_event(event)

    names = pipeline.get_feature_names()
    assert len(names) == 3, f"Expected 3 factors, got {len(names)}"
    print(f"PASS: Pipeline with {len(names)} factors: {names}")


if __name__ == "__main__":
    test_rsi_factor()
    test_orderbook_imbalance()
    test_spread_factor()
    test_momentum_factor()
    test_factor_pipeline()
    print("\nAll factor tests passed.")
