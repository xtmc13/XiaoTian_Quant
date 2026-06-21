#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant 启动脚本
# ═══════════════════════════════════════════════════════════════════
set -e
DIR="$(cd "$(dirname "$0")" && pwd)"

# 确保标准目录存在
mkdir -p "${DIR}/data" "${DIR}/logs" "${DIR}/user_data"

echo -e "\033[0;36m"
cat <<'BANNER'
╔══════════════════════════════════════════════════════════════╗
║           XiaoTianQuant 多系统启动器                        ║
╚══════════════════════════════════════════════════════════════╝
BANNER
echo -e "\033[0m"

# 加载 .env 环境变量
if [ -f "${DIR}/.env" ]; then
  set -a
  # shellcheck source=/dev/null
  source "${DIR}/.env"
  set +a
fi

# Python venv 路径
VENV_PYTHON="${DIR}/sandbox/.venv/bin/python"

# ── 启动 Redis（可选）────────────────────────────────────────────
if command -v redis-server >/dev/null 2>&1 && ! pgrep -x redis-server >/dev/null 2>&1; then
  echo "[START] Redis :6379"
  redis-server --daemonize yes 2>/dev/null || true
fi

# ── 启动 CCXT Bridge ─────────────────────────────────────────────
if [ -f "${DIR}/sandbox/ccxt_bridge/main.py" ] && [ -f "$VENV_PYTHON" ]; then
  echo "[START] CCXT Bridge :8002"
  cd "${DIR}/sandbox/ccxt_bridge"
  nohup "$VENV_PYTHON" main.py > "${DIR}/logs/ccxt_bridge.log" 2>&1 &
  CCXT_PID=$!
  cd "${DIR}"
fi

# ── 启动 Python 策略引擎 ─────────────────────────────────────────
if [ -f "${DIR}/sandbox/strategy_engine/main.py" ] && [ -f "$VENV_PYTHON" ]; then
  echo "[START] Python Strategy Engine :8003"
  cd "${DIR}/sandbox/strategy_engine"
  nohup "$VENV_PYTHON" main.py > "${DIR}/logs/strategy_engine.log" 2>&1 &
  STRATEGY_PID=$!
  cd "${DIR}"
fi

# ── 启动 ML Server ───────────────────────────────────────────────
if [ -d "${DIR}/sandbox/ml_server" ] && command -v python3 >/dev/null 2>&1; then
  echo "[START] ML Server :8001"
  cd "${DIR}/sandbox/ml_server"
  nohup python3 -m uvicorn main:app --host 0.0.0.0 --port 8001 > "${DIR}/logs/ml_server.log" 2>&1 &
  ML_PID=$!
  cd "${DIR}"
fi

# ── 启动 Go Gateway ─────────────────────────────────────────────
GATEWAY_BIN="${DIR}/dist/gateway"
if [ -f "$GATEWAY_BIN" ]; then
  echo "[START] Gateway :8080"
  cd "${DIR}"
  nohup "$GATEWAY_BIN" > "${DIR}/logs/gateway.log" 2>&1 &
  GW_PID=$!
else
  echo "[ERROR] dist/gateway 未构建，请先运行: make build"
  exit 1
fi

echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo "║  All services started!                                    ║"
echo "║  Frontend: http://localhost:8080                        ║"
echo "║  Gateway:  http://localhost:8080/api                    ║"
echo "║  Logs:     ${DIR}/logs/                                 ║"
echo "║  Data:     ${DIR}/data/                                 ║"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""
echo "Press Ctrl+C to stop all services"

trap 'kill ${CCXT_PID} ${STRATEGY_PID} ${ML_PID} ${GW_PID} 2>/dev/null; exit' INT TERM
wait
