"""Dashboard API — enriched summary with charts, positions, strategy stats."""

import json
import time
import math
import logging
from collections import defaultdict

from fastapi import APIRouter, Request, Query
from fastapi.responses import JSONResponse
from .shared import get_engine

logger = logging.getLogger("xtquant.web.dashboard")
router = APIRouter()


@router.get("/api/dashboard/summary")
async def api_dashboard_summary(request: Request):
    """Return enriched dashboard data: KPIs, daily PnL, strategy stats,
    hourly distribution, calendar data, positions, recent trades.
    """
    try:
        engine = get_engine()
        if engine is None:
            return _empty_response("Engine not available")

        # ── Basic stats from engine ──
        stats = engine.get_stats()
        portfolio = stats.get("portfolio", {}) if isinstance(stats, dict) else {}

        total_equity = float(portfolio.get("total_equity", 0))
        available_balance = float(portfolio.get("available_balance", 0))
        total_return_pct = float(portfolio.get("total_return_pct", 0))
        max_drawdown_pct = float(portfolio.get("max_drawdown_pct", 0))
        sharpe = float(portfolio.get("sharpe_ratio", 0))
        win_rate = float(portfolio.get("win_rate_pct", 0))
        total_trades = int(portfolio.get("total_trades", 0))

        # ── Positions (from portfolio) ──
        positions = portfolio.get("positions", [])
        if isinstance(positions, dict):
            positions = list(positions.values())

        # ── Recent trades (from OMS / backtest engine) ──
        recent_trades = _get_recent_trades(engine)

        # ── Strategy stats ──
        strategy_stats = _get_strategy_stats(engine)

        # ── Daily PnL from equity curve ──
        equity_curve = stats.get("equity_curve", [])
        daily_pnl = _compute_daily_pnl(equity_curve) if equity_curve else _compute_daily_pnl_from_trades(recent_trades)

        # ── Hourly distribution ──
        hourly_dist = _compute_hourly_distribution(recent_trades)

        # ── Monthly calendar ──
        calendar_months = _compute_calendar_data(daily_pnl)

        # ── Monthly returns ──
        monthly_returns = _compute_monthly_returns(daily_pnl)

        # ── Best/worst day ──
        best_day, worst_day = 0.0, 0.0
        if daily_pnl:
            profits = [d["profit"] for d in daily_pnl]
            best_day = round(max(profits), 2)
            worst_day = round(min(profits), 2)

        # ── Strategy PnL pie ──
        strategy_pnl_pie = _compute_strategy_pie(strategy_stats)

        return JSONResponse({
            "status": "ok",
            "data": {
                # KPIs
                "total_equity": round(total_equity, 2),
                "available_balance": round(available_balance, 2),
                "total_return_pct": round(total_return_pct, 2),
                "max_drawdown_pct": round(max_drawdown_pct, 2),
                "sharpe_ratio": round(sharpe, 3),
                "win_rate_pct": round(win_rate, 1),
                "total_trades": total_trades,

                # Charts
                "daily_pnl": daily_pnl[-30:],  # Last 30 days
                "hourly_distribution": hourly_dist,
                "monthly_returns": monthly_returns[-12:],  # Last 12 months
                "calendar_months": calendar_months[-6:],  # Last 6 months
                "strategy_pnl_pie": strategy_pnl_pie,

                # Details
                "best_day": best_day,
                "worst_day": worst_day,
                "positions": positions,
                "recent_trades": recent_trades[-20:],
                "strategy_stats": strategy_stats,
            }
        })
    except Exception as e:
        logger.error(f"Dashboard summary failed: {e}", exc_info=True)
        return _empty_response(str(e))


def _empty_response(error: str = ""):
    return JSONResponse({
        "status": "error",
        "detail": error,
        "data": {
            "total_equity": 0, "available_balance": 0, "total_return_pct": 0,
            "max_drawdown_pct": 0, "sharpe_ratio": 0, "win_rate_pct": 0,
            "total_trades": 0, "daily_pnl": [], "hourly_distribution": [],
            "monthly_returns": [], "calendar_months": [], "strategy_pnl_pie": [],
            "best_day": 0, "worst_day": 0, "positions": [], "recent_trades": [],
            "strategy_stats": [],
        }
    })


def _get_recent_trades(engine) -> list:
    """Extract recent trades from OMS or backtest engine."""
    trades = []
    try:
        if engine.oms:
            orders = engine.oms.get_recent_orders(limit=500) if hasattr(engine.oms, 'get_recent_orders') else []
            for o in orders:
                trades.append({
                    "time": getattr(o, 'created_at', 0) or int(time.time()),
                    "symbol": getattr(o, 'symbol', ''),
                    "side": getattr(o, 'side', ''),
                    "price": getattr(o, 'price', 0),
                    "quantity": getattr(o, 'quantity', 0),
                    "pnl": getattr(o, 'pnl', None) or 0,
                })
    except Exception:
        pass

    # Fallback: try backtest trades
    if not trades:
        for exch in engine.exchanges.values():
            if hasattr(exch, 'trades'):
                for t in list(exch.trades)[-500:]:
                    trades.append({
                        "time": t.get("timestamp", int(time.time())),
                        "symbol": t.get("symbol", ""),
                        "side": t.get("side", ""),
                        "price": t.get("price", 0),
                        "quantity": t.get("qty", t.get("quantity", 0)),
                        "pnl": t.get("pnl", None) or 0,
                    })
    return trades


def _get_strategy_stats(engine) -> list:
    """Per-strategy statistics."""
    result = []
    try:
        for name, st in engine.strategies.items():
            result.append({
                "name": name,
                "running": getattr(st, '_running', False),
                "symbols": getattr(st, 'symbols', []),
                "trade_count": getattr(st, 'trade_count', 0),
                "pnl": round(getattr(st, 'pnl', 0) or 0, 2),
            })
    except Exception:
        pass
    return result


def _compute_daily_pnl(equity_curve: list) -> list:
    """Aggregate equity curve into daily PnL."""
    if not equity_curve or len(equity_curve) < 2:
        return []

    day_pnl = defaultdict(float)
    for ts, eq in equity_curve:
        if isinstance(ts, (int, float)) and ts > 10000000000:
            ts = ts / 1000
        day = time.strftime("%Y-%m-%d", time.localtime(ts))
        day_pnl[day] = eq  # Last equity value of each day

    # Convert to daily changes
    days = sorted(day_pnl.keys())
    if len(days) < 2:
        return []

    result = []
    prev_eq = day_pnl[days[0]]
    for i in range(1, len(days)):
        curr_eq = day_pnl[days[i]]
        result.append({"date": days[i], "profit": round(curr_eq - prev_eq, 2)})
        prev_eq = curr_eq

    return result


def _compute_daily_pnl_from_trades(trades: list) -> list:
    """Fallback: daily PnL from trades."""
    if not trades:
        return []
    day_pnl = defaultdict(float)
    for t in trades:
        ts = t.get("time", 0)
        if isinstance(ts, (int, float)) and ts > 10000000000:
            ts = ts / 1000
        if ts <= 0:
            ts = time.time()
        day = time.strftime("%Y-%m-%d", time.localtime(ts))
        day_pnl[day] += float(t.get("pnl", 0) or 0)

    return [{"date": d, "profit": round(v, 2)}
            for d, v in sorted(day_pnl.items())]


def _compute_hourly_distribution(trades: list) -> list:
    """Trade count and profit by hour (0-23)."""
    hours = [{"hour": h, "count": 0, "profit": 0.0} for h in range(24)]
    for t in trades:
        ts = t.get("time", 0)
        if isinstance(ts, (int, float)) and ts > 10000000000:
            ts = ts / 1000
        if ts <= 0:
            continue
        h = int(time.strftime("%H", time.localtime(ts)))
        hours[h]["count"] += 1
        hours[h]["profit"] += float(t.get("pnl", 0) or 0)
    for h in hours:
        h["profit"] = round(h["profit"], 2)
    return hours


def _compute_calendar_data(daily_pnl: list) -> list:
    """Monthly calendar view with daily PnL."""
    if not daily_pnl:
        return []

    import calendar as cal
    from datetime import datetime

    month_data = defaultdict(lambda: {"days": {}, "total": 0.0, "win_days": 0, "lose_days": 0})

    for entry in daily_pnl:
        try:
            dt = datetime.strptime(entry["date"], "%Y-%m-%d")
            mk = dt.strftime("%Y-%m")
            day = dt.strftime("%d")
            p = entry["profit"]
            month_data[mk]["days"][day] = round(p, 2)
            month_data[mk]["total"] = round(month_data[mk]["total"] + p, 2)
            if p > 0:
                month_data[mk]["win_days"] += 1
            elif p < 0:
                month_data[mk]["lose_days"] += 1
        except Exception:
            pass

    result = []
    for mk in sorted(month_data.keys(), reverse=True):
        md = month_data[mk]
        y, m = int(mk[:4]), int(mk[5:7])
        _, days_in_month = cal.monthrange(y, m)
        first_wd = cal.monthrange(y, m)[0]
        result.append({
            "month_key": mk,
            "year": y,
            "month": m,
            "days_in_month": days_in_month,
            "first_weekday": first_wd,
            "days": md["days"],
            "total": md["total"],
            "win_days": md["win_days"],
            "lose_days": md["lose_days"],
        })
    return result


def _compute_monthly_returns(daily_pnl: list) -> list:
    """Aggregate daily PnL into monthly returns."""
    month_totals = defaultdict(float)
    for entry in daily_pnl:
        mk = entry["date"][:7]  # "YYYY-MM"
        month_totals[mk] += entry["profit"]

    return [{"month": k, "profit": round(v, 2)}
            for k, v in sorted(month_totals.items())]


def _compute_strategy_pie(strategy_stats: list) -> list:
    """Strategy contribution to total PnL (pie chart data)."""
    result = []
    for s in strategy_stats:
        result.append({
            "name": s.get("name", ""),
            "value": abs(s.get("pnl", 0)),
            "pnl": s.get("pnl", 0),
        })
    result.sort(key=lambda x: x["value"], reverse=True)
    return result[:8]  # Top 8
