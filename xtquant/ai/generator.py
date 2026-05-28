"""
AI strategy generator — OpenAI-compatible API integration + sandboxed execution.

Supports:
  - OpenAI GPT-4, GPT-3.5
  - Any OpenAI-compatible endpoint (通义千问, DeepSeek, etc.)
  - Static code validation + restricted sandbox execution

Configuration via environment variables:
  OPENAI_API_KEY  — required
  OPENAI_BASE_URL — optional (defaults to https://api.openai.com/v1)
  OPENAI_MODEL    — optional (defaults to gpt-4)
"""

import os
import re
import json
import asyncio
import logging
from typing import Tuple

logger = logging.getLogger("xtquant.ai")

# ── Safe builtins whitelist ──
SAFE_BUILTINS = {
    "abs", "all", "any", "ascii", "bin", "bool", "bytearray", "bytes",
    "callable", "chr", "classmethod", "complex", "copyright", "credits",
    "delattr", "dict", "dir", "divmod", "enumerate", "filter", "float",
    "format", "frozenset", "getattr", "globals", "hasattr", "hash",
    "hex", "id", "int", "isinstance", "issubclass", "iter", "len",
    "license", "list", "locals", "map", "max", "memoryview", "min",
    "next", "object", "oct", "ord", "pow", "print", "property",
    "range", "repr", "reversed", "round", "set", "setattr", "slice",
    "sorted", "staticmethod", "str", "sum", "super", "tuple", "type",
    "vars", "zip", "None", "True", "False", "Ellipsis", "Exception",
    "ValueError", "TypeError", "KeyError", "IndexError", "StopIteration",
    "ArithmeticError", "ZeroDivisionError", "RuntimeError",
    "__build_class__", "__import__",
}

SAFE_IMPORTS = {"math", "numpy", "collections", "itertools", "functools", "random",
                "datetime", "time", "json", "statistics", "decimal", "fractions",
                "asyncio", "logging", "xtquant"}

FORBIDDEN_PATTERNS = [
    r'\b__import__\s*\(',
    r'\beval\s*\(',
    r'\bexec\s*\(',
    r'\bcompile\s*\(',
    r'\bopen\s*\(',
    r'\bos\.',
    r'\bsubprocess\b',
    r'\bsys\.exit\b',
    r'\bimportlib\b',
    r'\bshutil\b',
    r'\bglobals\s*\(\s*\)',
    r'\blocals\s*\(\s*\)',
    r'\bgetattr\s*\(',
    r'\bsetattr\s*\(',
    r'\bdelattr\s*\(',
    r'__class__',
    r'__bases__',
    r'__mro__',
    r'__subclasses__',
    r'__globals__',
    r'__builtins__',
    r'__code__',
    r'__dict__',
    r'\bbreakpoint\s*\(',
    r'__reduce__',
    r'__reduce_ex__',
]


def gather_market_context(bars: list, symbol: str, interval: str) -> str:
    """Compute market analytics from kline bars and return a markdown summary for AI prompts.

    Args:
        bars: List of Bar objects (sorted by timestamp, oldest first)
        symbol: Trading pair (e.g. BTCUSDT)
        interval: K-line interval (e.g. 1h, 4h, 1d)

    Returns:
        Markdown string with current price, trend, volatility, volume analysis.
    """
    if not bars or len(bars) < 20:
        return ""

    closes = [b.close for b in bars]
    volumes = [b.volume for b in bars]
    highs = [b.high for b in bars]
    lows = [b.low for b in bars]
    current_price = closes[-1]

    # Price changes
    price_1h = closes[-2] if len(closes) >= 2 else current_price
    change_1h = (current_price - price_1h) / price_1h * 100 if price_1h else 0
    price_24h = closes[-min(len(closes), 24)]
    change_24h = (current_price - price_24h) / price_24h * 100 if price_24h else 0

    # Moving averages
    def sma(data, n):
        if len(data) < n:
            return None
        return sum(data[-n:]) / n

    ma20 = sma(closes, 20)
    ma60 = sma(closes, 60) if len(closes) >= 60 else ma20
    ma_trend = ""
    if ma20 and ma60:
        if ma20 > ma60 * 1.02:
            ma_trend = "Bullish (MA20 > MA60, uptrend)"
        elif ma20 < ma60 * 0.98:
            ma_trend = "Bearish (MA20 < MA60, downtrend)"
        else:
            ma_trend = "Neutral (MA20 ≈ MA60, ranging)"

    # Volatility (ATR-14 and daily range %)
    tr_list = []
    for i in range(1, len(bars)):
        h, l, pc = highs[i], lows[i], closes[i-1]
        tr = max(h - l, abs(h - pc), abs(l - pc))
        tr_list.append(tr)
    atr14 = sum(tr_list[-14:]) / 14 if len(tr_list) >= 14 else sum(tr_list) / max(len(tr_list), 1)
    volatility_pct = (atr14 / current_price) * 100 if current_price else 0

    # Volume trend
    vol_ma5 = sum(volumes[-5:]) / 5 if len(volumes) >= 5 else volumes[-1]
    vol_ma20 = sum(volumes[-20:]) / 20 if len(volumes) >= 20 else vol_ma5
    vol_trend = "Increasing" if vol_ma5 > vol_ma20 * 1.1 else ("Decreasing" if vol_ma5 < vol_ma20 * 0.9 else "Stable")

    # Support / Resistance (simple: recent min/max)
    recent_high = max(highs[-20:])
    recent_low = min(lows[-20:])

    return f"""## Market Context (Real-time)
- **Current Price**: {current_price:.2f} USDT
- **1-Period Change**: {change_1h:+.2f}% | **24-Period Change**: {change_24h:+.2f}%
- **MA Trend**: {ma_trend}
- **MA20**: {ma20:.2f} | **MA60**: {ma60:.2f} (if available)
- **ATR(14)**: {atr14:.2f} | **Volatility**: {volatility_pct:.2f}%
- **Volume**: {vol_trend} (short-term vs. 20-period avg)
- **Recent 20-bar Range**: {recent_low:.2f} – {recent_high:.2f}
- **Interval**: {interval}"""


def _restricted_import(name, globals=None, locals=None, fromlist=(), level=0):
    """Only allow imports from the whitelist or submodules of whitelisted packages."""
    top = name.split(".")[0]
    if top in SAFE_IMPORTS:
        return __import__(name, globals, locals, fromlist, level)
    raise ImportError(f"import of '{name}' is not allowed")


class AIStrategyGenerator:
    """Generates trading strategy code via OpenAI-compatible API."""

    def __init__(self, api_key: str = None, base_url: str = None, model: str = None):
        self.api_key = api_key or os.environ.get("OPENAI_API_KEY", "")
        self.base_url = (base_url or os.environ.get("OPENAI_BASE_URL", "")
                        ).rstrip("/") or "https://api.openai.com/v1"
        self.model = model or os.environ.get("OPENAI_MODEL", "gpt-4")

    # ── System Prompt ──
    def build_system_prompt(self) -> str:
        return """You are a quantitative trading strategy engineer. Generate Python code for a trading strategy.

IMPORTANT RULES:
1. The strategy class MUST inherit from BaseStrategy (import: from xtquant.strategy.base import BaseStrategy)
2. The class MUST implement: async def on_tick(self, tick)  and  async def run(self)
3. Access market data via tick.price, tick.symbol, tick.volume, tick.timestamp
4. Place orders via: self.buy(symbol, price, qty)  and  self.sell(symbol, price, qty)
5. Get current price: self.get_price(symbol)
6. Track positions: self.positions dict
7. The run() method should be a simple while self._running loop with await asyncio.sleep(1)
8. DO NOT use __import__, eval, exec, open, os, subprocess, sys.exit, or any I/O operations
9. DO NOT access internal engine attributes directly
10. Keep the strategy simple and self-contained (max ~80 lines of code)

The strategy MUST follow this class structure:
```python
import asyncio
from xtquant.strategy.base import BaseStrategy
from xtquant.core.data import Tick

class AIStrategy(BaseStrategy):
    def __init__(self, symbols, **params):
        super().__init__('AIStrategy', symbols)
        # Store any user parameters here

    async def on_tick(self, tick):
        # Trading logic here
        pass

    async def run(self):
        while self._running:
            await asyncio.sleep(1)
```

Parameter naming conventions (use these in __init__):
- first_amount: initial order amount in USDT
- avg_count: number of averaging orders
- tp_ratio: take profit ratio in percentage
- sl_ratio: stop loss ratio in percentage
- period: lookback period for indicators
- qty: order quantity

RESPOND WITH ONLY A VALID JSON OBJECT. NO markdown, NO code fences, NO extra text:
{"strategy_name": "StrategyName", "strategy_code": "import ...\\n\\nclass AIStrategy...", "description": "Brief description of how the strategy works"}"""

    def build_vectorized_prompt(self) -> str:
        return """You are a quantitative trading strategy engineer. Generate Python code for a VECTORIZED (DataFrame-based) trading strategy.

IMPORTANT: Vectorized strategies operate on the ENTIRE DataFrame at once — indicators are calculated as columns, signals are boolean masks. This is MUCH faster than event-driven strategies.

RULES:
1. The strategy class MUST inherit from VectorizedStrategy (from xtquant.strategy.vectorized import VectorizedStrategy)
2. The class MUST implement THREE methods:
   a. populate_indicators(self, dataframe, metadata) -> DataFrame:  Add indicator columns (RSI, MACD, EMA, etc.)
   b. populate_entry_trend(self, dataframe, metadata) -> DataFrame:  Set 'enter_long' or 'enter_short' to 1 where signals trigger
   c. populate_exit_trend(self, dataframe, metadata) -> DataFrame:  Set 'exit_long' or 'exit_short' to 1 where signals trigger
3. Available DataFrame columns: 'open', 'high', 'low', 'close', 'volume'
4. Use pandas/numpy for calculations: dataframe['rsi'] = ..., dataframe['ema'] = dataframe['close'].ewm(span=20).mean()
5. DO NOT use any I/O, __import__, eval, exec, open, os, subprocess, sys.exit
6. Set class-level attributes: timeframe (default "1h"), stoploss (default -0.05), minimal_roi (default {"0": 0.10})
7. Keep the strategy simple (max ~80 lines)

The strategy MUST follow this class structure:
```python
import numpy as np
import pandas as pd
from xtquant.strategy.vectorized import VectorizedStrategy

class AIVectorizedStrategy(VectorizedStrategy):
    timeframe = "1h"
    stoploss = -0.05
    minimal_roi = {"0": 0.10}

    def __init__(self, symbols=None, **params):
        super().__init__('AIVectorizedStrategy', symbols or ["BTCUSDT"], params)

    def populate_indicators(self, dataframe, metadata=None):
        # Calculate indicators as columns
        return dataframe

    def populate_entry_trend(self, dataframe, metadata=None):
        # Set enter_long / enter_short columns
        dataframe['enter_long'] = 0
        return dataframe

    def populate_exit_trend(self, dataframe, metadata=None):
        # Set exit_long / exit_short columns
        dataframe['exit_long'] = 0
        return dataframe
```

Signal column conventions (use exactly these names):
- 'enter_long' = 1  →  open a long position
- 'exit_long'  = 1  →  close a long position
- 'enter_short' = 1 →  open a short position (optional)
- 'exit_short'  = 1 →  close a short position (optional)

RESPOND WITH ONLY A VALID JSON OBJECT. NO markdown, NO code fences, NO extra text:
{"strategy_name": "StrategyName", "strategy_code": "import ...\\n\\nclass AIVectorizedStrategy...", "description": "Brief description of the strategy logic"}"""

    def build_user_prompt(self, symbol: str, interval: str, risk: str, prompt: str,
                          market_context: str = "") -> str:
        risk_desc = {"low": "conservative with low drawdown", "medium": "balanced",
                     "high": "aggressive with high returns"}.get(risk, "balanced")
        parts = [f"""Trading pair: {symbol}
K-line interval: {interval}
Risk preference: {risk} ({risk_desc})"""]
        if market_context:
            parts.append(market_context)
        parts.append(f"""User request: {prompt}

Generate a complete trading strategy that matches the above requirements.""")
        return "\n\n".join(parts)

    # ── OpenAI API Call ──
    async def generate(self, symbol: str, interval: str, risk: str, prompt: str,
                       market_context: str = "", strategy_type: str = "event_driven") -> dict:
        """Call AI to generate strategy code. Returns {strategy_name, strategy_code, description}.

        Args:
            strategy_type: 'event_driven' (default, BaseStrategy) or 'vectorized' (VectorizedStrategy)
        """
        import urllib.request
        import urllib.error

        if not self.api_key:
            raise ValueError("OPENAI_API_KEY is not configured")

        if strategy_type == "vectorized":
            system_prompt = self.build_vectorized_prompt()
        else:
            system_prompt = self.build_system_prompt()
        user_prompt = self.build_user_prompt(symbol, interval, risk, prompt, market_context)

        payload = {
            "model": self.model,
            "messages": [
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_prompt},
            ],
            "temperature": 0.7,
            "max_tokens": 4096,
        }

        url = f"{self.base_url}/chat/completions"
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

        try:
            data = json.dumps(payload).encode("utf-8")
            req = urllib.request.Request(url, data=data, headers=headers)
            resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 60)
            text = resp.read().decode("utf-8")
            if resp.status != 200:
                logger.error(f"AI API error {resp.status}: {text[:500]}")
                raise ValueError(f"AI API returned {resp.status}: {text[:200]}")
        except urllib.error.URLError as e:
            raise ValueError(f"AI API connection error: {e}")

        # Parse OpenAI response
        try:
            data = json.loads(text)
            content = data["choices"][0]["message"]["content"]
        except (KeyError, IndexError, json.JSONDecodeError) as e:
            logger.error(f"Failed to parse AI response: {text[:500]}")
            raise ValueError(f"Failed to parse AI response: {e}")

        # Extract JSON from content (AI might wrap in markdown)
        content = content.strip()
        # Remove markdown code fences if present
        if content.startswith("```"):
            lines = content.split("\n")
            # Remove first fence line
            lines = lines[1:]
            # Remove last fence line
            if lines and lines[-1].strip() == "```":
                lines = lines[:-1]
            content = "\n".join(lines)

        # Try to find JSON object
        json_match = re.search(r'\{.*\}', content, re.DOTALL)
        if json_match:
            content = json_match.group(0)

        try:
            result = json.loads(content)
        except json.JSONDecodeError as e:
            logger.error(f"AI response not valid JSON: {content[:500]}")
            raise ValueError(f"AI response is not valid JSON: {e}")

        # Validate required fields
        for field in ("strategy_name", "strategy_code", "description"):
            if field not in result:
                raise ValueError(f"AI response missing required field: {field}")

        return result

    # ── Code Validation ──
    @staticmethod
    def validate_code(strategy_code: str) -> Tuple[bool, str]:
        """Static security analysis. Returns (is_valid, reason)."""
        if not strategy_code or not strategy_code.strip():
            return False, "Empty strategy code"

        # Check for dangerous patterns
        for pattern in FORBIDDEN_PATTERNS:
            if re.search(pattern, strategy_code):
                return False, f"Dangerous pattern detected: {pattern}"

        # Check for BaseStrategy import
        if "BaseStrategy" not in strategy_code:
            return False, "Missing BaseStrategy import/inheritance"

        # Check for class definition
        if "class " not in strategy_code:
            return False, "No class definition found"

        # Check for required methods (event-driven or vectorized)
        has_on_tick = "on_tick" in strategy_code or "on_bar" in strategy_code
        has_vectorized = all(m in strategy_code for m in (
            "populate_indicators", "populate_entry_trend", "populate_exit_trend"
        ))
        if not has_on_tick and not has_vectorized:
            return False, "Missing on_tick/on_bar or populate_indicators/entry/exit methods"

        return True, ""

    # ── Sandbox Execution ──
    @staticmethod
    def execute_sandbox(strategy_code: str) -> type:
        """Execute strategy code in restricted sandbox. Returns the strategy class."""
        from xtquant.strategy.base import BaseStrategy
        from xtquant.strategy.vectorized import VectorizedStrategy
        from xtquant.core.data import Tick, Bar

        # Build restricted builtins
        import builtins as _builtins
        restricted_builtins = {}
        for name in SAFE_BUILTINS:
            if hasattr(_builtins, name):
                restricted_builtins[name] = getattr(_builtins, name)
        # Add custom import function
        restricted_builtins["__import__"] = _restricted_import

        restricted_globals = {
            "__builtins__": restricted_builtins,
            "__name__": "xtquant_ai_sandbox",
            "__doc__": None,
            "BaseStrategy": BaseStrategy,
            "VectorizedStrategy": VectorizedStrategy,
            "Tick": Tick,
            "Bar": Bar,
            "asyncio": asyncio,
            "logging": logging,
            "math": __import__("math"),
            "numpy": __import__("numpy"),
            "collections": __import__("collections"),
            "itertools": __import__("itertools"),
            "random": __import__("random"),
        }

        # Also add pandas since vectorized strategies need it
        try:
            restricted_globals["pd"] = __import__("pandas")
        except ImportError:
            pass
        try:
            restricted_globals["np"] = restricted_globals["numpy"]
        except Exception:
            pass

        # Execute in restricted namespace
        exec(strategy_code, restricted_globals)

        # Find the strategy class (subclass of BaseStrategy or VectorizedStrategy)
        strategy_class = None
        for name, obj in restricted_globals.items():
            if (isinstance(obj, type) and
                    not name.startswith("_")):
                if issubclass(obj, BaseStrategy) and obj is not BaseStrategy:
                    strategy_class = obj
                    break
                if issubclass(obj, VectorizedStrategy) and obj is not VectorizedStrategy:
                    strategy_class = obj
                    break

        if strategy_class is None:
            raise ValueError("No BaseStrategy or VectorizedStrategy subclass found in generated code")

        return strategy_class
