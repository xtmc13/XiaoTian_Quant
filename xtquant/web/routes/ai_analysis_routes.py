"""AI Multi-Model Analysis Routes — parallel analysis, voting, auto-trade, chat."""

import json
import os
import time
import asyncio
import logging
import urllib.request
import urllib.error
import threading
from typing import Optional
from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

from .shared import load_config

logger = logging.getLogger("xtquant.web")
router = APIRouter()

# ── In-memory analysis task store ──
_analysis_tasks: dict[str, dict] = {}
_analysis_tasks_lock = threading.Lock()
_task_cleanup_timer: Optional[asyncio.Task] = None

# ── Auto-trade config (in-memory, synced to config.yaml) ──
_auto_trade_config: dict = {
    "enabled": False,
    "threshold": 70.0,
    "divergence_protection": 30.0,
    "symbol": "BTCUSDT",
    "qty": 0.001,
    "max_position_pct": 50.0,
}

# ── Model Registry ──
MODEL_REGISTRY = {
    "deepseek": {
        "id": "deepseek", "name": "DeepSeek", "color": "#10B981",
        "model": "deepseek-chat", "base_url": "https://api.deepseek.com/v1",
        "capabilities": ["reasoning", "coding", "analysis", "chinese"],
    },
    "openai": {
        "id": "openai", "name": "GPT-4o", "color": "#4285F4",
        "model": "gpt-4o", "base_url": "https://api.openai.com/v1",
        "capabilities": ["reasoning", "analysis"],
    },
    "claude": {
        "id": "claude", "name": "Claude 3.5", "color": "#A855F7",
        "model": "claude-3-5-sonnet-20241022", "base_url": "https://api.anthropic.com/v1",
        "capabilities": ["reasoning", "coding", "analysis", "safety"],
    },
    "qwen": {
        "id": "qwen", "name": "通义千问 Max", "color": "#6366F1",
        "model": "qwen-max", "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
        "capabilities": ["reasoning", "coding", "analysis", "chinese"],
    },
    "gemini": {
        "id": "gemini", "name": "Gemini 2.0", "color": "#FBBC04",
        "model": "gemini-2.0-flash", "base_url": "https://generativelanguage.googleapis.com/v1beta",
        "capabilities": ["reasoning", "analysis"],
    },
    "qwen_turbo": {
        "id": "qwen_turbo", "name": "千问 Turbo", "color": "#8B5CF6",
        "model": "qwen-turbo", "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
        "capabilities": ["reasoning", "analysis", "chinese"],
    },
    "moonshot": {
        "id": "moonshot", "name": "Moonshot Kimi", "color": "#F59E0B",
        "model": "moonshot-v1-8k", "base_url": "https://api.moonshot.cn/v1",
        "capabilities": ["reasoning", "coding", "analysis", "chinese"],
    },
    "zhipu": {
        "id": "zhipu", "name": "智谱 GLM-4", "color": "#EC4899",
        "model": "glm-4", "base_url": "https://open.bigmodel.cn/api/paas/v4",
        "capabilities": ["reasoning", "analysis", "chinese"],
    },
    "baidu": {
        "id": "baidu", "name": "文心一言 ERNIE", "color": "#3B82F6",
        "model": "ernie-4.0-8k", "base_url": "https://aip.baidubce.com/rpc/2.0/ai_custom/v1/wenxinworkshop/chat",
        "capabilities": ["reasoning", "analysis", "chinese"],
    },
    "minimax": {
        "id": "minimax", "name": "MiniMax", "color": "#F43F5E",
        "model": "abab6.5s-chat", "base_url": "https://api.minimax.chat/v1",
        "capabilities": ["reasoning", "analysis", "chinese"],
    },
    "stepfun": {
        "id": "stepfun", "name": "阶跃星辰", "color": "#14B8A6",
        "model": "step-1-8k", "base_url": "https://api.stepfun.com/v1",
        "capabilities": ["reasoning", "analysis", "chinese"],
    },
    "doubao": {
        "id": "doubao", "name": "豆包", "color": "#F97316",
        "model": "doubao-pro-32k", "base_url": "https://ark.cn-beijing.volces.com/api/v3",
        "capabilities": ["reasoning", "analysis", "chinese"],
    },
}

DEFAULT_ENABLED_MODELS = ["deepseek", "openai", "qwen"]

ANALYSIS_SYSTEM_PROMPT = """You are a professional cryptocurrency market analyst. Analyze the given market data and provide your assessment.

Respond in JSON format only, no other text:
{
  "direction": "bullish" | "bearish" | "neutral",
  "support": <number> (key support price level),
  "resistance": <number> (key resistance price level),
  "target": <number> (short-term target price),
  "confidence": <number 0-100> (how confident are you in this assessment),
  "reasoning": "<string>" (2-3 sentences explaining your analysis),
  "risk_factors": "<string>" (key risks to watch)
}"""

CHAT_SYSTEM_PROMPT = """You are a cryptocurrency trading analyst in a multi-model discussion.
Answer the user's question concisely. If you have a directional view, mention it clearly.
Keep responses under 200 words. Use Chinese if the question is in Chinese."""


def _load_models():
    """Merge MODEL_REGISTRY with config.yaml providers."""
    models = []
    cfg = load_config()
    ai_section = cfg.get("ai", {})
    ai_analysis = cfg.get("ai_analysis", {}).get("models", {})

    for mid, meta in MODEL_REGISTRY.items():
        provider_cfg = ai_section.get(mid, {})
        analysis_cfg = ai_analysis.get(mid, {})

        api_key = provider_cfg.get("api_key", "") or os.environ.get(f"{mid.upper()}_API_KEY", "")
        base_url = provider_cfg.get("base_url", meta["base_url"])
        model_name = provider_cfg.get("model", meta["model"])

        models.append({
            "id": mid,
            "name": meta["name"],
            "color": meta["color"],
            "model": model_name,
            "base_url": base_url,
            "capabilities": meta["capabilities"],
            "enabled": analysis_cfg.get("enabled", mid in DEFAULT_ENABLED_MODELS),
            "weight": analysis_cfg.get("weight", 1.0),
            "has_api_key": bool(api_key),
            "status": "configured" if api_key else "no_key",
        })

    models.sort(key=lambda m: (not m["enabled"], -m["weight"], m["id"]))
    return models


def _get_model_config(model_id: str) -> Optional[dict]:
    cfg = load_config()
    ai_section = cfg.get("ai", {})
    meta = MODEL_REGISTRY.get(model_id)
    if not meta:
        return None
    provider_cfg = ai_section.get(model_id, {})
    api_key = provider_cfg.get("api_key", "") or os.environ.get(f"{model_id.upper()}_API_KEY", "")
    if not api_key:
        return None
    return {
        "api_key": api_key,
        "base_url": provider_cfg.get("base_url", meta["base_url"]),
        "model": provider_cfg.get("model", meta["model"]),
    }


async def _call_model(provider_id: str, system_prompt: str, user_message: str, model_cfg: dict) -> dict:
    """Call one model's OpenAI-compatible chat completions API."""
    body = {
        "model": model_cfg["model"],
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_message},
        ],
        "temperature": 0.3,
        "max_tokens": 600,
    }

    endpoint = model_cfg["base_url"].rstrip("/")
    if not endpoint.endswith("/chat/completions"):
        endpoint += "/chat/completions"

    req = urllib.request.Request(
        endpoint,
        data=json.dumps(body).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {model_cfg['api_key']}",
            "Content-Type": "application/json",
        },
    )

    loop = asyncio.get_event_loop()
    try:
        resp = await loop.run_in_executor(None, lambda: urllib.request.urlopen(req, timeout=45))
        data = json.loads(resp.read().decode("utf-8"))
        content = data.get("choices", [{}])[0].get("message", {}).get("content", "")
        return {"success": True, "content": content, "model": provider_id}
    except Exception as e:
        logger.error(f"Model {provider_id} call failed: {e}")
        return {"success": False, "error": str(e), "model": provider_id}


def _parse_analysis(content: str) -> dict:
    """Parse JSON from model response, with fallback regex extraction."""
    try:
        # Try direct JSON parse
        result = json.loads(content)
        return result
    except (json.JSONDecodeError, TypeError):
        pass

    # Try to extract JSON block
    import re
    m = re.search(r'\{[^{}]*"direction"[^{}]*\}', content, re.DOTALL)
    if m:
        try:
            return json.loads(m.group())
        except (json.JSONDecodeError, TypeError):
            pass

    # Fallback: parse direction from text
    direction = "neutral"
    if re.search(r'\bbullish\b', content, re.IGNORECASE):
        direction = "bullish"
    elif re.search(r'\bbearish\b', content, re.IGNORECASE):
        direction = "bearish"

    return {
        "direction": direction,
        "support": 0,
        "resistance": 0,
        "target": 0,
        "confidence": 50,
        "reasoning": content[:300],
        "risk_factors": "",
    }


def _compute_vote_summary(results: dict[str, dict]) -> dict:
    """Compute voting consensus, composite confidence, and divergence."""
    bullish = sum(1 for r in results.values() if r.get("direction") == "bullish")
    bearish = sum(1 for r in results.values() if r.get("direction") == "bearish")
    neutral = sum(1 for r in results.values() if r.get("direction") == "neutral")
    total = bullish + bearish + neutral or 1

    bullish_pct = round(bullish / total * 100, 1)
    bearish_pct = round(bearish / total * 100, 1)
    neutral_pct = round(neutral / total * 100, 1)

    confidences = [r.get("confidence", 50) for r in results.values() if isinstance(r.get("confidence"), (int, float))]
    composite_confidence = round(sum(confidences) / len(confidences), 1) if confidences else 0

    divergence = round(max(bullish_pct, bearish_pct) - min(bullish_pct, bearish_pct), 1)

    if bullish > bearish:
        consensus = "bullish"
    elif bearish > bullish:
        consensus = "bearish"
    else:
        consensus = "neutral"

    if composite_confidence >= 80:
        strength = "strong"
    elif composite_confidence >= 60:
        strength = "moderate"
    else:
        strength = "weak"

    return {
        "bullish": bullish, "bearish": bearish, "neutral": neutral, "total": total,
        "bullish_pct": bullish_pct, "bearish_pct": bearish_pct, "neutral_pct": neutral_pct,
        "composite_confidence": composite_confidence,
        "divergence": divergence,
        "consensus": consensus,
        "consensus_strength": strength,
    }


async def _run_multi_model_analysis(task_id: str, symbol: str, interval: str,
                                     prompt: str, enabled_models: list[str]):
    """Run parallel analysis across all enabled models."""
    with _analysis_tasks_lock:
        _analysis_tasks[task_id]["status"] = "running"

    results = {}
    signals = []

    async def analyze_one(model_id: str):
        cfg = _get_model_config(model_id)
        if not cfg:
            return model_id, {"success": False, "error": "No API key configured"}

        # Fetch klines for context
        ctx_prompt = f"Symbol: {symbol}\nInterval: {interval}\nPrompt: {prompt}\n\nProvide your analysis based on this context."

        resp = await _call_model(model_id, ANALYSIS_SYSTEM_PROMPT, ctx_prompt, cfg)

        if resp.get("success"):
            parsed = _parse_analysis(resp.get("content", ""))
            parsed["model_name"] = MODEL_REGISTRY.get(model_id, {}).get("name", model_id)
            parsed["model_color"] = MODEL_REGISTRY.get(model_id, {}).get("color", "#6B7280")
            signals.append({
                "model": model_id,
                "name": parsed.get("model_name"),
                "color": parsed.get("model_color"),
                "direction": parsed.get("direction"),
                "support": parsed.get("support", 0),
                "resistance": parsed.get("resistance", 0),
                "target": parsed.get("target", 0),
                "confidence": parsed.get("confidence", 50),
            })
            return model_id, parsed
        return model_id, {"error": resp.get("error", "Unknown error"), "direction": "neutral", "confidence": 0, "support": 0, "resistance": 0, "target": 0, "reasoning": "", "risk_factors": ""}

    # Run all models in parallel with a small stagger
    coros = [analyze_one(mid) for mid in enabled_models]
    gathered = await asyncio.gather(*coros)

    for mid, result in gathered:
        results[mid] = result
        if result.get("model_name") is None:
            result["model_name"] = MODEL_REGISTRY.get(mid, {}).get("name", mid)

    vote = _compute_vote_summary(results)

    with _analysis_tasks_lock:
        _analysis_tasks[task_id].update({
            "status": "completed",
            "results": results,
            "vote_summary": vote,
            "signals": signals,
        })


def _cleanup_old_tasks():
    """Remove analysis tasks older than 1 hour."""
    now = time.time()
    with _analysis_tasks_lock:
        expired = [tid for tid, t in _analysis_tasks.items()
                   if now - t.get("created_at", now) > 3600]
        for tid in expired:
            del _analysis_tasks[tid]


# ═══════════════════════════════════════════════ Endpoints ═══════════════════════════════════════════

@router.get("/api/models/list")
async def api_models_list(request: Request):
    return JSONResponse({"status": "ok", "models": _load_models()})


@router.post("/api/analysis/start")
async def api_analysis_start(request: Request):
    try:
        data = await request.json()
    except Exception:
        return JSONResponse({"status": "error", "detail": "Invalid JSON"}, status_code=400)

    symbol = data.get("symbol", "BTCUSDT")
    interval = data.get("interval", "1h")
    prompt = data.get("prompt", "分析当前行情")
    enabled_models = data.get("enabled_models", DEFAULT_ENABLED_MODELS)

    if not enabled_models:
        return JSONResponse({"status": "error", "detail": "No models enabled"}, status_code=400)

    task_id = f"ana-{int(time.time()*1000)}"

    with _analysis_tasks_lock:
        _analysis_tasks[task_id] = {
            "task_id": task_id,
            "status": "pending",
            "symbol": symbol,
            "interval": interval,
            "prompt": prompt,
            "enabled_models": enabled_models,
            "created_at": time.time(),
            "results": {},
            "vote_summary": None,
            "signals": [],
        }

    _cleanup_old_tasks()

    asyncio.create_task(_run_multi_model_analysis(
        task_id, symbol, interval, prompt, enabled_models
    ))

    return JSONResponse({
        "status": "ok",
        "task_id": task_id,
        "estimated_time": len(enabled_models) * 3,
    })


@router.get("/api/analysis/result")
async def api_analysis_result(task_id: str = ""):
    if not task_id:
        return JSONResponse({"status": "error", "detail": "task_id required"}, status_code=400)

    with _analysis_tasks_lock:
        task = _analysis_tasks.get(task_id)

    if not task:
        return JSONResponse({"status": "error", "detail": "Task not found"}, status_code=404)

    if task["status"] in ("pending", "running"):
        completed = sum(1 for r in task.get("results", {}).values() if r)
        return JSONResponse({
            "status": "running",
            "task_id": task_id,
            "completed": completed,
            "total": len(task.get("enabled_models", [])),
        })

    return JSONResponse({
        "status": "completed",
        "task_id": task_id,
        "symbol": task["symbol"],
        "interval": task["interval"],
        "results": task.get("results", {}),
        "vote_summary": task.get("vote_summary"),
        "signals": task.get("signals", []),
    })


@router.get("/api/auto-trade/config")
async def api_auto_trade_config_get():
    return JSONResponse({"status": "ok", "config": _auto_trade_config})


@router.put("/api/auto-trade/config")
async def api_auto_trade_config_put(request: Request):
    global _auto_trade_config
    try:
        data = await request.json()
    except Exception:
        return JSONResponse({"status": "error", "detail": "Invalid JSON"}, status_code=400)

    for key in ("enabled", "threshold", "divergence_protection", "symbol", "qty", "max_position_pct"):
        if key in data:
            _auto_trade_config[key] = data[key]

    return JSONResponse({"status": "ok", "config": _auto_trade_config})


@router.post("/api/chat/send")
async def api_chat_send(request: Request):
    try:
        data = await request.json()
    except Exception:
        return JSONResponse({"status": "error", "detail": "Invalid JSON"}, status_code=400)

    message = data.get("message", "").strip()
    enabled_models = data.get("enabled_models", DEFAULT_ENABLED_MODELS)

    if not message:
        return JSONResponse({"status": "error", "detail": "Message required"}, status_code=400)

    responses = {}

    async def chat_one(model_id: str):
        cfg = _get_model_config(model_id)
        if not cfg:
            return model_id, {"reply": "未配置API密钥", "sentiment": "neutral"}
        resp = await _call_model(model_id, CHAT_SYSTEM_PROMPT, message, cfg)
        if resp.get("success"):
            content = resp.get("content", "")
            # Quick sentiment detection
            import re
            sentiment = "neutral"
            if re.search(r'(看涨|bullish|做多|买入|上涨|突破)', content, re.IGNORECASE):
                sentiment = "bullish"
            elif re.search(r'(看跌|bearish|做空|卖出|下跌|回调)', content, re.IGNORECASE):
                sentiment = "bearish"
            return model_id, {"reply": content, "sentiment": sentiment}
        return model_id, {"reply": f"错误: {resp.get('error', 'Unknown')}", "sentiment": "neutral"}

    coros = [chat_one(mid) for mid in enabled_models]
    gathered = await asyncio.gather(*coros)

    for mid, resp in gathered:
        resp["model_name"] = MODEL_REGISTRY.get(mid, {}).get("name", mid)
        resp["model_color"] = MODEL_REGISTRY.get(mid, {}).get("color", "#6B7280")
        responses[mid] = resp

    return JSONResponse({"status": "ok", "responses": responses})
