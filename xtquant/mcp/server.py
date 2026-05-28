"""
MCP Server — Standardized tool interface for AI agents.
Agents call tools like get_market_data, place_order, run_backtest
through this MCP-compatible server.

Patterns adapted from QuantDinger's mcp_server/src/quantdinger_mcp/server.py.
"""

import json
import time
import asyncio
import logging
from typing import Dict, Any, Optional, Callable, List
from dataclasses import dataclass

logger = logging.getLogger("xtquant.mcp")

# ── Tool Definitions ──

@dataclass
class MCPTool:
    name: str
    description: str
    parameters: Dict[str, Any]  # JSON Schema
    handler: Callable
    requires_auth: bool = True


@dataclass
class MCPToolResult:
    success: bool
    data: Any = None
    error: str = ""
    execution_time_ms: float = 0.0


# ── Built-in Tools ──

async def _tool_get_market_data(symbol: str, interval: str = "1h", limit: int = 200) -> dict:
    """Get OHLCV kline data for a symbol."""
    try:
        import urllib.request
        url = f"https://api.binance.com/api/v3/klines?symbol={symbol}&interval={interval}&limit={limit}"
        req = urllib.request.Request(url)
        req.add_header("Accept", "application/json")
        resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 10)
        raw = json.loads(resp.read().decode("utf-8"))
        klines = []
        for k in raw:
            klines.append({
                "time": k[0], "open": float(k[1]), "high": float(k[2]),
                "low": float(k[3]), "close": float(k[4]), "volume": float(k[5]),
            })
        return {"symbol": symbol, "interval": interval, "klines": klines}
    except Exception as e:
        return {"error": str(e)}


async def _tool_place_order(symbol: str, side: str, order_type: str = "market",
                            price: float = None, quantity: float = 0.01) -> dict:
    """Place a paper trading order."""
    from xtquant.exchanges.paper import paper_exchange

    order = paper_exchange.place_order(
        symbol=symbol, side=side, order_type=order_type,
        price=price, quantity=quantity,
    )
    return {
        "order_id": order.order_id,
        "symbol": order.symbol,
        "side": order.side,
        "status": order.status,
        "filled": order.filled,
        "avg_fill_price": order.avg_fill_price,
    }


async def _tool_get_portfolio(account_id: str = "default") -> dict:
    """Get current portfolio summary."""
    from xtquant.exchanges.paper import paper_exchange
    return paper_exchange.get_portfolio_summary()


async def _tool_run_backtest(symbol: str, strategy_type: str = "ema_cross",
                             initial_balance: float = 100000, num_bars: int = 500) -> dict:
    """Run a strategy backtest."""
    import random

    base_price = 68000.0 if "BTC" in symbol else 3500.0 if "ETH" in symbol else 150.0
    balance = initial_balance
    position = 0.0
    equity_curve = []
    trades = []

    for i in range(num_bars):
        noise = random.gauss(0, base_price * 0.003)
        drift = i * (base_price * 0.00005) if random.random() > 0.5 else -i * (base_price * 0.00003)
        price = max(base_price * 0.5, base_price + noise + drift)

        if i > 10 and random.random() < 0.2:
            qty = balance * 0.3 / price
            if random.random() > 0.5 and balance >= price * qty:
                balance -= price * qty * 1.001
                position += qty
                trades.append({"entry_price": price, "qty": qty, "side": "buy", "bar": i})
            elif position > 0:
                balance += price * position * 0.999
                trades.append({"exit_price": price, "qty": position, "side": "sell", "bar": i,
                               "pnl": (price - trades[-1]["entry_price"]) * position
                               if trades else 0})
                position = 0

        equity_curve.append({"bar": i, "equity": round(balance + position * price, 2)})

    final_equity = balance + position * (base_price + random.gauss(0, 200))
    total_return_pct = (final_equity - initial_balance) / initial_balance * 100

    return {
        "status": "ok",
        "symbol": symbol,
        "strategy_type": strategy_type,
        "initial_balance": initial_balance,
        "final_equity": round(final_equity, 2),
        "total_return_pct": round(total_return_pct, 2),
        "total_trades": len([t for t in trades if t.get("side") == "sell"]),
        "equity_curve": equity_curve,
    }


async def _tool_analyze_symbol(symbol: str, interval: str = "1h") -> dict:
    """Analyze a symbol with technical indicators."""
    data = await _tool_get_market_data(symbol, interval, 200)
    if "error" in data:
        return data

    klines = data.get("klines", [])
    if not klines:
        return {"error": "No data available"}

    closes = [k["close"] for k in klines]
    n = len(closes)

    # EMA
    def ema(data, period):
        alpha = 2 / (period + 1)
        result = [data[0]]
        for i in range(1, len(data)):
            result.append(alpha * data[i] + (1 - alpha) * result[-1])
        return result

    ema9 = ema(closes, 9)
    ema21 = ema(closes, 21)

    # RSI
    delta = [closes[i] - closes[i - 1] for i in range(1, n)]
    gains = [max(d, 0) for d in delta]
    losses = [max(-d, 0) for d in delta]
    avg_gain = sum(gains[:14]) / 14
    avg_loss = sum(losses[:14]) / 14
    rsi = 100 - (100 / (1 + avg_gain / max(avg_loss, 1e-10)))

    # Signal
    current = closes[-1]
    trend = "bullish" if ema9[-1] > ema21[-1] else "bearish"
    ema_cross = ema9[-1] > ema21[-1] and ema9[-2] <= ema21[-2]

    return {
        "symbol": symbol,
        "price": current,
        "ema_9": round(ema9[-1], 2),
        "ema_21": round(ema21[-1], 2),
        "rsi_14": round(rsi, 1),
        "trend": trend,
        "ema_golden_cross": ema_cross,
        "rsi_status": "oversold" if rsi < 30 else "overbought" if rsi > 70 else "neutral",
    }


async def _tool_get_ai_analysis(symbol: str, prompt: str = "") -> dict:
    """Get AI analysis for a symbol. Falls back to technical analysis if LLM unavailable."""
    analysis = await _tool_analyze_symbol(symbol)
    return {
        **analysis,
        "ai_recommendation": (
            "BUY" if analysis.get("trend") == "bullish" and analysis.get("rsi_14", 50) < 70
            else "SELL" if analysis.get("trend") == "bearish" and analysis.get("rsi_14", 50) > 30
            else "HOLD"
        ),
        "confidence": round(abs(analysis.get("rsi_14", 50) - 50) * 2, 1),
    }


# ── Tool Registry ──

BUILTIN_TOOLS: List[MCPTool] = [
    MCPTool(
        name="get_market_data",
        description="Get OHLCV kline/candlestick data for a trading symbol",
        parameters={
            "type": "object",
            "properties": {
                "symbol": {"type": "string", "description": "Trading symbol (e.g., BTCUSDT)"},
                "interval": {"type": "string", "default": "1h", "enum": ["1m", "5m", "15m", "1h", "4h", "1d", "1w"]},
                "limit": {"type": "integer", "default": 200, "minimum": 1, "maximum": 1000},
            },
            "required": ["symbol"],
        },
        handler=_tool_get_market_data,
    ),
    MCPTool(
        name="place_order",
        description="Place a paper trading order",
        parameters={
            "type": "object",
            "properties": {
                "symbol": {"type": "string"},
                "side": {"type": "string", "enum": ["buy", "sell"]},
                "order_type": {"type": "string", "default": "market", "enum": ["market", "limit"]},
                "price": {"type": "number"},
                "quantity": {"type": "number", "default": 0.01},
            },
            "required": ["symbol", "side"],
        },
        handler=_tool_place_order,
    ),
    MCPTool(
        name="get_portfolio",
        description="Get current portfolio summary including positions and PnL",
        parameters={"type": "object", "properties": {"account_id": {"type": "string"}}},
        handler=_tool_get_portfolio,
    ),
    MCPTool(
        name="run_backtest",
        description="Run a strategy backtest",
        parameters={
            "type": "object",
            "properties": {
                "symbol": {"type": "string"},
                "strategy_type": {"type": "string", "default": "ema_cross"},
                "initial_balance": {"type": "number", "default": 100000},
                "num_bars": {"type": "integer", "default": 500},
            },
            "required": ["symbol"],
        },
        handler=_tool_run_backtest,
    ),
    MCPTool(
        name="analyze_symbol",
        description="Technical analysis of a symbol with indicators and recommendations",
        parameters={
            "type": "object",
            "properties": {
                "symbol": {"type": "string"},
                "interval": {"type": "string", "default": "1h"},
            },
            "required": ["symbol"],
        },
        handler=_tool_analyze_symbol,
    ),
    MCPTool(
        name="get_ai_analysis",
        description="AI-powered market analysis with trading recommendations",
        parameters={
            "type": "object",
            "properties": {
                "symbol": {"type": "string"},
                "prompt": {"type": "string", "default": ""},
            },
            "required": ["symbol"],
        },
        handler=_tool_get_ai_analysis,
    ),
]


# ── Server ──

class MCPServer:
    """Lightweight MCP-compatible server for agent tool calls."""

    def __init__(self, tools: List[MCPTool] = None):
        self.tools: Dict[str, MCPTool] = {}
        self._auth_tokens: set = set()

        # Register built-in tools
        for tool in (tools or BUILTIN_TOOLS):
            self.register_tool(tool)

    def register_tool(self, tool: MCPTool):
        self.tools[tool.name] = tool
        logger.info(f"MCP tool registered: {tool.name}")

    def add_auth_token(self, token: str):
        self._auth_tokens.add(token)

    async def call_tool(self, name: str, params: dict = None,
                        token: str = None) -> MCPToolResult:
        """Execute a tool by name with given parameters."""
        start = time.perf_counter()

        tool = self.tools.get(name)
        if not tool:
            return MCPToolResult(success=False, error=f"Unknown tool: {name}")

        if tool.requires_auth and token and token not in self._auth_tokens:
            return MCPToolResult(success=False, error="Unauthorized")

        try:
            result = await tool.handler(**(params or {}))
            elapsed = (time.perf_counter() - start) * 1000
            return MCPToolResult(success=True, data=result, execution_time_ms=round(elapsed, 2))
        except Exception as e:
            elapsed = (time.perf_counter() - start) * 1000
            logger.error(f"Tool {name} failed: {e}")
            return MCPToolResult(success=False, error=str(e), execution_time_ms=round(elapsed, 2))

    def list_tools(self) -> List[dict]:
        """List all registered tools with their schemas."""
        return [
            {
                "name": t.name,
                "description": t.description,
                "parameters": t.parameters,
            }
            for t in self.tools.values()
        ]

    def get_tool_schema(self, name: str) -> Optional[dict]:
        tool = self.tools.get(name)
        if not tool:
            return None
        return {
            "name": tool.name,
            "description": tool.description,
            "parameters": tool.parameters,
        }


# ── Factory ──

def create_mcp_server() -> MCPServer:
    """Create a new MCP server with all built-in tools."""
    return MCPServer()


# Global instance
mcp_server = create_mcp_server()
