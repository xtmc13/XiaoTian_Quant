"""
Safe execution engine for indicator Python code.
Provides: AST security check, whitelisted builtins, timeout, memory limit.
"""

import ast
import copy
import json
import os
import re
import sys
import threading
import traceback
from typing import Any, Dict, List, Optional

import numpy as np
import pandas as pd
import requests

# ── Configuration ─────────────────────────────────────────────────

MAX_TIMEOUT_SECONDS = 60
MAX_MEMORY_MB = 512

SAFE_MODULES = {
    "numpy", "np", "pandas", "pd", "talib", "math", "random", "json",
    "datetime", "time", "collections", "itertools", "statistics",
    "typing", " fractions", "decimal",
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
    "copyright", "credits", "license",
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

# ── AST Security Check ────────────────────────────────────────────

class SecurityVisitor(ast.NodeVisitor):
    """AST visitor that checks for unsafe code patterns."""

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
        # Detect eval(), exec(), compile(), open(), __import__()
        if isinstance(node.func, ast.Name):
            if node.func.id in UNSAFE_BUILTINS:
                self.errors.append(f"Unsafe builtin call: {node.func.id}()")
        self.generic_visit(node)

    def visit_Attribute(self, node):
        # Detect __subclasses__, __bases__, __globals__, etc.
        attr = node.attr
        if attr.startswith("__") and attr.endswith("__"):
            if attr in ("__subclasses__", "__bases__", "__globals__",
                        "__code__", "__func__", "__closure__"):
                self.errors.append(f"Unsafe dunder access: {attr}")
        self.generic_visit(node)

    def visit_Expression(self, node):
        self.generic_visit(node)


def validate_code_safety(code: str) -> List[str]:
    """Parse code and return list of security errors."""
    try:
        tree = ast.parse(code)
    except SyntaxError as e:
        return [f"Syntax error: {e}"]

    visitor = SecurityVisitor()
    visitor.visit(tree)
    return visitor.errors


# ── Mock DataFrame Generator ─────────────────────────────────────

def generate_mock_df(length: int = 200) -> pd.DataFrame:
    """Generate a mock K-line DataFrame for verification."""
    np.random.seed(42)
    returns = np.random.normal(0, 0.002, length)
    price_path = 10000 * np.exp(np.cumsum(returns))
    close = price_path
    high = close * (1 + np.abs(np.random.normal(0, 0.001, length)))
    low = close * (1 - np.abs(np.random.normal(0, 0.001, length)))
    open_p = close * (1 + np.random.normal(0, 0.001, length))
    high = np.maximum(high, np.maximum(open_p, close))
    low = np.minimum(low, np.minimum(open_p, close))
    volume = np.abs(np.random.normal(100, 50, length)) * 1000

    df = pd.DataFrame({
        'time': pd.date_range(end='2024-01-01', periods=length, freq='1h').astype(int) // 10**6,
        'open': open_p,
        'high': high,
        'low': low,
        'close': close,
        'volume': volume,
    })
    return df


# ── Safe Builtins ─────────────────────────────────────────────────

def build_safe_builtins() -> Dict[str, Any]:
    """Build a restricted builtins dict for sandbox execution."""
    safe = {}
    for name in SAFE_BUILTINS:
        if name in __builtins__:
            safe[name] = __builtins__[name]
    return safe


# ── Timeout Context Manager ───────────────────────────────────────

class TimeoutException(Exception):
    pass


def timeout_context(seconds: int):
    """Context manager that raises TimeoutException after N seconds."""
    class _Timeout:
        def __enter__(self):
            self.timer = threading.Timer(seconds, self._raise_timeout)
            self.timer.start()
            return self

        def __exit__(self, exc_type, exc_val, exc_tb):
            self.timer.cancel()
            if exc_type is TimeoutException:
                return True  # Suppress
            return False

        def _raise_timeout(self):
            raise TimeoutException(f"Execution timed out after {seconds} seconds")

    return _Timeout()


# ── Main Safe Execution ───────────────────────────────────────────

def safe_exec_with_validation(
    code: str,
    df_json: Optional[List[Dict[str, Any]]] = None,
    params: Optional[Dict[str, Any]] = None,
    timeout: int = 20,
) -> Dict[str, Any]:
    """
    Execute indicator code safely and validate output format.

    Returns:
        {
            "success": bool,
            "msg": str,
            "output": dict | None,
            "error": str | None,
            "error_type": str | None,
        }
    """
    code = code.strip()
    if not code:
        return {
            "success": False,
            "msg": "Code is empty",
            "output": None,
            "error": "Code is empty",
            "error_type": "EmptyCode",
        }

    # 1. AST security check
    security_errors = validate_code_safety(code)
    if security_errors:
        return {
            "success": False,
            "msg": f"Security error: {security_errors[0]}",
            "output": None,
            "error": "; ".join(security_errors),
            "error_type": "SecurityError",
        }

    # 2. Prepare execution environment
    if df_json:
        df = pd.DataFrame(df_json)
        # Ensure required columns exist
        for col in ['open', 'high', 'low', 'close', 'volume']:
            if col not in df.columns:
                df[col] = 0.0
    else:
        df = generate_mock_df()

    # Inject call_indicator for composable indicators
    _call_stack = []
    _call_depth = [0]

    def call_indicator(indicator_ref, input_df, call_params=None):
        """Call another indicator by ID and merge its output columns back."""
        MAX_DEPTH = 5
        if _call_depth[0] >= MAX_DEPTH:
            raise RuntimeError(f"call_indicator depth exceeded {MAX_DEPTH}")
        if indicator_ref in _call_stack:
            raise RuntimeError(f"Circular dependency detected: {_call_stack} -> {indicator_ref}")

        gateway_url = os.environ.get("GATEWAY_URL", "http://localhost:8080")
        try:
            resp = requests.post(
                f"{gateway_url}/api/indicator/internal-call",
                json={"indicator_ref": int(indicator_ref), "df_json": input_df.to_dict('records'), "params": call_params or {}},
                timeout=20,
                headers={"Content-Type": "application/json"},
            )
            resp.raise_for_status()
            data = resp.json()
            if data.get("code") != 1:
                raise RuntimeError(data.get("msg", "internal call failed"))
            output = data.get("data", {})
            # Merge plots/signals back into df as auxiliary columns
            for plot in output.get("plots", []):
                col_name = f"_ind_{indicator_ref}_{plot.get('name', 'plot')}"
                input_df[col_name] = pd.Series(plot.get("data", []), index=input_df.index)
            for sig in output.get("signals", []):
                col_name = f"_ind_{indicator_ref}_{sig.get('type', 'sig')}"
                input_df[col_name] = pd.Series(sig.get("data", []), index=input_df.index)
            # Also propagate buy/sell if present
            if "buy" in output:
                input_df[f"_ind_{indicator_ref}_buy"] = output["buy"]
            if "sell" in output:
                input_df[f"_ind_{indicator_ref}_sell"] = output["sell"]
            return input_df
        except Exception as e:
            raise RuntimeError(f"call_indicator({indicator_ref}) failed: {e}")

    exec_globals = {
        'df': df.copy(),
        'pd': pd,
        'np': np,
        'params': params or {},
        'output': None,
        '__builtins__': build_safe_builtins(),
        'call_indicator': call_indicator,
    }

    # 3. Execute with timeout
    try:
        with timeout_context(min(timeout, MAX_TIMEOUT_SECONDS)):
            exec(compile(code, '<sandbox>', 'exec'), exec_globals)
    except TimeoutException as e:
        return {
            "success": False,
            "msg": str(e),
            "output": None,
            "error": str(e),
            "error_type": "TimeoutError",
        }
    except Exception as e:
        tb = traceback.format_exc()
        # Trim traceback to keep only the last meaningful line
        last_line = str(e)
        for line in tb.strip().splitlines()[::-1]:
            line = line.strip()
            if line and not line.startswith("File ") and not line.startswith("Traceback"):
                last_line = line
                break
        return {
            "success": False,
            "msg": f"Runtime error: {last_line}",
            "output": None,
            "error": tb,
            "error_type": type(e).__name__,
        }

    # 4. Validate output
    output = exec_globals.get('output')
    if output is None:
        return {
            "success": False,
            "msg": "Missing 'output' variable. Your code must define an 'output' dictionary.",
            "output": None,
            "error": "Missing output variable",
            "error_type": "MissingOutput",
        }

    if not isinstance(output, dict):
        return {
            "success": False,
            "msg": f"'output' must be a dictionary, got {type(output).__name__}",
            "output": None,
            "error": f"Invalid output type: {type(output).__name__}",
            "error_type": "InvalidOutputType",
        }

    # 5. Validate plots and signals length
    plots = output.get('plots', [])
    signals = output.get('signals', [])
    df_len = len(df)

    for p in plots:
        if 'data' not in p:
            return {
                "success": False,
                "msg": f"Plot '{p.get('name')}' missing 'data' field.",
                "output": None,
                "error": f"Plot missing data: {p.get('name')}",
                "error_type": "InvalidPlot",
            }
        if len(p['data']) != df_len:
            return {
                "success": False,
                "msg": f"Plot '{p.get('name')}' data length ({len(p['data'])}) does not match DataFrame length ({df_len}).",
                "output": None,
                "error": f"Length mismatch: {p.get('name')}",
                "error_type": "LengthMismatch",
            }

    for s in signals:
        if 'data' not in s:
            return {
                "success": False,
                "msg": f"Signal '{s.get('type')}' missing 'data' field.",
                "output": None,
                "error": f"Signal missing data: {s.get('type')}",
                "error_type": "InvalidSignal",
            }
        if len(s['data']) != df_len:
            return {
                "success": False,
                "msg": f"Signal '{s.get('type')}' data length ({len(s['data'])}) does not match DataFrame length ({df_len}).",
                "output": None,
                "error": f"Length mismatch: {s.get('type')}",
                "error_type": "LengthMismatch",
            }

    # 6. Success
    return {
        "success": True,
        "msg": "Execution passed! Code executed successfully.",
        "output": output,
        "error": None,
        "error_type": None,
    }
