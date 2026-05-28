"""
Paper Trading / Dry Run Exchange Adapter.
Simulates order execution with configurable latency, slippage, and fee.

Patterns adapted from QuantDinger's dry_run_deviation.py.
"""

import time
import uuid
import random
import logging
from typing import Dict, Any, Optional
from dataclasses import dataclass, field

logger = logging.getLogger("xtquant.paper")

# ── Configuration ──

PAPER_INITIAL_BALANCE = float(__import__("os").getenv("PAPER_TRADING_INITIAL_BALANCE", "100000"))
PAPER_FEE_RATE = float(__import__("os").getenv("PAPER_TRADING_FEE_RATE", "0.001"))
PAPER_SLIPPAGE = float(__import__("os").getenv("PAPER_TRADING_SLIPPAGE", "0.0005"))
PAPER_LATENCY_MS = (50, 300)  # Simulated latency range

# Timeframe-aware slippage thresholds (bps), adapted from QuantDinger
SLIPPAGE_THRESHOLDS_BY_TIMEFRAME = {
    "1m":  {"good": 2,  "warn": 8,  "bad": 20},
    "5m":  {"good": 3,  "warn": 10, "bad": 25},
    "15m": {"good": 4,  "warn": 12, "bad": 30},
    "1h":  {"good": 5,  "warn": 15, "bad": 40},
    "4h":  {"good": 8,  "warn": 20, "bad": 50},
    "1d":  {"good": 10, "warn": 30, "bad": 80},
    "1w":  {"good": 15, "warn": 40, "bad": 100},
}


def get_slippage_verdict(slippage_bps: float, timeframe: str = "1h") -> str:
    """Evaluate slippage quality with a human-readable verdict."""
    thresholds = SLIPPAGE_THRESHOLDS_BY_TIMEFRAME.get(
        timeframe, SLIPPAGE_THRESHOLDS_BY_TIMEFRAME["1h"]
    )
    abs_bps = abs(slippage_bps)
    if abs_bps <= thresholds["good"]:
        return "good"
    elif abs_bps <= thresholds["warn"]:
        return "warning"
    else:
        return "poor"


@dataclass
class PaperPosition:
    symbol: str
    side: str  # long / short
    quantity: float
    entry_price: float
    current_price: float
    unrealized_pnl: float = 0.0
    realized_pnl: float = 0.0
    opened_at: float = field(default_factory=time.time)


@dataclass
class PaperOrder:
    order_id: str
    symbol: str
    side: str
    order_type: str  # market / limit
    price: Optional[float]
    quantity: float
    filled: float = 0.0
    avg_fill_price: float = 0.0
    status: str = "pending"  # pending / partial / filled / cancelled / rejected
    created_at: float = field(default_factory=time.time)


class PaperExchange:
    """Simulated exchange for paper trading with realistic fill simulation."""

    def __init__(self):
        self.balance: Dict[str, float] = {"USDT": PAPER_INITIAL_BALANCE}
        self.positions: Dict[str, PaperPosition] = {}
        self.orders: Dict[str, PaperOrder] = {}
        self.order_history: list = []
        self.trade_history: list = []
        self._prices: Dict[str, float] = {}  # Current simulated market prices

    # ── Market Data ──

    def set_price(self, symbol: str, price: float):
        """Update the current market price for a symbol."""
        old_price = self._prices.get(symbol)
        self._prices[symbol] = price
        # Update unrealized PnL
        if symbol in self.positions:
            pos = self.positions[symbol]
            pos.current_price = price
            if pos.side == "long":
                pos.unrealized_pnl = (price - pos.entry_price) * pos.quantity
            else:
                pos.unrealized_pnl = (pos.entry_price - price) * pos.quantity

    def get_price(self, symbol: str) -> float:
        return self._prices.get(symbol, 0.0)

    # ── Order Management ──

    def place_order(
        self,
        symbol: str,
        side: str,
        order_type: str = "market",
        price: Optional[float] = None,
        quantity: float = 0.0,
    ) -> PaperOrder:
        """Place a new order with simulated latency."""
        # Simulate latency
        time.sleep(random.uniform(*PAPER_LATENCY_MS) / 1000.0)

        order_id = f"paper_{uuid.uuid4().hex[:12]}"
        order = PaperOrder(
            order_id=order_id,
            symbol=symbol,
            side=side,
            order_type=order_type,
            price=price,
            quantity=quantity,
        )
        self.orders[order_id] = order

        # Market orders fill immediately
        if order_type == "market":
            self._fill_order(order)
        elif order_type == "limit" and price:
            # Limit orders fill if price is favorable
            current = self.get_price(symbol)
            if current and (
                (side == "buy" and current <= price) or
                (side == "sell" and current >= price)
            ):
                self._fill_order(order)

        self.order_history.append(order)
        return order

    def cancel_order(self, order_id: str) -> bool:
        order = self.orders.get(order_id)
        if not order or order.status not in ("pending", "partial"):
            return False
        order.status = "cancelled"
        return True

    def _fill_order(self, order: PaperOrder, fill_ratio: float = 1.0):
        """Simulate order fill with slippage and fees."""
        base_price = self.get_price(order.symbol)
        if not base_price:
            order.status = "rejected"
            return

        # Apply slippage
        slip = random.uniform(-PAPER_SLIPPAGE, PAPER_SLIPPAGE)
        if order.side == "buy":
            fill_price = base_price * (1 + abs(slip))
        else:
            fill_price = base_price * (1 - abs(slip))

        # Apply fee
        fee = fill_price * order.quantity * fill_ratio * PAPER_FEE_RATE

        # Update balance
        quote = order.symbol[-4:] if order.symbol.endswith(("USDT", "USDC")) else "USDT"
        if quote not in self.balance:
            self.balance[quote] = PAPER_INITIAL_BALANCE

        if order.side == "buy":
            cost = fill_price * order.quantity * fill_ratio + fee
            if self.balance.get(quote, 0) >= cost:
                self.balance[quote] -= cost
            else:
                order.status = "rejected"
                return
        else:
            proceeds = fill_price * order.quantity * fill_ratio - fee
            self.balance[quote] += proceeds

        # Update position
        pos_key = order.symbol
        if pos_key in self.positions:
            pos = self.positions[pos_key]
            if order.side == "buy":
                new_qty = pos.quantity + order.quantity * fill_ratio
                pos.entry_price = (
                    (pos.entry_price * pos.quantity + fill_price * order.quantity * fill_ratio)
                    / new_qty
                )
                pos.quantity = new_qty
            else:
                pos.quantity -= order.quantity * fill_ratio
                if pos.quantity <= 0:
                    self.positions.pop(pos_key)
        else:
            if order.side == "buy":
                self.positions[pos_key] = PaperPosition(
                    symbol=order.symbol,
                    side="long",
                    quantity=order.quantity * fill_ratio,
                    entry_price=fill_price,
                    current_price=fill_price,
                )

        # Record trade
        order.filled = order.quantity * fill_ratio
        order.avg_fill_price = fill_price
        order.status = "filled" if fill_ratio >= 1.0 else "partial"

        self.trade_history.append({
            "order_id": order.order_id,
            "symbol": order.symbol,
            "side": order.side,
            "price": fill_price,
            "quantity": order.filled,
            "fee": fee,
            "timestamp": time.time(),
        })

        logger.info(
            f"[PAPER] {order.side.upper()} {order.quantity} {order.symbol} @ {fill_price:.4f}"
        )

    # ── Portfolio ──

    def get_equity(self) -> float:
        """Calculate total portfolio equity."""
        equity = self.balance.get("USDT", 0.0)
        for pos in self.positions.values():
            if pos.side == "long":
                equity += pos.current_price * pos.quantity
            else:
                equity += pos.entry_price * pos.quantity * 2 - pos.current_price * pos.quantity
        return equity

    def get_portfolio_summary(self) -> Dict[str, Any]:
        equity = self.get_equity()
        initial = PAPER_INITIAL_BALANCE
        return {
            "total_equity": round(equity, 2),
            "available_balance": round(self.balance.get("USDT", 0), 2),
            "unrealized_pnl": round(
                sum(p.unrealized_pnl for p in self.positions.values()), 2
            ),
            "total_return_pct": round((equity - initial) / initial * 100, 2),
            "positions": [
                {
                    "symbol": p.symbol,
                    "side": p.side,
                    "quantity": p.quantity,
                    "entry_price": p.entry_price,
                    "current_price": p.current_price,
                    "unrealized_pnl": round(p.unrealized_pnl, 2),
                }
                for p in self.positions.values()
            ],
            "open_orders": len([o for o in self.orders.values() if o.status in ("pending", "partial")]),
        }

    def reset(self):
        """Reset the paper trading account."""
        self.balance = {"USDT": PAPER_INITIAL_BALANCE}
        self.positions.clear()
        self.orders.clear()
        self.order_history.clear()
        self.trade_history.clear()
        logger.info("[PAPER] Account reset")


# Global paper trading instance
paper_exchange = PaperExchange()
