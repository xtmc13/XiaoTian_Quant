"""
Static code quality analyzer for indicator scripts.
Detects common AI-generated code bugs and contract violations.
"""

import re
from typing import Any, Dict, List

# ── Comment stripper ──────────────────────────────────────────────

def strip_comments(code: str) -> str:
    """Remove Python comments while preserving strings."""
    result = []
    for line in code.split("\n"):
        stripped = _strip_line_comment(line)
        result.append(stripped)
    return "\n".join(result)


def _strip_line_comment(line: str) -> str:
    in_string = False
    string_char = None
    escaped = False
    for i, ch in enumerate(line):
        if escaped:
            escaped = False
            continue
        if ch == "\\" and in_string:
            escaped = True
            continue
        if not in_string and ch in ('"', "'"):
            in_string = True
            string_char = ch
            continue
        if in_string and ch == string_char:
            in_string = False
            continue
        if not in_string and ch == "#":
            return line[:i]
    return line


# ── Core analysis ─────────────────────────────────────────────────

def analyze_indicator_code_quality(code: str) -> List[Dict[str, Any]]:
    """
    Analyze indicator code and return a list of quality hints.
    Each hint: {"severity": "error"|"warn"|"info", "code": str, "params": dict}
    """
    hints = []
    if not code or not code.strip():
        return hints

    clean = strip_comments(code)

    # 1. Metadata checks
    if not re.search(r"(?m)^\s*my_indicator_name\s*=\s*['\"]", code):
        hints.append({"severity": "warn", "code": "MISSING_INDICATOR_NAME", "params": {}})

    if not re.search(r"(?m)^\s*my_indicator_description\s*=\s*['\"]", code):
        hints.append({"severity": "warn", "code": "MISSING_INDICATOR_DESCRIPTION", "params": {}})

    # 2. df.copy()
    if not re.search(r"(?m)^\s*df\s*=\s*df\.copy\s*\(\s*\)", code):
        hints.append({"severity": "info", "code": "MISSING_DF_COPY", "params": {}})

    # 3. output variable
    if not re.search(r"\boutput\s*=\s*\{", clean):
        hints.append({"severity": "error", "code": "MISSING_OUTPUT", "params": {}})

    # 4. buy/sell signals
    has_buy = re.search(r"df\s*\[\s*['\"]buy['\"]\s*\]", clean) is not None
    has_sell = re.search(r"df\s*\[\s*['\"]sell['\"]\s*\]", clean) is not None
    if not (has_buy and has_sell):
        hints.append({"severity": "warn", "code": "MISSING_BUY_SELL_COLUMNS", "params": {}})

    # 5. Declared params read via params.get()
    declared = _find_declared_params(code)
    if declared:
        unread = []
        for name in declared:
            pattern = rf"params\.get\s*\(\s*['\"`]{re.escape(name)}['\"`]"
            if not re.search(pattern, clean):
                unread.append(name)
        if unread:
            hints.append({
                "severity": "warn",
                "code": "DECLARED_PARAMS_NOT_READ_VIA_PARAMS_GET",
                "params": {"names": unread},
            })

    # 6. Future data leak
    hints.extend(_find_future_data_leak(clean))

    # 7. ndarray-pandas misuse
    hints.extend(_find_ndarray_pandas_misuse(clean))

    # 8. Unsafe imports
    unsafe = _find_unsafe_imports(clean)
    if unsafe:
        hints.append({
            "severity": "error",
            "code": "UNSAFE_IMPORT",
            "params": {"modules": unsafe},
        })

    # 9. Strategy annotations
    hints.extend(_check_strategy_annotations(code, clean, has_buy or has_sell))

    return hints


# ── Helper detectors ──────────────────────────────────────────────

PARAM_REGEX = re.compile(r"^\s*#\s*@param\s+(\S+)\s+(\S+)\s+(\S+)\s+(.+)$")
STRATEGY_REGEX = re.compile(r"^\s*#\s*@strategy\s+(\S+)\s+(.+)$")

VALID_STRATEGY_KEYS = {
    "stopLossPct", "takeProfitPct", "entryPct", "trailingEnabled",
    "trailingStopPct", "trailingActivationPct", "tradeDirection",
}

UNSAFE_MODULES = [
    "os", "sys", "subprocess", "socket", "threading", "multiprocessing",
    "requests", "urllib", "http", "ftplib", "smtplib", "sqlite3",
    "pickle", "marshal", "ctypes", "builtins",
]


def _find_declared_params(code: str) -> List[str]:
    names = []
    for line in code.split("\n"):
        m = PARAM_REGEX.match(line.strip())
        if m:
            names.append(m.group(1))
    return names


def _find_future_data_leak(clean: str) -> List[Dict[str, Any]]:
    leaks = []
    # .shift(-N)
    for m in re.finditer(r"\.shift\s*\(\s*-\s*(\d+)\s*\)", clean):
        snippet = _extract_snippet(clean, m.start(), 40)
        leaks.append({"severity": "error", "code": "FUTURE_DATA_LEAK", "params": {"kind": "shift", "snippet": snippet}})
    # .iloc[i+N]
    for m in re.finditer(r"\.iloc\s*\[\s*\w+\s*\+\s*(\d+)\s*\]", clean):
        snippet = _extract_snippet(clean, m.start(), 40)
        leaks.append({"severity": "error", "code": "FUTURE_DATA_LEAK", "params": {"kind": "iloc", "snippet": snippet}})
    # bars_ago(-N)
    for m in re.finditer(r"bars_ago\s*\(\s*-\s*(\d+)\s*\)", clean):
        snippet = _extract_snippet(clean, m.start(), 40)
        leaks.append({"severity": "error", "code": "FUTURE_DATA_LEAK", "params": {"kind": "bars_ago", "snippet": snippet}})
    return leaks


def _find_ndarray_pandas_misuse(clean: str) -> List[Dict[str, Any]]:
    misuses = []
    # Direct chain: np.where(...).rolling(...)
    for m in re.finditer(r"np\.(?:where|maximum|minimum|abs)\s*\([^)]{0,200}\)\s*\.(rolling|fillna|shift|ewm|iloc|tolist)", clean):
        snippet = _extract_snippet(clean, m.start(), 50)
        method = m.group(1) if m.lastindex else ""
        misuses.append({"severity": "error", "code": "NDARRAY_PANDAS_METHOD_MISUSE", "params": {"symbol": "np.ndarray", "method": method, "snippet": snippet}})

    # Track tainted variables
    assign_re = re.compile(r"(?m)^\s*(\w+)\s*=\s*np\.(?:where|maximum|minimum|abs)\s*\(")
    tainted = set()
    for m in assign_re.finditer(clean):
        tainted.add(m.group(1))

    for var_name in tainted:
        pattern = rf"\b{re.escape(var_name)}\b\s*\.(rolling|fillna|shift|ewm|iloc|tolist)"
        for m in re.finditer(pattern, clean):
            snippet = _extract_snippet(clean, m.start(), 50)
            method = m.group(1) if m.lastindex else ""
            misuses.append({"severity": "error", "code": "NDARRAY_PANDAS_METHOD_MISUSE", "params": {"symbol": var_name, "method": method, "snippet": snippet}})

    return misuses


def _find_unsafe_imports(clean: str) -> List[str]:
    unsafe = []
    for mod in UNSAFE_MODULES:
        if re.search(rf"(?m)^\s*import\s+{mod}\b", clean):
            unsafe.append(mod)
        elif re.search(rf"(?m)^\s*from\s+{mod}\s+import", clean):
            unsafe.append(mod)
    return unsafe


def _check_strategy_annotations(code: str, clean: str, has_signals: bool) -> List[Dict[str, Any]]:
    hints = []
    if not has_signals:
        return hints

    parsed = {}
    for line in code.split("\n"):
        m = STRATEGY_REGEX.match(line.strip())
        if m:
            parsed[m.group(1)] = m.group(2).strip()

    # Check unknown keys
    for key in parsed:
        if key not in VALID_STRATEGY_KEYS:
            hints.append({"severity": "warn", "code": "UNKNOWN_STRATEGY_KEY", "params": {"key": key}})

    # Check stop loss / take profit
    has_stop = "stopLossPct" in parsed and float(parsed["stopLossPct"]) > 0
    has_tp = "takeProfitPct" in parsed and float(parsed["takeProfitPct"]) > 0

    if not has_stop and not has_tp:
        hints.append({"severity": "warn", "code": "NO_STOP_AND_TAKE_PROFIT", "params": {}})
    else:
        if not has_stop:
            hints.append({"severity": "warn", "code": "NO_STOP_LOSS", "params": {}})
        if not has_tp:
            hints.append({"severity": "warn", "code": "NO_TAKE_PROFIT", "params": {}})

    return hints


def _extract_snippet(s: str, pos: int, max_len: int) -> str:
    start = max(0, pos - max_len // 2)
    end = min(len(s), pos + max_len // 2)
    snippet = s[start:end]
    snippet = snippet.replace("\n", " ")
    return snippet.strip()
