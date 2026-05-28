"""
Portfolio Monitor — Periodic PnL tracking, drawdown alerts, position alerts, and snapshot recording.

Patterns adapted from QuantDinger's portfolio_monitor.py.
"""

import time
import asyncio
import logging
from typing import Dict, Any, List, Optional
from dataclasses import dataclass, field

logger = logging.getLogger("xtquant.portfolio.monitor")


@dataclass
class PortfolioSnapshot:
    timestamp: float
    total_equity: float
    available_balance: float
    margin_used: float
    unrealized_pnl: float
    realized_pnl: float
    open_positions: int


@dataclass
class DrawdownState:
    peak_equity: float = 0.0
    current_drawdown_pct: float = 0.0
    max_drawdown_pct: float = 0.0
    drawdown_start: Optional[float] = None


@dataclass
class PositionAlert:
    """User-configurable price/PnL threshold alert for a position."""
    symbol: str
    alert_type: str  # "price_above", "price_below", "pnl_above", "pnl_below"
    threshold: float
    repeat_interval_sec: int = 300  # min interval between repeated alerts
    last_fired: float = 0.0
    enabled: bool = True


class PortfolioMonitor:
    """Tracks portfolio equity over time with alerts for drawdowns, anomalies, and position thresholds."""

    def __init__(
        self,
        max_daily_loss_pct: float = 5.0,
        max_drawdown_alert_pct: float = 10.0,
        max_consecutive_losses: int = 5,
        snapshot_interval_sec: int = 60,
    ):
        self.max_daily_loss_pct = max_daily_loss_pct
        self.max_drawdown_alert_pct = max_drawdown_alert_pct
        self.max_consecutive_losses = max_consecutive_losses
        self.snapshot_interval_sec = snapshot_interval_sec

        self.snapshots: List[PortfolioSnapshot] = []
        self.drawdown = DrawdownState()
        self.daily_pnl: float = 0.0
        self.consecutive_losses: int = 0
        self.alert_callbacks: List[callable] = []
        self.position_alerts: List[PositionAlert] = []
        self.notifier = None  # Will be connected to NotificationManager

        self._running = False
        self._task: Optional[asyncio.Task] = None
        self._alert_debounce: Dict[str, float] = {}  # alert_type -> last_fire_time

    # ── Notification Integration ──

    def set_notifier(self, notifier):
        """Connect to NotificationManager for multi-channel alert delivery."""
        self.notifier = notifier

    # ── Position Alerts ──

    def add_position_alert(self, symbol: str, alert_type: str, threshold: float,
                           repeat_interval_sec: int = 300):
        """Add a configurable price/PnL threshold alert."""
        alert = PositionAlert(
            symbol=symbol, alert_type=alert_type, threshold=threshold,
            repeat_interval_sec=repeat_interval_sec,
        )
        self.position_alerts.append(alert)
        return alert

    def remove_position_alert(self, symbol: str, alert_type: str):
        self.position_alerts = [a for a in self.position_alerts
                                if not (a.symbol == symbol and a.alert_type == alert_type)]

    async def check_position_alerts(self, positions: dict, prices: dict):
        """Check all position alerts against current prices/PnL."""
        now = time.time()
        for alert in self.position_alerts:
            if not alert.enabled:
                continue
            if now - alert.last_fired < alert.repeat_interval_sec:
                continue

            price = prices.get(alert.symbol, 0)
            pos = positions.get(alert.symbol)
            if not pos or price <= 0:
                continue

            triggered = False
            msg = ""

            if alert.alert_type == "price_above" and price >= alert.threshold:
                triggered = True
                msg = f"{alert.symbol} 价格 ${price:.2f} 超过阈值 ${alert.threshold:.2f}"
            elif alert.alert_type == "price_below" and price <= alert.threshold:
                triggered = True
                msg = f"{alert.symbol} 价格 ${price:.2f} 跌破阈值 ${alert.threshold:.2f}"
            elif alert.alert_type == "pnl_above":
                entry = pos.get("entry_price", 0)
                if entry > 0:
                    pnl_pct = (price - entry) / entry * 100
                    if pnl_pct >= alert.threshold:
                        triggered = True
                        msg = f"{alert.symbol} 浮盈 {pnl_pct:.1f}% 超过阈值 {alert.threshold:.1f}%"
            elif alert.alert_type == "pnl_below":
                entry = pos.get("entry_price", 0)
                if entry > 0:
                    pnl_pct = (price - entry) / entry * 100
                    if pnl_pct <= alert.threshold:
                        triggered = True
                        msg = f"{alert.symbol} 浮亏 {pnl_pct:.1f}% 超过阈值 {abs(alert.threshold):.1f}%"

            if triggered:
                alert.last_fired = now
                await self._send_notification("position_alert", msg, {
                    "symbol": alert.symbol, "alert_type": alert.alert_type,
                    "threshold": alert.threshold, "price": price,
                })

    # ── Alert Callbacks ──

    def on_alert(self, callback: callable):
        """Register an alert callback: func(alert_type: str, message: str, data: dict)."""
        self.alert_callbacks.append(callback)

    async def _send_notification(self, alert_type: str, message: str, data: dict = None):
        """Send alert through notifier + callbacks with dedup debounce."""
        now = time.time()
        debounce_key = f"{alert_type}:{data.get('symbol','')}" if data else alert_type
        if now - self._alert_debounce.get(debounce_key, 0) < 10:  # 10s dedup
            return
        self._alert_debounce[debounce_key] = now

        if self.notifier:
            try:
                await self.notifier.notify(f"[{alert_type}] {message}", str(data or {}), "WARNING")
            except Exception as e:
                logger.error(f"Notifier send failed: {e}")

        for cb in self.alert_callbacks:
            try:
                if asyncio.iscoroutinefunction(cb):
                    await cb(alert_type, message, data or {})
                else:
                    cb(alert_type, message, data or {})
            except Exception as e:
                logger.error(f"Alert callback failed: {e}")

    async def _fire_alert(self, alert_type: str, message: str, data: dict = None):
        logger.warning(f"[ALERT] {alert_type}: {message}")
        await self._send_notification(alert_type, message, data)

    # ── Snapshot Recording ──

    def record_snapshot(
        self,
        total_equity: float,
        available_balance: float = 0,
        margin_used: float = 0,
        unrealized_pnl: float = 0,
        realized_pnl: float = 0,
        open_positions: int = 0,
    ):
        now = time.time()
        snap = PortfolioSnapshot(
            timestamp=now,
            total_equity=total_equity,
            available_balance=available_balance,
            margin_used=margin_used,
            unrealized_pnl=unrealized_pnl,
            realized_pnl=realized_pnl,
            open_positions=open_positions,
        )
        self.snapshots.append(snap)

        # Track drawdown
        if total_equity > self.drawdown.peak_equity:
            self.drawdown.peak_equity = total_equity
            self.drawdown.current_drawdown_pct = 0.0
            self.drawdown.drawdown_start = None
        else:
            dd = (self.drawdown.peak_equity - total_equity) / self.drawdown.peak_equity * 100
            self.drawdown.current_drawdown_pct = dd
            if dd > self.drawdown.max_drawdown_pct:
                self.drawdown.max_drawdown_pct = dd
            if self.drawdown.drawdown_start is None:
                self.drawdown.drawdown_start = now

        # Track consecutive losses
        if realized_pnl < 0:
            self.consecutive_losses += 1
        else:
            self.consecutive_losses = 0

        # Prune old snapshots (keep last 24h)
        cutoff = now - 86400
        self.snapshots = [s for s in self.snapshots if s.timestamp > cutoff]

    # ── Risk Checks ──

    async def check_risk_limits(self) -> List[str]:
        """Run risk checks and return list of triggered alerts."""
        alerts = []
        dd = self.drawdown

        if dd.max_drawdown_pct >= self.max_drawdown_alert_pct:
            alerts.append(f"MAX DRAWDOWN: {dd.max_drawdown_pct:.1f}% (limit: {self.max_drawdown_alert_pct}%)")
            await self._fire_alert("max_drawdown",
                f"Portfolio drawdown {dd.max_drawdown_pct:.1f}% exceeds {self.max_drawdown_alert_pct}% limit",
                {"drawdown_pct": dd.max_drawdown_pct, "peak_equity": dd.peak_equity})

        if self.consecutive_losses >= self.max_consecutive_losses:
            alerts.append(f"CONSECUTIVE LOSSES: {self.consecutive_losses} in a row")
            await self._fire_alert("consecutive_losses",
                f"{self.consecutive_losses} consecutive losing trades — circuit breaker may trip",
                {"consecutive_losses": self.consecutive_losses})

        # Daily loss check
        if self.snapshots:
            day_start = time.time() - (time.time() % 86400)  # midnight
            today_snaps = [s for s in self.snapshots if s.timestamp > day_start]
            if today_snaps:
                first_equity = today_snaps[0].total_equity
                current_equity = today_snaps[-1].total_equity
                daily_pct = (current_equity - first_equity) / first_equity * 100
                if abs(daily_pct) >= self.max_daily_loss_pct and daily_pct < 0:
                    alerts.append(f"DAILY LOSS: {daily_pct:.1f}% (limit: {self.max_daily_loss_pct}%)")
                    await self._fire_alert("daily_loss",
                        f"Daily loss {daily_pct:.1f}% exceeds {self.max_daily_loss_pct}% limit",
                        {"daily_pnl_pct": daily_pct})

        return alerts

    # ── Metrics ──

    def get_metrics(self) -> Dict[str, Any]:
        """Calculate portfolio performance metrics."""
        if not self.snapshots:
            return {"error": "No snapshots recorded"}

        equities = [s.total_equity for s in self.snapshots]
        returns = [(equities[i] - equities[i - 1]) / equities[i - 1]
                   for i in range(1, len(equities)) if equities[i - 1] > 0]

        if not returns:
            return {"error": "Insufficient data"}

        import math
        avg_return = sum(returns) / len(returns)
        std_return = math.sqrt(sum((r - avg_return) ** 2 for r in returns) / max(len(returns) - 1, 1))

        # Sharpe ratio (assuming 0% risk-free rate)
        sharpe = (avg_return / std_return * math.sqrt(252 * 24 * 60 * 60 / self.snapshot_interval_sec)
                  if std_return > 0 else 0)

        winning = sum(1 for r in returns if r > 0)
        total = len(returns)

        return {
            "total_equity": round(equities[-1], 2),
            "peak_equity": round(self.drawdown.peak_equity, 2),
            "max_drawdown_pct": round(self.drawdown.max_drawdown_pct, 2),
            "current_drawdown_pct": round(self.drawdown.current_drawdown_pct, 2),
            "daily_pnl_pct": round((equities[-1] - equities[0]) / equities[0] * 100, 2) if equities[0] else 0,
            "sharpe_ratio": round(sharpe, 2),
            "win_rate_pct": round(winning / total * 100, 1) if total > 0 else 0,
            "total_snapshots": len(self.snapshots),
            "consecutive_losses": self.consecutive_losses,
        }

    # ── Auto-recording Task ──

    async def start(self, get_equity_func: callable):
        """Start periodic snapshot recording. `get_equity_func` should be an async function returning portfolio dict."""
        self._running = True
        self._task = asyncio.create_task(self._record_loop(get_equity_func))
        logger.info(f"Portfolio monitor started (interval: {self.snapshot_interval_sec}s)")

    async def stop(self):
        self._running = False
        if self._task:
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass
        logger.info("Portfolio monitor stopped")

    async def _record_loop(self, get_equity_func):
        while self._running:
            try:
                data = await get_equity_func()
                self.record_snapshot(
                    total_equity=data.get("total_equity", 0),
                    available_balance=data.get("available_balance", 0),
                    margin_used=data.get("margin_used", 0),
                    unrealized_pnl=data.get("unrealized_pnl", 0),
                    realized_pnl=data.get("realized_pnl", 0),
                    open_positions=len(data.get("positions", [])),
                )
                await self.check_risk_limits()
            except Exception as e:
                logger.error(f"Portfolio snapshot error: {e}")

            await asyncio.sleep(self.snapshot_interval_sec)


# Global instance
portfolio_monitor = PortfolioMonitor()
