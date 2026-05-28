"""MCP stdio server — JSON-RPC 2.0 over stdio for AI coding tools (Cursor / Claude Code / Codex).

Usage:
    python -m xtquant.agent.mcp_server

Environment:
    QUANTDINGER_BASE_URL       Gateway base URL (default: http://localhost:8080)
    QUANTDINGER_AGENT_TOKEN    Agent bearer token (required)

Config:
    Reads agent.ai from config.yaml for AI model + proxy configuration.
"""

import os
import sys
import json
import asyncio
import logging
from pathlib import Path

logger = logging.getLogger("xtquant.agent.mcp")

BASE_URL = (os.environ.get("XIAOTIAN_AGENT_URL") or
            os.environ.get("QUANTDINGER_BASE_URL", "http://localhost:8080")).rstrip("/")
AGENT_TOKEN = (os.environ.get("XIAOTIAN_AGENT_TOKEN") or
               os.environ.get("QUANTDINGER_AGENT_TOKEN", ""))

SERVER_INFO = {"name": "xiaotian-quant", "version": "2.0"}

# ── AI Config (loaded from config.yaml) ──
AI_CONFIG: dict = {}
PROXY_URL: str = ""


def _load_ai_config():
    """Load agent.ai configuration from config.yaml if available.
    Falls back to default AI provider if agent.ai has no valid key."""
    global AI_CONFIG, PROXY_URL
    for candidate in ("config.yaml", "config.yml", "../config.yaml"):
        p = Path(candidate)
        if p.exists():
            try:
                import yaml
                with open(p, encoding="utf-8") as f:
                    raw = yaml.safe_load(f) or {}
                ai = raw.get("agent", {}).get("ai", {})
                provider = ai.get("provider", "")
                api_key = ai.get("api_key", "")
                base_url = ai.get("base_url", "")
                model = ai.get("model", "")
                # Fallback: inherit from default AI provider if agent AI has no valid key
                if not api_key or api_key.startswith("sk-proxy-test") or api_key.startswith("sk-test"):
                    def_prov = raw.get("default_ai_provider", "")
                    if def_prov:
                        pcfg = (raw.get("ai", {}) or {}).get(def_prov, {})
                        if pcfg.get("api_key"):
                            provider = def_prov
                            api_key = pcfg.get("api_key", "")
                            base_url = pcfg.get("base_url", "")
                            model = pcfg.get("model", "")
                AI_CONFIG = {
                    "provider": provider,
                    "api_key": api_key,
                    "base_url": base_url,
                    "model": model,
                    "proxy_enabled": ai.get("proxy_enabled", False),
                    "http_proxy": ai.get("http_proxy", ""),
                    "https_proxy": ai.get("https_proxy", ""),
                }
                if AI_CONFIG["proxy_enabled"]:
                    PROXY_URL = AI_CONFIG["https_proxy"] or AI_CONFIG["http_proxy"]
                logger.info("[MCP] AI config loaded: provider=%s model=%s proxy=%s",
                          AI_CONFIG["provider"], AI_CONFIG["model"], PROXY_URL or "none")
                return
            except Exception as e:
                logger.warning("[MCP] Failed to load config.yaml: %s", e)
                return


TOOLS = [
    {
        "name": "get_market_data",
        "description": "Get real-time market data for a trading symbol: price, bid/ask, volume.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "symbol": {"type": "string", "description": "Trading symbol, e.g. BTCUSDT, ETHUSDT"}
            },
            "required": ["symbol"]
        }
    },
    {
        "name": "run_backtest",
        "description": "Run a backtest with the given strategy configuration. Returns performance metrics and equity curve.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "config": {"type": "object", "description": "Backtest configuration: strategy, symbol, capital, fee, slippage, bars, price, params"}
            },
            "required": ["config"]
        }
    },
    {
        "name": "list_strategies",
        "description": "List all available strategy templates.",
        "inputSchema": {
            "type": "object",
            "properties": {}
        }
    },
    {
        "name": "place_paper_order",
        "description": "Place a paper (simulated) order. Does NOT use real funds unless live trading is explicitly enabled on the server.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "symbol": {"type": "string", "description": "Trading symbol, e.g. BTCUSDT"},
                "side": {"type": "string", "enum": ["BUY", "SELL"], "description": "Order side"},
                "order_type": {"type": "string", "enum": ["MARKET", "LIMIT"], "description": "Order type"},
                "price": {"type": "number", "description": "Limit price (0 for market orders)"},
                "quantity": {"type": "number", "description": "Order quantity"}
            },
            "required": ["symbol", "side", "order_type", "price", "quantity"]
        }
    },
    {
        "name": "get_orders",
        "description": "Get active orders, optionally filtered by symbol.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "symbol": {"type": "string", "description": "Filter by symbol (optional)"}
            }
        }
    },
    {
        "name": "get_stats",
        "description": "Get account balances and portfolio statistics.",
        "inputSchema": {
            "type": "object",
            "properties": {}
        }
    },
    {
        "name": "cancel_order",
        "description": "Cancel an active order by its order_id.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "order_id": {"type": "string", "description": "Order ID to cancel"}
            },
            "required": ["order_id"]
        }
    },
    {
        "name": "deploy_strategy",
        "description": "Create a new strategy and deploy it to the strategy library. Choose a template type and configure parameters.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "name": {"type": "string", "description": "Strategy name"},
                "strategy_type": {"type": "string", "enum": ["breakout", "grid_trading", "market_making", "arbitrage", "torch"], "description": "Strategy type"},
                "coin": {"type": "string", "description": "Trading pair, e.g. BTCUSDT"},
                "config_json": {"type": "object", "description": "Strategy-specific configuration parameters"}
            },
            "required": ["name", "strategy_type", "coin"]
        }
    },
    {
        "name": "start_strategy",
        "description": "Start a deployed strategy by its ID.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "strategy_id": {"type": "string", "description": "Strategy ID from deploy_strategy or list_strategies"}
            },
            "required": ["strategy_id"]
        }
    },
    {
        "name": "stop_strategy",
        "description": "Stop a running strategy by its ID.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "strategy_id": {"type": "string", "description": "Strategy ID to stop"}
            },
            "required": ["strategy_id"]
        }
    },
    {
        "name": "delete_strategy",
        "description": "Delete a strategy by its ID. Cannot be undone.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "strategy_id": {"type": "string", "description": "Strategy ID to delete"}
            },
            "required": ["strategy_id"]
        }
    },
    {
        "name": "get_klines",
        "description": "Get historical K-line (OHLCV) data for a symbol. Essential for backtesting and technical analysis.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "symbol": {"type": "string", "description": "Trading symbol, e.g. BTCUSDT"},
                "interval": {"type": "string", "description": "K-line interval: 1m, 5m, 15m, 1h, 4h, 1d, 1w"},
                "limit": {"type": "integer", "description": "Number of bars to fetch (default 200, max 1500)"}
            },
            "required": ["symbol"]
        }
    },
    {
        "name": "list_markets",
        "description": "List all available trading markets/exchanges.",
        "inputSchema": {
            "type": "object",
            "properties": {}
        }
    },
    {
        "name": "search_symbols",
        "description": "Search for trading symbols by keyword. Returns matching symbols with their names.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "keyword": {"type": "string", "description": "Search keyword, e.g. BTC, ETH"}
            },
            "required": ["keyword"]
        }
    },
    {
        "name": "submit_backtest",
        "description": "Submit an async backtest job. Returns a job_id for polling with get_job.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "config": {"type": "object", "description": "Backtest configuration"},
                "idempotency_key": {"type": "string", "description": "Optional key to prevent duplicate submissions"}
            },
            "required": ["config"]
        }
    },
    {
        "name": "get_job",
        "description": "Get the status and result of a submitted backtest job.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "job_id": {"type": "string", "description": "Job ID from submit_backtest"}
            },
            "required": ["job_id"]
        }
    },
]

TOOL_ENDPOINT_MAP = {
    "get_market_data": ("GET", "/market/{symbol}"),
    "run_backtest": ("POST", "/backtest"),
    "list_strategies": ("GET", "/strategies"),
    "place_paper_order": ("POST", "/orders/paper"),
    "get_orders": ("GET", "/orders"),
    "get_stats": ("GET", "/account"),
    "cancel_order": ("DELETE", "/orders/{order_id}"),
    "deploy_strategy": ("POST", "/strategies/deploy"),
    "start_strategy": ("POST", "/strategies/{strategy_id}/start"),
    "stop_strategy": ("POST", "/strategies/{strategy_id}/stop"),
    "delete_strategy": ("DELETE", "/strategies/{strategy_id}"),
    "get_klines": ("GET", "/klines/{symbol}"),
    "list_markets": ("GET", "/exchanges"),
    "search_symbols": ("GET", "/symbols/search"),
    "submit_backtest": ("POST", "/backtest"),
    "get_job": ("GET", "/backtest/jobs/{job_id}"),
}


async def http_request(method: str, path: str, body: dict | None = None) -> dict:
    """Make an HTTP request to the Agent Gateway, optionally via proxy."""
    import aiohttp
    url = f"{BASE_URL}/api/agent/v1{path}"
    headers = {"Content-Type": "application/json"}
    if AGENT_TOKEN:
        headers["Authorization"] = f"Bearer {AGENT_TOKEN}"

    timeout = aiohttp.ClientTimeout(total=30)
    async with aiohttp.ClientSession(timeout=timeout, headers=headers) as session:
        kwargs = {}
        if PROXY_URL:
            kwargs["proxy"] = PROXY_URL
        if method == "GET":
            async with session.get(url, **kwargs) as resp:
                text = await resp.text()
                if resp.status >= 400:
                    return {"error": f"HTTP {resp.status}: {text[:300]}"}
                try:
                    return json.loads(text)
                except json.JSONDecodeError:
                    return {"raw": text}
        elif method == "POST":
            async with session.post(url, json=body, **kwargs) as resp:
                text = await resp.text()
                if resp.status >= 400:
                    return {"error": f"HTTP {resp.status}: {text[:300]}"}
                try:
                    return json.loads(text)
                except json.JSONDecodeError:
                    return {"raw": text}
        elif method == "DELETE":
            async with session.delete(url, **kwargs) as resp:
                text = await resp.text()
                if resp.status >= 400:
                    return {"error": f"HTTP {resp.status}: {text[:300]}"}
                try:
                    return json.loads(text)
                except json.JSONDecodeError:
                    return {"raw": text}


async def call_ai_model(prompt: str) -> str:
    """Call the configured AI model. Returns the model's response text."""
    if not AI_CONFIG.get("api_key") or not AI_CONFIG.get("base_url"):
        return "[AI not configured] Please set up Agent AI model in Settings → Agent tab."

    import urllib.request
    import urllib.error
    provider = AI_CONFIG.get("provider", "")
    api_key = AI_CONFIG["api_key"]
    base_url = AI_CONFIG["base_url"].rstrip("/")
    model = AI_CONFIG.get("model", "gpt-4o")

    # Build OpenAI-compatible chat completions request
    url = f"{base_url}/chat/completions"
    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": "You are a quantitative trading assistant for xiaotian_quant. Help users analyze markets, create strategies, and manage trades. Keep responses concise and actionable."},
            {"role": "user", "content": prompt},
        ],
        "max_tokens": 1024,
        "temperature": 0.3,
    }

    try:
        data = json.dumps(payload).encode("utf-8")
        req = urllib.request.Request(url, data=data, headers=headers)
        resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 60)
        text = resp.read().decode("utf-8")
        if resp.status >= 400:
            return f"[AI Error] HTTP {resp.status}: {text[:300]}"
        result = json.loads(text)
        choices = result.get("choices", [])
        if choices:
            return choices[0].get("message", {}).get("content", "[No response]")
        return "[AI returned empty response]"
    except Exception as e:
        return f"[AI Error] {str(e)[:300]}"


async def handle_tool_call(name: str, arguments: dict) -> list:
    """Execute a tool call and return MCP content items."""
    endpoint_info = TOOL_ENDPOINT_MAP.get(name)
    if not endpoint_info:
        return [{"type": "text", "text": f"Unknown tool: {name}"}]

    method, path_template = endpoint_info

    if name == "get_market_data":
        symbol = arguments.get("symbol", "BTCUSDT").upper()
        path = f"/market/{symbol}"
        result = await http_request(method, path)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    elif name == "run_backtest":
        config = arguments.get("config", {})
        result = await http_request(method, path_template, body=config)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    elif name == "list_strategies":
        result = await http_request(method, path_template)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    elif name == "place_paper_order":
        body = {
            "symbol": arguments.get("symbol", "BTCUSDT"),
            "side": arguments.get("side", "BUY"),
            "order_type": arguments.get("order_type", "MARKET"),
            "price": float(arguments.get("price", 0)),
            "quantity": float(arguments.get("quantity", 0.001)),
        }
        result = await http_request(method, path_template, body=body)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    elif name == "get_orders":
        symbol = arguments.get("symbol", "")
        path = path_template
        if symbol:
            path += f"?symbol={symbol.upper()}"
        result = await http_request(method, path)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    elif name == "get_stats":
        result = await http_request(method, path_template)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    elif name == "cancel_order":
        order_id = arguments.get("order_id", "")
        path = f"/orders/{order_id}"
        result = await http_request(method, path)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    elif name == "deploy_strategy":
        body = {
            "name": arguments.get("name", "agent_strategy"),
            "strategy_type": arguments.get("strategy_type", "breakout"),
            "coin": arguments.get("coin", "BTCUSDT"),
            "config_json": arguments.get("config_json", {}),
        }
        result = await http_request(method, path_template, body=body)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    elif name == "start_strategy":
        sid = arguments.get("strategy_id", "")
        path = f"/strategies/{sid}/start"
        result = await http_request(method, path)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    elif name == "stop_strategy":
        sid = arguments.get("strategy_id", "")
        path = f"/strategies/{sid}/stop"
        result = await http_request(method, path)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    elif name == "delete_strategy":
        sid = arguments.get("strategy_id", "")
        path = f"/strategies/{sid}"
        result = await http_request(method, path)
        return [{"type": "text", "text": json.dumps(result, ensure_ascii=False)}]

    return [{"type": "text", "text": f"Tool {name} not implemented"}]


async def run_stdio():
    """Run JSON-RPC 2.0 over stdin/stdout."""
    loop = asyncio.get_running_loop()
    reader = asyncio.StreamReader()
    protocol = asyncio.StreamReaderProtocol(reader)
    await loop.connect_read_pipe(lambda: protocol, sys.stdin)

    writer = asyncio.StreamWriter(
        sys.stdout.buffer, None, loop,
        write_limits=(2**16, 2**16),
        protocol=asyncio.StreamReaderProtocol(asyncio.StreamReader()))
    await writer.drain()

    logger.info("[MCP] Server started, waiting for initialize...")

    initialized = False

    while True:
        try:
            line = await reader.readline()
            if not line:
                break
            line = line.decode("utf-8").strip()
            if not line:
                continue

            try:
                request = json.loads(line)
            except json.JSONDecodeError:
                logger.warning("[MCP] invalid JSON: %s", line[:100])
                continue

            req_id = request.get("id")
            method = request.get("method", "")

            # Handle notifications (no id)
            if req_id is None:
                if method == "notifications/initialized":
                    initialized = True
                    logger.info("[MCP] Client initialized")
                continue

            # ── initialize ──
            if method == "initialize":
                response = {
                    "jsonrpc": "2.0",
                    "id": req_id,
                    "result": {
                        "protocolVersion": request.get("params", {}).get("protocolVersion", "2024-11-05"),
                        "capabilities": {"tools": {}},
                        "serverInfo": SERVER_INFO,
                    }
                }
                resp_str = json.dumps(response, ensure_ascii=False) + "\n"
                writer.write(resp_str.encode("utf-8"))
                await writer.drain()
                logger.info("[MCP] Initialize response sent")

            # ── tools/list ──
            elif method == "tools/list":
                response = {
                    "jsonrpc": "2.0",
                    "id": req_id,
                    "result": {"tools": TOOLS}
                }
                resp_str = json.dumps(response, ensure_ascii=False) + "\n"
                writer.write(resp_str.encode("utf-8"))
                await writer.drain()

            # ── tools/call ──
            elif method == "tools/call":
                params = request.get("params", {})
                tool_name = params.get("name", "")
                arguments = params.get("arguments", {})
                content = await handle_tool_call(tool_name, arguments)
                response = {
                    "jsonrpc": "2.0",
                    "id": req_id,
                    "result": {"content": content}
                }
                resp_str = json.dumps(response, ensure_ascii=False) + "\n"
                writer.write(resp_str.encode("utf-8"))
                await writer.drain()

            # ── unknown ──
            else:
                response = {
                    "jsonrpc": "2.0",
                    "id": req_id,
                    "error": {"code": -32601, "message": f"Method not found: {method}"}
                }
                resp_str = json.dumps(response, ensure_ascii=False) + "\n"
                writer.write(resp_str.encode("utf-8"))
                await writer.drain()

        except Exception as e:
            logger.error("[MCP] Error processing request: %s", e)


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    _load_ai_config()
    if not AGENT_TOKEN:
        logger.warning("[MCP] QUANTDINGER_AGENT_TOKEN is not set — requests will be unauthenticated")
    logger.info("[MCP] Gateway: %s", BASE_URL)
    if PROXY_URL:
        logger.info("[MCP] Proxy: %s", PROXY_URL)
    asyncio.run(run_stdio())


if __name__ == "__main__":
    main()
