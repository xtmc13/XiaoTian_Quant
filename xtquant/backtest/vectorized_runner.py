"""
向量化策略回测引擎 — 对 DataFrame 逐行迭代执行信号，输出完整绩效报告
"""

from __future__ import annotations
import time
import numpy as np
import pandas as pd
from typing import List, Dict, Optional
from dataclasses import dataclass, field

from .stats import (
    sharpe_ratio, sortino_ratio, max_drawdown, calmar_ratio,
    win_rate, profit_factor, compute_performance_report, compute_returns,
)


@dataclass
class VectorizedBacktestConfig:
    initial_capital: float = 100000.0
    fee_rate: float = 0.001
    slippage: float = 0.0005
    position_size: float = 1.0  # fraction of capital per trade (1.0 = all-in)
    allow_short: bool = False


def run_vectorized_backtest(
    df: pd.DataFrame,
    strategy,
    config: VectorizedBacktestConfig = None,
) -> dict:
    """对向量化策略运行逐行回测。

    Args:
        df: OHLCV DataFrame, 必须包含 open/high/low/close/volume 列
        strategy: VectorizedStrategy 实例
        config: 回测配置

    Returns:
        {"trades": [...], "equity_curve": [(ts, equity), ...], "report": {...}}
    """
    if config is None:
        config = VectorizedBacktestConfig()

    metadata = {"pair": strategy.symbols[0], "timeframe": strategy.timeframe}
    df = df.copy()
    df = strategy.generate_signals(df, metadata)

    capital = config.initial_capital
    position = 0.0        # 持仓数量
    entry_price = 0.0     # 入场均价
    entry_time = None     # 入场时间戳
    position_type = None  # "long" | "short"
    peak_price = 0.0      # 用于 trailing stop

    trades: List[dict] = []
    equity_curve: List[tuple] = []

    n = len(df)
    startup = max(strategy.startup_candle_count, 1)

    for i in range(n):
        row = df.iloc[i]
        ts = row.get("timestamp", i)
        price = float(row["close"])

        # —— 持仓状态更新 ——
        if position > 0:
            # Trailing stop 峰值更新
            if position_type == "long":
                peak_price = max(peak_price, price)
            else:
                peak_price = min(peak_price, price) if peak_price else price

            exit_signal = 0.0
            exit_reason = ""

            # 止损检查
            minutes_held = ((ts - entry_time) / 60000
                            if entry_time and isinstance(ts, (int, float))
                            else 0)
            sl_exit = strategy.should_exit_for_stoploss(
                price, entry_price, position_type or "long"
            )
            if sl_exit > 0:
                exit_signal = 1.0
                exit_reason = "stoploss"

            # 止盈检查 (minimal_roi)
            if not exit_signal:
                roi_exit = strategy.should_exit_for_roi(
                    price, entry_price, minutes_held, position_type or "long"
                )
                if roi_exit > 0:
                    exit_signal = 1.0
                    exit_reason = f"roi_{roi_exit:.4f}"

            # 信号出场
            if not exit_signal:
                if position_type == "long" and row.get("exit_long", 0):
                    exit_signal = 1.0
                    exit_reason = "exit_signal"
                elif position_type == "short" and row.get("exit_short", 0):
                    exit_signal = 1.0
                    exit_reason = "exit_signal"

            # Trailing stop
            if (not exit_signal and strategy.trailing_stop
                    and position_type == "long" and peak_price > entry_price):
                trail_level = peak_price * (1 + strategy.stoploss)
                if price <= trail_level:
                    exit_signal = 1.0
                    exit_reason = "trailing_stop"

            if exit_signal:
                exec_price = price * (1 - config.slippage) if position_type == "long" else price * (1 + config.slippage)
                fee = exec_price * position * config.fee_rate

                if position_type == "long":
                    pnl = (exec_price - entry_price) * position - fee
                else:
                    pnl = (entry_price - exec_price) * position - fee

                capital += position * entry_price + pnl  # 回收本金+盈亏

                trades.append({
                    "entry_time": entry_time,
                    "exit_time": ts,
                    "entry_price": round(entry_price, 2),
                    "exit_price": round(exec_price, 2),
                    "quantity": round(position, 6),
                    "pnl": round(pnl, 2),
                    "pnl_pct": round(pnl / (position * entry_price) * 100, 2),
                    "side": position_type,
                    "exit_reason": exit_reason,
                })

                position = 0.0
                entry_price = 0.0
                position_type = None
                peak_price = 0.0

        # —— 入场检查 ——
        if position == 0 and i >= startup:
            enter_long = row.get("enter_long", 0)
            enter_short = row.get("enter_short", 0) if config.allow_short else 0

            if enter_long or enter_short:
                side = "long" if enter_long else "short"
                exec_price = (price * (1 + config.slippage)
                              if side == "long"
                              else price * (1 - config.slippage))

                trade_capital = capital * config.position_size
                position = trade_capital / exec_price
                fee = exec_price * position * config.fee_rate
                capital -= fee

                entry_price = exec_price
                entry_time = ts
                position_type = side
                peak_price = exec_price

        # —— 权益记录 ——
        if position > 0:
            if position_type == "long":
                unrealized = (price - entry_price) * position
            else:
                unrealized = (entry_price - price) * position
            equity = capital + unrealized
        else:
            equity = capital

        equity_curve.append((ts, equity))

    # —— 收盘强制平仓 ——
    if position > 0:
        last_price = float(df.iloc[-1]["close"])
        if position_type == "long":
            pnl = (last_price - entry_price) * position
        else:
            pnl = (entry_price - last_price) * position
        capital += position * entry_price + pnl
        trades.append({
            "entry_time": entry_time,
            "exit_time": equity_curve[-1][0] if equity_curve else 0,
            "entry_price": round(entry_price, 2),
            "exit_price": round(last_price, 2),
            "quantity": round(position, 6),
            "pnl": round(pnl, 2),
            "pnl_pct": round(pnl / (position * entry_price) * 100, 2),
            "side": position_type,
            "exit_reason": "force_close",
        })

    equity_values = [e[1] for e in equity_curve]
    if not equity_values:
        equity_values = [config.initial_capital]

    report = compute_performance_report(equity_values, trades)

    return {
        "trades": trades,
        "equity_curve": equity_curve,
        "report": report,
        "final_capital": round(equity_values[-1], 2),
        "total_return_pct": report.get("total_return_pct", 0),
        "num_trades": len(trades),
    }
