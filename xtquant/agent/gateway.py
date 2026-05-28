"""Agent Gateway — REST API /api/agent/v1 with auth middleware and audit logging."""

import json
import time
import logging
import os
from fastapi import Request
from fastapi.responses import JSONResponse

from .token import AgentTokenManager
from .audit import log_audit

logger = logging.getLogger("xtquant.agent.gateway")

SCOPE_ENDPOINT_MAP = {
    "GET /health": None,
    "GET /market": "read_market",
    "POST /backtest": "run_backtest",
    "GET /strategies": "read_market",
    "POST /strategies/deploy": "manage_strategies",
    "POST /strategies/*/start": "manage_strategies",
    "POST /strategies/*/stop": "manage_strategies",
    "DELETE /strategies/*": "manage_strategies",
    "POST /orders/paper": "trade",
    "GET /orders": "read_market",
    "DELETE /orders/*": "trade",
    "GET /account": "read_market",
}


def register_agent_gateway(app, engine, db):
    """Register Agent Gateway routes and middleware on a FastAPI app."""
    token_manager = AgentTokenManager(db)

    async def _get_token_info(request: Request) -> dict | None:
        auth = request.headers.get("Authorization", "")
        if not auth.startswith("Bearer "):
            return None
        token_str = auth[7:]
        return await token_manager.validate_token(token_str)

    async def _check_live_trading(token_info: dict) -> bool:
        if not token_info.get("paper_only", True):
            live_enabled = os.environ.get(
                "AGENT_LIVE_TRADING_ENABLED",
                os.environ.get("agent_live_trading_enabled", "false")
            ).lower() in ("1", "true", "yes")
            if not live_enabled:
                return False
        return True

    def _match_scope(method: str, path: str) -> str | None:
        import re
        for prefix, scope in SCOPE_ENDPOINT_MAP.items():
            parts = prefix.split(" ", 1)
            if len(parts) == 2 and parts[0] == method:
                pattern = parts[1].replace("*", "[^/]+")
                if re.match("^" + pattern + "$", path) or re.match("^" + pattern + "/", path):
                    return scope
        return None

    @app.middleware("http")
    async def agent_auth_middleware(request: Request, call_next):
        path = request.url.path
        if not path.startswith("/api/agent/v1/"):
            return await call_next(request)

        token_info = await _get_token_info(request)
        endpoint = path.replace("/api/agent/v1", "").split("?")[0]
        method = request.method
        ip = request.client.host if request.client else ""

        if token_info is None:
            # Allow localhost access without token
            if ip in ("127.0.0.1", "::1", ""):
                request.state.agent_token = {"token_id": "localhost", "token_name": "local",
                    "scopes": ["read_market", "run_backtest", "trade", "manage_strategies"],
                    "paper_only": True, "rate_limit": 0}
                return await call_next(request)
            await log_audit(db, endpoint=endpoint, method=method,
                          params_summary="no token", status_code=401, ip=ip)
            return JSONResponse({"detail": "Invalid or missing agent token"}, status_code=401)

        required_scope = _match_scope(method, endpoint)
        if required_scope and required_scope not in token_info.get("scopes", []):
            await log_audit(db, token_id=token_info["token_id"],
                          token_name=token_info["token_name"],
                          endpoint=endpoint, method=method,
                          params_summary=f"scope denied: need {required_scope}",
                          status_code=403, ip=ip)
            return JSONResponse(
                {"detail": f"Token lacks required scope: {required_scope}"},
                status_code=403
            )

        if not await _check_live_trading(token_info):
            await log_audit(db, token_id=token_info["token_id"],
                          token_name=token_info["token_name"],
                          endpoint=endpoint, method=method,
                          params_summary="live trading not enabled",
                          status_code=403, ip=ip)
            return JSONResponse(
                {"detail": "Live trading is not enabled on this server. Set AGENT_LIVE_TRADING_ENABLED=true"},
                status_code=403
            )

        request.state.agent_token = token_info
        response = await call_next(request)
        await log_audit(db, token_id=token_info["token_id"],
                      token_name=token_info["token_name"],
                      endpoint=endpoint, method=method,
                      params_summary="", status_code=response.status_code, ip=ip)
        return response

    # ── Routes ─────────────────────────────────────────────

    @app.get("/api/agent/v1/health")
    async def agent_health(request: Request):
        token_info = getattr(request.state, "agent_token", {})
        return {
            "status": "ok",
            "server_time": int(time.time() * 1000),
            "authenticated": True,
            "token_name": token_info.get("token_name", ""),
            "paper_only": token_info.get("paper_only", True),
            "scopes": token_info.get("scopes", []),
        }

    @app.get("/api/agent/v1/market/{symbol}")
    async def agent_market(request: Request, symbol: str):
        price = engine.get_price(symbol)
        tick = engine.get_tick(symbol)
        book = engine.get_orderbook(symbol)
        bid = book.bids[0][0] if book and book.bids else 0
        ask = book.asks[0][0] if book and book.asks else 0
        volume = tick.volume if tick else 0
        return {
            "symbol": symbol.upper(),
            "price": price or 0,
            "bid": bid,
            "ask": ask,
            "volume": volume,
            "timestamp": int(time.time() * 1000),
        }

    @app.post("/api/agent/v1/backtest")
    async def agent_backtest(request: Request):
        body = await request.json()
        result = await engine.run_backtest_web(body)
        return result

    @app.get("/api/agent/v1/strategies")
    async def agent_strategies(request: Request):
        repo = getattr(engine, "strategy_config_repo", None)
        if repo:
            rows = await repo.list(is_template=True)
            return [
                {
                    "id": r.get("id"),
                    "name": r.get("name"),
                    "category": r.get("category"),
                    "strategy_type": r.get("strategy_type"),
                    "coin": r.get("coin"),
                    "config_json": r.get("config_json"),
                }
                for r in (rows or [])
            ]
        return []

    @app.post("/api/agent/v1/strategies/deploy")
    async def agent_deploy_strategy(request: Request):
        body = await request.json()
        repo = getattr(engine, "strategy_config_repo", None)
        if not repo:
            return JSONResponse({"detail": "Strategy repo not available"}, status_code=500)
        import time as _time
        sid = await repo.insert({
            "name": body.get("name", "agent_strategy"),
            "strategy_type": body.get("strategy_type", "breakout"),
            "category": body.get("category", "agent"),
            "coin": body.get("coin", "BTCUSDT"),
            "config_json": json.dumps(body.get("config_json", {})),
            "is_template": 1,
            "status": "stopped",
            "created_at": int(_time.time()),
            "updated_at": int(_time.time()),
        })
        logger.info("[Agent] strategy deployed: %s (id=%s)", body.get("name"), sid)
        return {"status": "ok", "id": sid}

    @app.post("/api/agent/v1/strategies/{sid}/start")
    async def agent_start_strategy(request: Request, sid: str):
        repo = getattr(engine, "strategy_config_repo", None)
        if not repo:
            return JSONResponse({"detail": "Strategy repo not available"}, status_code=500)
        await repo.set_status(sid, "running")
        logger.info("[Agent] strategy started: %s", sid)
        return {"status": "ok", "id": sid, "action": "started"}

    @app.post("/api/agent/v1/strategies/{sid}/stop")
    async def agent_stop_strategy(request: Request, sid: str):
        repo = getattr(engine, "strategy_config_repo", None)
        if not repo:
            return JSONResponse({"detail": "Strategy repo not available"}, status_code=500)
        await repo.set_status(sid, "stopped")
        logger.info("[Agent] strategy stopped: %s", sid)
        return {"status": "ok", "id": sid, "action": "stopped"}

    @app.delete("/api/agent/v1/strategies/{sid}")
    async def agent_delete_strategy(request: Request, sid: str):
        repo = getattr(engine, "strategy_config_repo", None)
        if not repo:
            return JSONResponse({"detail": "Strategy repo not available"}, status_code=500)
        await repo.delete(sid)
        logger.info("[Agent] strategy deleted: %s", sid)
        return {"status": "ok", "id": sid, "action": "deleted"}

    @app.post("/api/agent/v1/orders/paper")
    async def agent_place_paper_order(request: Request):
        body = await request.json()
        symbol = body.get("symbol", "BTCUSDT").upper()
        side = body.get("side", "BUY").upper()
        order_type = body.get("order_type", "MARKET").upper()
        price = float(body.get("price", 0))
        quantity = float(body.get("quantity", 0.001))
        exchange_name = body.get("exchange", "")
        if not exchange_name:
            exchanges = getattr(engine, "_exchanges", {})
            exchange_name = next(iter(exchanges.keys()), "BINANCE") if exchanges else "BINANCE"
        try:
            order = await engine.place_order(
                exchange=exchange_name, symbol=symbol, side=side,
                order_type=order_type, price=price, quantity=quantity,
                strategy="agent"
            )
            return {
                "status": "ok",
                "order_id": getattr(order, "order_id", ""),
                "symbol": symbol, "side": side,
                "order_type": order_type, "quantity": quantity,
            }
        except Exception as e:
            logger.error("[Agent] paper order failed: %s", e)
            return JSONResponse({"detail": str(e)[:200]}, status_code=500)

    @app.get("/api/agent/v1/orders")
    async def agent_get_orders(request: Request):
        symbol = request.query_params.get("symbol", "").upper()
        oms = getattr(engine, "oms", None)
        if not oms:
            return []
        active = oms.get_active_orders()
        result = []
        for o in (active or []):
            od = o.__dict__ if hasattr(o, "__dict__") else o
            sym = od.get("symbol", "")
            if symbol and sym.upper() != symbol:
                continue
            result.append({
                "order_id": od.get("order_id", ""),
                "symbol": sym,
                "side": od.get("side", ""),
                "order_type": od.get("order_type", ""),
                "price": od.get("price", 0),
                "quantity": od.get("quantity", 0),
                "filled": od.get("filled", 0),
                "status": od.get("status", ""),
                "created_at": od.get("created_at", 0),
            })
        return result

    @app.delete("/api/agent/v1/orders/{order_id}")
    async def agent_cancel_order(request: Request, order_id: str):
        oms = getattr(engine, "oms", None)
        if not oms:
            return JSONResponse({"detail": "OMS not available"}, status_code=500)
        success = await oms.cancel(order_id)
        if success:
            return {"status": "cancelled", "order_id": order_id}
        return JSONResponse({"detail": "Order not found"}, status_code=404)

    @app.get("/api/agent/v1/account")
    async def agent_account(request: Request):
        balances = []
        portfolio = getattr(engine, "portfolio", None)
        if portfolio:
            pos = getattr(portfolio, "get_positions", None)
            if pos:
                positions = await pos() if callable(pos) else []
                for p in (positions or []):
                    pd = p.__dict__ if hasattr(p, "__dict__") else p
                    balances.append({
                        "asset": pd.get("symbol", pd.get("asset", "USDT")),
                        "free": pd.get("free", 0),
                        "locked": pd.get("locked", 0),
                    })
        if not balances:
            balances = [{"asset": "USDT", "free": 100000, "locked": 0}]
        return {"balances": balances}

    logger.info("[Agent] Gateway registered on /api/agent/v1")
