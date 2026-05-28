"""WebSocket endpoint for real-time data broadcast."""

import time
import random
import asyncio
import logging
from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from .shared import load_config, _startup_time

logger = logging.getLogger("xtquant.web")
router = APIRouter()


@router.websocket("/ws")
async def ws_endpoint(ws: WebSocket):
    await ws.accept()
    symbols = ["BTCUSDT", "ETHUSDT", "BNBUSDT"]
    base_prices = {"BTCUSDT": 68000.0, "ETHUSDT": 3500.0, "BNBUSDT": 600.0}
    prices = dict(base_prices)
    tick = 0

    async def send_status():
        cfg = load_config()
        equity = 100000.0 + random.uniform(-2000, 5000)
        curve = []
        peak = equity
        for i in range(60):
            val = 100000.0 + i * 50 + random.uniform(-3000, 3000)
            peak = max(peak, val)
            curve.append({"time": int(time.time()) - (60 - i), "equity": round(val, 2)})
        return {
            "type": "status",
            "data": {
                "runtime_seconds": int(time.time() - _startup_time),
                "portfolio": {
                    "total_equity": round(equity, 2),
                    "available_balance": round(equity * 0.65, 2),
                    "margin_used": round(equity * 0.35, 2),
                },
                "risk": {
                    "current_drawdown_pct": f"{round(random.uniform(0.5, 5.0), 2)}%",
                    "daily_orders": random.randint(3, 30),
                    "consecutive_losses": random.randint(0, 2),
                },
                "equity_curve": curve,
                "strategies": {},
            },
        }

    async def send_price(sym):
        base = base_prices[sym]
        prices[sym] = max(base * 0.9, min(base * 1.1, prices[sym] + random.gauss(0, base * 0.002)))
        price = prices[sym]
        change = (price - base) / base * 100
        return {
            "type": "price",
            "symbol": sym,
            "data": {
                "last": round(price, 2),
                "price": round(price, 2),
                "change_pct": round(change, 4),
                "high": round(price * (1 + abs(random.gauss(0, 0.005))), 2),
                "low": round(price * (1 - abs(random.gauss(0, 0.005))), 2),
                "volume": round(random.uniform(100, 5000), 1),
            },
        }

    async def send_orderbook(sym):
        mid = prices[sym]
        bids = []
        asks = []
        for i in range(15):
            bid_price = mid * (1 - (i + 1) * 0.0008)
            ask_price = mid * (1 + (i + 1) * 0.0008)
            bids.append([round(bid_price, 2), round(random.uniform(0.1, 5), 4)])
            asks.append([round(ask_price, 2), round(random.uniform(0.1, 5), 4)])
        return {"type": "orderbook", "data": {"symbol": sym, "bids": bids, "asks": asks}}

    async def send_trades():
        trades = []
        for _ in range(10):
            sym = random.choice(symbols)
            side = random.choice(["BUY", "SELL"])
            price = round(prices[sym] * (1 + random.uniform(-0.002, 0.002)), 2)
            trades.append({
                "symbol": sym,
                "side": side,
                "price": price,
                "qty": round(random.uniform(0.001, 2), 4),
                "time": int(time.time() * 1000),
            })
        return {"type": "trades", "data": trades}

    try:
        while True:
            try:
                await asyncio.wait_for(ws.receive_text(), timeout=1.0)
            except asyncio.TimeoutError:
                pass
            except WebSocketDisconnect:
                break

            try:
                tick += 1
                await ws.send_json(await send_status())
                for sym in symbols:
                    await ws.send_json(await send_price(sym))
                if tick % 2 == 0:
                    await ws.send_json(await send_orderbook("BTCUSDT"))
                if tick % 3 == 0:
                    await ws.send_json(await send_trades())
            except Exception:
                break

            await asyncio.sleep(1.0)
    except WebSocketDisconnect:
        pass
