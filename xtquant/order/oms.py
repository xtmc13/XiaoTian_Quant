"""
订单管理中心 (OMS) — 统一订单入口
所有策略信号通过OMS下单，经过风控→执行引擎→交易所
"""

from __future__ import annotations
import logging
import time
from typing import Dict, List, Optional, Callable
from collections import defaultdict

from .types import (
    Order, OrderRequest, OrderStatus, OrderSide, OrderType,
    TERMINAL_STATUSES,
)
from ..core.component import Component
from ..core.event import EventBus, EventType, MarketEvent

logger = logging.getLogger("xtquant.oms")


class OrderManager(Component):
    """
    订单管理中心

    职责:
      1. 接收策略的OrderRequest
      2. 通过风控检查链
      3. 路由到执行引擎
      4. 跟踪订单全生命周期
      5. 持久化到数据库
      6. 订单恢复（重启后重建）
    """

    def __init__(self, event_bus: EventBus, db=None, risk_manager=None,
                 portfolio=None):
        super().__init__("OMS")
        self._event_bus = event_bus
        self._db = db
        self._risk_manager = risk_manager
        self._portfolio = portfolio

        # 订单存储: order_id -> Order (内存缓存)
        self._orders: Dict[str, Order] = {}

        # 活跃订单索引
        self._active_orders: Dict[str, Order] = {}
        self._orders_by_symbol: Dict[str, List[str]] = defaultdict(list)
        self._orders_by_strategy: Dict[str, List[str]] = defaultdict(list)

        # 订单速率限制: symbol -> [timestamps]
        self._rate_tracker: Dict[str, List[float]] = defaultdict(list)
        self._rate_window_sec = 1.0
        self._max_rate = 10  # 每秒最多10单

        # 事件回调
        self._on_order_complete: Optional[Callable] = None

        # 从DB恢复
        self._recovered = False
        self._persisted_ids: set = set()  # Track which orders have been saved to DB

    async def _on_start(self):
        """启动OMS"""
        await self._recover_orders()
        self._event_bus.subscribe(EventType.ORDER_STATUS, self._on_order_event, priority=5)
        logger.info(f"[OMS] 已启动 | 恢复订单: {len(self._orders)}")

    async def _on_stop(self):
        """停止OMS"""
        active_count = len(self._active_orders)
        if active_count > 0:
            logger.warning(f"[OMS] 停止时仍有 {active_count} 个活跃订单")
            for oid, order in self._active_orders.items():
                logger.warning(f"  {order}")
        self._active_orders.clear()
        logger.info("[OMS] 已停止")

    async def _recover_orders(self):
        """从数据库恢复未完成的订单"""
        if not self._db:
            return

        try:
            from ..db.repository import OrderRepository
            repo = OrderRepository(self._db)

            open_orders = await repo.get_open()
            for row in open_orders:
                order = Order(
                    id=row["id"],
                    exchange=row["exchange"],
                    symbol=row["symbol"],
                    side=OrderSide(row["side"]),
                    order_type=OrderType(row["order_type"]),
                    price=row["price"],
                    quantity=row["quantity"],
                    status=OrderStatus(row["status"]),
                    filled_qty=row["filled_qty"],
                    remaining_qty=row["remaining_qty"],
                    avg_fill_price=row["avg_fill_price"],
                    fee=row["fee"],
                    strategy=row.get("strategy", ""),
                    client_order_id=row.get("client_order_id", ""),
                    created_at=row["created_at"],
                )
                self._orders[order.id] = order
                if order.is_active:
                    self._active_orders[order.id] = order
                self._orders_by_symbol[order.symbol].append(order.id)
                if order.strategy:
                    self._orders_by_strategy[order.strategy].append(order.id)
                self._persisted_ids.add(order.id)

            self._recovered = True
            logger.info(f"[OMS] 从DB恢复 {len(open_orders)} 个活跃订单")
        except Exception as e:
            logger.error(f"[OMS] 订单恢复失败: {e}")

    # ============================================================
    #  核心: 提交订单
    # ============================================================

    async def submit(self, request: OrderRequest) -> Optional[Order]:
        """
        提交订单

        流程:
          1. 创建Order对象 (CREATED)
          2. 余额锁定
          3. 风控检查 → PENDING 或 REJECTED
          4. 持久化到DB
          5. 发布ORDER_STATUS事件
          6. 执行（由执行引擎处理）
        """
        # 1. 创建订单
        order = Order.from_request(request)
        if order.id in self._orders:
            logger.warning(f"[OMS] 重复订单ID: {order.id}")
            return None

        self._orders[order.id] = order
        self._active_orders[order.id] = order
        self._orders_by_symbol[order.symbol].append(order.id)
        if order.strategy:
            self._orders_by_strategy[order.strategy].append(order.id)

        logger.info(f"[OMS] 收到订单: {order}")

        # 2. 速率检查
        if not self._check_rate(order.symbol):
            order.transition_to(OrderStatus.REJECTED, "订单频率超限")
            await self._persist(order)
            self._active_orders.pop(order.id, None)
            logger.warning(f"[OMS] 速率限制拦截: {order.symbol}")
            return order

        # 2.5 余额锁定
        if self._portfolio:
            amount_to_lock = 0.0
            lock_token = "USDT"
            if order.order_type == OrderType.MARKET:
                amount_to_lock = order.price * order.quantity if order.price > 0 else order.quantity
            else:
                amount_to_lock = order.price * order.quantity if order.price > 0 else order.quantity
            if order.side == OrderSide.SELL:
                lock_token = order.symbol.replace("USDT", "").replace("USDC", "")
                amount_to_lock = order.quantity
            if amount_to_lock > 0:
                if not self._portfolio.lock_balance(lock_token, amount_to_lock, order.id):
                    order.transition_to(OrderStatus.REJECTED, f"余额不足: 需要 {amount_to_lock:.2f} {lock_token}")
                    await self._persist(order)
                    self._active_orders.pop(order.id, None)
                    logger.warning(f"[OMS] 余额不足拦截: {lock_token} {amount_to_lock}")
                    return order

        # 3. 风控检查
        if self._risk_manager:
            # 传递活跃订单数给风控上下文
            self._risk_manager._ctx.active_order_count = len(self._active_orders)
            passed, msg = await self._risk_manager.check(order)
            if not passed:
                # 风控拦截 → 解锁余额
                if self._portfolio:
                    self._portfolio.unlock_balance(order.id)
                order.transition_to(OrderStatus.REJECTED, msg)
                await self._persist(order)
                self._active_orders.pop(order.id, None)
                self._emit_order_event(order)
                logger.warning(f"[OMS] 风控拦截: {msg}")
                return order

        # 4. 通过风控 → PENDING
        order.transition_to(OrderStatus.PENDING)
        await self._persist(order)
        self._emit_order_event(order)

        # 5. 提交到执行引擎 (由外部调用或内部路由)
        return order

    async def execute(self, order: Order):
        """执行订单（由执行引擎调用，将订单发送到交易所）"""
        if order.status != OrderStatus.PENDING:
            logger.warning(f"[OMS] 订单状态不是PENDING，无法执行: {order}")
            return

        order.transition_to(OrderStatus.NEW)
        await self._persist(order)
        self._emit_order_event(order)
        logger.info(f"[OMS] 订单已发送: {order}")

    async def cancel(self, order_id: str) -> bool:
        """取消订单"""
        order = self._orders.get(order_id)
        if not order:
            return False
        if order.status in TERMINAL_STATUSES:
            return False
        order.transition_to(OrderStatus.CANCELLED, "用户取消")
        self._active_orders.pop(order.id, None)
        # 解锁余额
        if self._portfolio:
            self._portfolio.unlock_balance(order.id)
        await self._persist(order)
        self._emit_order_event(order)
        logger.info(f"[OMS] 订单已取消: {order}")
        return True

    # ============================================================
    #  订单状态更新
    # ============================================================

    async def _on_order_event(self, event: MarketEvent):
        """处理交易所返回的订单状态更新"""
        d = event.data
        order_id = d.get("order_id", "")
        order = self._orders.get(order_id)
        if not order:
            # 可能是exchange_order_id
            for o in self._orders.values():
                if o.exchange_order_id == order_id:
                    order = o
                    break
        if not order:
            return

        exchange_status = d.get("status", "")
        new_status = self._map_exchange_status(exchange_status)

        if new_status and order.can_transition_to(new_status):
            if new_status == OrderStatus.FILLED or new_status == OrderStatus.PARTIALLY_FILLED:
                fill_price = d.get("avg_fill_price", d.get("avg_price", order.price))
                fill_qty = d.get("filled_qty", d.get("filled", 0))
                fee = d.get("fee", 0)
                order.apply_fill(fill_price, fill_qty, fee)

                # 更新持仓
                if self._portfolio:
                    await self._portfolio.on_fill(order, fill_price, fill_qty)
                    if new_status == OrderStatus.FILLED:
                        self._portfolio.unlock_balance(order.id)

            elif new_status in TERMINAL_STATUSES:
                order.transition_to(new_status, d.get("error_msg", ""))
                self._active_orders.pop(order.id, None)
                if self._portfolio:
                    self._portfolio.unlock_balance(order.id)

                if self._on_order_complete:
                    await self._on_order_complete(order)

            await self._persist(order)
            self._emit_order_event(order)

    def _map_exchange_status(self, status: str) -> Optional[OrderStatus]:
        """交易所状态 → 内部状态"""
        s = status.upper().replace("_", "")
        mapping = {
            "NEW": OrderStatus.NEW,
            "PARTIALLYFILLED": OrderStatus.PARTIALLY_FILLED,
            "FILLED": OrderStatus.FILLED,
            "CANCELED": OrderStatus.CANCELLED,
            "CANCELLED": OrderStatus.CANCELLED,
            "REJECTED": OrderStatus.REJECTED,
            "EXPIRED": OrderStatus.EXPIRED,
        }
        return mapping.get(s)

    # ============================================================
    #  速率限制
    # ============================================================

    def _check_rate(self, symbol: str) -> bool:
        """检查下单频率"""
        now = time.time()
        timestamps = self._rate_tracker[symbol]

        # 清理过期记录
        self._rate_tracker[symbol] = [t for t in timestamps if now - t < self._rate_window_sec]

        if len(self._rate_tracker[symbol]) >= self._max_rate:
            return False

        self._rate_tracker[symbol].append(now)
        return True

    # ============================================================
    #  持久化
    # ============================================================

    async def _persist(self, order: Order):
        """持久化订单到DB（自动判断新建还是更新）"""
        if not self._db:
            return
        try:
            from ..db.repository import OrderRepository
            repo = OrderRepository(self._db)
            # First persistence: insert new row
            if order.id not in self._persisted_ids:
                await repo.save(order.to_db_dict())
                self._persisted_ids.add(order.id)
            else:
                # Subsequent updates: update status only
                await repo.update_status(
                    order.id, order.status.value,
                    filled_qty=order.filled_qty,
                    remaining_qty=order.remaining_qty,
                    avg_fill_price=order.avg_fill_price,
                    fee=order.fee,
                    error_msg=order.error_msg,
                    completed_at=order.completed_at,
                    updated_at=order.updated_at,
                )
        except Exception as e:
            logger.error(f"[OMS] 持久化失败: {e}")

    async def persist_new(self, order: Order):
        """新建订单持久化（外部调用入口）"""
        if not self._db:
            return
        try:
            from ..db.repository import OrderRepository
            repo = OrderRepository(self._db)
            await repo.save(order.to_db_dict())
            self._persisted_ids.add(order.id)
        except Exception as e:
            logger.error(f"[OMS] 持久化失败: {e}")

    # ============================================================
    #  事件发布
    # ============================================================

    def _emit_order_event(self, order: Order):
        """发布订单事件"""
        self._event_bus.emit_nowait(MarketEvent(
            EventType.ORDER_STATUS,
            order.exchange,
            order.symbol,
            order.updated_at,
            {
                "order_id": order.id,
                "exchange_order_id": order.exchange_order_id,
                "side": order.side.value,
                "type": order.order_type.value,
                "price": order.price,
                "qty": order.quantity,
                "status": order.status.value,
                "filled": order.filled_qty,
                "remaining": order.remaining_qty,
                "avg_price": order.avg_fill_price,
                "fee": order.fee,
                "strategy": order.strategy,
                "client_id": order.client_order_id,
                "error_msg": order.error_msg,
            }
        ))

    # ============================================================
    #  查询接口
    # ============================================================

    def get_order(self, order_id: str) -> Optional[Order]:
        return self._orders.get(order_id)

    def get_active_orders(self, symbol: str = None, strategy: str = None) -> List[Order]:
        orders = list(self._active_orders.values())
        if symbol:
            orders = [o for o in orders if o.symbol == symbol]
        if strategy:
            orders = [o for o in orders if o.strategy == strategy]
        return orders

    def get_order_history(self, limit: int = 100) -> List[Order]:
        """最近订单（按时间倒序）"""
        sorted_orders = sorted(
            self._orders.values(),
            key=lambda o: o.created_at, reverse=True
        )
        return sorted_orders[:limit]

    def get_stats(self) -> dict:
        active = len(self._active_orders)
        total = len(self._orders)
        filled = sum(1 for o in self._orders.values() if o.is_filled)
        return {
            "name": self.name,
            "status": self.status.value,
            "total_orders": total,
            "active_orders": active,
            "filled_orders": filled,
            "recovered": self._recovered,
        }
