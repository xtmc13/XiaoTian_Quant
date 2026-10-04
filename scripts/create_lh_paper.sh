#!/usr/bin/env bash
# 创建 liquidity_heat paper 观察实例并启动，然后轮询 runtime 状态。
# 用法: XT_ADMIN_PASSWORD=xxx ./create_lh_paper.sh
set -euo pipefail

BASE="http://localhost:8080/api"
PW="${XT_ADMIN_PASSWORD:?need XT_ADMIN_PASSWORD}"

TOKEN=$(curl -fsS -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PW\"}" | python3 -c '
import sys, json
d = json.load(sys.stdin)
data = d.get("data") or {}
t = d.get("token") or d.get("access_token") or data.get("token") or data.get("access_token")
if not t:
    print("login response missing token:", d, file=sys.stderr); sys.exit(1)
print(t)')
echo "[1/4] login ok"

CREATE=$(curl -fsS -X POST "$BASE/strategies/configs" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "name": "流动性热力扫反-paper观察",
  "symbol": "BTCUSDT",
  "strategy_type": "liquidity_heat",
  "category": "spot",
  "direction": "long",
  "execution_mode": "paper",
  "timeframe": "15m",
  "config": {
    "symbol": "BTCUSDT", "timeframe": "15m", "trade_direction": "long",
    "execution_mode": "paper",
    "lookback_bars": 300, "bins": 50, "volume_len": 10, "atr_len": 5,
    "pivot_bars": 2, "min_pool_strength_pct": 30,
    "tp_min_pool_strength_pct": 15, "tp_fallback_pct": 0.03,
    "sl_buffer_atr": 0.5, "position_size": 100, "max_hold_bars": 96,
    "first_order_amount": 100, "order_count": 3, "enable_add_position": true,
    "add_positions": [
      {"order": 2, "multiplier": 1.5, "spread": 0.01, "callback": 0.01},
      {"order": 3, "multiplier": 2.0, "spread": 0.01, "callback": 0.01}
    ],
    "tp_mode": "static", "take_profit_method": "full", "take_profit_ratio": 0.05,
    "profit_callback": 0
  }
}')
ID=$(echo "$CREATE" | python3 -c '
import sys, json
d = json.load(sys.stdin)
# 兼容 {data:{id}} / {id} / {success,data}
for k in ("data",):
    v = d.get(k)
    if isinstance(v, dict) and v.get("id"):
        print(v["id"]); sys.exit(0)
if d.get("id"):
    print(d["id"]); sys.exit(0)
print("create response missing id:", d, file=sys.stderr); sys.exit(1)')
echo "[2/4] created config id=$ID"

curl -fsS -X POST "$BASE/strategies/configs/$ID/start" \
  -H "Authorization: Bearer $TOKEN" >/dev/null
echo "[3/4] started"

echo "[4/4] runtime 快照:"
curl -fsS "$BASE/strategies/configs/$ID/runtime" -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
echo "实例 ID: $ID  （停止: POST $BASE/strategies/configs/$ID/stop）"
