"""
风控管理器 — 多级检查链 + 熔断器
"""

from __future__ import annotations
import time
import logging
from typing import Dict

from .checks import RiskContext, CHECK_CHAIN
from .circuit import CircuitBreaker
from ..core.component import Component

logger = logging.getLogger("xtquant.risk")


class RiskManager(Component):
    """
    风控管理器

    特性:
      - 14项风控检查链
      - 熔断器自动触发/恢复
      - 风控事件持久化
      - 实时上下文更新
    """

    def __init__(self, config: dict = None, db=None):
        super().__init__("RiskManager")
        self.config = config or {}
        self._db = db
        self._ctx = RiskContext()
        self._circuit_breaker = CircuitBreaker()
        self._event_bus = None

        # 当日统计
        self._daily_order_count: int = 0
        self._consecutive_losses: int = 0
        self._last_pnl: float = 0.0
        self._day_start: float = time.time()
        self._peak_equity: float = 0.0
        self._equity_history: list = []

        # 检查统计
        self._check_stats: Dict[str, int] = {}  # check_name -> reject_count

    def set_event_bus(self, bus):
        self._event_bus = bus

    async def _on_start(self):
        self._day_start = time.time()
        self._daily_order_count = 0
        logger.info("[Risk] 风控管理器已启动 | 14项检查就绪")

    async def _on_stop(self):
        logger.info(f"[Risk] 风控管理器已停止 | 今日拦截: {sum(self._check_stats.values())}")

    # ============================================================
    #  核心: 订单风控检查
    # ============================================================

    async def check(self, order) -> tuple:
        """
        执行完整检查链

        Returns:
            (passed: bool, reason: str)
        """
        # 0. 熔断器优先（可配置）
        cb_enabled = self.config.get("circuit_breaker_enabled", True)
        if cb_enabled and self._circuit_breaker.is_open:
            return False, f"熔断器已触发: {self._circuit_breaker.reason}"

        # 0.5 传播上下文到检查链
        self._ctx.daily_orders = self._daily_order_count
        self._ctx.consecutive_losses = self._consecutive_losses

        # 1. 顺序执行检查链
        for check_name, check_fn in CHECK_CHAIN:
            try:
                passed, msg = check_fn(order, self._ctx, self.config)
                if not passed:
                    self._record_reject(check_name, msg, order)
                    return False, msg
            except Exception as e:
                logger.error(f"[Risk] 检查异常 {check_name}: {e}")
                self._record_reject(check_name, f"检查异常: {e}", order)
                return False, f"{check_name} 异常: {e}"

        self._daily_order_count += 1
        return True, ""

    def _record_reject(self, check_name: str, msg: str, order):
        """记录拦截事件"""
        self._check_stats[check_name] = self._check_stats.get(check_name, 0) + 1
        logger.warning(f"[Risk] [{check_name}] 拦截: {msg} | {order}")

        # 持久化
        if self._db:
            import asyncio
            asyncio.create_task(self._persist_risk_event(check_name, "WARNING", msg, order))

        # 发布事件
        if self._event_bus:
            from ..core.event import MarketEvent, EventType
            self._event_bus.emit_nowait(MarketEvent(
                EventType.RISK_ALERT,
                order.exchange,
                order.symbol,
                int(time.time() * 1000),
                {"check": check_name, "level": "WARNING", "detail": msg}
            ))

    async def _persist_risk_event(self, check_name: str, level: str, detail: str, order):
        try:
            from ..db.repository import RiskEventRepository
            repo = RiskEventRepository(self._db)
            await repo.save(
                check_name=check_name,
                level=level,
                detail=detail,
                symbol=order.symbol,
                strategy=order.strategy,
                order_id=order.id,
            )
        except Exception:
            pass

    # ============================================================
    #  上下文更新 (由引擎/策略定期调用)
    # ============================================================

    def update_price(self, symbol: str, price: float):
        """更新最新价格（支持所有交易对）"""
        old = self._ctx.prices.get(symbol, 0.0)
        self._ctx.prices[symbol] = price
        # Use the most liquid symbol as reference price for price_sanity checks
        if not self._ctx.current_price or symbol in ("BTCUSDT", "ETHUSDT"):
            self._ctx.current_price = price
        # Per-symbol price change
        if old > 0 and price > 0:
            self._ctx.price_change_1min = (price - old) / old

    def update_equity(self, total_equity: float):
        """更新权益信息"""
        self._ctx.total_equity = total_equity
        if total_equity > self._peak_equity:
            self._peak_equity = total_equity
        if self._peak_equity > 0:
            self._ctx.current_drawdown = (self._peak_equity - total_equity) / self._peak_equity

        # 检查连续亏损
        self._equity_history.append(total_equity)
        if len(self._equity_history) > 2:
            if self._equity_history[-1] < self._equity_history[-2]:
                self._consecutive_losses += 1
            else:
                self._consecutive_losses = 0
        self._ctx.consecutive_losses = self._consecutive_losses

    def update_positions(self, positions_summary: dict):
        """更新持仓汇总"""
        self._ctx.positions = positions_summary
        # 计算净敞口
        net = 0.0
        for pos in positions_summary.values():
            size = pos.get("size", 0)
            side = pos.get("side", "LONG")
            price = pos.get("entry_price", 0)
            net += size * price if side == "LONG" else -size * price
        self._ctx.net_exposure = net

    def update_margin(self, margin_used: float, margin_ratio: float):
        """更新保证金信息"""
        self._ctx.margin_used = margin_used
        self._ctx.margin_ratio = margin_ratio

    def update_funding(self, rate: float):
        """更新资金费率"""
        self._ctx.funding_rate = rate

    def update_volatility(self, vol: float):
        """更新波动率"""
        self._ctx.current_volatility = vol

    def update_balance(self, available: float):
        """更新可用余额"""
        self._ctx.available_balance = available

    # ============================================================
    #  熔断器
    # ============================================================

    @property
    def is_circuit_open(self) -> bool:
        return self._circuit_breaker.is_open

    def trip_circuit(self, reason: str):
        """触发熔断"""
        self._circuit_breaker.trip(reason)
        logger.critical(f"[Risk] 熔断器触发! {reason}")

    def reset_circuit(self):
        """手动重置熔断"""
        self._circuit_breaker.reset()

    # ============================================================
    #  查询
    # ============================================================

    def get_stats(self) -> dict:
        return {
            "name": self.name,
            "circuit_open": self._circuit_breaker.is_open,
            "circuit_reason": self._circuit_breaker.reason,
            "daily_orders": self._daily_order_count,
            "consecutive_losses": self._ctx.consecutive_losses,
            "current_drawdown_pct": f"{self._ctx.current_drawdown*100:.2f}%",
            "price_change_1min_pct": f"{self._ctx.price_change_1min*100:.2f}%",
            "margin_ratio": f"{self._ctx.margin_ratio*100:.1f}%" if self._ctx.margin_used > 0 else "N/A",
            "check_stats": self._check_stats,
        }
