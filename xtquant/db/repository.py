"""
Repository — 业务层数据访问接口
封装所有DB操作，提供类型安全的CRUD
"""

from __future__ import annotations
import time
import json
import logging
from typing import Optional, List

from .database import Database

logger = logging.getLogger("xtquant.db.repo")


class OrderRepository:
    """订单数据仓库"""

    def __init__(self, db: Database):
        self.db = db

    async def save(self, order_data: dict) -> str:
        order_data.setdefault("created_at", int(time.time() * 1000))
        order_data.setdefault("updated_at", int(time.time() * 1000))
        order_data.setdefault("status", "CREATED")
        order_data.setdefault("filled_qty", 0.0)
        order_data.setdefault("remaining_qty", order_data.get("quantity", 0.0))
        order_data.setdefault("avg_fill_price", 0.0)
        order_data.setdefault("fee", 0.0)
        order_data.setdefault("error_msg", "")
        order_data.setdefault("client_order_id", "")
        order_data.setdefault("strategy", "")
        order_data.setdefault("completed_at", 0)
        order_data.setdefault("raw_json", "{}")
        await self.db.insert("orders", order_data)
        return order_data["id"]

    async def update_status(self, order_id: str, status: str, **extra):
        data = {"status": status, "updated_at": int(time.time() * 1000), **extra}
        if status in ("FILLED", "CANCELLED", "REJECTED", "EXPIRED"):
            data["completed_at"] = int(time.time() * 1000)
        await self.db.update("orders", data, "id = ?", order_id)

    async def get(self, order_id: str) -> Optional[dict]:
        return await self.db.fetch_one("SELECT * FROM orders WHERE id = ?", order_id)

    async def get_open(self, exchange: str = None, symbol: str = None) -> List[dict]:
        sql = "SELECT * FROM orders WHERE status NOT IN ('FILLED', 'CANCELLED', 'REJECTED', 'EXPIRED')"
        params = []
        if exchange:
            sql += " AND exchange = ?"
            params.append(exchange)
        if symbol:
            sql += " AND symbol = ?"
            params.append(symbol)
        sql += " ORDER BY created_at DESC"
        return await self.db.fetch_all(sql, *params)

    async def get_by_strategy(self, strategy: str, limit: int = 100) -> List[dict]:
        return await self.db.fetch_all(
            "SELECT * FROM orders WHERE strategy = ? ORDER BY created_at DESC LIMIT ?",
            strategy, limit
        )

    async def get_history(
        self, exchange: str = None, symbol: str = None, limit: int = 100
    ) -> List[dict]:
        sql = "SELECT * FROM orders WHERE 1=1"
        params = []
        if exchange:
            sql += " AND exchange = ?"
            params.append(exchange)
        if symbol:
            sql += " AND symbol = ?"
            params.append(symbol)
        sql += " ORDER BY created_at DESC LIMIT ?"
        params.append(limit)
        return await self.db.fetch_all(sql, *params)

    async def count_today(self, exchange: str = None) -> int:
        """统计今日订单数"""
        today_start = int(time.time()) // 86400 * 86400 * 1000
        sql = "SELECT COUNT(*) as cnt FROM orders WHERE created_at >= ?"
        params = [today_start]
        if exchange:
            sql += " AND exchange = ?"
            params.append(exchange)
        row = await self.db.fetch_one(sql, *params)
        return row["cnt"] if row else 0


class TradeRepository:
    """成交数据仓库"""

    def __init__(self, db: Database):
        self.db = db

    async def save(self, trade_data: dict):
        await self.db.insert("trades", trade_data)

    async def get_by_order(self, order_id: str) -> List[dict]:
        return await self.db.fetch_all(
            "SELECT * FROM trades WHERE order_id = ? ORDER BY timestamp", order_id
        )

    async def get_recent(self, limit: int = 100) -> List[dict]:
        return await self.db.fetch_all(
            "SELECT * FROM trades ORDER BY timestamp DESC LIMIT ?", limit
        )


class PositionRepository:
    """持仓数据仓库"""

    def __init__(self, db: Database):
        self.db = db

    async def upsert(self, exchange: str, symbol: str, side: str, size: float,
                     entry_price: float, mark_price: float = 0,
                     unrealized_pnl: float = 0, realized_pnl: float = 0,
                     leverage: float = 1.0):
        data = {
            "exchange": exchange, "symbol": symbol, "side": side,
            "size": size, "entry_price": entry_price, "mark_price": mark_price,
            "unrealized_pnl": unrealized_pnl, "realized_pnl": realized_pnl,
            "leverage": leverage, "updated_at": int(time.time() * 1000),
        }
        await self.db.insert("positions", data)

    async def get(self, exchange: str, symbol: str, side: str = "LONG") -> Optional[dict]:
        return await self.db.fetch_one(
            "SELECT * FROM positions WHERE exchange = ? AND symbol = ? AND side = ?",
            exchange, symbol, side
        )

    async def get_all(self, exchange: str = None) -> List[dict]:
        if exchange:
            return await self.db.fetch_all(
                "SELECT * FROM positions WHERE exchange = ? AND size != 0", exchange
            )
        return await self.db.fetch_all("SELECT * FROM positions WHERE size != 0")

    async def delete(self, exchange: str, symbol: str, side: str = "LONG"):
        await self.db.update(
            "positions", {"size": 0, "updated_at": int(time.time() * 1000)},
            "exchange = ? AND symbol = ? AND side = ?",
            exchange, symbol, side
        )

    async def save_snapshot(self, exchange: str, symbol: str, size: float,
                            entry_price: float, mark_price: float,
                            unrealized_pnl: float, total_equity: float):
        await self.db.insert("position_snapshots", {
            "exchange": exchange, "symbol": symbol, "size": size,
            "entry_price": entry_price, "mark_price": mark_price,
            "unrealized_pnl": unrealized_pnl, "total_equity": total_equity,
            "timestamp": int(time.time() * 1000),
        })


class AccountRepository:
    """账户数据仓库"""

    def __init__(self, db: Database):
        self.db = db

    async def upsert_balance(self, exchange: str, asset: str, free: float, locked: float):
        await self.db.insert("accounts", {
            "exchange": exchange, "asset": asset, "free": free, "locked": locked,
            "updated_at": int(time.time() * 1000),
        })

    async def get_balances(self, exchange: str = None) -> List[dict]:
        if exchange:
            return await self.db.fetch_all(
                "SELECT * FROM accounts WHERE exchange = ? AND (free + locked) > 0", exchange
            )
        return await self.db.fetch_all(
            "SELECT * FROM accounts WHERE (free + locked) > 0"
        )

    async def save_snapshot(self, exchange: str, total_equity: float,
                            available_balance: float, margin_used: float = 0,
                            margin_ratio: float = 0):
        await self.db.insert("account_snapshots", {
            "exchange": exchange, "total_equity": total_equity,
            "available_balance": available_balance, "margin_used": margin_used,
            "margin_ratio": margin_ratio, "timestamp": int(time.time() * 1000),
        })

    async def get_latest_snapshot(self, exchange: str) -> Optional[dict]:
        return await self.db.fetch_one(
            "SELECT * FROM account_snapshots WHERE exchange = ? ORDER BY timestamp DESC LIMIT 1",
            exchange
        )


class SignalRepository:
    """信号数据仓库"""

    def __init__(self, db: Database):
        self.db = db

    async def save(self, strategy: str, symbol: str, signal_type: str,
                   strength: float = 0, price: float = 0, metadata: dict = None):
        await self.db.insert("signals", {
            "strategy": strategy, "symbol": symbol, "signal_type": signal_type,
            "strength": strength, "price": price,
            "metadata": json.dumps(metadata or {}),
            "timestamp": int(time.time() * 1000),
        })

    async def get_recent(self, strategy: str = None, limit: int = 100) -> List[dict]:
        if strategy:
            return await self.db.fetch_all(
                "SELECT * FROM signals WHERE strategy = ? ORDER BY timestamp DESC LIMIT ?",
                strategy, limit
            )
        return await self.db.fetch_all(
            "SELECT * FROM signals ORDER BY timestamp DESC LIMIT ?", limit
        )


class RiskEventRepository:
    """风控事件数据仓库"""

    def __init__(self, db: Database):
        self.db = db

    async def save(self, check_name: str, level: str, detail: str,
                   symbol: str = "", strategy: str = "", order_id: str = ""):
        await self.db.insert("risk_events", {
            "check_name": check_name, "level": level, "detail": detail,
            "symbol": symbol, "strategy": strategy, "order_id": order_id,
            "timestamp": int(time.time() * 1000),
        })

    async def get_recent(self, level: str = None, limit: int = 100) -> List[dict]:
        if level:
            return await self.db.fetch_all(
                "SELECT * FROM risk_events WHERE level = ? ORDER BY timestamp DESC LIMIT ?",
                level, limit
            )
        return await self.db.fetch_all(
            "SELECT * FROM risk_events ORDER BY timestamp DESC LIMIT ?", limit
        )


class MarketDataRepository:
    """行情数据仓库"""

    def __init__(self, db: Database):
        self.db = db

    async def save_bar(self, exchange: str, symbol: str, interval: str,
                       open_time: int, open_: float, high: float, low: float,
                       close: float, volume: float, quote_volume: float = 0,
                       trade_count: int = 0):
        await self.db.insert("market_bars", {
            "exchange": exchange, "symbol": symbol, "interval": interval,
            "open_time": open_time, "open": open_, "high": high, "low": low,
            "close": close, "volume": volume, "quote_volume": quote_volume,
            "trade_count": trade_count,
        })

    async def get_bars(self, exchange: str, symbol: str, interval: str,
                       start_time: int = 0, end_time: int = None, limit: int = 500) -> List[dict]:
        if end_time is None:
            end_time = int(time.time() * 1000)
        sql = ("SELECT * FROM market_bars WHERE exchange = ? AND symbol = ? "
               "AND interval = ? AND open_time >= ? AND open_time <= ? "
               "ORDER BY open_time ASC LIMIT ?")
        return await self.db.fetch_all(sql, exchange, symbol, interval, start_time, end_time, limit)

    async def get_latest_bar(self, exchange: str, symbol: str, interval: str) -> Optional[dict]:
        return await self.db.fetch_one(
            "SELECT * FROM market_bars WHERE exchange = ? AND symbol = ? "
            "AND interval = ? ORDER BY open_time DESC LIMIT 1",
            exchange, symbol, interval
        )
