"""Order placement, cancellation, history routes."""

import time
import logging
from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

from .shared import get_orders_store, next_order_id

logger = logging.getLogger("xtquant.web")
router = APIRouter()


@router.get("/api/orders")
async def api_get_orders(symbol: str = None):
    orders = list(get_orders_store().values())
    if symbol:
        orders = [o for o in orders if o["symbol"] == symbol]
    active = [o for o in orders if o["status"] not in ("CANCELLED", "FILLED", "REJECTED")]
    return JSONResponse(active)


@router.post("/api/order")
async def api_place_order(request: Request):
    try:
        data = await request.json()
        order_id = next_order_id()
        price = float(data.get("price", 0))
        qty = float(data.get("quantity", 0))
        order = {
            "order_id": order_id,
            "symbol": data.get("symbol", "BTCUSDT"),
            "side": data.get("side", "BUY"),
            "order_type": data.get("order_type", "LIMIT"),
            "price": price,
            "quantity": qty,
            "filled": 0,
            "status": "NEW",
            "exchange": data.get("exchange", "BINANCE"),
            "created_at": time.time(),
        }
        get_orders_store()[order_id] = order
        return JSONResponse({"status": "ok", "order_id": order_id})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)


@router.post("/api/orders/{order_id}/cancel")
async def api_cancel_order(order_id: str):
    order = get_orders_store().get(order_id)
    if not order:
        return JSONResponse({"detail": "Order not found"}, status_code=404)
    if order["status"] in ("CANCELLED", "FILLED"):
        return JSONResponse({"detail": f"Order already {order['status'].lower()}"}, status_code=400)
    order["status"] = "CANCELLED"
    return JSONResponse({"status": "ok"})


@router.get("/api/orders/history")
async def api_order_history(symbol: str = None, limit: int = 50):
    orders = [o for o in get_orders_store().values() if o["status"] in ("CANCELLED", "FILLED", "REJECTED")]
    if symbol:
        orders = [o for o in orders if o["symbol"] == symbol]
    orders.sort(key=lambda o: o.get("created_at", 0), reverse=True)
    return JSONResponse(orders[:limit])
