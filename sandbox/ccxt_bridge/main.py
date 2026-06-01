"""
CCXT Bridge — Wraps 100+ exchanges behind a unified REST API.
Go gateway calls this to get instant access to ALL CCXT-supported exchanges.
"""
import asyncio
import os
from typing import Any, Dict, List, Optional

import ccxt
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

app = FastAPI(title="XiaoTianQuant CCXT Bridge", version="1.0.0")

# Cache exchange instances
_exchanges: Dict[str, ccxt.Exchange] = {}


def get_exchange(name: str, config: Optional[Dict[str, str]] = None) -> ccxt.Exchange:
    """Get or create an exchange instance."""
    key = name
    if config:
        key += ":" + (config.get("apiKey", "") or "")
    if key not in _exchanges:
        exchange_class = getattr(ccxt, name, None)
        if exchange_class is None:
            raise HTTPException(status_code=400, detail=f"Unknown exchange: {name}")
        ex = exchange_class(config or {})
        if "urls" in ex.describe():
            ex.urls["api"] = ex.describe()["urls"]["api"]
        _exchanges[key] = ex
    return _exchanges[key]


# ═══════════════════════════════════════════════════════════════
# Request / Response Models
# ═══════════════════════════════════════════════════════════════

class OrderRequest(BaseModel):
    exchange: str
    symbol: str
    side: str  # buy / sell
    order_type: str = "limit"  # limit / market
    price: float = 0
    quantity: float = 0
    api_key: str = ""
    api_secret: str = ""
    password: str = ""


class OHLCVRequest(BaseModel):
    exchange: str
    symbol: str
    timeframe: str = "1h"
    limit: int = 200
    since: Optional[int] = None


class BalanceRequest(BaseModel):
    exchange: str
    api_key: str = ""
    api_secret: str = ""


class CancelRequest(BaseModel):
    exchange: str
    symbol: str
    order_id: str
    api_key: str = ""
    api_secret: str = ""


# ═══════════════════════════════════════════════════════════════
# Endpoints
# ═══════════════════════════════════════════════════════════════

@app.get("/health")
def health():
    return {"status": "ok", "exchanges": list(ccxt.exchanges[:10]) + ["..."]}


@app.get("/exchanges")
def list_exchanges():
    """List all supported CCXT exchanges."""
    return {"exchanges": ccxt.exchanges}


@app.post("/markets")
def get_markets(req: dict):
    """Get markets for an exchange."""
    ex = get_exchange(req["exchange"])
    try:
        markets = ex.load_markets()
        result = {}
        for symbol, m in markets.items():
            result[symbol] = {
                "base": m.get("base", ""),
                "quote": m.get("quote", ""),
                "type": m.get("type", ""),
                "spot": m.get("spot", False),
                "active": m.get("active", False),
                "precision": {
                    "price": m.get("precision", {}).get("price"),
                    "amount": m.get("precision", {}).get("amount"),
                },
                "limits": {
                    "amount": m.get("limits", {}).get("amount"),
                    "price": m.get("limits", {}).get("price"),
                    "cost": m.get("limits", {}).get("cost"),
                },
            }
        return {"exchange": req["exchange"], "markets": result, "count": len(result)}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/ticker")
def get_ticker(req: dict):
    """Get ticker for symbols."""
    ex = get_exchange(req["exchange"])
    try:
        symbols = req.get("symbols", [])
        if not symbols:
            raise HTTPException(status_code=400, detail="symbols required")
        tickers = ex.fetch_tickers(symbols)
        return {"exchange": req["exchange"], "tickers": {k: _simplify_ticker(v) for k, v in tickers.items()}}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/ohlcv")
def get_ohlcv(req: OHLCVRequest):
    """Fetch OHLCV candlestick data."""
    ex = get_exchange(req.exchange)
    try:
        bars = ex.fetch_ohlcv(req.symbol, req.timeframe, limit=req.limit, since=req.since)
        result = []
        for b in bars:
            result.append({
                "time": b[0], "open": b[1], "high": b[2],
                "low": b[3], "close": b[4], "volume": b[5]
            })
        return {"exchange": req.exchange, "symbol": req.symbol, "bars": result, "count": len(result)}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/balance")
def get_balance(req: BalanceRequest):
    """Fetch account balance."""
    config = {}
    if req.api_key:
        config["apiKey"] = req.api_key
        config["secret"] = req.api_secret
    ex = get_exchange(req.exchange, config)
    try:
        balance = ex.fetch_balance()
        free = {k: v for k, v in balance.get("free", {}).items() if v and v > 0}
        used = {k: v for k, v in balance.get("used", {}).items() if v and v > 0}
        return {
            "exchange": req.exchange,
            "free": free,
            "used": used,
            "total": {k: free.get(k, 0) + used.get(k, 0) for k in set(free) | set(used)},
        }
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/order")
def place_order(req: OrderRequest):
    """Place a new order."""
    config = {"apiKey": req.api_key, "secret": req.api_secret}
    if req.password:
        config["password"] = req.password
    ex = get_exchange(req.exchange, config)
    try:
        if req.order_type == "market":
            order = ex.create_market_order(req.symbol, req.side, req.quantity)
        else:
            order = ex.create_limit_order(req.symbol, req.side, req.quantity, req.price)
        return {"exchange": req.exchange, "order": order}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/cancel")
def cancel_order(req: CancelRequest):
    """Cancel an existing order."""
    config = {"apiKey": req.api_key, "secret": req.api_secret}
    ex = get_exchange(req.exchange, config)
    try:
        result = ex.cancel_order(req.order_id, req.symbol)
        return {"exchange": req.exchange, "result": result}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/orders")
def get_orders(req: dict):
    """Fetch open orders."""
    config = {}
    if req.get("api_key"):
        config["apiKey"] = req["api_key"]
        config["secret"] = req.get("api_secret", "")
    ex = get_exchange(req["exchange"], config)
    try:
        orders = ex.fetch_open_orders(req.get("symbol"))
        return {"exchange": req["exchange"], "orders": orders}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/positions")
def get_positions(req: dict):
    """Fetch open positions."""
    config = {}
    if req.get("api_key"):
        config["apiKey"] = req["api_key"]
        config["secret"] = req.get("api_secret", "")
    ex = get_exchange(req["exchange"], config)
    try:
        positions = ex.fetch_positions(req.get("symbols"))
        return {"exchange": req["exchange"], "positions": positions}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


# ═══════════════════════════════════════════════════════════════
# Helpers
# ═══════════════════════════════════════════════════════════════

def _simplify_ticker(t: dict) -> dict:
    return {
        "symbol": t.get("symbol"),
        "last": t.get("last"),
        "bid": t.get("bid"),
        "ask": t.get("ask"),
        "high": t.get("high"),
        "low": t.get("low"),
        "volume": t.get("baseVolume") or t.get("volume"),
        "change_pct": t.get("percentage"),
        "timestamp": t.get("timestamp"),
    }


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8002)
