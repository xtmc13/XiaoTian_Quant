"""
仓位管理器 — 多账户聚合、仓位计算、对账
"""

from __future__ import annotations
import asyncio
import logging
import time
from typing import Dict, List, Optional, Any
from dataclasses import dataclass

from ..core.component import Component

logger = logging.getLogger("xtquant.portfolio")


@dataclass
class PositionInfo:
    """持仓信息"""
    exchange: str
    symbol: str
    side: str = "LONG"
    size: float = 0.0
    entry_price: float = 0.0
    mark_price: float = 0.0
    unrealized_pnl: float = 0.0
    realized_pnl: float = 0.0
    leverage: float = 1.0

    @property
    def notional(self) -> float:
        return self.size * self.mark_price if self.mark_price > 0 else self.size * self.entry_price

    @property
    def pnl_pct(self) -> float:
        if self.size <= 0 or self.entry_price <= 0:
            return 0.0
        if self.side == "LONG":
            return (self.mark_price - self.entry_price) / self.entry_price
        return (self.entry_price - self.mark_price) / self.entry_price


@dataclass
class AccountInfo:
    """账户信息"""
    exchange: str
    total_equity: float = 0.0
    available_balance: float = 0.0
    margin_used: float = 0.0
    margin_ratio: float = 999.0  # >1 安全


class PortfolioManager(Component):
    """
    组合管理器

    职责:
      - 聚合所有交易所的账户和持仓
      - 实时计算总权益、可用保证金、净敞口
      - 仓位计算（Kelly/固定比例/风险预算）
      - 定期对账
      - 权益快照
    """

    def __init__(self, db=None, exchanges: Dict[str, Any] = None):
        super().__init__("Portfolio")
        self._db = db
        self._exchanges = exchanges or {}

        # 账户聚合
        self._accounts: Dict[str, AccountInfo] = {}

        # 持仓聚合: exchange:symbol:side -> PositionInfo
        self._positions: Dict[str, Dict[str, Dict[str, PositionInfo]]] = {}

        # 历史快照
        self._equity_snapshots: List[tuple] = []
        self._peak_equity: float = 0.0

        # 对账任务
        self._reconcile_task: Optional[asyncio.Task] = None

        # 余额锁定: order_id -> {"token": str, "amount": float}
        self._balance_locks: Dict[str, Dict[str, float]] = {}

    async def _on_start(self):
        """启动"""
        self._reconcile_task = asyncio.create_task(self._reconcile_loop())
        logger.info("[Portfolio] 仓位管理器已启动")

    async def _on_stop(self):
        """停止"""
        if self._reconcile_task:
            self._reconcile_task.cancel()
        logger.info("[Portfolio] 仓位管理器已停止")

    async def _reconcile_loop(self):
        """定期对账循环 (每30s)"""
        while self.is_running:
            try:
                await self._reconcile()
                await self._snapshot()
                await asyncio.sleep(30)
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"[Portfolio] 对账异常: {e}")
                await asyncio.sleep(30)

    async def _reconcile(self):
        """与交易所对账"""
        for exch_name, exchange in self._exchanges.items():
            if not exchange.is_connected:
                continue
            try:
                balances = await exchange.get_balance()
                if balances:
                    total = sum(balances.values())
                    available = balances.get("USDT", 0)
                    self._accounts[exch_name] = AccountInfo(
                        exchange=exch_name,
                        total_equity=total,
                        available_balance=available,
                    )
            except Exception as e:
                logger.warning(f"[Portfolio] {exch_name} 余额获取失败: {e}")

    async def _snapshot(self):
        """保存权益快照"""
        total = self.total_equity
        if total > self._peak_equity:
            self._peak_equity = total
        self._equity_snapshots.append((int(time.time() * 1000), total))
        if len(self._equity_snapshots) > 10000:
            self._equity_snapshots = self._equity_snapshots[-5000:]

        if self._db:
            try:
                from ..db.repository import PositionRepository, AccountRepository
                pos_repo = PositionRepository(self._db)
                acct_repo = AccountRepository(self._db)

                for exch_name, account in self._accounts.items():
                    await acct_repo.save_snapshot(
                        exch_name, account.total_equity,
                        account.available_balance,
                        account.margin_used, account.margin_ratio,
                    )
                    for symbol_positions in self._positions.get(exch_name, {}).values():
                        for side, pos in symbol_positions.items():
                            await pos_repo.save_snapshot(
                                exch_name, pos.symbol, pos.size,
                                pos.entry_price, pos.mark_price,
                                pos.unrealized_pnl, account.total_equity,
                            )
            except Exception:
                pass

    # ============================================================
    #  余额锁定 (防止重复下单透支余额)
    # ============================================================

    def lock_balance(self, token: str, amount: float, order_id: str) -> bool:
        """
        锁定余额用于订单。

        返回 True 表示锁定成功，False 表示余额不足。
        """
        available = self.get_available_balance(token)
        if available < amount:
            logger.warning(f"[Portfolio] 余额不足，锁定失败: {token} 可用={available:.2f} 需要={amount:.2f}")
            return False
        self._balance_locks[order_id] = {"token": token, "amount": amount}
        logger.debug(f"[Portfolio] 余额锁定: {token} {amount:.2f} (order={order_id})")
        return True

    def unlock_balance(self, order_id: str) -> float:
        """
        解锁订单的余额锁定。

        返回解锁的金额，如果订单没有锁定则返回 0。
        """
        lock = self._balance_locks.pop(order_id, None)
        if lock:
            logger.debug(f"[Portfolio] 余额解锁: {lock['token']} {lock['amount']:.2f} (order={order_id})")
            return lock["amount"]
        return 0.0

    def get_locked_total(self, token: str) -> float:
        """获取指定代币的总锁定金额"""
        total = 0.0
        for lock in self._balance_locks.values():
            if lock["token"] == token:
                total += lock["amount"]
        return total

    def get_available_balance(self, token: str) -> float:
        """获取可用余额（总余额 - 锁定）"""
        total = 0.0
        for account in self._accounts.values():
            if token == "USDT":
                total += account.available_balance
            elif token == "USDC":
                total += account.available_balance  # simplified
        locked = self.get_locked_total(token)
        return total - locked

    # ============================================================
    #  成交处理
    # ============================================================

    async def on_fill(self, order, fill_price: float, fill_qty: float):
        """处理成交，更新持仓"""
        exch = order.exchange
        symbol = order.symbol
        side = "LONG" if order.side.value == "BUY" else "SHORT"

        if exch not in self._positions:
            self._positions[exch] = {}
        if symbol not in self._positions[exch]:
            self._positions[exch][symbol] = {}

        pos = self._positions[exch][symbol].get(side)

        if order.side.value == "BUY":
            if pos is None:
                pos = PositionInfo(exch, symbol, side)
                self._positions[exch][symbol][side] = pos
            old_notional = pos.size * pos.entry_price
            pos.size += fill_qty
            pos.entry_price = (old_notional + fill_price * fill_qty) / pos.size if pos.size > 0 else fill_price
        else:
            if pos is None:
                pos = PositionInfo(exch, symbol, side)
                self._positions[exch][symbol][side] = pos
            pos.size -= fill_qty
            if pos.size <= 0:
                self._positions[exch][symbol].pop(side, None)

        pos.mark_price = fill_price

        # 持久化
        if self._db:
            try:
                from ..db.repository import PositionRepository
                repo = PositionRepository(self._db)
                await repo.upsert(
                    exch, symbol, side, pos.size,
                    pos.entry_price, pos.mark_price,
                    pos.unrealized_pnl, 0
                )
            except Exception:
                pass

    # ============================================================
    #  查询
    # ============================================================

    @property
    def total_equity(self) -> float:
        """总权益"""
        total = sum(a.total_equity for a in self._accounts.values())
        for exch_positions in self._positions.values():
            for sym_positions in exch_positions.values():
                for pos in sym_positions.values():
                    total += pos.unrealized_pnl
        return total

    @property
    def available_balance(self) -> float:
        """总可用余额"""
        return sum(a.available_balance for a in self._accounts.values())

    @property
    def margin_used(self) -> float:
        """总已用保证金"""
        return sum(a.margin_used for a in self._accounts.values())

    @property
    def margin_ratio(self) -> float:
        """整体保证金率"""
        m = self.margin_used
        return self.total_equity / m if m > 0 else 999.0

    @property
    def current_drawdown(self) -> float:
        """当前回撤"""
        if self._peak_equity <= 0:
            return 0.0
        return (self._peak_equity - self.total_equity) / self._peak_equity

    @property
    def net_exposure(self) -> float:
        """净敞口"""
        net = 0.0
        for exch_positions in self._positions.values():
            for sym_positions in exch_positions.values():
                for pos in sym_positions.values():
                    net += pos.notional if pos.side == "LONG" else -pos.notional
        return net

    def get_position(self, exchange: str, symbol: str) -> Optional[PositionInfo]:
        """获取指定持仓"""
        sym_positions = self._positions.get(exchange, {}).get(symbol, {})
        return sym_positions.get("LONG") or sym_positions.get("SHORT")

    def get_all_positions(self) -> List[PositionInfo]:
        """获取所有持仓"""
        result = []
        for exch_positions in self._positions.values():
            for sym_positions in exch_positions.values():
                result.extend(sym_positions.values())
        return result

    def get_positions_summary(self) -> dict:
        """获取持仓汇总（给风控用）"""
        summary = {}
        for exch_positions in self._positions.values():
            for symbol, sym_positions in exch_positions.items():
                for side, pos in sym_positions.items():
                    key = f"{symbol}_{side}"
                    summary[key] = {
                        "side": side,
                        "size": pos.size,
                        "entry_price": pos.entry_price,
                    }
        return summary

    def get_stats(self) -> dict:
        return {
            "name": self.name,
            "total_equity": f"{self.total_equity:.2f}",
            "available_balance": f"{self.available_balance:.2f}",
            "margin_used": f"{self.margin_used:.2f}",
            "margin_ratio": f"{self.margin_ratio:.2f}",
            "drawdown_pct": f"{self.current_drawdown*100:.2f}%",
            "net_exposure": f"{self.net_exposure:.2f}",
            "positions": len(self.get_all_positions()),
            "accounts": list(self._accounts.keys()),
        }
