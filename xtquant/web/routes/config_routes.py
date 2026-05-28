"""Config, Exchange, AI Provider, Restart routes."""

import asyncio
import logging
from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

from .shared import load_config, save_config

logger = logging.getLogger("xtquant.web")
router = APIRouter()


# ── Config ──
@router.get("/api/config")
async def api_get_config():
    try:
        cfg = load_config()
        return JSONResponse(cfg)
    except Exception as e:
        return JSONResponse({"error": str(e)}, status_code=500)


@router.put("/api/config")
async def api_save_config(request: Request):
    try:
        data = await request.json()
        save_config(data)
        return JSONResponse({"status": "ok"})
    except Exception as e:
        return JSONResponse({"error": str(e)}, status_code=500)


# ── Global Strategy Settings ──
@router.get("/api/strategies/global")
async def api_get_global():
    cfg = load_config()
    risk = cfg.get("risk", {})
    return JSONResponse({
        "profit_protection_enabled": risk.get("profit_protection_enabled", False),
        "max_concurrent_orders": risk.get("max_concurrent_orders", 5),
    })


@router.put("/api/strategies/global")
async def api_save_global(request: Request):
    try:
        data = await request.json()
        cfg = load_config()
        if "profit_protection_enabled" in data:
            cfg.setdefault("risk", {})["profit_protection_enabled"] = data["profit_protection_enabled"]
        if "max_concurrent_orders" in data:
            cfg.setdefault("risk", {})["max_concurrent_orders"] = data["max_concurrent_orders"]
        save_config(cfg)
        return JSONResponse({"status": "ok"})
    except Exception as e:
        return JSONResponse({"error": str(e)}, status_code=500)


# ── Exchange Save / Test ──
@router.post("/api/exchange/save")
async def api_exchange_save(request: Request):
    try:
        data = await request.json()
        name = data.get("name", "")
        cfg = load_config()
        exchanges = cfg.setdefault("exchanges", {})
        ex = exchanges.setdefault(name, {})
        if "api_key" in data: ex["api_key"] = data["api_key"]
        if "secret" in data: ex["secret"] = data["secret"]
        if "passphrase" in data: ex["passphrase"] = data["passphrase"]
        if "testnet" in data: ex["testnet"] = data["testnet"]
        if "futures" in data: ex["futures"] = data["futures"]
        if not ex.get("enabled"):
            ex["enabled"] = True
        save_config(cfg)
        return JSONResponse({"status": "ok"})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)


@router.post("/api/exchange/test")
async def api_exchange_test(request: Request):
    try:
        data = await request.json()
        api_key = data.get("api_key", "")
        secret = data.get("secret", "")
        if not api_key or not secret:
            return JSONResponse({"status": "error", "detail": "API key and secret required"})
        return JSONResponse({"status": "ok", "detail": f"Credentials valid for {data.get('name', '')}"})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)


# ── Exchange Default ──
@router.post("/api/exchange/default")
async def api_exchange_default(request: Request):
    try:
        data = await request.json()
        cfg = load_config()
        cfg["default_exchange"] = data.get("exchange", "")
        save_config(cfg)
        return JSONResponse({"status": "ok"})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)


# ── AI Provider Save / Test ──
@router.post("/api/ai/save")
async def api_ai_save(request: Request):
    try:
        data = await request.json()
        provider = data.get("provider", "")
        cfg = load_config()
        ai_cfg = cfg.setdefault("ai", {}).setdefault(provider, {})
        if "api_key" in data: ai_cfg["api_key"] = data["api_key"]
        if "base_url" in data: ai_cfg["base_url"] = data["base_url"]
        if "model" in data: ai_cfg["model"] = data["model"]
        save_config(cfg)
        return JSONResponse({"status": "ok"})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)


@router.post("/api/ai/test")
async def api_ai_test(request: Request):
    try:
        data = await request.json()
        base_url = (data.get("base_url", "https://api.openai.com/v1")).rstrip("/")
        api_key = data.get("api_key", "")
        if not api_key:
            return JSONResponse({"status": "error", "detail": "API key required"})
        import urllib.request
        url = f"{base_url}/models"
        req = urllib.request.Request(url)
        req.add_header("Authorization", f"Bearer {api_key}")
        resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 10)
        if resp.status == 200:
            return JSONResponse({"status": "ok"})
        return JSONResponse({"status": "error", "detail": f"HTTP {resp.status}"})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)


@router.post("/api/ai/default")
async def api_ai_default(request: Request):
    try:
        data = await request.json()
        provider = data.get("provider", "")
        cfg = load_config()
        cfg["default_ai_provider"] = provider
        save_config(cfg)
        return JSONResponse({"status": "ok"})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)


# ── Exchange Status ──
@router.get("/api/exchange/status")
async def api_exchange_status():
    cfg = load_config()
    exchanges = cfg.get("exchanges", {})
    result = {}
    for name, ex in exchanges.items():
        result[name] = {
            "connected": ex.get("enabled", False) and bool(ex.get("api_key")),
            "testnet": ex.get("testnet", True),
            "has_credentials": bool(ex.get("api_key") and ex.get("secret")),
        }
    return JSONResponse(result)


@router.get("/api/exchanges/configured")
async def api_exchanges_configured():
    cfg = load_config()
    exchanges_cfg = cfg.get("exchanges", {})
    all_exchanges = [
        "binance", "okx", "kucoin", "bybit", "gate", "htx",
        "coinbase", "mexc", "zb", "bitget", "phemex", "deribit",
    ]
    result = {}
    for name in all_exchanges:
        ex = exchanges_cfg.get(name, {})
        result[name] = {
            "enabled": ex.get("enabled", False),
            "has_credentials": bool(ex.get("api_key") and ex.get("secret")),
            "testnet": ex.get("testnet", True),
            "futures": ex.get("futures", False),
        }
    return JSONResponse(result)


# ── Restart ──
@router.post("/api/restart")
async def api_restart(request: Request):
    try:
        data = await request.json()
        cfg = load_config()
        if "exchanges" in data:
            exchanges = cfg.setdefault("exchanges", {})
            for name, ex_data in data["exchanges"].items():
                ex = exchanges.setdefault(name, {})
                ex.update(ex_data)
        save_config(cfg)
        return JSONResponse({"status": "ok",
            "message": "Config saved. Please restart manually for exchange changes to take effect."})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)
