"""Strategy config CRUD, batch operations, logs, templates."""

import json
import time
import logging
import uuid as _uuid
from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

from .shared import (
    get_strategy_configs_store, get_strategy_configs_lock,
    persist_strategy_configs, get_logs_store, get_templates_store,
)

logger = logging.getLogger("xtquant.web")
router = APIRouter()


# ── Strategy Configs CRUD ──
@router.get("/api/strategies/configs")
async def api_get_strategy_configs(request: Request):
    q = request.query_params
    category = q.get("category", "")
    status = q.get("status", "")
    coin = q.get("coin", "")
    stype = q.get("type", "")
    limit = int(q.get("limit", 200))
    offset = int(q.get("offset", 0))

    with get_strategy_configs_lock():
        items = list(get_strategy_configs_store().values())

    if category:
        items = [it for it in items if it.get("category") == category]
    if status:
        items = [it for it in items if it.get("status") == status]
    if coin:
        items = [it for it in items if coin.lower() in (it.get("coin") or "").lower()]
    if stype:
        items = [it for it in items if it.get("strategy_type") == stype]

    items.sort(key=lambda x: x.get("updated_at", 0), reverse=True)
    items = items[offset:offset + limit]

    for it in items:
        if isinstance(it.get("config_json"), str):
            try:
                it["config"] = json.loads(it["config_json"])
            except Exception:
                it["config"] = {}
        else:
            it["config"] = it.get("config_json", {})
    return JSONResponse(items)


@router.get("/api/strategies/configs/{strategy_id}")
async def api_get_strategy_config(strategy_id: str):
    with get_strategy_configs_lock():
        item = get_strategy_configs_store().get(strategy_id)
    if not item:
        return JSONResponse({"detail": "not found"}, status_code=404)
    result = dict(item)
    if isinstance(result.get("config_json"), str):
        try:
            result["config"] = json.loads(result["config_json"])
        except Exception:
            result["config"] = {}
    return JSONResponse(result)


@router.post("/api/strategies/configs")
async def api_create_strategy_config(request: Request):
    try:
        body = await request.json()
    except Exception:
        return JSONResponse({"detail": "invalid json"}, status_code=400)
    sid = str(_uuid.uuid4())[:8]
    now_ts = int(time.time() * 1000)
    config = body.get("config", {})
    config_json_val = body.get("config_json", "")
    if isinstance(config, dict) and config:
        config_json = json.dumps(config, ensure_ascii=False)
    elif isinstance(config_json_val, str) and config_json_val:
        config_json = config_json_val
    else:
        config_json = "{}"
    item = {
        "id": sid,
        "name": body.get("name", sid),
        "category": body.get("category", "spot"),
        "strategy_type": body.get("strategy_type", ""),
        "coin": body.get("coin", ""),
        "config_json": config_json,
        "direction": body.get("direction", "long"),
        "leverage": float(body.get("leverage", 1.0)),
        "status": "stopped",
        "pnl": 0.0,
        "created_at": now_ts,
        "updated_at": now_ts,
    }
    with get_strategy_configs_lock():
        get_strategy_configs_store()[sid] = item
    persist_strategy_configs()
    logger.info(f"Strategy config created: {sid} ({item.get('name', '')})")
    return JSONResponse({"status": "ok", "id": sid})


@router.put("/api/strategies/configs/{strategy_id}")
async def api_update_strategy_config(strategy_id: str, request: Request):
    try:
        body = await request.json()
    except Exception:
        return JSONResponse({"detail": "invalid json"}, status_code=400)
    with get_strategy_configs_lock():
        if strategy_id not in get_strategy_configs_store():
            return JSONResponse({"detail": "not found"}, status_code=404)
    now_ts = int(time.time() * 1000)
    with get_strategy_configs_lock():
        item = get_strategy_configs_store()[strategy_id]
        for f in ["name", "coin", "strategy_type", "direction", "leverage", "category"]:
            if f in body:
                item[f] = body[f]
        if "config" in body:
            config_val = body["config"]
            item["config_json"] = json.dumps(config_val, ensure_ascii=False) if isinstance(config_val, dict) else config_val
        elif "config_json" in body:
            item["config_json"] = body["config_json"]
        item["updated_at"] = now_ts
    persist_strategy_configs()
    return JSONResponse({"status": "ok"})


@router.delete("/api/strategies/configs/{strategy_id}")
async def api_delete_strategy_config(strategy_id: str):
    with get_strategy_configs_lock():
        if strategy_id not in get_strategy_configs_store():
            return JSONResponse({"detail": "not found"}, status_code=404)
        del get_strategy_configs_store()[strategy_id]
    persist_strategy_configs()
    return JSONResponse({"status": "ok"})


@router.post("/api/strategies/configs/batch-start")
async def api_batch_start_configs(request: Request):
    body = await request.json()
    ids = body.get("ids", [])
    with get_strategy_configs_lock():
        for sid in ids:
            if sid in get_strategy_configs_store():
                get_strategy_configs_store()[sid]["status"] = "running"
                get_strategy_configs_store()[sid]["updated_at"] = int(time.time() * 1000)
    persist_strategy_configs()
    return JSONResponse({"status": "ok"})


@router.post("/api/strategies/configs/batch-stop")
async def api_batch_stop_configs(request: Request):
    body = await request.json()
    ids = body.get("ids", [])
    with get_strategy_configs_lock():
        for sid in ids:
            if sid in get_strategy_configs_store():
                get_strategy_configs_store()[sid]["status"] = "stopped"
                get_strategy_configs_store()[sid]["updated_at"] = int(time.time() * 1000)
    persist_strategy_configs()
    return JSONResponse({"status": "ok"})


@router.post("/api/strategies/configs/{strategy_id}/start")
async def api_start_strategy_config(strategy_id: str):
    with get_strategy_configs_lock():
        if strategy_id not in get_strategy_configs_store():
            return JSONResponse({"detail": "not found"}, status_code=404)
        get_strategy_configs_store()[strategy_id]["status"] = "running"
        get_strategy_configs_store()[strategy_id]["updated_at"] = int(time.time() * 1000)
    persist_strategy_configs()
    logger.info(f"Strategy {strategy_id} started")
    return JSONResponse({"status": "ok"})


@router.post("/api/strategies/configs/{strategy_id}/stop")
async def api_stop_strategy_config(strategy_id: str):
    with get_strategy_configs_lock():
        if strategy_id not in get_strategy_configs_store():
            return JSONResponse({"detail": "not found"}, status_code=404)
        get_strategy_configs_store()[strategy_id]["status"] = "stopped"
        get_strategy_configs_store()[strategy_id]["updated_at"] = int(time.time() * 1000)
    persist_strategy_configs()
    logger.info(f"Strategy {strategy_id} stopped")
    return JSONResponse({"status": "ok"})


# ── Strategy Logs ──
@router.get("/api/strategies/logs")
async def api_get_strategy_logs(request: Request):
    q = request.query_params
    sid = q.get("strategy_id", "")
    limit = int(q.get("limit", 200))
    logs = [l for l in get_logs_store() if not sid or l.get("strategy_id") == sid]
    return JSONResponse(logs[-limit:])


@router.delete("/api/strategies/logs")
async def api_clear_strategy_logs(request: Request):
    sid = request.query_params.get("strategy_id", "")
    if sid:
        get_logs_store()[:] = [l for l in get_logs_store() if l.get("strategy_id") != sid]
    else:
        get_logs_store().clear()
    return JSONResponse({"status": "ok"})


# ── Strategy Templates ──
@router.get("/api/strategies/templates")
async def api_get_templates(request: Request):
    category = request.query_params.get("category", "spot")
    limit = int(request.query_params.get("limit", 200))
    items = [t for t in get_templates_store() if t.get("category", "spot") == category]
    return JSONResponse(items[-limit:])


@router.post("/api/strategies/templates")
async def api_create_template(request: Request):
    try:
        data = await request.json()
        tpl = {
            "id": f"tpl-{int(time.time()*1000)}",
            "name": data.get("strategy_name", data.get("name", "Untitled")),
            "strategy_code": data.get("strategy_code", ""),
            "description": data.get("description", ""),
            "category": data.get("category", "spot"),
            "symbol": data.get("symbol", "BTCUSDT"),
            "risk_level": data.get("risk", "medium"),
            "created_at": time.time(),
        }
        get_templates_store().append(tpl)
        return JSONResponse({"status": "ok", "id": tpl["id"]})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)


@router.delete("/api/strategies/templates/{template_id}")
async def api_delete_template(template_id: str):
    for i, t in enumerate(get_templates_store()):
        if t["id"] == template_id:
            get_templates_store().pop(i)
            return JSONResponse({"status": "ok"})
    return JSONResponse({"detail": "Template not found"}, status_code=404)
