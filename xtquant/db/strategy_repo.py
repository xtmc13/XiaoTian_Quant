"""
策略配置/日志/交易记录 Repository
"""

import time
import logging
from typing import Optional, List

logger = logging.getLogger("xtquant.strategy_repo")


class StrategyConfigRepository:
    """策略配置CRUD"""

    def __init__(self, db):
        self.db = db

    async def create(self, data: dict) -> str:
        await self.db.insert("strategy_configs", data)
        return data["id"]

    async def update(self, id: str, data: dict):
        data["updated_at"] = int(time.time() * 1000)
        await self.db.update("strategy_configs", data, "id = ?", id)

    async def delete(self, id: str):
        await self.db.update("strategy_configs",
            {"status": "deleted", "updated_at": int(time.time() * 1000)},
            "id = ?", id)

    async def get(self, id: str) -> Optional[dict]:
        return await self.db.fetch_one("SELECT * FROM strategy_configs WHERE id = ?", id)

    async def list(self, category: str = None, status: str = None,
                   coin: str = None, strategy_type: str = None,
                   is_template: bool = False,
                   limit: int = 100, offset: int = 0) -> List[dict]:
        conditions = ["is_template = ?"]
        params = [1 if is_template else 0]

        if category:
            conditions.append("category = ?")
            params.append(category)
        if status:
            conditions.append("status = ?")
            params.append(status)
        if coin:
            conditions.append("coin LIKE ?")
            params.append(f"%{coin}%")
        if strategy_type:
            conditions.append("strategy_type = ?")
            params.append(strategy_type)

        where = " AND ".join(conditions)
        sql = f"SELECT * FROM strategy_configs WHERE {where} ORDER BY updated_at DESC LIMIT ? OFFSET ?"
        params.extend([limit, offset])
        return await self.db.fetch_all(sql, *params)

    async def set_status(self, id: str, status: str):
        now = int(time.time() * 1000)
        data = {"status": status, "updated_at": now}
        if status == "running":
            data["started_at"] = now
        elif status == "stopped":
            data["stopped_at"] = now
        await self.db.update("strategy_configs", data, "id = ?", id)

    async def batch_set_status(self, ids: List[str], status: str):
        now = int(time.time() * 1000)
        for id in ids:
            data = {"status": status, "updated_at": now}
            if status == "running":
                data["started_at"] = now
            elif status == "stopped":
                data["stopped_at"] = now
            await self.db.update("strategy_configs", data, "id = ?", id)


class StrategyLogRepository:
    """策略运行日志"""

    def __init__(self, db):
        self.db = db

    async def append(self, strategy_id: str, level: str, message: str):
        await self.db.insert("strategy_run_logs", {
            "strategy_id": strategy_id,
            "level": level,
            "message": message,
            "timestamp": int(time.time() * 1000),
        })

    async def list(self, strategy_id: str = None, level: str = None,
                   limit: int = 100, offset: int = 0) -> List[dict]:
        conditions = []
        params = []
        if strategy_id:
            conditions.append("strategy_id = ?")
            params.append(strategy_id)
        if level:
            conditions.append("level = ?")
            params.append(level)

        if conditions:
            where = " AND ".join(conditions)
            sql = f"SELECT * FROM strategy_run_logs WHERE {where} ORDER BY timestamp DESC LIMIT ? OFFSET ?"
        else:
            sql = "SELECT * FROM strategy_run_logs ORDER BY timestamp DESC LIMIT ? OFFSET ?"
        params.extend([limit, offset])
        return await self.db.fetch_all(sql, *params)

    async def clear(self, strategy_id: str = None):
        if strategy_id:
            await self.db.execute("DELETE FROM strategy_run_logs WHERE strategy_id = ?", strategy_id)
        else:
            await self.db.execute("DELETE FROM strategy_run_logs")


class StrategyTradeLogRepository:
    """策略交易日志"""

    def __init__(self, db):
        self.db = db

    async def record(self, strategy_id: str, strategy_name: str, coin: str,
                     event_type: str, direction: str, price: float,
                     quantity: float, pnl: float = 0.0, note: str = ""):
        await self.db.insert("strategy_trade_logs", {
            "strategy_id": strategy_id,
            "strategy_name": strategy_name,
            "coin": coin,
            "event_type": event_type,
            "direction": direction,
            "price": price,
            "quantity": quantity,
            "pnl": pnl,
            "note": note,
            "timestamp": int(time.time() * 1000),
        })

    async def list(self, strategy_id: str = None,
                   limit: int = 100, offset: int = 0) -> List[dict]:
        if strategy_id:
            sql = "SELECT * FROM strategy_trade_logs WHERE strategy_id = ? ORDER BY timestamp DESC LIMIT ? OFFSET ?"
            return await self.db.fetch_all(sql, strategy_id, limit, offset)
        sql = "SELECT * FROM strategy_trade_logs ORDER BY timestamp DESC LIMIT ? OFFSET ?"
        return await self.db.fetch_all(sql, limit, offset)


class StrategyGlobalRepository:
    """策略全局设置（单例行）"""

    def __init__(self, db):
        self.db = db

    async def get(self) -> dict:
        row = await self.db.fetch_one("SELECT * FROM strategy_global_settings WHERE id = 1")
        if not row:
            now = int(time.time() * 1000)
            await self.db.insert("strategy_global_settings", {
                "id": 1,
                "profit_protection_enabled": 0,
                "max_concurrent_orders": 5,
                "updated_at": now,
            })
            return {
                "profit_protection_enabled": False,
                "max_concurrent_orders": 5,
                "updated_at": now,
            }
        return {
            "profit_protection_enabled": bool(row["profit_protection_enabled"]),
            "max_concurrent_orders": row["max_concurrent_orders"],
            "updated_at": row["updated_at"],
        }

    async def update(self, data: dict):
        data["id"] = 1
        data["updated_at"] = int(time.time() * 1000)
        await self.db.insert("strategy_global_settings", data)
