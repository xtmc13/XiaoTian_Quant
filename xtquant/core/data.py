"""
小天量化交易 - 数据模型
统一数据中间表达层，所有交易所数据转换为标准格式
"""

from dataclasses import dataclass, field
from typing import List
from .event import MarketEvent, EventType


@dataclass
class Tick:
    """Tick数据 - 最新成交"""
    exchange: str
    symbol: str
    price: float
    volume: float
    timestamp: int
    bid: float = 0.0
    ask: float = 0.0
    trade_id: str = ""
    is_buyer_maker: bool = False

    def to_event(self) -> MarketEvent:
        return MarketEvent(
            EventType.TICK, self.exchange, self.symbol, self.timestamp,
            {
                "price": self.price, "volume": self.volume,
                "bid": self.bid, "ask": self.ask,
                "trade_id": self.trade_id, "is_buyer_maker": self.is_buyer_maker
            },
            self
        )


@dataclass
class OrderBook:
    """订单簿深度"""
    symbol: str
    exchange: str
    bids: List[List[float]] = field(default_factory=list)   # [[price, qty], ...]
    asks: List[List[float]] = field(default_factory=list)
    timestamp: int = 0
    last_update_id: int = 0

    def best_bid(self) -> float:
        return float(self.bids[0][0]) if self.bids else 0.0

    def best_ask(self) -> float:
        return float(self.asks[0][0]) if self.asks else 0.0

    def mid_price(self) -> float:
        b, a = self.best_bid(), self.best_ask()
        return (b + a) / 2 if b and a else 0.0

    def spread(self) -> float:
        b, a = self.best_bid(), self.best_ask()
        return (a - b) / mid if (mid := self.mid_price()) > 0 else 0.0

    def spread_bps(self) -> float:
        return self.spread() * 10000

    def imbalance(self, depth: int = 5) -> float:
        """订单簿失衡度: >0 买方占优, <0 卖方占优"""
        bid_vol = sum(float(q) for _, q in self.bids[:depth])
        ask_vol = sum(float(q) for _, q in self.asks[:depth])
        total = bid_vol + ask_vol
        return (bid_vol - ask_vol) / total if total > 0 else 0.0

    def weighted_mid(self) -> float:
        """成交量加权中间价"""
        b, bv = self.best_bid(), float(self.bids[0][1]) if self.bids else 0
        a, av = self.best_ask(), float(self.asks[0][1]) if self.asks else 0
        total = bv + av
        return (b * av + a * bv) / total if total > 0 else self.mid_price()

    def to_event(self) -> MarketEvent:
        return MarketEvent(
            EventType.ORDERBOOK, self.exchange, self.symbol, self.timestamp,
            {
                "bids": self.bids, "asks": self.asks,
                "mid": self.mid_price(), "spread": self.spread(),
                "imbalance": self.imbalance(), "weighted_mid": self.weighted_mid()
            },
            self
        )


@dataclass
class Bar:
    """K线数据"""
    exchange: str
    symbol: str
    interval: str          # 1m, 5m, 15m, 1h, 4h, 1d
    open: float
    high: float
    low: float
    close: float
    volume: float
    quote_volume: float = 0.0
    trade_count: int = 0
    timestamp: int = 0     # 开盘时间

    def to_event(self) -> MarketEvent:
        return MarketEvent(
            EventType.BAR, self.exchange, self.symbol, self.timestamp,
            {
                "open": self.open, "high": self.high, "low": self.low,
                "close": self.close, "volume": self.volume,
                "quote_volume": self.quote_volume, "trade_count": self.trade_count,
                "interval": self.interval
            },
            self
        )

    @property
    def range(self) -> float:
        return self.high - self.low

    @property
    def body(self) -> float:
        return abs(self.close - self.open)

    @property
    def is_green(self) -> bool:
        return self.close >= self.open


@dataclass
class Trade:
    """逐笔成交明细"""
    exchange: str
    symbol: str
    price: float
    quantity: float
    timestamp: int
    side: str = ""          # buy / sell
    trade_id: str = ""

    def to_event(self) -> MarketEvent:
        return MarketEvent(
            EventType.TRADE, self.exchange, self.symbol, self.timestamp,
            {"price": self.price, "quantity": self.quantity, "side": self.side, "trade_id": self.trade_id},
            self
        )


@dataclass
class OrderData:
    """订单数据 — 轻量级交易所事件数据对象 (区别于 order.types.Order)"""
    order_id: str
    exchange: str
    symbol: str
    side: str              # BUY / SELL
    order_type: str        # LIMIT / MARKET / STOP / TAKE_PROFIT
    price: float
    quantity: float
    status: str = "PENDING"    # PENDING / NEW / PARTIALLY_FILLED / FILLED / CANCELLED / REJECTED
    filled_qty: float = 0.0
    remaining_qty: float = 0.0
    avg_fill_price: float = 0.0
    fee: float = 0.0
    fee_asset: str = ""
    timestamp: int = 0
    update_time: int = 0
    client_order_id: str = ""

    def to_event(self) -> MarketEvent:
        return MarketEvent(
            EventType.ORDER_STATUS, self.exchange, self.symbol, self.timestamp,
            {
                "order_id": self.order_id, "side": self.side, "type": self.order_type,
                "price": self.price, "qty": self.quantity, "status": self.status,
                "filled_qty": self.filled_qty, "remaining_qty": self.remaining_qty,
                "avg_fill_price": self.avg_fill_price, "fee": self.fee,
                "client_id": self.client_order_id
            }
        )

    @property
    def is_active(self) -> bool:
        return self.status in ("PENDING", "NEW", "PARTIALLY_FILLED")

    @property
    def is_filled(self) -> bool:
        return self.status == "FILLED"


@dataclass
class Balance:
    """账户余额"""
    exchange: str
    asset: str
    free: float
    locked: float
    timestamp: int = 0

    @property
    def total(self) -> float:
        return self.free + self.locked

    def to_event(self) -> MarketEvent:
        return MarketEvent(
            EventType.BALANCE, self.exchange, self.asset, self.timestamp,
            {"asset": self.asset, "free": self.free, "locked": self.locked, "total": self.total},
            self
        )


@dataclass
class Position:
    """持仓"""
    exchange: str
    symbol: str
    side: str              # LONG / SHORT
    size: float
    entry_price: float
    mark_price: float = 0.0
    unrealized_pnl: float = 0.0
    realized_pnl: float = 0.0
    leverage: float = 1.0
    timestamp: int = 0

    def to_event(self) -> MarketEvent:
        return MarketEvent(
            EventType.POSITION, self.exchange, self.symbol, self.timestamp,
            {
                "side": self.side, "size": self.size, "entry_price": self.entry_price,
                "mark_price": self.mark_price, "unrealized_pnl": self.unrealized_pnl,
                "realized_pnl": self.realized_pnl, "leverage": self.leverage
            },
            self
        )
