"""Agent tokens, CC Switch, AI config, Chat routes."""

import json
import time
import asyncio
import logging
from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

from .shared import load_config, save_config, get_agent_tokens_store

logger = logging.getLogger("xtquant.web")
router = APIRouter()


# ── Agent Tokens ──
@router.get("/api/agent/tokens")
async def api_get_agent_tokens():
    return JSONResponse(get_agent_tokens_store())


@router.post("/api/agent/tokens")
async def api_create_agent_token(request: Request):
    try:
        data = await request.json()
        token = {
            "id": f"tok-{int(time.time()*1000)}",
            "name": data.get("name", "Untitled"),
            "token": data.get("token", ""),
            "scopes": data.get("scopes", []),
            "created_at": int(time.time()),
            "last_used_at": None,
        }
        get_agent_tokens_store().append(token)
        return JSONResponse({"status": "ok", "id": token["id"]})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)


@router.delete("/api/agent/tokens/{token_id}")
async def api_delete_agent_token(token_id: str):
    for i, t in enumerate(get_agent_tokens_store()):
        if t["id"] == token_id:
            get_agent_tokens_store().pop(i)
            return JSONResponse({"status": "ok"})
    return JSONResponse({"detail": "Token not found"}, status_code=404)


# ── CC Switch ──
from xtquant.agent.cc_switch import configure, start as cc_start, stop as cc_stop, get_status as cc_status


@router.get("/api/agent/cc-switch")
async def api_cc_switch_status():
    return JSONResponse(cc_status())


@router.post("/api/agent/cc-switch/configure")
async def api_cc_switch_configure(request: Request):
    try:
        data = await request.json()
        result = configure(
            port=data.get("port"),
            target_url=data.get("target_url"),
            target_model=data.get("target_model"),
            api_key=data.get("api_key"),
        )
        return JSONResponse(result)
    except Exception as e:
        return JSONResponse({"status": "error", "error": str(e)}, status_code=500)


@router.post("/api/agent/cc-switch/start")
async def api_cc_switch_start():
    try:
        result = cc_start()
        return JSONResponse(result)
    except Exception as e:
        return JSONResponse({"status": "error", "error": str(e)}, status_code=500)


@router.post("/api/agent/cc-switch/stop")
async def api_cc_switch_stop():
    try:
        result = cc_stop()
        return JSONResponse(result)
    except Exception as e:
        return JSONResponse({"status": "error", "error": str(e)}, status_code=500)


# ── Agent AI Config ──
@router.get("/api/agent/ai-config")
async def api_get_agent_ai():
    cfg = load_config()
    agent_cfg = cfg.get("agent", {}).get("ai", {})
    return JSONResponse({
        "provider": agent_cfg.get("provider", ""),
        "api_key": agent_cfg.get("api_key", ""),
        "base_url": agent_cfg.get("base_url", ""),
        "model": agent_cfg.get("model", ""),
        "proxy_enabled": agent_cfg.get("proxy_enabled", False),
        "http_proxy": agent_cfg.get("http_proxy", ""),
        "https_proxy": agent_cfg.get("https_proxy", ""),
    })


@router.put("/api/agent/ai-config")
@router.post("/api/agent/ai-config")
async def api_save_agent_ai(request: Request):
    try:
        data = await request.json()
        cfg = load_config()
        agent_cfg = cfg.setdefault("agent", {}).setdefault("ai", {})
        if "provider" in data: agent_cfg["provider"] = data["provider"]
        if "api_key" in data: agent_cfg["api_key"] = data["api_key"]
        if "base_url" in data: agent_cfg["base_url"] = data["base_url"]
        if "model" in data: agent_cfg["model"] = data["model"]
        if "proxy_enabled" in data: agent_cfg["proxy_enabled"] = data["proxy_enabled"]
        if "http_proxy" in data: agent_cfg["http_proxy"] = data["http_proxy"]
        if "https_proxy" in data: agent_cfg["https_proxy"] = data["https_proxy"]
        save_config(cfg)
        return JSONResponse({"status": "ok"})
    except Exception as e:
        return JSONResponse({"error": str(e)}, status_code=500)


# ── Agent AI Test ──
@router.post("/api/agent/ai-test")
async def api_test_agent_ai(request: Request):
    try:
        data = await request.json()
        import urllib.request
        url = (data.get("base_url", "https://api.openai.com/v1")).rstrip("/") + "/models"
        req = urllib.request.Request(url)
        req.add_header("Authorization", f"Bearer {data.get('api_key', '')}")
        resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 10)
        return JSONResponse({"status": "ok", "message": f"Connected (HTTP {resp.status})"})
    except Exception as e:
        return JSONResponse({"status": "error", "message": str(e)})


# ── Agent Chat ──
@router.post("/api/agent/chat")
async def api_agent_chat(request: Request):
    try:
        data = await request.json()
        message = data.get("message", "").strip()
        if not message:
            return JSONResponse({"reply": "请发送一条消息。"})

        cfg = load_config()
        ai_cfg = cfg.get("ai", {}).get("deepseek", {})
        api_key = ai_cfg.get("api_key", "")
        base_url = ai_cfg.get("base_url", "https://api.deepseek.com/v1").rstrip("/")
        model = ai_cfg.get("model", "deepseek-chat")

        if not api_key:
            return JSONResponse({"reply": "未配置AI API Key。请在设置中配置DeepSeek或其他AI提供商的API Key。"})

        import urllib.request
        system_prompt = """你是小天量化(XiaoTianQuant)的AI助手，一个专业的加密货币量化交易平台。
你可以帮助用户解答：
- 交易策略的设计和优化（网格、马丁、突破、做市、套利等）
- 技术指标的使用（RSI、MACD、EMA、布林带、VWAP等）
- 风控参数设置（止损、止盈、仓位管理）
- 平台功能使用指导

请用简洁、专业的中文回答，回复控制在200字以内。"""

        payload = json.dumps({
            "model": model,
            "messages": [
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": message},
            ],
            "temperature": 0.7,
            "max_tokens": 800,
        }).encode("utf-8")

        url = f"{base_url}/chat/completions"
        req = urllib.request.Request(url, data=payload)
        req.add_header("Authorization", f"Bearer {api_key}")
        req.add_header("Content-Type", "application/json")
        resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 30)
        raw = resp.read()
        try:
            text = raw.decode("utf-8")
        except Exception:
            try:
                import gzip
                text = gzip.decompress(raw).decode("utf-8")
            except Exception:
                text = raw.decode("latin-1")
        result = json.loads(text)
        reply = result["choices"][0]["message"]["content"]
        return JSONResponse({"reply": reply})
    except Exception as e:
        logger.error(f"[Chat] error: {e}")
        return JSONResponse({"reply": "AI服务暂时不可用，请稍后重试。"})
