# -*- coding: utf-8 -*-
"""Backtest tests — verify backtest engine and strategy execution."""

import asyncio
import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import numpy as np
from xtquant.exchanges.backtest import BacktestExchange, BacktestConfig
from xtquant.core.data import Bar


def make_bars(base_price=68000, count=100, trend=2):
    bars = []
    for i in range(count):
        noise = np.random.normal(0, 100)
        price = base_price + noise + i * trend
        bars.append(Bar(
            "BACKTEST", "BTCUSDT", "1m",
            price - 30, price + 60, price - 60, price,
            50 + i, quote_volume=price * (50 + i),
            timestamp=1716600000000 + i * 60000
        ))
    return bars


def test_backtest_basic():
    bars = make_bars(count=100)
    config = BacktestConfig(
        initial_balance={"USDT": 100000.0},
        fee_rate=0.001,
        slippage=0.0005
    )
    backtest = BacktestExchange(config)
    backtest.load_bars("BTCUSDT", bars)

    assert len(backtest._events) == 100, f"Event count wrong: {len(backtest._events)}"
    print(f"PASS: Loaded {len(backtest._events)} events")

    balance = asyncio.run(backtest.get_balance())
    assert balance["USDT"] == 100000.0, f"Initial balance wrong: {balance}"
    print(f"PASS: Initial balance = {balance['USDT']} USDT")

    async def place():
        order = await backtest.place_order("BTCUSDT", "BUY", "LIMIT", 68000, 0.001)
        assert order.status == "FILLED", f"Order status: {order.status}"
        assert order.filled_qty == 0.001, f"Filled qty: {order.filled_qty}"
        return order

    order = asyncio.run(place())
    print(f"PASS: Order filled {order.filled_qty} @ {order.price:.2f}")

    position = asyncio.run(backtest.get_position("BTCUSDT"))
    assert position is not None
    assert position.size == 0.001, f"Position size wrong: {position.size}"
    print(f"PASS: Position = {position.size} BTC")

    report = backtest.get_performance_report()
    assert "total_return" in report, "Missing total_return in report"
    print(f"PASS: Report generated successfully")
    for k, v in report.items():
        print(f"  {k}: {v}")


def test_backtest_strategy():
    from strategies.market_making import SimpleBreakoutStrategy
    from xtquant.core.engine import TradingEngine

    bars = []
    for i in range(200):
        price = 68000 + np.sin(i * 0.1) * 1000 + i * 5
        bars.append(Bar(
            "BACKTEST", "BTCUSDT", "1m",
            price - 20, price + 40, price - 40, price,
            100, timestamp=1716600000000 + i * 60000
        ))

    engine = TradingEngine()
    config = BacktestConfig(initial_balance={"USDT": 100000.0})
    backtest = BacktestExchange(config)
    backtest.load_bars("BTCUSDT", bars)
    engine.add_exchange(backtest)

    strategy = SimpleBreakoutStrategy(["BTCUSDT"], period=20, qty=0.001)
    engine.add_strategy(strategy)

    async def run():
        await engine.start()
        while backtest._running:
            await asyncio.sleep(0.1)
        await engine.stop()

    asyncio.run(run())

    report = backtest.get_performance_report()
    print(f"PASS: Strategy backtest complete")
    print(f"  Trades: {report.get('total_trades', 0)}")
    print(f"  Final equity: {report.get('final_equity', 'N/A')}")
    print(f"  Total return: {report.get('total_return', 'N/A')}")


if __name__ == "__main__":
    test_backtest_basic()
    test_backtest_strategy()
    print("\nAll backtest tests passed.")
