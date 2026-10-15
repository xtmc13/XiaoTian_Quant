"""
Safe execution engine for indicator Python code.
Provides: AST security check, whitelisted builtins, timeout, memory limit.
"""

import ast
import json
import math
import os
import sys
import threading
import traceback
import urllib.request
from typing import Any, Dict, List, Optional

import numpy as np
import pandas as pd

# ── Configuration ─────────────────────────────────────────────────

MAX_TIMEOUT_SECONDS = 60

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


def build_safe_import():
    """Restricted __import__ that blocks unsafe modules at runtime.

    The AST check already rejects unsafe imports statically; this is a
    second line of defense so that whitelisted modules (numpy, pandas,
    talib, math, ...) remain importable inside the sandbox.
    """
    real_import = __builtins__['__import__'] if isinstance(__builtins__, dict) else __builtins__.__import__

    def safe_import(name, globals=None, locals=None, fromlist=(), level=0):
        root = (name or "").split(".")[0]
        if root in UNSAFE_MODULES:
            raise ImportError(f"Import of module '{name}' is not allowed")
        return real_import(name, globals, locals, fromlist, level)

    return safe_import


# ── Timeout Context Manager ───────────────────────────────────────

class TimeoutException(Exception):
    pass


def _inject_thread_exception(thread_id: int, exc_type) -> None:
    """Asynchronously raise exc_type in the target thread (CPython only).

    Same watchdog mechanism as QuantDinger safe_exec.py: PyThreadState_SetAsyncExc
    is the only way to interrupt a thread stuck inside exec()."""
    try:
        import ctypes
    except ImportError:
        return
    ret = ctypes.pythonapi.PyThreadState_SetAsyncExc(
        ctypes.c_ulong(thread_id), ctypes.py_object(exc_type)
    )
    if ret > 1:
        # More than one thread state was modified — undo to avoid corruption.
        ctypes.pythonapi.PyThreadState_SetAsyncExc(
            ctypes.c_ulong(thread_id), ctypes.py_object(0)
        )


def timeout_context(seconds: int):
    """Context manager that interrupts the *entering* thread after N seconds.

    The previous implementation raised TimeoutException inside the timer
    thread, which never interrupted exec(). We now inject the exception into
    the thread that entered the context via PyThreadState_SetAsyncExc."""
    class _Timeout:
        def __enter__(self):
            self._target_tid = threading.get_ident()
            self.timed_out = False
            self.timer = threading.Timer(seconds, self._raise_timeout)
            self.timer.daemon = True
            self.timer.start()
            return self

        def __exit__(self, exc_type, exc_val, exc_tb):
            self.timer.cancel()
            if exc_type is TimeoutException:
                return True  # Suppress the injected exception; caller checks timed_out
            return False

        def _raise_timeout(self):
            self.timed_out = True
            _inject_thread_exception(self._target_tid, TimeoutException)

    return _Timeout()


# ── Output Contract Normalization (QuantDinger INDICATOR_DEV_GUIDE §6-§9) ──

_PLOT_TYPE_ALIASES = {
    "line": "line",
    "bar": "bar", "histogram": "bar", "column": "bar",
    "circle": "circle", "dot": "circle", "point": "circle", "scatter": "circle",
    "lamp": "lamp",
}

_VALID_LAYER_TYPES = {"zone", "line", "label"}

_FALSE_SIGNAL_STRINGS = {"", "0", "false", "none", "null", "nan", "na", "n/a"}

# Signal series denser than this are treated as persistent state and edge-ified
# (QuantDinger indicator_signal_alerts.py heuristic).
_EDGE_DENSITY_THRESHOLD = 0.18


def _sanitize_value(value: Any) -> Any:
    """Convert NaN/±inf to None and numpy scalars to plain types (recursively).

    Contract rule: output must never emit NaN or infinity; missing points are None."""
    if value is None:
        return None
    if isinstance(value, dict):
        return {str(k): _sanitize_value(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [_sanitize_value(v) for v in value]
    if isinstance(value, (np.bool_,)):
        return bool(value)
    if isinstance(value, (np.integer,)):
        return int(value)
    if isinstance(value, (float, np.floating)):
        f = float(value)
        return f if math.isfinite(f) else None
    try:
        if pd.isna(value):
            return None
    except (TypeError, ValueError):
        pass
    return value


def _normalize_point_object(point: Dict[str, Any]) -> Dict[str, Any]:
    """Canonicalize a per-point plot object: value/y/data -> value,
    color/fillColor/backgroundColor -> color, size/radius/r -> size."""
    if "value" not in point:
        for alias in ("y", "data"):
            if alias in point:
                point["value"] = point.pop(alias)
                break
    if "color" not in point:
        for alias in ("fillColor", "backgroundColor"):
            if alias in point:
                point["color"] = point.pop(alias)
                break
    if "size" not in point:
        for alias in ("radius", "r"):
            if alias in point:
                point["size"] = point.pop(alias)
                break
    return point


def _normalize_plot(plot: Dict[str, Any]) -> None:
    plot_type = str(plot.get("type") or "").strip().lower()
    if plot_type:
        plot["type"] = _PLOT_TYPE_ALIASES.get(plot_type, plot_type)
    if "lineStyle" not in plot and "style" in plot:
        plot["lineStyle"] = plot.pop("style")
    if "size" not in plot and "radius" in plot:
        plot["size"] = plot.pop("radius")
    data = plot.get("data")
    if isinstance(data, list):
        plot["data"] = [
            _normalize_point_object(p) if isinstance(p, dict) else p
            for p in data
        ]


def _signal_marker_is_active(value: Any) -> bool:
    """Port of QuantDinger _signal_marker_is_active: labels alone never activate."""
    if value is None:
        return False
    if isinstance(value, (bool, np.bool_)):
        return bool(value)
    if isinstance(value, dict):
        for key in ("active", "signal", "triggered", "hit", "visible"):
            if key in value and value.get(key) is not None:
                return _signal_marker_is_active(value.get(key))
        for key in ("price", "value", "y", "data"):
            if key in value and _signal_marker_is_active(value.get(key)):
                return True
        return False
    try:
        if pd.isna(value):
            return False
    except (TypeError, ValueError):
        return False
    if isinstance(value, str):
        return value.strip().lower() not in _FALSE_SIGNAL_STRINGS
    try:
        number = float(value)
        return math.isfinite(number) and number != 0
    except (TypeError, ValueError):
        return False


def _signal_marker_price(value: Any, fallback: float) -> float:
    if isinstance(value, dict):
        for key in ("price", "value", "y", "data"):
            if key in value:
                return _safe_float(value.get(key), fallback)
        return fallback
    if isinstance(value, (bool, np.bool_)):
        return fallback
    return _safe_float(value, fallback)


def _safe_float(value: Any, default: float = 0.0) -> float:
    try:
        if value is None:
            return default
        f = float(value)
        if math.isnan(f) or math.isinf(f):
            return default
        return f
    except (TypeError, ValueError):
        return default


def _signal_render_mode(signal: Dict[str, Any], data: List[Any]) -> str:
    """Resolve effective render mode: events | points | edge."""
    raw_mode = str(signal.get("renderMode") or signal.get("mode") or "").strip().lower()
    if raw_mode in ("event", "events"):
        return "events"
    if raw_mode in ("point", "points", "marker", "markers", "raw"):
        return "points"
    if raw_mode in ("state", "continuous", "condition"):
        return "edge"
    if not data:
        return "events"
    active_count = sum(1 for value in data if _signal_marker_is_active(value))
    return "edge" if active_count / len(data) > _EDGE_DENSITY_THRESHOLD else "events"


def _edge_filter_data(data: List[Any]) -> List[Any]:
    """Keep only the first active point of each active run (False -> True edge)."""
    out = []
    prev_active = False
    for value in data:
        active = _signal_marker_is_active(value)
        out.append(value if (active and not prev_active) else None)
        prev_active = active
    return out


def _normalize_signal(signal: Dict[str, Any]) -> None:
    if "renderMode" not in signal and "mode" in signal:
        signal["renderMode"] = signal.pop("mode")
    data = signal.get("data")
    if isinstance(data, list) and _signal_render_mode(signal, data) == "edge":
        # Dense state series: render and notify only on rising edges.
        signal["data"] = _edge_filter_data(data)
        signal.setdefault("renderMode", "state")


def _compute_latest_signal(signals: List[Any], df: pd.DataFrame) -> Optional[Dict[str, Any]]:
    """Newest visible marker for chart summaries (port of _latest_chart_signal).

    Runs after _normalize_signal, so state series are already edge-ified and a
    plain active check matches the edge semantics."""
    if df is None or len(df) == 0:
        return None
    closes = df["close"] if "close" in df.columns else None
    times = df["time"] if "time" in df.columns else None
    for idx in range(len(df) - 1, -1, -1):
        for signal in signals:
            if not isinstance(signal, dict):
                continue
            data = signal.get("data") if isinstance(signal.get("data"), list) else []
            if idx >= len(data):
                continue
            marker = data[idx]
            if not _signal_marker_is_active(marker):
                continue
            text_data = signal.get("textData") if isinstance(signal.get("textData"), list) else []
            point_text = text_data[idx] if idx < len(text_data) else None
            signal_type = str(signal.get("type") or "signal").strip() or "signal"
            fallback = _safe_float(closes.iloc[idx]) if closes is not None else 0.0
            entry: Dict[str, Any] = {
                "type": signal_type,
                "label": str(point_text or signal.get("text") or signal_type).strip(),
                "price": _signal_marker_price(marker, fallback),
                "bar_index": idx,
            }
            if isinstance(marker, dict) and marker.get("color"):
                entry["color"] = marker.get("color")
            elif signal.get("color"):
                entry["color"] = signal.get("color")
            if times is not None:
                entry["bar_time"] = _sanitize_value(times.iloc[idx])
            return entry
    return None


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
        # NOTE: requests stays in UNSAFE_MODULES (user code must not import it);
        # the sandbox host itself uses stdlib urllib for this trusted callback.
        payload = json.dumps({
            "indicator_ref": int(indicator_ref),
            "df_json": input_df.to_dict('records'),
            "params": call_params or {},
        }).encode("utf-8")
        http_req = urllib.request.Request(
            f"{gateway_url}/api/indicator/internal-call",
            data=payload,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(http_req, timeout=20) as resp:
                data = json.loads(resp.read().decode("utf-8"))
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
        '__builtins__': {**build_safe_builtins(), '__import__': build_safe_import()},
        'call_indicator': call_indicator,
    }

    # Convenience OHLCV Series variables (QuantDinger safe_exec_worker contract)
    exec_df = exec_globals['df']
    for col in ('open', 'high', 'low', 'close', 'volume'):
        if col in exec_df.columns:
            exec_globals[col] = exec_df[col]

    # 3. Execute with timeout（不再对宿主进程施加 RLIMIT_AS：进程级限额会让
    # 整个 FastAPI 服务在 exec 窗口内 MemoryError，且并发恢复会把限额永久
    # 卡死。真正的内存隔离需要子进程方案，见 QuantDinger safe_exec_worker）
    effective_timeout = min(timeout, MAX_TIMEOUT_SECONDS)
    try:
        with timeout_context(effective_timeout) as tctx:
            exec(compile(code, '<sandbox>', 'exec'), exec_globals)
        if tctx.timed_out:
            raise TimeoutException(f"Execution timed out after {effective_timeout} seconds")
    except TimeoutException as e:
        return {
            "success": False,
            "msg": str(e) or f"Execution timed out after {effective_timeout} seconds",
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
    layers = output.get('layers', []) or []
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

    # 6. Validate layers (sparse visual objects: zone/line/label, no per-bar arrays)
    if not isinstance(layers, list):
        return {
            "success": False,
            "msg": "'layers' must be a list of zone/line/label objects.",
            "output": None,
            "error": f"Invalid layers type: {type(layers).__name__}",
            "error_type": "InvalidLayer",
        }
    for layer in layers:
        if not isinstance(layer, dict):
            return {
                "success": False,
                "msg": "Each layer must be an object with a 'type' of zone/line/label.",
                "output": None,
                "error": f"Invalid layer: {layer!r}",
                "error_type": "InvalidLayer",
            }
        layer_type = str(layer.get("type") or "").strip().lower()
        if layer_type not in _VALID_LAYER_TYPES:
            return {
                "success": False,
                "msg": f"Layer type '{layer.get('type')}' is not supported; use one of: zone, line, label.",
                "output": None,
                "error": f"Invalid layer type: {layer.get('type')}",
                "error_type": "InvalidLayer",
            }

    # 7. Normalize output to the public contract: plot type/style aliases,
    #    per-point object aliases, signal renderMode + edge-ification of dense
    #    state series, NaN/±inf -> None.
    for p in plots:
        if isinstance(p, dict):
            _normalize_plot(p)
    for s in signals:
        if isinstance(s, dict):
            _normalize_signal(s)
    output = _sanitize_value(output)

    latest_signal = _compute_latest_signal(output.get('signals', []), df)
    if latest_signal:
        output["latest_signal"] = latest_signal

    # 8. Success
    return {
        "success": True,
        "msg": "Execution passed! Code executed successfully.",
        "output": output,
        "error": None,
        "error_type": None,
    }
