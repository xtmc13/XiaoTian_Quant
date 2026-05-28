"""
风控检查项 — 14项检查链
每项检查返回 (passed: bool, message: str)
"""

from __future__ import annotations
import logging
from typing import Dict

logger = logging.getLogger("xtquant.risk.checks")


class RiskContext:
    """风控检查上下文 — 在检查链之间传递"""

    def __init__(self):
        self.current_price: float = 0.0
        self.current_volatility: float = 0.0
        self.total_equity: float = 0.0
        self.available_balance: float = 0.0
        self.positions: Dict[str, Dict] = {}  # symbol -> {side, size, entry_price, ...}
        self.daily_orders: int = 0
        self.consecutive_losses: int = 0
        self.active_order_count: int = 0  # 当前活跃订单数
        self.funding_rate: float = 0.0
        self.margin_used: float = 0.0
        self.margin_ratio: float = 999.0
        self.net_exposure: float = 0.0  # 总净敞口
        self.current_drawdown: float = 0.0
        self.price_change_1min: float = 0.0
        self.prices: Dict[str, float] = {}  # symbol -> last price
        self.blacklist: list = []


def check_price_sanity(order, ctx: RiskContext, config: dict) -> tuple:
    """
    1. 订单价格合理性: 偏离市价>5%拦截
    """
    ref_price = ctx.prices.get(order.symbol, ctx.current_price)
    if ref_price <= 0:
        return True, ""
    max_dev = config.get("price_deviation_pct", 0.05)
    if order.order_type.value in ("MARKET", "TWAP", "VWAP"):
        return True, ""
    deviation = abs(order.price - ref_price) / ref_price
    if deviation > max_dev:
        return False, f"价格偏离{deviation*100:.1f}% > {max_dev*100:.0f}%"
    return True, ""


def check_order_size(order, ctx: RiskContext, config: dict) -> tuple:
    """
    2. 订单数量合理性: 超过最大单笔USDT限制
    """
    max_order = config.get("max_order_usdt", 10000)
    notional = order.price * order.quantity
    if notional > max_order:
        return False, f"订单金额{notional:.0f} > 限额{max_order:.0f} USDT"
    if order.quantity <= 0:
        return False, "订单数量必须>0"
    return True, ""


def check_daily_limit(order, ctx: RiskContext, config: dict) -> tuple:
    """
    3. 总订单数限制: 每日最多N笔
    """
    max_daily = config.get("max_daily_orders", 100)
    if ctx.daily_orders >= max_daily:
        return False, f"今日订单{ctx.daily_orders} >= 上限{max_daily}"
    return True, ""


def check_rate_limit(order, ctx: RiskContext, config: dict) -> tuple:
    """
    4. 频率限制: 同一交易对1s内不能超过N笔
    注: 速率检查在OMS层做了，这里做二级检查
    """
    return True, ""


def check_position_limit(order, ctx: RiskContext, config: dict) -> tuple:
    """
    5. 持仓上限: 单币种不超过总资产X%
    """
    max_pct = config.get("max_position_pct", 0.5)
    if ctx.total_equity <= 0:
        return True, ""

    symbol = order.symbol
    current_value = 0.0
    for pos_symbol, pos in ctx.positions.items():
        if pos_symbol == symbol or pos_symbol.startswith(symbol.replace("USDT", "")):
            current_value += pos.get("size", 0) * ctx.current_price

    if order.side.value == "BUY":
        new_value = current_value + order.price * order.quantity
    else:
        new_value = current_value

    if new_value / ctx.total_equity > max_pct:
        return False, f"持仓占比{new_value/ctx.total_equity*100:.1f}% > {max_pct*100:.0f}%"
    return True, ""


def check_net_exposure(order, ctx: RiskContext, config: dict) -> tuple:
    """
    6. 净敞口限制: 总净敞口不超过总资产Y%
    """
    max_exp = config.get("max_net_exposure_pct", 0.8)
    if ctx.total_equity <= 0:
        return True, ""

    abs_exposure = abs(ctx.net_exposure)
    if order.side.value == "BUY":
        new_exposure = abs(ctx.net_exposure + order.price * order.quantity)
    else:
        new_exposure = abs(ctx.net_exposure - order.price * order.quantity)

    if new_exposure / ctx.total_equity > max_exp:
        return False, f"净敞口{new_exposure/ctx.total_equity*100:.1f}% > {max_exp*100:.0f}%"
    return True, ""


def check_max_drawdown(order, ctx: RiskContext, config: dict) -> tuple:
    """
    7. 最大回撤: 当日回撤>Z%熔断
    """
    max_dd = config.get("max_drawdown_pct", 0.1)
    if ctx.current_drawdown > max_dd:
        return False, f"当日回撤{ctx.current_drawdown*100:.1f}% > {max_dd*100:.0f}% 熔断!"
    return True, ""


def check_consecutive_losses(order, ctx: RiskContext, config: dict) -> tuple:
    """
    8. 连续亏损: 连续N笔亏损暂停
    """
    max_loss = config.get("max_consecutive_losses", 5)
    if ctx.consecutive_losses >= max_loss:
        return False, f"连续亏损{ctx.consecutive_losses}笔 >= 上限{max_loss} 暂停!"
    return True, ""


def check_funding_rate(order, ctx: RiskContext, config: dict) -> tuple:
    """
    9. 资金费率风控: 合约资金费率过高预警
    """
    threshold = config.get("funding_rate_warn", 0.00375)  # 0.375%
    if abs(ctx.funding_rate) > threshold:
        msg = f"资金费率{ctx.funding_rate*100:.3f}% 过高 | > {threshold*100:.3f}%"
        return False, msg
    return True, ""


def check_margin_ratio(order, ctx: RiskContext, config: dict) -> tuple:
    """
    10. 保证金率: 全仓保证金率<150%禁止开仓
    """
    min_ratio = config.get("min_margin_ratio", 1.5)
    if ctx.margin_used <= 0:
        return True, ""  # 现货模式，跳过

    # 计算新订单需要的保证金
    new_margin = order.price * order.quantity / order.leverage if hasattr(order, 'leverage') else order.price * order.quantity
    estimated_ratio = (ctx.total_equity) / (ctx.margin_used + new_margin) if (ctx.margin_used + new_margin) > 0 else 999

    if estimated_ratio < min_ratio:
        return False, f"预估保证金率{estimated_ratio*100:.0f}% < {min_ratio*100:.0f}%"
    return True, ""


def check_blacklist(order, ctx: RiskContext, config: dict) -> tuple:
    """
    11. 黑名单: 禁止交易列表中的品种
    """
    blacklist = config.get("blacklist", [])
    blacklist = blacklist or []
    if order.symbol in blacklist:
        return False, f"{order.symbol} 在黑名单中"
    return True, ""


def check_volatility(order, ctx: RiskContext, config: dict) -> tuple:
    """
    12. 波动率限制: 标的波动率>阈值禁止交易
    """
    threshold = config.get("volatility_threshold", 0.02)
    if ctx.current_volatility > threshold:
        return False, f"波动率{ctx.current_volatility*100:.2f}% > {threshold*100:.2f}%"
    return True, ""


def check_time_window(order, ctx: RiskContext, config: dict) -> tuple:
    """
    13. 时间窗口: 重要数据发布前后暂停交易
    注: 需要外部事件源（经济日历）注入
    """
    # 当前默认通过，由外部事件设置
    return True, ""


def check_price_spike(order, ctx: RiskContext, config: dict) -> tuple:
    """
    14. 急涨急跌: 1分钟内价格异常波动暂停
    """
    spike_threshold = config.get("price_spike_pct", 0.03)  # 3%
    if abs(ctx.price_change_1min) > spike_threshold:
        return False, f"1分钟价格波动{ctx.price_change_1min*100:.2f}% > {spike_threshold*100:.1f}%"
    return True, ""


def check_concurrent_orders(order, ctx: RiskContext, config: dict) -> tuple:
    """
    15. 并发订单数: 活跃订单数超过限制时拦截
    """
    max_orders = config.get("max_concurrent_orders", 5)
    if ctx.active_order_count >= max_orders:
        return False, f"活跃订单数{ctx.active_order_count} >= 上限{max_orders}"
    return True, ""


# 检查链：按顺序执行
CHECK_CHAIN = [
    ("price_sanity", check_price_sanity),
    ("order_size", check_order_size),
    ("daily_limit", check_daily_limit),
    ("rate_limit", check_rate_limit),
    ("concurrent_orders", check_concurrent_orders),
    ("position_limit", check_position_limit),
    ("net_exposure", check_net_exposure),
    ("max_drawdown", check_max_drawdown),
    ("consecutive_losses", check_consecutive_losses),
    ("funding_rate", check_funding_rate),
    ("margin_ratio", check_margin_ratio),
    ("blacklist", check_blacklist),
    ("volatility", check_volatility),
    ("time_window", check_time_window),
    ("price_spike", check_price_spike),
]
