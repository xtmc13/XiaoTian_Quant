"""
Python Strategy Engine — Executes user-defined Python strategies for XiaoTianQuant.

Supported strategy modes:
- indicator: Vectorized DataFrame strategy. Code manipulates `df` and must produce
  `df['buy']` and `df['sell']` columns (1/0 or bool).
- script: Event-driven strategy. The `on_bar(ctx, bar)` function is called for each bar.
  ctx provides: ctx.buy(), ctx.sell(), ctx.close_position(), ctx.position_size().

Run: python main.py  (listens on port 8003)
"""
import ast
import json
import os
import threading
import traceback
from typing import Any, Dict, List, Optional

import numpy as np
import pandas as pd
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

app = FastAPI(title="XiaoTianQuant Python Strategy Engine", version="1.0.0")

# ── Configuration ─────────────────────────────────────────────────

MAX_TIMEOUT_SECONDS = 60
MAX_MEMORY_MB = 512

SAFE_MODULES = {
    "numpy", "np", "pandas", "pd", "talib", "math", "random", "json",
    "datetime", "time", "collections", "itertools", "statistics",
    "typing", "fractions", "decimal",
}

UNSAFE_MODULES = {
    "os", "sys", "subprocess", "socket", "threading", "multiprocessing",
    "requests", "urllib", "http", "ftplib", "smtplib", "sqlite3",
    "pickle", "marshal", "ctypes", "builtins", "importlib",
}

UNSAFE_BUILTINS = {
    "eval", "exec", "compile", "open", "__import__", "getattr",
    "setattr", "delattr", "globals", "vars", "dir", "input",
    "raw_input", "help", "quit", "exit", "breakpoint",
}

SAFE_BUILTINS = {
    "abs", "all", "any", "bin", "bool", "bytearray", "bytes",
    "chr", "complex", "dict", "divmod", "enumerate", "filter",
    "float", "format", "frozenset", "hasattr", "hash", "hex",
    "int", "isinstance", "issubclass", "iter", "len", "list",
    "map", "max", "min", "next", "oct", "ord", "pow", "range",
    "repr", "reversed", "round", "set", "slice", "sorted", "str",
    "sum", "tuple", "type", "zip", "print", "staticmethod",
    "classmethod", "property", "Exception", "ValueError", "TypeError",
    "RuntimeError", "ArithmeticError", "LookupError", "IndexError",
    "KeyError", "AttributeError", "ZeroDivisionError", "AssertionError",
}


# ═══════════════════════════════════════════════════════════════════
# Security
# ═══════════════════════════════════════════════════════════════════

class SecurityVisitor(ast.NodeVisitor):
    def __init__(self):
        self.errors = []

    def visit_Import(self, node):
        for alias in node.names:
            mod = alias.name.split(".")[0]
            if mod in UNSAFE_MODULES:
                self.errors.append(f"Unsafe import: {alias.name}")
        self.generic_visit(node)

    def visit_ImportFrom(self, node):
        if node.module:
            mod = node.module.split(".")[0]
            if mod in UNSAFE_MODULES:
                self.errors.append(f"Unsafe import from: {node.module}")
        self.generic_visit(node)

    def visit_Call(self, node):
        if isinstance(node.func, ast.Name) and node.func.id in UNSAFE_BUILTINS:
            self.errors.append(f"Unsafe builtin call: {node.func.id}()")
        self.generic_visit(node)


def validate_code_safety(code: str) -> List[str]:
    try:
        tree = ast.parse(code)
    except SyntaxError as e:
        return [f"Syntax error: {e}"]
    visitor = SecurityVisitor()
    visitor.visit(tree)
    return visitor.errors


def build_safe_builtins() -> Dict[str, Any]:
    safe = {}
    builtins_dict = vars(__builtins__) if isinstance(__builtins__, type(os)) else __builtins__
    for name in SAFE_BUILTINS:
        if name in builtins_dict:
            safe[name] = builtins_dict[name]
    return safe


class TimeoutException(Exception):
    pass


def timeout_context(seconds: int):
    class _Timeout:
        def __enter__(self):
            self.timer = threading.Timer(seconds, self._raise_timeout)
            self.timer.start()
            return self

        def __exit__(self, exc_type, exc_val, exc_tb):
            self.timer.cancel()
            if exc_type is TimeoutException:
                return True
            return False

        def _raise_timeout(self):
            raise TimeoutException(f"Execution timed out after {seconds} seconds")

    return _Timeout()


def apply_memory_limit(mb: int):
    try:
        import resource
        soft, hard = resource.getrlimit(resource.RLIMIT_AS)
        limit_bytes = mb * 1024 * 1024
        if soft > limit_bytes or soft == -1:
            resource.setrlimit(resource.RLIMIT_AS, (limit_bytes, hard))
    except (ImportError, OSError, ValueError):
        pass


# ═══════════════════════════════════════════════════════════════════
# Request / Response Models
# ═══════════════════════════════════════════════════════════════════

class IndicatorStrategyRequest(BaseModel):
    code: str
    bars: List[Dict[str, Any]]
    params: Dict[str, Any] = {}
    symbol: str = "BTCUSDT"
    interval: str = "1h"
    timeout: int = 30


class ScriptStrategyRequest(BaseModel):
    code: str
    bars: List[Dict[str, Any]]
    params: Dict[str, Any] = {}
    symbol: str = "BTCUSDT"
    interval: str = "1h"
    initial_balance: float = 100000.0
    timeout: int = 30


class Signal(BaseModel):
    time: int
    bar_index: int
    action: str  # buy / sell / close
    price: float
    reason: str = ""


# ═══════════════════════════════════════════════════════════════════
# Indicator (vectorized) strategy
# ═══════════════════════════════════════════════════════════════════

@app.post("/indicator/run")
def run_indicator_strategy(req: IndicatorStrategyRequest):
    """Run a vectorized DataFrame strategy and return buy/sell signals."""
    errors = validate_code_safety(req.code)
    if errors:
        raise HTTPException(status_code=400, detail=errors[0])

    if not req.bars:
        return {"success": True, "signals": [], "count": 0}

    df = pd.DataFrame(req.bars)
    for col in ["open", "high", "low", "close", "volume"]:
        if col not in df.columns:
            df[col] = 0.0

    exec_globals = {
        "df": df.copy(),
        "pd": pd,
        "np": np,
        "params": req.params,
        "symbol": req.symbol,
        "interval": req.interval,
        "__builtins__": build_safe_builtins(),
    }

    try:
        apply_memory_limit(MAX_MEMORY_MB)
        with timeout_context(min(req.timeout, MAX_TIMEOUT_SECONDS)):
            exec(compile(req.code, "<strategy>", "exec"), exec_globals)
    except TimeoutException as e:
        raise HTTPException(status_code=408, detail=str(e))
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Runtime error: {traceback.format_exc()}")

    result_df = exec_globals.get("df", df)
    if "buy" not in result_df.columns:
        result_df["buy"] = 0
    if "sell" not in result_df.columns:
        result_df["sell"] = 0

    signals = []
    for i, row in result_df.iterrows():
        buy = bool(row.get("buy", 0)) if not pd.isna(row.get("buy", 0)) else False
        sell = bool(row.get("sell", 0)) if not pd.isna(row.get("sell", 0)) else False
        time_val = int(row.get("time", 0))
        if buy:
            signals.append({
                "time": time_val,
                "bar_index": int(i),
                "action": "buy",
                "price": float(row.get("close", 0)),
                "reason": "indicator buy signal",
            })
        elif sell:
            signals.append({
                "time": time_val,
                "bar_index": int(i),
                "action": "sell",
                "price": float(row.get("close", 0)),
                "reason": "indicator sell signal",
            })

    return {"success": True, "signals": signals, "count": len(signals)}


# ═══════════════════════════════════════════════════════════════════
# Script (event-driven) strategy
# ═══════════════════════════════════════════════════════════════════

class ScriptContext:
    """Execution context for script strategies."""

    def __init__(self, params: Dict[str, Any], initial_balance: float):
        self.params = params
        self.balance = initial_balance
        self.position = 0.0  # positive long, negative short
        self.signals: List[Dict[str, Any]] = []

    def buy(self, size: float = 1.0, price: float = 0.0):
        self.position += size
        self.signals.append({"action": "buy", "size": size, "price": price})

    def sell(self, size: float = 1.0, price: float = 0.0):
        self.position -= size
        self.signals.append({"action": "sell", "size": size, "price": price})

    def close_position(self, price: float = 0.0):
        if self.position != 0:
            action = "sell" if self.position > 0 else "buy"
            self.signals.append({"action": action, "size": abs(self.position), "price": price})
            self.position = 0.0

    def position_size(self) -> float:
        return self.position


@app.post("/script/run")
def run_script_strategy(req: ScriptStrategyRequest):
    """Run an event-driven script strategy and return signals."""
    errors = validate_code_safety(req.code)
    if errors:
        raise HTTPException(status_code=400, detail=errors[0])

    if not req.bars:
        return {"success": True, "signals": [], "count": 0}

    ctx = ScriptContext(req.params, req.initial_balance)

    # Prepare namespace with on_bar function from user code
    exec_globals = {
        "pd": pd,
        "np": np,
        "params": req.params,
        "symbol": req.symbol,
        "interval": req.interval,
        "__builtins__": build_safe_builtins(),
    }

    try:
        apply_memory_limit(MAX_MEMORY_MB)
        with timeout_context(min(req.timeout, MAX_TIMEOUT_SECONDS)):
            exec(compile(req.code, "<strategy>", "exec"), exec_globals)
    except TimeoutException as e:
        raise HTTPException(status_code=408, detail=str(e))
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Runtime error: {traceback.format_exc()}")

    on_bar = exec_globals.get("on_bar")
    if not callable(on_bar):
        raise HTTPException(status_code=400, detail="Strategy code must define an on_bar(ctx, bar) function")

    signals: List[Dict[str, Any]] = []
    for idx, bar in enumerate(req.bars):
        ctx.signals = []
        try:
            with timeout_context(min(req.timeout, MAX_TIMEOUT_SECONDS)):
                on_bar(ctx, bar)
        except TimeoutException as e:
            raise HTTPException(status_code=408, detail=str(e))
        except Exception as e:
            raise HTTPException(status_code=500, detail=f"on_bar error: {traceback.format_exc()}")

        for sig in ctx.signals:
            signals.append({
                "time": int(bar.get("time", 0)),
                "bar_index": idx,
                "action": sig["action"],
                "price": float(sig.get("price", bar.get("close", 0))),
                "reason": "script strategy",
            })

    return {"success": True, "signals": signals, "count": len(signals)}


# ═══════════════════════════════════════════════════════════════════
# Health
# ═══════════════════════════════════════════════════════════════════

@app.get("/health")
def health():
    return {"status": "ok"}


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8003)
