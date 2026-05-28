"""
仓位计算 — 确定每次交易的头寸大小
"""

import logging

logger = logging.getLogger("xtquant.portfolio.sizing")


def fixed_fraction(
    equity: float,
    fraction: float = 0.01,
    min_qty: float = 0.0,
    max_qty: float = float("inf"),
) -> float:
    """
    固定比例仓位

    Args:
        equity: 总权益
        fraction: 每次交易占总权益的比例 (默认1%)
    """
    qty = equity * fraction
    return max(min_qty, min(qty, max_qty))


def kelly_criterion(
    win_rate: float,
    avg_win: float,
    avg_loss: float,
    equity: float,
    fraction: float = 0.5,   # 半凯利
    max_pct: float = 0.25,    # 最大仓位比例
) -> float:
    """
    凯利公式仓位

    f* = (p * b - q) / b
    where:
      p = 胜率
      b = 平均盈利 / 平均亏损
      q = 1 - p

    Args:
        fraction: 凯利分数 (0.5 = 半凯利, 更保守)
        max_pct: 最大仓位占总权益比例
    """
    if avg_loss <= 0:
        return equity * 0.01  # 无亏损数据，保守1%

    b = avg_win / avg_loss
    q = 1.0 - win_rate

    # 凯利公式
    f_star = (win_rate * b - q) / b if b > 0 else 0
    f_star = max(0, min(f_star, max_pct))

    # 应用凯利分数
    f = f_star * fraction

    return equity * f


def risk_budget(
    equity: float,
    risk_per_trade_pct: float = 0.01,
    stop_loss_pct: float = 0.02,
) -> float:
    """
    风险预算仓位

    Args:
        equity: 总权益
        risk_per_trade_pct: 每笔交易愿承担的最大亏损比例
        stop_loss_pct: 止损距离
    """
    if stop_loss_pct <= 0:
        return 0.0
    max_loss_amount = equity * risk_per_trade_pct
    return max_loss_amount / stop_loss_pct


def equal_weight(equity: float, num_positions: int, max_pct: float = 0.5) -> float:
    """
    等权重仓位

    Args:
        num_positions: 同时持有的持仓数
    """
    if num_positions <= 0:
        return 0.0
    return equity * min(1.0 / num_positions, max_pct)


def volatility_adjusted(
    equity: float,
    volatility: float,
    target_vol_pct: float = 0.01,
    base_qty: float = 0.0,
) -> float:
    """
    波动率调整仓位

    波动率越高，仓位越小

    Args:
        volatility: 当前波动率 (如 0.02 = 2%)
        target_vol_pct: 目标波动率
    """
    if base_qty <= 0:
        base_qty = equity * 0.01
    if volatility <= 0:
        return base_qty
    scale = target_vol_pct / volatility
    return base_qty * max(0.1, min(scale, 2.0))  # 限制在 [0.1x, 2x]
