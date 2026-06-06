#!/usr/bin/env python3
"""
XiaoTianQuant Data Sync — Download real OHLCV from Binance via VPN proxy.
Writes directly to gateway.db so Go backend can read it immediately.
"""
import os
import sys
import json
import time
import sqlite3
from datetime import datetime
from urllib import request, error

# ── Configuration ─────────────────────────────────────────────
DB_PATH = r"C:\Users\20545\Desktop\xiaotian_quant\gateway\gateway.db"
PROXY_URL = "http://127.0.0.1:7897"  # Your VPN proxy

# Common symbols to sync
DEFAULT_SYMBOLS = ["BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT"]
DEFAULT_INTERVALS = ["1h", "4h", "1d"]

# Binance API limits
BINANCE_MAX_CANDLES = 1000
RATE_LIMIT_SLEEP = 0.5  # seconds between requests


def create_proxy_handler():
    """Create urllib opener with proxy support."""
    proxy = request.ProxyHandler({
        "http": PROXY_URL,
        "https": PROXY_URL,
    })
    opener = request.build_opener(proxy, request.HTTPHandler)
    return opener


def fetch_binance_klines(symbol, interval, start_ms, end_ms, opener):
    """Fetch klines from Binance API via proxy."""
    url = (
        f"https://api.binance.com/api/v3/klines"
        f"?symbol={symbol}&interval={interval}&limit={BINANCE_MAX_CANDLES}"
    )
    if start_ms > 0:
        url += f"&startTime={start_ms}"
    if end_ms > 0:
        url += f"&endTime={end_ms}"

    req = request.Request(url, headers={"Accept": "application/json"})
    try:
        resp = opener.open(req, timeout=30)
        data = json.loads(resp.read().decode())
        return data
    except error.HTTPError as e:
        print(f"  HTTP {e.code}: {e.read().decode()[:200]}")
        return []
    except Exception as e:
        print(f"  Error: {e}")
        return []


def parse_klines(raw_data, symbol, interval):
    """Convert Binance kline format to OHLCV tuples."""
    bars = []
    for k in raw_data:
        if len(k) < 6:
            continue
        # Binance format: [open_time, open, high, low, close, volume, ...]
        bars.append((
            symbol,
            interval,
            float(k[1]),   # open
            float(k[2]),   # high
            float(k[3]),   # low
            float(k[4]),   # close
            float(k[5]),   # volume
            int(k[0]),     # timestamp (open_time)
        ))
    return bars


def save_to_db(bars, db_path):
    """Insert or replace bars into gateway.db."""
    if not bars:
        return 0

    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()

    # Use INSERT OR REPLACE to handle duplicates
    cursor.executemany(
        """INSERT OR REPLACE INTO market_bars
           (symbol, interval, open, high, low, close, volume, timestamp)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?)""",
        bars
    )
    conn.commit()
    count = cursor.rowcount
    conn.close()
    return count


def get_db_coverage(symbol, interval, db_path):
    """Check what data we already have in DB."""
    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()
    cursor.execute(
        "SELECT MIN(timestamp), MAX(timestamp), COUNT(*) FROM market_bars WHERE symbol=? AND interval=?",
        (symbol, interval)
    )
    row = cursor.fetchone()
    conn.close()
    if row and row[0]:
        return {"min": row[0], "max": row[1], "count": row[2]}
    return None


def sync_symbol_interval(symbol, interval, days_back, opener, db_path):
    """Sync historical data for one symbol/interval pair."""
    print(f"\n[DATA] {symbol} {interval} (last {days_back} days)")

    # Check existing coverage
    coverage = get_db_coverage(symbol, interval, db_path)
    if coverage:
        print(f"  DB has {coverage['count']} bars from {ts_to_str(coverage['min'])} to {ts_to_str(coverage['max'])}")

    # Calculate time range
    end_ms = int(time.time() * 1000)
    start_ms = end_ms - days_back * 24 * 3600 * 1000

    # If we have data, only fetch what's missing
    if coverage and coverage["max"] and coverage["max"] > start_ms:
        start_ms = coverage["max"] + 1  # Start from after last bar
        print(f"  Incremental: fetching from {ts_to_str(start_ms)}")

    if start_ms >= end_ms:
        print(f"  [OK] Already up to date")
        return 0

    all_bars = []
    current_from = start_ms
    total_fetched = 0

    while current_from < end_ms:
        batch = fetch_binance_klines(symbol, interval, current_from, end_ms, opener)
        if not batch:
            break

        bars = parse_klines(batch, symbol, interval)
        if not bars:
            break

        all_bars.extend(bars)
        total_fetched += len(bars)

        # Update cursor to after last bar
        last_ts = batch[-1][0]
        if last_ts <= current_from:
            break  # No progress, avoid infinite loop
        current_from = last_ts + 1

        print(f"  Fetched {len(bars)} bars (total: {total_fetched})")
        time.sleep(RATE_LIMIT_SLEEP)

    if all_bars:
        saved = save_to_db(all_bars, db_path)
        print(f"  [SAVED] {saved} bars to database")
        return saved
    else:
        print(f"  [WARN] No new data fetched")
        return 0


def ts_to_str(ts_ms):
    """Convert timestamp to human-readable string."""
    return datetime.fromtimestamp(ts_ms / 1000).strftime("%Y-%m-%d %H:%M")


def main():
    print("=" * 60)
    print("  XiaoTianQuant Data Sync")
    print("  Download real market data from Binance via VPN")
    print("=" * 60)

    # Check proxy
    print(f"\n[PROXY] Testing: {PROXY_URL}")
    opener = create_proxy_handler()
    try:
        req = request.Request("https://api.binance.com/api/v3/ping", headers={"Accept": "application/json"})
        resp = opener.open(req, timeout=10)
        if resp.status == 200:
            print("  [OK] Proxy is working! Binance API reachable.")
        else:
            print(f"  [WARN] Proxy test returned status {resp.status}")
    except Exception as e:
        print(f"  [FAIL] Proxy test failed: {e}")
        print("  Please check your VPN is running on port 7897")
        return 1

    # Check database
    if not os.path.exists(DB_PATH):
        print(f"\n[FAIL] Database not found: {DB_PATH}")
        return 1
    print(f"\n[DB] Database: {DB_PATH}")

    # Parse arguments
    args = sys.argv[1:]
    if args:
        symbols = [s.upper() for s in args[0].split(",")]
    else:
        symbols = DEFAULT_SYMBOLS

    intervals = DEFAULT_INTERVALS
    days_back = 90  # Default: 90 days

    print(f"\n[SYNC] Will sync: {', '.join(symbols)}")
    print(f"   Intervals: {', '.join(intervals)}")
    print(f"   History:   {days_back} days")

    total_saved = 0
    for symbol in symbols:
        for interval in intervals:
            saved = sync_symbol_interval(symbol, interval, days_back, opener, DB_PATH)
            total_saved += saved
            time.sleep(RATE_LIMIT_SLEEP * 2)  # Extra delay between pairs

    print("\n" + "=" * 60)
    print(f"  [DONE] Sync complete! Total bars saved: {total_saved}")
    print("  You can now train ML models in the web UI.")
    print("=" * 60)
    return 0


if __name__ == "__main__":
    sys.exit(main())
