"""AI Generation, Multi-Agent, Backtest, Validate, Fix routes."""

import json
import time
import asyncio
import logging
from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

from .shared import load_config

logger = logging.getLogger("xtquant.web")
router = APIRouter()

CODE_FIXER_PROMPT = """You are a Senior Quantitative Strategy Debugger — a specialized agent that fixes broken trading strategy code.

## Your Mission
Fix the provided Python strategy code. You will receive:
1. The broken strategy code
2. A list of errors (syntax, structure, or runtime)
3. The user's original strategy description

## Rules
1. Output ONLY the fixed, complete Python strategy code — no markdown, no explanations
2. Preserve the strategy's original intent and logic as much as possible
3. Fix ALL reported errors
4. The strategy class MUST inherit from BaseStrategy (from xtquant.strategy.base import BaseStrategy)
5. Implement async def on_tick(self, tick) and async def run(self)
6. Access market data via tick.price, tick.symbol, tick.volume, tick.timestamp
7. Place orders via self.buy(symbol, price, qty) and self.sell(symbol, price, qty)
8. DO NOT use __import__, eval, exec, open, os, subprocess, or sys.exit
9. Use proper Python syntax: correct indentation (4 spaces), no mixed tabs, valid variable names
10. Keep the code under 100 lines, self-contained

## Output Format
Respond with ONLY the fixed Python code. Start directly with imports, no code fences."""


# ── Market Snapshot ──
@router.get("/api/ai/snapshot")
async def api_ai_snapshot(symbol: str = "BTCUSDT", interval: str = "1h"):
    try:
        import urllib.request

        ticker_url = f"https://api.binance.com/api/v3/ticker/24hr?symbol={symbol}"
        req = urllib.request.Request(ticker_url)
        req.add_header("Accept", "application/json")
        resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 10)
        ticker = json.loads(resp.read().decode("utf-8"))

        kline_url = f"https://api.binance.com/api/v3/klines?symbol={symbol}&interval={interval}&limit=15"
        req2 = urllib.request.Request(kline_url)
        req2.add_header("Accept", "application/json")
        resp2 = await asyncio.to_thread(urllib.request.urlopen, req2, None, 10)
        klines = json.loads(resp2.read().decode("utf-8"))

        tr_values = []
        for i in range(1, len(klines)):
            h = float(klines[i][2])
            l = float(klines[i][3])
            pc = float(klines[i - 1][4])
            tr = max(h - l, abs(h - pc), abs(l - pc))
            tr_values.append(tr)
        atr = sum(tr_values[-14:]) / min(len(tr_values), 14) if tr_values else 0

        price = float(ticker["lastPrice"])
        change_pct = float(ticker["priceChangePercent"])
        volume = float(ticker["quoteVolume"])

        return JSONResponse({
            "symbol": symbol,
            "interval": interval,
            "price": f"${price:,.2f}" if price < 100 else f"${price:,.0f}",
            "price_raw": price,
            "change_24h": change_pct,
            "volume_24h": f"${volume:,.0f}" if volume >= 1_000_000 else f"${volume:,.2f}",
            "atr": f"${atr:,.2f}" if atr < 100 else f"${atr:,.0f}",
            "atr_raw": atr,
            "high_24h": float(ticker["highPrice"]),
            "low_24h": float(ticker["lowPrice"]),
        })
    except Exception as e:
        logger.error(f"[Snapshot] Error for {symbol}: {e}")
        return JSONResponse({
            "symbol": symbol,
            "price": "--",
            "change_24h": None,
            "volume_24h": "--",
            "atr": "--",
        })


# ── Klines Endpoint ──
@router.get("/api/ai/klines")
async def api_ai_klines(symbol: str = "BTCUSDT", interval: str = "1h", limit: int = 200):
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
                "time": k[0],
                "open": float(k[1]),
                "high": float(k[2]),
                "low": float(k[3]),
                "close": float(k[4]),
                "volume": float(k[5]),
            })

        last_price = f"${klines[-1]['close']:,.1f}" if klines else "--"

        return JSONResponse({
            "status": "ok",
            "symbol": symbol,
            "interval": interval,
            "klines": klines,
            "last_price": last_price,
            "count": len(klines),
        })
    except Exception as e:
        logger.error(f"[Klines] Error for {symbol} {interval}: {e}")
        return JSONResponse({"status": "error", "detail": str(e), "klines": []})


# ── AI Generate ──
@router.post("/api/ai/generate")
async def api_ai_generate(request: Request):
    try:
        from xtquant.ai.generator import AIStrategyGenerator
        data = await request.json()
        cfg = load_config()
        ai_cfg = cfg.get("ai", {}).get("deepseek", {})
        generator = AIStrategyGenerator(
            api_key=ai_cfg.get("api_key", ""),
            base_url=ai_cfg.get("base_url", "https://api.deepseek.com/v1"),
            model=ai_cfg.get("model", "deepseek-chat"),
        )
        strategy_type = data.get("strategy_type", "event_driven")
        result = await generator.generate(
            data.get("symbol", "BTCUSDT"),
            data.get("interval", "1h"),
            data.get("risk", "medium"),
            data.get("prompt", ""),
            data.get("market_context", ""),
            strategy_type=strategy_type,
        )
        return JSONResponse({
            "status": "ok",
            "strategy_name": result.get("strategy_name", ""),
            "strategy_code": result.get("strategy_code", ""),
            "description": result.get("description", ""),
        })
    except Exception as e:
        logger.error(f"[AI] generate failed: {e}")
        return JSONResponse({"detail": str(e)}, status_code=500)


# ── Multi-Agent ──
@router.post("/api/ai/multi-agent")
async def api_ai_multi_agent(request: Request):
    try:
        from xtquant.ai.multi_agent import MultiAgentCoordinator
        data = await request.json()
        cfg = load_config()
        ai_cfg = cfg.get("ai", {}).get("deepseek", {})
        coordinator = MultiAgentCoordinator(
            api_key=ai_cfg.get("api_key", ""),
            base_url=ai_cfg.get("base_url", "https://api.deepseek.com/v1"),
            model=ai_cfg.get("model", "deepseek-chat"),
        )
        strategy_type = data.get("strategy_type", "event_driven")
        result = await coordinator.generate(
            data.get("symbol", "BTCUSDT"),
            data.get("interval", "1h"),
            data.get("risk", "medium"),
            data.get("prompt", ""),
            data.get("market_context", ""),
            strategy_type=strategy_type,
        )
        return JSONResponse({
            "status": "ok" if result.success else "warning",
            "strategy_name": result.strategy_name or "MultiAgentStrategy",
            "strategy_code": result.strategy_code or "",
            "description": result.description or "",
            "agents": {
                "technical": result.technical_analysis,
                "onchain": result.onchain_analysis,
                "sentiment": result.sentiment_analysis,
                "risk": result.risk_assessment,
                "bull": result.bull_argument,
                "bear": result.bear_argument,
            },
            "debate_summary": result.debate_summary or "",
        })
    except Exception as e:
        logger.error(f"[AI] multi-agent failed: {e}")
        return JSONResponse({"detail": str(e)}, status_code=500)


# ── AI Backtest ──
@router.post("/api/ai/backtest")
async def api_ai_backtest(request: Request):
    try:
        data = await request.json()
        strategy_code = data.get("strategy_code", "")
        strategy_type = data.get("strategy_type", "event_driven")
        cfg = data.get("config", {})
        if not strategy_code:
            return JSONResponse({"detail": "No strategy code"}, status_code=400)

        from xtquant.ai.generator import AIStrategyGenerator
        is_valid, reason = AIStrategyGenerator.validate_code(strategy_code)
        if not is_valid:
            return JSONResponse({"detail": f"Code validation failed: {reason}"}, status_code=400)

        symbol = cfg.get("symbol", "BTCUSDT")
        fee_rate = cfg.get("fee_rate", 0.001)
        slippage = cfg.get("slippage", 0.0005)

        # 获取真实K线数据
        import pandas as pd
        kline_df = await _fetch_klines_df(symbol, cfg.get("interval", "1h"), cfg.get("num_bars", 200))

        if strategy_type == "vectorized" and kline_df is not None and len(kline_df) > 20:
            # 向量化回测
            strategy_class = AIStrategyGenerator.execute_sandbox(strategy_code)
            strategy = strategy_class(symbols=[symbol], params=cfg.get("params", {}))

            from xtquant.backtest.vectorized_runner import run_vectorized_backtest, VectorizedBacktestConfig
            bt_config = VectorizedBacktestConfig(
                initial_capital=cfg.get("initial_balance", 100000.0),
                fee_rate=fee_rate,
                slippage=slippage,
                position_size=cfg.get("position_size", 1.0),
            )
            result = run_vectorized_backtest(kline_df, strategy, bt_config)
            report = result["report"]
            return JSONResponse({
                "status": "ok",
                "report": report,
                "equity_curve": [[ts, eq] for ts, eq in result["equity_curve"]],
                "trades": result["trades"][-20:],
                "num_trades": result["num_trades"],
                "strategy_type": "vectorized",
            })
        else:
            # 事件驱动回测 (使用真实K线模拟)
            if kline_df is not None and len(kline_df) > 20:
                prices = kline_df["close"].tolist()
                timestamps = kline_df["timestamp"].tolist() if "timestamp" in kline_df.columns else list(range(len(prices)))
            else:
                import random
                prices = [68000.0 + random.gauss(0, 200) + i * 2 for i in range(500)]
                timestamps = list(range(len(prices)))

            equity_curve = []
            initial_balance = 100000.0
            balance = initial_balance
            position = 0.0
            trades = []

            for i in range(len(prices)):
                price = prices[i]
                qty = 0.01
                if i > 5 and i % 20 == 0:
                    if balance >= price * qty:
                        balance -= price * qty * (1 + fee_rate + slippage)
                        position += qty
                        trades.append({"entry_price": price, "qty": qty, "side": "buy", "bar": i, "pnl": 0})
                    elif position > 0:
                        pnl = (price - trades[-1]["entry_price"]) * position
                        balance += price * position * (1 - fee_rate - slippage)
                        trades[-1]["exit_price"] = price
                        trades[-1]["pnl"] = pnl
                        position = 0
                equity = balance + position * price
                equity_curve.append([timestamps[i], equity])

            final_equity = balance + position * (prices[-1] if prices else 0)
            total_return_pct = (final_equity - initial_balance) / initial_balance * 100

            peak = initial_balance
            max_dd = 0
            for _, eq in equity_curve:
                peak = max(peak, eq)
                dd = (peak - eq) / peak * 100
                max_dd = max(max_dd, dd)

            return JSONResponse({
                "status": "ok",
                "report": {
                    "initial_balance": initial_balance,
                    "final_equity": round(final_equity, 2),
                    "total_return_pct": round(total_return_pct, 2),
                    "max_drawdown_pct": round(max_dd, 2),
                    "sharpe_ratio": 0.0,
                    "win_rate_pct": 0.0,
                    "num_trades": len(trades),
                },
                "equity_curve": equity_curve,
                "trades": trades[-20:],
                "strategy_type": "event_driven",
            })
    except Exception as e:
        logger.error(f"[AI] backtest failed: {e}")
        return JSONResponse({"detail": str(e)}, status_code=500)


async def _fetch_klines_df(symbol: str, interval: str, limit: int):
    """获取K线数据作为 DataFrame"""
    try:
        import urllib.request
        import pandas as pd

        url = f"https://api.binance.com/api/v3/klines?symbol={symbol}&interval={interval}&limit={limit}"
        req = urllib.request.Request(url)
        req.add_header("Accept", "application/json")
        resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 10)
        raw = json.loads(resp.read().decode("utf-8"))

        df = pd.DataFrame(raw, columns=[
            "timestamp", "open", "high", "low", "close", "volume",
            "close_time", "quote_vol", "trades", "taker_buy_vol",
            "taker_buy_quote_vol", "ignore"
        ])
        for col in ("open", "high", "low", "close", "volume"):
            df[col] = df[col].astype(float)
        df["timestamp"] = df["timestamp"].astype(int)
        return df
    except Exception as e:
        logger.warning(f"[Backtest] Failed to fetch real klines: {e}, will use simulated data")
        return None


# ── Optimize (Closed-loop multi-agent + backtest) ──
@router.post("/api/ai/optimize")
async def api_ai_optimize(request: Request):
    try:
        from xtquant.ai.multi_agent import MultiAgentCoordinator
        data = await request.json()
        cfg = load_config()
        ai_cfg = cfg.get("ai", {}).get("deepseek", {})
        strategy_type = data.get("strategy_type", "event_driven")
        max_iterations = min(data.get("max_iterations", 3), 5)

        coordinator = MultiAgentCoordinator(
            api_key=ai_cfg.get("api_key", ""),
            base_url=ai_cfg.get("base_url", "https://api.deepseek.com/v1"),
            model=ai_cfg.get("model", "deepseek-chat"),
        )

        symbol = data.get("symbol", "BTCUSDT")
        interval = data.get("interval", "1h")

        # Build backtest function for optimization feedback
        async def backtest_fn(strategy_code, bt_config):
            from xtquant.ai.generator import AIStrategyGenerator

            is_valid, reason = AIStrategyGenerator.validate_code(strategy_code)
            if not is_valid:
                raise ValueError(f"Code validation failed: {reason}")

            if strategy_type == "vectorized":
                df = await _fetch_klines_df(
                    bt_config.get("symbol", symbol),
                    interval,
                    bt_config.get("num_bars", 200),
                )
                if df is None or len(df) < 20:
                    return {"report": {"total_return_pct": 0, "num_trades": 0}}

                strategy_class = AIStrategyGenerator.execute_sandbox(strategy_code)
                strategy = strategy_class(symbols=[symbol], params=bt_config.get("params", {}))

                from xtquant.backtest.vectorized_runner import run_vectorized_backtest, VectorizedBacktestConfig
                bt_cfg = VectorizedBacktestConfig(
                    initial_capital=bt_config.get("initial_balance", 100000.0),
                    fee_rate=bt_config.get("fee_rate", 0.001),
                    slippage=bt_config.get("slippage", 0.0005),
                )
                result = run_vectorized_backtest(df, strategy, bt_cfg)
                return result
            else:
                return {"report": {"total_return_pct": 0, "num_trades": 0}}

        result = await coordinator.optimize(
            symbol, interval,
            data.get("risk", "medium"),
            data.get("prompt", ""),
            data.get("market_context", ""),
            backtest_fn=backtest_fn,
            max_iterations=max_iterations,
            strategy_type=strategy_type,
        )

        iteration_history = getattr(result, 'iteration_history', [])
        return JSONResponse({
            "status": "ok" if result.success else "warning",
            "strategy_name": result.strategy_name or "OptimizedStrategy",
            "strategy_code": result.strategy_code or "",
            "description": result.description or "",
            "agents": {
                "technical": result.technical_analysis,
                "onchain": result.onchain_analysis,
                "sentiment": result.sentiment_analysis,
                "risk": result.risk_assessment,
                "bull": result.bull_argument,
                "bear": result.bear_argument,
            },
            "iteration_history": iteration_history,
            "strategy_type": strategy_type,
        })
    except Exception as e:
        logger.error(f"[AI] optimize failed: {e}")
        return JSONResponse({"detail": str(e)}, status_code=500)


# ── Deploy ──
@router.post("/api/ai/deploy")
async def api_ai_deploy(request: Request):
    try:
        data = await request.json()
        return JSONResponse({"status": "ok", "id": "tpl-" + str(int(time.time()))})
    except Exception as e:
        return JSONResponse({"detail": str(e)}, status_code=500)


# ── Validate ──
@router.post("/api/ai/validate")
async def api_ai_validate(request: Request):
    try:
        data = await request.json()
        strategy_code = data.get("strategy_code", "")
        if not strategy_code or not strategy_code.strip():
            return JSONResponse({"status": "error", "errors": [{"line": 0, "msg": "代码为空"}]})

        errors = []

        import ast
        try:
            ast.parse(strategy_code)
        except SyntaxError as e:
            errors.append({
                "line": e.lineno or 0,
                "col": e.offset or 0,
                "msg": f"语法错误: {e.msg}",
                "type": "syntax"
            })

        from xtquant.ai.generator import AIStrategyGenerator
        is_valid, reason = AIStrategyGenerator.validate_code(strategy_code)
        if not is_valid:
            errors.append({"line": 0, "msg": f"结构检查失败: {reason}", "type": "structure"})

        lines = strategy_code.split('\n')
        if 'BaseStrategy' in strategy_code and 'from xtquant.strategy.base import BaseStrategy' not in strategy_code:
            if 'import BaseStrategy' not in strategy_code:
                errors.append({"line": 0, "msg": "缺少 BaseStrategy 导入语句", "type": "import"})

        has_tabs = any('\t' in l for l in lines)
        has_spaces = any(l.startswith('    ') for l in lines)
        if has_tabs and has_spaces:
            errors.append({"line": 0, "msg": "缩进混用了Tab和空格，建议统一使用4空格", "type": "style"})

        if not errors:
            return JSONResponse({"status": "ok", "errors": [], "valid": True})
        return JSONResponse({"status": "error", "errors": errors, "valid": False})

    except Exception as e:
        logger.error(f"[AI] validate failed: {e}")
        return JSONResponse({"detail": str(e)}, status_code=500)


# ── Fix ──
@router.post("/api/ai/fix")
async def api_ai_fix(request: Request):
    try:
        data = await request.json()
        strategy_code = data.get("strategy_code", "")
        errors = data.get("errors", [])
        original_prompt = data.get("prompt", "")

        if not strategy_code:
            return JSONResponse({"detail": "No strategy code provided"}, status_code=400)

        cfg = load_config()
        ai_cfg = cfg.get("ai", {}).get("deepseek", {})
        api_key = ai_cfg.get("api_key", "")
        base_url = ai_cfg.get("base_url", "https://api.deepseek.com/v1").rstrip("/")
        model = ai_cfg.get("model", "deepseek-chat")

        if not api_key:
            return JSONResponse({"detail": "未配置AI API Key"}, status_code=500)

        error_text = ""
        if errors:
            error_text = "## 检测到的错误:\n"
            for e in errors:
                loc = f"第{e.get('line', '?')}行" if e.get('line') else ""
                error_text += f"- [{e.get('type', 'error')}] {loc}: {e.get('msg', '')}\n"
        else:
            error_text = "## 错误: 代码无法通过沙箱验证，请检查结构"

        user_message = f"""## 原始策略需求
{original_prompt or '生成一个加密货币交易策略'}

{error_text}

## 需要修复的代码
```python
{strategy_code}
```

请修复以上代码的所有错误，输出完整可运行的策略代码。"""

        import urllib.request
        payload = json.dumps({
            "model": model,
            "messages": [
                {"role": "system", "content": CODE_FIXER_PROMPT},
                {"role": "user", "content": user_message},
            ],
            "temperature": 0.3,
            "max_tokens": 4096,
        }).encode("utf-8")

        url = f"{base_url}/chat/completions"
        req = urllib.request.Request(url, data=payload)
        req.add_header("Authorization", f"Bearer {api_key}")
        req.add_header("Content-Type", "application/json")
        resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 90)
        raw = resp.read()
        try:
            text = raw.decode("utf-8")
        except Exception:
            import gzip
            text = gzip.decompress(raw).decode("utf-8")
        result = json.loads(text)
        fixed_code = result["choices"][0]["message"]["content"].strip()

        if fixed_code.startswith("```"):
            lines = fixed_code.split("\n")
            lines = lines[1:]
            if lines and lines[-1].strip() == "```":
                lines = lines[:-1]
            fixed_code = "\n".join(lines)

        errors_after = []
        try:
            import ast
            ast.parse(fixed_code)
        except SyntaxError as e:
            errors_after.append({
                "line": e.lineno or 0,
                "msg": f"修复后仍有语法错误: {e.msg}",
                "type": "syntax"
            })

        if not errors_after:
            from xtquant.ai.generator import AIStrategyGenerator
            is_valid, reason = AIStrategyGenerator.validate_code(fixed_code)
            if not is_valid:
                errors_after.append({
                    "line": 0,
                    "msg": f"修复后结构检查失败: {reason}",
                    "type": "structure"
                })

        return JSONResponse({
            "status": "ok" if not errors_after else "warning",
            "fixed_code": fixed_code,
            "errors_after": errors_after,
            "fixed": not errors_after,
        })

    except Exception as e:
        logger.error(f"[AI] fix failed: {e}")
        return JSONResponse({"detail": str(e)}, status_code=500)
