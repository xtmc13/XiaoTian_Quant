"""CC Switch — Anthropic ↔ OpenAI API proxy for Claude Code / Cursor / Codex.

Runs as a local HTTP proxy, translating Anthropic Messages API requests
to OpenAI Chat Completions format so you can use DeepSeek / Qwen / etc.
"""

import json
import uuid
import logging
import threading
import urllib.request
import urllib.error
from http.server import HTTPServer, BaseHTTPRequestHandler

logger = logging.getLogger("xtquant.agent.cc_switch")

# ── Global state ──
_lock = threading.RLock()
_config: dict = {
    "port": 12435,
    "target_url": "https://api.deepseek.com/v1/chat/completions",
    "target_model": "deepseek-chat",
    "api_key": "",
    "running": False,
}
_server: HTTPServer | None = None
_server_thread: threading.Thread | None = None


def get_status() -> dict:
    with _lock:
        return {
            "running": _config.get("running", False),
            "port": _config.get("port", 12435),
            "target_url": _config.get("target_url", ""),
            "target_model": _config.get("target_model", ""),
            "has_api_key": bool(_config.get("api_key")),
        }


def configure(port: int = None, target_url: str = None,
              target_model: str = None, api_key: str = None) -> dict:
    with _lock:
        if port is not None:
            _config["port"] = port
        if target_url is not None:
            _config["target_url"] = target_url
        if target_model is not None:
            _config["target_model"] = target_model
        if api_key is not None:
            _config["api_key"] = api_key
    return get_status()


def _translate_anthropic_to_openai(body: dict, target_model: str = "deepseek-chat") -> dict:
    """Convert Anthropic Messages request to OpenAI Chat Completions."""
    messages = []

    # System prompt → system message
    system = body.get("system", "")
    if system:
        if isinstance(system, list):
            system = "\n".join(s.get("text", "") for s in system if isinstance(s, dict))
        messages.append({"role": "system", "content": system})

    # Messages
    for msg in body.get("messages", []):
        role = msg.get("role", "")
        content = msg.get("content", "")
        if isinstance(content, list):
            parts = []
            for block in content:
                if isinstance(block, dict):
                    if block.get("type") == "text":
                        parts.append(block.get("text", ""))
                    elif block.get("type") == "image":
                        parts.append("[image]")
                    elif block.get("type") == "tool_use":
                        parts.append(json.dumps(block, ensure_ascii=False))
                    elif block.get("type") == "tool_result":
                        parts.append(json.dumps(block, ensure_ascii=False))
            content = "\n".join(parts)
        messages.append({"role": role, "content": content})

    openai_body = {
        "model": target_model,
        "messages": messages,
    }

    max_tokens = body.get("max_tokens", 4096)
    openai_body["max_tokens"] = min(max_tokens, 4096)

    if body.get("temperature") is not None:
        openai_body["temperature"] = body["temperature"]

    tools = body.get("tools")
    if tools:
        openai_body["tools"] = tools
        tool_choice = body.get("tool_choice")
        if tool_choice:
            openai_body["tool_choice"] = tool_choice

    # Strictly force tool mode back to the proxy
    openai_body["tool_choice"] = "auto"

    return openai_body


def _translate_openai_to_anthropic(data: dict, orig_model: str) -> dict:
    """Convert OpenAI Chat Completion response to Anthropic Messages."""
    choice = data.get("choices", [{}])[0]
    msg = choice.get("message", {})
    content_text = msg.get("content") or ""
    finish = choice.get("finish_reason", "stop")

    # Build Anthropic content blocks
    content_blocks = []
    if content_text:
        content_blocks.append({"type": "text", "text": content_text})

    # Tool calls
    tool_calls = msg.get("tool_calls", []) or []
    for tc in tool_calls:
        func = tc.get("function", {})
        try:
            args = json.loads(func.get("arguments", "{}"))
        except (json.JSONDecodeError, TypeError):
            args = {}
        content_blocks.append({
            "type": "tool_use",
            "id": tc.get("id", "toolu_" + uuid.uuid4().hex[:8]),
            "name": func.get("name", ""),
            "input": args,
        })

    stop_map = {"stop": "end_turn", "tool_calls": "tool_use", "length": "max_tokens"}
    stop_reason = stop_map.get(finish, "end_turn")

    usage = data.get("usage", {})

    return {
        "id": "msg_" + uuid.uuid4().hex[:12],
        "type": "message",
        "role": "assistant",
        "content": content_blocks,
        "model": orig_model,
        "stop_reason": stop_reason,
        "stop_sequence": None,
        "usage": {
            "input_tokens": usage.get("prompt_tokens", 0),
            "output_tokens": usage.get("completion_tokens", 0),
        },
    }


class _ProxyHandler(BaseHTTPRequestHandler):

    def log_message(self, fmt, *args):
        logger.debug("CC_SWITCH: " + fmt, *args)

    def _send(self, code: int, body: dict | str):
        data = json.dumps(body, ensure_ascii=False) if isinstance(body, dict) else body
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(data.encode("utf-8"))

    def do_OPTIONS(self):
        self.send_response(204)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "*")
        self.end_headers()

    def do_GET(self):
        if self.path.startswith("/v1/models"):
            # Forward models list to target
            self._proxy_to_target("GET")
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        if self.path.startswith("/v1/messages"):
            self._handle_messages()
        else:
            self._send(404, {"error": "not found"})

    def _handle_messages(self):
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length)
        try:
            body = json.loads(raw)
        except json.JSONDecodeError:
            self._send(400, {"error": "invalid json"})
            return

        orig_model = body.get("model", "unknown")
        with _lock:
            api_key = _config.get("api_key", "")
            target_url = _config.get("target_url", "")
            target_model = _config.get("target_model", "deepseek-chat")

        if not api_key:
            self._send(500, {"error": "CC Switch API key not configured"})
            return

        openai_body = _translate_anthropic_to_openai(body, target_model)
        req = urllib.request.Request(
            target_url,
            data=json.dumps(openai_body).encode("utf-8"),
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
        )

        try:
            resp = urllib.request.urlopen(req, timeout=120)
            resp_data = json.loads(resp.read().decode("utf-8"))
            anthropic_resp = _translate_openai_to_anthropic(resp_data, orig_model)
            self._send(200, anthropic_resp)
        except urllib.error.HTTPError as e:
            err_body = e.read().decode("utf-8", errors="replace")
            logger.error("CC_SWITCH upstream error %s: %s", e.code, err_body[:300])
            self._send(e.code, {"error": f"upstream: {e.code} {err_body[:200]}"})
        except Exception as e:
            logger.error("CC_SWITCH proxy error: %s", e)
            self._send(502, {"error": str(e)[:300]})

    def _proxy_to_target(self, method: str):
        """Simple GET forward for /v1/models etc."""
        with _lock:
            api_key = _config.get("api_key", "")
            target = _config.get("target_url", "").replace("/chat/completions", "/models")
        req = urllib.request.Request(
            target,
            headers={"Authorization": f"Bearer {api_key}"} if api_key else {},
        )
        try:
            resp = urllib.request.urlopen(req, timeout=30)
            data = resp.read()
            self.send_response(resp.status)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(data)
        except Exception as e:
            self._send(502, {"error": str(e)[:200]})


def start() -> dict:
    global _server, _server_thread
    with _lock:
        if _config.get("running"):
            return {"status": "already_running", **get_status()}

        port = _config.get("port", 12435)
        try:
            _server = HTTPServer(("127.0.0.1", port), _ProxyHandler)
            _server_thread = threading.Thread(target=_server.serve_forever, daemon=True)
            _server_thread.start()
            _config["running"] = True
            logger.info("CC_SWITCH started on http://127.0.0.1:%d", port)
            return {"status": "started", **get_status()}
        except OSError as e:
            return {"status": "error", "error": str(e), **get_status()}


def stop() -> dict:
    global _server, _server_thread
    with _lock:
        if _server:
            try:
                _server.shutdown()
            except Exception:
                pass
            _server = None
        _server_thread = None
        _config["running"] = False
        logger.info("CC_SWITCH stopped")
        return {"status": "stopped", **get_status()}
