"""
Shared state and helpers for web route modules.
"""
import os
import yaml
import json
import time
import logging
import threading
from pathlib import Path

logger = logging.getLogger("xtquant.web")

CONFIG_PATH = Path(__file__).parent.parent.parent.parent / "config.yaml"
_startup_time = time.time()

# Global engine reference — set by app.py WebServer or main.py
_engine = None


def set_engine(engine):
    global _engine
    _engine = engine


def get_engine():
    return _engine


def _resolve_env(value):
    """Recursively resolve ${ENV_VAR} placeholders."""
    if isinstance(value, str) and value.startswith("${") and value.endswith("}"):
        return os.environ.get(value[2:-1], "")
    if isinstance(value, str):
        return value
    if isinstance(value, dict):
        return {k: _resolve_env(v) for k, v in value.items()}
    if isinstance(value, list):
        return [_resolve_env(v) for v in value]
    return value


def load_config():
    with open(CONFIG_PATH, "r", encoding="utf-8") as f:
        raw = yaml.safe_load(f) or {}
    return _resolve_env(raw)


def save_config(cfg: dict):
    with open(CONFIG_PATH, "w", encoding="utf-8") as f:
        yaml.dump(cfg, f, allow_unicode=True, default_flow_style=False, sort_keys=False)


# ── Strategy Configs Store (persisted to disk) ──

_STRATEGY_CONFIGS_PATH = Path(__file__).parent.parent.parent.parent / "strategy_configs.json"
_strategy_configs_store: dict[str, dict] = {}
_strategy_configs_lock = threading.Lock()


def _load_strategy_configs():
    global _strategy_configs_store
    try:
        if _STRATEGY_CONFIGS_PATH.exists():
            with open(_STRATEGY_CONFIGS_PATH, "r", encoding="utf-8") as f:
                data = json.load(f)
            _strategy_configs_store = {item["id"]: item for item in data if "id" in item}
            logger.info(f"Loaded {len(_strategy_configs_store)} strategy configs from disk")
    except Exception as e:
        logger.warning(f"Failed to load strategy configs: {e}")


def _save_strategy_configs():
    try:
        with _strategy_configs_lock:
            items = list(_strategy_configs_store.values())
        with open(_STRATEGY_CONFIGS_PATH, "w", encoding="utf-8") as f:
            json.dump(items, f, ensure_ascii=False, indent=2)
    except Exception as e:
        logger.error(f"Failed to save strategy configs: {e}")


# Load on import
_load_strategy_configs()


def get_strategy_configs_store():
    return _strategy_configs_store


def get_strategy_configs_lock():
    return _strategy_configs_lock


def persist_strategy_configs():
    _save_strategy_configs()


# ── In-memory stores (survive between requests, not between restarts) ──
_logs_store: list[dict] = []
_templates_store: list[dict] = []
_agent_tokens_store: list[dict] = []

# ── Orders store ──
_orders: dict = {}
_order_counter = 0


def next_order_id():
    global _order_counter
    _order_counter += 1
    return f"ord-{int(time.time()*1000)}-{_order_counter}"


def get_orders_store():
    return _orders


def get_logs_store():
    return _logs_store


def get_templates_store():
    return _templates_store


def get_agent_tokens_store():
    return _agent_tokens_store
