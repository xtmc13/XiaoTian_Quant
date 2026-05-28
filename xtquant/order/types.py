"""
订单类型定义 — 支持高级订单类型
"""

from __future__ import annotations
from enum import Enum
from dataclasses import dataclass, field
from typing import Dict, Any
import time
import uuid


class OrderStatus(Enum):
    """订单状态 — 与状态机同步"""
    CREATED = "CREATED"             # 策略生成，尚未风控
    PENDING = "PENDING"             # 通过风控，等待提交
    NEW = "NEW"                     # 交易所已确认
    PARTIALLY_FILLED = "PARTIALLY_FILLED"
    FILLED = "FILLED"
    CANCELLED = "CANCELLED"
    REJECTED = "REJECTED"          # 交易所拒绝
    EXPIRED = "EXPIRED"             # 超时


class OrderSide(Enum):
    BUY = "BUY"
    SELL = "SELL"


class OrderType(Enum):
    MARKET = "MARKET"
    LIMIT = "LIMIT"
    STOP_LOSS = "STOP_LOSS"
    TAKE_PROFIT = "TAKE_PROFIT"
    STOP_LOSS_LIMIT = "STOP_LOSS_LIMIT"
    TAKE_PROFIT_LIMIT = "TAKE_PROFIT_LIMIT"
    TRAILING_STOP = "TRAILING_STOP"
    OCO = "OCO"            # 二选一：止损+止盈，触发一个则取消另一个
    BRACKET = "BRACKET"    # 括号单：主单+止损+止盈
    ICEBERG = "ICEBERG"    # 冰山单
    TWAP = "TWAP"          # 时间加权均价
    VWAP = "VWAP"          # 成交量加权均价


class TimeInForce(Enum):
    GTC = "GTC"         # 有效直到取消
    IOC = "IOC"         # 立即成交或取消
    FOK = "FOK"         # 全部成交或取消
    GTX = "GTX"         # 做市商保护


class ExecAlgo(Enum):
    """执行算法类型"""
    DIRECT = "DIRECT"       # 直接发单
    TWAP = "TWAP"
    VWAP = "VWAP"
    POV = "POV"            # 成交量百分比


# 有效的状态转换
VALID_TRANSITIONS: dict[OrderStatus, set[OrderStatus]] = {
    OrderStatus.CREATED: {OrderStatus.PENDING, OrderStatus.REJECTED},
    OrderStatus.PENDING: {OrderStatus.NEW, OrderStatus.REJECTED, OrderStatus.EXPIRED},
    OrderStatus.NEW: {OrderStatus.PARTIALLY_FILLED, OrderStatus.FILLED,
                      OrderStatus.CANCELLED, OrderStatus.EXPIRED},
    OrderStatus.PARTIALLY_FILLED: {OrderStatus.PARTIALLY_FILLED, OrderStatus.FILLED,
                                   OrderStatus.CANCELLED, OrderStatus.EXPIRED},
    OrderStatus.FILLED: set(),       # 终态
    OrderStatus.CANCELLED: set(),    # 终态
    OrderStatus.REJECTED: set(),     # 终态
    OrderStatus.EXPIRED: set(),      # 终态
}

ACTIVE_STATUSES = {
    OrderStatus.CREATED, OrderStatus.PENDING, OrderStatus.NEW,
    OrderStatus.PARTIALLY_FILLED,
}

TERMINAL_STATUSES = {
    OrderStatus.FILLED, OrderStatus.CANCELLED,
    OrderStatus.REJECTED, OrderStatus.EXPIRED,
}


@dataclass
class OrderRequest:
    """
    统一下单请求 — 策略端使用的下单对象

    经过风控->OMS->执行引擎->交易所适配器
    """
    symbol: str
    side: OrderSide
    order_type: OrderType
    price: float
    quantity: float
    exchange: str = ""
    strategy: str = ""
    time_in_force: TimeInForce = TimeInForce.GTC
    exec_algo: ExecAlgo = ExecAlgo.DIRECT
    client_order_id: str = ""

    # 高级参数
    stop_price: float = 0.0          # 止损/止盈触发价
    trailing_delta: float = 0.0      # 追踪止损偏移
    iceberg_qty: float = 0.0         # 冰山单展示量
    twap_duration_sec: float = 300.0 # TWAP执行时长
    metadata: Dict[str, Any] = field(default_factory=dict)

    def __post_init__(self):
        if not self.client_order_id:
            self.client_order_id = f"xt_{uuid.uuid4().hex[:12]}"

    def to_dict(self) -> dict:
        return {
            "id": self.client_order_id,
            "symbol": self.symbol,
            "side": self.side.value,
            "order_type": self.order_type.value,
            "price": self.price,
            "quantity": self.quantity,
            "exchange": self.exchange,
            "strategy": self.strategy,
            "time_in_force": self.time_in_force.value,
            "exec_algo": self.exec_algo.value,
            "stop_price": self.stop_price,
            "trailing_delta": self.trailing_delta,
            "iceberg_qty": self.iceberg_qty,
            "metadata": self.metadata,
        }


@dataclass
class Order:
    """订单对象 — 全生命周期追踪"""
    id: str
    exchange: str
    symbol: str
    side: OrderSide
    order_type: OrderType
    price: float
    quantity: float
    status: OrderStatus = OrderStatus.CREATED
    filled_qty: float = 0.0
    remaining_qty: float = 0.0
    avg_fill_price: float = 0.0
    fee: float = 0.0
    fee_asset: str = ""
    strategy: str = ""
    client_order_id: str = ""
    exchange_order_id: str = ""
    time_in_force: TimeInForce = TimeInForce.GTC
    exec_algo: ExecAlgo = ExecAlgo.DIRECT
    stop_price: float = 0.0
    created_at: int = 0
    updated_at: int = 0
    completed_at: int = 0
    error_msg: str = ""

    def __post_init__(self):
        if not self.created_at:
            self.created_at = int(time.time() * 1000)
        if not self.remaining_qty:
            self.remaining_qty = self.quantity
        self.updated_at = int(time.time() * 1000)

    @classmethod
    def from_request(cls, req: OrderRequest) -> "Order":
        return cls(
            id=req.client_order_id,
            exchange=req.exchange,
            symbol=req.symbol,
            side=req.side,
            order_type=req.order_type,
            price=req.price,
            quantity=req.quantity,
            strategy=req.strategy,
            client_order_id=req.client_order_id,
            time_in_force=req.time_in_force,
            exec_algo=req.exec_algo,
            stop_price=req.stop_price,
        )

    @property
    def is_active(self) -> bool:
        return self.status in ACTIVE_STATUSES

    @property
    def is_terminal(self) -> bool:
        return self.status in TERMINAL_STATUSES

    @property
    def is_filled(self) -> bool:
        return self.status == OrderStatus.FILLED

    @property
    def fill_pct(self) -> float:
        return self.filled_qty / self.quantity if self.quantity > 0 else 0.0

    @property
    def notional(self) -> float:
        return self.price * self.quantity

    @property
    def executed_notional(self) -> float:
        return self.avg_fill_price * self.filled_qty if self.filled_qty > 0 else 0.0

    def can_transition_to(self, target: OrderStatus) -> bool:
        return target in VALID_TRANSITIONS.get(self.status, set())

    def transition_to(self, target: OrderStatus, error_msg: str = "") -> bool:
        if not self.can_transition_to(target):
            return False
        self.status = target
        self.updated_at = int(time.time() * 1000)
        if target in TERMINAL_STATUSES:
            self.completed_at = int(time.time() * 1000)
        if error_msg:
            self.error_msg = error_msg
        return True

    def apply_fill(self, fill_price: float, fill_qty: float, fee: float = 0,
                   fee_asset: str = ""):
        """处理部分/完全成交"""
        self.filled_qty += fill_qty
        self.remaining_qty = max(0, self.quantity - self.filled_qty)
        self.avg_fill_price = (
            (self.avg_fill_price * (self.filled_qty - fill_qty) + fill_price * fill_qty)
            / self.filled_qty if self.filled_qty > 0 else fill_price
        )
        self.fee += fee
        self.fee_asset = fee_asset
        self.updated_at = int(time.time() * 1000)

        if self.remaining_qty <= 0:
            self.transition_to(OrderStatus.FILLED)
        else:
            self.transition_to(OrderStatus.PARTIALLY_FILLED)

    def to_db_dict(self) -> dict:
        return {
            "id": self.id,
            "client_order_id": self.client_order_id,
            "exchange": self.exchange,
            "symbol": self.symbol,
            "side": self.side.value,
            "order_type": self.order_type.value,
            "price": self.price,
            "quantity": self.quantity,
            "status": self.status.value,
            "filled_qty": self.filled_qty,
            "remaining_qty": self.remaining_qty,
            "avg_fill_price": self.avg_fill_price,
            "fee": self.fee,
            "fee_asset": self.fee_asset,
            "strategy": self.strategy,
            "created_at": self.created_at,
            "updated_at": self.updated_at,
            "completed_at": self.completed_at,
            "error_msg": self.error_msg,
        }

    def __repr__(self):
        return (
            f"Order({self.id[:12]} | {self.symbol} {self.side.value} "
            f"{self.order_type.value} | {self.status.value} | "
            f"filled={self.filled_qty}/{self.quantity})"
        )
