"""
绩效统计 — Sharpe, Sortino, Calmar, WinRate等
"""

import numpy as np
from typing import List, Tuple


def compute_returns(equity_curve: List[float]) -> np.ndarray:
    """从权益曲线计算收益率序列"""
    if len(equity_curve) < 2:
        return np.array([])
    arr = np.array(equity_curve)
    return np.diff(arr) / arr[:-1]


def sharpe_ratio(returns: np.ndarray, risk_free: float = 0.0, periods_per_year: int = 365) -> float:
    """年化Sharpe比率"""
    if len(returns) < 2:
        return 0.0
    excess = returns - risk_free / periods_per_year
    std = np.std(returns)
    if std == 0:
        return 0.0
    return np.mean(excess) / std * np.sqrt(periods_per_year)


def sortino_ratio(returns: np.ndarray, risk_free: float = 0.0, periods_per_year: int = 365) -> float:
    """年化Sortino比率（只考虑下行波动）"""
    if len(returns) < 2:
        return 0.0
    excess = returns - risk_free / periods_per_year
    downside = returns[returns < 0]
    if len(downside) == 0:
        return 0.0
    downside_std = np.std(downside)
    if downside_std == 0:
        return 0.0
    return np.mean(excess) / downside_std * np.sqrt(periods_per_year)


def max_drawdown(equity_curve: List[float]) -> Tuple[float, int, int]:
    """最大回撤 (value, start_idx, end_idx)"""
    if len(equity_curve) < 2:
        return 0.0, 0, 0
    arr = np.array(equity_curve)
    peak = np.maximum.accumulate(arr)
    dd = (peak - arr) / peak
    max_dd_idx = np.argmax(dd)
    if max_dd_idx == 0 or dd[max_dd_idx] == 0:
        return 0.0, 0, 0
    peak_idx = np.argmax(arr[:max_dd_idx])
    return float(dd[max_dd_idx]), int(peak_idx), int(max_dd_idx)


def calmar_ratio(returns: np.ndarray, equity_curve: List[float], periods_per_year: int = 365) -> float:
    """Calmar比率 = 年化收益率 / 最大回撤"""
    dd, _, _ = max_drawdown(equity_curve)
    if dd == 0:
        return 0.0
    annual_return = np.mean(returns) * periods_per_year
    return annual_return / dd


def win_rate(trades: List[dict]) -> float:
    """胜率"""
    if not trades:
        return 0.0
    wins = sum(1 for t in trades if t.get("pnl", 0) > 0)
    return wins / len(trades)


def profit_factor(trades: List[dict]) -> float:
    """盈亏比"""
    gross_profit = sum(t["pnl"] for t in trades if t.get("pnl", 0) > 0)
    gross_loss = abs(sum(t["pnl"] for t in trades if t.get("pnl", 0) < 0))
    return gross_profit / gross_loss if gross_loss > 0 else float("inf")


def compute_performance_report(
    equity_curve: List[float],
    trades: List[dict] = None,
    periods_per_year: int = 365,
) -> dict:
    """生成完整绩效报告"""
    trades = trades or []

    if len(equity_curve) < 2:
        return {"error": "数据不足"}

    returns = compute_returns(equity_curve)
    initial = equity_curve[0]
    final = equity_curve[-1]
    total_return = (final - initial) / initial if initial > 0 else 0

    dd, dd_start, dd_end = max_drawdown(equity_curve)

    # 年化收益率 (粗略)
    n_periods = len(returns)
    annual_return = np.mean(returns) * periods_per_year if n_periods > 0 else 0

    return {
        "initial_equity": round(initial, 2),
        "final_equity": round(final, 2),
        "total_return_pct": round(total_return * 100, 2),
        "annual_return_pct": round(annual_return * 100, 2),
        "sharpe_ratio": round(sharpe_ratio(returns, periods_per_year=periods_per_year), 3),
        "sortino_ratio": round(sortino_ratio(returns, periods_per_year=periods_per_year), 3),
        "calmar_ratio": round(calmar_ratio(returns, equity_curve, periods_per_year), 3),
        "max_drawdown_pct": round(dd * 100, 2),
        "win_rate_pct": round(win_rate(trades) * 100, 2),
        "profit_factor": round(profit_factor(trades), 3),
        "total_trades": len(trades),
        "n_periods": n_periods,
    }
